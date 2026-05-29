package expand

import (
	"errors"
	"reflect"
	"testing"

	"ish/core"
)

// resolvedRef extracts a resolved reference whether it is bare or the callee of
// a zero-argument application — a bare value identifier expands to a zero-arg
// call (the call/value model), so resolution checks unwrap that.
func resolvedRef(t *testing.T, stx *core.Syntax) core.Resolved {
	t.Helper()
	switch n := stx.Node.(type) {
	case core.Resolved:
		return n
	case core.App:
		if len(n.Args) == 0 {
			if r, ok := n.Callee.Node.(core.Resolved); ok {
				return r
			}
		}
	}
	t.Fatalf("expected a resolved reference (bare or zero-arg call), got %#v", stx.Node)
	return core.Resolved{}
}

func newTestCtx() (*Context, *Diagnostics) {
	tbl := NewBindingTable()
	InstallKernel(tbl)
	diag := &Diagnostics{}
	return NewContext("test", tbl, diag), diag
}

func word(s string) *core.Syntax { return &core.Syntax{Node: core.Word(s)} }

func TestExpandLiteralIsIdentity(t *testing.T) {
	ctx, _ := newTestCtx()
	stx := &core.Syntax{Node: core.Int(42)}
	out, err := Expand(stx, ctx)
	if err != nil || out.Node != core.Int(42) {
		t.Fatalf("literal not preserved: out=%v err=%v", out, err)
	}
}

func TestExpandQuoteLowersToDatum(t *testing.T) {
	ctx, diag := newTestCtx()
	inner := core.SyntaxList(core.Span{}, word("foo"), word("bar"))
	stx := core.SyntaxList(core.Span{}, word("quote"), inner)
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("quote expansion failed: %v (diag: %+v)", err, diag.Items)
	}
	expected := core.Pair{Head: core.Word("foo"), Tail: core.Pair{Head: core.Word("bar"), Tail: core.Nil{}}}
	if !reflect.DeepEqual(out.Node, expected) {
		t.Fatalf("expected literal datum %#v, got %#v", expected, out.Node)
	}
}

func TestExpandQuoteOfAtom(t *testing.T) {
	ctx, _ := newTestCtx()
	stx := core.SyntaxList(core.Span{}, word("quote"), &core.Syntax{Node: core.Atom("ok")})
	out, err := Expand(stx, ctx)
	if err != nil || out.Node != core.Atom("ok") {
		t.Fatalf("quote of atom failed: out=%v err=%v", out, err)
	}
}

func TestExpandQuoteLowersReaderGroupToDatumList(t *testing.T) {
	ctx, diag := newTestCtx()
	inner := core.SyntaxList(core.Span{}, word("%-expr"), word("a"), word("b"))
	group := core.SyntaxList(core.Span{}, word("%-group"), inner)
	stx := core.SyntaxList(core.Span{}, word("quote"), group)
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("quote group expansion failed: %v (diag: %+v)", err, diag.Items)
	}
	expected := core.Pair{Head: core.Word("a"), Tail: core.Pair{Head: core.Word("b"), Tail: core.Nil{}}}
	if !reflect.DeepEqual(out.Node, expected) {
		t.Fatalf("quote group datum = %#v, want %#v", out.Node, expected)
	}
}

func TestExpandQuoteLowersReaderGroupToDottedDatum(t *testing.T) {
	ctx, diag := newTestCtx()
	inner := core.SyntaxList(core.Span{}, word("%-expr"), word("a"), word("."), word("b"))
	group := core.SyntaxList(core.Span{}, word("%-group"), inner)
	stx := core.SyntaxList(core.Span{}, word("quote"), group)
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("quote dotted group expansion failed: %v (diag: %+v)", err, diag.Items)
	}
	expected := core.Pair{Head: core.Word("a"), Tail: core.Word("b")}
	if !reflect.DeepEqual(out.Node, expected) {
		t.Fatalf("quote dotted datum = %#v, want %#v", out.Node, expected)
	}
}

func TestExpandUnboundHeadDiagnoses(t *testing.T) {
	ctx, diag := newTestCtx()
	stx := core.SyntaxList(core.Span{}, word("nope"))
	if _, err := Expand(stx, ctx); err == nil {
		t.Fatal("expected error for unbound head")
	}
	if !diag.HasErrors() || diag.Items[0].Kind != DiagUnbound {
		t.Fatalf("expected DiagUnbound, got %+v", diag.Items)
	}
}

func TestReaderProtocolUnhandledDiagnoses(t *testing.T) {
	ctx, diag := newTestCtx()
	stx := core.SyntaxList(core.Span{}, word("%-unknown"), word("printf"), word("hello"))
	if _, err := Expand(stx, ctx); err == nil {
		t.Fatal("expected unhandled protocol error")
	}
	if !diag.HasErrors() || diag.Items[0].Kind != DiagProtocolUnhandled {
		t.Fatalf("expected DiagProtocolUnhandled, got %+v", diag.Items)
	}
}

func TestActiveProtocolHandlesReaderForm(t *testing.T) {
	ctx, diag := newTestCtx()
	installTestHandler(ctx, ProtocolHandler{
		Form:  "%-unknown",
		Kind:  PackageCtx,
		Phase: core.PhaseRuntime,
		Transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
			return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom("handled")}), nil
		}),
	})
	stx := core.SyntaxList(core.Span{}, word("%-unknown"), word("anything"))
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("protocol expansion failed: %v (diag: %+v)", err, diag.Items)
	}
	if out.Node != core.Atom("handled") {
		t.Fatalf("protocol result = %#v, want :handled", out.Node)
	}
}

