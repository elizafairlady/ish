package stdlib

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"ish/internal/core"
)

// hd list -> first element (error on empty)
func stdlibHd(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("hd: expected 1 argument, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("hd: expected list, got %s", list.Inspect())
	}
	if len(list.Elems) == 0 {
		return core.Nil, fmt.Errorf("hd: empty list")
	}
	return list.Elems[0], nil
}

func stdlibTl(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("tl: expected 1 argument, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("tl: expected list, got %s", list.Inspect())
	}
	if len(list.Elems) == 0 {
		return core.Nil, fmt.Errorf("tl: empty list")
	}
	return core.ListVal(list.Elems[1:]...), nil
}

func stdlibLength(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("length: expected 1 argument, got %d", len(args))
	}
	v := args[0]
	switch v.Kind {
	case core.VList:
		return core.IntVal(int64(len(v.Elems))), nil
	case core.VTuple:
		return core.IntVal(int64(len(v.Elems))), nil
	case core.VString:
		return core.IntVal(int64(len(v.Str))), nil
	case core.VMap:
		if v.Map == nil {
			return core.IntVal(0), nil
		}
		return core.IntVal(int64(len(v.Map.Keys))), nil
	default:
		return core.Nil, fmt.Errorf("length: unsupported type %s", v.Inspect())
	}
}

func stdlibAppend(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("append: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("append: first argument must be a list, got %s", list.Inspect())
	}
	newElems := make([]core.Value, len(list.Elems)+1)
	copy(newElems, list.Elems)
	newElems[len(list.Elems)] = args[1]
	return core.ListVal(newElems...), nil
}

func stdlibConcat(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("concat: expected 2 arguments, got %d", len(args))
	}
	a, b := args[0], args[1]
	if a.Kind != core.VList {
		return core.Nil, fmt.Errorf("concat: first argument must be a list, got %s", a.Inspect())
	}
	if b.Kind != core.VList {
		return core.Nil, fmt.Errorf("concat: second argument must be a list, got %s", b.Inspect())
	}
	newElems := make([]core.Value, 0, len(a.Elems)+len(b.Elems))
	newElems = append(newElems, a.Elems...)
	newElems = append(newElems, b.Elems...)
	return core.ListVal(newElems...), nil
}

func stdlibMap(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("map: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("map: first argument must be a list, got %s", list.Inspect())
	}
	fn := args[1]
	if fn.Kind != core.VFn || fn.Fn == nil {
		return core.Nil, fmt.Errorf("map: second argument must be a function, got %s", fn.Inspect())
	}
	if env.CallFn == nil {
		return core.Nil, fmt.Errorf("map: CallFn not set")
	}
	result := make([]core.Value, len(list.Elems))
	for i, elem := range list.Elems {
		v, err := env.CallFn(fn.Fn, []core.Value{elem}, env)
		if err != nil {
			return core.Nil, fmt.Errorf("map: %w", err)
		}
		result[i] = v
	}
	return core.ListVal(result...), nil
}

func stdlibFilter(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("filter: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("filter: first argument must be a list, got %s", list.Inspect())
	}
	fn := args[1]
	if fn.Kind != core.VFn || fn.Fn == nil {
		return core.Nil, fmt.Errorf("filter: second argument must be a function, got %s", fn.Inspect())
	}
	if env.CallFn == nil {
		return core.Nil, fmt.Errorf("filter: CallFn not set")
	}
	var result []core.Value
	for _, elem := range list.Elems {
		v, err := env.CallFn(fn.Fn, []core.Value{elem}, env)
		if err != nil {
			return core.Nil, fmt.Errorf("filter: %w", err)
		}
		if v.Truthy() {
			result = append(result, elem)
		}
	}
	return core.ListVal(result...), nil
}

func stdlibReduce(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 3 {
		return core.Nil, fmt.Errorf("reduce: expected 3 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("reduce: first argument must be a list, got %s", list.Inspect())
	}
	acc := args[1]
	fn := args[2]
	if fn.Kind != core.VFn || fn.Fn == nil {
		return core.Nil, fmt.Errorf("reduce: third argument must be a function, got %s", fn.Inspect())
	}
	if env.CallFn == nil {
		return core.Nil, fmt.Errorf("reduce: CallFn not set")
	}
	for _, elem := range list.Elems {
		var err error
		acc, err = env.CallFn(fn.Fn, []core.Value{acc, elem}, env)
		if err != nil {
			return core.Nil, fmt.Errorf("reduce: %w", err)
		}
	}
	return acc, nil
}

func stdlibRange(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("range: expected 2 arguments, got %d", len(args))
	}
	if args[0].Kind != core.VInt {
		return core.Nil, fmt.Errorf("range: start must be an integer, got %s", args[0].Inspect())
	}
	if args[1].Kind != core.VInt {
		return core.Nil, fmt.Errorf("range: stop must be an integer, got %s", args[1].Inspect())
	}
	start, stop := args[0].Int, args[1].Int
	if start >= stop {
		return core.ListVal(), nil
	}
	const maxRangeSize = 10_000_000
	if stop-start > maxRangeSize {
		return core.Nil, fmt.Errorf("range: size %d exceeds maximum %d", stop-start, maxRangeSize)
	}
	elems := make([]core.Value, 0, stop-start)
	for i := start; i < stop; i++ {
		elems = append(elems, core.IntVal(i))
	}
	return core.ListVal(elems...), nil
}

