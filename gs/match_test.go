package gs

import "testing"

type mtAlpha struct{ X int }

func (a *mtAlpha) Get() int      { return a.X }
func (a *mtAlpha) GetTwice() int { return 2 * a.X }
func (a *mtAlpha) Put(n int)     { a.X = n }

type mtBeta struct{ Y int }

func (b *mtBeta) Get() int { return b.Y }

func TestGlob(t *testing.T) {
	cases := []struct {
		pat, s string
		want   bool
	}{
		{"*", "", true},
		{"*", "anything", true},
		{"", "", true},
		{"", "x", false},
		{"Get*", "GetName", true},
		{"Get*", "Get", true},
		{"Get*", "SetName", false},
		{"*Name", "GetName", true},
		{"*e*e*", "seventeen", true},
		{"a*b*c", "abc", true},
		{"a*b*c", "axxbxxc", true},
		{"a*b*c", "axxbxx", false},
		{"box", "box", true},
		{"box", "boxy", false},
		{"**", "anything", true},
	}
	for _, c := range cases {
		if got := Glob(c.pat, c.s); got != c.want {
			t.Errorf("Glob(%q, %q) = %v, want %v", c.pat, c.s, got, c.want)
		}
	}
}

func TestMatch(t *testing.T) {
	Register[mtAlpha](WithName("mt.alpha"))
	Register[mtBeta](WithName("mt.beta"))

	want := []MethodRef{
		{Type: "mt.alpha", Method: "Get"},
		{Type: "mt.alpha", Method: "GetTwice"},
		{Type: "mt.beta", Method: "Get"},
	}
	got := Match("mt.*", "Get*")
	if len(got) != len(want) {
		t.Fatalf("Match = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Match[%d] = %v, want %v", i, got[i], want[i])
		}
	}

	if refs := Match("mt.*", "Nope"); len(refs) != 0 {
		t.Fatalf("Match on absent method = %v, want empty", refs)
	}
	if refs := Match("zz*", "*"); len(refs) != 0 {
		t.Fatalf("Match on absent type = %v, want empty", refs)
	}
}
