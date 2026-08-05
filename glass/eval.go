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

	case *ast.RangeStmt:
		return in.evalRange(s, st)

	case *ast.SwitchStmt:
		return in.evalSwitch(s, st)

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
		vals := make(tupleVal, len(st.Results))
		for i, r := range st.Results {
			v, err := in.evalExpr(s, r)
			if err != nil {
				return reflect.Value{}, err
			}
			vals[i] = v
		}
		return reflect.Value{}, &returnSignal{v: reflect.ValueOf(vals)}

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
	if len(st.Lhs) > 1 {
		return in.evalMultiAssign(s, st)
	}
	if len(st.Rhs) != 1 {
		return fmt.Errorf("glass: assignment mismatch: 1 variable but %d values", len(st.Rhs))
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
	if _, ok := asTuple(rhs); ok {
		return fmt.Errorf("glass: multi-value expression in single-value context")
	}
	switch st.Tok {
	case token.DEFINE:
		id, ok := st.Lhs[0].(*ast.Ident)
		if !ok {
			return fmt.Errorf("glass: := needs an identifier on the left")
		}
		if id.Name != "_" {
			s.define(id.Name, rhs)
		}
		return nil
	case token.ASSIGN:
		return in.store(s, st.Lhs[0], rhs)
	}
	return fmt.Errorf("glass: assignment %s not supported", st.Tok)
}

// evalMultiAssign destructures a tuple (a, b := f()) or assigns in parallel
// (a, b = b, a); every rhs evaluates before any store.
func (in *Interp) evalMultiAssign(s *scope, st *ast.AssignStmt) error {
	if st.Tok != token.DEFINE && st.Tok != token.ASSIGN {
		return fmt.Errorf("glass: %s needs a single variable on the left", st.Tok)
	}
	var vals []reflect.Value
	if len(st.Rhs) == 1 {
		v, err := in.evalExpr(s, st.Rhs[0])
		if err != nil {
			return err
		}
		t, ok := asTuple(v)
		if !ok {
			return fmt.Errorf("glass: assignment mismatch: %d variables but 1 value", len(st.Lhs))
		}
		vals = t
	} else {
		if len(st.Rhs) != len(st.Lhs) {
			return fmt.Errorf("glass: assignment mismatch: %d variables but %d values", len(st.Lhs), len(st.Rhs))
		}
		for _, e := range st.Rhs {
			v, err := in.evalExpr(s, e)
			if err != nil {
				return err
			}
			if _, ok := asTuple(v); ok {
				return fmt.Errorf("glass: multi-value expression in multi-value assignment")
			}
			vals = append(vals, v)
		}
	}
	if len(vals) != len(st.Lhs) {
		return fmt.Errorf("glass: assignment mismatch: %d variables but %d values", len(st.Lhs), len(vals))
	}
	for i, lhs := range st.Lhs {
		if id, ok := lhs.(*ast.Ident); ok && id.Name == "_" {
			continue
		}
		if st.Tok == token.DEFINE {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				return fmt.Errorf("glass: := needs identifiers on the left")
			}
			s.define(id.Name, vals[i])
			continue
		}
		if err := in.store(s, lhs, vals[i]); err != nil {
			return err
		}
	}
	return nil
}

