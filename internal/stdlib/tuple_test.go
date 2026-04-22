package stdlib_test

import (
	"testing"

	"ish/internal/core"
	"ish/internal/testutil"
)

func TestTupleToList(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Tuple.to_list {1, 2, 3}`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(1), core.IntVal(2), core.IntVal(3))
	if !got.Equal(want) {
		t.Errorf("to_list = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestTupleFromList(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Tuple.from_list [1, 2, 3]`)
	got, _ := env.Get("result")
	want := core.TupleVal(core.IntVal(1), core.IntVal(2), core.IntVal(3))
	if !got.Equal(want) {
		t.Errorf("from_list = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestTupleAt(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Tuple.at({10, 20, 30}, 1)`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(20)) {
		t.Errorf("at = %s, want 20", got.Inspect())
	}
}

func TestTupleSize(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Tuple.size {1, 2, 3}`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(3)) {
		t.Errorf("size = %s, want 3", got.Inspect())
	}
}

func TestTupleReduce(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Tuple.reduce({1, 2, 3}, 0, \acc, x -> acc + x)`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(6)) {
		t.Errorf("reduce = %s, want 6", got.Inspect())
	}
}

func TestEnumOnTuple(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
mapped = Enum.map({1, 2, 3}, \x -> x * 10)
filtered = Enum.filter({1, 2, 3, 4}, \x -> x > 2)
summed = Enum.sum {1, 2, 3}
`)
	type tc struct {
		name string
		want core.Value
	}
	for _, c := range []tc{
		{"mapped", core.ListVal(core.IntVal(10), core.IntVal(20), core.IntVal(30))},
		{"filtered", core.ListVal(core.IntVal(3), core.IntVal(4))},
		{"summed", core.IntVal(6)},
	} {
		got, _ := env.Get(c.name)
		if !got.Equal(c.want) {
			t.Errorf("%s = %s, want %s", c.name, got.Inspect(), c.want.Inspect())
		}
	}
}
