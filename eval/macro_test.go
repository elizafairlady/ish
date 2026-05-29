package eval

import (
	"reflect"
	"testing"

	"ish/core"
	"ish/expand"
	"ish/reader"
)

// setupWithMacros returns a Context wired with a MacroRunner so the macro
// core form can install user-defined transformers during expansion.
func setupWithMacros(t *testing.T) (*expand.Context, *expand.Diagnostics, *Env) {
	t.Helper()
	tbl := expand.NewBindingTable()
	expand.InstallKernel(tbl)
	InstallRuntimeKernel(tbl)
	diag := &expand.Diagnostics{}
	rt := NewRuntime()
	ctx := expand.NewContext("test", tbl, diag)
	ctx.Macros = &MacroRunner{Runtime: rt}
	return ctx, diag, &Env{Runtime: rt, Process: rt.NewProcess()}
}

func expandAndEvalWithMacros(t *testing.T, src *core.Syntax) Value {
	t.Helper()
	ctx, diag, env := setupWithMacros(t)
	expanded, err := expand.Expand(src, ctx)
	if err != nil {
		t.Fatalf("expand: %v (diag: %+v)", err, diag.Items)
	}
	v, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	return v
}

func readExpandEvalWithMacros(t *testing.T, source string) Value {
	t.Helper()
	program, err := reader.ReadProgram("test", source)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	ctx, diag, env := setupWithMacros(t)
	expanded, err := expand.Expand(program, ctx)
	if err != nil {
		t.Fatalf("expand: %v (diag: %+v)", err, diag.Items)
	}
	v, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	return v
}

// A trivial macro: (defmacro answer stx -> (syntax 42)) installs a transformer
// that always rewrites its use to syntax `42`. Evaluating `(answer)` after
// the macro definition yields Int(42).
func TestMacroTrivial(t *testing.T) {
	macroDef := core.SyntaxList(core.Span{}, word("defmacro"),
		word("answer"),
		word("stx"),
		word("->"),
		core.SyntaxList(core.Span{}, word("syntax"), &core.Syntax{Node: core.Int(42)}),
	)
	use := core.SyntaxList(core.Span{}, word("answer"))
	prog := core.SyntaxList(core.Span{}, word("do"), macroDef, use)
	if v := expandAndEvalWithMacros(t, prog); v != core.Int(42) {
		t.Fatalf("trivial macro: %v", v)
	}
}

// A macro that ignores its argument and returns a fixed syntax. Tests that
// the use site's arguments are accepted by the transformer and discarded.
func TestMacroIgnoringArguments(t *testing.T) {
	macroDef := core.SyntaxList(core.Span{}, word("defmacro"),
		word("alwaysok"),
		word("stx"),
		word("->"),
		core.SyntaxList(core.Span{}, word("syntax"), &core.Syntax{Node: core.Atom("ok")}),
	)
	use := core.SyntaxList(core.Span{}, word("alwaysok"),
		&core.Syntax{Node: core.Int(1)},
		&core.Syntax{Node: core.Int(2)})
	prog := core.SyntaxList(core.Span{}, word("do"), macroDef, use)
	if v := expandAndEvalWithMacros(t, prog); v != core.Atom("ok") {
		t.Fatalf("ignoring macro: %v", v)
	}
}

func TestReadExpandEvalMacroArrowSurface(t *testing.T) {
	v := readExpandEvalWithMacros(t, "defmacro ok stx -> %':ok\nok")
	if v != core.Atom("ok") {
		t.Fatalf("macro arrow result = %#v, want :ok", v)
	}
}

func TestReadExpandEvalAnonymousMacroBinding(t *testing.T) {
	v := readExpandEvalWithMacros(t, "ok = macro stx -> %':ok\nok")
	if v != core.Atom("ok") {
		t.Fatalf("anonymous macro binding result = %#v, want :ok", v)
	}
}

func TestReadExpandEvalMacroQuasisyntaxSurface(t *testing.T) {
	v := readExpandEvalWithMacros(t, "defmacro ok stx -> %`(quote :ok)\nok")
	if v != core.Atom("ok") {
		t.Fatalf("macro quasisyntax result = %#v, want :ok", v)
	}
}

