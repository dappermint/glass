package gs

import (
	"fmt"
	"reflect"
)

func descriptorFor(instance any) (*Descriptor, reflect.Value, error) {
	if instance == nil {
		return nil, reflect.Value{}, fmt.Errorf("%w: nil instance", ErrNotRegistered)
	}
	v := reflect.ValueOf(instance)
	t := v.Type()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	mu.RLock()
	d, ok := byType[t]
	mu.RUnlock()
	if !ok {
		return nil, reflect.Value{}, fmt.Errorf("%w: %s", ErrNotRegistered, t)
	}
	return d, v, nil
}

// Describe returns the Descriptor for the dynamic type of instance,
// unwrapping pointers.
func Describe(instance any) (*Descriptor, error) {
	d, _, err := descriptorFor(instance)
	return d, err
}

// Get reads a field by Go name or tag alias. The instance may be a value or
// a pointer.
func Get(instance any, field string) (any, error) {
	d, v, err := descriptorFor(instance)
	if err != nil {
		return nil, err
	}
	fi, ok := d.Fields[field]
	if !ok {
		return nil, fmt.Errorf("%w: %s.%s", ErrNoSuchField, d.Name, field)
	}
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, fmt.Errorf("gs: Get on nil %s", d.Name)
		}
		v = v.Elem()
	}
	return v.Field(fi.index).Interface(), nil
}

// Set writes a field by Go name or tag alias. The instance must be a pointer:
// a reflection object can only be modified if it holds the original value,
// not a copy.
func Set(instance any, field string, value any) error {
	d, v, err := descriptorFor(instance)
	if err != nil {
		return err
	}
	fi, ok := d.Fields[field]
	if !ok {
		return fmt.Errorf("%w: %s.%s", ErrNoSuchField, d.Name, field)
	}
	if fi.ReadOnly {
		return fmt.Errorf("%w: %s.%s", ErrReadOnly, d.Name, fi.Name)
	}
	if v.Kind() != reflect.Pointer {
		return fmt.Errorf("%w: Set(%s.%s) was given a %s by value", ErrNeedPointer, d.Name, fi.Name, d.Name)
	}
	if v.IsNil() {
		return fmt.Errorf("gs: Set on nil %s", d.Name)
	}
	f := v.Elem().Field(fi.index)
	if !f.CanSet() {
		return fmt.Errorf("%w: %s.%s", ErrNotSettable, d.Name, fi.Name)
	}
	val, err := coerce(fi.Type, value)
	if err != nil {
		return fmt.Errorf("%s.%s: %w", d.Name, fi.Name, err)
	}
	f.Set(val)
	return nil
}

// Call invokes a method by name and returns its results as plain values.
// Pointer-receiver methods require a pointer instance.
func Call(instance any, method string, args ...any) ([]any, error) {
	d, v, err := descriptorFor(instance)
	if err != nil {
		return nil, err
	}
	mi, ok := d.Methods[method]
	if !ok {
		return nil, fmt.Errorf("%w: %s.%s", ErrNoSuchMethod, d.Name, method)
	}
	m := v.MethodByName(mi.Name)
	if !m.IsValid() {
		if v.Kind() != reflect.Pointer {
			return nil, fmt.Errorf("%w: %s.%s has a pointer receiver", ErrNeedPointer, d.Name, method)
		}
		return nil, fmt.Errorf("%w: %s.%s", ErrNoSuchMethod, d.Name, method)
	}

	mt := m.Type()
	var in []reflect.Value
	if mt.IsVariadic() {
		fixed := mt.NumIn() - 1
		if len(args) < fixed {
			return nil, fmt.Errorf("%w: %s.%s wants at least %d args, got %d", ErrBadArg, d.Name, method, fixed, len(args))
		}
		in = make([]reflect.Value, 0, len(args))
		for i := 0; i < fixed; i++ {
			av, err := coerce(mt.In(i), args[i])
			if err != nil {
				return nil, fmt.Errorf("%s.%s arg %d: %w", d.Name, method, i, err)
			}
			in = append(in, av)
		}
		elem := mt.In(fixed).Elem()
		for i := fixed; i < len(args); i++ {
			av, err := coerce(elem, args[i])
			if err != nil {
				return nil, fmt.Errorf("%s.%s arg %d: %w", d.Name, method, i, err)
			}
			in = append(in, av)
		}
	} else {
		if len(args) != mt.NumIn() {
			return nil, fmt.Errorf("%w: %s.%s wants %d args, got %d", ErrBadArg, d.Name, method, mt.NumIn(), len(args))
		}
		in = make([]reflect.Value, len(args))
		for i, a := range args {
			av, err := coerce(mt.In(i), a)
			if err != nil {
				return nil, fmt.Errorf("%s.%s arg %d: %w", d.Name, method, i, err)
			}
			in[i] = av
		}
	}

	out := m.Call(in)
	res := make([]any, len(out))
	for i, o := range out {
		res[i] = o.Interface()
	}
	return res, nil
}

func coerce(t reflect.Type, a any) (reflect.Value, error) {
	if a == nil {
		switch t.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer:
			return reflect.Zero(t), nil
		}
		return reflect.Value{}, fmt.Errorf("%w: cannot use nil as %s", ErrBadArg, t)
	}
	v := reflect.ValueOf(a)
	if v.Type().AssignableTo(t) {
		return v, nil
	}
	return reflect.Value{}, fmt.Errorf("%w: cannot use %s as %s", ErrBadArg, v.Type(), t)
}