func (in *Interp) evalRange(s *scope, st *ast.RangeStmt) (reflect.Value, error) {
	inner := newScope(s)
	x, err := in.evalExpr(inner, st.X)
	if err != nil {
		return reflect.Value{}, err
	}
	x = indirect(x)
	if !x.IsValid() {
		return reflect.Value{}, fmt.Errorf("glass: cannot range over nil")
	}

	bind := func(lhs ast.Expr, v reflect.Value, it *scope) error {
		if lhs == nil {
			return nil
		}
		if st.Tok == token.DEFINE {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				return fmt.Errorf("glass: := needs identifiers on the left")
			}
			if id.Name != "_" {
				it.define(id.Name, v)
			}
			return nil
		}
		return in.store(it, lhs, v)
	}
	iter := func(k, v reflect.Value) (bool, error) {
		it := newScope(inner)
		if err := bind(st.Key, k, it); err != nil {
			return false, err
		}
		if err := bind(st.Value, v, it); err != nil {
			return false, err
		}
		_, err := in.evalStmt(it, st.Body)
		if err == errBreak {
			return true, nil
		}
		if err != nil && err != errContinue {
			return false, err
		}
		return false, nil
	}

	switch {
	case x.CanInt():
		if st.Value != nil {
			return reflect.Value{}, fmt.Errorf("glass: range over an integer has no second variable")
		}
		n := int(x.Int())
		for i := 0; i < n; i++ {
			done, err := iter(reflect.ValueOf(i), reflect.Value{})
			if done || err != nil {
				return reflect.Value{}, err
			}
		}
	case x.Kind() == reflect.String:
		for i, r := range x.String() {
			done, err := iter(reflect.ValueOf(i), reflect.ValueOf(r))
			if done || err != nil {
				return reflect.Value{}, err
			}
		}
	case x.Kind() == reflect.Slice || x.Kind() == reflect.Array:
		for i := 0; i < x.Len(); i++ {
			done, err := iter(reflect.ValueOf(i), indirect(x.Index(i)))
			if done || err != nil {
				return reflect.Value{}, err
			}
		}
	case x.Kind() == reflect.Map:
		mr := x.MapRange()
		for mr.Next() {
			done, err := iter(mr.Key(), indirect(mr.Value()))
			if done || err != nil {
				return reflect.Value{}, err
			}
		}
	default:
		return reflect.Value{}, fmt.Errorf("glass: cannot range over %s", tname(x))
	}
	return reflect.Value{}, nil
}

func (in *Interp) evalSwitch(s *scope, st *ast.SwitchStmt) (reflect.Value, error) {
	inner := newScope(s)
	if st.Init != nil {
		if _, err := in.evalStmt(inner, st.Init); err != nil {
			return reflect.Value{}, err
		}
	}
	var tag reflect.Value
	if st.Tag != nil {
		v, err := in.evalExpr(inner, st.Tag)
		if err != nil {
			return reflect.Value{}, err
		}
		tag = v
	}

	// break inside a case body stops the switch, not an enclosing loop
	runClause := func(cc *ast.CaseClause) (reflect.Value, error) {
		cs := newScope(inner)
		for _, sub := range cc.Body {
			if _, err := in.evalStmt(cs, sub); err != nil {
				if err == errBreak {
					return reflect.Value{}, nil
				}
				return reflect.Value{}, err
			}
		}
		return reflect.Value{}, nil
	}

	var deflt *ast.CaseClause
	for _, c := range st.Body.List {
		cc := c.(*ast.CaseClause)
		if cc.List == nil {
			deflt = cc
			continue
		}
		for _, e := range cc.List {
			v, err := in.evalExpr(inner, e)
			if err != nil {
				return reflect.Value{}, err
			}
			match := false
			if st.Tag != nil {
				r, err := binop(token.EQL, tag, v)
				if err != nil {
					return reflect.Value{}, err
				}
				match = r.Bool()
			} else {
				if !v.IsValid() || v.Kind() != reflect.Bool {
					return reflect.Value{}, fmt.Errorf("glass: non-bool switch case (%s)", tname(v))
				}
				match = v.Bool()
			}
			if match {
				return runClause(cc)
			}
		}
	}
	if deflt != nil {
		return runClause(deflt)
	}
	return reflect.Value{}, nil
}

func (in *Interp) store(s *scope, lhs ast.Expr, v reflect.Value) error {
	switch l := lhs.(type) {
	case *ast.Ident:
		if l.Name == "_" {
			return nil
		}
		if !s.assign(l.Name, v) {
			return fmt.Errorf("glass: undefined: %s", l.Name)
		}
		return nil
	case *ast.IndexExpr:
		x, err := in.evalExpr(s, l.X)
		if err != nil {
			return err
		}
		x = indirect(x)
		idx, err := in.evalExpr(s, l.Index)
		if err != nil {
			return err
		}
		if !x.IsValid() || !idx.IsValid() {
			return fmt.Errorf("glass: no value in index expression")
		}
		switch x.Kind() {
		case reflect.Map:
			key, err := coerce(idx, x.Type().Key(), "map key")
			if err != nil {
				return err
			}
			val, err := coerce(v, x.Type().Elem(), "map value")
			if err != nil {
				return err
			}
			x.SetMapIndex(key, val)
			return nil
		case reflect.Slice:
			if !idx.CanInt() {
				return fmt.Errorf("glass: non-integer index (%s)", tname(idx))
			}
			i := int(idx.Int())
			if i < 0 || i >= x.Len() {
				return fmt.Errorf("glass: index out of range [%d] with length %d", i, x.Len())
			}
			val, err := coerce(v, x.Type().Elem(), "element")
			if err != nil {
				return err
			}
			x.Index(i).Set(val)
			return nil
		}
		return fmt.Errorf("glass: cannot assign to index of %s", tname(x))
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
		case "nil":
			return reflect.Value{}, nil
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

	case *ast.SliceExpr:
		return in.evalSlice(s, e)

	case *ast.CompositeLit:
		return in.evalComposite(s, e)

	default:
		return reflect.Value{}, fmt.Errorf("glass: expression %T not supported", expr)
	}
}

