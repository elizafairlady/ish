package eval

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"ish/internal/ast"
	"ish/internal/core"
	"ish/internal/stdlib"
)

// isCommandNode returns true if the node produces bytes on stdout
// rather than an ish value. Used to decide pipe coercion.
// For NCmd, we check whether the command name resolves to a function
// (value-producing) or external command/builtin (byte-producing).
func isCommandNode(node *ast.Node, scope core.Scope) bool {
	switch node.Kind {
	case ast.NPipe, ast.NSubshell, ast.NGroup,
		ast.NIf, ast.NFor, ast.NWhile, ast.NUntil, ast.NCase:
		return true
	case ast.NCmd:
		if len(node.Children) == 0 {
			return true
		}
		child := node.Children[0]
		name := ""
		if child.Kind == ast.NIdent {
			name = child.Tok.Val
		} else if child.Kind == ast.NAccess {
			// Module-qualified: String.split, JSON.parse, etc.
			// These resolve to functions, not external commands.
			return false
		}
		if name == "" {
			return true
		}
		r := ResolveCmd(name, scope)
		return r.IsCmd() || r.Kind == KindNotFound
	}
	return false
}

// isBridgeFn returns true if the node is a call to an explicit bridge
// function (from_json, from_csv, etc.) that should override auto-coercion.
func accessName(node *ast.Node) string {
	if node.Kind == ast.NAccess && len(node.Children) > 0 && node.Children[0].Kind == ast.NIdent {
		return node.Children[0].Tok.Val + "." + node.Tok.Val
	}
	return ""
}

func isBridgeFn(node *ast.Node) bool {
	var name string
	switch node.Kind {
	case ast.NCmd:
		if len(node.Children) > 0 {
			child := node.Children[0]
			if child.Kind == ast.NIdent {
				name = child.Tok.Val
			} else if child.Kind == ast.NAccess {
				name = accessName(child)
			}
		}
	case ast.NIdent:
		name = node.Tok.Val
	case ast.NAccess:
		name = accessName(node)
	}
	switch name {
	case "from_json", "from_csv", "from_tsv",
		"JSON.parse", "CSV.parse", "CSV.parse_tsv", "IO.unlines":
		return true
	}
	return false
}

func evalPipe(node *ast.Node, scope core.Scope) (core.Value, error) {
	left := node.Children[0]
	right := node.Children[1]
	if right == nil {
		return core.Nil, fmt.Errorf("pipe: missing right-hand side")
	}
	pipeStderr := node.Tok.Val == "|&"

	// Auto-coerce: if left produces a value (not bytes), apply IO.unlines
	if !isCommandNode(left, scope) {
		val, err := Eval(left, scope)
		if err != nil {
			return core.Nil, err
		}
		text := stdlib.Lines(val)
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}

		pr, pw, err := os.Pipe()
		if err != nil {
			return core.Nil, err
		}
		go func() {
			io.WriteString(pw, text)
			pw.Close()
		}()
		finalStdout := os.Stdout
		if f, ok := scope.GetCtx().Stdout.(*os.File); ok {
			finalStdout = f
		}
		val2, err2 := evalWithIO(right, scope, pr, finalStdout)
		pr.Close()
		return val2, err2
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		return core.Nil, err
	}

	done := make(chan error, 1)
	leftEnv := core.CopyEnv(scope.NearestEnv())
	if pipeStderr {
		leftEnv.Ctx = scope.GetCtx().ForRedirect(pw)
	}
	go func() {
		_, err := evalWithIO(left, leftEnv, os.Stdin, pw)
		pw.Close()
		done <- err
	}()

	finalStdout := os.Stdout
	if f, ok := scope.GetCtx().Stdout.(*os.File); ok {
		finalStdout = f
	}
	val, err2 := evalWithIO(right, scope, pr, finalStdout)
	pr.Close()
	<-done

	// pipefail: if any stage failed, use the first non-zero exit code
	if scope.GetCtx().Shell.HasFlag('P') && leftEnv.Ctx.ExitCode() != 0 && scope.GetCtx().ExitCode() == 0 {
		scope.GetCtx().SetExit(leftEnv.Ctx.ExitCode())
	}

	return val, err2
}

