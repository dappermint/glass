package glass

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/dappermint/glass/gs"
)

// callable is one link of a method dispatch chain.
type callable func(args []reflect.Value) (reflect.Value, error)

type adviceEnt struct {
	kind string // "before", "after", "around"
	fn   *funcVal
}

// adviceSpec validates the (kind, fn) tail shared by advise and adviseMatch.
func adviceSpec(kindV, fnV reflect.Value) (string, *funcVal, error) {
	kind := kindV.String()
	if kind != "before" && kind != "after" && kind != "around" {
		return "", nil, fmt.Errorf("glass: advice kind must be before, after, or around, got %q", kind)
	}
	f, ok := asFuncVal(fnV)
	if !ok {
		return "", nil, fmt.Errorf("glass: advice wants a func literal as last arg")
	}
	minParams := 1
	if kind == "around" {
		minParams = 2
	}
	if len(f.params) < minParams {
		return "", nil, fmt.Errorf("glass: %s advice needs at least %d params (self%s, ...)", kind, minParams, map[bool]string{true: ", next", false: ""}[kind == "around"])
	}
	return kind, f, nil
}

func (in *Interp) addAdvice(typeName, method, kind string, f *funcVal) {
	m := in.advice[typeName]
	if m == nil {
		m = map[string][]adviceEnt{}
		in.advice[typeName] = m
	}
	m[method] = append(m[method], adviceEnt{kind: kind, fn: f})
}

// matchMethods quantifies a pointcut over registry methods plus this
// interp's shards.
func (in *Interp) matchMethods(typePat, methodPat string) []gs.MethodRef {
	refs := gs.Match(typePat, methodPat)
	seen := make(map[gs.MethodRef]bool, len(refs))
	for _, r := range refs {
		seen[r] = true
	}
	for typeName, m := range in.shards {
		if !gs.Glob(typePat, typeName) {
			continue
		}
		for name := range m {
			r := gs.MethodRef{Type: typeName, Method: name}
			if gs.Glob(methodPat, name) && !seen[r] {
				seen[r] = true
				refs = append(refs, r)
			}
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Type != refs[j].Type {
			return refs[i].Type < refs[j].Type
		}
		return refs[i].Method < refs[j].Method
	})
	return refs
}

// wrapAdvice builds the dispatch chain: core innermost, later advice
// outermost.
func wrapAdvice(list []adviceEnt, recv reflect.Value, core callable) callable {
	chain := core
	for _, adv := range list {
		inner := chain
		fn := adv.fn
		switch adv.kind {
		case "before":
			chain = func(args []reflect.Value) (reflect.Value, error) {
				if _, err := fn.call(prepend(recv, args)); err != nil {
					return reflect.Value{}, err
				}
				return inner(args)
			}
		case "after":
			chain = func(args []reflect.Value) (reflect.Value, error) {
				v, err := inner(args)
				if err != nil {
					return reflect.Value{}, err
				}
				if _, err := fn.call(prepend(recv, args)); err != nil {
					return reflect.Value{}, err
				}
				return v, nil
			}
		case "around":
			chain = func(args []reflect.Value) (reflect.Value, error) {
				nargs := make([]reflect.Value, 0, len(args)+2)
				nargs = append(nargs, recv, nextFor(inner))
				nargs = append(nargs, args...)
				return fn.call(nargs)
			}
		}
	}
	return chain
}

func prepend(recv reflect.Value, args []reflect.Value) []reflect.Value {
	out := make([]reflect.Value, 0, len(args)+1)
	out = append(out, recv)
	return append(out, args...)
}

// nextFor exposes the inner chain to around advice as a callable host func.
func nextFor(inner callable) reflect.Value {
	return reflect.ValueOf(func(args ...any) (any, error) {
		vals := make([]reflect.Value, len(args))
		for i, a := range args {
			vals[i] = reflect.ValueOf(a)
		}
		v, err := inner(vals)
		if err != nil {
			return nil, err
		}
		if !v.IsValid() {
			return nil, nil
		}
		return v.Interface(), nil
	})
}
