package gs

import "sort"

// MethodRef identifies one registered method, the unit a pointcut selects.
type MethodRef struct {
	Type   string
	Method string
}

// Match returns every registered (type, method) pair whose registry name and
// method name match the glob patterns. It is the quantification half of
// aspect-oriented advice: Match selects join points, advice wraps them.
// Results are sorted by type then method.
func Match(typePat, methodPat string) []MethodRef {
	mu.RLock()
	defer mu.RUnlock()
	var out []MethodRef
	for name, d := range byName {
		if !Glob(typePat, name) {
			continue
		}
		for m := range d.Methods {
			if Glob(methodPat, m) {
				out = append(out, MethodRef{Type: name, Method: m})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// Glob reports whether s matches pat, where `*` matches any (possibly empty)
// run of characters and everything else matches literally.
func Glob(pat, s string) bool {
	px, sx := 0, 0
	star, mark := -1, 0
	for sx < len(s) {
		switch {
		case px < len(pat) && pat[px] == '*':
			star, mark = px, sx
			px++
		case px < len(pat) && pat[px] == s[sx]:
			px++
			sx++
		case star >= 0:
			mark++
			px, sx = star+1, mark
		default:
			return false
		}
	}
	for px < len(pat) && pat[px] == '*' {
		px++
	}
	return px == len(pat)
}