func evalWithIO(node *ast.Node, scope core.Scope, stdin *os.File, stdout *os.File) (core.Value, error) {
	if node.Kind == ast.NCmd {
		if len(node.Children) == 0 {
			return core.Nil, nil
		}
		nameNode := node.Children[0]
		var name string
		if nameNode.Kind == ast.NIdent {
			name = nameNode.Tok.Val
		} else {
			v, err := Eval(nameNode, scope)
			if err != nil {
				return core.Nil, err
			}
			name = v.ToStr()
		}

		r := ResolveCmd(name, scope)
		if r.IsFn() {
			pipeEnv := core.NewEnv(scope)
			pipeEnv.Ctx = scope.GetCtx().ForRedirect(stdout)
			argVals := make([]core.Value, 0, len(node.Children)-1)
			for _, child := range node.Children[1:] {
				if child.Kind == ast.NVarRef && child.Tok.Type == ast.TSpecialVar && child.Tok.Val == "$@" {
					for _, arg := range scope.GetCtx().PosArgs() {
						argVals = append(argVals, core.StringVal(arg))
					}
					continue
				}
				v, err := Eval(child, scope)
				if err != nil {
					return core.Nil, err
				}
				argVals = append(argVals, v)
			}
			switch r.Kind {
			case KindModuleFn, KindUserFn, KindVarFn:
				return CallFn(r.Fn, argVals, pipeEnv)
			case KindModuleNativeFn:
				return r.Fn.Native(argVals, pipeEnv)
			}
		}

		var strArgs []string
		for _, child := range node.Children[1:] {
			if child.Kind == ast.NVarRef && child.Tok.Type == ast.TSpecialVar && child.Tok.Val == "$@" {
				strArgs = append(strArgs, scope.GetCtx().PosArgs()...)
				continue
			}
			if child.Kind == ast.NAccess {
				v, err := Eval(child, scope)
				if err != nil {
					strArgs = append(strArgs, stringifyAccess(child))
				} else {
					strArgs = append(strArgs, v.ToStr())
				}
				continue
			}
			v, err := Eval(child, scope)
			if err != nil {
				return core.Nil, err
			}
			strArgs = append(strArgs, v.ToStr())
		}
		expanded := expandGlobs(strArgs)

		if r.Kind == KindBuiltin {
			pipeEnv := core.NewEnv(scope)
			pipeEnv.Ctx = scope.GetCtx().ForRedirect(stdout)
			code, err := r.Builtin(expanded, pipeEnv)
			scope.GetCtx().SetExit(code)
			if err != nil && err != core.ErrReturn && err != core.ErrBreak && err != core.ErrContinue {
				fmt.Fprintln(os.Stderr, err)
			}
			return core.Nil, nil
		}

		cmd := exec.Command(name, expanded...)
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Env = scope.NearestEnv().BuildEnv()
		if envOut, ok := scope.GetCtx().Stdout.(*os.File); ok && envOut == stdout {
			cmd.Stderr = stdout
		} else {
			cmd.Stderr = os.Stderr
		}
		cleanup, redirErr := applyRedirects(cmd, node.Redirs, scope)
		if redirErr != nil {
			return core.Nil, redirErr
		}
		defer cleanup()
		err := cmd.Run()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				scope.GetCtx().SetExit(exitErr.ExitCode())
			} else {
				scope.GetCtx().SetExit(127)
			}
		} else {
			scope.GetCtx().SetExit(0)
		}
		return core.Nil, nil
	}

	if node.Kind == ast.NPipe {
		inner := node.Children[0]
		right := node.Children[1]

		pr2, pw2, err := os.Pipe()
		if err != nil {
			return core.Nil, err
		}
		done := make(chan error, 1)
		go func() {
			_, err := evalWithIO(inner, scope, stdin, pw2)
			pw2.Close()
			done <- err
		}()
		val, err2 := evalWithIO(right, scope, pr2, stdout)
		pr2.Close()
		<-done
		return val, err2
	}

	// Bare identifier in pipe context (e.g. `echo | cat` parsed in expression mode
	// produces NIdent "cat") — treat as a command invocation.
	if node.Kind == ast.NIdent {
		wrapped := &ast.Node{Kind: ast.NCmd, Children: []*ast.Node{node}, Pos: node.Pos}
		return evalWithIO(wrapped, scope, stdin, stdout)
	}

	pipeEnv := core.NewEnv(scope)
	pipeEnv.Ctx = scope.GetCtx().ForRedirect(stdout)
	return Eval(node, pipeEnv)
}

