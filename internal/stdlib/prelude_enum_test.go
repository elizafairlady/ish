package stdlib_test

import (
	"testing"

	"ish/internal/core"
	"ish/internal/testutil"
)

func TestPreludeEnumList(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
mapped  = [1, 2, 3] |> Enum.map \x -> x * 10
filtered = [1, 2, 3, 4] |> Enum.filter \x -> x > 2
reduced = [1, 2, 3] |> Enum.reduce(0, \a, x -> a + x)
counted = Enum.count [1, 2, 3, 4, 5]
anyed   = [1, 2, 3] |> Enum.any \x -> x > 2
alled   = [1, 2, 3] |> Enum.all \x -> x > 0
found   = [1, 2, 3] |> Enum.find \x -> x > 1
`)
	type tc struct {
		name string
		want core.Value
	}
	for _, c := range []tc{
		{"mapped", core.ListVal(core.IntVal(10), core.IntVal(20), core.IntVal(30))},
		{"filtered", core.ListVal(core.IntVal(3), core.IntVal(4))},
		{"reduced", core.IntVal(6)},
		{"counted", core.IntVal(5)},
		{"anyed", core.True},
		{"alled", core.True},
		{"found", core.IntVal(2)},
	} {
		got, _ := env.Get(c.name)
		if !got.Equal(c.want) {
			t.Errorf("%s = %s, want %s", c.name, got.Inspect(), c.want.Inspect())
		}
	}
}

func TestPreludeEnumEach(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = [1, 2, 3] |> Enum.each \x -> x`)
	got, _ := env.Get("result")
	if !got.Equal(core.AtomVal("ok")) {
		t.Errorf("each = %s, want :ok", got.Inspect())
	}
}

func TestPreludeEnumWithIndex(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Enum.with_index ["a", "b", "c"]`)
	got, _ := env.Get("result")
	want := core.ListVal(
		core.TupleVal(core.IntVal(0), core.StringVal("a")),
		core.TupleVal(core.IntVal(1), core.StringVal("b")),
		core.TupleVal(core.IntVal(2), core.StringVal("c")),
	)
	if !got.Equal(want) {
		t.Errorf("with_index = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestPreludeEnumZip(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Enum.zip([1, 2, 3], ["a", "b", "c"])`)
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

func TestPreludeEnumMapOnMap(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Enum.map(%{a: 1, b: 2}, \{_, v} -> v * 10)`)
	got, _ := env.Get("result")
	// Map iteration order is insertion order
	want := core.ListVal(core.IntVal(10), core.IntVal(20))
	if !got.Equal(want) {
		t.Errorf("map on map = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestPreludeEnumMapOnTuple(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Enum.map({1, 2, 3}, \x -> x * 10)`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(10), core.IntVal(20), core.IntVal(30))
	if !got.Equal(want) {
		t.Errorf("map on tuple = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestPreludeEnumReduceOnMap(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Enum.reduce(%{a: 1, b: 2}, 0, \acc, {_, v} -> acc + v)`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(3)) {
		t.Errorf("reduce on map = %s, want 3", got.Inspect())
	}
}

func TestPreludeEnumReduceOnTuple(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Enum.reduce({1, 2, 3}, 0, \acc, x -> acc + x)`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(6)) {
		t.Errorf("reduce on tuple = %s, want 6", got.Inspect())
	}
}

func TestPreludeEnumReject(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Enum.reject([1, 2, 3, 4], \x -> x > 2)`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(1), core.IntVal(2))
	if !got.Equal(want) {
		t.Errorf("reject = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestPreludeEnumSumProduct(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
s = Enum.sum [1, 2, 3]
p = Enum.product [1, 2, 3, 4]
`)
	if got, _ := env.Get("s"); !got.Equal(core.IntVal(6)) {
		t.Errorf("sum = %s, want 6", got.Inspect())
	}
	if got, _ := env.Get("p"); !got.Equal(core.IntVal(24)) {
		t.Errorf("product = %s, want 24", got.Inspect())
	}
}

func TestPreludeEnumMember(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
yes = Enum.member([1, 2, 3], 2)
no = Enum.member([1, 2, 3], 5)
`)
	if got, _ := env.Get("yes"); !got.Equal(core.True) {
		t.Errorf("member(2) = %s, want true", got.Inspect())
	}
	if got, _ := env.Get("no"); !got.Equal(core.False) {
		t.Errorf("member(5) = %s, want false", got.Inspect())
	}
}

func TestPreludeEnumToTuple(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Enum.to_tuple [1, 2, 3]`)
	got, _ := env.Get("result")
	want := core.TupleVal(core.IntVal(1), core.IntVal(2), core.IntVal(3))
	if !got.Equal(want) {
		t.Errorf("to_tuple = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestPreludeEnumToList(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
from_list = Enum.to_list [1, 2, 3]
from_tuple = Enum.to_list {4, 5, 6}
`)
	if got, _ := env.Get("from_list"); !got.Equal(core.ListVal(core.IntVal(1), core.IntVal(2), core.IntVal(3))) {
		t.Errorf("to_list(list) = %s", got.Inspect())
	}
	if got, _ := env.Get("from_tuple"); !got.Equal(core.ListVal(core.IntVal(4), core.IntVal(5), core.IntVal(6))) {
		t.Errorf("to_list(tuple) = %s", got.Inspect())
	}
}
