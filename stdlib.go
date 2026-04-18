package main

import (
	"fmt"
	"strings"
)

// NativeFn is a Go function callable as an ish function.
type NativeFn func(args []Value, env *Env) (Value, error)

// RegisterStdlib registers the standard library native functions into env.
func RegisterStdlib(env *Env) {
	env.SetNativeFn("hd", stdlibHd)
	env.SetNativeFn("tl", stdlibTl)
	env.SetNativeFn("length", stdlibLength)
	env.SetNativeFn("append", stdlibAppend)
	env.SetNativeFn("concat", stdlibConcat)
	env.SetNativeFn("map", stdlibMap)
	env.SetNativeFn("filter", stdlibFilter)
	env.SetNativeFn("reduce", stdlibReduce)
	env.SetNativeFn("range", stdlibRange)
	env.SetNativeFn("at", stdlibAt)

	// Map functions
	env.SetNativeFn("put", stdlibPut)
	env.SetNativeFn("delete", stdlibDelete)
	env.SetNativeFn("merge", stdlibMerge)
	env.SetNativeFn("keys", stdlibKeys)
	env.SetNativeFn("values", stdlibValues)
	env.SetNativeFn("has_key", stdlibHasKey)

	// String functions
	env.SetNativeFn("split", stdlibSplit)
	env.SetNativeFn("join", stdlibJoin)
	env.SetNativeFn("trim", stdlibTrim)
	env.SetNativeFn("upcase", stdlibUpcase)
	env.SetNativeFn("downcase", stdlibDowncase)
	env.SetNativeFn("replace", stdlibReplace)
	env.SetNativeFn("replace_all", stdlibReplaceAll)
	env.SetNativeFn("starts_with", stdlibStartsWith)
	env.SetNativeFn("ends_with", stdlibEndsWith)
	env.SetNativeFn("contains", stdlibContains)
	env.SetNativeFn("substring", stdlibSubstring)
	env.SetNativeFn("index_of", stdlibIndexOf)
}

// hd list -> first element (error on empty)
func stdlibHd(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("hd: expected 1 argument, got %d", len(args))
	}
	list := args[0]
	if list.Kind != VList {
		return Nil, fmt.Errorf("hd: expected list, got %s", list.Inspect())
	}
	if len(list.Elems) == 0 {
		return Nil, fmt.Errorf("hd: empty list")
	}
	return list.Elems[0], nil
}

// tl list -> all elements except first (error on empty)
func stdlibTl(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("tl: expected 1 argument, got %d", len(args))
	}
	list := args[0]
	if list.Kind != VList {
		return Nil, fmt.Errorf("tl: expected list, got %s", list.Inspect())
	}
	if len(list.Elems) == 0 {
		return Nil, fmt.Errorf("tl: empty list")
	}
	return ListVal(list.Elems[1:]...), nil
}

// length val -> for lists: len(elems), for strings: len(str), for maps: len(keys), for tuples: len(elems)
func stdlibLength(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("length: expected 1 argument, got %d", len(args))
	}
	v := args[0]
	switch v.Kind {
	case VList:
		return IntVal(int64(len(v.Elems))), nil
	case VTuple:
		return IntVal(int64(len(v.Elems))), nil
	case VString:
		return IntVal(int64(len(v.Str))), nil
	case VMap:
		if v.Map == nil {
			return IntVal(0), nil
		}
		return IntVal(int64(len(v.Map.Keys))), nil
	default:
		return Nil, fmt.Errorf("length: unsupported type %s", v.Inspect())
	}
}

// append list, elem -> new list with elem appended
func stdlibAppend(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("append: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != VList {
		return Nil, fmt.Errorf("append: first argument must be a list, got %s", list.Inspect())
	}
	newElems := make([]Value, len(list.Elems)+1)
	copy(newElems, list.Elems)
	newElems[len(list.Elems)] = args[1]
	return ListVal(newElems...), nil
}

