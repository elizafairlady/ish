package stdlib

import (
	"fmt"

	"ish/internal/core"
)

func tupleReduce(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 3 {
		return core.Nil, fmt.Errorf("reduce: expected 3 arguments, got %d", len(args))
	}
	t := args[0]
	if t.Kind != core.VTuple {
		return core.Nil, fmt.Errorf("reduce: expected tuple, got %s", t.Inspect())
	}
	acc := args[1]
	fn := args[2]
	if fn.Kind != core.VFn || fn.GetFn() == nil {
		return core.Nil, fmt.Errorf("reduce: third argument must be a function, got %s", fn.Inspect())
	}
	callFn := scope.GetCtx().CallFn
	if callFn == nil {
		return core.Nil, fmt.Errorf("reduce: CallFn not set")
	}
	for _, elem := range t.GetElems() {
		var err error
		acc, err = callFn(fn.GetFn(), []core.Value{acc, elem}, scope)
		if err != nil {
			return core.Nil, fmt.Errorf("reduce: %w", err)
		}
	}
	return acc, nil
}

func tupleToList(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("to_list: expected 1 argument, got %d", len(args))
	}
	if args[0].Kind != core.VTuple {
		return core.Nil, fmt.Errorf("to_list: expected tuple, got %s", args[0].Inspect())
	}
	return core.ListVal(args[0].GetElems()...), nil
}

func tupleFromList(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("from_list: expected 1 argument, got %d", len(args))
	}
	if args[0].Kind != core.VList {
		return core.Nil, fmt.Errorf("from_list: expected list, got %s", args[0].Inspect())
	}
	return core.TupleVal(args[0].GetElems()...), nil
}

func tupleAt(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("at: expected 2 arguments, got %d", len(args))
	}
	if args[0].Kind != core.VTuple {
		return core.Nil, fmt.Errorf("at: expected tuple, got %s", args[0].Inspect())
	}
	if args[1].Kind != core.VInt {
		return core.Nil, fmt.Errorf("at: index must be integer, got %s", args[1].Inspect())
	}
	idx := args[1].GetInt()
	elems := args[0].GetElems()
	if idx < 0 || idx >= int64(len(elems)) {
		return core.Nil, fmt.Errorf("at: index %d out of bounds (size %d)", idx, len(elems))
	}
	return elems[idx], nil
}

func tupleSize(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("size: expected 1 argument, got %d", len(args))
	}
	if args[0].Kind != core.VTuple {
		return core.Nil, fmt.Errorf("size: expected tuple, got %s", args[0].Inspect())
	}
	return core.IntVal(int64(len(args[0].GetElems()))), nil
}
