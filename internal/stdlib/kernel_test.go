package stdlib_test

import (
	"testing"

	"ish/internal/core"
	"ish/internal/testutil"
)

func TestKernelPredicates(t *testing.T) {
	cases := []struct {
		script string
		want   core.Value
	}{
		{`result = Kernel.is_integer 5`, core.True},
		{`result = Kernel.is_integer "hi"`, core.False},
		{`result = Kernel.is_float 3.14`, core.True},
		{`result = Kernel.is_float 5`, core.False},
		{`result = Kernel.is_string "hi"`, core.True},
		{`result = Kernel.is_string 5`, core.False},
		{`result = Kernel.is_atom :ok`, core.True},
		{`result = Kernel.is_atom "ok"`, core.False},
		{`result = Kernel.is_list [1, 2]`, core.True},
		{`result = Kernel.is_list {1, 2}`, core.False},
		{`result = Kernel.is_map %{a: 1}`, core.True},
		{`result = Kernel.is_map [1, 2]`, core.False},
		{`result = Kernel.is_nil nil`, core.True},
		{`result = Kernel.is_nil 0`, core.False},
		{`result = Kernel.is_tuple {1, 2}`, core.True},
		{`result = Kernel.is_tuple [1, 2]`, core.False},
		{`result = Kernel.is_fn (fn do x -> x end)`, core.True},
		{`result = Kernel.is_fn 5`, core.False},
	}
	for _, c := range cases {
		env := testutil.TestEnv()
		evalScript(t, env, c.script)
		got, _ := env.Get("result")
		if !got.Equal(c.want) {
			t.Errorf("%s = %s, want %s", c.script, got.Inspect(), c.want.Inspect())
		}
	}
}

func TestKernelConversions(t *testing.T) {
	cases := []struct {
		script string
		want   core.Value
	}{
		{`result = Kernel.to_string 42`, core.StringVal("42")},
		{`result = Kernel.to_string 3.14`, core.StringVal("3.14")},
		{`result = Kernel.to_string :ok`, core.StringVal(":ok")},
		{`result = Kernel.to_string "hi"`, core.StringVal("hi")},
		{`result = Kernel.to_integer "42"`, core.IntVal(42)},
		{`result = Kernel.to_integer 42`, core.IntVal(42)},
		{`result = Kernel.to_integer 3.9`, core.IntVal(3)},
		{`result = Kernel.to_float "3.14"`, core.FloatVal(3.14)},
		{`result = Kernel.to_float 42`, core.FloatVal(42.0)},
	}
	for _, c := range cases {
		env := testutil.TestEnv()
		evalScript(t, env, c.script)
		got, _ := env.Get("result")
		if !got.Equal(c.want) {
			t.Errorf("%s = %s, want %s", c.script, got.Inspect(), c.want.Inspect())
		}
	}
}

func TestKernelToIntegerError(t *testing.T) {
	env := testutil.TestEnv()
	err := evalScriptErr(t, env, `result = Kernel.to_integer "not a number"`)
	if err == nil {
		t.Fatal("expected error for invalid integer string")
	}
}

func TestKernelInspect(t *testing.T) {
	cases := []struct {
		script string
		want   string
	}{
		{`result = Kernel.inspect "hi"`, `"hi"`},
		{`result = Kernel.inspect 42`, `42`},
		{`result = Kernel.inspect [1, 2]`, `[1, 2]`},
		{`result = Kernel.inspect :ok`, `:ok`},
	}
	for _, c := range cases {
		env := testutil.TestEnv()
		evalScript(t, env, c.script)
		got, _ := env.Get("result")
		if got.Kind != core.VString || got.Str != c.want {
			t.Errorf("%s = %s, want %q", c.script, got.Inspect(), c.want)
		}
	}
}

func TestKernelApply(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
f = fn do
  x, y -> x + y
end
result = Kernel.apply(f, [3, 4])
`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(7)) {
		t.Errorf("apply = %s, want 7", got.Inspect())
	}
}

func TestKernelHd(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Kernel.hd [1, 2, 3]`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(1)) {
		t.Errorf("hd = %s, want 1", got.Inspect())
	}
}

