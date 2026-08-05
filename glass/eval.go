package glass

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"strconv"

	"github.com/dappermint/glass/gs"
)

var (
	errBreak    = errors.New("glass: break outside loop")
	errContinue = errors.New("glass: continue outside loop")
)

var compound = map[token.Token]token.Token{
	token.ADD_ASSIGN: token.ADD,
	token.SUB_ASSIGN: token.SUB,
	token.MUL_ASSIGN: token.MUL,
	token.QUO_ASSIGN: token.QUO,
	token.REM_ASSIGN: token.REM,
}

func (in *Interp) evalStmt(s *scope, stmt ast.Stmt) (reflect.Value, error) {
	switch st := stmt.(type) {
	case *ast.ExprStmt:
		return in.evalExpr(s, st.X)

	case *ast.AssignStmt:
		return reflect.Value{}, in.evalAssign(s, st)

	case *ast.IncDecStmt:
		cur, err := in.evalExpr(s, st.X)
		if err != nil {
			return reflect.Value{}, err
		}
		op := token.ADD
		if st.Tok == token.DEC {
			op = token.SUB
		}
		v, err := binop(op, cur, reflect.ValueOf(1))
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.Value{}, in.store(s, st.X, v)

	case *ast.IfStmt:
		inner := newScope(s)
		if st.Init != nil {
			if _, err := in.evalStmt(inner, st.Init); err != nil {
				return reflect.Value{}, err
			}
		}
		cond, err := in.evalExpr(inner, st.Cond)
		if err != nil {
			return reflect.Value{}, err
		}
		if !cond.IsValid() || cond.Kind() != reflect.Bool {
			return reflect.Value{}, fmt.Errorf("glass: non-bool if condition (%s)", tname(cond))
		}
		if cond.Bool() {
			return in.evalStmt(inner, st.Body)
		}
		if st.Else != nil {
			return in.evalStmt(inner, st.Else)
		}
		return reflect.Value{}, nil

	case *ast.ForStmt:
		inner := newScope(s)
		if st.Init != nil {
			if _, err := in.evalStmt(inner, st.Init); err != nil {
				return reflect.Value{}, err
			}
		}
		for {
			if st.Cond != nil {
				cond, err := in.evalExpr(inner, st.Cond)
				if err != nil {
					return reflect.Value{}, err
				}
				if !cond.IsValid() || cond.Kind() != reflect.Bool {
					return reflect.Value{}, fmt.Errorf("glass: non-bool for condition (%s)", tname(cond))
				}
				if !cond.Bool() {
					break
				}
			}
			_, err := in.evalStmt(inner, st.Body)
			if err == errBreak {
				break
			}
			if err != nil && err != errContinue {
				return reflect.Value{}, err
			}
			if st.Post != nil {
				if _, err := in.evalStmt(inner, st.Post); err != nil {
					return reflect.Value{}, err
				}
			}
		}
		return reflect.Value{}, nil

	case *ast.BlockStmt:
		inner := newScope(s)
		for _, sub := range st.List {
			if _, err := in.evalStmt(inner, sub); err != nil {
				return reflect.Value{}, err
			}
		}
		return reflect.Value{}, nil

	case *ast.ReturnStmt:
		switch len(st.Results) {
		case 0:
			return reflect.Value{}, &returnSignal{}
		case 1:
			v, err := in.evalExpr(s, st.Results[0])
			if err != nil {
				return reflect.Value{}, err
			}
			return reflect.Value{}, &returnSignal{v: v}
		}
		return reflect.Value{}, fmt.Errorf("glass: multi-value return not supported")

	case *ast.BranchStmt:
		switch st.Tok {
		case token.BREAK:
			return reflect.Value{}, errBreak
		case token.CONTINUE:
			return reflect.Value{}, errContinue
		}
		return reflect.Value{}, fmt.Errorf("glass: %s not supported", st.Tok)

	case *ast.DeclStmt:
		return reflect.Value{}, fmt.Errorf("glass: var declarations not supported, use :=")

	default:
		return reflect.Value{}, fmt.Errorf("glass: statement %T not supported", stmt)
	}
}

