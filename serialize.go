package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// RegisterSerialize registers format conversion functions (the bridge between
// bytes and values). These convert strings ↔ structured ish values.
func RegisterSerialize(env *Env) {
	env.SetNativeFn("from_json", stdlibFromJSON)
	env.SetNativeFn("to_json", stdlibToJSON)
	env.SetNativeFn("from_csv", stdlibFromCSV)
	env.SetNativeFn("to_csv", stdlibToCSV)
	env.SetNativeFn("from_tsv", stdlibFromTSV)
	env.SetNativeFn("to_tsv", stdlibToTSV)
	env.SetNativeFn("from_lines", stdlibFromLines)
	env.SetNativeFn("to_lines", stdlibToLines)
}

// ---------------------------------------------------------------------------
// JSON
// ---------------------------------------------------------------------------

func stdlibFromJSON(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("from_json: expected 1 argument, got %d", len(args))
	}
	s := args[0].ToStr()
	var raw interface{}
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return Nil, fmt.Errorf("from_json: %s", err)
	}
	return jsonToValue(raw), nil
}

func jsonToValue(v interface{}) Value {
	switch val := v.(type) {
	case nil:
		return Nil
	case bool:
		if val {
			return True
		}
		return False
	case float64:
		// JSON numbers are float64. If it's a whole number, store as int.
		if val == float64(int64(val)) {
			return IntVal(int64(val))
		}
		// We don't have a float type — store as string representation
		return StringVal(strconv.FormatFloat(val, 'f', -1, 64))
	case string:
		return StringVal(val)
	case []interface{}:
		elems := make([]Value, len(val))
		for i, elem := range val {
			elems[i] = jsonToValue(elem)
		}
		return ListVal(elems...)
	case map[string]interface{}:
		// Decode special types
		if pid, ok := val["__pid"]; ok && len(val) == 1 {
			if id, ok := pid.(float64); ok {
				if p := FindProcess(int64(id)); p != nil {
					return Value{Kind: VPid, Pid: p}
				}
			}
			return Nil
		}
		if fn, ok := val["__fn"]; ok && len(val) == 1 {
			_ = fn // Function cannot be deserialized — return atom with name
			if name, ok := fn.(string); ok {
				return AtomVal(name)
			}
			return Nil
		}
		m := NewOrdMap()
		for k, v := range val {
			m.Set(k, jsonToValue(v))
		}
		return Value{Kind: VMap, Map: m}
	}
	return StringVal(fmt.Sprintf("%v", v))
}

func stdlibToJSON(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("to_json: expected 1 argument, got %d", len(args))
	}
	raw := valueToJSON(args[0])
	bs, err := json.Marshal(raw)
	if err != nil {
		return Nil, fmt.Errorf("to_json: %s", err)
	}
	return StringVal(string(bs)), nil
}

func valueToJSON(v Value) interface{} {
	switch v.Kind {
	case VNil:
		return nil
	case VAtom:
		if v.Str == "true" {
			return true
		}
		if v.Str == "false" {
			return false
		}
		return v.Str
	case VInt:
		return v.Int
	case VString:
		return v.Str
	case VList:
		arr := make([]interface{}, len(v.Elems))
		for i, elem := range v.Elems {
			arr[i] = valueToJSON(elem)
		}
		return arr
	case VTuple:
		arr := make([]interface{}, len(v.Elems))
		for i, elem := range v.Elems {
			arr[i] = valueToJSON(elem)
		}
		return arr
	case VMap:
		obj := make(map[string]interface{})
		if v.Map != nil {
			for _, k := range v.Map.Keys {
				obj[k] = valueToJSON(v.Map.Vals[k])
			}
		}
		return obj
	case VPid:
		if v.Pid != nil {
			return map[string]interface{}{"__pid": v.Pid.id}
		}
		return nil
	case VFn:
		name := ""
		if v.Fn != nil {
			name = v.Fn.Name
		}
		return map[string]interface{}{"__fn": name}
	default:
		return v.String()
	}
}

// ---------------------------------------------------------------------------
// CSV
// ---------------------------------------------------------------------------

func stdlibFromCSV(args []Value, env *Env) (Value, error) {
	if len(args) < 1 || len(args) > 1 {
		return Nil, fmt.Errorf("from_csv: expected 1 argument, got %d", len(args))
	}
	return parseDelimited(args[0].ToStr(), ',')
}

