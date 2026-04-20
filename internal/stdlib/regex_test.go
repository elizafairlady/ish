package stdlib_test

import (
	"testing"

	"ish/internal/core"
	"ish/internal/testutil"
)

func TestRegexMatch(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
r1 = Regex.match "hello world", "w[aeiou]rld"
r2 = Regex.match "hello", "xyz"
`)
	if r, _ := env.Get("r1"); !r.Equal(core.True) {
		t.Errorf("r1 = %s, want :true", r.Inspect())
	}
	if r, _ := env.Get("r2"); !r.Equal(core.False) {
		t.Errorf("r2 = %s, want :false", r.Inspect())
	}
}

func TestRegexScan(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Regex.scan "a1 b22 c333", "[0-9]+"`)
	got, _ := env.Get("result")
	want := core.ListVal(core.StringVal("1"), core.StringVal("22"), core.StringVal("333"))
	if !got.Equal(want) {
		t.Errorf("scan = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestRegexReplace(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
r1 = Regex.replace "aaa", "a", "b"
r2 = Regex.replace_all "aaa", "a", "b"
`)
	if r, _ := env.Get("r1"); r.Kind != core.VString || r.Str != "baa" {
		t.Errorf("replace = %s, want baa", r.Inspect())
	}
	if r, _ := env.Get("r2"); r.Kind != core.VString || r.Str != "bbb" {
		t.Errorf("replace_all = %s, want bbb", r.Inspect())
	}
}

func TestRegexSplit(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Regex.split "one,two;three", "[,;]"`)
	got, _ := env.Get("result")
	want := core.ListVal(core.StringVal("one"), core.StringVal("two"), core.StringVal("three"))
	if !got.Equal(want) {
		t.Errorf("split = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestRegexInvalidPattern(t *testing.T) {
	env := testutil.TestEnv()
	err := evalScriptErr(t, env, `result = Regex.match "x", "("`)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}
