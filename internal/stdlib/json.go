package stdlib

import (
	"encoding/json"

	"ish/internal/value"
)

func jsonModule() *value.OrdMap {
	return makeModule(map[string]func([]value.Value) (value.Value, error){
		"parse":  jsonParse,
		"encode": jsonEncode,
	})
}

func jsonParse(args []value.Value) (value.Value, error) {
	var raw interface{}
	if err := json.Unmarshal([]byte(arg(args, 0).ToStr()), &raw); err != nil {
		return value.Nil, err
	}
	return jsonToValue(raw), nil
}

func jsonToValue(raw interface{}) value.Value {
	switch v := raw.(type) {
	case nil:
		return value.Nil
	case bool:
		return value.BoolVal(v)
	case float64:
		if v == float64(int64(v)) {
			return value.IntVal(int64(v))
		}
		return value.FloatVal(v)
	case string:
		return value.StringVal(v)
	case []interface{}:
		elems := make([]value.Value, len(v))
		for i, e := range v {
			elems[i] = jsonToValue(e)
		}
		return value.ListVal(elems...)
	case map[string]interface{}:
		m := &value.OrdMap{Vals: make(map[string]value.Value)}
		for k, val := range v {
			m.Keys = append(m.Keys, k)
			m.Vals[k] = jsonToValue(val)
		}
		return value.MapVal(m)
	}
	return value.Nil
}

func jsonEncode(args []value.Value) (value.Value, error) {
	b, err := json.Marshal(valueToJSON(arg(args, 0)))
	if err != nil {
		return value.Nil, err
	}
	return value.StringVal(string(b)), nil
}

func valueToJSON(v value.Value) interface{} {
	switch v.Kind {
	case value.VNil:
		return nil
	case value.VInt:
		return v.Int()
	case value.VFloat:
		return v.Float()
	case value.VString:
		return v.Str()
	case value.VAtom:
		return v.Str()
	case value.VList:
		result := make([]interface{}, len(v.Elems()))
		for i, e := range v.Elems() {
			result[i] = valueToJSON(e)
		}
		return result
	case value.VMap:
		m := v.Map()
		result := make(map[string]interface{})
		for _, k := range m.Keys {
			result[k] = valueToJSON(m.Vals[k])
		}
		return result
	case value.VTuple:
		result := make([]interface{}, len(v.Elems()))
		for i, e := range v.Elems() {
			result[i] = valueToJSON(e)
		}
		return result
	}
	return nil
}
