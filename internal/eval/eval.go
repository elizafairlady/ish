package eval

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"ish/internal/ast"
	"ish/internal/core"
	"ish/internal/debug"
	"ish/internal/lexer"
	"ish/internal/parser"
)

// osMu protects process-global OS state (cwd, umask) during subshell execution.
var osMu sync.Mutex

// MakeIsCommand builds a callback for the parser that identifies known commands
// (builtins, PATH executables, user-defined functions, stdlib).
// PATH lookups are cached per callback instance to avoid repeated filesystem scans.
func MakeIsCommand(env *core.Env) func(string) bool {
	return ResolveCmdCached(env)
}

func Eval(node *ast.Node, env *core.Env) (core.Value, error) {
	if node == nil {
		return core.Nil, nil
	}

	if d, ok := env.Debugger.(*debug.Debugger); ok {
		d.SetNode(node.Pos)
		if d.TraceAll {
			d.TraceNode(node)
		}
	}

	switch node.Kind {
	case ast.NBlock:
		return evalBlock(node, env)
	case ast.NCmd:
		return evalCmd(node, env)
	case ast.NAssign:
		return evalPosixAssign(node, env)
	case ast.NMatch:
		return evalMatch(node, env)
	case ast.NPipe:
		return evalPipe(node, env)
	case ast.NPipeFn:
		return evalPipeFn(node, env)
	case ast.NAndList:
		return evalAndList(node, env)
	case ast.NOrList:
		return evalOrList(node, env)
	case ast.NBg:
		return evalBg(node, env)
	case ast.NSubshell:
		return evalSubshell(node, env)
	case ast.NGroup:
		return evalGroup(node, env)
	case ast.NIf:
		return evalIf(node, env)
	case ast.NFor:
		return evalFor(node, env)
	case ast.NWhile:
		return evalWhileUntil(node, env, false)
	case ast.NUntil:
		return evalWhileUntil(node, env, true)
	case ast.NCase:
		return evalCase(node, env)
	case ast.NLit:
		return evalLit(node, env)
	case ast.NIdent:
		return evalIdent(node, env)
	case ast.NVarRef:
		return evalVarRef(node, env)
	case ast.NCall:
		return evalCall(node, env)
	case ast.NCmdSub:
		return evalCmdSubNode(node, env)
	case ast.NArithSub:
		return evalArithSubNode(node, env)
	case ast.NParamExpand:
		return evalParamExpandNode(node, env)
	case ast.NInterpolation:
		return evalInterpolationNode(node, env)
	case ast.NInterpString:
		return evalInterpStringNode(node, env)
	case ast.NArg:
		return evalArg(node, env)
	case ast.NPath:
		return core.StringVal(node.Tok.Val), nil
	case ast.NFlag:
		return core.StringVal(node.Tok.Val), nil
	case ast.NIshIf:
		return evalIshIf(node, env)
	case ast.NBinOp:
		return evalBinOp(node, env)
	case ast.NUnary:
		return evalUnary(node, env)
	case ast.NTuple:
		return evalTuple(node, env)
	case ast.NList:
		return evalList(node, env)
	case ast.NMap:
		return evalMap(node, env)
	case ast.NAccess:
		return evalAccess(node, env)
	case ast.NIshFn:
		return evalIshFn(node, env)
	case ast.NIshMatch:
		return evalIshMatch(node, env)
	case ast.NIshSpawn:
		return evalIshSpawn(node, env)
	case ast.NIshSpawnLink:
		return evalIshSpawnLink(node, env)
	case ast.NIshSend:
		return evalIshSend(node, env)
	case ast.NIshReceive:
		return evalIshReceive(node, env)
	case ast.NIshMonitor:
		return evalIshMonitor(node, env)
	case ast.NIshAwait:
		return evalIshAwait(node, env)
	case ast.NIshSupervise:
		return evalIshSupervise(node, env)
	case ast.NIshTry:
		return evalIshTry(node, env)
	case ast.NFnDef:
		return evalPosixFnDef(node, env)
	case ast.NLambda:
		return evalLambda(node, env)
	case ast.NDefModule:
		return evalDefModule(node, env)
	case ast.NUse:
		return evalUse(node, env)
	case ast.NCapture:
		return evalCapture(node, env)
	default:
		return core.Nil, fmt.Errorf("unknown node kind: %d", node.Kind)
	}
}

