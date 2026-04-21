package builtin

import (
	"ish/internal/core"
	"ish/internal/jobs"
)

// BuiltinFunc is the signature for POSIX shell builtins.
// Builtins are commands — they receive string arguments, same as any external
// command. The evaluator converts values to strings before calling builtins,
// just as it does for exec.Command.
type BuiltinFunc func(args []string, env *core.Env) (int, error)

// EvalContext provides callbacks that builtins need from eval, breaking the import cycle.
type EvalContext struct {
	RunSource func(src string, env *core.Env) (core.Value, error)
}

var evalCtx EvalContext

// Init sets the eval callbacks. Must be called before any builtin that needs eval.
func Init(ctx EvalContext) {
	evalCtx = ctx
}

// Builtins is the map of all builtin commands.
var Builtins map[string]BuiltinFunc

func init() {
	Builtins = map[string]BuiltinFunc{
		// echo.go
		"echo": builtinEcho,
		// cd.go
		"cd": builtinCd,
		// flow.go
		"exit":     builtinExit,
		"logout":   builtinLogout,
		"return":   builtinReturn,
		"break":    builtinBreak,
		"continue": builtinContinue,
		":":        builtinTrue,
		"true":     builtinTrue,
		"false":    builtinFalse,
		// vars.go
		"export":   builtinExport,
		"unset":    builtinUnset,
		"set":      builtinSet,
		"shift":    builtinShift,
		"local":    builtinLocal,
		"readonly": builtinReadonly,
		// test.go
		"test": builtinTest,
		"[":    builtinTest,
		// io.go
		"read":   builtinRead,
		"exec":   builtinExec,
		"eval":   builtinEval,
		"source": builtinSource,
		".":      builtinSource,
		"printf": builtinPrintf,
		// trap.go
		"trap": builtinTrap,
		// process.go
		"wait": builtinWait,
		"kill": builtinKill,
		// system.go
		"type":    builtinType,
		"pwd":     builtinPwd,
		"times":   builtinTimes,
		"umask":   builtinUmask,
		"ulimit":  builtinUlimit,
		"getopts": builtinGetopts,
		// alias.go
		"alias":     builtinAlias,
		"unalias":   builtinUnalias,
		"command":   builtinCommand,
		"delete_fn": builtinDeleteFn,
		// jobs (from jobs package)
		"jobs": jobs.BuiltinJobs,
		"fg":   jobs.BuiltinFg,
		"bg":   jobs.BuiltinBg,
	}
}
