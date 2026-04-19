package stdlib

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"

	"ish/internal/core"
)

func stdlibFromJSON(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("from_json: expected 1 argument, got %d", len(args))
	}
	s := args[0].ToStr()
	var raw interface{}
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return core.Nil, fmt.Errorf("from_json: %s", err)
	}
	return jsonToValue(raw), nil
}

func jsonToValue(v interface{}) core.Value {
	switch val := v.(type) {
	case nil:
		return core.Nil
	case bool:
		if val {
			return core.True
		}
		return core.False
	case float64:
		if val == float64(int64(val)) {
			return core.IntVal(int64(val))
		}
		return core.FloatVal(val)
	case string:
		return core.StringVal(val)
	case []interface{}:
		elems := make([]core.Value, len(val))
		for i, elem := range val {
			elems[i] = jsonToValue(elem)
		}
		return core.ListVal(elems...)
	case map[string]interface{}:
		if pid, ok := val["__pid"]; ok && len(val) == 1 {
			if id, ok := pid.(float64); ok {
				if core.FindPid != nil {
					if p := core.FindPid(int64(id)); p != nil {
						return core.Value{Kind: core.VPid, Pid: p}
					}
				}
			}
			return core.Nil
		}
		if fn, ok := val["__fn"]; ok && len(val) == 1 {
			_ = fn
			if name, ok := fn.(string); ok {
				return core.AtomVal(name)
			}
			return core.Nil
		}
		m := core.NewOrdMap()
		for k, v := range val {
			m.Set(k, jsonToValue(v))
		}
		return core.Value{Kind: core.VMap, Map: m}
	}
	return core.StringVal(fmt.Sprintf("%v", v))
}

func stdlibToJSON(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("to_json: expected 1 argument, got %d", len(args))
	}
	raw := valueToJSON(args[0])
	bs, err := json.Marshal(raw)
	if err != nil {
		return core.Nil, fmt.Errorf("to_json: %s", err)
	}
	return core.StringVal(string(bs)), nil
}

func valueToJSON(v core.Value) interface{} {
	switch v.Kind {
	case core.VNil:
		return nil
	case core.VAtom:
		if v.Str == "true" {
			return true
		}
		if v.Str == "false" {
			return false
		}
		return v.Str
	case core.VInt:
		return v.Int
	case core.VFloat:
		return v.Float
	case core.VString:
		return v.Str
	case core.VList:
		arr := make([]interface{}, len(v.Elems))
		for i, elem := range v.Elems {
			arr[i] = valueToJSON(elem)
		}
		return arr
	case core.VTuple:
		arr := make([]interface{}, len(v.Elems))
		for i, elem := range v.Elems {
			arr[i] = valueToJSON(elem)
		}
		return arr
	case core.VMap:
		obj := make(map[string]interface{})
		if v.Map != nil {
			for _, k := range v.Map.Keys {
				obj[k] = valueToJSON(v.Map.Vals[k])
			}
		}
		return obj
	case core.VPid:
		if v.Pid != nil {
			return map[string]interface{}{"__pid": v.Pid.ID()}
		}
		return nil
	case core.VFn:
		name := ""
		if v.Fn != nil {
			name = v.Fn.Name
		}
		return map[string]interface{}{"__fn": name}
	default:
		return v.String()
	}
}

func stdlibFromCSV(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("from_csv: expected 1 argument, got %d", len(args))
	}
	return parseDelimited(args[0].ToStr(), ',')
}

func stdlibToCSV(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("to_csv: expected 1 argument, got %d", len(args))
	}
	return formatDelimited(args[0], ',')
}

func stdlibFromTSV(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("from_tsv: expected 1 argument, got %d", len(args))
	}
	return parseDelimited(args[0].ToStr(), '\t')
}

func stdlibToTSV(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("to_tsv: expected 1 argument, got %d", len(args))
	}
	return formatDelimited(args[0], '\t')
}

func parseDelimited(s string, delim rune) (core.Value, error) {
	r := csv.NewReader(strings.NewReader(s))
	r.Comma = delim
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	records, err := r.ReadAll()
	if err != nil {
		return core.Nil, fmt.Errorf("parse error: %s", err)
	}
	if len(records) == 0 {
		return core.ListVal(), nil
	}

	if len(records) == 1 {
		elems := make([]core.Value, len(records[0]))
		for i, field := range records[0] {
			elems[i] = core.StringVal(field)
		}
		return core.ListVal(elems...), nil
	}

	headers := records[0]
	rows := make([]core.Value, 0, len(records)-1)
	for _, record := range records[1:] {
		m := core.NewOrdMap()
		for i, header := range headers {
			val := ""
			if i < len(record) {
				val = record[i]
			}
			m.Set(header, core.StringVal(val))
		}
		rows = append(rows, core.Value{Kind: core.VMap, Map: m})
	}
	return core.ListVal(rows...), nil
}

func formatDelimited(v core.Value, delim rune) (core.Value, error) {
	if v.Kind != core.VList {
		return core.Nil, fmt.Errorf("expected a list, got %s", v.Inspect())
	}
	if len(v.Elems) == 0 {
		return core.StringVal(""), nil
	}

	var buf strings.Builder
	w := csv.NewWriter(&buf)
	w.Comma = delim

	first := v.Elems[0]
	if first.Kind == core.VMap && first.Map != nil {
		headers := first.Map.Keys
		w.Write(headers)
		for _, elem := range v.Elems {
			if elem.Kind != core.VMap || elem.Map == nil {
				continue
			}
			row := make([]string, len(headers))
			for i, h := range headers {
				if val, ok := elem.Map.Get(h); ok {
					row[i] = val.ToStr()
				}
			}
			w.Write(row)
		}
	} else if first.Kind == core.VList {
		for _, elem := range v.Elems {
			if elem.Kind != core.VList {
				continue
			}
			row := make([]string, len(elem.Elems))
			for i, field := range elem.Elems {
				row[i] = field.ToStr()
			}
			w.Write(row)
		}
	} else {
		row := make([]string, len(v.Elems))
		for i, elem := range v.Elems {
			row[i] = elem.ToStr()
		}
		w.Write(row)
	}

	w.Flush()
	result := buf.String()
	result = strings.TrimRight(result, "\n")
	return core.StringVal(result), nil
}

func stdlibFromLines(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("from_lines: expected 1 argument, got %d", len(args))
	}
	s := args[0].ToStr()
	if s == "" {
		return core.ListVal(), nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	elems := make([]core.Value, len(lines))
	for i, line := range lines {
		elems[i] = core.StringVal(line)
	}
	return core.ListVal(elems...), nil
}

func stdlibToLines(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("to_lines: expected 1 argument, got %d", len(args))
	}
	if args[0].Kind != core.VList {
		return core.Nil, fmt.Errorf("to_lines: expected a list, got %s", args[0].Inspect())
	}
	parts := make([]string, len(args[0].Elems))
	for i, elem := range args[0].Elems {
		parts[i] = elem.ToStr()
	}
	return core.StringVal(strings.Join(parts, "\n")), nil
}