func TestKernelTl(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Kernel.tl [1, 2, 3]`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(2), core.IntVal(3))
	if !got.Equal(want) {
		t.Errorf("tl = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestKernelLength(t *testing.T) {
	cases := []struct {
		script string
		want   int64
	}{
		{`result = Kernel.length [1, 2, 3]`, 3},
		{`result = Kernel.length "hello"`, 5},
		{`result = Kernel.length %{a: 1, b: 2}`, 2},
		{`result = Kernel.length {1, 2, 3, 4}`, 4},
	}
	for _, c := range cases {
		env := testutil.TestEnv()
		evalScript(t, env, c.script)
		got, _ := env.Get("result")
		if !got.Equal(core.IntVal(c.want)) {
			t.Errorf("%s = %s, want %d", c.script, got.Inspect(), c.want)
		}
	}
}

func TestKernelAbs(t *testing.T) {
	cases := []struct {
		script string
		want   core.Value
	}{
		{`result = Kernel.abs (0 - 5)`, core.IntVal(5)},
		{`result = Kernel.abs 5`, core.IntVal(5)},
		{`result = Kernel.abs (0.0 - 3.14)`, core.FloatVal(3.14)},
		{`result = Kernel.abs 3.14`, core.FloatVal(3.14)},
	}
	for _, c := range cases {
		env := testutil.TestEnv()
		evalScript(t, env, c.script)
		got, _ := env.Get("result")
		if !got.Equal(c.want) {
			t.Errorf("%s = %s, want %s", c.script, got.Inspect(), c.want.Inspect())
		}
	}
}

func TestKernelMinMax(t *testing.T) {
	cases := []struct {
		script string
		want   core.Value
	}{
		{`result = Kernel.min(3, 5)`, core.IntVal(3)},
		{`result = Kernel.min(5, 3)`, core.IntVal(3)},
		{`result = Kernel.max(3, 5)`, core.IntVal(5)},
		{`result = Kernel.max(5, 3)`, core.IntVal(5)},
		{`result = Kernel.min(1.5, 2.5)`, core.FloatVal(1.5)},
		{`result = Kernel.max(1, 2.5)`, core.FloatVal(2.5)},
	}
	for _, c := range cases {
		env := testutil.TestEnv()
		evalScript(t, env, c.script)
		got, _ := env.Get("result")
		if !got.Equal(c.want) {
			t.Errorf("%s = %s, want %s", c.script, got.Inspect(), c.want.Inspect())
		}
	}
}

func TestKernelAutoImport(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
r1 = is_integer 5
r2 = is_list [1]
r3 = hd [9, 8, 7]
r4 = length "hello"
r5 = abs (0 - 3)
r6 = min(3, 7)
r7 = max(3, 7)
`)
	cases := []struct {
		name string
		want core.Value
	}{
		{"r1", core.True},
		{"r2", core.True},
		{"r3", core.IntVal(9)},
		{"r4", core.IntVal(5)},
		{"r5", core.IntVal(3)},
		{"r6", core.IntVal(3)},
		{"r7", core.IntVal(7)},
	}
	for _, c := range cases {
		got, _ := env.Get(c.name)
		if !got.Equal(c.want) {
			t.Errorf("%s = %s, want %s", c.name, got.Inspect(), c.want.Inspect())
		}
	}
}

func TestKernelPredicatesInGuards(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
fn describe x when is_integer x do "int" end
fn describe x when is_string x do "str" end
fn describe _ do "other" end

r1 = describe 5
r2 = describe "hi"
r3 = describe :ok
`)
	cases := []struct {
		name string
		want string
	}{
		{"r1", "int"},
		{"r2", "str"},
		{"r3", "other"},
	}
	for _, c := range cases {
		got, _ := env.Get(c.name)
		if got.Kind != core.VString || got.Str != c.want {
			t.Errorf("%s = %s, want %q", c.name, got.Inspect(), c.want)
		}
	}
}
