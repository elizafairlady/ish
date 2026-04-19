package builtin

import (
	"fmt"
	"os"
	"strconv"

	"ish/internal/core"
)

func builtinExit(args []string, env *core.Env) (int, error) {
	code := 0
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "exit: %s: numeric argument required\n", args[0])
			os.Exit(2)
		}
		code = n
	}
	os.Exit(code)
	return code, nil
}

func builtinReturn(args []string, env *core.Env) (int, error) {
	code := 0
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return 1, fmt.Errorf("return: %s: numeric argument required", args[0])
		}
		code = n
	}
	env.SetExit(code)
	return code, core.ErrReturn
}

func builtinBreak(args []string, env *core.Env) (int, error) {
	return 0, core.ErrBreak
}

func builtinContinue(args []string, env *core.Env) (int, error) {
	return 0, core.ErrContinue
}

func builtinTrue(args []string, env *core.Env) (int, error) {
	return 0, nil
}

func builtinFalse(args []string, env *core.Env) (int, error) {
	return 1, nil
}
