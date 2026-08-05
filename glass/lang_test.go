package glass

import (
	"strings"
	"testing"
)

func (b *box) Pair() (string, error) { return b.Name, nil }

func TestMultiAssign(t *testing.T) {
	in := New()
	if got := eval(t, in, "a, b := 1, 2\na, b = b, a\na - b"); got != 1 {
		t.Fatalf("swap got %v, want 1", got)
	}

	in.Define("two", func() (int, string) { return 4, "x" })
	if got := eval(t, in, "n, s := two()\ns"); got != "x" {
		t.Fatalf("host destructure got %v", got)
	}
	if got := eval(t, in, "n"); got != 4 {
		t.Fatalf("host destructure got n = %v", got)
	}

	if got := eval(t, in, "f := func() { return 1, 2 }\nx, y := f()\nx + y"); got != 3 {
		t.Fatalf("interpreted multi-return got %v", got)
	}
	if got := eval(t, in, "z, _ := f()\nz"); got != 1 {
		t.Fatalf("blank got %v", got)
	}

	if _, err := in.Eval("p, q := 1"); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("got %v, want mismatch error", err)
	}
	if _, err := in.Eval("w := two()"); err == nil || !strings.Contains(err.Error(), "single-value") {
		t.Fatalf("got %v, want single-value context error", err)
	}
}

func TestNilComparison(t *testing.T) {
	in := New()
	in.Define("xs", []int{1})
	if got := eval(t, in, "nil == nil"); got != true {
		t.Fatalf("got %v", got)
	}
	if got := eval(t, in, "xs != nil"); got != true {
		t.Fatalf("got %v", got)
	}
	if _, err := in.Eval("1 == nil"); err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("got %v, want nil comparison error", err)
	}

	src := `
b := new("box")
b.name = "kit"
v, e := b.Pair()
e == nil && v == "kit"`
	if got := eval(t, in, src); got != true {
		t.Fatalf("error-nil destructure got %v", got)
	}
}

func TestRange(t *testing.T) {
	in := New()
	in.Define("xs", []int{10, 20, 30})

	if got := eval(t, in, "s := 0\nfor i, v := range xs { s += i + v }\ns"); got != 63 {
		t.Fatalf("slice range got %v, want 63", got)
	}
	if got := eval(t, in, "n := 0\nfor range xs { n++ }\nn"); got != 3 {
		t.Fatalf("bare range got %v, want 3", got)
	}
	if got := eval(t, in, "t := 0\nfor i := range 5 { t += i }\nt"); got != 10 {
		t.Fatalf("int range got %v, want 10", got)
	}
	if got := eval(t, in, `c := 0
for _, r := range "héllo" {
	if r == 'l' {
		c++
	}
}
c`); got != 2 {
		t.Fatalf("string range got %v, want 2", got)
	}
	if got := eval(t, in, `m := map[string]int{"a": 1, "b": 2}
s := 0
for _, v := range m { s += v }
s`); got != 3 {
		t.Fatalf("map range got %v, want 3", got)
	}
	if got := eval(t, in, "s := 0\nfor _, v := range xs {\n\tif v == 20 { break }\n\ts += v\n}\ns"); got != 10 {
		t.Fatalf("range break got %v, want 10", got)
	}
	if _, err := in.Eval("for i, v := range 3 { }"); err == nil || !strings.Contains(err.Error(), "second variable") {
		t.Fatalf("got %v, want second variable error", err)
	}
}

func TestSwitch(t *testing.T) {
	in := New()
	src := `
label := func(n) {
	switch n {
	case 1, 2:
		return "small"
	case 3:
		return "three"
	default:
		return "big"
	}
}
label(2) + " " + label(3) + " " + label(9)`
	if got := eval(t, in, src); got != "small three big" {
		t.Fatalf("got %v", got)
	}

	if got := eval(t, in, "x := 7\nr := \"\"\nswitch {\ncase x > 5:\n\tr = \"hi\"\ncase x > 0:\n\tr = \"lo\"\n}\nr"); got != "hi" {
		t.Fatalf("tagless got %v", got)
	}

	if got := eval(t, in, "r := 0\nswitch y := 4; y {\ncase 4:\n\tr = 1\n}\nr"); got != 1 {
		t.Fatalf("init got %v", got)
	}

	// break inside a switch stops the switch, not the loop
	src = `
n := 0
for i := 0; i < 3; i++ {
	switch i {
	case 1:
		break
	}
	n++
}
n`
	if got := eval(t, in, src); got != 3 {
		t.Fatalf("switch break got %v, want 3", got)
	}
}