func stdlibAt(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("at: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("at: first argument must be a list, got %s", list.Inspect())
	}
	if args[1].Kind != core.VInt {
		return core.Nil, fmt.Errorf("at: index must be an integer, got %s", args[1].Inspect())
	}
	idx := args[1].Int
	if idx < 0 || idx >= int64(len(list.Elems)) {
		return core.Nil, fmt.Errorf("at: index %d out of bounds (list length %d)", idx, len(list.Elems))
	}
	return list.Elems[idx], nil
}

// String functions

func stdlibSplit(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("split: expected 2 arguments, got %d", len(args))
	}
	parts := strings.Split(args[0].ToStr(), args[1].ToStr())
	elems := make([]core.Value, len(parts))
	for i, p := range parts {
		elems[i] = core.StringVal(p)
	}
	return core.ListVal(elems...), nil
}

func stdlibJoin(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("join: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("join: first argument must be a list, got %s", list.Inspect())
	}
	delim := args[1].ToStr()
	strs := make([]string, len(list.Elems))
	for i, elem := range list.Elems {
		strs[i] = elem.ToStr()
	}
	return core.StringVal(strings.Join(strs, delim)), nil
}

func stdlibTrim(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("trim: expected 1 argument, got %d", len(args))
	}
	return core.StringVal(strings.TrimSpace(args[0].ToStr())), nil
}

func stdlibUpcase(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("upcase: expected 1 argument, got %d", len(args))
	}
	return core.StringVal(strings.ToUpper(args[0].ToStr())), nil
}

func stdlibDowncase(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("downcase: expected 1 argument, got %d", len(args))
	}
	return core.StringVal(strings.ToLower(args[0].ToStr())), nil
}

func stdlibReplace(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 3 {
		return core.Nil, fmt.Errorf("replace: expected 3 arguments, got %d", len(args))
	}
	return core.StringVal(strings.Replace(args[0].ToStr(), args[1].ToStr(), args[2].ToStr(), 1)), nil
}

func stdlibReplaceAll(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 3 {
		return core.Nil, fmt.Errorf("replace_all: expected 3 arguments, got %d", len(args))
	}
	return core.StringVal(strings.ReplaceAll(args[0].ToStr(), args[1].ToStr(), args[2].ToStr())), nil
}

func stdlibStartsWith(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("starts_with: expected 2 arguments, got %d", len(args))
	}
	if strings.HasPrefix(args[0].ToStr(), args[1].ToStr()) {
		return core.AtomVal("true"), nil
	}
	return core.AtomVal("false"), nil
}

func stdlibEndsWith(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("ends_with: expected 2 arguments, got %d", len(args))
	}
	if strings.HasSuffix(args[0].ToStr(), args[1].ToStr()) {
		return core.AtomVal("true"), nil
	}
	return core.AtomVal("false"), nil
}

func stdlibContains(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("contains: expected 2 arguments, got %d", len(args))
	}
	if strings.Contains(args[0].ToStr(), args[1].ToStr()) {
		return core.AtomVal("true"), nil
	}
	return core.AtomVal("false"), nil
}

func stdlibSubstring(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 3 {
		return core.Nil, fmt.Errorf("substring: expected 3 arguments, got %d", len(args))
	}
	s := args[0].ToStr()
	if args[1].Kind != core.VInt {
		return core.Nil, fmt.Errorf("substring: start must be an integer, got %s", args[1].Inspect())
	}
	if args[2].Kind != core.VInt {
		return core.Nil, fmt.Errorf("substring: length must be an integer, got %s", args[2].Inspect())
	}
	start := int(args[1].Int)
	length := int(args[2].Int)
	if start < 0 {
		start = 0
	}
	if start >= len(s) {
		return core.StringVal(""), nil
	}
	end := start + length
	if end > len(s) {
		end = len(s)
	}
	return core.StringVal(s[start:end]), nil
}

