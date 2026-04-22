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

	var val core.Value
	var err error

	switch node.Kind {
	case ast.NBlock:
		val, err = evalBlock(node, scope)
	case ast.NLit:
		val, err = evalLit(node, scope)
	case ast.NIdent:
		val, err = evalIdent(node, scope)
	case ast.NBinOp:
		val, err = evalBinOp(node, scope)
	case ast.NUnary:
		val, err = evalUnary(node, scope)
	case ast.NMatch:
		val, err = evalMatch(node, scope)
	case ast.NTuple:
		val, err = evalTuple(node, scope)
	case ast.NList:
		val, err = evalList(node, scope)
	case ast.NCall:
		val, err = evalCall(node, scope)
	case ast.NIshFn:
		val, err = evalIshFn(node, scope)
	case ast.NLambda:
		val, err = evalLambda(node, scope)
	case ast.NAccess:
		val, err = evalAccess(node, scope)
	case ast.NIshIf:
		val, err = evalIshIf(node, scope)
	case ast.NIshMatch:
		val, err = evalIshMatch(node, scope)
	case ast.NCmd:
		val, err = evalCmd(node, scope)
	case ast.NVarRef:
		val, err = evalVarRef(node, scope)
	case ast.NPath:
		val, err = core.StringVal(expandTilde(node.Tok.Val)), nil
	case ast.NIPv4, ast.NIPv6:
		val, err = core.StringVal(node.Tok.Val), nil
	case ast.NFlag:
		val, err = core.StringVal(node.Tok.Val), nil
	case ast.NAssign:
		val, err = evalPosixAssign(node, scope)
	case ast.NPipeFn:
		val, err = evalPipeFn(node, scope)
	case ast.NAndList:
		val, err = evalAndList(node, scope)
	case ast.NOrList:
		val, err = evalOrList(node, scope)
	case ast.NIf:
		val, err = evalIf(node, scope)
	case ast.NFor:
		val, err = evalFor(node, scope)
	case ast.NWhile:
		val, err = evalWhileUntil(node, scope, false)
	case ast.NUntil:
		val, err = evalWhileUntil(node, scope, true)
	case ast.NCase:
		val, err = evalCase(node, scope)
	case ast.NSubshell:
		val, err = evalSubshell(node, scope)
	case ast.NGroup:
		val, err = Eval(node.Children[0], scope)
	case ast.NCmdSub:
		val, err = evalCmdSubNode(node, scope)
	case ast.NArithSub:
		if len(node.Children) == 0 {
			val, err = core.IntVal(0), nil
		} else {
			val, err = Eval(node.Children[0], scope)
		}
	case ast.NInterpString:
		val, err = evalInterpString(node, scope)
	case ast.NInterpolation:
		if len(node.Children) == 0 {
			val, err = core.StringVal(""), nil
		} else {
			v, e := Eval(node.Children[0], scope)
			if e != nil {
				val, err = core.Nil, e
			} else {
				val, err = core.StringVal(v.ToStr()), nil
			}
		}
	case ast.NParamExpand:
		val, err = evalParamExpand(node, scope)
	case ast.NArg:
		val, err = evalArg(node, scope)
	case ast.NMap:
		val, err = evalMap(node, scope)
	case ast.NFnDef:
		val, err = evalPosixFnDef(node, scope)
	case ast.NCapture:
		val, err = evalCapture(node, scope)
	case ast.NPipe:
		val, err = evalPipe(node, scope)
	case ast.NBg:
		val, err = evalBg(node, scope)
	case ast.NIshSpawn:
		val, err = evalIshSpawn(node, scope)
	case ast.NIshSpawnLink:
		val, err = evalIshSpawnLink(node, scope)
	case ast.NIshSend:
		val, err = evalIshSend(node, scope)
	case ast.NIshReceive:
		val, err = evalIshReceive(node, scope)
	case ast.NIshMonitor:
		val, err = evalIshMonitor(node, scope)
	case ast.NIshAwait:
		val, err = evalIshAwait(node, scope)
	case ast.NIshSupervise:
		val, err = evalIshSupervise(node, scope)
	case ast.NIshTry:
		val, err = evalIshTry(node, scope)
	case ast.NDefModule:
		val, err = evalDefModule(node, scope)
	case ast.NUse:
		val, err = evalUse(node, scope)
	case ast.NImport:
		val, err = evalImport(node, scope)
	default:
		return core.Nil, fmt.Errorf("unhandled node kind %d (%q)", node.Kind, node.Tok.Val)
	}

	if err != nil {
		return val, err
	}

	// Value redirect: when a value-producing node has redirects, apply
	// IO.unlines to convert the value to text and write it to the target.
	// External commands and builtins return core.Nil (their redirects are
	// already handled by applyRedirects on the exec.Cmd), so this only fires
	// for function calls, pipelines, and other expression nodes.
	if len(node.Redirs) > 0 && val.Kind != core.VNil {
		if rerr := writeValueRedirs(val, node.Redirs, scope); rerr != nil {
			return core.Nil, rerr
		}
		return core.Nil, nil
	}

	return val, nil
}

// expandTilde replaces a leading ~ with the user's home directory.
func expandTilde(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + path[1:]
		}
	}
	return path
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

// makeSymbolLookup creates a parser.SymbolLookup that resolves names from the
// runtime scope. This lets the parser know about functions defined in previous
// RunSource calls without copying the entire env.
func makeSymbolLookup(scope core.Scope) parser.SymbolLookup {
	return func(name string) *parser.Symbol {
		// Check for user-defined or variable-bound function
		if v, ok := scope.Get(name); ok && v.Kind == core.VFn {
			return &parser.Symbol{Kind: parser.SymFn}
		}
		if _, ok := scope.GetFn(name); ok {
			return &parser.Symbol{Kind: parser.SymFn}
		}
		if _, ok := scope.GetNativeFn(name); ok {
			return &parser.Symbol{Kind: parser.SymFn}
		}
		// Check for module
		if _, ok := scope.GetModule(name); ok {
			return &parser.Symbol{Kind: parser.SymModule}
		}
		return nil
	}
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
	node, err := parser.ParseWithEnv(l, makeSymbolLookup(scope))
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
