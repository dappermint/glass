package glass

import (
	"fmt"
	"go/ast"
	"reflect"
)

// tupleVal carries multiple values from a call or return to the multi-assign
// that destructures it; it is never a first-class interpreter value.
type tupleVal []reflect.Value

var (
	tupleType = reflect.TypeOf(tupleVal(nil))
	anyType   = reflect.TypeOf((*any)(nil)).Elem()
)

func asTuple(v reflect.Value) (tupleVal, bool) {
	if v.IsValid() && v.Type() == tupleType {
		return v.Interface().(tupleVal), true
	}
	return nil, false
}

var basicTypes = map[string]reflect.Type{
	"int":     reflect.TypeOf(int(0)),
	"int64":   reflect.TypeOf(int64(0)),
	"float64": reflect.TypeOf(float64(0)),
	"string":  reflect.TypeOf(""),
	"bool":    reflect.TypeOf(false),
	"rune":    reflect.TypeOf(rune(0)),
	"byte":    reflect.TypeOf(byte(0)),
	"any":     anyType,
}

// typeFromExpr resolves the type syntax allowed in composite literals and
// make: basic types, any, and slice/map compositions of those.
func typeFromExpr(e ast.Expr) (reflect.Type, error) {
	switch t := e.(type) {
	case *ast.Ident:
		if bt, ok := basicTypes[t.Name]; ok {
			return bt, nil
		}
		return nil, fmt.Errorf("glass: unknown type %s", t.Name)
	case *ast.ArrayType:
		if t.Len != nil {
			return nil, fmt.Errorf("glass: fixed-size arrays not supported, use a slice")
		}
		elem, err := typeFromExpr(t.Elt)
		if err != nil {
			return nil, err
		}
		return reflect.SliceOf(elem), nil
	case *ast.MapType:
		key, err := typeFromExpr(t.Key)
		if err != nil {
			return nil, err
		}
		val, err := typeFromExpr(t.Value)
		if err != nil {
			return nil, err
		}
		return reflect.MapOf(key, val), nil
	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return anyType, nil
		}
	}
	return nil, fmt.Errorf("glass: unsupported type expression %T", e)
}

// coerce converts v toward t and errors if the result is not assignable.
func coerce(v reflect.Value, t reflect.Type, what string) (reflect.Value, error) {
	if !v.IsValid() {
		switch t.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
			return reflect.Zero(t), nil
		}
		return reflect.Value{}, fmt.Errorf("glass: no value in %s", what)
	}
	v = convertNumeric(v, t)
	if !v.Type().AssignableTo(t) {
		return reflect.Value{}, fmt.Errorf("glass: %s: cannot use %s as %s", what, v.Type(), t)
	}
	return v, nil
}
