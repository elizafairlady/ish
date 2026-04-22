package eval

import (
	"fmt"
	"path/filepath"
	"strings"

	"ish/internal/core"
)

func expandGlobs(args []string) []string {
	var result []string
	for _, arg := range args {
		result = append(result, expandGlob(arg)...)
	}
	return result
}

func expandGlobsSelective(args []string, quoted []bool) []string {
	var result []string
	for i, arg := range args {
		if i < len(quoted) && quoted[i] {
			result = append(result, arg)
		} else {
			result = append(result, expandGlob(arg)...)
		}
	}
	return result
}

func expandGlob(pattern string) []string {
	if !strings.ContainsAny(pattern, "*?[") {
		return []string{pattern}
	}
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return []string{pattern}
	}
	return matches
}

func matchPattern(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	matched, _ := filepath.Match(pattern, s)
	return matched
}

func checkUnsetVars(s string, scope core.Scope) error {
	i := 0
	for i < len(s) {
		if s[i] != '$' {
			i++
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		ch := s[i]
		if ch == '?' || ch == '$' || ch == '!' || ch == '@' || ch == '*' || ch == '#' {
			i++
			continue
		}
		if ch >= '0' && ch <= '9' {
			i++
			continue
		}
		if ch == '{' {
			i++
			start := i
			for i < len(s) && s[i] != '}' {
				i++
			}
			expr := s[start:i]
			if i < len(s) {
				i++
			}
			if strings.ContainsAny(expr, ":-+?=") {
				continue
			}
			if _, ok := scope.Get(expr); !ok {
				return fmt.Errorf("%s: unbound variable", expr)
			}
			continue
		}
		if ch == '(' {
			i++
			continue
		}
		start := i
		for i < len(s) && core.IsVarChar(s[i]) {
			i++
		}
		varName := s[start:i]
		if varName == "" {
			continue
		}
		if _, ok := scope.Get(varName); !ok {
			return fmt.Errorf("%s: unbound variable", varName)
		}
	}
	return nil
}
