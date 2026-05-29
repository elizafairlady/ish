package eval

import (
	"math"
	"reflect"
	"testing"

	"ish/core"
	"ish/expand"
)

func callFnErr(t *testing.T, name string, args ...Value) (Value, error) {
	t.Helper()
	tbl := expand.NewBindingTable()
	expand.InstallKernel(tbl)
	InstallRuntimeKernel(tbl)
	b, r := tbl.Resolve(core.Word(name), core.PhaseRuntime, expand.DefaultSpace, core.ScopeSet{})
	if r != expand.ResolveFound {
		t.Fatalf("kernel binding %s not found", name)
	}
	return apply(b.Value, args, NewEnv())
}

// Every arithmetic primitive type-checks its operands and reports fixed-width
// overflow as an error — never a panic and never a silently wrong value.
func TestArithmeticTypeChecksAndOverflow(t *testing.T) {
	if _, err := callFnErr(t, "sub", core.Atom("a"), core.Int(2)); err == nil {
		t.Error("sub :a 2 must error, not panic")
	}
	if _, err := callFnErr(t, "add", core.Int(math.MaxInt64), core.Int(1)); err == nil {
		t.Error("add overflow must error")
	}
	if _, err := callFnErr(t, "sub", core.Int(math.MinInt64), core.Int(1)); err == nil {
		t.Error("sub overflow must error")
	}
	if _, err := callFnErr(t, "mul", core.Int(math.MaxInt64), core.Int(2)); err == nil {
		t.Error("mul overflow must error")
	}
	if _, err := callFnErr(t, "mul", core.Int(math.MinInt64), core.Int(-1)); err == nil {
		t.Error("mul MinInt64 * -1 must error")
	}
	// Non-overflowing results are still exact.
	if v := callFn(t, "mul", core.Int(1000000), core.Int(1000000)); v != core.Int(1000000000000) {
		t.Errorf("mul = %#v, want 1e12", v)
	}
}

// mod is floored (result takes the divisor's sign), not Go's truncated remainder.
func TestModFloored(t *testing.T) {
	cases := []struct {
		a, b, want int64
	}{
		{-7, 3, 2}, {7, -3, -2}, {7, 3, 1}, {-7, -3, -1},
	}
	for _, c := range cases {
		if v := callFn(t, "mod", core.Int(c.a), core.Int(c.b)); v != core.Int(c.want) {
			t.Errorf("mod %d %d = %#v, want %d", c.a, c.b, v, c.want)
		}
	}
}

func callFn(t *testing.T, name string, args ...Value) Value {
	t.Helper()
	tbl := expand.NewBindingTable()
	expand.InstallKernel(tbl)
	InstallRuntimeKernel(tbl)
	b, r := tbl.Resolve(core.Word(name), core.PhaseRuntime, expand.DefaultSpace, core.ScopeSet{})
	if r != expand.ResolveFound {
		t.Fatalf("kernel binding %s not found", name)
	}
	if !isCallable(b.Value) {
		t.Fatalf("kernel %s is not callable: %T", name, b.Value)
	}
	v, err := apply(b.Value, args, NewEnv())
	if err != nil {
		t.Fatalf("%s call: %v", name, err)
	}
	return v
}

func TestKernelArithmetic(t *testing.T) {
	cases := []struct {
		name string
		args []Value
		want Value
	}{
		{"add", []Value{core.Int(1), core.Int(2), core.Int(3)}, core.Int(6)},
		{"sub", []Value{core.Int(10), core.Int(3), core.Int(2)}, core.Int(5)},
		{"sub", []Value{core.Int(5)}, core.Int(-5)},
		{"mul", []Value{core.Int(2), core.Int(3), core.Int(4)}, core.Int(24)},
		{"add", []Value{core.Int(1), core.Float(2.5)}, core.Float(3.5)},
		{"neg", []Value{core.Int(7)}, core.Int(-7)},
		{"mod", []Value{core.Int(10), core.Int(3)}, core.Int(1)},
	}
	for _, c := range cases {
		got := callFn(t, c.name, c.args...)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s%v = %v, want %v", c.name, c.args, got, c.want)
		}
	}
}

