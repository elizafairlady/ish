package stdlib_test

import (
	"testing"

	"ish/internal/core"
	"ish/internal/testutil"
)

func TestPreludeListZip(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = List.zip [1, 2, 3], ["a", "b", "c"]`)
	got, _ := env.Get("result")
	want := core.ListVal(
		core.TupleVal(core.IntVal(1), core.StringVal("a")),
		core.TupleVal(core.IntVal(2), core.StringVal("b")),
		core.TupleVal(core.IntVal(3), core.StringVal("c")),
	)
	if !got.Equal(want) {
		t.Errorf("zip = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestPreludeListFlatten(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = List.flatten [[1, 2], [3, [4, 5]], [6]]`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(1), core.IntVal(2), core.IntVal(3), core.IntVal(4), core.IntVal(5), core.IntVal(6))
	if !got.Equal(want) {
		t.Errorf("flatten = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestPreludeListTakeDrop(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
t = List.take [1, 2, 3, 4, 5], 3
d = List.drop [1, 2, 3, 4, 5], 3
`)
	if got, _ := env.Get("t"); !got.Equal(core.ListVal(core.IntVal(1), core.IntVal(2), core.IntVal(3))) {
		t.Errorf("take = %s", got.Inspect())
	}
	if got, _ := env.Get("d"); !got.Equal(core.ListVal(core.IntVal(4), core.IntVal(5))) {
		t.Errorf("drop = %s", got.Inspect())
	}
}

func TestPreludeListChunk(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = List.chunk [1, 2, 3, 4, 5], 2`)
	got, _ := env.Get("result")
	want := core.ListVal(
		core.ListVal(core.IntVal(1), core.IntVal(2)),
		core.ListVal(core.IntVal(3), core.IntVal(4)),
		core.ListVal(core.IntVal(5)),
	)
	if !got.Equal(want) {
		t.Errorf("chunk = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestPreludeListUniq(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = List.uniq [1, 2, 1, 3, 2, 4]`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(1), core.IntVal(2), core.IntVal(3), core.IntVal(4))
	if !got.Equal(want) {
		t.Errorf("uniq = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestPreludeListSumProduct(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
s = List.sum [1, 2, 3, 4]
p = List.product [1, 2, 3, 4]
`)
	if got, _ := env.Get("s"); !got.Equal(core.IntVal(10)) {
		t.Errorf("sum = %s", got.Inspect())
	}
	if got, _ := env.Get("p"); !got.Equal(core.IntVal(24)) {
		t.Errorf("product = %s", got.Inspect())
	}
}

func TestPreludeListFlatMap(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = List.flat_map [1, 2, 3], \x -> [x, x]`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(1), core.IntVal(1), core.IntVal(2), core.IntVal(2), core.IntVal(3), core.IntVal(3))
	if !got.Equal(want) {
		t.Errorf("flat_map = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestPreludeListIntersperse(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = List.intersperse [1, 2, 3], 0`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(1), core.IntVal(0), core.IntVal(2), core.IntVal(0), core.IntVal(3))
	if !got.Equal(want) {
		t.Errorf("intersperse = %s, want %s", got.Inspect(), want.Inspect())
	}
}
