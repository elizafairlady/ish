package stdlib

import (
	"encoding/csv"
	"strings"

	"ish/internal/value"
)

func csvModule() *value.OrdMap {
	return makeModule(map[string]func([]value.Value) (value.Value, error){
		"parse": func(args []value.Value) (value.Value, error) {
			r := csv.NewReader(strings.NewReader(arg(args, 0).ToStr()))
			records, err := r.ReadAll()
			if err != nil {
				return value.Nil, err
			}
			rows := make([]value.Value, len(records))
			for i, rec := range records {
				cols := make([]value.Value, len(rec))
				for j, c := range rec {
					cols[j] = value.StringVal(c)
				}
				rows[i] = value.ListVal(cols...)
			}
			return value.ListVal(rows...), nil
		},
		"encode": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VList {
				return value.StringVal(""), nil
			}
			var buf strings.Builder
			w := csv.NewWriter(&buf)
			for _, row := range args[0].Elems() {
				if row.Kind != value.VList {
					continue
				}
				var rec []string
				for _, col := range row.Elems() {
					rec = append(rec, col.ToStr())
				}
				w.Write(rec)
			}
			w.Flush()
			return value.StringVal(buf.String()), nil
		},
	})
}