func evalPipeFn(node *ast.Node, scope core.Scope) (core.Value, error) {
	leftNode := node.Children[0]
	right := node.Children[1]
	if right == nil {
		return core.Nil, fmt.Errorf("pipe arrow: missing right-hand side")
	}

	// Auto-coerce: if left is a command (produces bytes) and right is not
	// an explicit bridge function, capture stdout and apply from_lines
	var left core.Value
	if isCommandNode(leftNode, scope) && !isBridgeFn(right) {
		pr, pw, err := os.Pipe()
		if err != nil {
			return core.Nil, err
		}
		done := make(chan error, 1)
		leftEnv := core.CopyEnv(scope.NearestEnv())
		go func() {
			_, err := evalWithIO(leftNode, leftEnv, os.Stdin, pw)
			pw.Close()
			done <- err
		}()
		output, _ := io.ReadAll(pr)
		pr.Close()
		<-done
		// Apply IO.lines: convert byte output to value
		left = stdlib.Unlines(string(output))
	} else if isCommandNode(leftNode, scope) && isBridgeFn(right) {
		// Explicit bridge: capture stdout as raw string, pass to bridge fn
		pr, pw, err := os.Pipe()
		if err != nil {
			return core.Nil, err
		}
		done := make(chan error, 1)
		leftEnv := core.CopyEnv(scope.NearestEnv())
		go func() {
			_, err := evalWithIO(leftNode, leftEnv, os.Stdin, pw)
			pw.Close()
			done <- err
		}()
		output, _ := io.ReadAll(pr)
		pr.Close()
		<-done
		left = core.StringVal(string(output))
	} else {
		var err error
		left, err = Eval(leftNode, scope)
		if err != nil {
			return core.Nil, err
		}
	}

	switch right.Kind {
	case ast.NCall:
		// Function call on right side of |>: prepend pipe value as first arg.
		// e.g. list |> List.filter \x -> x > 1 → List.filter(list, \x -> x > 1)
		callee, err := Eval(right.Children[0], scope)
		if err != nil {
			return core.Nil, err
		}
		if callee.Kind != core.VFn || callee.GetFn() == nil {
			return core.Nil, fmt.Errorf("pipe arrow: not a function: %s", callee.Inspect())
		}
		argVals := []core.Value{left}
		for _, child := range right.Children[1:] {
			v, err := Eval(child, scope)
			if err != nil {
				return core.Nil, err
			}
			argVals = append(argVals, v)
		}
		if node.Tail {
			return core.TailCallVal(callee.GetFn(), argVals), nil
		}
		return CallFn(callee.GetFn(), argVals, scope)

	case ast.NCmd, ast.NIdent:
		var name string
		argVals := []core.Value{left}

		if right.Kind == ast.NCmd {
			if len(right.Children) == 0 {
				return core.Nil, fmt.Errorf("pipe arrow requires a function name on the right")
			}
			nameVal, err := Eval(right.Children[0], scope)
			if err != nil {
				return core.Nil, err
			}
			// If the name evaluated to a function value directly, call it
			if nameVal.Kind == core.VFn && nameVal.GetFn() != nil {
				for _, child := range right.Children[1:] {
					v, err := Eval(child, scope)
					if err != nil {
						return core.Nil, err
					}
					argVals = append(argVals, v)
				}
				if node.Tail {
					return core.TailCallVal(nameVal.GetFn(), argVals), nil
				}
				val, err := CallFn(nameVal.GetFn(), argVals, scope)
				if err == nil && len(right.Redirs) > 0 && val.Kind != core.VNil {
					if rerr := writeValueRedirs(val, right.Redirs, scope); rerr != nil {
						return core.Nil, rerr
					}
					return core.Nil, nil
				}
				return val, err
			}
			name = nameVal.ToStr()
			for _, child := range right.Children[1:] {
				v, err := Eval(child, scope)
				if err != nil {
					return core.Nil, err
				}
				argVals = append(argVals, v)
			}
		} else {
			name = right.Tok.Val
		}

		val, err := callResolved(name, argVals, node.Tail, scope)
		if err == nil && right.Kind == ast.NCmd && len(right.Redirs) > 0 && val.Kind != core.VNil {
			if rerr := writeValueRedirs(val, right.Redirs, scope); rerr != nil {
				return core.Nil, rerr
			}
			return core.Nil, nil
		}
		return val, err

	default:
		val, err := Eval(right, scope)
		if err != nil {
			return core.Nil, err
		}
		if val.Kind == core.VFn && val.GetFn() != nil {
			if node.Tail {
				return core.TailCallVal(val.GetFn(), []core.Value{left}), nil
			}
			return CallFn(val.GetFn(), []core.Value{left}, scope)
		}
		return core.Nil, fmt.Errorf("pipe arrow: right side must be a function or command")
	}
}

// callResolved resolves a name and dispatches with the given value args.
// Used by both |> NCmd and |> NWord paths.
func callResolved(name string, argVals []core.Value, tail bool, scope core.Scope) (core.Value, error) {
	r := ResolveCmd(name, scope)
	switch r.Kind {
	case KindModuleFn, KindUserFn, KindVarFn:
		if tail {
			return core.TailCallVal(r.Fn, argVals), nil
		}
		return CallFn(r.Fn, argVals, scope)
	case KindModuleNativeFn:
		return r.Fn.Native(argVals, scope)
	case KindBuiltin:
		strArgs := make([]string, len(argVals))
		for i, v := range argVals {
			strArgs[i] = v.ToStr()
		}
		code, err := r.Builtin(strArgs, scope)
		scope.GetCtx().SetExit(code)
		if err != nil {
			return core.Nil, err
		}
		return core.Nil, nil
	default:
		strArgs := make([]string, len(argVals))
		for i, v := range argVals {
			strArgs[i] = v.ToStr()
		}
		return evalExternalCmd(name, strArgs, nil, scope)
	}
}