func TestActiveProtocolAmbiguityDiagnoses(t *testing.T) {
	ctx, diag := newTestCtx()
	for _, name := range []string{"a", "b"} {
		name := name
		installTestHandler(ctx, ProtocolHandler{
			Form:  "%-unknown",
			Kind:  PackageCtx,
			Phase: core.PhaseRuntime,
			Transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
				return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom(name)}), nil
			}),
		})
	}
	stx := core.SyntaxList(core.Span{}, word("%-unknown"), word("anything"))
	if _, err := Expand(stx, ctx); err == nil {
		t.Fatal("expected ambiguous protocol error")
	}
	if !diag.HasErrors() || diag.Items[0].Kind != DiagProtocolAmbiguous {
		t.Fatalf("expected DiagProtocolAmbiguous, got %+v", diag.Items)
	}
}

func TestMoreSpecificProtocolWins(t *testing.T) {
	ctx, _ := newTestCtx()
	s := core.NewScope()
	installTestHandler(ctx, ProtocolHandler{
		Form:  "%-unknown",
		Kind:  PackageCtx,
		Phase: core.PhaseRuntime,
		Transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
			return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom("outer")}), nil
		}),
	})
	installTestHandler(ctx, ProtocolHandler{
		Form:   "%-unknown",
		Kind:   PackageCtx,
		Phase:  core.PhaseRuntime,
		Scopes: core.ScopeSet{}.Add(s),
		Transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
			return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom("inner")}), nil
		}),
	})
	stx := core.AddScope(core.SyntaxList(core.Span{}, word("%-unknown"), word("anything")), core.PhaseRuntime, s)
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("protocol expansion failed: %v", err)
	}
	if out.Node != core.Atom("inner") {
		t.Fatalf("protocol specificity result = %#v, want :inner", out.Node)
	}
}

func TestReaderExprPureApplicationUsesBaselinePath(t *testing.T) {
	ctx, _ := newTestCtx()
	id := ctx.Bindings.Define("id", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, ValueBinding, "id-value")
	stx := core.SyntaxList(core.Span{}, word("%-expr"), word("id"), &core.Syntax{Node: core.Int(7)})
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("reader expr application expansion failed: %v", err)
	}
	app, ok := out.Node.(core.App)
	if !ok {
		t.Fatalf("reader expr expanded to %T, want core.App", out.Node)
	}
	callee, ok := app.Callee.Node.(core.Resolved)
	if !ok || callee.ID != id.ID || callee.Value != id.Value {
		t.Fatalf("app callee = %#v, want resolved id", app.Callee.Node)
	}
	if len(app.Args) != 1 || app.Args[0].Node != core.Int(7) {
		t.Fatalf("app args = %#v", app.Args)
	}
}

func TestReaderExprUnboundHeadFailsWithoutImplementedMode(t *testing.T) {
	ctx, diag := newTestCtx()
	stx := core.SyntaxList(core.Span{}, word("%-expr"), word("tool"), word("arg"))
	if _, err := Expand(stx, ctx); err == nil {
		t.Fatal("expected unbound reader expression without implemented mode")
	}
	if !diag.HasErrors() || diag.Items[0].Kind != DiagUnbound {
		t.Fatalf("expected DiagUnbound, got %+v", diag.Items)
	}
}

func TestReaderExprImplementedModeClaimsOnlyUnboundHead(t *testing.T) {
	ctx, _ := newTestCtx()
	installTestHandler(ctx, ProtocolHandler{
		Form:  "%-expr",
		Kind:  PackageCtx,
		Phase: core.PhaseRuntime,
		Transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
			return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom("claimed")}), nil
		}),
	})

	unbound := core.SyntaxList(core.Span{}, word("%-expr"), word("tool"), word("arg"))
	out, err := Expand(unbound, ctx)
	if err != nil {
		t.Fatalf("implemented mode did not claim unbound head: %v", err)
	}
	if out.Node != core.Atom("claimed") {
		t.Fatalf("implemented mode result = %#v, want :claimed", out.Node)
	}

	id := ctx.Bindings.Define("tool", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, ValueBinding, "tool-value")
	resolved := core.SyntaxList(core.Span{}, word("%-expr"), word("tool"), &core.Syntax{Node: core.Int(1)})
	out, err = Expand(resolved, ctx)
	if err != nil {
		t.Fatalf("resolved head should remain pure application: %v", err)
	}
	app, ok := out.Node.(core.App)
	if !ok {
		t.Fatalf("resolved head expanded to %T, want core.App", out.Node)
	}
	callee, ok := app.Callee.Node.(core.Resolved)
	if !ok || callee.ID != id.ID || callee.Value != id.Value {
		t.Fatalf("resolved head was stolen by implemented mode: %#v", app.Callee.Node)
	}
}

func TestReaderExprOperatorTokenWithoutBindingIsOrdinaryIdentifier(t *testing.T) {
	// With no operator declared for `+`, it is not an operator at all — the
	// expander presupposes no operator set. `1 + 2` is then an application whose
	// `+` is an ordinary identifier, which here is unbound.
	ctx, diag := newTestCtx()
	stx := core.SyntaxList(core.Span{}, word("%-expr"), &core.Syntax{Node: core.Int(1)}, word("+"), &core.Syntax{Node: core.Int(2)})
	if _, err := Expand(stx, ctx); err == nil {
		t.Fatal("expected error: + is unbound")
	}
	if !diag.HasErrors() || diag.Items[0].Kind != DiagUnbound {
		t.Fatalf("expected DiagUnbound for the bare +, got %+v", diag.Items)
	}
}