func TestQuasisyntaxUnsyntaxSplicingSurface(t *testing.T) {
	program, err := reader.ReadProgram("test", "xs = [%'a %'b]\n%`(list %,@xs c)")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	ctx, diag, env := setupWithMacros(t)
	expanded, err := expand.Expand(program, ctx)
	if err != nil {
		t.Fatalf("expand failed: %v (diag: %+v)", err, diag.Items)
	}
	v, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	stx, ok := v.(*core.Syntax)
	if !ok {
		t.Fatalf("result = %T, want syntax", v)
	}
	if got, want := core.SyntaxToDatum(stx), (core.Pair{Head: core.Word("list"), Tail: core.Pair{Head: core.Word("a"), Tail: core.Pair{Head: core.Word("b"), Tail: core.Pair{Head: core.Word("c"), Tail: core.Nil{}}}}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("spliced syntax datum = %#v, want %#v", got, want)
	}
}

func TestReadExpandEvalMacroDestructuresSyntaxDatum(t *testing.T) {
	v := readExpandEvalWithMacros(t, "defmacro second stx -> case (syntax->datum stx) do\n  (second _ x) -> datum->syntax stx x\nend\nsecond :ignored :ok")
	if v != core.Atom("ok") {
		t.Fatalf("syntax datum destructuring macro result = %#v, want :ok", v)
	}
}

func TestReadExpandEvalSelfHostedIfMacro(t *testing.T) {
	source := "defmacro if stx -> syntax-case stx [else] do\n" +
		"  (if c t else e ...) -> %`(case %,c [:true %,t] [_ (%,@e)])\n" +
		"end\n" +
		"if :false do\n" +
		"  :bad\n" +
		"else if :true do\n" +
		"  :yes\n" +
		"else do\n" +
		"  :no\n" +
		"end"
	v := readExpandEvalWithMacros(t, source)
	if v != core.Atom("yes") {
		t.Fatalf("self-hosted if result = %#v, want :yes", v)
	}
}

func TestSyntaxCaseLiteralAndEllipsis(t *testing.T) {
	value := readExpandEvalWithMacros(t, "defmacro values stx -> syntax-case stx [values] do\n  (values x ...) -> %`(quote (%,@x))\nend\nvalues :a :b :c")
	want := core.Pair{Head: core.Atom("a"), Tail: core.Pair{Head: core.Atom("b"), Tail: core.Pair{Head: core.Atom("c"), Tail: core.Nil{}}}}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("syntax-case ellipsis result = %#v, want %#v", value, want)
	}
}

func TestSyntaxCaseLiteralIdentifierUsesBoundIdentity(t *testing.T) {
	// Without a resolver in scope, literal identifiers match by bound identity
	// (same word, literal scope set a subset of the target's), never by mere
	// spelling. This is the matcher both syntax-case and syntax-parse ~literal
	// share.
	sLit := core.NewScope()
	sOther := core.NewScope()
	lit := core.AddScope(word("kw"), core.PhaseRuntime, sLit)
	same := core.AddScope(word("kw"), core.PhaseRuntime, sLit)
	other := core.AddScope(word("kw"), core.PhaseRuntime, sOther)
	if !literalIdentifierMatches(lit, same, nil) {
		t.Fatal("literal did not match identical bound identity")
	}
	if literalIdentifierMatches(lit, other, nil) {
		t.Fatal("literal matched a differently-scoped identifier of the same spelling")
	}
}

func TestSyntaxCaseEllipsisWithMultipleVariables(t *testing.T) {
	// Each ellipsis variable is depth 1; a depth-correct template re-pairs them
	// under one ellipsis. (A bare %,x of a depth-1 attribute is a depth error.)
	value := readExpandEvalWithMacros(t, "defmacro pairs stx -> syntax-case stx [pairs] do\n  (pairs (x y) ...) -> %`(quote ((%,x %,y) ...))\nend\npairs (:a 1) (:b 2)")
	want := core.Pair{
		Head: core.Pair{Head: core.Atom("a"), Tail: core.Pair{Head: core.Int(1), Tail: core.Nil{}}},
		Tail: core.Pair{Head: core.Pair{Head: core.Atom("b"), Tail: core.Pair{Head: core.Int(2), Tail: core.Nil{}}}, Tail: core.Nil{}},
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("multi-var ellipsis result = %#v, want %#v", value, want)
	}
}

func TestSyntaxCaseNestedEllipsisDepthIsChecked(t *testing.T) {
	// Racket-faithful: an attribute used at the wrong ellipsis depth in a
	// template is a compile-time depth error, not a silent splat of the raw
	// capture. `x` is bound at depth 2 by `(x ...) ...`, and at depth 1 by
	// `x ...`; a bare `%,x` (depth 0) is rejected in both.
	for _, src := range []string{
		"defmacro m stx -> syntax-case stx [m] do\n  (m (x ...) ...) -> %`(quote %,x)\nend\nm (:a :b)",
		"defmacro m stx -> syntax-case stx [m] do\n  (m x ...) -> %`(quote %,x)\nend\nm :a :b",
	} {
		program, err := reader.ReadProgram("test", src)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		ctx, _, _ := setupWithMacros(t)
		if _, err := expand.Expand(program, ctx); err == nil {
			t.Fatalf("expected depth-mismatch error for bare %%,x; src=%q", src)
		}
	}
}

