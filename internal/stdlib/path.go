package stdlib

import (
	"os"
	"path/filepath"

	"ish/internal/value"
)

func pathModule() *value.OrdMap {
	return makeModule(map[string]func([]value.Value) (value.Value, error){
		"basename": func(args []value.Value) (value.Value, error) { return value.StringVal(filepath.Base(arg(args, 0).ToStr())), nil },
		"dirname":  func(args []value.Value) (value.Value, error) { return value.StringVal(filepath.Dir(arg(args, 0).ToStr())), nil },
		"extname":  func(args []value.Value) (value.Value, error) { return value.StringVal(filepath.Ext(arg(args, 0).ToStr())), nil },
		"join": func(args []value.Value) (value.Value, error) {
			var parts []string
			for _, a := range args {
				parts = append(parts, a.ToStr())
			}
			return value.StringVal(filepath.Join(parts...)), nil
		},
		"exists": func(args []value.Value) (value.Value, error) {
			_, err := os.Stat(arg(args, 0).ToStr())
			return value.BoolVal(err == nil), nil
		},
		"abs": func(args []value.Value) (value.Value, error) {
			p, err := filepath.Abs(arg(args, 0).ToStr())
			if err != nil {
				return value.Nil, err
			}
			return value.StringVal(p), nil
		},
	})
}