func TestReaderExprOperatorMetadataEnforestsPrecedence(t *testing.T) {
	ctx, _ := newTestCtx()
	add := ctx.Bindings.Define("add", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, ValueBinding, "add-value")
	mul := ctx.Bindings.Define("mul", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, ValueBinding, "mul-value")
	installTestOperator(ctx, infixTestOperator("+", 60, AssocLeft, "add"))
	installTestOperator(ctx, infixTestOperator("*", 70, AssocLeft, "mul"))

	stx := core.SyntaxList(core.Span{}, word("%-expr"), &core.Syntax{Node: core.Int(1)}, word("+"), &core.Syntax{Node: core.Int(2)}, word("*"), &core.Syntax{Node: core.Int(3)})
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("operator expansion failed: %v", err)
	}
	outer, ok := out.Node.(core.App)
	if !ok {
		t.Fatalf("operator output = %T, want outer App", out.Node)
	}
	outerCallee, ok := outer.Callee.Node.(core.Resolved)
	if !ok || outerCallee.ID != add.ID || outerCallee.Value != add.Value {
		t.Fatalf("outer callee = %#v, want add", outer.Callee.Node)
	}
	if len(outer.Args) != 2 || outer.Args[0].Node != core.Int(1) {
		t.Fatalf("outer args = %#v", outer.Args)
	}
	inner, ok := outer.Args[1].Node.(core.App)
	if !ok {
		t.Fatalf("right arg = %T, want inner App", outer.Args[1].Node)
	}
	innerCallee, ok := inner.Callee.Node.(core.Resolved)
	if !ok || innerCallee.ID != mul.ID || innerCallee.Value != mul.Value {
		t.Fatalf("inner callee = %#v, want mul", inner.Callee.Node)
	}
	if len(inner.Args) != 2 || inner.Args[0].Node != core.Int(2) || inner.Args[1].Node != core.Int(3) {
		t.Fatalf("inner args = %#v", inner.Args)
	}
}

func TestReaderExprNonAssociativeOperatorCannotChain(t *testing.T) {
	ctx, diag := newTestCtx()
	ctx.Bindings.Define("eq?", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, ValueBinding, "eq-value")
	installTestOperator(ctx, infixTestOperator("==", 40, AssocNone, "eq?"))
	stx := core.SyntaxList(core.Span{}, word("%-expr"), word("a"), word("=="), word("b"), word("=="), word("c"))
	if _, err := Expand(stx, ctx); err == nil {
		t.Fatal("expected chained non-associative operator to fail")
	}
	if !diag.HasErrors() || diag.Items[0].Kind != DiagSyntaxShape {
		t.Fatalf("expected syntax-shape diagnostic, got %+v", diag.Items)
	}
}

func TestImportUseAndImplementsAreSeparate(t *testing.T) {
	ctx, _ := newTestCtx()
	pkg := NewPackage("std/math")
	pkg.ExportValue("inc", "inc-value")
	pkg.ExportProtocol("ops", ProtocolExport{Operators: []OperatorEntry{infixTestOperator("+", 60, AssocLeft, "opadd")}})
	RegisterPackage(ctx, pkg)

	// import enables qualified package access only: it binds the package alias in
	// the package space (which the access protocol later resolves through), and
	// nothing else. (Default access itself is std/impl/kernel, an ish package
	// tested at the runtime layer; the expander core bakes in no access.)
	importForm := core.SyntaxList(core.Span{}, word("import"), word("std/math"))
	if _, err := Expand(importForm, ctx); err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if _, r := ctx.Bindings.Resolve("math", core.PhaseRuntime, PackageSpace, core.ScopeSet{}); r != ResolveFound {
		t.Fatal("import did not bind the package alias for qualified access")
	}
	if _, err := Expand(word("inc"), ctx); err == nil {
		t.Fatal("import made inc unqualified; want separation from use")
	}
	if _, err := Expand(core.SyntaxList(core.Span{}, word("%-expr"), &core.Syntax{Node: core.Int(1)}, word("+"), &core.Syntax{Node: core.Int(2)}), ctx); err == nil {
		t.Fatal("import activated operator protocol; want separation from implements")
	}

	// use enables unqualified exports but still does not activate protocols.
	useForm := core.SyntaxList(core.Span{}, word("use"), word("std/math"))
	if _, err := Expand(useForm, ctx); err != nil {
		t.Fatalf("use failed: %v", err)
	}
	out, err := Expand(word("inc"), ctx)
	if err != nil {
		t.Fatalf("unqualified inc failed after use: %v", err)
	}
	ref := resolvedRef(t, out)
	if ref.Value != "inc-value" {
		t.Fatalf("use result = %#v, want inc export", out.Node)
	}
	if _, err := Expand(core.SyntaxList(core.Span{}, word("%-expr"), &core.Syntax{Node: core.Int(1)}, word("+"), &core.Syntax{Node: core.Int(2)}), ctx); err == nil {
		t.Fatal("use activated operator protocol; want separation from implements")
	}

	// implements activates exported protocol metadata without importing values.
	ctx2, _ := newTestCtx()
	RegisterPackage(ctx2, pkg)
	opadd := ctx2.Bindings.Define("opadd", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, ValueBinding, "opadd-value")
	if _, err := Expand(core.SyntaxList(core.Span{}, word("implements"), word("std/math")), ctx2); err != nil {
		t.Fatalf("implements failed: %v", err)
	}
	opOut, err := Expand(core.SyntaxList(core.Span{}, word("%-expr"), &core.Syntax{Node: core.Int(1)}, word("+"), &core.Syntax{Node: core.Int(2)}), ctx2)
	if err != nil {
		t.Fatalf("implements did not activate operator protocol: %v", err)
	}
	app, ok := opOut.Node.(core.App)
	if !ok {
		t.Fatalf("operator output = %T, want core.App", opOut.Node)
	}
	callee, ok := app.Callee.Node.(core.Resolved)
	if !ok || callee.ID != opadd.ID {
		t.Fatalf("operator callee = %#v, want opadd", app.Callee.Node)
	}
	if _, err := Expand(word("inc"), ctx2); err == nil {
		t.Fatal("implements imported value inc; want protocol-only activation")
	}
}

