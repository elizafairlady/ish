package core

import (
	"reflect"
	"testing"
)

func TestScopeSetSubset(t *testing.T) {
	a, b, c := NewScope(), NewScope(), NewScope()
	empty := ScopeSet{}
	ab := ScopeSet{}.Add(a).Add(b)
	abc := ScopeSet{}.Add(a).Add(b).Add(c)
	ac := ScopeSet{}.Add(a).Add(c)
	cases := []struct {
		name     string
		s, t     ScopeSet
		expected bool
	}{
		{"empty subset of anything", empty, abc, true},
		{"empty subset of empty", empty, empty, true},
		{"equal sets", ab, ab, true},
		{"proper subset", ab, abc, true},
		{"superset not subset", abc, ab, false},
		{"disjoint overlap", ac, ab, false},
		{"single missing", ab, ac, false},
	}
	for _, c := range cases {
		if got := c.s.Subset(c.t); got != c.expected {
			t.Errorf("%s: %v ⊆ %v = %v, want %v", c.name, c.s, c.t, got, c.expected)
		}
	}
}

func sampleTree() *Syntax {
	w := func(s string) *Syntax { return &Syntax{Node: Word(s)} }
	return SyntaxList(Span{}, w("a"), SyntaxList(Span{}, w("b"), w("c")), w("d"))
}

func collectWordScopes(stx *Syntax, ph Phase, out map[string]ScopeSet) {
	if stx == nil {
		return
	}
	switch n := stx.Node.(type) {
	case Word:
		out[string(n)] = stx.Scopes[ph]
	case SyntaxPair:
		collectWordScopes(n.Head, ph, out)
		collectWordScopes(n.Tail, ph, out)
	}
}

func TestAddScopeReachesAllNodesAndIsImmutable(t *testing.T) {
	tree := sampleTree()
	s := NewScope()
	withScope := AddScope(tree, PhaseRuntime, s)
	got := map[string]ScopeSet{}
	collectWordScopes(withScope, PhaseRuntime, got)
	for _, name := range []string{"a", "b", "c", "d"} {
		if !got[name].Has(s) {
			t.Errorf("scope missing on %q: %v", name, got[name])
		}
	}
	orig := map[string]ScopeSet{}
	collectWordScopes(tree, PhaseRuntime, orig)
	for _, name := range []string{"a", "b", "c", "d"} {
		if orig[name].Has(s) {
			t.Errorf("original tree mutated at %q", name)
		}
	}
}

func TestFlipScopeSelfInverseOnTree(t *testing.T) {
	tree := sampleTree()
	s := NewScope()
	twice := FlipScope(FlipScope(tree, PhaseRuntime, s), PhaseRuntime, s)
	a, b := map[string]ScopeSet{}, map[string]ScopeSet{}
	collectWordScopes(tree, PhaseRuntime, a)
	collectWordScopes(twice, PhaseRuntime, b)
	for k, va := range a {
		if !va.Equal(b[k]) {
			t.Errorf("flip not self-inverse on %q: %v vs %v", k, va, b[k])
		}
	}
}

func TestFlipScopePhaseIsolation(t *testing.T) {
	stx := &Syntax{Node: Word("x")}
	s := NewScope()
	flipped := FlipScope(stx, PhaseExpand, s)
	if flipped.Scopes[PhaseRuntime].Has(s) {
		t.Error("expand-phase flip leaked into runtime phase")
	}
	if !flipped.Scopes[PhaseExpand].Has(s) {
		t.Error("expand-phase flip did not take effect")
	}
}

