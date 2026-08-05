package glass

import (
	"fmt"
	"go/token"
	"reflect"
)

// indirect unwraps interface-typed values (e.g. results of host funcs
// returning any) so operators see the dynamic type.
func indirect(v reflect.Value) reflect.Value {
	if v.IsValid() && v.Kind() == reflect.Interface && !v.IsNil() {
		return v.Elem()
	}
	return v
}

func tname(v reflect.Value) string {
	if !v.IsValid() {
		return "<no value>"
	}
	return v.Type().String()
}

func isInt(v reflect.Value) bool   { return v.IsValid() && v.CanInt() }
func isFloat(v reflect.Value) bool { return v.IsValid() && v.CanFloat() }

func toFloat(v reflect.Value) float64 {
	if isInt(v) {
		return float64(v.Int())
	}
	return v.Float()
}

// convertNumeric bridges the interpreter's default literal types (int,
// float64) to whatever numeric type the target wants. Non-numeric or already
// assignable values pass through untouched.
func convertNumeric(v reflect.Value, t reflect.Type) reflect.Value {
	if !v.IsValid() || v.Type().AssignableTo(t) {
		return v
	}
	if (isInt(v) || isFloat(v)) && isNumKind(t.Kind()) && v.Type().ConvertibleTo(t) {
		return v.Convert(t)
	}
	return v
}

func isNumKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

func binop(op token.Token, x, y reflect.Value) (reflect.Value, error) {
	if !x.IsValid() || !y.IsValid() {
		return reflect.Value{}, fmt.Errorf("glass: no value in expression")
	}

	switch {
	case isInt(x) && isInt(y):
		a, b := x.Int(), y.Int()
		switch op {
		case token.ADD:
			return reflect.ValueOf(int(a + b)), nil
		case token.SUB:
			return reflect.ValueOf(int(a - b)), nil
		case token.MUL:
			return reflect.ValueOf(int(a * b)), nil
		case token.QUO:
			if b == 0 {
				return reflect.Value{}, fmt.Errorf("glass: division by zero")
			}
			return reflect.ValueOf(int(a / b)), nil
		case token.REM:
			if b == 0 {
				return reflect.Value{}, fmt.Errorf("glass: division by zero")
			}
			return reflect.ValueOf(int(a % b)), nil
		case token.LSS:
			return reflect.ValueOf(a < b), nil
		case token.LEQ:
			return reflect.ValueOf(a <= b), nil
		case token.GTR:
			return reflect.ValueOf(a > b), nil
		case token.GEQ:
			return reflect.ValueOf(a >= b), nil
		case token.EQL:
			return reflect.ValueOf(a == b), nil
		case token.NEQ:
			return reflect.ValueOf(a != b), nil
		}

	case (isInt(x) || isFloat(x)) && (isInt(y) || isFloat(y)):
		a, b := toFloat(x), toFloat(y)
		switch op {
		case token.ADD:
			return reflect.ValueOf(a + b), nil
		case token.SUB:
			return reflect.ValueOf(a - b), nil
		case token.MUL:
			return reflect.ValueOf(a * b), nil
		case token.QUO:
			return reflect.ValueOf(a / b), nil
		case token.LSS:
			return reflect.ValueOf(a < b), nil
		case token.LEQ:
			return reflect.ValueOf(a <= b), nil
		case token.GTR:
			return reflect.ValueOf(a > b), nil
		case token.GEQ:
			return reflect.ValueOf(a >= b), nil
		case token.EQL:
			return reflect.ValueOf(a == b), nil
		case token.NEQ:
			return reflect.ValueOf(a != b), nil
		}

	case x.Kind() == reflect.String && y.Kind() == reflect.String:
		a, b := x.String(), y.String()
		switch op {
		case token.ADD:
			return reflect.ValueOf(a + b), nil
		case token.LSS:
			return reflect.ValueOf(a < b), nil
		case token.LEQ:
			return reflect.ValueOf(a <= b), nil
		case token.GTR:
			return reflect.ValueOf(a > b), nil
		case token.GEQ:
			return reflect.ValueOf(a >= b), nil
		case token.EQL:
			return reflect.ValueOf(a == b), nil
		case token.NEQ:
			return reflect.ValueOf(a != b), nil
		}

	case x.Kind() == reflect.Bool && y.Kind() == reflect.Bool:
		switch op {
		case token.EQL:
			return reflect.ValueOf(x.Bool() == y.Bool()), nil
		case token.NEQ:
			return reflect.ValueOf(x.Bool() != y.Bool()), nil
		}

	default:
		if (op == token.EQL || op == token.NEQ) && x.Type() == y.Type() && x.Type().Comparable() {
			eq := x.Interface() == y.Interface()
			if op == token.NEQ {
				eq = !eq
			}
			return reflect.ValueOf(eq), nil
		}
	}
	return reflect.Value{}, fmt.Errorf("glass: invalid operation: %s %s %s", tname(x), op, tname(y))
}

func unop(op token.Token, x reflect.Value) (reflect.Value, error) {
	if !x.IsValid() {
		return reflect.Value{}, fmt.Errorf("glass: no value in expression")
	}
	switch op {
	case token.SUB:
		if isInt(x) {
			return reflect.ValueOf(int(-x.Int())), nil
		}
		if isFloat(x) {
			return reflect.ValueOf(-x.Float()), nil
		}
	case token.ADD:
		if isInt(x) || isFloat(x) {
			return x, nil
		}
	case token.NOT:
		if x.Kind() == reflect.Bool {
			return reflect.ValueOf(!x.Bool()), nil
		}
	}
	return reflect.Value{}, fmt.Errorf("glass: invalid operation: %s %s", op, tname(x))
}