func TestSyntaxCaseNestedTemplateEllipsisFlattens(t *testing.T) {
	value := readExpandEvalWithMacros(t, "defmacro nested stx -> syntax-case stx [nested] do\n  (nested (x ...) ...) -> %`(quote (%,x ... ...))\nend\nnested (:a :b) (:c :d)")
	want := core.Pair{Head: core.Atom("a"), Tail: core.Pair{Head: core.Atom("b"), Tail: core.Pair{Head: core.Atom("c"), Tail: core.Pair{Head: core.Atom("d"), Tail: core.Nil{}}}}}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("nested template ellipsis result = %#v, want %#v", value, want)
	}
}

func TestQuasisyntaxTemplateEllipsis(t *testing.T) {
	value := readExpandEvalWithMacros(t, "defmacro vals stx -> syntax-case stx [vals] do\n  (vals x ...) -> %`(quote (%,x ...))\nend\nvals :a :b :c")
	want := core.Pair{Head: core.Atom("a"), Tail: core.Pair{Head: core.Atom("b"), Tail: core.Pair{Head: core.Atom("c"), Tail: core.Nil{}}}}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("template ellipsis result = %#v, want %#v", value, want)
	}
}

func TestQuasisyntaxRepeatedTemplateStructure(t *testing.T) {
	value := readExpandEvalWithMacros(t, "defmacro wrap stx -> syntax-case stx [wrap] do\n  (wrap x ...) -> %`(quote ((%,x) ...))\nend\nwrap :a :b")
	want := core.Pair{Head: core.Pair{Head: core.Atom("a"), Tail: core.Nil{}}, Tail: core.Pair{Head: core.Pair{Head: core.Atom("b"), Tail: core.Nil{}}, Tail: core.Nil{}}}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("repeated template structure = %#v, want %#v", value, want)
	}
}

func TestSyntaxCaseVectorPattern(t *testing.T) {
	value := readExpandEvalWithMacros(t, "defmacro m stx -> syntax-case stx [m] do\n  (m [a b]) -> %`(quote {%,a %,b})\nend\nm [:x :y]")
	want := core.Tuple{core.Atom("x"), core.Atom("y")}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("vector pattern result = %#v, want %#v", value, want)
	}
}

func TestSyntaxCaseTuplePattern(t *testing.T) {
	value := readExpandEvalWithMacros(t, "defmacro m stx -> syntax-case stx [m] do\n  (m {a b}) -> %`(quote {%,a %,b})\nend\nm {:x :y}")
	want := core.Tuple{core.Atom("x"), core.Atom("y")}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("tuple pattern result = %#v, want %#v", value, want)
	}
}

func TestSyntaxCaseVectorEllipsisPattern(t *testing.T) {
	value := readExpandEvalWithMacros(t, "defmacro m stx -> syntax-case stx [m] do\n  (m [a ...]) -> %`(quote (%,a ...))\nend\nm [:x :y :z]")
	want := core.Pair{Head: core.Atom("x"), Tail: core.Pair{Head: core.Atom("y"), Tail: core.Pair{Head: core.Atom("z"), Tail: core.Nil{}}}}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("vector ellipsis pattern result = %#v, want %#v", value, want)
	}
}

func TestSyntaxCaseDictPattern(t *testing.T) {
	value := readExpandEvalWithMacros(t, "defmacro m stx -> syntax-case stx [m] do\n  (m %{:k v}) -> %`(quote %,v)\nend\nm %{:k :found}")
	if value != core.Atom("found") {
		t.Fatalf("dict pattern result = %#v, want :found", value)
	}
}

func TestSyntaxCaseContainerKindMismatchFallsThrough(t *testing.T) {
	value := readExpandEvalWithMacros(t, "defmacro m stx -> syntax-case stx [m] do\n  (m [a b]) -> %':vec\n  _ -> %':other\nend\nm :notvec")
	if value != core.Atom("other") {
		t.Fatalf("container mismatch result = %#v, want :other", value)
	}
}

func TestSyntaxCaseRepeatedPatternVariableRequiresSameDatum(t *testing.T) {
	value := readExpandEvalWithMacros(t, "defmacro same stx -> syntax-case stx [] do\n  (same x x) -> %':same\n  _ -> %':different\nend\nsame :a :a")
	if value != core.Atom("same") {
		t.Fatalf("same repeated syntax variable result = %#v", value)
	}
	value = readExpandEvalWithMacros(t, "defmacro same stx -> syntax-case stx [] do\n  (same x x) -> %':same\n  _ -> %':different\nend\nsame :a :b")
	if value != core.Atom("different") {
		t.Fatalf("different repeated syntax variable result = %#v", value)
	}
}