func TestWalkScopesTraversesExpandedCore(t *testing.T) {
	s := NewScope()
	body := &Syntax{Node: Word("body")}
	app := SyntaxApp(Span{}, &Syntax{Node: Lambda{Clauses: []LambdaClause{{Body: body}}}}, &Syntax{Node: Word("arg")})
	walked := AddScope(app, PhaseRuntime, s)

	appNode, ok := walked.Node.(App)
	if !ok {
		t.Fatalf("walked node = %T, want App", walked.Node)
	}
	lambda, ok := appNode.Callee.Node.(Lambda)
	if !ok || len(lambda.Clauses) != 1 {
		t.Fatalf("callee = %#v, want one-clause Lambda", appNode.Callee.Node)
	}
	if !lambda.Clauses[0].Body.Scopes[PhaseRuntime].Has(s) {
		t.Fatal("scope did not reach lambda body")
	}
	if !appNode.Args[0].Scopes[PhaseRuntime].Has(s) {
		t.Fatal("scope did not reach app arg")
	}
}

func TestContainsExpandedCore(t *testing.T) {
	if ContainsExpandedCore(SyntaxList(Span{}, &Syntax{Node: Word("a")}, &Syntax{Node: Word("b")})) {
		t.Fatal("reader list reported as expanded core")
	}
	if !ContainsExpandedCore(SyntaxApp(Span{}, &Syntax{Node: Word("f")}, &Syntax{Node: Word("x")})) {
		t.Fatal("App not reported as expanded core")
	}
	if !ContainsExpandedCore(&Syntax{Node: SyntaxVector{SyntaxApp(Span{}, &Syntax{Node: Word("f")})}}) {
		t.Fatal("nested App not reported as expanded core")
	}
}

func TestSyntaxContainersAreNotRuntimeDatums(t *testing.T) {
	if _, ok := any(SyntaxVector{}).(Datum); ok {
		t.Fatal("SyntaxVector must not be runtime Datum")
	}
	if _, ok := any(SyntaxTuple{}).(Datum); ok {
		t.Fatal("SyntaxTuple must not be runtime Datum")
	}
	if _, ok := any(SyntaxDict{}).(Datum); ok {
		t.Fatal("SyntaxDict must not be runtime Datum")
	}
}

func TestBoundIdentEqual(t *testing.T) {
	s := NewScope()
	x1 := &Syntax{Node: Word("x"), Scopes: PhaseScopes{}.Add(PhaseRuntime, s)}
	x2 := &Syntax{Node: Word("x"), Scopes: PhaseScopes{}.Add(PhaseRuntime, s)}
	x3 := &Syntax{Node: Word("x"), Scopes: PhaseScopes{}.Add(PhaseRuntime, NewScope())}
	y := &Syntax{Node: Word("y"), Scopes: PhaseScopes{}.Add(PhaseRuntime, s)}
	if !BoundIdentEqual(x1, x2, PhaseRuntime) {
		t.Error("identical bound identifiers not equal")
	}
	if BoundIdentEqual(x1, x3, PhaseRuntime) {
		t.Error("different scopes treated as equal")
	}
	if BoundIdentEqual(x1, y, PhaseRuntime) {
		t.Error("different names treated as equal")
	}
}

func TestSyntaxToDatumRoundTrip(t *testing.T) {
	tree := sampleTree()
	d := SyntaxToDatum(tree)
	stx := DatumToSyntax(nil, d)
	d2 := SyntaxToDatum(stx)
	if !reflect.DeepEqual(d, d2) {
		t.Fatalf("round trip mismatch:\n  %#v\n  %#v", d, d2)
	}
}

func TestSyntaxListElemsProperAndImproper(t *testing.T) {
	a := &Syntax{Node: Word("a")}
	b := &Syntax{Node: Word("b")}
	list := SyntaxList(Span{}, a, b)
	elems, ok := SyntaxListElems(list)
	if !ok || len(elems) != 2 || elems[0] != a || elems[1] != b {
		t.Fatalf("proper list round trip failed: ok=%v elems=%v", ok, elems)
	}
	improper := &Syntax{Node: SyntaxPair{Head: a, Tail: b}}
	if _, ok := SyntaxListElems(improper); ok {
		t.Error("improper list reported as proper")
	}
}
