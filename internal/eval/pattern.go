package eval

import (
	"fmt"

	"ish/internal/ast"
	"ish/internal/core"
)

// TryBind attempts to match a pattern against a value and bind variables.
// Returns true if the pattern matched. On false, partial bindings are the
// caller's responsibility to discard (typically via ResetFlat).
func TryBind(pat *ast.Node, val core.Value, scope core.Scope) bool {
	switch pat.Kind {
	case ast.NIdent:
		if pat.Tok.Val == "_" { return true }
		if scope != nil { scope.SetLocal(pat.Tok.Val, val) }
		return true
	case ast.NLit:
		expected, _ := litToValue(pat)
		return expected.Equal(val)
	case ast.NTuple:
		if val.Kind != core.VTuple { return false }
		if len(val.GetElems()) != len(pat.Children) { return false }
		for i, c := range pat.Children {
			if !TryBind(c, val.GetElems()[i], scope) { return false }
		}
		return true
	case ast.NList:
		if val.Kind != core.VList { return false }
		elems := val.GetElems()
		if pat.Rest != nil {
			if len(elems) < len(pat.Children) { return false }
			for i, c := range pat.Children {
				if !TryBind(c, elems[i], scope) { return false }
			}
			rest := core.ListVal(elems[len(pat.Children):]...)
			return TryBind(pat.Rest, rest, scope)
		}
		if len(elems) != len(pat.Children) { return false }
		for i, c := range pat.Children {
			if !TryBind(c, elems[i], scope) { return false }
		}
		return true
	case ast.NMap:
		if val.Kind != core.VMap || val.GetMap() == nil { return false }
		for i := 0; i+1 < len(pat.Children); i += 2 {
			key := pat.Children[i].Tok.Val
			mapVal, ok := val.GetMap().Get(key)
			if !ok { return false }
			if !TryBind(pat.Children[i+1], mapVal, scope) { return false }
		}
		return true
	}
	return false
}

// PatternBind binds a pattern into scope, returning a descriptive error on mismatch.
func PatternBind(pat *ast.Node, val core.Value, scope core.Scope) error {
	if TryBind(pat, val, scope) {
		return nil
	}
	return patternError(pat, val)
}

func patternError(pat *ast.Node, val core.Value) error {
	switch pat.Kind {
	case ast.NLit:
		expected, _ := litToValue(pat)
		return fmt.Errorf("match error: expected %s, got %s", expected.Inspect(), val.Inspect())
	case ast.NTuple:
		if val.Kind != core.VTuple {
			return fmt.Errorf("match error: expected tuple, got %s", val.Inspect())
		}
		if len(val.GetElems()) != len(pat.Children) {
			return fmt.Errorf("match error: expected %d-tuple, got %d-tuple", len(pat.Children), len(val.GetElems()))
		}
		for i, child := range pat.Children {
			if !TryBind(child, val.GetElems()[i], nil) {
				return patternError(child, val.GetElems()[i])
			}
		}
	case ast.NList:
		if val.Kind != core.VList {
			return fmt.Errorf("match error: expected list, got %s", val.Inspect())
		}
		return fmt.Errorf("match error: list length mismatch")
	case ast.NMap:
		if val.Kind != core.VMap || val.GetMap() == nil {
			return fmt.Errorf("match error: expected map, got %s", val.Inspect())
		}
		for i := 0; i+1 < len(pat.Children); i += 2 {
			key := pat.Children[i].Tok.Val
			if _, ok := val.GetMap().Get(key); !ok {
				return fmt.Errorf("match error: key %q not found in map", key)
			}
		}
	}
	return fmt.Errorf("match error: no match for %s", val.Inspect())
}