func TestIndexAssign(t *testing.T) {
	in := New()
	if got := eval(t, in, "xs := []int{1, 2}\nxs[0] = 9\nxs[0] + xs[1]"); got != 11 {
		t.Fatalf("slice got %v, want 11", got)
	}
	if got := eval(t, in, "ys := []int64{1}\nys[0] = 5\nys[0]"); got != int64(5) {
		t.Fatalf("coerced slice got %#v", got)
	}
	if got := eval(t, in, `m := map[string]int{"a": 1}
m["a"] = 2
m["b"] = 3
m["a"]++
m["a"] + m["b"]`); got != 6 {
		t.Fatalf("map got %v, want 6", got)
	}
	if _, err := in.Eval("xs[9] = 1"); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("got %v, want out of range", err)
	}
}

func TestSliceExpr(t *testing.T) {
	in := New()
	in.Define("xs", []int{1, 2, 3, 4})
	if got := eval(t, in, "ys := xs[1:3]\nys[0] + ys[1] + len(ys)"); got != 7 {
		t.Fatalf("got %v, want 7", got)
	}
	if got := eval(t, in, "len(xs[:2]) + len(xs[2:])"); got != 4 {
		t.Fatalf("defaults got %v, want 4", got)
	}
	if got := eval(t, in, `"gopher"[2:5]`); got != "phe" {
		t.Fatalf("string slice got %v", got)
	}
	if _, err := in.Eval("xs[3:9]"); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("got %v, want out of range", err)
	}
}

func TestCompositeLiterals(t *testing.T) {
	in := New()
	if got := eval(t, in, "s := 0\nfor _, v := range []int{1, 2, 3} { s += v }\ns"); got != 6 {
		t.Fatalf("slice lit got %v, want 6", got)
	}
	if got := eval(t, in, `mix := []any{1, "a", true}
len(mix)`); got != 3 {
		t.Fatalf("any lit got %v", got)
	}
	if got := eval(t, in, `ages := map[string]int{"kit": 3, "pod": 5}
ages["kit"]`); got != 3 {
		t.Fatalf("map lit got %v", got)
	}
	if got := eval(t, in, "grid := [][]int{[]int{1, 2}, []int{3}}\ngrid[0][1] + grid[1][0]"); got != 5 {
		t.Fatalf("nested got %v, want 5", got)
	}
	if _, err := in.Eval("zz := notatype{1}"); err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("got %v, want unknown type", err)
	}
}

func TestMakeAppendDelete(t *testing.T) {
	in := New()
	if got := eval(t, in, "s := make([]int, 2)\ns[1] = 8\nlen(s) + s[1]"); got != 10 {
		t.Fatalf("make slice got %v, want 10", got)
	}
	if got := eval(t, in, `m := make(map[string]int)
m["a"] = 1
len(m)`); got != 1 {
		t.Fatalf("make map got %v", got)
	}
	if got := eval(t, in, "xs := []int{1}\nxs = append(xs, 2, 3)\nlen(xs) + xs[2]"); got != 6 {
		t.Fatalf("append got %v, want 6", got)
	}
	if got := eval(t, in, "ys := append(xs, xs...)\nlen(ys)"); got != 6 {
		t.Fatalf("append spread got %v, want 6", got)
	}
	if got := eval(t, in, `n := map[string]int{"a": 1, "b": 2}
delete(n, "a")
len(n)`); got != 1 {
		t.Fatalf("delete got %v", got)
	}
}
