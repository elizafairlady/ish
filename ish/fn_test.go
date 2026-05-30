package ish

import (
	"testing"

	"ish/core"
)

func evalFn(t *testing.T, src string) core.Datum {
	t.Helper()
	v, err := NewRuntime().EvalSource("fn", src)
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	d, _ := v.(core.Datum)
	return d
}

// A fn with no parameters is a thunk. Both body syntaxes must accept zero
// parameters: the arrow form `fn -> body` and the block form `fn do body end`.
// (Previously the arrow form rejected an empty parameter list and the block
// form tried to read the body as clauses.)
func TestFnZeroArgThunk(t *testing.T) {
	cases := map[string]core.Datum{
		"f = fn -> 42\nf":                      core.Int(42),
		"f = fn -> 6 * 7\nf":                   core.Int(42),
		"f = fn do 42 end\nf":                  core.Int(42),
		"f = fn do\n  x = 10\n  x + 5\nend\nf": core.Int(15),
	}
	for src, want := range cases {
		if got := evalFn(t, src); !core.DatumEqual(got, want) {
			t.Errorf("%q = %#v, want %#v", src, got, want)
		}
	}
}

// The other fn shapes must be unaffected by the zero-arg additions.
func TestFnParameterizedFormsStillWork(t *testing.T) {
	cases := map[string]core.Datum{
		"f = fn x -> x + 1\nf 10":                          core.Int(11),
		"f = fn a b -> a + b\nf 3 4":                       core.Int(7),
		"f = fn x do x * 2 end\nf 21":                      core.Int(42),
		"f = fn do x -> x + 1 end\nf 10":                   core.Int(11),
		"f = fn do\n  0 -> :zero\n  n -> :other\nend\nf 0": core.Atom("zero"),
		"f = fn do\n  0 -> :zero\n  n -> :other\nend\nf 7": core.Atom("other"),
	}
	for src, want := range cases {
		if got := evalFn(t, src); !core.DatumEqual(got, want) {
			t.Errorf("%q = %#v, want %#v", src, got, want)
		}
	}
}

// The thunk-vs-clause discriminator must look past arrows that belong to nested
// lambdas: a block whose statements carry such arrows is a thunk, while a block
// of `pattern -> body` clauses is a multi-clause lambda. These cases would be
// misclassified by a naive "does any form contain ->" test.
func TestFnThunkVsClauseDiscrimination(t *testing.T) {
	cases := map[string]core.Datum{
		// thunk: the -> belongs to the nested `fn x -> x + 1`, bound to g
		"f = fn do\n  g = fn x -> x + 1\n  g 9\nend\nf": core.Int(10),
		// thunk: the body simply returns a lambda, which is then applied
		"mk = fn do fn x -> x * 3 end\nh = mk\nh 4": core.Int(12),
		// clause: `x -> x + 1` is a genuine one-clause lambda over its argument
		"f = fn do x -> x + 1 end\nf 41": core.Int(42),
	}
	for src, want := range cases {
		if got := evalFn(t, src); !core.DatumEqual(got, want) {
			t.Errorf("%q = %#v, want %#v", src, got, want)
		}
	}
}

// Zero-arg thunks are the natural spawn argument; both forms must work as the
// process entry thunk (the parent pid is captured in the closure).
func TestFnThunkAsSpawnEntry(t *testing.T) {
	cases := map[string]core.Datum{
		"me = self\nspawn (fn -> send me :hi)\nreceive do m -> m end":     core.Atom("hi"),
		"me = self\nspawn (fn do send me :yo end)\nreceive do m -> m end": core.Atom("yo"),
	}
	for src, want := range cases {
		if got := evalFn(t, src); !core.DatumEqual(got, want) {
			t.Errorf("%q = %#v, want %#v", src, got, want)
		}
	}
}
