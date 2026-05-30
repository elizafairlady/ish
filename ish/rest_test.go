package ish

import (
	"testing"

	"ish/core"
)

func evalRest(t *testing.T, src string) core.Datum {
	t.Helper()
	v, err := NewRuntime().EvalSource("rest", src)
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	d, _ := v.(core.Datum)
	return d
}

// A `.` rest marker binds the remainder of a sequence. Lists rebind the rest as
// a list, vectors as a vector, tuples as a tuple — the same kind as the matched
// container.
func TestRestPatterns(t *testing.T) {
	cases := map[string]core.Datum{
		// list rest (already supported, kept working)
		"to-list (match '(1 2 3) do (a . t) -> t end)": list(core.Int(2), core.Int(3)),
		"match '(1 2 3) do (a . t) -> a end":           core.Int(1),
		// vector rest rebinds as a vector
		"match [1 2 3 4] do [a . r] -> a end":           core.Int(1),
		"to-list (match [1 2 3 4] do [a . r] -> r end)": list(core.Int(2), core.Int(3), core.Int(4)),
		"match [1 2 3] do [a b . r] -> b end":           core.Int(2),
		// tuple rest rebinds as a tuple
		"match {1 2 3} do {a . r} -> r end": core.Tuple{core.Int(2), core.Int(3)},
		// empty rest is allowed
		"to-list (match [1] do [a . r] -> r end)": core.Nil{},
		// exact (non-rest) vector/tuple matching is unchanged
		"match [1 2] do [a b] -> a + b end": core.Int(3),
		"match {1 2} do {a b} -> a + b end": core.Int(3),
	}
	for src, want := range cases {
		if got := evalRest(t, src); !core.DatumEqual(got, want) {
			t.Errorf("%q = %#v, want %#v", src, got, want)
		}
	}
}

// `...` is meaningful only in syntax patterns; in a value pattern it used to
// bind a variable literally named "...", silently changing arity. It is now a
// clear error that points at the `.` rest pattern.
func TestEllipsisInValuePatternIsError(t *testing.T) {
	for _, src := range []string{
		"match [1 2 3] do [a ...] -> a end",
		"match '(1 2 3) do (a b ...) -> a end",
	} {
		if _, err := NewRuntime().EvalSource("rest", src); err == nil {
			t.Errorf("%q: expected error for `...` in a value pattern", src)
		}
	}
}
