package stdlib

import "ish/internal/value"

// Enum delegates to List — the real implementations are the same primitives.
// Higher-level Enum functions (sum, count) are in the prelude.
func enumModule() *value.OrdMap {
	return makeModule(map[string]func([]value.Value) (value.Value, error){
		"map":    listMap,
		"filter": listFilter,
		"reduce": listReduce,
		"count":  kernelLength,
		"sum": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VList {
				return value.IntVal(0), nil
			}
			var sum int64
			for _, e := range args[0].Elems() {
				if e.Kind == value.VInt {
					sum += e.Int()
				}
			}
			return value.IntVal(sum), nil
		},
	})
}
