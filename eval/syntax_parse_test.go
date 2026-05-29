package eval

import (
	"reflect"
	"strings"
	"testing"

	"ish/core"
	"ish/expand"
	"ish/reader"
)

func TestSyntaxParseBasicVariables(t *testing.T) {
	v := readExpandEvalWithMacros(t, "defmacro m stx -> syntax-parse stx do\n  (_ a b) -> %`(quote {%,a %,b})\nend\nm :x :y")
	want := core.Tuple{core.Atom("x"), core.Atom("y")}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("syntax-parse vars = %#v, want %#v", v, want)
	}
}

func TestSyntaxParseEllipsis(t *testing.T) {
	v := readExpandEvalWithMacros(t, "defmacro m stx -> syntax-parse stx do\n  (_ a ...) -> %`(quote (%,a ...))\nend\nm :a :b :c")
	want := core.Pair{Head: core.Atom("a"), Tail: core.Pair{Head: core.Atom("b"), Tail: core.Pair{Head: core.Atom("c"), Tail: core.Nil{}}}}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("syntax-parse ellipsis = %#v, want %#v", v, want)
	}
}

func TestSyntaxParseLiteralCombinator(t *testing.T) {
	v := readExpandEvalWithMacros(t, "defmacro m stx -> syntax-parse stx do\n  (_ a (%~literal arrow) b) -> %`(quote {%,a %,b})\nend\nm :x arrow :y")
	want := core.Tuple{core.Atom("x"), core.Atom("y")}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("syntax-parse ~literal = %#v, want %#v", v, want)
	}
}

func TestSyntaxParseDatumCombinator(t *testing.T) {
	v := readExpandEvalWithMacros(t, "defmacro m stx -> syntax-parse stx do\n  (_ (%~datum 5) b) -> %`(quote %,b)\nend\nm 5 :hit")
	if v != core.Atom("hit") {
		t.Fatalf("syntax-parse ~datum = %#v, want :hit", v)
	}
}

func TestSyntaxParseOrCombinator(t *testing.T) {
	src := "defmacro m stx -> syntax-parse stx do\n  (_ (%~or [a] a)) -> %`(quote %,a)\nend\n"
	if v := readExpandEvalWithMacros(t, src+"m :solo"); v != core.Atom("solo") {
		t.Fatalf("~or second branch = %#v, want :solo", v)
	}
	want := core.Atom("p")
	if v := readExpandEvalWithMacros(t, src+"m [:p]"); v != want {
		t.Fatalf("~or first branch = %#v, want :p", v)
	}
}

func TestSyntaxParseOrBranchesMustBindSameAttributes(t *testing.T) {
	program, err := reader.ReadProgram("test", "defmacro m stx -> syntax-parse stx do\n  (_ (%~or (a b) a)) -> %':ok\nend\nm :x")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	ctx, diag, _ := setupWithMacros(t)
	if _, err := expand.Expand(program, ctx); err == nil {
		t.Fatal("expected ~or attribute-mismatch failure")
	}
	if !diag.HasErrors() || !strings.Contains(diag.Items[0].Message, "same attributes") {
		t.Fatalf("expected ~or attribute diagnostic, got %+v", diag.Items)
	}
}

func TestSyntaxParseOptionalPresentAndAbsent(t *testing.T) {
	src := "defmacro m stx -> syntax-parse stx do\n  (_ (%~optional a) b) -> %`(quote {%,a %,b})\nend\n"
	if v := readExpandEvalWithMacros(t, src+"m :x :y"); !reflect.DeepEqual(v, core.Tuple{core.Atom("x"), core.Atom("y")}) {
		t.Fatalf("~optional present = %#v", v)
	}
	if v := readExpandEvalWithMacros(t, src+"m :y"); !reflect.DeepEqual(v, core.Tuple{core.Nil{}, core.Atom("y")}) {
		t.Fatalf("~optional absent = %#v, want {nil :y}", v)
	}
}

