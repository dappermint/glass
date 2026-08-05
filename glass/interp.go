// Package glass is a tree-walking interpreter for a subset of Go, written in
// Go. Every runtime value is a reflect.Value and all struct access routes
// through the gs registry: the interpreter's world is exactly what has been
// registered, plus whatever the host binds with Define.
package glass

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
)

type Interp struct {
	global *scope
	shards map[string]map[string]*funcVal
	advice map[string]map[string][]adviceEnt
}

type scope struct {
	vars   map[string]reflect.Value
	parent *scope
}

func newScope(parent *scope) *scope {
	return &scope{vars: map[string]reflect.Value{}, parent: parent}
}

func (s *scope) lookup(name string) (reflect.Value, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if v, ok := cur.vars[name]; ok {
			return v, true
		}
	}
	return reflect.Value{}, false
}

func (s *scope) assign(name string, v reflect.Value) bool {
	for cur := s; cur != nil; cur = cur.parent {
		if _, ok := cur.vars[name]; ok {
			cur.vars[name] = v
			return true
		}
	}
	return false
}

func (s *scope) define(name string, v reflect.Value) { s.vars[name] = v }

func New() *Interp {
	in := &Interp{
		global: newScope(nil),
		shards: map[string]map[string]*funcVal{},
		advice: map[string]map[string][]adviceEnt{},
	}
	in.Define("print", func(args ...any) { fmt.Println(args...) })
	return in
}

// Define binds a host value or function into the interpreter's global scope.
// Bound functions following the error-last convention have their error
// surfaced as an evaluation error.
func (in *Interp) Define(name string, value any) {
	in.global.define(name, reflect.ValueOf(value))
}

// Eval runs one or more statements and returns the value of the last
// top-level expression statement, or nil if there is none. State persists
// across calls.
func (in *Interp) Eval(src string) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("glass: panic: %v", r)
		}
	}()

	fset := token.NewFileSet()
	file, perr := parser.ParseFile(fset, "glass", "package glass\nfunc _() {\n"+src+"\n}", parser.SkipObjectResolution)
	if perr != nil {
		return nil, fmt.Errorf("glass: parse: %w", perr)
	}
	body := file.Decls[0].(*ast.FuncDecl).Body

	var last reflect.Value
	for _, stmt := range body.List {
		v, serr := in.evalStmt(in.global, stmt)
		if serr != nil {
			if _, ok := serr.(*returnSignal); ok {
				return nil, fmt.Errorf("glass: return outside function")
			}
			return nil, serr
		}
		if _, ok := stmt.(*ast.ExprStmt); ok {
			last = v
		}
	}
	if !last.IsValid() {
		return nil, nil
	}
	return last.Interface(), nil
}
