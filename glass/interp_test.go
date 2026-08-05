package glass

import (
	"errors"
	"strings"
	"testing"

	"github.com/dappermint/glass/gs"
)

type box struct {
	ID   int    `gs:"id,readonly"`
	Name string `gs:"name"`
	N    int64
}

func (b *box) SetN(n int64) { b.N = n }

func (b *box) Hello(greeting string) string { return greeting + " " + b.Name }

func (b *box) Sum(ns ...int) int {
	t := 0
	for _, n := range ns {
		t += n
	}
	return t
}

type crate struct {
	Weight int `gs:"weight"`
}

func (c *crate) Hello(greeting string) string { return greeting + " crate" }

func (c *crate) Seal() string { return "sealed" }

func init() {
	gs.Register[box](gs.WithName("box"))
	gs.Register[crate](gs.WithName("crate"))
}

// eval is a test wrapper around Interp.Eval, the interpreter under test.
// It only touches gs-registered types and test-bound funcs, no host exec.
func eval(t *testing.T, in *Interp, src string) any {
	t.Helper()
	v, err := in.Eval(src)
	if err != nil {
		t.Fatalf("Eval(%q): %v", src, err)
	}
	return v
}

func TestArithmetic(t *testing.T) {
	in := New()
	cases := map[string]any{
		"1 + 2*3":     7,
		"(1 + 2) * 3": 9,
		"7 % 3":       1,
		"1 / 2.0":     0.5,
		"-4 + 1":      -3,
		`"a" + "b"`:   "ab",
		"2 > 1":       true,
		`"x" == "x"`:  true,
		"!true":       false,
		"1.5 == 1.5":  true,
		"'a'":         'a',
		"3 <= 2":      false,
	}
	for src, want := range cases {
		if got := eval(t, in, src); got != want {
			t.Errorf("%s = %#v, want %#v", src, got, want)
		}
	}
}

func TestVariables(t *testing.T) {
	in := New()
	got := eval(t, in, "x := 2\nx = x + 3\nx *= 2\nx++\nx")
	if got != 11 {
		t.Fatalf("got %v, want 11", got)
	}
}

func TestStatePersistsAcrossEvals(t *testing.T) {
	in := New()
	eval(t, in, `n := 40`)
	if got := eval(t, in, "n + 2"); got != 42 {
		t.Fatalf("got %v, want 42", got)
	}
}

func TestIfForBreakContinue(t *testing.T) {
	in := New()
	src := `
s := 0
for i := 0; i < 10; i++ {
	if i == 3 {
		continue
	}
	if i > 7 {
		break
	}
	s += i
}
s`
	if got := eval(t, in, src); got != 25 {
		t.Fatalf("got %v, want 25", got)
	}
}

func TestShortCircuit(t *testing.T) {
	in := New()
	called := false
	in.Define("boom", func() bool { called = true; return true })
	if got := eval(t, in, "false && boom()"); got != false {
		t.Fatalf("got %v", got)
	}
	if got := eval(t, in, "true || boom()"); got != true {
		t.Fatalf("got %v", got)
	}
	if called {
		t.Fatal("short circuit failed, boom was called")
	}
}

func TestRegistryRoundTrip(t *testing.T) {
	in := New()
	src := `
b := new("box")
b.name = "kit"
b.Hello("hey")`
	if got := eval(t, in, src); got != "hey kit" {
		t.Fatalf("got %v, want hey kit", got)
	}
}

func TestNumericConversion(t *testing.T) {
	in := New()
	// literal is int, SetN wants int64, field N is int64
	got := eval(t, in, "b := new(\"box\")\nb.SetN(41)\nb.N")
	if got != int64(41) {
		t.Fatalf("got %#v, want int64(41)", got)
	}
	// selector store converts too
	got = eval(t, in, "b.N = 5\nb.N++\nb.N")
	if got != int64(6) {
		t.Fatalf("got %#v, want int64(6)", got)
	}
}

func TestVariadicMethod(t *testing.T) {
	in := New()
	if got := eval(t, in, "b := new(\"box\")\nb.Sum(1, 2, 3)"); got != 6 {
		t.Fatalf("got %v, want 6", got)
	}
	if got := eval(t, in, "b.Sum()"); got != 0 {
		t.Fatalf("got %v, want 0", got)
	}
}

