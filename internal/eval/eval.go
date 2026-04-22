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

var framePool = sync.Pool{
	New: func() interface{} { return &core.Frame{} },
}

func getFrame(parent core.Scope) *core.Frame {
	f := framePool.Get().(*core.Frame)
	f.Init(parent)
	return f
}

func putFrame(f *core.Frame) {
	f.ResetFlat()
	framePool.Put(f)
}

// MakeIsCommand builds a callback for the parser that identifies known commands.
func MakeIsCommand(scope core.Scope) func(string) bool {
	return ResolveCmdCached(scope)
}

func Eval(node *ast.Node, scope core.Scope) (core.Value, error) {
	if node == nil {
		return core.Nil, nil
	}

	if d, ok := scope.GetCtx().Debugger.(*debug.Debugger); ok {
		d.SetNode(node.Pos)
		if d.TraceAll {
			d.TraceNode(node)
		}
	}

	switch node.Kind {
	case ast.NBlock:
		return evalBlock(node, scope)
	case ast.NLit:
		return evalLit(node, scope)
	case ast.NIdent:
		return evalIdent(node, scope)
	case ast.NBinOp:
		return evalBinOp(node, scope)
	case ast.NUnary:
		return evalUnary(node, scope)
	case ast.NMatch:
		return evalMatch(node, scope)
	case ast.NTuple:
		return evalTuple(node, scope)
	case ast.NList:
		return evalList(node, scope)
	case ast.NCall:
		return evalCall(node, scope)
	case ast.NIshFn:
		return evalIshFn(node, scope)
	case ast.NLambda:
		return evalLambda(node, scope)
	case ast.NAccess:
		return evalAccess(node, scope)
	case ast.NIshIf:
		return evalIshIf(node, scope)
	case ast.NIshMatch:
		return evalIshMatch(node, scope)
	case ast.NCmd:
		return evalCmd(node, scope)
	case ast.NVarRef:
		return evalVarRef(node, scope)
	case ast.NPath:
		return core.StringVal(node.Tok.Val), nil
	case ast.NFlag:
		return core.StringVal(node.Tok.Val), nil
	case ast.NAssign:
		return evalPosixAssign(node, scope)
	case ast.NPipeFn:
		return evalPipeFn(node, scope)
	case ast.NAndList:
		return evalAndList(node, scope)
	case ast.NOrList:
		return evalOrList(node, scope)
	case ast.NIf:
		return evalIf(node, scope)
	case ast.NFor:
		return evalFor(node, scope)
	case ast.NWhile:
		return evalWhileUntil(node, scope, false)
	case ast.NUntil:
		return evalWhileUntil(node, scope, true)
	case ast.NCase:
		return evalCase(node, scope)
	case ast.NSubshell:
		return evalSubshell(node, scope)
	case ast.NGroup:
		return Eval(node.Children[0], scope)
	case ast.NCmdSub:
		return evalCmdSubNode(node, scope)
	case ast.NArithSub:
		if len(node.Children) == 0 { return core.IntVal(0), nil }
		return Eval(node.Children[0], scope)
	case ast.NInterpString:
		return evalInterpString(node, scope)
	case ast.NInterpolation:
		if len(node.Children) == 0 { return core.StringVal(""), nil }
		v, err := Eval(node.Children[0], scope)
		if err != nil { return core.Nil, err }
		return core.StringVal(v.ToStr()), nil
	case ast.NParamExpand:
		return evalParamExpand(node, scope)
	case ast.NArg:
		return evalArg(node, scope)
	case ast.NMap:
		return evalMap(node, scope)
	case ast.NFnDef:
		return evalPosixFnDef(node, scope)
	case ast.NCapture:
		return evalCapture(node, scope)
	case ast.NPipe:
		return evalPipe(node, scope)
	case ast.NBg:
		return evalBg(node, scope)
	case ast.NIshSpawn:
		return evalIshSpawn(node, scope)
	case ast.NIshSpawnLink:
		return evalIshSpawnLink(node, scope)
	case ast.NIshSend:
		return evalIshSend(node, scope)
	case ast.NIshReceive:
		return evalIshReceive(node, scope)
	case ast.NIshMonitor:
		return evalIshMonitor(node, scope)
	case ast.NIshAwait:
		return evalIshAwait(node, scope)
	case ast.NIshSupervise:
		return evalIshSupervise(node, scope)
	case ast.NIshTry:
		return evalIshTry(node, scope)
	case ast.NDefModule:
		return evalDefModule(node, scope)
	case ast.NUse:
		return evalUse(node, scope)
	default:
		return core.Nil, fmt.Errorf("unhandled node kind %d (%q)", node.Kind, node.Tok.Val)
	}
}

func evalBlock(node *ast.Node, scope core.Scope) (core.Value, error) {
	var last core.Value
	for _, child := range node.Children {
		v, err := Eval(child, scope)
		if err != nil { return v, err }
		last = v

		// set -e and trap ERR: exempt control flow constructs per POSIX
		exempt := child.Kind == ast.NIf || child.Kind == ast.NWhile ||
			child.Kind == ast.NUntil || child.Kind == ast.NAndList ||
			child.Kind == ast.NOrList ||
			(child.Kind == ast.NUnary && child.Tok.Type == ast.TBang)

		if !exempt {
			if scope.GetCtx().ExitCode() != 0 {
				if cmd, ok := scope.GetCtx().Shell.GetTrap("ERR"); ok && cmd != "" {
					RunSource(cmd, scope.NearestEnv())
				}
			}
			if scope.GetCtx().ShouldExitOnError() {
				return last, core.ErrSetE
			}
		}
	}
	return last, nil
}

