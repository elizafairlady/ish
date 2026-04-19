package eval

import (
	"os"
	"strings"
	"syscall"

	"ish/internal/ast"
	"ish/internal/core"
)

func evalAndList(node *ast.Node, env *core.Env) (core.Value, error) {
	left, err := Eval(node.Children[0], env)
	if err != nil {
		return left, err
	}
	syncExit(left, env)
	if env.ExitCode() == 0 {
		right, err := Eval(node.Children[1], env)
		if err == nil {
			syncExit(right, env)
		}
		return right, err
	}
	return left, nil
}

func evalOrList(node *ast.Node, env *core.Env) (core.Value, error) {
	left, err := Eval(node.Children[0], env)
	if err != nil {
		return left, err
	}
	syncExit(left, env)
	if env.ExitCode() != 0 {
		right, err := Eval(node.Children[1], env)
		if err == nil {
			syncExit(right, env)
		}
		return right, err
	}
	return left, nil
}

func evalSubshell(node *ast.Node, env *core.Env) (core.Value, error) {
	osMu.Lock()
	origCwd, _ := os.Getwd()
	origMask := syscall.Umask(0)
	syscall.Umask(origMask)

	subEnv := core.CopyEnv(env)
	val, err := Eval(node.Children[0], subEnv)

	os.Chdir(origCwd)
	syscall.Umask(origMask)
	osMu.Unlock()

	env.SetExit(subEnv.ExitCode())

	return val, err
}

func evalGroup(node *ast.Node, env *core.Env) (core.Value, error) {
	return Eval(node.Children[0], env)
}

func evalRedir(node *ast.Node, env *core.Env) (core.Value, error) {
	return Eval(node.Children[0], env)
}

func evalIf(node *ast.Node, env *core.Env) (core.Value, error) {
	for _, clause := range node.Clauses {
		if clause.Pattern == nil {
			return Eval(clause.Body, env)
		}

		condVal, err := Eval(clause.Pattern, env)
		if err != nil {
			return core.Nil, err
		}

		syncExit(condVal, env)

		if env.ExitCode() == 0 {
			return Eval(clause.Body, env)
		}
	}
	return core.Nil, nil
}

func evalFor(node *ast.Node, env *core.Env) (core.Value, error) {
	varName := node.Children[0].Tok.Val
	words := node.Children[1:]
	body := node.Clauses[0].Body

	var last core.Value
	for _, w := range words {
		v, err := evalCmdArg(w, env)
		if err != nil {
			return core.Nil, err
		}
		val := v.ToStr()
		fields := strings.Fields(val)
		for _, field := range fields {
			expanded := expandGlob(field)
			for _, v := range expanded {
				env.Set(varName, core.StringVal(v))
				var err error
				last, err = Eval(body, env)
				if err == core.ErrBreak {
					return last, nil
				}
				if err == core.ErrContinue {
					continue
				}
				if err != nil {
					return last, err
				}
			}
		}
	}
	return last, nil
}

func evalWhileUntil(node *ast.Node, env *core.Env, invert bool) (core.Value, error) {
	cond := node.Children[0]
	body := node.Clauses[0].Body
	var last core.Value
	for {
		condVal, err := Eval(cond, env)
		if err != nil {
			return core.Nil, err
		}
		syncExit(condVal, env)
		shouldRun := env.ExitCode() == 0
		if invert {
			shouldRun = !shouldRun
		}
		if !shouldRun {
			break
		}
		last, err = Eval(body, env)
		if err == core.ErrBreak {
			env.SetExit(0)
			return last, nil
		}
		if err == core.ErrContinue {
			continue
		}
		if err != nil {
			return last, err
		}
	}
	env.SetExit(0)
	return last, nil
}

func evalCase(node *ast.Node, env *core.Env) (core.Value, error) {
	word := env.Expand(node.Children[0].Tok.Val)
	for _, clause := range node.Clauses {
		patStr := clause.Pattern.Tok.Val
		alternatives := strings.Split(patStr, "|")
		for _, alt := range alternatives {
			if alt == "*" || matchPattern(alt, word) {
				return Eval(clause.Body, env)
			}
		}
	}
	return core.Nil, nil
}
