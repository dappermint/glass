// Package gs is a reflection-first framework for Go: register a type once,
// then construct, inspect, mutate, and call it entirely by string name.
//
// The design follows the three laws of reflection:
//
//  1. interface -> reflection object: Register walks a type with reflect once
//     and caches a Descriptor, so the per-call cost is lookup, not analysis.
//  2. reflection object -> interface: Call and Get unpack reflect.Values back
//     to plain any values, so callers never touch the reflect package.
//  3. settability: Set and pointer-receiver Call require a pointer instance
//     and return a clear error instead of panicking on unaddressable values.
package gs

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

type FieldInfo struct {
	Name     string
	Alias    string
	ReadOnly bool
	Type     reflect.Type
	index    int
}

type MethodInfo struct {
	Name     string
	In       []reflect.Type
	Out      []reflect.Type
	Variadic bool
}

type Descriptor struct {
	Name    string
	Fields  map[string]FieldInfo
	Methods map[string]MethodInfo
	rtype   reflect.Type
	ptype   reflect.Type
}

func (d *Descriptor) Type() reflect.Type { return d.rtype }

func (d *Descriptor) FieldNames() []string {
	seen := map[string]bool{}
	var names []string
	for _, fi := range d.Fields {
		if !seen[fi.Name] {
			seen[fi.Name] = true
			names = append(names, fi.Name)
		}
	}
	sort.Strings(names)
	return names
}

func (d *Descriptor) MethodNames() []string {
	names := make([]string, 0, len(d.Methods))
	for name := range d.Methods {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type Option func(*config)

type config struct {
	name string
}

// WithName overrides the registry key, which defaults to the type's
// "pkg.Type" string.
func WithName(name string) Option {
	return func(c *config) { c.name = name }
}

var (
	mu     sync.RWMutex
	byName = map[string]*Descriptor{}
	byType = map[reflect.Type]*Descriptor{}
)

// Register analyzes struct type T and adds it to the registry. It panics on
// non-struct types or conflicting registrations; registration is expected to
// happen in init, where panicking on programmer error is conventional.
func Register[T any](opts ...Option) *Descriptor {
	return register(reflect.TypeOf((*T)(nil)).Elem(), opts...)
}

// RegisterValue registers the dynamic type of v, unwrapping pointers.
// Use it when you only hold an any; prefer Register when the type is static.
func RegisterValue(v any, opts ...Option) *Descriptor {
	if v == nil {
		panic("gs: RegisterValue called with nil")
	}
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return register(t, opts...)
}

func register(t reflect.Type, opts ...Option) *Descriptor {
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("gs: Register requires a struct type, got %s (kind %s)", t, t.Kind()))
	}
	cfg := config{name: t.String()}
	for _, o := range opts {
		o(&cfg)
	}

	d := &Descriptor{
		Name:    cfg.name,
		Fields:  map[string]FieldInfo{},
		Methods: map[string]MethodInfo{},
		rtype:   t,
		ptype:   reflect.PointerTo(t),
	}

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("gs")
		if tag == "-" {
			continue
		}
		fi := FieldInfo{Name: sf.Name, Type: sf.Type, index: i}
		if tag != "" {
			parts := strings.Split(tag, ",")
			fi.Alias = parts[0]
			for _, p := range parts[1:] {
				if p == "readonly" {
					fi.ReadOnly = true
				}
			}
		}
		d.addFieldKey(sf.Name, fi)
		if fi.Alias != "" && fi.Alias != sf.Name {
			d.addFieldKey(fi.Alias, fi)
		}
	}

	// the pointer type's method set includes value-receiver methods too
	for i := 0; i < d.ptype.NumMethod(); i++ {
		m := d.ptype.Method(i)
		mi := MethodInfo{Name: m.Name, Variadic: m.Type.IsVariadic()}
		for j := 1; j < m.Type.NumIn(); j++ { // skip receiver
			mi.In = append(mi.In, m.Type.In(j))
		}
		for j := 0; j < m.Type.NumOut(); j++ {
			mi.Out = append(mi.Out, m.Type.Out(j))
		}
		d.Methods[m.Name] = mi
	}

	mu.Lock()
	defer mu.Unlock()
	if prev, ok := byName[cfg.name]; ok && prev.rtype != t {
		panic(fmt.Sprintf("gs: name %q already registered for %s", cfg.name, prev.rtype))
	}
	byName[cfg.name] = d
	byType[t] = d
	return d
}

func (d *Descriptor) addFieldKey(key string, fi FieldInfo) {
	if prev, dup := d.Fields[key]; dup {
		panic(fmt.Sprintf("gs: %s: field key %q maps to both %q and %q", d.Name, key, prev.Name, fi.Name))
	}
	d.Fields[key] = fi
}

func Lookup(name string) (*Descriptor, bool) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := byName[name]
	return d, ok
}

// Types returns the sorted names of all registered types.
func Types() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// New constructs a zero value of the named type and returns it as a *T,
// so the result is usable with Set and pointer-receiver methods.
func New(name string) (any, error) {
	d, ok := Lookup(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotRegistered, name)
	}
	return reflect.New(d.rtype).Interface(), nil
}