// concat list1, list2 -> concatenated list
func stdlibConcat(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("concat: expected 2 arguments, got %d", len(args))
	}
	a, b := args[0], args[1]
	if a.Kind != VList {
		return Nil, fmt.Errorf("concat: first argument must be a list, got %s", a.Inspect())
	}
	if b.Kind != VList {
		return Nil, fmt.Errorf("concat: second argument must be a list, got %s", b.Inspect())
	}
	newElems := make([]Value, 0, len(a.Elems)+len(b.Elems))
	newElems = append(newElems, a.Elems...)
	newElems = append(newElems, b.Elems...)
	return ListVal(newElems...), nil
}

// map list, fn -> apply fn to each element, return new list
func stdlibMap(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("map: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != VList {
		return Nil, fmt.Errorf("map: first argument must be a list, got %s", list.Inspect())
	}
	fn := args[1]
	if fn.Kind != VFn || fn.Fn == nil {
		return Nil, fmt.Errorf("map: second argument must be a function, got %s", fn.Inspect())
	}
	result := make([]Value, len(list.Elems))
	for i, elem := range list.Elems {
		v, err := callFn(fn.Fn, []Value{elem}, env)
		if err != nil {
			return Nil, fmt.Errorf("map: %w", err)
		}
		result[i] = v
	}
	return ListVal(result...), nil
}

// filter list, fn -> keep elements where fn returns truthy
func stdlibFilter(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("filter: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != VList {
		return Nil, fmt.Errorf("filter: first argument must be a list, got %s", list.Inspect())
	}
	fn := args[1]
	if fn.Kind != VFn || fn.Fn == nil {
		return Nil, fmt.Errorf("filter: second argument must be a function, got %s", fn.Inspect())
	}
	var result []Value
	for _, elem := range list.Elems {
		v, err := callFn(fn.Fn, []Value{elem}, env)
		if err != nil {
			return Nil, fmt.Errorf("filter: %w", err)
		}
		if v.Truthy() {
			result = append(result, elem)
		}
	}
	return ListVal(result...), nil
}

// reduce list, acc, fn -> fold left: fn(acc, elem) for each elem
func stdlibReduce(args []Value, env *Env) (Value, error) {
	if len(args) != 3 {
		return Nil, fmt.Errorf("reduce: expected 3 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != VList {
		return Nil, fmt.Errorf("reduce: first argument must be a list, got %s", list.Inspect())
	}
	acc := args[1]
	fn := args[2]
	if fn.Kind != VFn || fn.Fn == nil {
		return Nil, fmt.Errorf("reduce: third argument must be a function, got %s", fn.Inspect())
	}
	for _, elem := range list.Elems {
		var err error
		acc, err = callFn(fn.Fn, []Value{acc, elem}, env)
		if err != nil {
			return Nil, fmt.Errorf("reduce: %w", err)
		}
	}
	return acc, nil
}

// range start, stop -> list of integers [start, start+1, ..., stop-1]
func stdlibRange(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("range: expected 2 arguments, got %d", len(args))
	}
	if args[0].Kind != VInt {
		return Nil, fmt.Errorf("range: start must be an integer, got %s", args[0].Inspect())
	}
	if args[1].Kind != VInt {
		return Nil, fmt.Errorf("range: stop must be an integer, got %s", args[1].Inspect())
	}
	start, stop := args[0].Int, args[1].Int
	if start >= stop {
		return ListVal(), nil
	}
	const maxRangeSize = 10_000_000
	if stop-start > maxRangeSize {
		return Nil, fmt.Errorf("range: size %d exceeds maximum %d", stop-start, maxRangeSize)
	}
	elems := make([]Value, 0, stop-start)
	for i := start; i < stop; i++ {
		elems = append(elems, IntVal(i))
	}
	return ListVal(elems...), nil
}

// at list, index -> element at 0-based index
func stdlibAt(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("at: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != VList {
		return Nil, fmt.Errorf("at: first argument must be a list, got %s", list.Inspect())
	}
	if args[1].Kind != VInt {
		return Nil, fmt.Errorf("at: index must be an integer, got %s", args[1].Inspect())
	}
	idx := args[1].Int
	if idx < 0 || idx >= int64(len(list.Elems)) {
		return Nil, fmt.Errorf("at: index %d out of bounds (list length %d)", idx, len(list.Elems))
	}
	return list.Elems[idx], nil
}