func TestKernelComparison(t *testing.T) {
	if callFn(t, "lt?", core.Int(1), core.Int(2)) != core.Atom("true") {
		t.Error("lt? 1 2")
	}
	if callFn(t, "gte?", core.Int(2), core.Int(2)) != core.Atom("true") {
		t.Error("gte? 2 2")
	}
	if callFn(t, "lt?", core.Int(3), core.Int(2)) != core.Atom("false") {
		t.Error("lt? 3 2")
	}
	if callFn(t, "not", core.Atom("false")) != core.Atom("true") {
		t.Error("not :false")
	}
	if callFn(t, "not", core.Atom("true")) != core.Atom("false") {
		t.Error("not :true")
	}
}

func TestKernelCollections(t *testing.T) {
	v := callFn(t, "tuple", core.Int(1), core.Int(2), core.Int(3))
	if !reflect.DeepEqual(v, core.Tuple{core.Int(1), core.Int(2), core.Int(3)}) {
		t.Errorf("tuple: %v", v)
	}
	v = callFn(t, "vector", core.Int(1), core.Int(2))
	if !reflect.DeepEqual(v, core.Vector{core.Int(1), core.Int(2)}) {
		t.Errorf("vector: %v", v)
	}
	d := callFn(t, "dict", core.Atom("a"), core.Int(1), core.Atom("b"), core.Int(2))
	got := callFn(t, "dict-get", d, core.Atom("a"))
	if got != core.Int(1) {
		t.Errorf("dict-get a: %v", got)
	}
	missing := callFn(t, "dict-get", d, core.Atom("c"))
	if missing != (core.Nil{}) {
		t.Errorf("dict-get c (missing): %v", missing)
	}
	d2 := callFn(t, "dict-put", d, core.Atom("a"), core.Int(99))
	got = callFn(t, "dict-get", d2, core.Atom("a"))
	if got != core.Int(99) {
		t.Errorf("dict-put replace: %v", got)
	}
}

func TestBindingKindAndRefToSyntax(t *testing.T) {
	ref := &expand.Binding{Name: "greet", ID: 7, Kind: expand.ValueBinding, Value: core.Atom("hello")}
	if got := callFn(t, "binding-kind", ref); got != core.Atom("value") {
		t.Errorf("binding-kind value = %#v, want :value", got)
	}
	tref := &expand.Binding{Name: "m", ID: 8, Kind: expand.TransformerBinding}
	if got := callFn(t, "binding-kind", tref); got != core.Atom("transformer") {
		t.Errorf("binding-kind transformer = %#v, want :transformer", got)
	}
	// ref->syntax reconstructs a reference syntax carrying the binding's identity
	// and value, so a protocol can emit a binding it resolved in another space.
	out := callFn(t, "ref->syntax", ref)
	stx, ok := out.(*core.Syntax)
	if !ok {
		t.Fatalf("ref->syntax = %T, want *core.Syntax", out)
	}
	res, ok := stx.Node.(core.Resolved)
	if !ok || res.Name != "greet" || res.ID != 7 || res.Value != core.Atom("hello") {
		t.Fatalf("ref->syntax node = %#v, want Resolved{greet 7 :hello}", stx.Node)
	}
}

func TestKernelApply(t *testing.T) {
	argList := core.Pair{Head: core.Int(1), Tail: core.Pair{Head: core.Int(2), Tail: core.Nil{}}}
	tbl := expand.NewBindingTable()
	expand.InstallKernel(tbl)
	InstallRuntimeKernel(tbl)
	b, _ := tbl.Resolve("cons", core.PhaseRuntime, expand.DefaultSpace, core.ScopeSet{})
	v := callFn(t, "apply", b.Value, argList)
	expected := core.Pair{Head: core.Int(1), Tail: core.Int(2)}
	if !reflect.DeepEqual(v, expected) {
		t.Errorf("apply cons: %v", v)
	}
}

