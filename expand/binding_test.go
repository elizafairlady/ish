package expand

import (
	"testing"

	"ish/core"
)

// scopes builds a ScopeSet from variadic scope values.
func scopes(ss ...core.Scope) core.ScopeSet {
	out := core.ScopeSet{}
	for _, s := range ss {
		out = out.Add(s)
	}
	return out
}

func TestResolveUnbound(t *testing.T) {
	tbl := NewBindingTable()
	if _, r := tbl.Resolve("x", core.PhaseRuntime, DefaultSpace, scopes()); r != ResolveUnbound {
		t.Fatalf("expected unbound, got %v", r)
	}
}

func TestResolveSimple(t *testing.T) {
	tbl := NewBindingTable()
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(), ValueBinding, "outer-x")
	got, r := tbl.Resolve("x", core.PhaseRuntime, DefaultSpace, scopes())
	if r != ResolveFound || got.Value != "outer-x" {
		t.Fatalf("simple resolve failed: result=%v binding=%v", r, got)
	}
}

// Shadowing: an inner binding with a strict-superset scope set wins.
func TestResolveShadowing(t *testing.T) {
	s1 := core.NewScope()
	tbl := NewBindingTable()
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(), ValueBinding, "outer")
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(s1), ValueBinding, "inner")
	got, r := tbl.Resolve("x", core.PhaseRuntime, DefaultSpace, scopes(s1))
	if r != ResolveFound || got.Value != "inner" {
		t.Fatalf("inner did not shadow outer: result=%v binding=%v", r, got)
	}
}

// Reference outside the inner region resolves to the outer binding.
func TestResolveShadowingOutsideRegion(t *testing.T) {
	s1 := core.NewScope()
	tbl := NewBindingTable()
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(), ValueBinding, "outer")
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(s1), ValueBinding, "inner")
	got, r := tbl.Resolve("x", core.PhaseRuntime, DefaultSpace, scopes())
	if r != ResolveFound || got.Value != "outer" {
		t.Fatalf("reference outside inner region did not resolve to outer: result=%v binding=%v", r, got)
	}
}

// Hygiene: a macro-introduced identifier does not capture a use-site binding.
//
// Setup: top-level x_outer (empty scopes), use-site let binds x_inner at {s1}.
// The use site is at scope {s1}. The expander allocates s_macro, the
// transformer returns syntax x with no scopes, the expander flips s_macro
// through the result — so the introduced x has scope set {s_macro}.
// {s_macro} contains s_macro but not s1, so x_inner ({s1}) is not a candidate.
// Only x_outer ({}) is a candidate. Result: x_outer, not x_inner.
func TestHygieneMacroDoesNotCaptureUseSiteBinding(t *testing.T) {
	s1, sMacro := core.NewScope(), core.NewScope()
	tbl := NewBindingTable()
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(), ValueBinding, "x_outer")
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(s1), ValueBinding, "x_inner")
	introducedX := scopes(sMacro)
	got, r := tbl.Resolve("x", core.PhaseRuntime, DefaultSpace, introducedX)
	if r != ResolveFound || got.Value != "x_outer" {
		t.Fatalf("macro-introduced x captured use-site binding: result=%v binding=%v", r, got)
	}
}

// Hygiene: a use-site identifier passed through a macro still resolves to its
// use-site binding. The use-site x at scope {s1} has s_macro flipped in then
// flipped out, returning to {s1}, where x_inner ({s1}) wins over x_outer ({}).
func TestHygieneUseSiteReferenceSurvivesFlip(t *testing.T) {
	s1, sMacro := core.NewScope(), core.NewScope()
	tbl := NewBindingTable()
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(), ValueBinding, "x_outer")
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(s1), ValueBinding, "x_inner")
	useSiteX := scopes(s1).Add(sMacro).Flip(sMacro)
	got, r := tbl.Resolve("x", core.PhaseRuntime, DefaultSpace, useSiteX)
	if r != ResolveFound || got.Value != "x_inner" {
		t.Fatalf("use-site x lost identity through hygiene flip: result=%v binding=%v", r, got)
	}
}

// Hygiene: macro can reference its lexically-visible globals. helper is bound
// at empty scope set; the transformer returns helper with no scopes; after
// flip, the introduced helper has scope set {s_macro}, and the empty-scope
// helper binding is still in scope ({} ⊆ {s_macro}).
func TestHygieneMacroSeesItsOwnGlobals(t *testing.T) {
	sMacro := core.NewScope()
	tbl := NewBindingTable()
	tbl.Define("helper", core.PhaseRuntime, DefaultSpace, scopes(), ValueBinding, "the-helper")
	got, r := tbl.Resolve("helper", core.PhaseRuntime, DefaultSpace, scopes(sMacro))
	if r != ResolveFound || got.Value != "the-helper" {
		t.Fatalf("macro could not see its global: result=%v binding=%v", r, got)
	}
}

// Ambiguity: two bindings with incomparable scope sets both visible from the
// reference produce an ambiguous result.
func TestResolveAmbiguousIncomparable(t *testing.T) {
	s1, s2 := core.NewScope(), core.NewScope()
	tbl := NewBindingTable()
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(s1), ValueBinding, "first")
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(s2), ValueBinding, "second")
	_, r := tbl.Resolve("x", core.PhaseRuntime, DefaultSpace, scopes(s1, s2))
	if r != ResolveAmbiguous {
		t.Fatalf("expected ambiguous, got %v", r)
	}
}

