package stdlib_test

import (
	"testing"

	"ish/internal/core"
	"ish/internal/testutil"
)

func TestPreludeStringRepeat(t *testing.T) {
	cases := []struct {
		script string
		want   string
	}{
		{`result = String.repeat "ab", 3`, "ababab"},
		{`result = String.repeat "x", 0`, ""},
		{`result = String.repeat "", 5`, ""},
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
