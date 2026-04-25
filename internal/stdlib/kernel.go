package stdlib

import (
	"strconv"

	"ish/internal/value"
)

func kernelNatives() map[string]func([]value.Value) (value.Value, error) {
	return map[string]func([]value.Value) (value.Value, error){
		"hd":     kernelHd,
		"tl":     kernelTl,
		"length": kernelLength,
		"typeof": kernelTypeof,
		"to_string": func(args []value.Value) (value.Value, error) {
			return value.StringVal(arg(args, 0).ToStr()), nil
		},
		"to_integer": func(args []value.Value) (value.Value, error) {
			a := arg(args, 0)
			if a.Kind == value.VInt {
				return a, nil
			}
			n, err := strconv.ParseInt(a.ToStr(), 10, 64)
			if err != nil {
				return value.Nil, nil
			}
			return value.IntVal(n), nil
		},
		"to_float": func(args []value.Value) (value.Value, error) {
			a := arg(args, 0)
			if a.Kind == value.VFloat {
				return a, nil
			}
			if a.Kind == value.VInt {
				return value.FloatVal(float64(a.Int())), nil
			}
			f, err := strconv.ParseFloat(a.ToStr(), 64)
			if err != nil {
				return value.Nil, nil
			}
			return value.FloatVal(f), nil
		},
		"inspect": func(args []value.Value) (value.Value, error) {
			return value.StringVal(arg(args, 0).ToStr()), nil
		},
		"apply": func(args []value.Value) (value.Value, error) {
			if len(args) < 2 || args[0].Kind != value.VFn || args[1].Kind != value.VList {
				return value.Nil, nil
			}
			return invoke(args[0].Fn(), args[1].Elems())
		},
	}
}

func kernelTypeof(args []value.Value) (value.Value, error) {
	a := arg(args, 0)
	switch a.Kind {
	case value.VNil:
		return value.AtomVal("nil"), nil
	case value.VInt:
		return value.AtomVal("integer"), nil
	case value.VFloat:
		return value.AtomVal("float"), nil
	case value.VString:
		return value.AtomVal("string"), nil
	case value.VAtom:
		return value.AtomVal("atom"), nil
	case value.VTuple:
		return value.AtomVal("tuple"), nil
	case value.VList:
		return value.AtomVal("list"), nil
	case value.VMap:
		return value.AtomVal("map"), nil
	case value.VFn:
		return value.AtomVal("fn"), nil
	case value.VPid:
		return value.AtomVal("pid"), nil
	}
	return value.AtomVal("unknown"), nil
}

func kernelHd(args []value.Value) (value.Value, error) {
	if len(args) < 1 || args[0].Kind != value.VList {
		return value.Nil, nil
	}
	elems := args[0].Elems()
	if len(elems) == 0 {
		return value.Nil, nil
	}
	return elems[0], nil
}

func kernelTl(args []value.Value) (value.Value, error) {
	if len(args) < 1 || args[0].Kind != value.VList {
		return value.Nil, nil
	}
	elems := args[0].Elems()
	if len(elems) <= 1 {
		return value.ListVal(), nil
	}
	return value.ListVal(elems[1:]...), nil
}

func kernelLength(args []value.Value) (value.Value, error) {
	if len(args) < 1 {
		return value.IntVal(0), nil
	}
	switch args[0].Kind {
	case value.VList:
		return value.IntVal(int64(len(args[0].Elems()))), nil
	case value.VString:
		return value.IntVal(int64(len(args[0].Str()))), nil
	case value.VMap:
		m := args[0].Map()
		if m != nil {
			return value.IntVal(int64(len(m.Keys))), nil
		}
	case value.VTuple:
		return value.IntVal(int64(len(args[0].Elems()))), nil
	}
	return value.IntVal(0), nil
}
