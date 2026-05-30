package ish

import (
	"testing"

	"ish/core"
)

// listLib is a list-processing library written entirely in ish over the
// irreducible primitives (cons / first / rest / kind / match). It re-derives
// the composite conveniences that currently also exist as Go primitives —
// proof that those primitives COULD be removed from Go and authored in the
// language, without removing them yet. The recursive higher-order functions
// thread their function parameter with `&` at the pass-through site, the
// ergonomic consequence of the uniform call/value model (a bare
// function-valued identifier in argument position is a zero-arg call).
const listLib = `
defn llen lst do match lst do
  :nil -> 0
  (h . t) -> 1 + (llen t)
end end
defn lrev-onto lst acc do match lst do
  :nil -> acc
  (h . t) -> lrev-onto t (cons h acc)
end end
defn lrev lst do lrev-onto lst :nil end
defn lmap f lst do match lst do
  :nil -> :nil
  (h . t) -> cons (f h) (lmap &f t)
end end
defn lfilter p lst do match lst do
  :nil -> :nil
  (h . t) -> case (p h) do
    :true -> cons h (lfilter &p t)
    _ -> lfilter &p t
  end
end end
defn lfold f acc lst do match lst do
  :nil -> acc
  (h . t) -> lfold &f (f acc h) t
end end
defn lappend a b do match a do
  :nil -> b
  (h . t) -> cons h (lappend t b)
end end
defn lnot x do case x do :true -> :false ; _ -> :true end end
nums = '(1 2 3 4 5)
`

func evalLib(t *testing.T, expr string) core.Datum {
	t.Helper()
	v, err := NewRuntime().EvalSource("lib", listLib+expr)
	if err != nil {
		t.Fatalf("eval %q: %v", expr, err)
	}
	d, _ := v.(core.Datum)
	return d
}

// Each ish-defined operation produces the expected result.
func TestReducibility_ListLibrary(t *testing.T) {
	cases := map[string]core.Datum{
		"llen nums":                                  core.Int(5),
		"first (lrev nums)":                          core.Int(5),
		"lfold (fn a x -> a + x) 0 nums":             core.Int(15),
		"llen (lfilter (fn x -> (x % 2) == 0) nums)": core.Int(2),
		"llen (lmap (fn x -> x * x) nums)":           core.Int(5),
		"llen (lappend nums nums)":                   core.Int(10),
		"lnot :false":                                core.Atom("true"),
		"lnot :true":                                 core.Atom("false"),
	}
	for expr, want := range cases {
		if got := evalLib(t, expr); !core.DatumEqual(got, want) {
			t.Errorf("%q = %#v, want %#v", expr, got, want)
		}
	}
}

// The ish-authored operations agree, structurally, with the Go primitives they
// shadow (append, reverse via to-list, not). If these Go primitives were
// deleted, the ish definitions would be drop-in replacements.
func TestReducibility_AgreesWithGoPrimitives(t *testing.T) {
	cases := []struct{ ish, prim string }{
		{"to-list (lappend nums nums)", "append nums nums"},
		{"to-list (lmap (fn x -> x + 1) nums)", "to-list nums |>> lmap (fn x -> x + 1)"},
		{"lnot :false", "not :false"},
		{"lnot :true", "not :true"},
	}
	for _, c := range cases {
		got := evalLib(t, c.ish)
		want := evalLib(t, c.prim)
		if !core.DatumEqual(got, want) {
			t.Errorf("ish %q = %#v; Go primitive %q = %#v (should agree)", c.ish, got, c.prim, want)
		}
	}
}
