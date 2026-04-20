package stdlib

import (
	"fmt"
	"os"
	"path/filepath"

	"ish/internal/core"
)

func pathBasename(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("basename: expected 1 argument, got %d", len(args))
	}
	return core.StringVal(filepath.Base(args[0].ToStr())), nil
}

func pathDirname(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("dirname: expected 1 argument, got %d", len(args))
	}
	return core.StringVal(filepath.Dir(args[0].ToStr())), nil
}

func pathExtname(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("extname: expected 1 argument, got %d", len(args))
	}
	return core.StringVal(filepath.Ext(args[0].ToStr())), nil
}

func pathJoin(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) == 0 {
		return core.StringVal(""), nil
	}
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = a.ToStr()
	}
	return core.StringVal(filepath.Join(parts...)), nil
}

func pathAbs(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("abs: expected 1 argument, got %d", len(args))
	}
	abs, err := filepath.Abs(args[0].ToStr())
	if err != nil {
		return core.Nil, fmt.Errorf("abs: %v", err)
	}
	return core.StringVal(abs), nil
}

func pathExists(args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return core.Nil, fmt.Errorf("exists: expected 1 argument, got %d", len(args))
	}
	_, err := os.Stat(args[0].ToStr())
	return boolAtom(err == nil), nil
}