func TestDottedPackageAccessRequiresAnActiveAccessProtocol(t *testing.T) {
	ctx, _ := newTestCtx()
	pkg := NewPackage("std/math")
	pkg.ExportValue("inc", "inc-value")
	RegisterPackage(ctx, pkg)
	if _, err := Expand(core.SyntaxList(core.Span{}, word("import"), word("std/math")), ctx); err != nil {
		t.Fatalf("import failed: %v", err)
	}
	// The expander core bakes in no qualified-access behavior. Default access is
	// std/impl/kernel (an ish package) loaded by the runtime; with no access
	// protocol active here, a dotted access has no claimant and errors.
	if _, err := Expand(core.SyntaxList(core.Span{}, word("%-expr"), word("math"), word("."), word("inc")), ctx); err == nil {
		t.Fatal("dotted package access worked with no active access protocol")
	}
}

func TestUseImportsTransformerExportsWithoutActivatingProtocols(t *testing.T) {
	ctx, _ := newTestCtx()
	pkg := NewPackage("std/macros")
	pkg.ExportTransformer("always", Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
		return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom("ok")}), nil
	}))
	pkg.ExportProtocol("unused", ProtocolExport{Operators: []OperatorEntry{infixTestOperator("+", 60, AssocLeft, "add")}})
	RegisterPackage(ctx, pkg)
	if _, err := Expand(core.SyntaxList(core.Span{}, word("use"), word("std/macros")), ctx); err != nil {
		t.Fatalf("use failed: %v", err)
	}
	out, err := Expand(core.SyntaxList(core.Span{}, word("always")), ctx)
	if err != nil {
		t.Fatalf("macro export did not expand after use: %v", err)
	}
	if out.Node != core.Atom("ok") {
		t.Fatalf("macro export result = %#v, want :ok", out.Node)
	}
	if _, err := Expand(core.SyntaxList(core.Span{}, word("%-expr"), &core.Syntax{Node: core.Int(1)}, word("+"), &core.Syntax{Node: core.Int(2)}), ctx); err == nil {
		t.Fatal("use activated protocol export; want transformer import only")
	}
}

func TestMacroIntroducedIdentifierUsesDefinitionContext(t *testing.T) {
	ctx, _ := newTestCtx()
	helperScope := core.NewScope()
	helper := ctx.Bindings.Define("helper", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}.Add(helperScope), ValueBinding, "definition-helper")
	ctx.Macros = staticMacroRunner{transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
		return word("helper"), nil
	})}
	macroDef := core.AddScope(core.SyntaxList(core.Span{}, word("defmacro"), word("use-helper"), word("stx"), word("->"), core.SyntaxList(core.Span{}, word("quote"), word("ignored"))), core.PhaseRuntime, helperScope)
	if _, err := Expand(macroDef, ctx); err != nil {
		t.Fatalf("macro definition failed: %v", err)
	}
	useScope := core.NewScope()
	ctx.Bindings.Define("helper", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}.Add(useScope), ValueBinding, "use-helper")
	out, err := Expand(core.AddScope(core.SyntaxList(core.Span{}, word("use-helper")), core.PhaseRuntime, useScope), ctx)
	if err != nil {
		t.Fatalf("macro use failed: %v", err)
	}
	ref := resolvedRef(t, out)
	if ref.ID != helper.ID {
		t.Fatalf("introduced helper resolved to %#v, want definition helper", out.Node)
	}
}

func TestProtocolHandlersClaimActualSyntax(t *testing.T) {
	ctx, _ := newTestCtx()
	installTestHandler(ctx, ProtocolHandler{
		Form:  "%-expr",
		Kind:  PackageCtx,
		Phase: core.PhaseRuntime,
		Claim: func(stx *core.Syntax) bool { return readerExprHeadIs(stx, "tool") },
		Transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
			return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom("tool")}), nil
		}),
	})
	installTestHandler(ctx, ProtocolHandler{
		Form:  "%-expr",
		Kind:  PackageCtx,
		Phase: core.PhaseRuntime,
		Claim: func(stx *core.Syntax) bool { return readerExprHeadIs(stx, "task:") },
		Transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
			return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom("task")}), nil
		}),
	})
	out, err := Expand(core.SyntaxList(core.Span{}, word("%-expr"), word("tool"), word("arg")), ctx)
	if err != nil || out.Node != core.Atom("tool") {
		t.Fatalf("tool handler = %#v err=%v", out.Node, err)
	}
	out, err = Expand(core.SyntaxList(core.Span{}, word("%-expr"), word("task:"), word("dep")), ctx)
	if err != nil || out.Node != core.Atom("task") {
		t.Fatalf("task handler = %#v err=%v", out.Node, err)
	}
	if _, err := Expand(core.SyntaxList(core.Span{}, word("%-expr"), word("other")), ctx); err == nil {
		t.Fatal("unclaimed expression unexpectedly handled")
	}
}

