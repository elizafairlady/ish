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
	"ish/internal/lexer"
	"ish/internal/parser"
)

// osMu protects process-global OS state (cwd, umask) during subshell execution.
var osMu sync.Mutex

func Eval(node *ast.Node, env *core.Env) (core.Value, error) {
	if node == nil {
		return core.Nil, nil
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
	case ast.NRedir:
		return evalRedir(node, env)
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
	case ast.NWord:
		return evalWord(node, env)
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
					RunSource(cmd, env)
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
func CallFn(fn *core.FnValue, vals []core.Value, env *core.Env) (core.Value, error) {
	strArgs := make([]string, len(vals))
	for i, v := range vals {
		strArgs[i] = v.ToStr()
	}

	// Use closure environment as parent if available, otherwise caller's env
	parentEnv := env
	if fn.Env != nil {
		parentEnv = fn.Env
	}

	for _, clause := range fn.Clauses {
		if len(clause.Params) == 0 {
			fnEnv := core.NewEnv(parentEnv)
			fnEnv.Args = strArgs
			val, err := Eval(clause.Body, fnEnv)
			if err == core.ErrReturn {
				return val, nil
			}
			return val, err
		}

		if len(clause.Params) != len(vals) {
			continue
		}

		matches := true
		for i, param := range clause.Params {
			if !PatternMatches(&param, vals[i], env) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}

		if clause.Guard != nil {
			fnEnv := core.NewEnv(parentEnv)
			for i, param := range clause.Params {
				PatternBind(&param, vals[i], fnEnv)
			}
			guardVal, err := Eval(clause.Guard, fnEnv)
			if err != nil {
				continue
			}
			if !guardVal.Truthy() {
				continue
			}
		}

		fnEnv := core.NewEnv(parentEnv)
		fnEnv.Args = strArgs
		for i, param := range clause.Params {
			PatternBind(&param, vals[i], fnEnv)
		}
		val, err := Eval(clause.Body, fnEnv)
		if err == core.ErrReturn {
			return val, nil
		}
		return val, err
	}

	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = v.Inspect()
	}
	return core.Nil, fmt.Errorf("no matching clause for %s(%s)", fn.Name, strings.Join(parts, ", "))
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
	val := RunSource(cmd, subEnv)
	// If the command produced a non-nil value (e.g. an expression),
	// write its string representation to the pipe
	if val.Kind != core.VNil {
		fmt.Fprintln(w, val.ToStr())
	}
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()
	result := strings.TrimRight(buf.String(), "\n")
	return result, nil
}

func RunSource(src string, env *core.Env) core.Value {
	l := lexer.New(src)
	node, err := parser.Parse(l)
	if l.Error() != "" {
		fmt.Fprintf(os.Stderr, "ish: %s\n", l.Error())
		env.SetExit(2)
		return core.Nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ish: parse error: %s\n", err)
		env.SetExit(2)
		return core.Nil
	}
	val, err := Eval(node, env)
	if err != nil {
		if err == core.ErrExit || err == core.ErrSetE {
			return core.Nil
		}
		if err != core.ErrReturn && err != core.ErrBreak && err != core.ErrContinue {
			fmt.Fprintf(os.Stderr, "ish: %s\n", err)
			env.SetExit(1)
		}
		return core.Nil
	}
	return val
}

// RunSourceErr is like RunSource but returns ErrExit if exit was called.
func RunSourceErr(src string, env *core.Env) (core.Value, error) {
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
		if err == core.ErrExit {
			return core.Nil, core.ErrExit
		}
		if err == core.ErrSetE {
			return core.Nil, core.ErrExit
		}
		if err != core.ErrReturn && err != core.ErrBreak && err != core.ErrContinue {
			fmt.Fprintf(os.Stderr, "ish: %s\n", err)
			env.SetExit(1)
		}
		return core.Nil, nil
	}
	return val, nil
}