func TestSyntaxParseSeqGroupUnderEllipsis(t *testing.T) {
	v := readExpandEvalWithMacros(t, "defmacro m stx -> syntax-parse stx do\n  (_ (%~seq k v) ...) -> %`(quote ((%,k %,v) ...))\nend\nm :a 1 :b 2")
	want := core.Pair{
		Head: core.Pair{Head: core.Atom("a"), Tail: core.Pair{Head: core.Int(1), Tail: core.Nil{}}},
		Tail: core.Pair{Head: core.Pair{Head: core.Atom("b"), Tail: core.Pair{Head: core.Int(2), Tail: core.Nil{}}}, Tail: core.Nil{}},
	}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("~seq ellipsis = %#v, want %#v", v, want)
	}
}

func TestSyntaxParseAndCombinator(t *testing.T) {
	v := readExpandEvalWithMacros(t, "defmacro m stx -> syntax-parse stx do\n  (_ (%~and whole (x y))) -> %`(quote {%,x %,y})\nend\nm (:p :q)")
	want := core.Tuple{core.Atom("p"), core.Atom("q")}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("~and = %#v, want %#v", v, want)
	}
}

func TestSyntaxParseNotCombinator(t *testing.T) {
	src := "defmacro m stx -> syntax-parse stx do\n  (_ (%~not 0) a) -> %':nonzero\n  (_ b a) -> %':zero\nend\n"
	if v := readExpandEvalWithMacros(t, src+"m 1 :x"); v != core.Atom("nonzero") {
		t.Fatalf("~not nonzero = %#v", v)
	}
	if v := readExpandEvalWithMacros(t, src+"m 0 :x"); v != core.Atom("zero") {
		t.Fatalf("~not zero = %#v", v)
	}
}

func TestSyntaxParseFenderGuard(t *testing.T) {
	src := "defmacro m stx -> syntax-parse stx do\n  (_ a) when (eq? (syntax->datum a) :good) -> %':ok\n  (_ a) -> %':fallback\nend\n"
	if v := readExpandEvalWithMacros(t, src+"m :good"); v != core.Atom("ok") {
		t.Fatalf("guard true branch = %#v, want :ok", v)
	}
	if v := readExpandEvalWithMacros(t, src+"m :bad"); v != core.Atom("fallback") {
		t.Fatalf("guard false branch = %#v, want :fallback", v)
	}
}

func TestSyntaxParseDescribeProducesError(t *testing.T) {
	program, err := reader.ReadProgram("test", "defmacro m stx -> syntax-parse stx do\n  (_ (%~describe \"needs nine\" 9)) -> %':ok\nend\nm 8")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	ctx, diag, _ := setupWithMacros(t)
	if _, err := expand.Expand(program, ctx); err == nil {
		t.Fatal("expected syntax-parse failure")
	}
	if !diag.HasErrors() || !strings.Contains(diag.Items[0].Message, "needs nine") {
		t.Fatalf("expected describe message in diagnostics, got %+v", diag.Items)
	}
}

func TestSyntaxParseFailCombinator(t *testing.T) {
	program, err := reader.ReadProgram("test", "defmacro m stx -> syntax-parse stx do\n  (_ (%~fail \"always\")) -> %':ok\nend\nm :x")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	ctx, diag, _ := setupWithMacros(t)
	if _, err := expand.Expand(program, ctx); err == nil {
		t.Fatal("expected ~fail failure")
	}
	if !diag.HasErrors() || !strings.Contains(diag.Items[0].Message, "always") {
		t.Fatalf("expected ~fail message, got %+v", diag.Items)
	}
}

func TestSyntaxParseUnknownCombinatorRejected(t *testing.T) {
	program, err := reader.ReadProgram("test", "defmacro m stx -> syntax-parse stx do\n  (_ (%~bogus a)) -> %':ok\nend\nm :x")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	ctx, diag, _ := setupWithMacros(t)
	if _, err := expand.Expand(program, ctx); err == nil {
		t.Fatal("expected unknown-combinator failure")
	}
	if !diag.HasErrors() || !strings.Contains(diag.Items[0].Message, "unknown pattern combinator") {
		t.Fatalf("expected unknown combinator diagnostic, got %+v", diag.Items)
	}
}

