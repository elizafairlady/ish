package stdlib

import (
	"sort"

	"ish/internal/value"
)

func listModule() *value.OrdMap {
	return makeModule(map[string]func([]value.Value) (value.Value, error){
		"map":        listMap,
		"filter":     listFilter,
		"reduce":     listReduce,
		"append":     listAppend,
		"concat":     listConcat,
		"at":         listAt,
		"sort":       listSort,
		"reverse":    listReverse,
		"range":      listRange,
		"each":       listEach,
		"any":        listAny,
		"all":        listAll,
		"find":       listFind,
		"with_index": listWithIndex,
		"first": func(args []value.Value) (value.Value, error) {
			if len(args) < 1 || args[0].Kind != value.VList { return value.Nil, nil }
			elems := args[0].Elems()
			if len(elems) == 0 { return value.Nil, nil }
			return elems[0], nil
		},
		"last": func(args []value.Value) (value.Value, error) {
			if len(args) < 1 || args[0].Kind != value.VList { return value.Nil, nil }
			elems := args[0].Elems()
			if len(elems) == 0 { return value.Nil, nil }
			return elems[len(elems)-1], nil
		},
	})
}

func listMap(args []value.Value) (value.Value, error) {
	if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn {
		return value.Nil, nil
	}
	elems := args[0].Elems()
	result := make([]value.Value, len(elems))
	for i, elem := range elems {
		v, err := invoke(args[1].Fn(), []value.Value{elem})
		if err != nil {
			return value.Nil, err
		}
		result[i] = v
	}
	return value.ListVal(result...), nil
}

func listFilter(args []value.Value) (value.Value, error) {
	if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn {
		return value.Nil, nil
	}
	var result []value.Value
	for _, elem := range args[0].Elems() {
		v, err := invoke(args[1].Fn(), []value.Value{elem})
		if err != nil {
			return value.Nil, err
		}
		if v.Truthy() {
			result = append(result, elem)
		}
	}
	return value.ListVal(result...), nil
}

func listReduce(args []value.Value) (value.Value, error) {
	if len(args) < 3 || args[0].Kind != value.VList || args[2].Kind != value.VFn {
		return value.Nil, nil
	}
	acc := args[1]
	fn := args[2].Fn()
	for _, elem := range args[0].Elems() {
		v, err := invoke(fn, []value.Value{acc, elem})
		if err != nil {
			return value.Nil, err
		}
		acc = v
	}
	return acc, nil
}

func listAppend(args []value.Value) (value.Value, error) {
	if len(args) < 2 || args[0].Kind != value.VList {
		return value.Nil, nil
	}
	return value.ListVal(append(args[0].Elems(), args[1])...), nil
}

func listConcat(args []value.Value) (value.Value, error) {
	if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VList {
		return value.Nil, nil
	}
	return value.ListVal(append(args[0].Elems(), args[1].Elems()...)...), nil
}

func listAt(args []value.Value) (value.Value, error) {
	if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VInt {
		return value.Nil, nil
	}
	elems := args[0].Elems()
	idx := int(args[1].Int())
	if idx < 0 || idx >= len(elems) {
		return value.Nil, nil
	}
	return elems[idx], nil
}

func listSort(args []value.Value) (value.Value, error) {
	if len(args) < 1 || args[0].Kind != value.VList {
		return value.Nil, nil
	}
	elems := append([]value.Value{}, args[0].Elems()...)
	sort.Slice(elems, func(i, j int) bool {
		if elems[i].Kind == value.VInt && elems[j].Kind == value.VInt {
			return elems[i].Int() < elems[j].Int()
		}
		return elems[i].ToStr() < elems[j].ToStr()
	})
	return value.ListVal(elems...), nil
}

func listReverse(args []value.Value) (value.Value, error) {
	if len(args) < 1 || args[0].Kind != value.VList {
		return value.Nil, nil
	}
	elems := args[0].Elems()
	rev := make([]value.Value, len(elems))
	for i, e := range elems {
		rev[len(elems)-1-i] = e
	}
	return value.ListVal(rev...), nil
}

func listRange(args []value.Value) (value.Value, error) {
	if len(args) < 2 || args[0].Kind != value.VInt || args[1].Kind != value.VInt {
		return value.ListVal(), nil
	}
	from, to := args[0].Int(), args[1].Int()
	elems := make([]value.Value, 0, to-from+1)
	for i := from; i <= to; i++ {
		elems = append(elems, value.IntVal(i))
	}
	return value.ListVal(elems...), nil
}

func listEach(args []value.Value) (value.Value, error) {
	if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn {
		return value.Nil, nil
	}
	for _, elem := range args[0].Elems() {
		if _, err := invoke(args[1].Fn(), []value.Value{elem}); err != nil {
			return value.Nil, err
		}
	}
	return value.OkVal(value.Nil), nil
}

func listAny(args []value.Value) (value.Value, error) {
	if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn {
		return value.False, nil
	}
	for _, elem := range args[0].Elems() {
		v, err := invoke(args[1].Fn(), []value.Value{elem})
		if err != nil {
			return value.Nil, err
		}
		if v.Truthy() {
			return value.True, nil
		}
	}
	return value.False, nil
}

func listAll(args []value.Value) (value.Value, error) {
	if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn {
		return value.True, nil
	}
	for _, elem := range args[0].Elems() {
		v, err := invoke(args[1].Fn(), []value.Value{elem})
		if err != nil {
			return value.Nil, err
		}
		if !v.Truthy() {
			return value.False, nil
		}
	}
	return value.True, nil
}

func listFind(args []value.Value) (value.Value, error) {
	if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn {
		return value.Nil, nil
	}
	for _, elem := range args[0].Elems() {
		v, err := invoke(args[1].Fn(), []value.Value{elem})
		if err != nil {
			return value.Nil, err
		}
		if v.Truthy() {
			return elem, nil
		}
	}
	return value.Nil, nil
}

func listWithIndex(args []value.Value) (value.Value, error) {
	if len(args) < 1 || args[0].Kind != value.VList {
		return value.Nil, nil
	}
	elems := args[0].Elems()
	result := make([]value.Value, len(elems))
	for i, e := range elems {
		result[i] = value.TupleVal(value.IntVal(int64(i)), e)
	}
	return value.ListVal(result...), nil
}