// Equal-set ambiguity: two distinct bindings sharing the same scope set are
// ambiguous, not last-wins. This catches the CL-style fall-through bug.
func TestResolveAmbiguousEqualSets(t *testing.T) {
	tbl := NewBindingTable()
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(), ValueBinding, "first")
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(), ValueBinding, "second")
	_, r := tbl.Resolve("x", core.PhaseRuntime, DefaultSpace, scopes())
	if r != ResolveAmbiguous {
		t.Fatalf("expected ambiguous on equal scope sets, got %v", r)
	}
}

// Three-candidate dominator: A={s1}, B={s2}, C={s1,s2}. A and B are
// incomparable to each other, but C dominates both. C must win, not Ambiguous.
func TestResolveCommonDominatorOverIncomparablePair(t *testing.T) {
	s1, s2 := core.NewScope(), core.NewScope()
	tbl := NewBindingTable()
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(s1), ValueBinding, "A")
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(s2), ValueBinding, "B")
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(s1, s2), ValueBinding, "C")
	got, r := tbl.Resolve("x", core.PhaseRuntime, DefaultSpace, scopes(s1, s2))
	if r != ResolveFound || got.Value != "C" {
		t.Fatalf("expected C to dominate; got result=%v binding=%v", r, got)
	}
}

// Domain extensions install transformer bindings at their own scope set;
// these must shadow a core-form binding at the empty scope set when seen
// from a reference that includes the extension's scope. This is the
// load-bearing mechanism behind shell- and make-as-package.
func TestTransformerShadowsCoreFormViaScope(t *testing.T) {
	sExt := core.NewScope()
	tbl := NewBindingTable()
	tbl.Define("%-expr", core.PhaseRuntime, DefaultSpace, scopes(), CoreFormBinding, "core-default")
	tbl.Define("%-expr", core.PhaseRuntime, DefaultSpace, scopes(sExt), TransformerBinding, "ext-transformer")
	got, r := tbl.Resolve("%-expr", core.PhaseRuntime, DefaultSpace, scopes(sExt))
	if r != ResolveFound || got.Kind != TransformerBinding || got.Value != "ext-transformer" {
		t.Fatalf("transformer did not shadow core form: result=%v binding=%v", r, got)
	}
	// Outside the extension's region, the core form is still visible.
	got2, r2 := tbl.Resolve("%-expr", core.PhaseRuntime, DefaultSpace, scopes())
	if r2 != ResolveFound || got2.Kind != CoreFormBinding {
		t.Fatalf("core form not visible outside extension: result=%v binding=%v", r2, got2)
	}
}

// Binding spaces are interned names; the core doesn't know which spaces
// exist. A domain package can introduce its own space and bindings in that
// space won't collide with the default space.
func TestResolveBindingSpaceIsolation(t *testing.T) {
	const customSpace BindingSpace = "custom"
	tbl := NewBindingTable()
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(), ValueBinding, "in-default")
	tbl.Define("x", core.PhaseRuntime, customSpace, scopes(), ValueBinding, "in-custom")
	d, _ := tbl.Resolve("x", core.PhaseRuntime, DefaultSpace, scopes())
	c, _ := tbl.Resolve("x", core.PhaseRuntime, customSpace, scopes())
	if d.Value != "in-default" || c.Value != "in-custom" {
		t.Fatalf("space isolation broken: default=%v custom=%v", d.Value, c.Value)
	}
}

func TestResolvePhaseIsolation(t *testing.T) {
	tbl := NewBindingTable()
	tbl.Define("x", core.PhaseRuntime, DefaultSpace, scopes(), ValueBinding, "runtime")
	tbl.Define("x", core.PhaseExpand, DefaultSpace, scopes(), ValueBinding, "expand")
	r, _ := tbl.Resolve("x", core.PhaseRuntime, DefaultSpace, scopes())
	e, _ := tbl.Resolve("x", core.PhaseExpand, DefaultSpace, scopes())
	if r.Value != "runtime" || e.Value != "expand" {
		t.Fatalf("phase isolation broken: runtime=%v expand=%v", r.Value, e.Value)
	}
}

func TestContextIntroduceScope(t *testing.T) {
	tbl := NewBindingTable()
	diag := &Diagnostics{}
	root := NewContext("test", tbl, diag)
	s, child := root.IntroduceScope()
	if root.Scopes[core.PhaseRuntime].Has(s) {
		t.Error("IntroduceScope mutated parent context")
	}
	if !child.Scopes[core.PhaseRuntime].Has(s) {
		t.Error("IntroduceScope did not add scope to child")
	}
	if child.Phase != root.Phase || child.Kind != root.Kind || child.Bindings != root.Bindings {
		t.Error("Sub failed to inherit parent fields")
	}
}

func TestContextSubOverrides(t *testing.T) {
	const customSpace BindingSpace = "custom"
	root := NewContext("test", NewBindingTable(), &Diagnostics{})
	child := root.Sub(WithKind(PatternCtx), WithSpace(customSpace))
	if child.Kind != PatternCtx || child.Space != customSpace {
		t.Errorf("Sub did not apply overrides: kind=%v space=%v", child.Kind, child.Space)
	}
	if root.Kind != PackageCtx || root.Space != DefaultSpace {
		t.Error("Sub mutated parent")
	}
}