func TestDottedParts(t *testing.T) {
	// (%-expr objects . frobnicate 1 2) -> {objects frobnicate (1 2) ()}
	access := core.SyntaxList(core.Span{},
		word("%-expr"), word("objects"), word("."), word("frobnicate"),
		&core.Syntax{Node: core.Int(1)}, &core.Syntax{Node: core.Int(2)})
	got := callFn(t, "dotted-parts", access)
	tup, ok := got.(core.Tuple)
	if !ok || len(tup) != 4 {
		t.Fatalf("dotted-parts shape = %#v, want 4-tuple", got)
	}
	if w, ok := tup[0].(*core.Syntax).Node.(core.Word); !ok || w != "objects" {
		t.Errorf("base = %#v, want objects", tup[0])
	}
	if w, ok := tup[1].(*core.Syntax).Node.(core.Word); !ok || w != "frobnicate" {
		t.Errorf("member = %#v, want frobnicate", tup[1])
	}
	assertIntListIs(t, "args", tup[2], 1, 2)
	if _, ok := tup[3].(core.Nil); !ok {
		t.Errorf("chain = %#v, want () for a single-segment access", tup[3])
	}
}

// A chained access a.b.c must split into base=a, member=b, args=(), chain=(. c)
// — the `.` must never leak into member b's argument list.
func TestDottedPartsChainedAccessDoesNotLeakDot(t *testing.T) {
	access := core.SyntaxList(core.Span{},
		word("%-expr"), word("a"), word("."), word("b"), word("."), word("c"))
	tup, ok := callFn(t, "dotted-parts", access).(core.Tuple)
	if !ok || len(tup) != 4 {
		t.Fatalf("dotted-parts shape = %#v, want 4-tuple", tup)
	}
	if _, ok := tup[2].(core.Nil); !ok {
		t.Errorf("args = %#v, want () (no args before the chain)", tup[2])
	}
	// chain is (. c)
	p, ok := tup[3].(core.Pair)
	if !ok {
		t.Fatalf("chain = %#v, want a pair (. c)", tup[3])
	}
	if w, ok := p.Head.(*core.Syntax).Node.(core.Word); !ok || w != "." {
		t.Errorf("chain head = %#v, want the `.` token", p.Head)
	}
}

func assertIntListIs(t *testing.T, name string, list core.Datum, want ...int64) {
	t.Helper()
	cur := list
	for i, w := range want {
		p, ok := cur.(core.Pair)
		if !ok {
			t.Fatalf("%s[%d]: not a pair: %#v", name, i, cur)
		}
		if got := p.Head.(*core.Syntax).Node; got != core.Int(w) {
			t.Errorf("%s[%d] = %#v, want %d", name, i, got, w)
		}
		cur = p.Tail
	}
	if _, ok := cur.(core.Nil); !ok {
		t.Errorf("%s tail = %#v, want nil", name, cur)
	}
}

func TestDottedPartsDeclinesNonDotted(t *testing.T) {
	// A plain application is not a dotted access.
	app := core.SyntaxList(core.Span{},
		word("%-expr"), word("f"), &core.Syntax{Node: core.Int(1)})
	if got := callFn(t, "dotted-parts", app); got != core.Atom("false") {
		t.Errorf("dotted-parts of application = %#v, want false", got)
	}
}

func TestKernelKind(t *testing.T) {
	cases := map[string]Value{
		"int":    core.Int(1),
		"float":  core.Float(1.0),
		"atom":   core.Atom("x"),
		"string": core.String("x"),
		"tuple":  core.Tuple{core.Int(1)},
	}
	for want, v := range cases {
		got := callFn(t, "kind", v)
		if got != core.Atom(want) {
			t.Errorf("kind %T = %v, want %v", v, got, want)
		}
	}
	// The kind of the nil value is the nil value itself (`:nil`), since `:nil`
	// is the canonical spelling of nil rather than a distinct atom.
	if got := callFn(t, "kind", core.Nil{}); got != (core.Nil{}) {
		t.Errorf("kind nil = %#v, want core.Nil{}", got)
	}
}