func TestFakeShellProtocolClaimsGenericEnvRedirectPipe(t *testing.T) {
	ctx, _ := newTestCtx()
	installTestHandler(ctx, ProtocolHandler{
		Form:  "%-expr",
		Kind:  PackageCtx,
		Phase: core.PhaseRuntime,
		Claim: hasAnyToken("=", ">", "|"),
		Transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
			return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom("shellish")}), nil
		}),
	})
	for _, stx := range []*core.Syntax{
		core.SyntaxList(core.Span{}, word("%-expr"), word("FOO"), word("="), word("bar"), word("cmd")),
		core.SyntaxList(core.Span{}, word("%-expr"), word("cmd"), word(">"), word("out")),
		core.SyntaxList(core.Span{}, word("%-expr"), word("cmd"), word("|"), word("next")),
	} {
		out, err := Expand(stx, ctx)
		if err != nil || out.Node != core.Atom("shellish") {
			t.Fatalf("fake shell claim failed: out=%#v err=%v", out.Node, err)
		}
	}
}

func TestFakeTaskProtocolClaimsGenericColon(t *testing.T) {
	ctx, _ := newTestCtx()
	installTestHandler(ctx, ProtocolHandler{
		Form:  "%-expr",
		Kind:  PackageCtx,
		Phase: core.PhaseRuntime,
		Claim: func(stx *core.Syntax) bool {
			elems, ok := core.SyntaxListElems(stx)
			return ok && len(elems) >= 3 && syntaxWord(elems[2]) == ":"
		},
		Transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
			return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom("task")}), nil
		}),
	})
	out, err := Expand(core.SyntaxList(core.Span{}, word("%-expr"), word("build"), word(":"), word("deps")), ctx)
	if err != nil || out.Node != core.Atom("task") {
		t.Fatalf("fake task claim failed: out=%#v err=%v", out.Node, err)
	}
}

func TestAmbiguousTokenStreamsFailWithoutClaimingProtocol(t *testing.T) {
	ctx, _ := newTestCtx()
	cases := []*core.Syntax{
		core.SyntaxList(core.Span{}, word("%-expr"), word("foo"), word(".")),
		core.SyntaxList(core.Span{}, word("%-expr"), word("cmd"), word("2"), word("|"), word("&"), word("1")),
		core.SyntaxList(core.Span{}, word("%-expr"), word("build"), word(":"), word("deps")),
		core.SyntaxList(core.Span{}, word("%-expr"), word("$"), word("name")),
	}
	for _, stx := range cases {
		if _, err := Expand(stx, ctx); err == nil {
			t.Fatalf("ambiguous token stream expanded without protocol: %#v", stx)
		}
	}
}

func TestFakeProtocolCanClaimOtherwiseInvalidDottedSyntax(t *testing.T) {
	ctx, _ := newTestCtx()
	installTestHandler(ctx, ProtocolHandler{
		Form:  "%-expr",
		Kind:  PackageCtx,
		Phase: core.PhaseRuntime,
		Claim: hasAnyToken("."),
		Transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
			return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom("dotted")}), nil
		}),
	})
	out, err := Expand(core.SyntaxList(core.Span{}, word("%-expr"), word("foo"), word(".")), ctx)
	if err != nil || out.Node != core.Atom("dotted") {
		t.Fatalf("dotted claim = %#v err=%v, want :dotted", out.Node, err)
	}
}

func hasAnyToken(tokens ...core.Word) func(*core.Syntax) bool {
	want := map[core.Word]bool{}
	for _, t := range tokens {
		want[t] = true
	}
	return func(stx *core.Syntax) bool {
		elems, ok := core.SyntaxListElems(stx)
		if !ok {
			return false
		}
		for _, elem := range elems[1:] {
			if want[syntaxWord(elem)] {
				return true
			}
		}
		return false
	}
}

func syntaxWord(stx *core.Syntax) core.Word {
	if w, ok := stx.Node.(core.Word); ok {
		return w
	}
	return ""
}

func TestClaimingProtocolHandlersAmbiguousOnlyWhenBothClaim(t *testing.T) {
	ctx, diag := newTestCtx()
	for _, name := range []string{"a", "b"} {
		name := name
		installTestHandler(ctx, ProtocolHandler{
			Form:  "%-expr",
			Kind:  PackageCtx,
			Phase: core.PhaseRuntime,
			Claim: func(stx *core.Syntax) bool { return readerExprHeadIs(stx, "tool") },
			Transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
				return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom(name)}), nil
			}),
		})
	}
	if _, err := Expand(core.SyntaxList(core.Span{}, word("%-expr"), word("tool")), ctx); err == nil {
		t.Fatal("expected ambiguity when both handlers claim")
	}
	if !diag.HasErrors() || diag.Items[0].Kind != DiagProtocolAmbiguous {
		t.Fatalf("expected protocol ambiguity, got %+v", diag.Items)
	}
}

