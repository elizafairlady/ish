package stdlib

import "ish/internal/value"

func mapModule() *value.OrdMap {
	return makeModule(map[string]func([]value.Value) (value.Value, error){
		"get": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VMap {
				return value.Nil, nil
			}
			if v, ok := args[0].Map().Vals[arg(args, 1).ToStr()]; ok {
				return v, nil
			}
			return value.Nil, nil
		},
		"put": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VMap {
				return value.Nil, nil
			}
			src := args[0].Map()
			key := arg(args, 1).ToStr()
			nm := &value.OrdMap{Vals: make(map[string]value.Value)}
			for _, k := range src.Keys {
				nm.Keys = append(nm.Keys, k)
				nm.Vals[k] = src.Vals[k]
			}
			if _, exists := nm.Vals[key]; !exists {
				nm.Keys = append(nm.Keys, key)
			}
			nm.Vals[key] = arg(args, 2)
			return value.MapVal(nm), nil
		},
		"delete": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VMap {
				return value.Nil, nil
			}
			src := args[0].Map()
			key := arg(args, 1).ToStr()
			nm := &value.OrdMap{Vals: make(map[string]value.Value)}
			for _, k := range src.Keys {
				if k != key {
					nm.Keys = append(nm.Keys, k)
					nm.Vals[k] = src.Vals[k]
				}
			}
			return value.MapVal(nm), nil
		},
		"keys": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VMap {
				return value.ListVal(), nil
			}
			m := args[0].Map()
			elems := make([]value.Value, len(m.Keys))
			for i, k := range m.Keys {
				elems[i] = value.StringVal(k)
			}
			return value.ListVal(elems...), nil
		},
		"values": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VMap {
				return value.ListVal(), nil
			}
			m := args[0].Map()
			elems := make([]value.Value, len(m.Keys))
			for i, k := range m.Keys {
				elems[i] = m.Vals[k]
			}
			return value.ListVal(elems...), nil
		},
		"merge": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VMap || arg(args, 1).Kind != value.VMap {
				return value.Nil, nil
			}
			a, b := args[0].Map(), args[1].Map()
			nm := &value.OrdMap{Vals: make(map[string]value.Value)}
			for _, k := range a.Keys {
				nm.Keys = append(nm.Keys, k)
				nm.Vals[k] = a.Vals[k]
			}
			for _, k := range b.Keys {
				if _, exists := nm.Vals[k]; !exists {
					nm.Keys = append(nm.Keys, k)
				}
				nm.Vals[k] = b.Vals[k]
			}
			return value.MapVal(nm), nil
		},
		"has_key": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VMap {
				return value.False, nil
			}
			_, ok := args[0].Map().Vals[arg(args, 1).ToStr()]
			return value.BoolVal(ok), nil
		},
		"pairs": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VMap {
				return value.ListVal(), nil
			}
			m := args[0].Map()
			elems := make([]value.Value, len(m.Keys))
			for i, k := range m.Keys {
				elems[i] = value.TupleVal(value.StringVal(k), m.Vals[k])
			}
			return value.ListVal(elems...), nil
		},
		"new": func(args []value.Value) (value.Value, error) {
			if len(args) < 1 || args[0].Kind != value.VList { return value.MapVal(&value.OrdMap{Vals: make(map[string]value.Value)}), nil }
			nm := &value.OrdMap{Vals: make(map[string]value.Value)}
			for _, pair := range args[0].Elems() {
				if pair.Kind == value.VTuple && len(pair.Elems()) >= 2 {
					k := pair.Elems()[0].ToStr()
					if _, exists := nm.Vals[k]; !exists { nm.Keys = append(nm.Keys, k) }
					nm.Vals[k] = pair.Elems()[1]
				}
			}
			return value.MapVal(nm), nil
		},
		"update": func(args []value.Value) (value.Value, error) {
			if len(args) < 3 || args[0].Kind != value.VMap || args[2].Kind != value.VFn { return value.Nil, nil }
			src := args[0].Map()
			key := arg(args, 1).ToStr()
			nm := &value.OrdMap{Vals: make(map[string]value.Value)}
			for _, k := range src.Keys { nm.Keys = append(nm.Keys, k); nm.Vals[k] = src.Vals[k] }
			if v, ok := nm.Vals[key]; ok {
				nv, err := invoke(args[2].Fn(), []value.Value{v})
				if err != nil { return value.Nil, err }
				nm.Vals[key] = nv
			}
			return value.MapVal(nm), nil
		},
		"take": func(args []value.Value) (value.Value, error) {
			if len(args) < 2 || args[0].Kind != value.VMap || args[1].Kind != value.VList { return value.Nil, nil }
			src := args[0].Map()
			want := make(map[string]bool)
			for _, k := range args[1].Elems() { want[k.ToStr()] = true }
			nm := &value.OrdMap{Vals: make(map[string]value.Value)}
			for _, k := range src.Keys {
				if want[k] { nm.Keys = append(nm.Keys, k); nm.Vals[k] = src.Vals[k] }
			}
			return value.MapVal(nm), nil
		},
		"drop": func(args []value.Value) (value.Value, error) {
			if len(args) < 2 || args[0].Kind != value.VMap || args[1].Kind != value.VList { return value.Nil, nil }
			src := args[0].Map()
			drop := make(map[string]bool)
			for _, k := range args[1].Elems() { drop[k.ToStr()] = true }
			nm := &value.OrdMap{Vals: make(map[string]value.Value)}
			for _, k := range src.Keys {
				if !drop[k] { nm.Keys = append(nm.Keys, k); nm.Vals[k] = src.Vals[k] }
			}
			return value.MapVal(nm), nil
		},
		"reduce": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VMap {
				return arg(args, 1), nil
			}
			m := args[0].Map()
			acc := arg(args, 1)
			fn := arg(args, 2)
			if fn.Kind != value.VFn {
				return acc, nil
			}
			for _, k := range m.Keys {
				var err error
				acc, err = Invoke(fn.Fn(), []value.Value{acc, value.StringVal(k), m.Vals[k]})
				if err != nil {
					return value.Nil, err
				}
			}
			return acc, nil
		},
	})
}
