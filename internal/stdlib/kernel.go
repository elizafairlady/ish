package stdlib

import (
	"fmt"
	"strconv"

	"ish/internal/core"
)

func boolAtom(b bool) core.Value {
	if b {
		return core.True
	}
	return core.False
}

// Type predicates

func kernelIsInteger(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("is_integer: expected 1 argument, got %d", len(args))
	}
	return boolAtom(args[0].Kind == core.VInt), nil
}

func kernelIsFloat(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("is_float: expected 1 argument, got %d", len(args))
	}
	return boolAtom(args[0].Kind == core.VFloat), nil
}

func kernelIsString(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("is_string: expected 1 argument, got %d", len(args))
	}
	return boolAtom(args[0].Kind == core.VString), nil
}

func kernelIsAtom(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("is_atom: expected 1 argument, got %d", len(args))
	}
	return boolAtom(args[0].Kind == core.VAtom), nil
}

func kernelIsList(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("is_list: expected 1 argument, got %d", len(args))
	}
	return boolAtom(args[0].Kind == core.VList), nil
}

func kernelIsMap(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("is_map: expected 1 argument, got %d", len(args))
	}
	return boolAtom(args[0].Kind == core.VMap), nil
}

func kernelIsNil(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("is_nil: expected 1 argument, got %d", len(args))
	}
	v := args[0]
	isNil := v.Kind == core.VNil || (v.Kind == core.VAtom && v.Str == "nil")
	return boolAtom(isNil), nil
}

func kernelIsTuple(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("is_tuple: expected 1 argument, got %d", len(args))
	}
	return boolAtom(args[0].Kind == core.VTuple), nil
}

func kernelIsPid(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("is_pid: expected 1 argument, got %d", len(args))
	}
	return boolAtom(args[0].Kind == core.VPid), nil
}

func kernelIsFn(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("is_fn: expected 1 argument, got %d", len(args))
	}
	return boolAtom(args[0].Kind == core.VFn), nil
}

// Type conversions

func kernelToString(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("to_string: expected 1 argument, got %d", len(args))
	}
	return core.StringVal(args[0].String()), nil
}

func kernelToInteger(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("to_integer: expected 1 argument, got %d", len(args))
	}
	switch args[0].Kind {
	case core.VInt:
		return args[0], nil
	case core.VFloat:
		return core.IntVal(int64(args[0].GetFloat())), nil
	case core.VString:
		n, err := strconv.ParseInt(args[0].Str, 10, 64)
		if err != nil {
			f, ferr := strconv.ParseFloat(args[0].Str, 64)
			if ferr != nil {
				return core.Nil, fmt.Errorf("to_integer: cannot parse %q", args[0].Str)
			}
			return core.IntVal(int64(f)), nil
		}
		return core.IntVal(n), nil
	default:
		return core.Nil, fmt.Errorf("to_integer: cannot convert %s", args[0].Inspect())
	}
}

func kernelToFloat(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("to_float: expected 1 argument, got %d", len(args))
	}
	switch args[0].Kind {
	case core.VFloat:
		return args[0], nil
	case core.VInt:
		return core.FloatVal(float64(args[0].GetInt())), nil
	case core.VString:
		f, err := strconv.ParseFloat(args[0].Str, 64)
		if err != nil {
			return core.Nil, fmt.Errorf("to_float: cannot parse %q", args[0].Str)
		}
		return core.FloatVal(f), nil
	default:
		return core.Nil, fmt.Errorf("to_float: cannot convert %s", args[0].Inspect())
	}
}

// Inspect and apply

func kernelInspect(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("inspect: expected 1 argument, got %d", len(args))
	}
	return core.StringVal(args[0].Inspect()), nil
}

func kernelApply(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("apply: expected 2 arguments, got %d", len(args))
	}
	fn := args[0]
	if fn.Kind != core.VFn || fn.GetFn() == nil {
		return core.Nil, fmt.Errorf("apply: first argument must be a function, got %s", fn.Inspect())
	}
	argList := args[1]
	if argList.Kind != core.VList {
		return core.Nil, fmt.Errorf("apply: second argument must be a list, got %s", argList.Inspect())
	}
	if scope.GetCtx().CallFn == nil {
		return core.Nil, fmt.Errorf("apply: CallFn not set")
	}
	return scope.GetCtx().CallFn(fn.GetFn(), argList.GetElems(), scope)
}

// Numeric utilities

func numericValue(v core.Value) (float64, bool, bool) {
	switch v.Kind {
	case core.VInt:
		return float64(v.GetInt()), false, true
	case core.VFloat:
		return v.GetFloat(), true, true
	}
	return 0, false, false
}

func kernelAbs(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("abs: expected 1 argument, got %d", len(args))
	}
	switch args[0].Kind {
	case core.VInt:
		n := args[0].GetInt()
		if n < 0 {
			n = -n
		}
		return core.IntVal(n), nil
	case core.VFloat:
		f := args[0].GetFloat()
		if f < 0 {
			f = -f
		}
		return core.FloatVal(f), nil
	}
	return core.Nil, fmt.Errorf("abs: expected number, got %s", args[0].Inspect())
}

func kernelMin(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("min: expected 2 arguments, got %d", len(args))
	}
	a, aFloat, aOk := numericValue(args[0])
	b, bFloat, bOk := numericValue(args[1])
	if !aOk || !bOk {
		return core.Nil, fmt.Errorf("min: expected numbers, got %s and %s", args[0].Inspect(), args[1].Inspect())
	}
	pick := args[0]
	if b < a {
		pick = args[1]
	}
	if aFloat || bFloat {
		if pick.Kind == core.VInt {
			return core.FloatVal(float64(pick.GetInt())), nil
		}
	}
	return pick, nil
}

func kernelMax(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("max: expected 2 arguments, got %d", len(args))
	}
	a, aFloat, aOk := numericValue(args[0])
	b, bFloat, bOk := numericValue(args[1])
	if !aOk || !bOk {
		return core.Nil, fmt.Errorf("max: expected numbers, got %s and %s", args[0].Inspect(), args[1].Inspect())
	}
	pick := args[0]
	if b > a {
		pick = args[1]
	}
	if aFloat || bFloat {
		if pick.Kind == core.VInt {
			return core.FloatVal(float64(pick.GetInt())), nil
		}
	}
	return pick, nil
}
