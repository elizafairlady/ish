package builtin

import (
	"fmt"
	"os"
	"strings"

	"ish/internal/core"
)

func builtinCd(args []string, scope core.Scope) (int, error) {
	env := scope.NearestEnv()
	dir := ""
	if len(args) == 0 {
		if v, ok := scope.Get("HOME"); ok {
			dir = v.ToStr()
		}
		if dir == "" {
			return 1, fmt.Errorf("cd: HOME not set")
		}
	} else {
		dir = args[0]
	}
	if dir == "-" {
		if v, ok := scope.Get("OLDPWD"); ok {
			dir = v.ToStr()
		}
	}

	if !strings.HasPrefix(dir, "/") && !strings.HasPrefix(dir, "./") && !strings.HasPrefix(dir, "../") && dir != "-" {
		if cdpath, ok := scope.Get("CDPATH"); ok {
			for _, prefix := range strings.Split(cdpath.ToStr(), ":") {
				if prefix == "" {
					prefix = "."
				}
				candidate := prefix + "/" + dir
				if info, err := os.Stat(candidate); err == nil && info.IsDir() {
					dir = candidate
					fmt.Fprintln(scope.GetCtx().Stdout, dir)
					break
				}
			}
		}
	}

	old, _ := os.Getwd()
	err := os.Chdir(dir)
	if err != nil {
		return 1, fmt.Errorf("cd: %s: %s", dir, err)
	}
	env.Export("OLDPWD", old)
	cwd, _ := os.Getwd()
	env.Export("PWD", cwd)
	return 0, nil
}