func TestGroupedDoBlockEvaluates(t *testing.T) {
	// Regression for the reader quirk: a do-block nested inside a group reads
	// and evaluates (last body form wins) rather than failing to parse.
	v := readExpandEvalWithMacros(t, "add 1 (do :ignored ; 2 end)")
	if v != core.Int(3) {
		t.Fatalf("grouped do-block = %#v, want 3", v)
	}
}

func TestSyntaxParseDoBlockTemplate(t *testing.T) {
	// A macro whose template is a grouped do-block `%`(do ... end)` reads,
	// compiles (unsyntax inside the %-body is processed), and expands.
	v := readExpandEvalWithMacros(t, "defmacro seq stx -> syntax-parse stx do\n  (_ a b) -> %`(do %,a ; %,b end)\nend\nseq :first :second")
	if v != core.Atom("second") {
		t.Fatalf("do-block template = %#v, want :second", v)
	}
}

func TestSyntaxParseHygieneOnIntroducedBinding(t *testing.T) {
	// The macro body references `secret`, bound at the macro DEFINITION site.
	// A use-site `secret` introduced in an inner scope must not capture it,
	// proving syntax-parse output participates in Flatt hygiene exactly as
	// syntax-case output does.
	src := "secret = :defsite\n" +
		"defmacro getsecret stx -> syntax-parse stx do\n  _ -> %`secret\nend\n" +
		"do\n  secret = :usesite\n  getsecret\nend"
	if v := readExpandEvalWithMacros(t, src); v != core.Atom("defsite") {
		t.Fatalf("syntax-parse hygiene = %#v, want :defsite (no use-site capture)", v)
	}
}

func TestQuotePreservesSingleElementLists(t *testing.T) {
	// In data position a parenthesized single form is a one-element LIST, not
	// the bare element: `'(x)` is the list (x), and inner `(b)` survives.
	// Expression grouping `(x) => x` is a distinct interpretation and is not
	// applied under quote.
	if v := readExpandEvalWithMacros(t, "'(x)"); !reflect.DeepEqual(v, core.Pair{Head: core.Word("x"), Tail: core.Nil{}}) {
		t.Fatalf("'(x) = %#v, want (x)", v)
	}
	got := readExpandEvalWithMacros(t, "'(a (b) c)")
	want := core.Pair{Head: core.Word("a"), Tail: core.Pair{Head: core.Pair{Head: core.Word("b"), Tail: core.Nil{}}, Tail: core.Pair{Head: core.Word("c"), Tail: core.Nil{}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("'(a (b) c) = %#v, want (a (b) c)", got)
	}
}

func TestSyntaxParseOneElementListPattern(t *testing.T) {
	// A list-shaped ellipsis pattern must match a one-element list, not just
	// 0 or 2+ (the single-element group-collapse regression).
	v := readExpandEvalWithMacros(t, "defmacro m stx -> syntax-parse stx do\n  (_ ((n v) ...)) -> %`(quote ((%,n %,v) ...))\nend\nm ((x 1))")
	want := core.Pair{Head: core.Pair{Head: core.Word("x"), Tail: core.Pair{Head: core.Int(1), Tail: core.Nil{}}}, Tail: core.Nil{}}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("one-element list pattern = %#v, want ((x 1))", v)
	}
}

func TestSyntaxParseVariadicMatchesZeroArgs(t *testing.T) {
	// A variadic `(_ a ...)` must match a zero-argument invocation uniformly,
	// whether written bare `m` or grouped `(m)`.
	src := "defmacro m stx -> syntax-parse stx do\n  (_ a ...) -> %`(quote (%,a ...))\nend\n"
	if v := readExpandEvalWithMacros(t, src+"m"); !reflect.DeepEqual(v, core.Nil{}) {
		t.Fatalf("variadic zero-arg (bare) = %#v, want ()", v)
	}
	if v := readExpandEvalWithMacros(t, src+"(m)"); !reflect.DeepEqual(v, core.Nil{}) {
		t.Fatalf("variadic zero-arg (paren) = %#v, want ()", v)
	}
	v := readExpandEvalWithMacros(t, src+"m :x :y")
	want := core.Pair{Head: core.Atom("x"), Tail: core.Pair{Head: core.Atom("y"), Tail: core.Nil{}}}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("variadic two-arg = %#v, want (:x :y)", v)
	}
}