func evalBlock(node *ast.Node, env *core.Env) (core.Value, error) {
	var last core.Value
	for _, child := range node.Children {
		v, err := Eval(child, env)
		if err != nil {
			return v, err
		}
		last = v

		// set -e and trap ERR exemptions: if, while, until, &&, ||, ! negation
		exempt := child.Kind == ast.NIf || child.Kind == ast.NWhile ||
			child.Kind == ast.NUntil || child.Kind == ast.NAndList ||
			child.Kind == ast.NOrList ||
			(child.Kind == ast.NUnary && child.Tok.Type == ast.TBang)

		if !exempt {
			// trap ERR: fire if last command failed
			if env.ExitCode() != 0 {
				if cmd, ok := env.GetTrap("ERR"); ok && cmd != "" {
					RunSource(cmd, env) //nolint: errcheck — trap handler
				}
			}
			if env.ShouldExitOnError() {
				return last, core.ErrSetE
			}
		}
	}
	return last, nil
}

func syncExit(val core.Value, env *core.Env) {
	if val.Kind != core.VNil {
		if val.Truthy() {
			env.SetExit(0)
		} else {
			env.SetExit(1)
		}
	}
}

// CallFn calls a user-defined function with Value arguments.
func CallFn(fn *core.FnValue, vals []core.Value, env *core.Env) (retVal core.Value, retErr error) {
	if fn.Native != nil {
		return fn.Native(vals, env)
	}
	dbg, hasDbg := env.Debugger.(*debug.Debugger)
	var tcoDepth int
	if hasDbg {
		dbg.PushFrame(fn.Name, len(vals))
		defer func() {
			if retErr != nil {
				retErr = dbg.WrapError(retErr)
			}
			for i := 0; i < tcoDepth; i++ {
				dbg.PopFrame()
			}
			dbg.PopFrame()
		}()
	}

	parentEnv := env
	if fn.Env != nil {
		parentEnv = fn.Env
	}

	for {
		// Check Native at the top of the loop — a tail call may have
		// replaced fn with a native function.
		if fn.Native != nil {
			return fn.Native(vals, env)
		}

		matched := false
		var guardErr error
		fnEnv := core.NewFlatEnv(parentEnv)
		for _, clause := range fn.Clauses {
			if len(clause.Params) == 0 {
				// POSIX function path: convert args to strings for $1/$@ access.
				strArgs := make([]string, len(vals))
				for i, v := range vals {
					strArgs[i] = v.ToStr()
				}
				fnEnv.Args = strArgs
				val, err := Eval(clause.Body, fnEnv)
				if err == core.ErrReturn {
					env.SetExit(fnEnv.ExitCode())
					return val, nil
				}
				if err != nil {
					return val, err
				}
				if val.Kind == core.VTailCall {
					fn = val.TailFn
					vals = val.TailArgs
					if fn.Env != nil {
						parentEnv = fn.Env
					}
					if hasDbg {
						dbg.PushFrame(fn.Name, len(vals))
						tcoDepth++
					}
					matched = true
					break
				}
				return val, nil
			}

			if len(clause.Params) != len(vals) {
				continue
			}

			// Single-pass: TryBind tests the pattern and binds on match.
			// fnEnv is reused across clause attempts — reset for each try.
			if fnEnv.FlatN < 0 {
				fnEnv = core.NewFlatEnv(parentEnv)
			} else {
				fnEnv.ResetFlat()
			}

			bindOK := true
			for i := range clause.Params {
				if !TryBind(&clause.Params[i], vals[i], fnEnv) {
					bindOK = false
					break
				}
			}
			if !bindOK {
				continue
			}

			if clause.Guard != nil {
				guardVal, err := Eval(clause.Guard, fnEnv)
				if err != nil {
					if guardErr == nil {
						guardErr = err
					}
					// Guard had side effects — need fresh env for next clause
					fnEnv = core.NewFlatEnv(parentEnv)
					continue
				}
				if !guardVal.Truthy() {
					fnEnv = core.NewFlatEnv(parentEnv)
					continue
				}
			}

			val, err := Eval(clause.Body, fnEnv)
			if err == core.ErrReturn {
				env.SetExit(fnEnv.ExitCode())
				return val, nil
			}
			if err != nil {
				return val, err
			}
			if val.Kind == core.VTailCall {
				fn = val.TailFn
				vals = val.TailArgs
				if fn.Env != nil {
					parentEnv = fn.Env
				}
				if hasDbg {
					dbg.PushFrame(fn.Name, len(vals))
					tcoDepth++
				}
				matched = true
				break
			}
			return val, nil
		}

		if !matched {
			if guardErr != nil {
				return core.Nil, fmt.Errorf("guard error in %s: %w", fn.Name, guardErr)
			}
			parts := make([]string, len(vals))
			for i, v := range vals {
				parts[i] = v.Inspect()
			}
			return core.Nil, fmt.Errorf("no matching clause for %s(%s)", fn.Name, strings.Join(parts, ", "))
		}
	}
}

