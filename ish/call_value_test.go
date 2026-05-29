package ish

import (
	"testing"

	"ish/core"
)

// The call/value model: a bare single identifier is a zero-argument call; `&`
// is the value escape; a zero-arg call of a non-callable short-circuits to the
// value. Application heads are the function being called (not zero-arg-called),
// and literals are never wrapped.
func TestCallValueModel(t *testing.T) {
	cases := []struct {
		src  string
		want core.Datum
	}{
		// zero-arg function: bare name calls it.
		{"defn answer do\n  42\nend\nanswer", core.Int(42)},
		// non-callable value: zero-arg call short-circuits to the value.
		{"x = 5\nx", core.Int(5)},
		// literal arguments are not wrapped; head is the function.
		{"add 1 2", core.Int(3)},
		// a value used as an operand short-circuits to itself.
		{"x = 5\nx + 1", core.Int(6)},
		// head with a value arg: arg short-circuits.
		{"inc = fn x -> add x 1\ny = 41\ninc y", core.Int(42)},
		// & escapes to the value; passing a function as an arg needs &.
		{"defn answer do\n  42\nend\napply &answer (to-list [])", core.Int(42)},
	}
	for _, c := range cases {
		v, err := NewRuntime().EvalSource("m", c.src)
		if err != nil || v != c.want {
			t.Errorf("%q = %#v err=%v, want %#v", c.src, v, err, c.want)
		}
	}
}

// `&fn` yields the function value (callable), not a call of it.
func TestAmpersandYieldsFunctionValue(t *testing.T) {
	v, err := NewRuntime().EvalSource("m", "defn answer do\n  42\nend\nkind &answer")
	if err != nil || v != core.Atom("function") {
		t.Fatalf("kind &answer = %#v err=%v, want :function", v, err)
	}
}
