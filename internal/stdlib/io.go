package stdlib

import (
	"strings"

	"ish/internal/value"
)

func ioModule() *value.OrdMap {
	return makeModule(map[string]func([]value.Value) (value.Value, error){
		"lines": func(args []value.Value) (value.Value, error) {
			lines := strings.Split(strings.TrimRight(arg(args, 0).ToStr(), "\n"), "\n")
			elems := make([]value.Value, len(lines))
			for i, l := range lines {
				elems[i] = value.StringVal(l)
			}
			return value.ListVal(elems...), nil
		},
	})
}