func TestImplementsSpecificProtocol(t *testing.T) {
	ctx, _ := newTestCtx()
	pkg := NewPackage("std/impl/mixed")
	pkg.ExportProtocol("plus", ProtocolExport{Operators: []OperatorEntry{infixTestOperator("+", 60, AssocLeft, "add")}})
	pkg.ExportProtocol("star", ProtocolExport{Operators: []OperatorEntry{infixTestOperator("*", 70, AssocLeft, "mul")}})
	RegisterPackage(ctx, pkg)
	ctx.Bindings.Define("add", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, ValueBinding, "add-value")
	ctx.Bindings.Define("mul", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, ValueBinding, "mul-value")
	if _, err := Expand(core.SyntaxList(core.Span{}, word("implements"), word("std/impl/mixed.plus")), ctx); err != nil {
		t.Fatalf("implements specific protocol failed: %v", err)
	}
	if _, err := Expand(core.SyntaxList(core.Span{}, word("%-expr"), &core.Syntax{Node: core.Int(1)}, word("+"), &core.Syntax{Node: core.Int(2)}), ctx); err != nil {
		t.Fatalf("plus operator inactive after specific implements: %v", err)
	}
	if _, err := Expand(core.SyntaxList(core.Span{}, word("%-expr"), &core.Syntax{Node: core.Int(1)}, word("*"), &core.Syntax{Node: core.Int(2)}), ctx); err == nil {
		t.Fatal("star operator active despite implementing only plus")
	}
}

func readerExprHeadIs(stx *core.Syntax, want core.Word) bool {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) < 2 {
		return false
	}
	head, ok := elems[1].Node.(core.Word)
	return ok && head == want
}

func TestExpressionContextRejectsCompileTimeForms(t *testing.T) {
	ctx, diag := newTestCtx()
	stx := core.SyntaxList(core.Span{}, word("%-expression"), core.SyntaxList(core.Span{}, word("import"), word("std/base")))
	if _, err := Expand(stx, ctx); err == nil {
		t.Fatal("expected import in expression context to fail")
	}
	if !diag.HasErrors() || diag.Items[0].Kind != DiagInvalidContext {
		t.Fatalf("expected invalid-context diagnostic, got %+v", diag.Items)
	}
}

func infixTestOperator(token string, precedence int, assoc OperatorAssociativity, target string) OperatorEntry {
	return OperatorEntry{
		Token:      core.Word(token),
		Kind:       PackageCtx,
		Phase:      core.PhaseRuntime,
		Precedence: precedence,
		Assoc:      assoc,
		Fixity:     FixityInfix,
		Transformer: OperatorTransformer(func(stx *core.Syntax, operands []*core.Syntax) (*core.Syntax, error) {
			elems := append([]*core.Syntax{word(target)}, operands...)
			return core.SyntaxList(stx.Span, elems...), nil
		}),
	}
}

func installTestOperator(ctx *Context, op OperatorEntry) {
	ctx.Bindings.Define(operatorBindingName(op.Kind, op.Token), op.Phase, OperatorSpace, op.Scopes, OperatorBinding, []OperatorEntry{op})
}

func installTestHandler(ctx *Context, h ProtocolHandler) {
	ctx.Bindings.Define(protocolHandlerBindingName(h.Kind, h.Form), h.Phase, ProtocolHandlerSpace, h.Scopes, ProtocolHandlerBinding, h)
}

func TestExpandQuoteArityDiagnoses(t *testing.T) {
	ctx, diag := newTestCtx()
	stx := core.SyntaxList(core.Span{}, word("quote"), word("a"), word("b"))
	if _, err := Expand(stx, ctx); err == nil {
		t.Fatal("expected arity error from quote")
	}
	if !diag.HasErrors() || diag.Items[0].Kind != DiagSyntaxArity {
		t.Fatalf("expected DiagSyntaxArity, got %+v", diag.Items)
	}
}

func TestExpandQuoteRejectsExpandedCore(t *testing.T) {
	ctx, diag := newTestCtx()
	stx := core.SyntaxList(core.Span{}, word("quote"), core.SyntaxApp(core.Span{}, word("f"), word("x")))
	if _, err := Expand(stx, ctx); err == nil {
		t.Fatal("quote accepted expanded core App")
	}
	if !diag.HasErrors() || diag.Items[0].Kind != DiagInvalidContext {
		t.Fatalf("expected DiagInvalidContext, got %+v", diag.Items)
	}
}

// End-to-end: a TransformerBinding installed at a region scope must shadow
// the kernel's core-form binding for the same name. This is the load-bearing
// extensibility check — domain languages override core behavior this way.
func TestKernelCoreFormCanBeShadowedByTransformer(t *testing.T) {
	ctx, _ := newTestCtx()
	sRegion := core.NewScope()
	called := false
	override := CoreFormHandler(func(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
		called = true
		return &core.Syntax{Node: core.Atom("overridden")}, nil
	})
	ctx.Bindings.Define("quote", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}.Add(sRegion), CoreFormBinding, override)

	stx := core.AddScope(core.SyntaxList(core.Span{}, word("quote"), word("x")), core.PhaseRuntime, sRegion)
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("expand failed: %v", err)
	}
	if !called || out.Node != core.Atom("overridden") {
		t.Fatalf("override not used: called=%v out=%v", called, out)
	}

	stx2 := core.SyntaxList(core.Span{}, word("quote"), word("x"))
	out2, err := Expand(stx2, ctx)
	if err != nil {
		t.Fatalf("kernel quote failed: %v", err)
	}
	if out2.Node != core.Word("x") {
		t.Fatalf("kernel quote did not run: out=%v", out2)
	}
}

