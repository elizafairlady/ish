package stdlib

import (
	"regexp"

	"ish/internal/value"
)

func regexModule() *value.OrdMap {
	return makeModule(map[string]func([]value.Value) (value.Value, error){
		"match": func(args []value.Value) (value.Value, error) {
			re, err := regexp.Compile(arg(args, 1).ToStr())
			if err != nil {
				return value.Nil, err
			}
			m := re.FindString(arg(args, 0).ToStr())
			if m == "" {
				return value.Nil, nil
			}
			return value.StringVal(m), nil
		},
		"scan": func(args []value.Value) (value.Value, error) {
			re, err := regexp.Compile(arg(args, 1).ToStr())
			if err != nil {
				return value.Nil, err
			}
			matches := re.FindAllString(arg(args, 0).ToStr(), -1)
			elems := make([]value.Value, len(matches))
			for i, m := range matches {
				elems[i] = value.StringVal(m)
			}
			return value.ListVal(elems...), nil
		},
		"replace": func(args []value.Value) (value.Value, error) {
			re, err := regexp.Compile(arg(args, 1).ToStr())
			if err != nil {
				return value.Nil, err
			}
			return value.StringVal(re.ReplaceAllString(arg(args, 0).ToStr(), arg(args, 2).ToStr())), nil
		},
		"replace_all": func(args []value.Value) (value.Value, error) {
			re, err := regexp.Compile(arg(args, 1).ToStr())
			if err != nil {
				return value.Nil, err
			}
			return value.StringVal(re.ReplaceAllString(arg(args, 0).ToStr(), arg(args, 2).ToStr())), nil
		},
		"split": func(args []value.Value) (value.Value, error) {
			re, err := regexp.Compile(arg(args, 1).ToStr())
			if err != nil {
				return value.Nil, err
			}
			parts := re.Split(arg(args, 0).ToStr(), -1)
			elems := make([]value.Value, len(parts))
			for i, p := range parts {
				elems[i] = value.StringVal(p)
			}
			return value.ListVal(elems...), nil
		},
	})
}