func (in *Interp) evalAssign(s *scope, st *ast.AssignStmt) error {
	if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return fmt.Errorf("glass: multi-assign not supported")
	}
	if op, ok := compound[st.Tok]; ok {
		cur, err := in.evalExpr(s, st.Lhs[0])
		if err != nil {
			return err
		}
		rhs, err := in.evalExpr(s, st.Rhs[0])
		if err != nil {
			return err
		}
		v, err := binop(op, cur, rhs)
		if err != nil {
			return err
		}
		return in.store(s, st.Lhs[0], v)
	}

	rhs, err := in.evalExpr(s, st.Rhs[0])
	if err != nil {
		return err
	}
	switch st.Tok {
	case token.DEFINE:
		id, ok := st.Lhs[0].(*ast.Ident)
		if !ok {
			return fmt.Errorf("glass: := needs an identifier on the left")
		}
		s.define(id.Name, rhs)
		return nil
	case token.ASSIGN:
		return in.store(s, st.Lhs[0], rhs)
	}
	return fmt.Errorf("glass: assignment %s not supported", st.Tok)
}

func (in *Interp) store(s *scope, lhs ast.Expr, v reflect.Value) error {
	switch l := lhs.(type) {
	case *ast.Ident:
		if !s.assign(l.Name, v) {
			return fmt.Errorf("glass: undefined: %s", l.Name)
		}
		return nil
	case *ast.SelectorExpr:
		recv, err := in.evalExpr(s, l.X)
		if err != nil {
			return err
		}
		if !recv.IsValid() {
			return fmt.Errorf("glass: no value in selector")
		}
		if d, derr := gs.Describe(recv.Interface()); derr == nil {
			if fi, ok := d.Fields[l.Sel.Name]; ok {
				v = convertNumeric(v, fi.Type)
			}
		}
		return gs.Set(recv.Interface(), l.Sel.Name, v.Interface())
	}
	return fmt.Errorf("glass: cannot assign to %T", lhs)
}

func (in *Interp) evalExpr(s *scope, expr ast.Expr) (reflect.Value, error) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return evalLit(e)

	case *ast.Ident:
		switch e.Name {
		case "true":
			return reflect.ValueOf(true), nil
		case "false":
			return reflect.ValueOf(false), nil
		}
		if v, ok := s.lookup(e.Name); ok {
			return v, nil
		}
		return reflect.Value{}, fmt.Errorf("glass: undefined: %s", e.Name)

	case *ast.ParenExpr:
		return in.evalExpr(s, e.X)

	case *ast.UnaryExpr:
		x, err := in.evalExpr(s, e.X)
		if err != nil {
			return reflect.Value{}, err
		}
		return unop(e.Op, x)

	case *ast.BinaryExpr:
		if e.Op == token.LAND || e.Op == token.LOR {
			return in.evalShortCircuit(s, e)
		}
		x, err := in.evalExpr(s, e.X)
		if err != nil {
			return reflect.Value{}, err
		}
		y, err := in.evalExpr(s, e.Y)
		if err != nil {
			return reflect.Value{}, err
		}
		return binop(e.Op, x, y)

	case *ast.SelectorExpr:
		recv, err := in.evalExpr(s, e.X)
		if err != nil {
			return reflect.Value{}, err
		}
		if !recv.IsValid() {
			return reflect.Value{}, fmt.Errorf("glass: no value in selector")
		}
		got, err := gs.Get(recv.Interface(), e.Sel.Name)
		if err != nil {
			return reflect.Value{}, err
		}
		v := reflect.ValueOf(got)
		if !v.IsValid() {
			// nil-valued field: recover its static type for a typed zero
			if d, derr := gs.Describe(recv.Interface()); derr == nil {
				if fi, ok := d.Fields[e.Sel.Name]; ok {
					v = reflect.Zero(fi.Type)
				}
			}
		}
		return v, nil

	case *ast.CallExpr:
		return in.evalCall(s, e)

	case *ast.FuncLit:
		return makeFuncVal(in, s, e)

	case *ast.IndexExpr:
		return in.evalIndex(s, e)

	default:
		return reflect.Value{}, fmt.Errorf("glass: expression %T not supported", expr)
	}
}

