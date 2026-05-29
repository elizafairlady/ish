package ish

import (
	"testing"

	"ish/core"
)

// The default operators come from the ish std/impl/kernel `defprotocol`, declared
// with the metadata-tag form `operator TOKEN %:precedence N %:assoc A -> target`.
// Operator-ness is decided by an active binding, not a character class, so `-`
// and `!=` (which a hardcoded predicate had wrongly excluded) work like any
// other.
func TestDefaultOperatorSet(t *testing.T) {
	cases := map[string]core.Datum{
		"5 - 2":      core.Int(3),
		"10 - 2 - 3": core.Int(5), // left associative
		"2 * 3 + 1":  core.Int(7), // precedence
		"1 + 2 * 3":  core.Int(7),
		"10 % 3":     core.Int(1),
		"1 == 1":     core.Atom("true"),
		"1 != 2":     core.Atom("true"),
		"2 < 3":      core.Atom("true"),
		"3 <= 3":     core.Atom("true"),
		"4 > 1":      core.Atom("true"),
		"4 >= 5":     core.Atom("false"),
	}
	for src, want := range cases {
		v, err := NewRuntime().EvalSource("op", src)
		if err != nil {
			t.Errorf("%q errored: %v", src, err)
			continue
		}
		if v != want {
			t.Errorf("%q = %#v, want %#v", src, v, want)
		}
	}
}

func TestNonAssociativeOperatorCannotChain(t *testing.T) {
	// `<` is declared %:assoc none, so chaining must be rejected, not silently
	// parsed.
	if _, err := NewRuntime().EvalSource("chain", "1 < 2 < 3"); err == nil {
		t.Fatal("expected non-associative chaining of < to fail")
	}
}

// A protocol can declare prefix and postfix operators purely through the
// %:fixity metadata tag; the enforestation honors them.
// A token can carry several fixities via the clause form `operator TOKEN do
// <clause>... end`, each clause one fixity, selected by position. The default
// `-` is both infix (sub) and prefix (neg), so `1 - -3` parses as sub(1,neg(3)).
func TestInfixAndPrefixMinus(t *testing.T) {
	cases := map[string]core.Int{
		"5 - 3":     2,
		"1 - -3":    4,  // sub(1, neg(3))
		"- 5":       -5, // prefix neg
		"- 5 + 2":   -3, // (neg 5) + 2, prefix binds tighter
		"2 - - - 3": -1, // sub(2, neg(neg(3)))
	}
	for src, want := range cases {
		v, err := NewRuntime().EvalSource("m", src)
		if err != nil || v != want {
			t.Errorf("%q = %#v err=%v, want %d", src, v, err, want)
		}
	}
}

// One token may declare several fixities in the clause form; the parser selects
// by position (prefix in operand position, postfix in operator position).
func TestOperatorClauseFormMultipleFixities(t *testing.T) {
	provider := "defn dec x do\n  sub x 1\nend\n" +
		"defn bang x do\n  mul x 10\nend\n" +
		"implements protocol do\n" +
		"  operator ~ do\n" +
		"    %:precedence 80 %:fixity prefix -> dec\n" +
		"    %:precedence 80 %:fixity postfix -> bang\n" +
		"  end\n" +
		"end\n"
	if v, err := NewRuntime().EvalSource("pre", provider+"~ 5"); err != nil || v != core.Int(4) {
		t.Fatalf("prefix ~5 = %#v err=%v, want 4", v, err)
	}
	if v, err := NewRuntime().EvalSource("post", provider+"5 ~"); err != nil || v != core.Int(50) {
		t.Fatalf("postfix 5~ = %#v err=%v, want 50", v, err)
	}
}

