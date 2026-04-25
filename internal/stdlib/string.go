package stdlib

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"ish/internal/value"
)

func stringModule() *value.OrdMap {
	return makeModule(map[string]func([]value.Value) (value.Value, error){
		"upcase":      func(args []value.Value) (value.Value, error) { return value.StringVal(strings.ToUpper(arg(args, 0).ToStr())), nil },
		"downcase":    func(args []value.Value) (value.Value, error) { return value.StringVal(strings.ToLower(arg(args, 0).ToStr())), nil },
		"trim":        func(args []value.Value) (value.Value, error) { return value.StringVal(strings.TrimSpace(arg(args, 0).ToStr())), nil },
		"length":      func(args []value.Value) (value.Value, error) { return value.IntVal(int64(utf8.RuneCountInString(arg(args, 0).ToStr()))), nil },
		"contains":    func(args []value.Value) (value.Value, error) { return value.BoolVal(strings.Contains(arg(args, 0).ToStr(), arg(args, 1).ToStr())), nil },
		"starts_with": func(args []value.Value) (value.Value, error) { return value.BoolVal(strings.HasPrefix(arg(args, 0).ToStr(), arg(args, 1).ToStr())), nil },
		"ends_with":   func(args []value.Value) (value.Value, error) { return value.BoolVal(strings.HasSuffix(arg(args, 0).ToStr(), arg(args, 1).ToStr())), nil },
		"split": func(args []value.Value) (value.Value, error) {
			parts := strings.Split(arg(args, 0).ToStr(), arg(args, 1).ToStr())
			elems := make([]value.Value, len(parts))
			for i, p := range parts {
				elems[i] = value.StringVal(p)
			}
			return value.ListVal(elems...), nil
		},
		"join": func(args []value.Value) (value.Value, error) {
			if arg(args, 0).Kind != value.VList {
				return value.StringVal(""), nil
			}
			var parts []string
			for _, e := range args[0].Elems() {
				parts = append(parts, e.ToStr())
			}
			return value.StringVal(strings.Join(parts, arg(args, 1).ToStr())), nil
		},
		"replace": func(args []value.Value) (value.Value, error) {
			return value.StringVal(strings.Replace(arg(args, 0).ToStr(), arg(args, 1).ToStr(), arg(args, 2).ToStr(), 1)), nil
		},
		"replace_all": func(args []value.Value) (value.Value, error) {
			return value.StringVal(strings.ReplaceAll(arg(args, 0).ToStr(), arg(args, 1).ToStr(), arg(args, 2).ToStr())), nil
		},
		"slice": func(args []value.Value) (value.Value, error) {
			s := arg(args, 0).ToStr()
			start := int(arg(args, 1).Int())
			length := int(arg(args, 2).Int())
			if start < 0 || start >= len(s) {
				return value.StringVal(""), nil
			}
			end := start + length
			if end > len(s) {
				end = len(s)
			}
			return value.StringVal(s[start:end]), nil
		},
		"chars": func(args []value.Value) (value.Value, error) {
			s := arg(args, 0).ToStr()
			elems := make([]value.Value, len(s))
			for i, ch := range s {
				elems[i] = value.StringVal(string(ch))
			}
			return value.ListVal(elems...), nil
		},
		"pad_left": func(args []value.Value) (value.Value, error) {
			s := arg(args, 0).ToStr()
			width := int(arg(args, 1).Int())
			pad := " "
			if len(args) > 2 {
				pad = arg(args, 2).ToStr()
			}
			for len(s) < width {
				s = pad + s
			}
			return value.StringVal(s[:width]), nil
		},
		"pad_right": func(args []value.Value) (value.Value, error) {
			s := arg(args, 0).ToStr()
			width := int(arg(args, 1).Int())
			pad := " "
			if len(args) > 2 {
				pad = arg(args, 2).ToStr()
			}
			for len(s) < width {
				s = s + pad
			}
			return value.StringVal(s[:width]), nil
		},
		"index_of": func(args []value.Value) (value.Value, error) {
			return value.IntVal(int64(strings.Index(arg(args, 0).ToStr(), arg(args, 1).ToStr()))), nil
		},
		"capitalize": func(args []value.Value) (value.Value, error) {
			s := arg(args, 0).ToStr()
			if len(s) == 0 { return value.StringVal(""), nil }
			r, size := utf8.DecodeRuneInString(s)
			return value.StringVal(string(unicode.ToUpper(r)) + s[size:]), nil
		},
		"trim_leading":  func(args []value.Value) (value.Value, error) { return value.StringVal(strings.TrimLeftFunc(arg(args, 0).ToStr(), unicode.IsSpace)), nil },
		"trim_trailing": func(args []value.Value) (value.Value, error) { return value.StringVal(strings.TrimRightFunc(arg(args, 0).ToStr(), unicode.IsSpace)), nil },
		"reverse": func(args []value.Value) (value.Value, error) {
			runes := []rune(arg(args, 0).ToStr())
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 { runes[i], runes[j] = runes[j], runes[i] }
			return value.StringVal(string(runes)), nil
		},
	})
}
