package glass

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"sort"

	"github.com/dappermint/glass/gs"
)

var errType = reflect.TypeOf((*error)(nil)).Elem()

func (in *Interp) evalCall(s *scope, call *ast.CallExpr) (reflect.Value, error) {
	if call.Ellipsis != token.NoPos {
		return reflect.Value{}, fmt.Errorf("glass: spread (...) not supported")
	}
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		return in.callMethod(s, fun, call.Args)
	case *ast.Ident:
		if v, ok := s.lookup(fun.Name); ok {
			if f, isShardFn := asFuncVal(v); isShardFn {
				args, err := in.evalArgs(s, call.Args)
				if err != nil {
					return reflect.Value{}, err
				}
				return f.call(args)
			}
			if v.Kind() == reflect.Func {
				return in.callHostFunc(s, fun.Name, v, call.Args)
			}
		}
		return in.callBuiltin(s, fun.Name, call.Args)
	}
	// anything else (paren'd expressions, immediately-invoked literals)
	v, err := in.evalExpr(s, call.Fun)
	if err != nil {
		return reflect.Value{}, err
	}
	if f, ok := asFuncVal(v); ok {
		args, err := in.evalArgs(s, call.Args)
		if err != nil {
			return reflect.Value{}, err
		}
		return f.call(args)
	}
	if v.IsValid() && v.Kind() == reflect.Func {
		return in.callHostFunc(s, "func", v, call.Args)
	}
	return reflect.Value{}, fmt.Errorf("glass: cannot call %s", tname(v))
}

func (in *Interp) callMethod(s *scope, fun *ast.SelectorExpr, argExprs []ast.Expr) (reflect.Value, error) {
	recv, err := in.evalExpr(s, fun.X)
	if err != nil {
		return reflect.Value{}, err
	}
	if !recv.IsValid() {
		return reflect.Value{}, fmt.Errorf("glass: no value in method call")
	}
	d, err := gs.Describe(recv.Interface())
	if err != nil {
		return reflect.Value{}, err
	}
	core, err := in.coreCallable(d, recv, fun.Sel.Name)
	if err != nil {
		return reflect.Value{}, err
	}
	args, err := in.evalArgs(s, argExprs)
	if err != nil {
		return reflect.Value{}, err
	}
	if list := in.advice[d.Name][fun.Sel.Name]; len(list) > 0 {
		return wrapAdvice(list, recv, core)(args)
	}
	return core(args)
}

// coreCallable resolves the underlying implementation for a method name:
// shards take precedence over compiled methods, so a shard can replace a
// compiled method at runtime and mend restores it.
func (in *Interp) coreCallable(d *gs.Descriptor, recv reflect.Value, name string) (callable, error) {
	if f, ok := in.shards[d.Name][name]; ok {
		return func(args []reflect.Value) (reflect.Value, error) {
			return f.call(prepend(recv, args))
		}, nil
	}
	mi, ok := d.Methods[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s.%s", gs.ErrNoSuchMethod, d.Name, name)
	}
	return func(args []reflect.Value) (reflect.Value, error) {
		converted, err := convertArgs(mi.In, mi.Variadic, args)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("%s.%s: %w", d.Name, name, err)
		}
		anyArgs := make([]any, len(converted))
		for i, v := range converted {
			anyArgs[i] = v.Interface()
		}
		out, err := gs.Call(recv.Interface(), name, anyArgs...)
		if err != nil {
			return reflect.Value{}, err
		}
		return wrapResults(out), nil
	}, nil
}

func (in *Interp) callHostFunc(s *scope, name string, fn reflect.Value, argExprs []ast.Expr) (reflect.Value, error) {
	ft := fn.Type()
	params := make([]reflect.Type, ft.NumIn())
	for i := range params {
		params[i] = ft.In(i)
	}
	args, err := in.evalArgs(s, argExprs)
	if err != nil {
		return reflect.Value{}, err
	}
	converted, err := convertArgs(params, ft.IsVariadic(), args)
	if err != nil {
		return reflect.Value{}, fmt.Errorf("%s: %w", name, err)
	}

	out := fn.Call(converted)
	if n := len(out); n > 0 && ft.Out(n-1) == errType {
		if !out[n-1].IsNil() {
			return reflect.Value{}, out[n-1].Interface().(error)
		}
		out = out[:n-1]
	}
	switch len(out) {
	case 0:
		return reflect.Value{}, nil
	case 1:
		return indirect(out[0]), nil
	}
	vals := make([]any, len(out))
	for i, o := range out {
		vals[i] = o.Interface()
	}
	return reflect.ValueOf(vals), nil
}