func (in *Interp) evalShortCircuit(s *scope, e *ast.BinaryExpr) (reflect.Value, error) {
	x, err := in.evalExpr(s, e.X)
	if err != nil {
		return reflect.Value{}, err
	}
	if !x.IsValid() || x.Kind() != reflect.Bool {
		return reflect.Value{}, fmt.Errorf("glass: invalid operation: %s %s ...", tname(x), e.Op)
	}
	if e.Op == token.LAND && !x.Bool() {
		return reflect.ValueOf(false), nil
	}
	if e.Op == token.LOR && x.Bool() {
		return reflect.ValueOf(true), nil
	}
	y, err := in.evalExpr(s, e.Y)
	if err != nil {
		return reflect.Value{}, err
	}
	if !y.IsValid() || y.Kind() != reflect.Bool {
		return reflect.Value{}, fmt.Errorf("glass: invalid operation: ... %s %s", e.Op, tname(y))
	}
	return reflect.ValueOf(y.Bool()), nil
}

func (in *Interp) evalIndex(s *scope, e *ast.IndexExpr) (reflect.Value, error) {
	x, err := in.evalExpr(s, e.X)
	if err != nil {
		return reflect.Value{}, err
	}
	idx, err := in.evalExpr(s, e.Index)
	if err != nil {
		return reflect.Value{}, err
	}
	if !x.IsValid() || !idx.IsValid() {
		return reflect.Value{}, fmt.Errorf("glass: no value in index expression")
	}
	switch x.Kind() {
	case reflect.Slice, reflect.Array, reflect.String:
		if !idx.CanInt() {
			return reflect.Value{}, fmt.Errorf("glass: non-integer index (%s)", tname(idx))
		}
		i := int(idx.Int())
		if i < 0 || i >= x.Len() {
			return reflect.Value{}, fmt.Errorf("glass: index out of range [%d] with length %d", i, x.Len())
		}
		return indirect(x.Index(i)), nil
	case reflect.Map:
		if !idx.Type().AssignableTo(x.Type().Key()) {
			return reflect.Value{}, fmt.Errorf("glass: cannot use %s as map key type %s", tname(idx), x.Type().Key())
		}
		v := x.MapIndex(idx)
		if !v.IsValid() {
			v = reflect.Zero(x.Type().Elem())
		}
		return indirect(v), nil
	}
	return reflect.Value{}, fmt.Errorf("glass: cannot index %s", tname(x))
}

func evalLit(lit *ast.BasicLit) (reflect.Value, error) {
	switch lit.Kind {
	case token.INT:
		n, err := strconv.ParseInt(lit.Value, 0, 64)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("glass: bad int literal %s", lit.Value)
		}
		return reflect.ValueOf(int(n)), nil
	case token.FLOAT:
		f, err := strconv.ParseFloat(lit.Value, 64)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("glass: bad float literal %s", lit.Value)
		}
		return reflect.ValueOf(f), nil
	case token.STRING:
		str, err := strconv.Unquote(lit.Value)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("glass: bad string literal %s", lit.Value)
		}
		return reflect.ValueOf(str), nil
	case token.CHAR:
		str, err := strconv.Unquote(lit.Value)
		if err != nil || str == "" {
			return reflect.Value{}, fmt.Errorf("glass: bad char literal %s", lit.Value)
		}
		return reflect.ValueOf([]rune(str)[0]), nil
	}
	return reflect.Value{}, fmt.Errorf("glass: literal %s not supported", lit.Kind)
}