// ---------------------------------------------------------------------------
// String functions
// ---------------------------------------------------------------------------

// split str, delim -> list of strings
func stdlibSplit(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("split: expected 2 arguments, got %d", len(args))
	}
	parts := strings.Split(args[0].ToStr(), args[1].ToStr())
	elems := make([]Value, len(parts))
	for i, p := range parts {
		elems[i] = StringVal(p)
	}
	return ListVal(elems...), nil
}

// join list, delim -> string
func stdlibJoin(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("join: expected 2 arguments, got %d", len(args))
	}
	list := args[0]
	if list.Kind != VList {
		return Nil, fmt.Errorf("join: first argument must be a list, got %s", list.Inspect())
	}
	delim := args[1].ToStr()
	strs := make([]string, len(list.Elems))
	for i, elem := range list.Elems {
		strs[i] = elem.ToStr()
	}
	return StringVal(strings.Join(strs, delim)), nil
}

// trim str -> trimmed string
func stdlibTrim(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("trim: expected 1 argument, got %d", len(args))
	}
	return StringVal(strings.TrimSpace(args[0].ToStr())), nil
}

// upcase str -> uppercase string
func stdlibUpcase(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("upcase: expected 1 argument, got %d", len(args))
	}
	return StringVal(strings.ToUpper(args[0].ToStr())), nil
}

// downcase str -> lowercase string
func stdlibDowncase(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("downcase: expected 1 argument, got %d", len(args))
	}
	return StringVal(strings.ToLower(args[0].ToStr())), nil
}

// replace str, old, new -> replace first occurrence
func stdlibReplace(args []Value, env *Env) (Value, error) {
	if len(args) != 3 {
		return Nil, fmt.Errorf("replace: expected 3 arguments, got %d", len(args))
	}
	return StringVal(strings.Replace(args[0].ToStr(), args[1].ToStr(), args[2].ToStr(), 1)), nil
}

// replace_all str, old, new -> replace all occurrences
func stdlibReplaceAll(args []Value, env *Env) (Value, error) {
	if len(args) != 3 {
		return Nil, fmt.Errorf("replace_all: expected 3 arguments, got %d", len(args))
	}
	return StringVal(strings.ReplaceAll(args[0].ToStr(), args[1].ToStr(), args[2].ToStr())), nil
}

// starts_with str, prefix -> bool (as atom :true/:false)
func stdlibStartsWith(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("starts_with: expected 2 arguments, got %d", len(args))
	}
	if strings.HasPrefix(args[0].ToStr(), args[1].ToStr()) {
		return AtomVal("true"), nil
	}
	return AtomVal("false"), nil
}

// ends_with str, suffix -> bool (as atom :true/:false)
func stdlibEndsWith(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("ends_with: expected 2 arguments, got %d", len(args))
	}
	if strings.HasSuffix(args[0].ToStr(), args[1].ToStr()) {
		return AtomVal("true"), nil
	}
	return AtomVal("false"), nil
}

// contains str, substr -> bool (as atom :true/:false)
func stdlibContains(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("contains: expected 2 arguments, got %d", len(args))
	}
	if strings.Contains(args[0].ToStr(), args[1].ToStr()) {
		return AtomVal("true"), nil
	}
	return AtomVal("false"), nil
}

// substring str, start, len -> substring
func stdlibSubstring(args []Value, env *Env) (Value, error) {
	if len(args) != 3 {
		return Nil, fmt.Errorf("substring: expected 3 arguments, got %d", len(args))
	}
	s := args[0].ToStr()
	if args[1].Kind != VInt {
		return Nil, fmt.Errorf("substring: start must be an integer, got %s", args[1].Inspect())
	}
	if args[2].Kind != VInt {
		return Nil, fmt.Errorf("substring: length must be an integer, got %s", args[2].Inspect())
	}
	start := int(args[1].Int)
	length := int(args[2].Int)
	if start < 0 {
		start = 0
	}
	if start >= len(s) {
		return StringVal(""), nil
	}
	end := start + length
	if end > len(s) {
		end = len(s)
	}
	return StringVal(s[start:end]), nil
}