func (in *Interp) callBuiltin(s *scope, name string, argExprs []ast.Expr) (reflect.Value, error) {
	args, err := in.evalArgs(s, argExprs)
	if err != nil {
		return reflect.Value{}, err
	}
	need := func(n int) error {
		if len(args) != n {
			return fmt.Errorf("glass: %s wants %d args, got %d", name, n, len(args))
		}
		for _, a := range args {
			if !a.IsValid() {
				return fmt.Errorf("glass: no value in argument to %s", name)
			}
		}
		return nil
	}

	switch name {
	case "new":
		if err := need(1); err != nil {
			return reflect.Value{}, err
		}
		if args[0].Kind() != reflect.String {
			return reflect.Value{}, fmt.Errorf("glass: new wants a type name string")
		}
		obj, err := gs.New(args[0].String())
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(obj), nil

	case "types":
		if err := need(0); err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(gs.Types()), nil

	case "fields", "methods":
		if err := need(1); err != nil {
			return reflect.Value{}, err
		}
		d, err := gs.Describe(args[0].Interface())
		if err != nil {
			return reflect.Value{}, err
		}
		if name == "fields" {
			return reflect.ValueOf(d.FieldNames()), nil
		}
		names := d.MethodNames()
		if m := in.shards[d.Name]; len(m) > 0 {
			seen := map[string]bool{}
			for _, n := range names {
				seen[n] = true
			}
			for n := range m {
				if !seen[n] {
					names = append(names, n)
				}
			}
			sort.Strings(names)
		}
		return reflect.ValueOf(names), nil

	case "shard":
		if err := need(3); err != nil {
			return reflect.Value{}, err
		}
		if args[0].Kind() != reflect.String || args[1].Kind() != reflect.String {
			return reflect.Value{}, fmt.Errorf("glass: shard wants (typeName, methodName, func)")
		}
		f, ok := asFuncVal(args[2])
		if !ok {
			return reflect.Value{}, fmt.Errorf("glass: shard wants a func literal as third arg")
		}
		if len(f.params) == 0 {
			return reflect.Value{}, fmt.Errorf("glass: shard func needs a receiver param: func(self, ...)")
		}
		typeName := args[0].String()
		d, ok := gs.Lookup(typeName)
		if !ok {
			return reflect.Value{}, fmt.Errorf("%w: %q", gs.ErrNotRegistered, typeName)
		}
		m := in.shards[d.Name]
		if m == nil {
			m = map[string]*funcVal{}
			in.shards[d.Name] = m
		}
		m[args[1].String()] = f
		return reflect.Value{}, nil

	case "mend":
		if err := need(2); err != nil {
			return reflect.Value{}, err
		}
		if args[0].Kind() != reflect.String || args[1].Kind() != reflect.String {
			return reflect.Value{}, fmt.Errorf("glass: mend wants (typeName, methodName)")
		}
		m := in.shards[args[0].String()]
		_, existed := m[args[1].String()]
		delete(m, args[1].String())
		return reflect.ValueOf(existed), nil

	case "advise":
		if err := need(4); err != nil {
			return reflect.Value{}, err
		}
		if args[0].Kind() != reflect.String || args[1].Kind() != reflect.String || args[2].Kind() != reflect.String {
			return reflect.Value{}, fmt.Errorf("glass: advise wants (typeName, methodName, kind, func)")
		}
		kind := args[2].String()
		if kind != "before" && kind != "after" && kind != "around" {
			return reflect.Value{}, fmt.Errorf("glass: advice kind must be before, after, or around, got %q", kind)
		}
		f, ok := asFuncVal(args[3])
		if !ok {
			return reflect.Value{}, fmt.Errorf("glass: advise wants a func literal as fourth arg")
		}
		minParams := 1
		if kind == "around" {
			minParams = 2
		}
		if len(f.params) < minParams {
			return reflect.Value{}, fmt.Errorf("glass: %s advice needs at least %d params (self%s, ...)", kind, minParams, map[bool]string{true: ", next", false: ""}[kind == "around"])
		}
		typeName := args[0].String()
		d, ok := gs.Lookup(typeName)
		if !ok {
			return reflect.Value{}, fmt.Errorf("%w: %q", gs.ErrNotRegistered, typeName)
		}
		m := in.advice[d.Name]
		if m == nil {
			m = map[string][]adviceEnt{}
			in.advice[d.Name] = m
		}
		m[args[1].String()] = append(m[args[1].String()], adviceEnt{kind: kind, fn: f})
		return reflect.Value{}, nil

	case "unadvise":
		if err := need(2); err != nil {
			return reflect.Value{}, err
		}
		if args[0].Kind() != reflect.String || args[1].Kind() != reflect.String {
			return reflect.Value{}, fmt.Errorf("glass: unadvise wants (typeName, methodName)")
		}
		m := in.advice[args[0].String()]
		n := len(m[args[1].String()])
		delete(m, args[1].String())
		return reflect.ValueOf(n), nil

	case "patch":
		if err := need(3); err != nil {
			return reflect.Value{}, err
		}
		if args[0].Kind() != reflect.String || args[1].Kind() != reflect.String {
			return reflect.Value{}, fmt.Errorf("glass: patch wants (typeName, methodName, func)")
		}
		f, ok := asFuncVal(args[2])
		if !ok {
			return reflect.Value{}, fmt.Errorf("glass: patch wants a func literal as third arg")
		}
		if len(f.params) == 0 {
			return reflect.Value{}, fmt.Errorf("glass: patch func needs a receiver param: func(self, ...)")
		}
		err := gs.Patch(args[0].String(), args[1].String(), func(recv any, pargs []any) []any {
			vals := make([]reflect.Value, 0, len(pargs)+1)
			vals = append(vals, reflect.ValueOf(recv))
			for _, a := range pargs {
				vals = append(vals, reflect.ValueOf(a))
			}
			out, err := f.call(vals)
			if err != nil {
				// the caller is compiled code with no error channel;
				// a failing patch is a failing method
				panic(err)
			}
			if !out.IsValid() {
				return nil
			}
			return []any{out.Interface()}
		})
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.Value{}, nil

	case "unpatch":
		if err := need(2); err != nil {
			return reflect.Value{}, err
		}
		if args[0].Kind() != reflect.String || args[1].Kind() != reflect.String {
			return reflect.Value{}, fmt.Errorf("glass: unpatch wants (typeName, methodName)")
		}
		return reflect.ValueOf(gs.Unpatch(args[0].String(), args[1].String())), nil

	case "shards":
		if err := need(1); err != nil {
			return reflect.Value{}, err
		}
		typeName := ""
		if args[0].Kind() == reflect.String {
			typeName = args[0].String()
		} else {
			d, err := gs.Describe(args[0].Interface())
			if err != nil {
				return reflect.Value{}, err
			}
			typeName = d.Name
		}
		names := make([]string, 0, len(in.shards[typeName]))
		for n := range in.shards[typeName] {
			names = append(names, n)
		}
		sort.Strings(names)
		return reflect.ValueOf(names), nil

	case "len":
		if err := need(1); err != nil {
			return reflect.Value{}, err
		}
		switch args[0].Kind() {
		case reflect.String, reflect.Slice, reflect.Array, reflect.Map:
			return reflect.ValueOf(args[0].Len()), nil
		}
		return reflect.Value{}, fmt.Errorf("glass: cannot take len of %s", tname(args[0]))
	}
	return reflect.Value{}, fmt.Errorf("glass: undefined function: %s", name)
}