func TestIntrospectionBuiltins(t *testing.T) {
	in := New()
	types := eval(t, in, "types()").([]string)
	found := false
	for _, name := range types {
		if name == "box" {
			found = true
		}
	}
	if !found {
		t.Fatalf("types() = %v, missing box", types)
	}
	fields := eval(t, in, "b := new(\"box\")\nfields(b)").([]string)
	if strings.Join(fields, ",") != "ID,N,Name" {
		t.Fatalf("fields(b) = %v", fields)
	}
	methods := eval(t, in, "methods(b)").([]string)
	if strings.Join(methods, ",") != "Hello,Pair,SetN,Sum" {
		t.Fatalf("methods(b) = %v", methods)
	}
}

func TestHostBindings(t *testing.T) {
	in := New()
	in.Define("xs", []int{10, 20, 30})
	in.Define("double", func(n int) int { return n * 2 })
	in.Define("fail", func() (int, error) { return 0, errors.New("nope") })

	if got := eval(t, in, "xs[1]"); got != 20 {
		t.Fatalf("got %v, want 20", got)
	}
	if got := eval(t, in, "len(xs)"); got != 3 {
		t.Fatalf("got %v, want 3", got)
	}
	if got := eval(t, in, "double(xs[2])"); got != 60 {
		t.Fatalf("got %v, want 60", got)
	}
	if _, err := in.Eval("fail()"); err == nil || err.Error() != "nope" {
		t.Fatalf("got %v, want nope", err)
	}
}

func TestFuncLiterals(t *testing.T) {
	in := New()
	if got := eval(t, in, "f := func(x, y) { return x + y }\nf(1, 2)"); got != 3 {
		t.Fatalf("got %v, want 3", got)
	}
	if got := eval(t, in, "n := 0\ninc := func() { n++ }\ninc()\ninc()\nn"); got != 2 {
		t.Fatalf("closure got %v, want 2", got)
	}
	src := `
fact := func(n) {
	if n <= 1 {
		return 1
	}
	return n * fact(n-1)
}
fact(5)`
	if got := eval(t, in, src); got != 120 {
		t.Fatalf("recursion got %v, want 120", got)
	}
}

func TestShardLifecycle(t *testing.T) {
	in := New()
	eval(t, in, `shard("box", "Double", func(self) { return self.N * 2 })`)
	if got := eval(t, in, "b := new(\"box\")\nb.SetN(21)\nb.Double()"); got != 42 {
		t.Fatalf("shard call got %v, want 42", got)
	}

	// swap: same name, new body, takes effect immediately
	eval(t, in, `shard("box", "Double", func(self) { return self.N * 3 })`)
	if got := eval(t, in, "b.Double()"); got != 63 {
		t.Fatalf("swapped shard got %v, want 63", got)
	}

	// shadow a compiled method, then restore it
	eval(t, in, "b.name = \"kit\"")
	eval(t, in, `shard("box", "Hello", func(self, g) { return "shadowed " + g })`)
	if got := eval(t, in, `b.Hello("hey")`); got != "shadowed hey" {
		t.Fatalf("shadowing got %v", got)
	}
	if got := eval(t, in, `mend("box", "Hello")`); got != true {
		t.Fatalf("mend got %v, want true", got)
	}
	if got := eval(t, in, `b.Hello("hey")`); got != "hey kit" {
		t.Fatalf("restored method got %v, want hey kit", got)
	}

	shardNames := eval(t, in, "shards(b)").([]string)
	if strings.Join(shardNames, ",") != "Double" {
		t.Fatalf("shards(b) = %v", shardNames)
	}
	methodNames := eval(t, in, "methods(b)").([]string)
	if strings.Join(methodNames, ",") != "Double,Hello,Pair,SetN,Sum" {
		t.Fatalf("methods(b) = %v", methodNames)
	}
}

func TestShardErrors(t *testing.T) {
	in := New()
	if _, err := in.Eval(`shard("ghost", "X", func(self) { return 1 })`); !errors.Is(err, gs.ErrNotRegistered) {
		t.Fatalf("got %v, want ErrNotRegistered", err)
	}
	if _, err := in.Eval(`shard("box", "X", func() { return 1 })`); err == nil || !strings.Contains(err.Error(), "receiver param") {
		t.Fatalf("got %v, want receiver param error", err)
	}
	if got := eval(t, in, `mend("box", "Nope")`); got != false {
		t.Fatalf("got %v, want false", got)
	}
	if _, err := in.Eval("return 1"); err == nil || !strings.Contains(err.Error(), "return outside function") {
		t.Fatalf("got %v, want return outside function", err)
	}
}

