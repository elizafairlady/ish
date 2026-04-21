package eval

import (
	"fmt"

	"ish/internal/ast"
	"ish/internal/core"
)

// TryBind attempts to match a pattern against a value. If env is non-nil,
// variables are bound into the env on success. If env is nil, only tests
// whether the pattern matches (no side effects).
//
// Returns true if the pattern matched. On false, any partial bindings in
// env are the caller's responsibility to discard (typically via ResetFlat).
func TryBind(pat *ast.Node, val core.Value, env *core.Env) bool {
	switch pat.Kind {
	case ast.NIdent:
		if env != nil && pat.Tok.Val != "_" {
			env.SetLocal(pat.Tok.Val, val) //nolint: errcheck
		}
		return true
	case ast.NLit:
		expected, _ := litToValue(pat)
		return expected.Equal(val)
	case ast.NTuple:
		if val.Kind != core.VTuple || len(val.Elems) != len(pat.Children) {
			return false
		}
		for i, child := range pat.Children {
			if !TryBind(child, val.Elems[i], env) {
				return false
			}
		}
		return true
	case ast.NList:
		if val.Kind != core.VList {
			return false
		}
		if pat.Rest != nil {
			if len(val.Elems) < len(pat.Children) {
				return false
			}
			for i, child := range pat.Children {
				if !TryBind(child, val.Elems[i], env) {
					return false
				}
			}
			remaining := val.Elems[len(pat.Children):]
			return TryBind(pat.Rest, core.ListVal(remaining...), env)
		}
		if len(val.Elems) != len(pat.Children) {
			return false
		}
		for i, child := range pat.Children {
			if !TryBind(child, val.Elems[i], env) {
				return false
			}
		}
		return true
	case ast.NMap:
		if val.Kind != core.VMap || val.Map == nil {
			return false
		}
		for i := 0; i+1 < len(pat.Children); i += 2 {
			key := pat.Children[i].Tok.Val
			mapVal, ok := val.Map.Get(key)
			if !ok {
				return false
			}
			if !TryBind(pat.Children[i+1], mapVal, env) {
				return false
			}
		}
		return true
	}
	return false
}

// PatternBind binds a pattern into env, returning a descriptive error on mismatch.
// Used by evalMatch (the = operator) where a specific error message is needed.
func PatternBind(pat *ast.Node, val core.Value, env *core.Env) error {
	if TryBind(pat, val, env) {
		return nil
	}
	return patternError(pat, val)
}

// patternError produces a descriptive error for a failed pattern match.
func patternError(pat *ast.Node, val core.Value) error {
	switch pat.Kind {
	case ast.NLit:
		expected, _ := litToValue(pat)
		return fmt.Errorf("match error: expected %s, got %s", expected.Inspect(), val.Inspect())
	case ast.NTuple:
		if val.Kind != core.VTuple {
			return fmt.Errorf("match error: expected tuple, got %s", val.Inspect())
		}
		if len(val.Elems) != len(pat.Children) {
			return fmt.Errorf("match error: expected %d-tuple, got %d-tuple", len(pat.Children), len(val.Elems))
		}
		for i, child := range pat.Children {
			if !TryBind(child, val.Elems[i], nil) {
				return patternError(child, val.Elems[i])
			}
		}
	case ast.NList:
		if val.Kind != core.VList {
			return fmt.Errorf("match error: expected list, got %s", val.Inspect())
		}
		return fmt.Errorf("match error: list length mismatch")
	case ast.NMap:
		if val.Kind != core.VMap || val.Map == nil {
			return fmt.Errorf("match error: expected map, got %s", val.Inspect())
		}
		for i := 0; i+1 < len(pat.Children); i += 2 {
			key := pat.Children[i].Tok.Val
			if _, ok := val.Map.Get(key); !ok {
				return fmt.Errorf("match error: key %q not found in map", key)
			}
		}
	}
	return fmt.Errorf("match error: no match for %s", val.Inspect())
}
