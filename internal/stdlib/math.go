package stdlib

import (
	"fmt"
	"math"

	"ish/internal/core"
)

func asFloat(v core.Value, fnName string) (float64, error) {
	switch v.Kind {
	case core.VFloat:
		return v.Float, nil
	case core.VInt:
		return float64(v.Int), nil
	}
	return 0, fmt.Errorf("%s: expected number, got %s", fnName, v.Inspect())
}

func mathSqrt(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("sqrt: expected 1 argument, got %d", len(args))
	}
	f, err := asFloat(args[0], "sqrt")
	if err != nil {
		return core.Nil, err
	}
	return core.FloatVal(math.Sqrt(f)), nil
}

func mathPow(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("pow: expected 2 arguments, got %d", len(args))
	}
	a, err := asFloat(args[0], "pow")
	if err != nil {
		return core.Nil, err
	}
	b, err := asFloat(args[1], "pow")
	if err != nil {
		return core.Nil, err
	}
	return core.FloatVal(math.Pow(a, b)), nil
}

func mathLog(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("log: expected 1 argument, got %d", len(args))
	}
	f, err := asFloat(args[0], "log")
	if err != nil {
		return core.Nil, err
	}
	return core.FloatVal(math.Log(f)), nil
}

func mathLog2(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("log2: expected 1 argument, got %d", len(args))
	}
	f, err := asFloat(args[0], "log2")
	if err != nil {
		return core.Nil, err
	}
	return core.FloatVal(math.Log2(f)), nil
}

func mathLog10(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("log10: expected 1 argument, got %d", len(args))
	}
	f, err := asFloat(args[0], "log10")
	if err != nil {
		return core.Nil, err
	}
	return core.FloatVal(math.Log10(f)), nil
}

func mathFloor(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("floor: expected 1 argument, got %d", len(args))
	}
	f, err := asFloat(args[0], "floor")
	if err != nil {
		return core.Nil, err
	}
	return core.IntVal(int64(math.Floor(f))), nil
}

func mathCeil(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("ceil: expected 1 argument, got %d", len(args))
	}
	f, err := asFloat(args[0], "ceil")
	if err != nil {
		return core.Nil, err
	}
	return core.IntVal(int64(math.Ceil(f))), nil
}

func mathRound(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("round: expected 1 argument, got %d", len(args))
	}
	f, err := asFloat(args[0], "round")
	if err != nil {
		return core.Nil, err
	}
	return core.IntVal(int64(math.Round(f))), nil
}
