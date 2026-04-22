package stdlib

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"

	"ish/internal/core"
)

func stdlibFromJSON(args []core.Value, scope core.Scope) (core.Value, error) {
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
						return core.PidVal(p)
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
		return core.MapVal(m)
	}
	return core.StringVal(fmt.Sprintf("%v", v))
}

func stdlibToJSON(args []core.Value, scope core.Scope) (core.Value, error) {
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
		return v.GetInt()
	case core.VFloat:
		return v.GetFloat()
	case core.VString:
		return v.Str
	case core.VList:
		elems := v.GetElems()
		arr := make([]interface{}, len(elems))
		for i, elem := range elems {
			arr[i] = valueToJSON(elem)
		}
		return arr
	case core.VTuple:
		elems := v.GetElems()
		arr := make([]interface{}, len(elems))
		for i, elem := range elems {
			arr[i] = valueToJSON(elem)
		}
		return arr
	case core.VMap:
		obj := make(map[string]interface{})
		if m := v.GetMap(); m != nil {
			for _, k := range m.Keys {
				obj[k] = valueToJSON(m.Vals[k])
			}
		}
		return obj
	case core.VPid:
		if p := v.GetPid(); p != nil {
			return map[string]interface{}{"__pid": p.ID()}
		}
		return nil
	case core.VFn:
		name := ""
		if fn := v.GetFn(); fn != nil {
			name = fn.Name
		}
		return map[string]interface{}{"__fn": name}
	default:
		return v.String()
	}
}

func stdlibFromCSV(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("from_csv: expected 1 argument, got %d", len(args))
	}
	return parseDelimited(args[0].ToStr(), ',')
}

func stdlibToCSV(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("to_csv: expected 1 argument, got %d", len(args))
	}
	return formatDelimited(args[0], ',')
}

func stdlibFromTSV(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("from_tsv: expected 1 argument, got %d", len(args))
	}
	return parseDelimited(args[0].ToStr(), '\t')
}

func stdlibToTSV(args []core.Value, scope core.Scope) (core.Value, error) {
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
		rows = append(rows, core.MapVal(m))
	}
	return core.ListVal(rows...), nil
}

func formatDelimited(v core.Value, delim rune) (core.Value, error) {
	if v.Kind != core.VList {
		return core.Nil, fmt.Errorf("expected a list, got %s", v.Inspect())
	}
	vElems := v.GetElems()
	if len(vElems) == 0 {
		return core.StringVal(""), nil
	}

	var buf strings.Builder
	w := csv.NewWriter(&buf)
	w.Comma = delim

	first := vElems[0]
	if first.Kind == core.VMap && first.GetMap() != nil {
		headers := first.GetMap().Keys
		w.Write(headers)
		for _, elem := range vElems {
			em := elem.GetMap()
			if elem.Kind != core.VMap || em == nil {
				continue
			}
			row := make([]string, len(headers))
			for i, h := range headers {
				if val, ok := em.Get(h); ok {
					row[i] = val.ToStr()
				}
			}
			w.Write(row)
		}
	} else if first.Kind == core.VList {
		for _, elem := range vElems {
			if elem.Kind != core.VList {
				continue
			}
			elemElems := elem.GetElems()
			row := make([]string, len(elemElems))
			for i, field := range elemElems {
				row[i] = field.ToStr()
			}
			w.Write(row)
		}
	} else {
		row := make([]string, len(vElems))
		for i, elem := range vElems {
			row[i] = elem.ToStr()
		}
		w.Write(row)
	}

	w.Flush()
	result := buf.String()
	result = strings.TrimRight(result, "\n")
	return core.StringVal(result), nil
}

// Lines converts an ish value to text for shell output (pipes, redirects).
// Protocol-based: each type converts to its natural text representation.
//   - String → the string itself
//   - List   → each element on its own line (strings raw, compounds inspected)
//   - Map    → JSON encoding
//   - Tuple  → inspect representation
//   - Nil    → empty string
//   - other  → ToStr()
func Lines(val core.Value) string {
	switch val.Kind {
	case core.VNil:
		return ""
	case core.VString:
		return val.ToStr()
	case core.VList:
		elems := val.GetElems()
		parts := make([]string, len(elems))
		for i, elem := range elems {
			parts[i] = elemToLine(elem)
		}
		return strings.Join(parts, "\n")
	case core.VMap:
		raw := valueToJSON(val)
		bs, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			return val.Inspect()
		}
		return string(bs)
	case core.VTuple:
		return val.Inspect()
	default:
		return val.ToStr()
	}
}

// elemToLine converts a single list element to its line representation.
// Strings are raw (no quotes), everything else uses its natural format.
func elemToLine(val core.Value) string {
	if val.Kind == core.VString {
		return val.Str
	}
	return val.ToStr()
}

// Unlines converts shell text (byte stream) to an ish value.
// Default: splits on newlines into a list of strings.
// Bridge functions (JSON.parse, CSV.parse) override this with their own parsing.
func Unlines(text string) core.Value {
	if text == "" {
		return core.ListVal()
	}
	if strings.HasSuffix(text, "\n") {
		text = text[:len(text)-1]
	}
	lines := strings.Split(text, "\n")
	elems := make([]core.Value, len(lines))
	for i, line := range lines {
		elems[i] = core.StringVal(line)
	}
	return core.ListVal(elems...)
}

func stdlibLines(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("IO.lines: expected 1 argument, got %d", len(args))
	}
	return core.StringVal(Lines(args[0])), nil
}

func stdlibUnlines(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("IO.unlines: expected 1 argument, got %d", len(args))
	}
	// If already a list, pass through (protocol: no double conversion)
	if args[0].Kind == core.VList {
		return args[0], nil
	}
	return Unlines(args[0].ToStr()), nil
}
