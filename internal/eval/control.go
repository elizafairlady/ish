package eval

import (
	"os"
	"strings"
	"syscall"

	"ish/internal/ast"
	"ish/internal/core"
)

func evalAndList(node *ast.Node, scope core.Scope) (core.Value, error) {
	left, err := Eval(node.Children[0], scope)
	if err != nil { return left, err }
	syncExit(left, scope)
	if scope.GetCtx().ExitCode() == 0 {
		right, err := Eval(node.Children[1], scope)
		if err == nil { syncExit(right, scope) }
		return right, err
	}
	return left, nil
}

func evalOrList(node *ast.Node, scope core.Scope) (core.Value, error) {
	left, err := Eval(node.Children[0], scope)
	if err != nil { return left, err }
	syncExit(left, scope)
	if scope.GetCtx().ExitCode() != 0 {
		right, err := Eval(node.Children[1], scope)
		if err == nil { syncExit(right, scope) }
		return right, err
	}
	return left, nil
}

func evalIf(node *ast.Node, scope core.Scope) (core.Value, error) {
	for _, clause := range node.Clauses {
		if clause.Pattern == nil {
			return Eval(clause.Body, scope)
		}
		condVal, err := Eval(clause.Pattern, scope)
		if err != nil { return core.Nil, err }
		syncExit(condVal, scope)
		if scope.GetCtx().ExitCode() == 0 {
			return Eval(clause.Body, scope)
		}
	}
	scope.GetCtx().SetExit(0)
	return core.Nil, nil
}

func evalFor(node *ast.Node, scope core.Scope) (core.Value, error) {
	varName := node.Children[0].Tok.Val
	words := node.Children[1:]
	body := node.Clauses[0].Body

	var last core.Value
	for _, w := range words {
		v, err := Eval(w, scope)
		if err != nil { return core.Nil, err }
		if v.Kind == core.VList {
			for _, elem := range v.GetElems() {
				scope.Set(varName, elem)
				last, err = Eval(body, scope)
				if err == core.ErrBreak { return last, nil }
				if err == core.ErrContinue { continue }
				if err != nil { return last, err }
			}
		} else {
			fields := strings.Fields(v.ToStr())
			for _, field := range fields {
				expanded := expandGlob(field)
				for _, g := range expanded {
					scope.Set(varName, core.StringVal(g))
					last, err = Eval(body, scope)
					if err == core.ErrBreak { return last, nil }
					if err == core.ErrContinue { continue }
					if err != nil { return last, err }
				}
			}
		}
	}
	return last, nil
}

func evalWhileUntil(node *ast.Node, scope core.Scope, invert bool) (core.Value, error) {
	cond := node.Children[0]
	body := node.Clauses[0].Body
	var last core.Value
	for {
		condVal, err := Eval(cond, scope)
		if err != nil { return core.Nil, err }
		syncExit(condVal, scope)
		shouldRun := scope.GetCtx().ExitCode() == 0
		if invert { shouldRun = !shouldRun }
		if !shouldRun { break }
		last, err = Eval(body, scope)
		if err == core.ErrBreak { scope.GetCtx().SetExit(0); return last, nil }
		if err == core.ErrContinue { continue }
		if err != nil { return last, err }
	}
	scope.GetCtx().SetExit(0)
	return last, nil
}

func evalCase(node *ast.Node, scope core.Scope) (core.Value, error) {
	wordVal, err := Eval(node.Children[0], scope)
	if err != nil { return core.Nil, err }
	word := wordVal.ToStr()
	for _, clause := range node.Clauses {
		patStr := clause.Pattern.Tok.Val
		alternatives := strings.Split(patStr, "|")
		for _, alt := range alternatives {
			if alt == "*" || matchPattern(alt, word) {
				return Eval(clause.Body, scope)
			}
		}
	}
	return core.Nil, nil
}

func evalSubshell(node *ast.Node, scope core.Scope) (core.Value, error) {
	origCwd, _ := os.Getwd()
	origMask := syscall.Umask(0)
	syscall.Umask(origMask)

	subEnv := core.CopyEnv(scope.NearestEnv())
	val, err := Eval(node.Children[0], subEnv)

	osMu.Lock()
	os.Chdir(origCwd)
	syscall.Umask(origMask)
	osMu.Unlock()

	scope.GetCtx().SetExit(subEnv.Ctx.ExitCode())
	return val, err
}
