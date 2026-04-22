package testutil

import (
	"bytes"
	"io"
	"os"

	"ish/internal/builtin"
	"ish/internal/core"
	"ish/internal/eval"
	"ish/internal/process"
	"ish/internal/stdlib"
)

var baseTestEnv *core.Env

func init() {
	baseTestEnv = core.TopEnv()
	baseTestEnv.Ctx.Proc = process.NewProcess()
	stdlib.Register(baseTestEnv)
	builtin.Init(builtin.EvalContext{RunSource: eval.RunSource})
	baseTestEnv.Ctx.CmdSub = eval.RunCmdSub
	baseTestEnv.Ctx.CallFn = eval.CallFn
	stdlib.LoadPrelude(baseTestEnv, func(src string, e *core.Env) {
		eval.RunSource(src, e) //nolint: errcheck
	})
}

// TestEnv creates a fresh top-level environment for tests with shared
// module/function definitions from the prelude. Each test gets its own
// ExecCtx so exit codes, process state, etc. don't leak.
func TestEnv() *core.Env {
	env := core.TopEnv()
	env.Ctx.Proc = process.NewProcess()
	env.Ctx.CmdSub = eval.RunCmdSub
	env.Ctx.CallFn = eval.CallFn
	// Copy modules, functions, and native functions from the prelude-loaded base
	if baseTestEnv.Modules != nil {
		env.Modules = make(map[string]*core.Module, len(baseTestEnv.Modules))
		for name, mod := range baseTestEnv.Modules {
			env.Modules[name] = mod
		}
	}
	if baseTestEnv.Fns != nil {
		env.Fns = make(map[string]*core.FnValue, len(baseTestEnv.Fns))
		for name, fn := range baseTestEnv.Fns {
			env.Fns[name] = fn
		}
	}
	return env
}

// CaptureOutput runs fn and captures what is written to the env's stdout.
func CaptureOutput(env *core.Env, fn func()) string {
	r, w, _ := os.Pipe()
	old := env.Ctx.Stdout
	env.Ctx.Stdout = w
	fn()
	w.Close()
	env.Ctx.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()
	return buf.String()
}

// RunSource is a convenience wrapper around eval.RunSource.
func RunSource(src string, env *core.Env) core.Value {
	val, _ := eval.RunSource(src, env)
	return val
}