// Identifier resolution returns a Resolved node carrying the binding.
func TestExpandIdentifierResolves(t *testing.T) {
	ctx, _ := newTestCtx()
	b := ctx.Bindings.Define("x", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, ValueBinding, "x-value")
	out, err := Expand(word("x"), ctx)
	if err != nil {
		t.Fatalf("identifier resolution failed: %v", err)
	}
	ref := resolvedRef(t, out)
	if ref.Name != "x" || ref.ID != b.ID || ref.Value != b.Value {
		t.Fatalf("expected Resolved{x, b}, got %#v", out.Node)
	}
}

func TestExpandApplicationEmitsExplicitApp(t *testing.T) {
	ctx, _ := newTestCtx()
	add := ctx.Bindings.Define("add", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, ValueBinding, "add-value")
	stx := core.SyntaxList(core.Span{}, word("add"), &core.Syntax{Node: core.Int(1)}, &core.Syntax{Node: core.Int(2)})
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("application expansion failed: %v", err)
	}
	app, ok := out.Node.(core.App)
	if !ok {
		t.Fatalf("application expanded to %T, want core.App", out.Node)
	}
	callee, ok := app.Callee.Node.(core.Resolved)
	if !ok || callee.ID != add.ID || callee.Value != add.Value {
		t.Fatalf("app callee = %#v, want resolved add", app.Callee.Node)
	}
	if len(app.Args) != 2 || app.Args[0].Node != core.Int(1) || app.Args[1].Node != core.Int(2) {
		t.Fatalf("app args = %#v", app.Args)
	}
}

func TestDottedExpressionCanBeClaimedByProtocol(t *testing.T) {
	ctx, _ := newTestCtx()
	s := core.NewScope()
	installTestHandler(ctx, ProtocolHandler{
		Form:   "%-expr",
		Kind:   PackageCtx,
		Phase:  core.PhaseRuntime,
		Scopes: core.ScopeSet{}.Add(s),
		Claim:  func(stx *core.Syntax) bool { return readerExprHeadIs(stx, "a") },
		Transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
			return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom("custom")}), nil
		}),
	})
	access := core.AddScope(core.SyntaxList(core.Span{}, word("%-expr"), word("a"), word("."), word("b")), core.PhaseRuntime, s)
	out, err := Expand(access, ctx)
	if err != nil {
		t.Fatalf("custom access failed: %v", err)
	}
	if out.Node != core.Atom("custom") {
		t.Fatalf("custom access result = %#v, want :custom", out.Node)
	}
}

func TestExpandRejectsExternallySuppliedApp(t *testing.T) {
	ctx, diag := newTestCtx()
	stx := core.SyntaxApp(core.Span{}, word("f"), word("x"))
	if _, err := Expand(stx, ctx); err == nil {
		t.Fatal("Expand(core.App) error = nil, want rejection")
	}
	if diag.HasErrors() {
		t.Fatalf("raw App rejection should be structural, got diagnostics %+v", diag.Items)
	}
}

func TestExpandRejectsExternallySuppliedResolved(t *testing.T) {
	ctx, diag := newTestCtx()
	stx := &core.Syntax{Node: core.Resolved{Name: "x"}}
	if _, err := Expand(stx, ctx); err == nil {
		t.Fatal("Expand(core.Resolved) error = nil, want rejection")
	}
	if diag.HasErrors() {
		t.Fatalf("raw Resolved rejection should be structural, got diagnostics %+v", diag.Items)
	}
}

func TestExpandRejectsExpandedCoreInsideReaderContainer(t *testing.T) {
	ctx, _ := newTestCtx()
	stx := &core.Syntax{Node: core.SyntaxVector{core.SyntaxApp(core.Span{}, word("f"))}}
	if _, err := Expand(stx, ctx); err == nil {
		t.Fatal("Expand accepted SyntaxVector containing App")
	}
}

func TestTransformerReturningAppIsRejected(t *testing.T) {
	ctx, _ := newTestCtx()
	ctx.Bindings.Define("bad", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, TransformerBinding,
		&SyntaxTransformer{Fn: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
			return core.SyntaxApp(stx.Span, word("f"), word("x")), nil
		})})
	if _, err := Expand(core.SyntaxList(core.Span{}, word("bad")), ctx); err == nil {
		t.Fatal("transformer returned App without expansion error")
	}
}

func TestBindAndMatchEmitExplicitApp(t *testing.T) {
	ctx, _ := newTestCtx()
	bind := core.SyntaxList(core.Span{}, word("bind"), &core.Syntax{Node: core.Int(1)}, &core.Syntax{Node: core.SyntaxVector{word("x"), word("x")}})
	bindOut, err := Expand(bind, ctx)
	if err != nil {
		t.Fatalf("bind expansion failed: %v", err)
	}
	if _, ok := bindOut.Node.(core.App); !ok {
		t.Fatalf("bind expanded to %T, want core.App", bindOut.Node)
	}

	match := core.SyntaxList(core.Span{}, word("match"), &core.Syntax{Node: core.Int(1)}, &core.Syntax{Node: core.SyntaxVector{word("x"), word("x")}})
	matchOut, err := Expand(match, ctx)
	if err != nil {
		t.Fatalf("match expansion failed: %v", err)
	}
	if _, ok := matchOut.Node.(core.App); !ok {
		t.Fatalf("match expanded to %T, want core.App", matchOut.Node)
	}
}