// index_of str, substr -> int (-1 if not found)
func stdlibIndexOf(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("index_of: expected 2 arguments, got %d", len(args))
	}
	idx := strings.Index(args[0].ToStr(), args[1].ToStr())
	return IntVal(int64(idx)), nil
}

// ---------------------------------------------------------------------------
// Map operations
// ---------------------------------------------------------------------------

// put map, key, value -> new map with key set
func stdlibPut(args []Value, env *Env) (Value, error) {
	if len(args) != 3 {
		return Nil, fmt.Errorf("put: expected 3 arguments, got %d", len(args))
	}
	if args[0].Kind != VMap {
		return Nil, fmt.Errorf("put: first argument must be a map, got %s", args[0].Inspect())
	}
	m := NewOrdMap()
	if args[0].Map != nil {
		for _, k := range args[0].Map.Keys {
			m.Set(k, args[0].Map.Vals[k])
		}
	}
	m.Set(args[1].ToStr(), args[2])
	return Value{Kind: VMap, Map: m}, nil
}

// delete map, key -> new map without key
func stdlibDelete(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("delete: expected 2 arguments, got %d", len(args))
	}
	if args[0].Kind != VMap {
		return Nil, fmt.Errorf("delete: first argument must be a map, got %s", args[0].Inspect())
	}
	key := args[1].ToStr()
	m := NewOrdMap()
	if args[0].Map != nil {
		for _, k := range args[0].Map.Keys {
			if k != key {
				m.Set(k, args[0].Map.Vals[k])
			}
		}
	}
	return Value{Kind: VMap, Map: m}, nil
}

// merge map1, map2 -> combined map (map2 wins on conflicts)
func stdlibMerge(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("merge: expected 2 arguments, got %d", len(args))
	}
	if args[0].Kind != VMap {
		return Nil, fmt.Errorf("merge: first argument must be a map, got %s", args[0].Inspect())
	}
	if args[1].Kind != VMap {
		return Nil, fmt.Errorf("merge: second argument must be a map, got %s", args[1].Inspect())
	}
	m := NewOrdMap()
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
	return Value{Kind: VMap, Map: m}, nil
}

// keys map -> list of key strings
func stdlibKeys(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("keys: expected 1 argument, got %d", len(args))
	}
	if args[0].Kind != VMap {
		return Nil, fmt.Errorf("keys: argument must be a map, got %s", args[0].Inspect())
	}
	if args[0].Map == nil {
		return ListVal(), nil
	}
	elems := make([]Value, len(args[0].Map.Keys))
	for i, k := range args[0].Map.Keys {
		elems[i] = StringVal(k)
	}
	return ListVal(elems...), nil
}

// values map -> list of values
func stdlibValues(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("values: expected 1 argument, got %d", len(args))
	}
	if args[0].Kind != VMap {
		return Nil, fmt.Errorf("values: argument must be a map, got %s", args[0].Inspect())
	}
	if args[0].Map == nil {
		return ListVal(), nil
	}
	elems := make([]Value, len(args[0].Map.Keys))
	for i, k := range args[0].Map.Keys {
		elems[i] = args[0].Map.Vals[k]
	}
	return ListVal(elems...), nil
}

// has_key map, key -> :true/:false
func stdlibHasKey(args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return Nil, fmt.Errorf("has_key: expected 2 arguments, got %d", len(args))
	}
	if args[0].Kind != VMap {
		return Nil, fmt.Errorf("has_key: first argument must be a map, got %s", args[0].Inspect())
	}
	key := args[1].ToStr()
	if args[0].Map != nil {
		if _, ok := args[0].Map.Get(key); ok {
			return True, nil
		}
	}
	return False, nil
}