func syncExit(val core.Value, scope core.Scope) {
	if val.Kind != core.VNil {
		if val.Truthy() {
			scope.GetCtx().SetExit(0)
		} else {
			scope.GetCtx().SetExit(1)
		}
	}
}

// CallFn calls a user-defined function with Value arguments.
// Tail calls are resolved in a loop to avoid stack growth.
func CallFn(fn *core.FnValue, vals []core.Value, scope core.Scope) (retVal core.Value, retErr error) {
	if fn.Native != nil {
		return fn.Native(vals, scope)
	}

	dbg, hasDbg := scope.GetCtx().Debugger.(*debug.Debugger)
	var tcoDepth int
	if hasDbg {
		dbg.PushFrame(fn.Name, len(vals))
		defer func() {
			if retErr != nil { retErr = dbg.WrapError(retErr) }
			for i := 0; i < tcoDepth; i++ { dbg.PopFrame() }
			dbg.PopFrame()
		}()
	}

	parentScope := core.Scope(scope)
	if fn.Env != nil {
		parentScope = fn.Env
	}

	for {
		if fn.Native != nil {
			return fn.Native(vals, scope)
		}

		matched := false
		frame := getFrame(parentScope)
		for _, clause := range fn.Clauses {
			if len(clause.Params) == 0 {
				putFrame(frame)
				// POSIX function path: uses $1/$@ positional args
				posixEnv := core.NewEnv(parentScope)
				strArgs := make([]string, len(vals))
				for i, v := range vals { strArgs[i] = v.ToStr() }
				posixEnv.Ctx.Args = strArgs
				val, err := Eval(clause.Body, posixEnv)
				if err == core.ErrReturn {
					scope.GetCtx().SetExit(posixEnv.Ctx.ExitCode())
					return val, nil
				}
				if err != nil { return val, err }
				if val.Kind == core.VTailCall {
					fn = val.GetTailFn()
					vals = val.GetTailArgs()
					if fn.Env != nil { parentScope = fn.Env }
					if hasDbg { dbg.PushFrame(fn.Name, len(vals)); tcoDepth++ }
					matched = true
					break
				}
				return val, nil
			}

			if len(clause.Params) != len(vals) { continue }

			frame.ResetFlat()
			bindOK := true
			for i := range clause.Params {
				if !TryBind(&clause.Params[i], vals[i], frame) {
					bindOK = false
					break
				}
			}
			if !bindOK { continue }

			if clause.Guard != nil {
				gv, err := Eval(clause.Guard, frame)
				if err != nil { continue }
				if !gv.Truthy() { continue }
			}

			val, err := Eval(clause.Body, frame)
			putFrame(frame)
			if err == core.ErrReturn { return val, nil }
			if err != nil { return val, err }
			if val.Kind == core.VTailCall {
				fn = val.GetTailFn()
				vals = val.GetTailArgs()
				if fn.Env != nil { parentScope = fn.Env }
				if hasDbg { dbg.PushFrame(fn.Name, len(vals)); tcoDepth++ }
				matched = true
				break
			}
			return val, nil
		}

		if !matched {
			putFrame(frame)
			parts := make([]string, len(vals))
			for i, v := range vals { parts[i] = v.Inspect() }
			return core.Nil, fmt.Errorf("no matching clause for %s(%s)", fn.Name, strings.Join(parts, ", "))
		}
	}
}

// RunCmdSub runs a command string and captures its stdout.
func RunCmdSub(cmd string, scope core.Scope) (string, error) {
	r, w, err := os.Pipe()
	if err != nil { return "", err }
	subEnv := core.NewEnv(scope)
	subEnv.Ctx = scope.GetCtx().ForRedirect(w)
	val, _ := RunSource(cmd, subEnv)
	if val.Kind != core.VNil {
		fmt.Fprintln(w, val.ToStr())
	}
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()
	scope.GetCtx().SetExit(subEnv.Ctx.ExitCode())
	result := strings.TrimRight(buf.String(), "\n")
	return result, nil
}

// RunSource parses and evaluates a source string.
func RunSource(src string, scope core.Scope) (core.Value, error) {
	scope.GetCtx().Source = src
	if d, ok := scope.GetCtx().Debugger.(*debug.Debugger); ok {
		name := scope.GetCtx().SourceName
		if name == "" { name = "<eval>" }
		sm := debug.NewSourceMap(name, src)
		d.PushSource(sm)
		defer d.PopSource()
	}
	l := lexer.New(src)
	node, err := parser.Parse(l)
	if l.Error() != "" {
		fmt.Fprintf(os.Stderr, "ish: %s\n", l.Error())
		scope.GetCtx().SetExit(2)
		return core.Nil, nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ish: parse error: %s\n", err)
		scope.GetCtx().SetExit(2)
		return core.Nil, nil
	}
	val, err := Eval(node, scope)
	if err != nil {
		if err == core.ErrExit || err == core.ErrSetE {
			return core.Nil, core.ErrExit
		}
		if err == core.ErrBreak {
			fmt.Fprintf(os.Stderr, "ish: break: only meaningful in a loop\n")
			scope.GetCtx().SetExit(1)
		} else if err == core.ErrContinue {
			fmt.Fprintf(os.Stderr, "ish: continue: only meaningful in a loop\n")
			scope.GetCtx().SetExit(1)
		} else if err != core.ErrReturn {
			fmt.Fprintf(os.Stderr, "ish: %s\n", err)
			scope.GetCtx().SetExit(1)
		}
		return core.Nil, nil
	}
	return val, nil
}
