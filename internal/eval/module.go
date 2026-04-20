package eval

import (
	"fmt"
	"strings"

	"ish/internal/ast"
	"ish/internal/core"
)

func evalDefModule(node *ast.Node, env *core.Env) (core.Value, error) {
	modName := node.Tok.Val
	modEnv := core.NewEnv(env)

	// Pre-register a placeholder module so the body can self-reference
	// (e.g. M.bar inside defmodule M). SetModule merges if it already exists.
	env.SetModule(modName, &core.Module{
		Name:      modName,
		Fns:       make(map[string]*core.FnValue),
		NativeFns: make(map[string]core.NativeFn),
	})
	mod, _ := env.GetModule(modName)

	for _, child := range node.Children {
		if _, err := Eval(child, modEnv); err != nil {
			return core.Nil, err
		}
		// Sync exported functions into the module after each definition
		// so later definitions can reference earlier ones via module name.
		for name, fn := range modEnv.Fns {
			if !strings.HasPrefix(name, "_") {
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

func evalUse(node *ast.Node, env *core.Env) (core.Value, error) {
	modName := node.Tok.Val
	mod, ok := env.GetModule(modName)
	if !ok {
		return core.Nil, fmt.Errorf("use: module %s not found", modName)
	}
	for name, fn := range mod.Fns {
		env.SetFnClauses(name, fn)
	}
	for name, nfn := range mod.NativeFns {
		env.SetNativeFn(name, nfn)
	}
	return core.Nil, nil
}
