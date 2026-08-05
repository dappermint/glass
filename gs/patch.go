package gs

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
)

// PatchFunc replaces a woven method's implementation at runtime. It receives
// the receiver and the call's arguments (variadics flattened) and must return
// values matching the method's result count; each is comma-ok asserted to the
// result type, so a mismatch yields that type's zero value.
type PatchFunc func(recv any, args []any) []any

var (
	patchMu    sync.RWMutex
	patchCount atomic.Int64
	patches    = map[reflect.Type]map[string]PatchFunc{}
)

// Patch installs f as the implementation of a woven method. name is the
// registry name, method the exported (woven) method name.
func Patch(name, method string, f PatchFunc) error {
	d, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotRegistered, name)
	}
	if _, ok := d.Methods[method]; !ok {
		return fmt.Errorf("%w: %s.%s", ErrNoSuchMethod, name, method)
	}
	patchMu.Lock()
	defer patchMu.Unlock()
	m := patches[d.rtype]
	if m == nil {
		m = map[string]PatchFunc{}
		patches[d.rtype] = m
	}
	if _, exists := m[method]; !exists {
		patchCount.Add(1)
	}
	m[method] = f
	return nil
}

// Unpatch removes a patch, restoring the compiled implementation. Reports
// whether a patch existed.
func Unpatch(name, method string) bool {
	d, ok := Lookup(name)
	if !ok {
		return false
	}
	patchMu.Lock()
	defer patchMu.Unlock()
	if _, exists := patches[d.rtype][method]; !exists {
		return false
	}
	delete(patches[d.rtype], method)
	patchCount.Add(-1)
	return true
}

// Hook is called by gsgen-generated trampolines on every method call. The
// unpatched fast path is a single atomic load.
func Hook(recv any, method string) (PatchFunc, bool) {
	if patchCount.Load() == 0 {
		return nil, false
	}
	t := reflect.TypeOf(recv)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	patchMu.RLock()
	f, ok := patches[t][method]
	patchMu.RUnlock()
	return f, ok
}
