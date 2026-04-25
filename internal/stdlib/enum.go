package stdlib

import (
	"sort"
	"strings"

	"ish/internal/value"
)

func enumModule() *value.OrdMap {
	return makeModule(map[string]func([]value.Value) (value.Value, error){
		"map":      listMap,
		"filter":   listFilter,
		"reduce":   listReduce,
		"count":    kernelLength,
		"each":     listEach,
		"any":      listAny,
		"all":      listAll,
		"find":     listFind,
		"sum": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VList { return value.IntVal(0), nil }
			var sum int64
			for _, e := range args[0].Elems() {
				if e.Kind == value.VInt { sum += e.Int() }
			}
			return value.IntVal(sum), nil
		},
		"group_by": func(args []value.Value) (value.Value, error) {
			if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn { return value.Nil, nil }
			m := &value.OrdMap{Vals: make(map[string]value.Value)}
			for _, elem := range args[0].Elems() {
				key, err := invoke(args[1].Fn(), []value.Value{elem})
				if err != nil { return value.Nil, err }
				k := key.ToStr()
				if existing, ok := m.Vals[k]; ok {
					m.Vals[k] = value.ListVal(append(existing.Elems(), elem)...)
				} else {
					m.Keys = append(m.Keys, k)
					m.Vals[k] = value.ListVal(elem)
				}
			}
			return value.MapVal(m), nil
		},
		"sort_by": func(args []value.Value) (value.Value, error) {
			if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn { return value.Nil, nil }
			elems := append([]value.Value{}, args[0].Elems()...)
			type pair struct { key value.Value; val value.Value }
			pairs := make([]pair, len(elems))
			for i, e := range elems {
				k, err := invoke(args[1].Fn(), []value.Value{e})
				if err != nil { return value.Nil, err }
				pairs[i] = pair{k, e}
			}
			sort.SliceStable(pairs, func(i, j int) bool {
				a, b := pairs[i].key, pairs[j].key
				if a.Kind == value.VInt && b.Kind == value.VInt { return a.Int() < b.Int() }
				return a.ToStr() < b.ToStr()
			})
			result := make([]value.Value, len(pairs))
			for i, p := range pairs { result[i] = p.val }
			return value.ListVal(result...), nil
		},
		"flat_map": func(args []value.Value) (value.Value, error) {
			if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn { return value.Nil, nil }
			var result []value.Value
			for _, elem := range args[0].Elems() {
				v, err := invoke(args[1].Fn(), []value.Value{elem})
				if err != nil { return value.Nil, err }
				if v.Kind == value.VList {
					result = append(result, v.Elems()...)
				} else {
					result = append(result, v)
				}
			}
			return value.ListVal(result...), nil
		},
		"frequencies": func(args []value.Value) (value.Value, error) {
			if len(args) < 1 || args[0].Kind != value.VList { return value.Nil, nil }
			m := &value.OrdMap{Vals: make(map[string]value.Value)}
			for _, elem := range args[0].Elems() {
				k := elem.ToStr()
				if existing, ok := m.Vals[k]; ok {
					m.Vals[k] = value.IntVal(existing.Int() + 1)
				} else {
					m.Keys = append(m.Keys, k)
					m.Vals[k] = value.IntVal(1)
				}
			}
			return value.MapVal(m), nil
		},
		"reject": func(args []value.Value) (value.Value, error) {
			if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn { return value.Nil, nil }
			var result []value.Value
			for _, elem := range args[0].Elems() {
				v, err := invoke(args[1].Fn(), []value.Value{elem})
				if err != nil { return value.Nil, err }
				if !v.Truthy() { result = append(result, elem) }
			}
			return value.ListVal(result...), nil
		},
		"chunk_every": func(args []value.Value) (value.Value, error) {
			if len(args) < 2 || args[0].Kind != value.VList { return value.Nil, nil }
			n := int(arg(args, 1).Int())
			if n <= 0 { return value.Nil, nil }
			elems := args[0].Elems()
			var chunks []value.Value
			for i := 0; i < len(elems); i += n {
				end := i + n
				if end > len(elems) { end = len(elems) }
				chunks = append(chunks, value.ListVal(elems[i:end]...))
			}
			return value.ListVal(chunks...), nil
		},
		"join": func(args []value.Value) (value.Value, error) {
			if len(args) < 1 || args[0].Kind != value.VList { return value.StringVal(""), nil }
			sep := ""
			if len(args) > 1 { sep = arg(args, 1).ToStr() }
			var parts []string
			for _, e := range args[0].Elems() { parts = append(parts, e.ToStr()) }
			return value.StringVal(strings.Join(parts, sep)), nil
		},
		"min_by": func(args []value.Value) (value.Value, error) {
			if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn { return value.Nil, nil }
			elems := args[0].Elems()
			if len(elems) == 0 { return value.Nil, nil }
			best := elems[0]
			bestKey, _ := invoke(args[1].Fn(), []value.Value{best})
			for _, e := range elems[1:] {
				k, err := invoke(args[1].Fn(), []value.Value{e})
				if err != nil { return value.Nil, err }
				if k.Kind == value.VInt && bestKey.Kind == value.VInt {
					if k.Int() < bestKey.Int() { best = e; bestKey = k }
				} else if k.ToStr() < bestKey.ToStr() { best = e; bestKey = k }
			}
			return best, nil
		},
		"max_by": func(args []value.Value) (value.Value, error) {
			if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn { return value.Nil, nil }
			elems := args[0].Elems()
			if len(elems) == 0 { return value.Nil, nil }
			best := elems[0]
			bestKey, _ := invoke(args[1].Fn(), []value.Value{best})
			for _, e := range elems[1:] {
				k, err := invoke(args[1].Fn(), []value.Value{e})
				if err != nil { return value.Nil, err }
				if k.Kind == value.VInt && bestKey.Kind == value.VInt {
					if k.Int() > bestKey.Int() { best = e; bestKey = k }
				} else if k.ToStr() > bestKey.ToStr() { best = e; bestKey = k }
			}
			return best, nil
		},
		"take_while": func(args []value.Value) (value.Value, error) {
			if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn { return value.Nil, nil }
			var result []value.Value
			for _, elem := range args[0].Elems() {
				v, err := invoke(args[1].Fn(), []value.Value{elem})
				if err != nil { return value.Nil, err }
				if !v.Truthy() { break }
				result = append(result, elem)
			}
			return value.ListVal(result...), nil
		},
		"drop_while": func(args []value.Value) (value.Value, error) {
			if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn { return value.Nil, nil }
			elems := args[0].Elems()
			i := 0
			for ; i < len(elems); i++ {
				v, err := invoke(args[1].Fn(), []value.Value{elems[i]})
				if err != nil { return value.Nil, err }
				if !v.Truthy() { break }
			}
			return value.ListVal(elems[i:]...), nil
		},
		"dedup": func(args []value.Value) (value.Value, error) {
			if len(args) < 1 || args[0].Kind != value.VList { return value.Nil, nil }
			elems := args[0].Elems()
			if len(elems) == 0 { return value.ListVal(), nil }
			result := []value.Value{elems[0]}
			for _, e := range elems[1:] {
				if !e.Equal(result[len(result)-1]) { result = append(result, e) }
			}
			return value.ListVal(result...), nil
		},
		"uniq_by": func(args []value.Value) (value.Value, error) {
			if len(args) < 2 || args[0].Kind != value.VList || args[1].Kind != value.VFn { return value.Nil, nil }
			seen := make(map[string]bool)
			var result []value.Value
			for _, elem := range args[0].Elems() {
				k, err := invoke(args[1].Fn(), []value.Value{elem})
				if err != nil { return value.Nil, err }
				key := k.ToStr()
				if !seen[key] {
					seen[key] = true
					result = append(result, elem)
				}
			}
			return value.ListVal(result...), nil
		},
	})
}
