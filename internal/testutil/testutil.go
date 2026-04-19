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
	env.Proc = process.NewProcess()
	stdlib.Register(env)
	builtin.Init(builtin.EvalContext{RunSource: eval.RunSource})
	env.CmdSub = eval.RunCmdSub
	env.CallFn = eval.CallFn
	return env
}

// CaptureOutput runs fn and captures what is written to the env's stdout.
func CaptureOutput(env *core.Env, fn func()) string {
	r, w, _ := os.Pipe()
	env.Stdout_ = w
	fn()
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()
	return buf.String()
}

// RunSource is a convenience wrapper around eval.RunSource.
func RunSource(src string, env *core.Env) core.Value {
	return eval.RunSource(src, env)
}
