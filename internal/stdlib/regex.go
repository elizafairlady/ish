package stdlib

import (
	"fmt"
	"regexp"

	"ish/internal/core"
)

func compilePattern(fnName string, v core.Value) (*regexp.Regexp, error) {
	if v.Kind != core.VString {
		return nil, fmt.Errorf("%s: pattern must be a string, got %s", fnName, v.Inspect())
	}
	re, err := regexp.Compile(v.Str)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", fnName, err)
	}
	return re, nil
}

func regexMatch(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("match: expected 2 arguments, got %d", len(args))
	}
	re, err := compilePattern("match", args[1])
	if err != nil {
		return core.Nil, err
	}
	return boolAtom(re.MatchString(args[0].ToStr())), nil
}

func regexScan(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("scan: expected 2 arguments, got %d", len(args))
	}
	re, err := compilePattern("scan", args[1])
	if err != nil {
		return core.Nil, err
	}
	matches := re.FindAllString(args[0].ToStr(), -1)
	elems := make([]core.Value, len(matches))
	for i, m := range matches {
		elems[i] = core.StringVal(m)
	}
	return core.ListVal(elems...), nil
}

func regexReplace(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 3 {
		return core.Nil, fmt.Errorf("replace: expected 3 arguments, got %d", len(args))
	}
	re, err := compilePattern("replace", args[1])
	if err != nil {
		return core.Nil, err
	}
	input := args[0].ToStr()
	repl := args[2].ToStr()
	idx := re.FindStringIndex(input)
	if idx == nil {
		return core.StringVal(input), nil
	}
	return core.StringVal(input[:idx[0]] + repl + input[idx[1]:]), nil
}

func regexReplaceAll(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 3 {
		return core.Nil, fmt.Errorf("replace_all: expected 3 arguments, got %d", len(args))
	}
	re, err := compilePattern("replace_all", args[1])
	if err != nil {
		return core.Nil, err
	}
	return core.StringVal(re.ReplaceAllString(args[0].ToStr(), args[2].ToStr())), nil
}

func regexSplit(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return core.Nil, fmt.Errorf("split: expected 2 arguments, got %d", len(args))
	}
	re, err := compilePattern("split", args[1])
	if err != nil {
		return core.Nil, err
	}
	parts := re.Split(args[0].ToStr(), -1)
	elems := make([]core.Value, len(parts))
	for i, p := range parts {
		elems[i] = core.StringVal(p)
	}
	return core.ListVal(elems...), nil
}