func (in *Interp) evalSlice(s *scope, e *ast.SliceExpr) (reflect.Value, error) {
	if e.Slice3 {
		return reflect.Value{}, fmt.Errorf("glass: 3-index slices not supported")
	}
	x, err := in.evalExpr(s, e.X)
	if err != nil {
		return reflect.Value{}, err
	}
	x = indirect(x)
	if !x.IsValid() || (x.Kind() != reflect.Slice && x.Kind() != reflect.String) {
		return reflect.Value{}, fmt.Errorf("glass: cannot slice %s", tname(x))
	}
	lo, hi := 0, x.Len()
	bound := func(expr ast.Expr) (int, error) {
		v, err := in.evalExpr(s, expr)
		if err != nil {
			return 0, err
		}
		if !v.IsValid() || !v.CanInt() {
			return 0, fmt.Errorf("glass: non-integer slice bound (%s)", tname(v))
		}
		return int(v.Int()), nil
	}
	if e.Low != nil {
		if lo, err = bound(e.Low); err != nil {
			return reflect.Value{}, err
		}
	}
	if e.High != nil {
		if hi, err = bound(e.High); err != nil {
			return reflect.Value{}, err
		}
	}
	if lo < 0 || hi > x.Len() || lo > hi {
		return reflect.Value{}, fmt.Errorf("glass: slice bounds out of range [%d:%d] with length %d", lo, hi, x.Len())
	}
	return x.Slice(lo, hi), nil
}

func (in *Interp) evalComposite(s *scope, e *ast.CompositeLit) (reflect.Value, error) {
	if e.Type == nil {
		return reflect.Value{}, fmt.Errorf("glass: nested composite literals need explicit types")
	}
	t, err := typeFromExpr(e.Type)
	if err != nil {
		return reflect.Value{}, err
	}
	switch t.Kind() {
	case reflect.Slice:
		out := reflect.MakeSlice(t, 0, len(e.Elts))
		for _, el := range e.Elts {
			if _, keyed := el.(*ast.KeyValueExpr); keyed {
				return reflect.Value{}, fmt.Errorf("glass: keyed slice literals not supported")
			}
			v, err := in.evalExpr(s, el)
			if err != nil {
				return reflect.Value{}, err
			}
			v, err = coerce(v, t.Elem(), "element")
			if err != nil {
				return reflect.Value{}, err
			}
			out = reflect.Append(out, v)
		}
		return out, nil
	case reflect.Map:
		out := reflect.MakeMapWithSize(t, len(e.Elts))
		for _, el := range e.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				return reflect.Value{}, fmt.Errorf("glass: map literals need key: value pairs")
			}
			k, err := in.evalExpr(s, kv.Key)
			if err != nil {
				return reflect.Value{}, err
			}
			if k, err = coerce(k, t.Key(), "map key"); err != nil {
				return reflect.Value{}, err
			}
			v, err := in.evalExpr(s, kv.Value)
			if err != nil {
				return reflect.Value{}, err
			}
			if v, err = coerce(v, t.Elem(), "map value"); err != nil {
				return reflect.Value{}, err
			}
			out.SetMapIndex(k, v)
		}
		return out, nil
	}
	return reflect.Value{}, fmt.Errorf("glass: composite literal of %s not supported", t)
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
		key, err := coerce(idx, x.Type().Key(), "map key")
		if err != nil {
			return reflect.Value{}, err
		}
		v := x.MapIndex(key)
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
