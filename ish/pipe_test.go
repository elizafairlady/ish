package ish

import (
	"testing"

	"ish/core"
)

// evalPipe evaluates source with the default kernel (which now provides the
// |> thread-first and |>> thread-last operators) and returns the datum.
func evalPipe(t *testing.T, src string) core.Datum {
	t.Helper()
	v, err := NewRuntime().EvalSource("pipe", src)
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	d, ok := v.(core.Datum)
	if !ok {
		t.Fatalf("eval %q: non-datum result %#v", src, v)
	}
	return d
}

// The |> and |>> operators are kernel operators whose targets are MACROS
// (thread-first / thread-last). They splice the left operand into the right
// call form as syntax — proof that an operator target need not be a runtime
// function. These tests pin the semantics so the macro-as-operator-target
// machinery cannot silently regress.
const pipePrelude = "defn inc x do x + 1 end\n" +
	"defn dbl x do x * 2 end\n" +
	"defn sub3 a b c do a - b - c end\n"

// thread-first |> splices the left operand as the FIRST argument of the right
// call: `x |> f a b` -> `(f x a b)`. A bare callee reduces to `(f x)`.
func TestPipeThreadFirst(t *testing.T) {
	cases := map[string]core.Datum{
		"5 |> inc":               core.Int(6),  // (inc 5)
		"5 |> sub3 1 1":          core.Int(3),  // (sub3 5 1 1) = 5-1-1
		"5 |> inc |> dbl |> inc": core.Int(13), // inc(dbl(inc 5)) = inc(dbl 6)=inc 12
	}
	for expr, want := range cases {
		if got := evalPipe(t, pipePrelude+expr); got != want {
			t.Errorf("%q = %#v, want %#v", expr, got, want)
		}
	}
}

// thread-last |>> splices the left operand as the LAST argument of the right
// call: `x |>> f a b` -> `(f a b x)`.
func TestPipeThreadLast(t *testing.T) {
	cases := map[string]core.Datum{
		"5 |>> inc":      core.Int(6), // (inc 5)
		"5 |>> sub3 9 1": core.Int(3), // (sub3 9 1 5) = 9-1-5
	}
	for expr, want := range cases {
		if got := evalPipe(t, pipePrelude+expr); got != want {
			t.Errorf("%q = %#v, want %#v", expr, got, want)
		}
	}
}

// The pipe operators are the lowest-precedence operators, so an arithmetic
// left operand groups before piping, and the two pipe directions may be mixed
// in one left-associative chain.
func TestPipePrecedenceAndMixing(t *testing.T) {
	cases := map[string]core.Datum{
		"2 + 3 |> dbl":             core.Int(10), // (dbl (2+3))
		"(2 + 3) |> dbl":           core.Int(10),
		"10 |> dbl |>> sub3 100 9": core.Int(71), // sub3(100, 9, dbl 10) = 100-9-20
	}
	for expr, want := range cases {
		if got := evalPipe(t, pipePrelude+expr); got != want {
			t.Errorf("%q = %#v, want %#v", expr, got, want)
		}
	}
}

// A realistic data pipeline: collection-last list functions written in ish are
// the natural fit for thread-last, exactly as Clojure's ->> threads a sequence
// through map/filter/reduce. (See reducibility_test.go for the library.)
func TestPipeRealisticPipeline(t *testing.T) {
	src := listLib + "to-list (nums |>> lfilter (fn x -> (x % 2) == 1) |>> lmap (fn x -> x * x))"
	got := evalPipe(t, src)
	want := list(core.Int(1), core.Int(9), core.Int(25)) // squares of the odd 1..5
	if !core.DatumEqual(got, want) {
		t.Fatalf("odd-squares pipeline = %#v, want %#v", got, want)
	}
}

// list builds a proper cons-list datum for expectations.
func list(items ...core.Datum) core.Datum {
	var out core.Datum = core.Nil{}
	for i := len(items) - 1; i >= 0; i-- {
		out = core.Pair{Head: items[i], Tail: out}
	}
	return out
}
