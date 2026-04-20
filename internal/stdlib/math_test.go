package stdlib_test

import (
	"math"
	"testing"

	"ish/internal/core"
	"ish/internal/testutil"
)

func TestMathFunctions(t *testing.T) {
	cases := []struct {
		script string
		want   core.Value
	}{
		{`result = Math.sqrt 16.0`, core.FloatVal(4.0)},
		{`result = Math.sqrt 2.0`, core.FloatVal(math.Sqrt(2.0))},
		{`result = Math.pow 2.0, 10.0`, core.FloatVal(1024.0)},
		{`result = Math.log 2.718281828459045`, core.FloatVal(1.0)},
		{`result = Math.log2 8.0`, core.FloatVal(3.0)},
		{`result = Math.log10 1000.0`, core.FloatVal(3.0)},
		{`result = Math.floor 3.7`, core.IntVal(3)},
		{`result = Math.ceil 3.2`, core.IntVal(4)},
		{`result = Math.round 3.5`, core.IntVal(4)},
		{`result = Math.round 3.4`, core.IntVal(3)},
	}
	for _, c := range cases {
		env := testutil.TestEnv()
		evalScript(t, env, c.script)
		got, _ := env.Get("result")
		if !valuesCloseEnough(got, c.want) {
			t.Errorf("%s = %s, want %s", c.script, got.Inspect(), c.want.Inspect())
		}
	}
}

func valuesCloseEnough(a, b core.Value) bool {
	if a.Kind == core.VFloat && b.Kind == core.VFloat {
		return math.Abs(a.Float-b.Float) < 1e-9
	}
	return a.Equal(b)
}
