package builtin

import (
	"fmt"
	"os"
	"strconv"

	"ish/internal/core"
)

func builtinExit(args []string, scope core.Scope) (int, error) {
	code := 0
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "exit: %s: numeric argument required\n", args[0])
			code = 2
		} else {
			code = n
		}
	}
	scope.GetCtx().SetExit(code)
	return code, core.ErrExit
}

func builtinLogout(args []string, scope core.Scope) (int, error) {
	if !scope.GetCtx().IsLoginShell {
		return 1, fmt.Errorf("logout: not login shell: use 'exit'")
	}
	return builtinExit(args, scope)
}

func builtinReturn(args []string, scope core.Scope) (int, error) {
	code := 0
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return 1, fmt.Errorf("return: %s: numeric argument required", args[0])
		}
		code = n
	}
	scope.GetCtx().SetExit(code)
	return code, core.ErrReturn
}

func builtinBreak(args []string, scope core.Scope) (int, error) {
	return 0, core.ErrBreak
}

func builtinContinue(args []string, scope core.Scope) (int, error) {
	return 0, core.ErrContinue
}

func builtinTrue(args []string, scope core.Scope) (int, error) {
	return 0, nil
}

func builtinFalse(args []string, scope core.Scope) (int, error) {
	return 1, nil
}
