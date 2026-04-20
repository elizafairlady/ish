package stdlib_test

import (
	"testing"

	"ish/internal/core"
	"ish/internal/testutil"
)

func TestPreludeMapFromPairs(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Map.from_pairs [{"a", 1}, {"b", 2}]`)
	got, _ := env.Get("result")
	if got.Kind != core.VMap {
		t.Fatalf("got %s", got.Inspect())
	}
	if v, _ := got.Map.Get("a"); !v.Equal(core.IntVal(1)) {
		t.Errorf("a = %s", v.Inspect())
	}
	if v, _ := got.Map.Get("b"); !v.Equal(core.IntVal(2)) {
		t.Errorf("b = %s", v.Inspect())
	}
}

func TestPreludeMapFetch(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
hit = Map.fetch %{a: 1}, "a"
miss = Map.fetch %{a: 1}, "b"
`)
	want := core.TupleVal(core.AtomVal("ok"), core.IntVal(1))
	if got, _ := env.Get("hit"); !got.Equal(want) {
		t.Errorf("hit = %s", got.Inspect())
	}
	if got, _ := env.Get("miss"); !got.Equal(core.AtomVal("error")) {
		t.Errorf("miss = %s", got.Inspect())
	}
}

func TestPreludeMapUpdate(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Map.update %{a: 1, b: 2}, "a", \v -> v * 100`)
	got, _ := env.Get("result")
	if v, _ := got.Map.Get("a"); !v.Equal(core.IntVal(100)) {
		t.Errorf("a = %s", v.Inspect())
	}
	if v, _ := got.Map.Get("b"); !v.Equal(core.IntVal(2)) {
		t.Errorf("b = %s", v.Inspect())
	}
}