func stdlibToCSV(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("to_csv: expected 1 argument, got %d", len(args))
	}
	return formatDelimited(args[0], ',')
}

// ---------------------------------------------------------------------------
// TSV
// ---------------------------------------------------------------------------

func stdlibFromTSV(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("from_tsv: expected 1 argument, got %d", len(args))
	}
	return parseDelimited(args[0].ToStr(), '\t')
}

func stdlibToTSV(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("to_tsv: expected 1 argument, got %d", len(args))
	}
	return formatDelimited(args[0], '\t')
}

// parseDelimited parses CSV/TSV text into ish values.
// If the data has more than one row, the first row is treated as headers
// and subsequent rows become maps. A single row returns a list of strings.
func parseDelimited(s string, delim rune) (Value, error) {
	r := csv.NewReader(strings.NewReader(s))
	r.Comma = delim
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	records, err := r.ReadAll()
	if err != nil {
		return Nil, fmt.Errorf("parse error: %s", err)
	}
	if len(records) == 0 {
		return ListVal(), nil
	}

	// Single row — return as list of strings
	if len(records) == 1 {
		elems := make([]Value, len(records[0]))
		for i, field := range records[0] {
			elems[i] = StringVal(field)
		}
		return ListVal(elems...), nil
	}

	// Multiple rows — first row is headers, rest are maps
	headers := records[0]
	rows := make([]Value, 0, len(records)-1)
	for _, record := range records[1:] {
		m := NewOrdMap()
		for i, header := range headers {
			val := ""
			if i < len(record) {
				val = record[i]
			}
			m.Set(header, StringVal(val))
		}
		rows = append(rows, Value{Kind: VMap, Map: m})
	}
	return ListVal(rows...), nil
}

// formatDelimited converts a list of maps (or list of lists) to CSV/TSV.
func formatDelimited(v Value, delim rune) (Value, error) {
	if v.Kind != VList {
		return Nil, fmt.Errorf("expected a list, got %s", v.Inspect())
	}
	if len(v.Elems) == 0 {
		return StringVal(""), nil
	}

	var buf strings.Builder
	w := csv.NewWriter(&buf)
	w.Comma = delim

	// Detect format: list of maps or list of lists
	first := v.Elems[0]
	if first.Kind == VMap && first.Map != nil {
		// Write header row from first map's keys
		headers := first.Map.Keys
		w.Write(headers)
		for _, elem := range v.Elems {
			if elem.Kind != VMap || elem.Map == nil {
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
	} else if first.Kind == VList {
		// List of lists
		for _, elem := range v.Elems {
			if elem.Kind != VList {
				continue
			}
			row := make([]string, len(elem.Elems))
			for i, field := range elem.Elems {
				row[i] = field.ToStr()
			}
			w.Write(row)
		}
	} else {
		// Single flat list — write as one row
		row := make([]string, len(v.Elems))
		for i, elem := range v.Elems {
			row[i] = elem.ToStr()
		}
		w.Write(row)
	}

	w.Flush()
	result := buf.String()
	// Trim trailing newline from csv.Writer
	result = strings.TrimRight(result, "\n")
	return StringVal(result), nil
}

// ---------------------------------------------------------------------------
// Lines
// ---------------------------------------------------------------------------

func stdlibFromLines(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("from_lines: expected 1 argument, got %d", len(args))
	}
	s := args[0].ToStr()
	if s == "" {
		return ListVal(), nil
	}
	lines := strings.Split(s, "\n")
	// Remove trailing empty string from final newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	elems := make([]Value, len(lines))
	for i, line := range lines {
		elems[i] = StringVal(line)
	}
	return ListVal(elems...), nil
}

func stdlibToLines(args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return Nil, fmt.Errorf("to_lines: expected 1 argument, got %d", len(args))
	}
	if args[0].Kind != VList {
		return Nil, fmt.Errorf("to_lines: expected a list, got %s", args[0].Inspect())
	}
	parts := make([]string, len(args[0].Elems))
	for i, elem := range args[0].Elems {
		parts[i] = elem.ToStr()
	}
	return StringVal(strings.Join(parts, "\n")), nil
}
