package eval

import (
	"fmt"
	"strings"

	"ish/internal/ast"
	"ish/internal/core"
)

func evalDefModule(node *ast.Node, scope core.Scope) (core.Value, error) {
	modName := node.Tok.Val
	modEnv := core.NewEnv(scope)

	// Pre-register a placeholder module so the body can self-reference
	// (e.g. M.bar inside defmodule M). SetModule merges if it already exists.
	scope.NearestEnv().SetModule(modName, &core.Module{
		Name: modName,
		Fns:  make(map[string]*core.FnValue),
	})
	mod, _ := scope.GetModule(modName)

	// Track names brought in by use/import so they don't leak to the public interface
	importedFns := make(map[string]bool)

	for _, child := range node.Children {
		// use/import inside defmodule: bring functions into modEnv
		// for unqualified access in the body. Does NOT add to the
		// module's public interface (Elixir semantics).
		if child.Kind == ast.NUse || child.Kind == ast.NImport {
			srcName := child.Tok.Val
			srcMod, ok := scope.GetModule(srcName)
			if !ok {
				return core.Nil, fmt.Errorf("use: module %s not found", srcName)
			}
			for name, fn := range srcMod.Fns {
				importedFns[name] = true
				modEnv.SetFnClauses(name, fn)
			}
			continue
		}

		// If a def overrides an imported name, clear the imported version
		// so clauses don't accumulate, and mark it as the module's own.
		if child.Kind == ast.NIshFn {
			fnName := child.Tok.Val
			if importedFns[fnName] {
				delete(modEnv.Fns, fnName)
			}
			delete(importedFns, fnName)
		}

		if _, err := Eval(child, modEnv); err != nil {
			return core.Nil, err
		}

		// Sync exported functions into the module after each definition
		// so later definitions can reference earlier ones via module name.
		// Skip imported names — only the module's own defs are public.
		for name, fn := range modEnv.Fns {
			if !strings.HasPrefix(name, "_") && !importedFns[name] {
				mod.Fns[name] = fn
			}
		}
	}

	// Set closure env on all module functions so they can see each other
	for _, fn := range modEnv.Fns {
		if fn.Env == nil {
			fn.Env = modEnv
		}
	}

	return core.Nil, nil
}

// evalImport copies a module's functions into the current scope for unqualified access.
// This is Elixir's `import` semantics — works both at top level and inside defmodule.
func evalImport(node *ast.Node, scope core.Scope) (core.Value, error) {
	modName := node.Tok.Val
	mod, ok := scope.GetModule(modName)
	if !ok {
		return core.Nil, fmt.Errorf("import: module %s not found", modName)
	}
	env := scope.NearestEnv()
	for name, fn := range mod.Fns {
		if env.Fns == nil { env.Fns = make(map[string]*core.FnValue) }
		env.Fns[name] = fn
	}
	return core.Nil, nil
}

// evalUse outside defmodule retains backward-compatible import semantics.
func evalUse(node *ast.Node, scope core.Scope) (core.Value, error) {
	return evalImport(node, scope)
}