func TestAdvice(t *testing.T) {
	in := New()
	eval(t, in, `b := new("box")
b.name = "kit"
advise("box", "Hello", "around", func(self, next, g) { return "[" + next(g) + "]" })`)
	if got := eval(t, in, `b.Hello("hey")`); got != "[hey kit]" {
		t.Fatalf("around got %v", got)
	}

	eval(t, in, `n := 0
advise("box", "Hello", "before", func(self, g) { n = n + 1 })`)
	if got := eval(t, in, `b.Hello("x")`); got != "[x kit]" {
		t.Fatalf("got %v", got)
	}
	if got := eval(t, in, "n"); got != 1 {
		t.Fatalf("before advice ran %v times, want 1", got)
	}

	// advice added later wraps outside earlier advice
	eval(t, in, `advise("box", "Hello", "around", func(self, next, g) { return "<" + next(g) + ">" })`)
	if got := eval(t, in, `b.Hello("z")`); got != "<[z kit]>" {
		t.Fatalf("ordering got %v", got)
	}

	if got := eval(t, in, `unadvise("box", "Hello")`); got != 3 {
		t.Fatalf("unadvise got %v, want 3", got)
	}
	if got := eval(t, in, `b.Hello("q")`); got != "q kit" {
		t.Fatalf("restored got %v", got)
	}

	// after advice observes but does not change the result
	eval(t, in, `advise("box", "Hello", "after", func(self, g) { n = 42 })`)
	if got := eval(t, in, `b.Hello("w")`); got != "w kit" {
		t.Fatalf("after got %v", got)
	}
	if got := eval(t, in, "n"); got != 42 {
		t.Fatalf("after advice did not run, n = %v", got)
	}
}

func TestAdviceOnShard(t *testing.T) {
	in := New()
	src := `
shard("box", "Twice", func(self) { return self.N * 2 })
advise("box", "Twice", "around", func(self, next) { return next() + 1 })
b := new("box")
b.SetN(10)
b.Twice()`
	if got := eval(t, in, src); got != 21 {
		t.Fatalf("got %v, want 21", got)
	}
}

func TestPatchBuiltin(t *testing.T) {
	in := New()
	eval(t, in, `patch("box", "Hello", func(self, g) { return "patched " + g })`)
	f, ok := gs.Hook(&box{}, "Hello")
	if !ok {
		t.Fatal("patch not installed in gs table")
	}
	out := f(&box{Name: "k"}, []any{"yo"})
	if out[0] != "patched yo" {
		t.Fatalf("got %v", out)
	}
	if got := eval(t, in, `unpatch("box", "Hello")`); got != true {
		t.Fatalf("unpatch got %v", got)
	}
	if _, ok := gs.Hook(&box{}, "Hello"); ok {
		t.Fatal("patch survived unpatch")
	}
}

func TestMatchBuiltin(t *testing.T) {
	in := New()
	got := eval(t, in, `match("*", "Hello")`).([]string)
	if strings.Join(got, ",") != "box.Hello,crate.Hello" {
		t.Fatalf("match = %v", got)
	}

	// shards participate in pointcut matching
	eval(t, in, `shard("box", "Zap", func(self) { return 1 })`)
	got = eval(t, in, `match("box", "Z*")`).([]string)
	if strings.Join(got, ",") != "box.Zap" {
		t.Fatalf("shard match = %v", got)
	}

	// no match is an empty result, not an error
	if got := eval(t, in, `match("ghost", "*")`).([]string); len(got) != 0 {
		t.Fatalf("ghost match = %v, want empty", got)
	}
}

func TestAdviseMatch(t *testing.T) {
	in := New()
	src := `
b := new("box")
b.name = "kit"
c := new("crate")
adviseMatch("*", "Hello", "around", func(self, next, g) { return "[" + next(g) + "]" })`
	if got := eval(t, in, src); got != 2 {
		t.Fatalf("adviseMatch count = %v, want 2", got)
	}
	if got := eval(t, in, `b.Hello("hey")`); got != "[hey kit]" {
		t.Fatalf("box got %v", got)
	}
	if got := eval(t, in, `c.Hello("hey")`); got != "[hey crate]" {
		t.Fatalf("crate got %v", got)
	}

	// methods outside the pointcut are untouched
	if got := eval(t, in, "b.Sum(1, 2)"); got != 3 {
		t.Fatalf("Sum got %v", got)
	}

	// unadviseMatch peels advice off every matched method
	if got := eval(t, in, `unadviseMatch("*", "*")`); got != 2 {
		t.Fatalf("unadviseMatch = %v, want 2", got)
	}
	if got := eval(t, in, `b.Hello("q")`); got != "q kit" {
		t.Fatalf("restored got %v", got)
	}
}

