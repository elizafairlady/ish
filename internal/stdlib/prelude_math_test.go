package stdlib_test

import (
	"testing"

	"ish/internal/core"
	"ish/internal/testutil"
)

func TestPreludeMathClamp(t *testing.T) {
	cases := []struct {
		script string
		want   int64
	}{
		{`result = Math.clamp 5, 0, 10`, 5},
		{`result = Math.clamp 15, 0, 10`, 10},
		{`result = Math.clamp (0 - 5), 0, 10`, 0},
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
