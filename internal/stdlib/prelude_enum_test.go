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
