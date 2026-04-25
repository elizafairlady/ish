package stdlib

import "ish/internal/value"

func tupleModule() *value.OrdMap {
	return makeModule(map[string]func([]value.Value) (value.Value, error){
		"at": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VTuple || arg(args, 1).Kind != value.VInt {
				return value.Nil, nil
			}
			elems := args[0].Elems()
			idx := int(args[1].Int())
			if idx < 0 || idx >= len(elems) {
				return value.Nil, nil
			}
			return elems[idx], nil
		},
		"size": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VTuple {
				return value.IntVal(0), nil
			}
			return value.IntVal(int64(len(args[0].Elems()))), nil
		},
		"to_list": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VTuple {
				return value.ListVal(), nil
			}
			return value.ListVal(args[0].Elems()...), nil
		},
	})
}