func (in *Interp) evalArgs(s *scope, exprs []ast.Expr) ([]reflect.Value, error) {
	args := make([]reflect.Value, len(exprs))
	for i, e := range exprs {
		v, err := in.evalExpr(s, e)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	return args, nil
}

func convertArgs(params []reflect.Type, variadic bool, args []reflect.Value) ([]reflect.Value, error) {
	check := func(v reflect.Value, t reflect.Type, i int) (reflect.Value, error) {
		if !v.IsValid() {
			switch t.Kind() {
			case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
				return reflect.Zero(t), nil
			}
			return reflect.Value{}, fmt.Errorf("glass: no value in arg %d", i)
		}
		v = convertNumeric(v, t)
		if !v.Type().AssignableTo(t) {
			return reflect.Value{}, fmt.Errorf("glass: arg %d: cannot use %s as %s", i, v.Type(), t)
		}
		return v, nil
	}

	if variadic {
		fixed := len(params) - 1
		if len(args) < fixed {
			return nil, fmt.Errorf("glass: wants at least %d args, got %d", fixed, len(args))
		}
		out := make([]reflect.Value, 0, len(args))
		for i := 0; i < fixed; i++ {
			v, err := check(args[i], params[i], i)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		elem := params[fixed].Elem()
		for i := fixed; i < len(args); i++ {
			v, err := check(args[i], elem, i)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}

	if len(args) != len(params) {
		return nil, fmt.Errorf("glass: wants %d args, got %d", len(params), len(args))
	}
	out := make([]reflect.Value, len(args))
	for i := range args {
		v, err := check(args[i], params[i], i)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func wrapResults(out []any) reflect.Value {
	switch len(out) {
	case 0:
		return reflect.Value{}
	case 1:
		return reflect.ValueOf(out[0])
	}
	return reflect.ValueOf(out)
}