func stdlibIndexOf(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("index_of: expected 2 arguments, got %d", len(args))
	}
	idx := strings.Index(args[0].ToStr(), args[1].ToStr())
	return core.IntVal(int64(idx)), nil
}

// Map operations

func stdlibPut(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 3 {
		return core.Nil, fmt.Errorf("put: expected 3 arguments, got %d", len(args))
	}
	if args[0].Kind != core.VMap {
		return core.Nil, fmt.Errorf("put: first argument must be a map, got %s", args[0].Inspect())
	}
	m := core.NewOrdMap()
	if args[0].Map != nil {
		for _, k := range args[0].Map.Keys {
			m.Set(k, args[0].Map.Vals[k])
		}
	}
	m.Set(args[1].ToStr(), args[2])
	return core.Value{Kind: core.VMap, Map: m}, nil
}

func stdlibDelete(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("delete: expected 2 arguments, got %d", len(args))
	}
	if args[0].Kind != core.VMap {
		return core.Nil, fmt.Errorf("delete: first argument must be a map, got %s", args[0].Inspect())
	}
	key := args[1].ToStr()
	m := core.NewOrdMap()
	if args[0].Map != nil {
		for _, k := range args[0].Map.Keys {
			if k != key {
				m.Set(k, args[0].Map.Vals[k])
			}
		}
	}
	return core.Value{Kind: core.VMap, Map: m}, nil
}

func stdlibMerge(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("merge: expected 2 arguments, got %d", len(args))
	}
	if args[0].Kind != core.VMap {
		return core.Nil, fmt.Errorf("merge: first argument must be a map, got %s", args[0].Inspect())
	}
	if args[1].Kind != core.VMap {
		return core.Nil, fmt.Errorf("merge: second argument must be a map, got %s", args[1].Inspect())
	}
	m := core.NewOrdMap()
	if args[0].Map != nil {
		for _, k := range args[0].Map.Keys {
			m.Set(k, args[0].Map.Vals[k])
		}
	}
	if args[1].Map != nil {
		for _, k := range args[1].Map.Keys {
			m.Set(k, args[1].Map.Vals[k])
		}
	}
	return core.Value{Kind: core.VMap, Map: m}, nil
}

func stdlibKeys(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("keys: expected 1 argument, got %d", len(args))
	}
	if args[0].Kind != core.VMap {
		return core.Nil, fmt.Errorf("keys: argument must be a map, got %s", args[0].Inspect())
	}
	if args[0].Map == nil {
		return core.ListVal(), nil
	}
	elems := make([]core.Value, len(args[0].Map.Keys))
	for i, k := range args[0].Map.Keys {
		elems[i] = core.StringVal(k)
	}
	return core.ListVal(elems...), nil
}

func stdlibValues(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("values: expected 1 argument, got %d", len(args))
	}
	if args[0].Kind != core.VMap {
		return core.Nil, fmt.Errorf("values: argument must be a map, got %s", args[0].Inspect())
	}
	if args[0].Map == nil {
		return core.ListVal(), nil
	}
	elems := make([]core.Value, len(args[0].Map.Keys))
	for i, k := range args[0].Map.Keys {
		elems[i] = args[0].Map.Vals[k]
	}
	return core.ListVal(elems...), nil
}

func stdlibHasKey(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("has_key: expected 2 arguments, got %d", len(args))
	}
	if args[0].Kind != core.VMap {
		return core.Nil, fmt.Errorf("has_key: first argument must be a map, got %s", args[0].Inspect())
	}
	key := args[1].ToStr()
	if args[0].Map != nil {
		if _, ok := args[0].Map.Get(key); ok {
			return core.True, nil
		}
	}
	return core.False, nil
}

// get map, key -> value (dynamic map access)
func stdlibGet(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("get: expected 2 arguments, got %d", len(args))
	}
	if args[0].Kind != core.VMap {
		return core.Nil, fmt.Errorf("get: first argument must be a map, got %s", args[0].Inspect())
	}
	if args[0].Map == nil {
		return core.Nil, nil
	}
	val, ok := args[0].Map.Get(args[1].ToStr())
	if !ok {
		return core.Nil, nil
	}
	return val, nil
}

// each list, fn -> nil (apply fn for side effects, discard results)
func stdlibEach(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("each: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("each: first argument must be a list, got %s", list.Inspect())
	}
	fn := args[1]
	if fn.Kind != core.VFn || fn.Fn == nil {
		return core.Nil, fmt.Errorf("each: second argument must be a function, got %s", fn.Inspect())
	}
	if env.CallFn == nil {
		return core.Nil, fmt.Errorf("each: CallFn not set")
	}
	for _, elem := range list.Elems {
		_, err := env.CallFn(fn.Fn, []core.Value{elem}, env)
		if err != nil {
			return core.Nil, fmt.Errorf("each: %w", err)
		}
	}
	return core.Nil, nil
}

