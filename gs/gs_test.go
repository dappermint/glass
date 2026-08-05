package gs

import (
	"errors"
	"fmt"
	"testing"
)

type thing struct {
	ID     int    `gs:"id,readonly"`
	Name   string `gs:"name"`
	Skip   string `gs:"-"`
	Plain  bool
	hidden string
}

func (t *thing) SetName(n string) { t.Name = n }

func (t thing) Label() string { return fmt.Sprintf("thing(%s)", t.Name) }

func (t *thing) Sum(base int, ns ...int) int {
	for _, n := range ns {
		base += n
	}
	return base
}

func (t *thing) MaybeNil(p *thing) bool { return p == nil }

func init() {
	Register[thing](WithName("thing"))
}

func TestRegisterDescriptor(t *testing.T) {
	d, ok := Lookup("thing")
	if !ok {
		t.Fatal("thing not registered")
	}
	for _, key := range []string{"ID", "id", "Name", "name", "Plain"} {
		if _, ok := d.Fields[key]; !ok {
			t.Errorf("field key %q missing", key)
		}
	}
	for _, key := range []string{"Skip", "hidden"} {
		if _, ok := d.Fields[key]; ok {
			t.Errorf("field key %q should not be registered", key)
		}
	}
	for _, m := range []string{"SetName", "Label", "Sum", "MaybeNil"} {
		if _, ok := d.Methods[m]; !ok {
			t.Errorf("method %q missing", m)
		}
	}
}

func TestNewSetGet(t *testing.T) {
	obj, err := New("thing")
	if err != nil {
		t.Fatal(err)
	}
	if err := Set(obj, "name", "gopher"); err != nil {
		t.Fatal(err)
	}
	got, err := Get(obj, "Name")
	if err != nil {
		t.Fatal(err)
	}
	if got != "gopher" {
		t.Fatalf("got %v, want gopher", got)
	}
}

func TestReadOnly(t *testing.T) {
	obj, _ := New("thing")
	err := Set(obj, "id", 7)
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("got %v, want ErrReadOnly", err)
	}
}

func TestSetNeedsPointer(t *testing.T) {
	err := Set(thing{}, "name", "nope")
	if !errors.Is(err, ErrNeedPointer) {
		t.Fatalf("got %v, want ErrNeedPointer", err)
	}
}

func TestCallValueMethodOnValue(t *testing.T) {
	out, err := Call(thing{Name: "x"}, "Label")
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != "thing(x)" {
		t.Fatalf("got %v", out[0])
	}
}

func TestCallPointerMethodOnValue(t *testing.T) {
	_, err := Call(thing{}, "SetName", "x")
	if !errors.Is(err, ErrNeedPointer) {
		t.Fatalf("got %v, want ErrNeedPointer", err)
	}
}

func TestCallMutates(t *testing.T) {
	obj, _ := New("thing")
	if _, err := Call(obj, "SetName", "renamed"); err != nil {
		t.Fatal(err)
	}
	got, _ := Get(obj, "name")
	if got != "renamed" {
		t.Fatalf("got %v, want renamed", got)
	}
}

func TestCallVariadic(t *testing.T) {
	obj, _ := New("thing")
	out, err := Call(obj, "Sum", 1, 2, 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != 10 {
		t.Fatalf("got %v, want 10", out[0])
	}
	out, err = Call(obj, "Sum", 5)
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != 5 {
		t.Fatalf("got %v, want 5", out[0])
	}
}

func TestCallNilArg(t *testing.T) {
	obj, _ := New("thing")
	out, err := Call(obj, "MaybeNil", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != true {
		t.Fatalf("got %v, want true", out[0])
	}
}

func TestCallBadArgs(t *testing.T) {
	obj, _ := New("thing")
	_, err := Call(obj, "SetName", 42)
	if !errors.Is(err, ErrBadArg) {
		t.Fatalf("got %v, want ErrBadArg", err)
	}
	_, err = Call(obj, "SetName")
	if !errors.Is(err, ErrBadArg) {
		t.Fatalf("got %v, want ErrBadArg", err)
	}
}

func TestUnknowns(t *testing.T) {
	obj, _ := New("thing")
	if _, err := Get(obj, "Nope"); !errors.Is(err, ErrNoSuchField) {
		t.Fatalf("got %v, want ErrNoSuchField", err)
	}
	if _, err := Call(obj, "Nope"); !errors.Is(err, ErrNoSuchMethod) {
		t.Fatalf("got %v, want ErrNoSuchMethod", err)
	}
	if _, err := New("ghost"); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("got %v, want ErrNotRegistered", err)
	}
	if _, err := Get(struct{ X int }{}, "X"); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("got %v, want ErrNotRegistered", err)
	}
}

func TestPatchTable(t *testing.T) {
	if _, ok := Hook(&thing{}, "Label"); ok {
		t.Fatal("unexpected patch present")
	}
	err := Patch("thing", "Label", func(recv any, args []any) []any {
		return []any{"patched"}
	})
	if err != nil {
		t.Fatal(err)
	}
	f, ok := Hook(&thing{}, "Label")
	if !ok {
		t.Fatal("hook missing after Patch")
	}
	if out := f(&thing{}, nil); out[0] != "patched" {
		t.Fatalf("got %v", out)
	}
	if !Unpatch("thing", "Label") {
		t.Fatal("unpatch reported no patch")
	}
	if Unpatch("thing", "Label") {
		t.Fatal("double unpatch reported a patch")
	}
	if _, ok := Hook(&thing{}, "Label"); ok {
		t.Fatal("patch survived unpatch")
	}
	if err := Patch("thing", "Nope", nil); !errors.Is(err, ErrNoSuchMethod) {
		t.Fatalf("got %v, want ErrNoSuchMethod", err)
	}
	if err := Patch("ghost", "X", nil); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("got %v, want ErrNotRegistered", err)
	}
}

func TestRegisterValue(t *testing.T) {
	type other struct {
		V int
	}
	RegisterValue(&other{}, WithName("other"))
	obj, err := New("other")
	if err != nil {
		t.Fatal(err)
	}
	if err := Set(obj, "V", 3); err != nil {
		t.Fatal(err)
	}
}
