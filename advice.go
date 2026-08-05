package glass

import (
	"reflect"
)

// callable is one link of a method dispatch chain: the shard-or-compiled
// core, or an advice wrapper around it.
type callable func(args []reflect.Value) (reflect.Value, error)

type adviceEnt struct {
	kind string // "before", "after", "around"
	fn   *funcVal
}

// wrapAdvice builds the dispatch chain. Advice added later wraps advice
// added earlier, with the core innermost, so the most recent advise() is the
// outermost layer.
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

// nextFor exposes the inner chain to around advice as a host func value,
// callable inside glass like any bound function.
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