func TestMacroCanRaiseSyntaxError(t *testing.T) {
	program, err := reader.ReadProgram("test", "defmacro bad stx -> syntax-error stx \"bad form\"\nbad")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	ctx, diag, _ := setupWithMacros(t)
	_, err = expand.Expand(program, ctx)
	if err == nil {
		t.Fatal("expected macro syntax-error to fail expansion")
	}
	if !diag.HasErrors() || diag.Items[0].Kind != expand.DiagBadMacroResult {
		t.Fatalf("expected bad macro diagnostic, got %+v", diag.Items)
	}
	if diag.Items[0].Span.Start.Line != 2 {
		t.Fatalf("syntax-error diagnostic span = %+v, want line 2 from macro use syntax", diag.Items[0].Span)
	}
}

func TestSyntaxInspectionPrimitives(t *testing.T) {
	value := readExpandEvalWithMacros(t, "x = %'hello\n{(syntax-kind x) (syntax-word? x) (bound-identifier=? x x)}")
	want := core.Tuple{core.Atom("word"), core.Atom("true"), core.Atom("true")}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("syntax inspection = %#v, want %#v", value, want)
	}
}

// Hygiene through a user-defined macro: the macro body references `helper`,
// resolving to the helper visible at macro DEFINITION site. A `bind` of
// `helper` at the USE site must NOT capture the macro's reference. This is
// the load-bearing proof that user macros enforce Flatt hygiene end-to-end.
func TestMacroHygieneDoesNotCaptureUseSiteBinding(t *testing.T) {
	ctx, diag, env := setupWithMacros(t)
	// Install global binding for `helper` at empty scope.
	ctx.Bindings.Define("helper", core.PhaseRuntime, expand.DefaultSpace,
		core.ScopeSet{}, expand.ValueBinding, core.Atom("global"))
	ctx.Bindings.Define("helper", core.PhaseExpand, expand.DefaultSpace,
		core.ScopeSet{}, expand.ValueBinding, core.Atom("global"))

	// (defmacro refhelper stx -> (syntax helper))
	macroDef := core.SyntaxList(core.Span{}, word("defmacro"),
		word("refhelper"),
		word("stx"),
		word("->"),
		core.SyntaxList(core.Span{}, word("syntax"), word("helper")),
	)
	// (bind :user-helper [helper (refhelper)])
	useClause := &core.Syntax{Node: core.SyntaxVector{
		word("helper"),
		core.SyntaxList(core.Span{}, word("refhelper")),
	}}
	use := core.SyntaxList(core.Span{}, word("bind"),
		core.SyntaxList(core.Span{}, word("quote"), &core.Syntax{Node: core.Atom("user-helper")}),
		useClause,
	)
	prog := core.SyntaxList(core.Span{}, word("do"), macroDef, use)
	expanded, err := expand.Expand(prog, ctx)
	if err != nil {
		t.Fatalf("expand: %v (diag: %+v)", err, diag.Items)
	}
	v, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != core.Atom("global") {
		t.Fatalf("hygiene violation: macro captured use-site helper, got %v", v)
	}
}

// macro with no MacroRunner on the context produces a clear diagnostic
// instead of silently misbehaving.
func TestMacroWithoutRunnerDiagnoses(t *testing.T) {
	tbl := expand.NewBindingTable()
	expand.InstallKernel(tbl)
	InstallRuntimeKernel(tbl)
	diag := &expand.Diagnostics{}
	ctx := expand.NewContext("test", tbl, diag)
	// Note: ctx.Macros intentionally not set.

	macroDef := core.SyntaxList(core.Span{}, word("defmacro"),
		word("nope"),
		word("stx"),
		word("->"),
		core.SyntaxList(core.Span{}, word("syntax"), &core.Syntax{Node: core.Int(1)}),
	)
	if _, err := expand.Expand(macroDef, ctx); err == nil {
		t.Fatal("expected macro-without-runner error")
	}
	if !diag.HasErrors() || diag.Items[0].Kind != expand.DiagInvalidContext {
		t.Fatalf("expected DiagInvalidContext, got %+v", diag.Items)
	}
}

func TestMacroPhaseDoesNotResolveActorPrimitivesByDefault(t *testing.T) {
	tbl := expand.NewBindingTable()
	expand.InstallKernel(tbl)
	InstallRuntimeKernel(tbl)
	if _, r := tbl.Resolve("spawn", core.PhaseExpand, expand.DefaultSpace, core.ScopeSet{}); r != expand.ResolveUnbound {
		t.Fatalf("spawn resolved at macro phase: %v", r)
	}
	if _, r := tbl.Resolve("send", core.PhaseExpand, expand.DefaultSpace, core.ScopeSet{}); r != expand.ResolveUnbound {
		t.Fatalf("send resolved at macro phase: %v", r)
	}
	if _, r := tbl.Resolve("self", core.PhaseExpand, expand.DefaultSpace, core.ScopeSet{}); r != expand.ResolveUnbound {
		t.Fatalf("self resolved at macro phase: %v", r)
	}
}