// sort list -> sorted list
func stdlibSort(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("sort: expected 1 argument, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("sort: expected list, got %s", list.Inspect())
	}
	sorted := make([]core.Value, len(list.Elems))
	copy(sorted, list.Elems)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Kind == core.VInt && b.Kind == core.VInt {
			return a.Int < b.Int
		}
		return a.ToStr() < b.ToStr()
	})
	return core.ListVal(sorted...), nil
}

// reverse list -> reversed list
func stdlibReverse(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("reverse: expected 1 argument, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("reverse: expected list, got %s", list.Inspect())
	}
	reversed := make([]core.Value, len(list.Elems))
	for i, v := range list.Elems {
		reversed[len(list.Elems)-1-i] = v
	}
	return core.ListVal(reversed...), nil
}

// any list, fn -> true if fn returns truthy for any element
func stdlibAny(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("any: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("any: first argument must be a list, got %s", list.Inspect())
	}
	fn := args[1]
	if fn.Kind != core.VFn || fn.Fn == nil {
		return core.Nil, fmt.Errorf("any: second argument must be a function, got %s", fn.Inspect())
	}
	if env.CallFn == nil {
		return core.Nil, fmt.Errorf("any: CallFn not set")
	}
	for _, elem := range list.Elems {
		v, err := env.CallFn(fn.Fn, []core.Value{elem}, env)
		if err != nil {
			return core.Nil, fmt.Errorf("any: %w", err)
		}
		if v.Truthy() {
			return core.True, nil
		}
	}
	return core.False, nil
}

// all list, fn -> true if fn returns truthy for all elements
func stdlibAll(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("all: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("all: first argument must be a list, got %s", list.Inspect())
	}
	fn := args[1]
	if fn.Kind != core.VFn || fn.Fn == nil {
		return core.Nil, fmt.Errorf("all: second argument must be a function, got %s", fn.Inspect())
	}
	if env.CallFn == nil {
		return core.Nil, fmt.Errorf("all: CallFn not set")
	}
	for _, elem := range list.Elems {
		v, err := env.CallFn(fn.Fn, []core.Value{elem}, env)
		if err != nil {
			return core.Nil, fmt.Errorf("all: %w", err)
		}
		if !v.Truthy() {
			return core.False, nil
		}
	}
	return core.True, nil
}

// find list, fn -> first element where fn is truthy, or nil
func stdlibFind(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("find: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("find: first argument must be a list, got %s", list.Inspect())
	}
	fn := args[1]
	if fn.Kind != core.VFn || fn.Fn == nil {
		return core.Nil, fmt.Errorf("find: second argument must be a function, got %s", fn.Inspect())
	}
	if env.CallFn == nil {
		return core.Nil, fmt.Errorf("find: CallFn not set")
	}
	for _, elem := range list.Elems {
		v, err := env.CallFn(fn.Fn, []core.Value{elem}, env)
		if err != nil {
			return core.Nil, fmt.Errorf("find: %w", err)
		}
		if v.Truthy() {
			return elem, nil
		}
	}
	return core.Nil, nil
}

// pairs map -> list of {key, value} tuples
func stdlibPairs(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("pairs: expected 1 argument, got %d", len(args))
	}
	if args[0].Kind != core.VMap {
		return core.Nil, fmt.Errorf("pairs: expected map, got %s", args[0].Inspect())
	}
	if args[0].Map == nil {
		return core.ListVal(), nil
	}
	elems := make([]core.Value, len(args[0].Map.Keys))
	for i, k := range args[0].Map.Keys {
		elems[i] = core.TupleVal(core.StringVal(k), args[0].Map.Vals[k])
	}
	return core.ListVal(elems...), nil
}

// enumerate list -> list of {index, value} tuples
func stdlibEnumerate(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("enumerate: expected 1 argument, got %d", len(args))
	}
	list := args[0]
	if list.Kind != core.VList {
		return core.Nil, fmt.Errorf("enumerate: expected list, got %s", list.Inspect())
	}
	elems := make([]core.Value, len(list.Elems))
	for i, v := range list.Elems {
		elems[i] = core.TupleVal(core.IntVal(int64(i)), v)
	}
	return core.ListVal(elems...), nil
}

// sleep ms -> nil (pause for milliseconds)
func stdlibSleep(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("sleep: expected 1 argument, got %d", len(args))
	}
	if args[0].Kind != core.VInt {
		return core.Nil, fmt.Errorf("sleep: expected integer (milliseconds), got %s", args[0].Inspect())
	}
	time.Sleep(time.Duration(args[0].Int) * time.Millisecond)
	return core.Nil, nil
}