func TestAdviseMatchMixedArity(t *testing.T) {
	in := New()
	src := `
n := 0
adviseMatch("box", "*", "before", func(self any, rest ...any) { n = n + len(rest) })
b := new("box")
b.name = "kit"
b.Hello("hey")
b.Sum(1, 2, 3)
n`
	// Hello contributes 1 arg, Sum contributes 3
	if got := eval(t, in, src); got != 4 {
		t.Fatalf("got %v, want 4", got)
	}
}

func TestAdviseMatchShard(t *testing.T) {
	in := New()
	src := `
shard("box", "Zing", func(self) { return "zing" })
adviseMatch("box", "Zing", "around", func(self, next) { return next() + "!" })
b := new("box")
b.Zing()`
	if got := eval(t, in, src); got != "zing!" {
		t.Fatalf("got %v", got)
	}
}

func TestVariadicFuncLit(t *testing.T) {
	in := New()
	src := `
f := func(first any, rest ...any) {
	return len(rest)
}
f(1, 2, 3)`
	if got := eval(t, in, src); got != 2 {
		t.Fatalf("got %v, want 2", got)
	}
	if got := eval(t, in, "f(1)"); got != 0 {
		t.Fatalf("got %v, want 0", got)
	}
	if _, err := in.Eval("f()"); err == nil || !strings.Contains(err.Error(), "at least 1") {
		t.Fatalf("got %v, want arity error", err)
	}
	if _, err := in.Eval("g := func(xs ...int) { return len(xs) }\ng(1, 2)"); err != nil {
		t.Fatalf("named variadic: %v", err)
	}
}

func TestSpread(t *testing.T) {
	in := New()
	in.Define("xs", []int{1, 2, 3})
	in.Define("sum", func(ns ...int) int {
		t := 0
		for _, n := range ns {
			t += n
		}
		return t
	})

	if got := eval(t, in, "sum(xs...)"); got != 6 {
		t.Fatalf("host spread got %v", got)
	}
	if got := eval(t, in, "f := func(a, b, c) { return a + b + c }\nf(xs...)"); got != 6 {
		t.Fatalf("funcVal spread got %v", got)
	}
	if got := eval(t, in, "b := new(\"box\")\nb.Sum(xs...)"); got != 6 {
		t.Fatalf("method spread got %v", got)
	}
	if _, err := in.Eval("n := 1\nsum(n...)"); err == nil || !strings.Contains(err.Error(), "spread") {
		t.Fatalf("got %v, want spread error", err)
	}
}

// the universal around advice: variadic params + spread forward any arity
func TestAdviseMatchForwarding(t *testing.T) {
	in := New()
	src := `
calls := 0
adviseMatch("box", "*", "around", func(self any, next any, rest ...any) {
	calls++
	return next(rest...)
})
b := new("box")
b.name = "kit"
b.Hello("hey")`
	if got := eval(t, in, src); got != "hey kit" {
		t.Fatalf("forwarded Hello got %v", got)
	}
	if got := eval(t, in, "b.Sum(1, 2)"); got != 3 {
		t.Fatalf("forwarded Sum got %v", got)
	}
	if got := eval(t, in, "calls"); got != 2 {
		t.Fatalf("advice ran %v times, want 2", got)
	}
}

func TestErrors(t *testing.T) {
	in := New()
	if _, err := in.Eval("zzz"); err == nil || !strings.Contains(err.Error(), "undefined") {
		t.Fatalf("got %v", err)
	}
	if _, err := in.Eval("1 / 0"); err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("got %v", err)
	}
	if _, err := in.Eval("b := new(\"box\")\nb.id = 9"); !errors.Is(err, gs.ErrReadOnly) {
		t.Fatalf("got %v, want ErrReadOnly", err)
	}
	if _, err := in.Eval("b := new(\"ghost\")"); !errors.Is(err, gs.ErrNotRegistered) {
		t.Fatalf("got %v, want ErrNotRegistered", err)
	}
	if _, err := in.Eval("1 +"); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("got %v, want parse error", err)
	}
}
