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

// TestEnv creates a fully-initialized environment for tests.
func TestEnv() *core.Env {
	env := core.TopEnv()
	env.Ctx.Proc = process.NewProcess()
	stdlib.Register(env)
	builtin.Init(builtin.EvalContext{RunSource: eval.RunSource})
	env.Ctx.CmdSub = eval.RunCmdSub
	env.Ctx.CallFn = eval.CallFn
	stdlib.LoadPrelude(env, func(src string, e *core.Env) {
		eval.RunSource(src, e) //nolint: errcheck
	})
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
