package glass

import (
	"fmt"
	"go/ast"
	"reflect"
)

// funcVal is an interpreted function: dynamically typed params, body closing
// over the defining scope. A variadic func binds its last param to an []any.
type funcVal struct {
	params   []string
	variadic bool
	body     *ast.BlockStmt
	def      *scope
	in       *Interp
}

// returnSignal carries a return value up through evalStmt as an error.
type returnSignal struct {
	v reflect.Value
}

func (r *returnSignal) Error() string { return "glass: return outside function" }

func makeFuncVal(in *Interp, s *scope, lit *ast.FuncLit) (reflect.Value, error) {
	params, variadic, err := paramNames(lit.Type)
	if err != nil {
		return reflect.Value{}, err
	}
	return reflect.ValueOf(&funcVal{params: params, variadic: variadic, body: lit.Body, def: s, in: in}), nil
}

// paramNames accepts the bare-ident form func(self, msg), where the parser
// reads the idents as anonymous types. Variadic funcs need the named form,
// func(self any, rest ...any), since Go rejects mixed named/unnamed params.
func paramNames(ft *ast.FuncType) ([]string, bool, error) {
	var names []string
	variadic := false
	if ft.Params == nil {
		return names, false, nil
	}
	for _, f := range ft.Params.List {
		if _, ok := f.Type.(*ast.Ellipsis); ok {
			if len(f.Names) == 0 {
				return nil, false, fmt.Errorf("glass: variadic param needs a name: func(self any, rest ...any)")
			}
			variadic = true
		}
		if len(f.Names) > 0 {
			for _, n := range f.Names {
				names = append(names, n.Name)
			}
			continue
		}
		id, ok := f.Type.(*ast.Ident)
		if !ok {
			return nil, false, fmt.Errorf("glass: func params are dynamically typed, use bare names: func(self, msg)")
		}
		names = append(names, id.Name)
	}
	return names, variadic, nil
}

func (f *funcVal) call(args []reflect.Value) (reflect.Value, error) {
	s := newScope(f.def)
	if f.variadic {
		fixed := len(f.params) - 1
		if len(args) < fixed {
			return reflect.Value{}, fmt.Errorf("glass: func wants at least %d args, got %d", fixed, len(args))
		}
		for i := 0; i < fixed; i++ {
			s.define(f.params[i], args[i])
		}
		rest := make([]any, 0, len(args)-fixed)
		for _, v := range args[fixed:] {
			if v.IsValid() {
				rest = append(rest, v.Interface())
			} else {
				rest = append(rest, nil)
			}
		}
		s.define(f.params[fixed], reflect.ValueOf(rest))
	} else {
		if len(args) != len(f.params) {
			return reflect.Value{}, fmt.Errorf("glass: func wants %d args, got %d", len(f.params), len(args))
		}
		for i, name := range f.params {
			s.define(name, args[i])
		}
	}
	_, err := f.in.evalStmt(s, f.body)
	if ret, ok := err.(*returnSignal); ok {
		return ret.v, nil
	}
	if err != nil {
		return reflect.Value{}, err
	}
	return reflect.Value{}, nil
}

func asFuncVal(v reflect.Value) (*funcVal, bool) {
	if !v.IsValid() || !v.CanInterface() {
		return nil, false
	}
	f, ok := v.Interface().(*funcVal)
	return f, ok
}
