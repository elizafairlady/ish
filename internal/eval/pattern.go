package eval

import (
	"fmt"

	"ish/internal/ast"
	"ish/internal/core"
)

func PatternBind(pat *ast.Node, val core.Value, env *core.Env) error {
	switch pat.Kind {
	case ast.NIdent:
		if pat.Tok.Val == "_" {
			return nil
		}
		if err := env.SetLocal(pat.Tok.Val, val); err != nil {
			return err
		}
		return nil
	case ast.NLit:
		expected, _ := litToValue(pat)
		if !expected.Equal(val) {
			return fmt.Errorf("match error: expected %s, got %s", expected.Inspect(), val.Inspect())
		}
		return nil
	case ast.NTuple:
		if val.Kind != core.VTuple || len(val.Elems) != len(pat.Children) {
			return fmt.Errorf("match error: expected %d-tuple, got %s", len(pat.Children), val.Inspect())
		}
		for i, child := range pat.Children {
			if err := PatternBind(child, val.Elems[i], env); err != nil {
				return err
			}
		}
		return nil
	case ast.NList:
		if val.Kind != core.VList {
			return fmt.Errorf("match error: expected list, got %s", val.Inspect())
		}
		if pat.Rest != nil {
			if len(val.Elems) < len(pat.Children) {
				return fmt.Errorf("match error: list has %d elements, need at least %d", len(val.Elems), len(pat.Children))
			}
			for i, child := range pat.Children {
				if err := PatternBind(child, val.Elems[i], env); err != nil {
					return err
				}
			}
			remaining := val.Elems[len(pat.Children):]
			restVal := core.ListVal(remaining...)
			return PatternBind(pat.Rest, restVal, env)
		}
		if len(pat.Children) != len(val.Elems) {
			return fmt.Errorf("match error: list length mismatch")
		}
		for i, child := range pat.Children {
			if err := PatternBind(child, val.Elems[i], env); err != nil {
				return err
			}
		}
		return nil
	case ast.NMap:
		if val.Kind != core.VMap || val.Map == nil {
			return fmt.Errorf("match error: expected map, got %s", val.Inspect())
		}
		for i := 0; i+1 < len(pat.Children); i += 2 {
			key := pat.Children[i].Tok.Val
			mapVal, ok := val.Map.Get(key)
			if !ok {
				return fmt.Errorf("match error: key %q not found in map", key)
			}
			if err := PatternBind(pat.Children[i+1], mapVal, env); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported pattern kind: %d", pat.Kind)
}

func PatternMatches(pat *ast.Node, val core.Value, env *core.Env) bool {
	switch pat.Kind {
	case ast.NIdent:
		return true
	case ast.NLit:
		expected, _ := litToValue(pat)
		return expected.Equal(val)
	case ast.NTuple:
		if val.Kind != core.VTuple || len(val.Elems) != len(pat.Children) {
			return false
		}
		for i, child := range pat.Children {
			if !PatternMatches(child, val.Elems[i], env) {
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
				if !PatternMatches(child, val.Elems[i], env) {
					return false
				}
			}
			return PatternMatches(pat.Rest, core.ListVal(val.Elems[len(pat.Children):]...), env)
		}
		if len(val.Elems) != len(pat.Children) {
			return false
		}
		for i, child := range pat.Children {
			if !PatternMatches(child, val.Elems[i], env) {
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
			if !PatternMatches(pat.Children[i+1], mapVal, env) {
				return false
			}
		}
		return true
	}
	return false
}
