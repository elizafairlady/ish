package builtin

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"ish/internal/core"
)

func builtinAlias(args []string, env *core.Env) (int, error) {
	if len(args) == 0 {
		for k, v := range env.AllAliases() {
			fmt.Fprintf(env.Stdout(), "alias %s='%s'\n", k, v)
		}
		return 0, nil
	}
	for _, arg := range args {
		if idx := strings.IndexByte(arg, '='); idx >= 0 {
			env.SetAlias(arg[:idx], arg[idx+1:])
		} else {
			if v, ok := env.GetAlias(arg); ok {
				fmt.Fprintf(env.Stdout(), "alias %s='%s'\n", arg, v)
			} else {
				fmt.Fprintf(os.Stderr, "alias: %s: not found\n", arg)
			}
		}
	}
	return 0, nil
}

func builtinUnalias(args []string, env *core.Env) (int, error) {
	for _, arg := range args {
		if arg == "-a" {
			if env.Shell != nil {
				env.Shell.Aliases = nil
			}
		} else {
			env.DeleteAlias(arg)
		}
	}
	return 0, nil
}

func builtinCommand(args []string, env *core.Env) (int, error) {
	if len(args) == 0 {
		return 0, nil
	}
	if args[0] == "-v" && len(args) > 1 {
		name := args[1]
		if _, ok := Builtins[name]; ok {
			fmt.Fprintln(env.Stdout(), name)
			return 0, nil
		}
		if path, err := exec.LookPath(name); err == nil {
			fmt.Fprintln(env.Stdout(), path)
			return 0, nil
		}
		return 1, nil
	}
	if args[0] == "-V" && len(args) > 1 {
		return builtinType(args[1:], env)
	}
	name := args[0]
	cmdArgs := args[1:]
	if b, ok := Builtins[name]; ok {
		return b(cmdArgs, env)
	}
	cmd := exec.Command(name, cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = env.Stdout()
	cmd.Stderr = os.Stderr
	cmd.Env = env.BuildEnv()
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 127, fmt.Errorf("command: %s: %s", name, err)
	}
	return 0, nil
}