// Transformer is invoked and the result is re-expanded.
func TestTransformerInvokedAndResultExpanded(t *testing.T) {
	ctx, _ := newTestCtx()
	// quoted-x is a transformer that returns (quote x); after re-expansion
	// the result should be the literal datum x.
	quotedX := Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
		return core.SyntaxList(stx.Span, word("quote"), word("x")), nil
	})
	ctx.Bindings.Define("quoted-x", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, TransformerBinding, &SyntaxTransformer{Fn: quotedX})

	stx := core.SyntaxList(core.Span{}, word("quoted-x"))
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("transformer expansion failed: %v", err)
	}
	if out.Node != core.Word("x") {
		t.Fatalf("expected Word(x) from quoted-x, got %#v", out.Node)
	}
}

func TestDoSkipsMacroDefinitionRuntimePlaceholder(t *testing.T) {
	ctx, _ := newTestCtx()
	ctx.Macros = staticMacroRunner{transformer: Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
		return core.SyntaxList(stx.Span, word("quote"), &core.Syntax{Node: core.Atom("expanded")}), nil
	})}
	macroDef := core.SyntaxList(core.Span{}, word("defmacro"), word("m"), word("stx"), word("->"), core.SyntaxList(core.Span{}, word("quote"), word("ignored")))
	stx := core.SyntaxList(core.Span{}, word("do"), macroDef, core.SyntaxList(core.Span{}, word("m")))
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("do expansion failed: %v", err)
	}
	begin, ok := out.Node.(core.Begin)
	if !ok {
		t.Fatalf("do expanded to %T, want Begin", out.Node)
	}
	if len(begin.Body) != 1 {
		t.Fatalf("do body length = %d, want 1 (macro definition skipped)", len(begin.Body))
	}
	if begin.Body[0].Node != core.Atom("expanded") {
		t.Fatalf("remaining body = %#v, want expanded macro result", begin.Body[0].Node)
	}
}

type staticMacroRunner struct{ transformer Transformer }

func (r staticMacroRunner) EvaluateTransformer(body *core.Syntax, ctx *Context) (Transformer, error) {
	return r.transformer, nil
}

// Hygiene positive: a transformer-introduced identifier resolves to a
// binding visible at the empty scope set (the "global" the transformer
// author wanted), even when called from a non-trivial use-site scope.
func TestTransformerSeesItsOwnGlobal(t *testing.T) {
	ctx, _ := newTestCtx()
	helperBinding := ctx.Bindings.Define("helper", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, ValueBinding, "the-helper")
	refHelper := Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
		return word("helper"), nil
	})
	ctx.Bindings.Define("ref-helper", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, TransformerBinding, &SyntaxTransformer{Fn: refHelper})

	sUser := core.NewScope()
	stx := core.AddScope(core.SyntaxList(core.Span{}, word("ref-helper")), core.PhaseRuntime, sUser)
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("expansion failed: %v", err)
	}
	ref := resolvedRef(t, out)
	if ref.ID != helperBinding.ID || ref.Value != helperBinding.Value {
		t.Fatalf("transformer did not see its global: got %#v", out.Node)
	}
}

// Hygiene negative: a transformer-introduced reference to `helper` must NOT
// capture a use-site binding of the same name. After the flip, the introduced
// helper carries the macro-scope (s_macro) only, so a binding at {s_user}
// is NOT a subset of {s_macro} and is correctly excluded.
func TestTransformerDoesNotCaptureUseSiteBinding(t *testing.T) {
	ctx, _ := newTestCtx()
	globalHelper := ctx.Bindings.Define("helper", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, ValueBinding, "global-helper")
	sUser := core.NewScope()
	ctx.Bindings.Define("helper", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}.Add(sUser), ValueBinding, "user-helper")

	refHelper := Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
		return word("helper"), nil
	})
	ctx.Bindings.Define("ref-helper", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, TransformerBinding, &SyntaxTransformer{Fn: refHelper})

	stx := core.AddScope(core.SyntaxList(core.Span{}, word("ref-helper")), core.PhaseRuntime, sUser)
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("expansion failed: %v", err)
	}
	ref := resolvedRef(t, out)
	if ref.ID != globalHelper.ID || ref.Value != globalHelper.Value {
		t.Fatalf("hygiene broken — captured user binding instead of global: %#v", out.Node)
	}
}

// Hygiene round-trip: a use-site identifier passed through an identity
// transformer must retain its use-site scope set, resolving to the
// use-site-shadowing binding rather than the global.
func TestUseSiteIdentifierSurvivesTransformer(t *testing.T) {
	ctx, _ := newTestCtx()
	ctx.Bindings.Define("helper", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, ValueBinding, "global-helper")
	sUser := core.NewScope()
	userHelper := ctx.Bindings.Define("helper", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}.Add(sUser), ValueBinding, "user-helper")

	idMacro := Transformer(func(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
		elems, ok := core.SyntaxListElems(stx)
		if !ok || len(elems) != 2 {
			return nil, errors.New("id: needs one arg")
		}
		return elems[1], nil
	})
	ctx.Bindings.Define("id", core.PhaseRuntime, DefaultSpace, core.ScopeSet{}, TransformerBinding, &SyntaxTransformer{Fn: idMacro})

	// (id helper) at scope {sUser}. helper carries {sUser}; after flip-in +
	// transformer + flip-out, it should still resolve to user-helper.
	stx := core.AddScope(core.SyntaxList(core.Span{}, word("id"), word("helper")), core.PhaseRuntime, sUser)
	out, err := Expand(stx, ctx)
	if err != nil {
		t.Fatalf("expansion failed: %v", err)
	}
	ref := resolvedRef(t, out)
	if ref.ID != userHelper.ID || ref.Value != userHelper.Value {
		t.Fatalf("use-site identifier did not survive flip: %#v", out.Node)
	}
}