func TestPrefixAndPostfixOperators(t *testing.T) {
	prefix := "defn negate x do\n  sub 0 x\nend\n" +
		"defprotocol p do\n" +
		"  operator ~ %:precedence 80 %:fixity prefix -> negate\n" +
		"end\n" +
		"implements p\n" +
		"~ 5"
	if v, err := NewRuntime().EvalSource("prefix", prefix); err != nil || v != core.Int(-5) {
		t.Fatalf("prefix operator = %#v err=%v, want -5", v, err)
	}

	postfix := "defn dbl x do\n  mul 2 x\nend\n" +
		"defprotocol q do\n" +
		"  operator ! %:precedence 80 %:fixity postfix -> dbl\n" +
		"end\n" +
		"implements q\n" +
		"5 !"
	if v, err := NewRuntime().EvalSource("postfix", postfix); err != nil || v != core.Int(10) {
		t.Fatalf("postfix operator = %#v err=%v, want 10", v, err)
	}
}

// A defmacro/macro body may be a single `-> expr` or a `do ... end` block, never
// mixed. This exercises the do-body form (the arrow form is exercised throughout
// the macro tests).
func TestOperatorDuplicateMetadataRejected(t *testing.T) {
	src := "defprotocol p do\n  operator ++ %:precedence 10 %:precedence 20 -> add\nend"
	if _, err := NewRuntime().EvalSource("dup", src); err == nil {
		t.Fatal("duplicate %:precedence tag should be rejected")
	}
}

func TestOperatorMissingPrecedenceRejected(t *testing.T) {
	src := "defprotocol p do\n  operator ++ %:assoc left -> add\nend"
	if _, err := NewRuntime().EvalSource("noprec", src); err == nil {
		t.Fatal("operator without %:precedence should be rejected")
	}
}

// A guard that errors is a guard failure (clause does not match), not a call
// abort — fn/match (via Closure.Call) behaves like receive here, matching
// Erlang/Elixir.
// Guards work in every clause form, including the `fn do … end` multi-clause
// sugar — it decodes clauses through the one canonical splitter, like match.
func TestFnDoBlockGuards(t *testing.T) {
	src := "classify = fn do\n" +
		"  x when (lt? x 0) -> :neg\n" +
		"  x when (eq? x 0) -> :zero\n" +
		"  x -> :pos\n" +
		"end\n" +
		"classify 7"
	if v, err := NewRuntime().EvalSource("g", src); err != nil || v != core.Atom("pos") {
		t.Fatalf("fn do-block guard = %#v err=%v, want :pos", v, err)
	}
}

func TestGuardErrorSkipsClause(t *testing.T) {
	src := "match 5 do\n" +
		"  x when (mod x 0) -> :first\n" + // guard errors (mod by zero) -> skip
		"  x -> :second\n" +
		"end"
	v, err := NewRuntime().EvalSource("guard", src)
	if err != nil || v != core.Atom("second") {
		t.Fatalf("guard-error clause skip = %#v err=%v, want :second", v, err)
	}
}

// A zero-argument named function is valid: `defn f do … end` with an empty
// parameter list. A bare reference calls it; `&f` is the value, and `apply`
// invokes a function value.
func TestZeroArgDefn(t *testing.T) {
	v, err := NewRuntime().EvalSource("z", "defn answer do\n  42\nend\nanswer")
	if err != nil || v != core.Int(42) {
		t.Fatalf("zero-arg defn called = %#v err=%v, want 42", v, err)
	}
	v, err = NewRuntime().EvalSource("z2", "defn answer do\n  42\nend\napply &answer (to-list [])")
	if err != nil || v != core.Int(42) {
		t.Fatalf("apply &answer = %#v err=%v, want 42", v, err)
	}
}

func TestDefmacroDoBodyForm(t *testing.T) {
	src := "defmacro twice stx do\n" +
		"  doubled = syntax-case stx [] do\n" +
		"    (twice n) -> %`(add %,n %,n)\n" +
		"  end\n" +
		"  doubled\n" +
		"end\n" +
		"twice 21"
	v, err := NewRuntime().EvalSource("m", src)
	if err != nil || v != core.Int(42) {
		t.Fatalf("defmacro do-body = %#v err=%v, want 42", v, err)
	}
}
