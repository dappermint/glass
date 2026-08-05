package glass

import (
	"fmt"
	"go/ast"
	"reflect"
)

// funcVal is an interpreted function. Params are dynamically typed and the
// body closes over the scope where the literal was evaluated. When stored in
// the shard registry, the first param receives the instance.
type funcVal struct {
	params []string
	body   *ast.BlockStmt
	def    *scope
	in     *Interp
}

// returnSignal propagates a return value up through evalStmt as an error.
type returnSignal struct {
	v reflect.Value
}

func (r *returnSignal) Error() string { return "glass: return outside function" }

func makeFuncVal(in *Interp, s *scope, lit *ast.FuncLit) (reflect.Value, error) {
	params, err := paramNames(lit.Type)
	if err != nil {
		return reflect.Value{}, err
	}
	return reflect.ValueOf(&funcVal{params: params, body: lit.Body, def: s, in: in}), nil
}

// paramNames accepts both `func(self, msg any)` and the bare-ident form
// `func(self, msg)`, where Go's parser reads the idents as anonymous types.
// Declared types are ignored either way; glass params are dynamic.
func paramNames(ft *ast.FuncType) ([]string, error) {
	var names []string
	if ft.Params == nil {
		return names, nil
	}
	for _, f := range ft.Params.List {
		if len(f.Names) > 0 {
			for _, n := range f.Names {
				names = append(names, n.Name)
			}
			continue
		}
		id, ok := f.Type.(*ast.Ident)
		if !ok {
			return nil, fmt.Errorf("glass: func params are dynamically typed, use bare names: func(self, msg)")
		}
		names = append(names, id.Name)
	}
	return names, nil
}

func (f *funcVal) call(args []reflect.Value) (reflect.Value, error) {
	if len(args) != len(f.params) {
		return reflect.Value{}, fmt.Errorf("glass: func wants %d args, got %d", len(f.params), len(args))
	}
	s := newScope(f.def)
	for i, name := range f.params {
		s.define(name, args[i])
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