// RunSource parses and evaluates a source string. Exported for builtin/main use.
// RunCmdSub runs a command string and captures its stdout, returning the
// output with trailing newlines stripped. This is the $() mechanism.
func RunCmdSub(cmd string, env *core.Env) (string, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	subEnv := core.NewEnv(env)
	subEnv.Stdout_ = w
	val, _ := RunSource(cmd, subEnv)
	// If the command produced a non-nil value (e.g. an expression),
	// write its string representation to the pipe
	if val.Kind != core.VNil {
		fmt.Fprintln(w, val.ToStr())
	}
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()
	// Propagate the child's exit code to the parent environment.
	env.SetExit(subEnv.ExitCode())
	result := strings.TrimRight(buf.String(), "\n")
	return result, nil
}

// RunSource parses and evaluates a source string.
// Returns ErrExit if exit was called during evaluation.
func RunSource(src string, env *core.Env) (core.Value, error) {
	env.Shell.Source = src
	if d, ok := env.Debugger.(*debug.Debugger); ok {
		name := env.Shell.SourceName
		if name == "" {
			name = "<eval>"
		}
		sm := debug.NewSourceMap(name, src)
		d.PushSource(sm)
		defer d.PopSource()
	}
	l := lexer.New(src)
	node, err := parser.Parse(l)
	if l.Error() != "" {
		fmt.Fprintf(os.Stderr, "ish: %s\n", l.Error())
		env.SetExit(2)
		return core.Nil, nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ish: parse error: %s\n", err)
		env.SetExit(2)
		return core.Nil, nil
	}
	val, err := Eval(node, env)
	if err != nil {
		if err == core.ErrExit || err == core.ErrSetE {
			return core.Nil, core.ErrExit
		}
		if err == core.ErrBreak {
			fmt.Fprintf(os.Stderr, "ish: break: only meaningful in a loop\n")
			env.SetExit(1)
		} else if err == core.ErrContinue {
			fmt.Fprintf(os.Stderr, "ish: continue: only meaningful in a loop\n")
			env.SetExit(1)
		} else if err != core.ErrReturn {
			fmt.Fprintf(os.Stderr, "ish: %s\n", err)
			env.SetExit(1)
		}
		return core.Nil, nil
	}
	return val, nil
}
