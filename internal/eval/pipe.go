package eval

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"ish/internal/ast"
	"ish/internal/builtin"
	"ish/internal/core"
)

// isCommandNode returns true if the node produces bytes on stdout
// rather than an ish value. Used to decide pipe coercion.
// For NCmd, we check whether the command name resolves to a function
// (value-producing) or external command/builtin (byte-producing).
func isCommandNode(node *ast.Node, env *core.Env) bool {
	switch node.Kind {
	case ast.NPipe, ast.NSubshell, ast.NGroup,
		ast.NIf, ast.NFor, ast.NWhile, ast.NUntil, ast.NCase:
		return true
	case ast.NCmd:
		if len(node.Children) == 0 {
			return true
		}
		name := ""
		if node.Children[0].Kind == ast.NWord {
			name = node.Children[0].Tok.Val
		}
		if name == "" {
			return true
		}
		// User functions, native functions, and variable-stored functions produce values
		if _, ok := env.GetFn(name); ok {
			return false
		}
		if _, ok := env.GetNativeFn(name); ok {
			return false
		}
		if v, ok := env.Get(name); ok && v.Kind == core.VFn {
			return false
		}
		return true // builtin or external command
	}
	return false
}

// isBridgeFn returns true if the node is a call to an explicit bridge
// function (from_json, from_csv, etc.) that should override auto-coercion.
func isBridgeFn(node *ast.Node) bool {
	var name string
	switch node.Kind {
	case ast.NCmd:
		if len(node.Children) > 0 && node.Children[0].Kind == ast.NWord {
			name = node.Children[0].Tok.Val
		}
	case ast.NWord:
		name = node.Tok.Val
	}
	switch name {
	case "from_json", "from_csv", "from_tsv", "from_lines":
		return true
	}
	return false
}

func evalPipe(node *ast.Node, env *core.Env) (core.Value, error) {
	left := node.Children[0]
	right := node.Children[1]
	pipeStderr := node.Tok.Val == "|&"

	// Auto-coerce: if left produces a value (not bytes), convert to lines
	if !isCommandNode(left, env) {
		val, err := Eval(left, env)
		if err != nil {
			return core.Nil, err
		}
		var text string
		if val.Kind == core.VList {
			parts := make([]string, len(val.Elems))
			for i, elem := range val.Elems {
				parts[i] = elem.ToStr()
			}
			text = strings.Join(parts, "\n") + "\n"
		} else {
			text = val.ToStr() + "\n"
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
		if f, ok := env.Stdout().(*os.File); ok {
			finalStdout = f
		}
		val2, err2 := evalWithIO(right, env, pr, finalStdout)
		pr.Close()
		return val2, err2
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		return core.Nil, err
	}

	done := make(chan error, 1)
	leftEnv := core.CopyEnv(env)
	if pipeStderr {
		leftEnv.Stdout_ = pw
	}
	go func() {
		_, err := evalWithIO(left, leftEnv, os.Stdin, pw)
		pw.Close()
		done <- err
	}()

	finalStdout := os.Stdout
	if f, ok := env.Stdout().(*os.File); ok {
		finalStdout = f
	}
	val, err2 := evalWithIO(right, env, pr, finalStdout)
	pr.Close()
	<-done

	// pipefail: if any stage failed, use the first non-zero exit code
	if env.HasFlag('P') && leftEnv.ExitCode() != 0 && env.ExitCode() == 0 {
		env.SetExit(leftEnv.ExitCode())
	}

	return val, err2
}

func evalWithIO(node *ast.Node, env *core.Env, stdin *os.File, stdout *os.File) (core.Value, error) {
	if node.Kind == ast.NCmd {
		if len(node.Children) == 0 {
			return core.Nil, nil
		}
		nameNode := node.Children[0]
		var name string
		if nameNode.Kind == ast.NWord {
			name = env.Expand(nameNode.Tok.Val)
		} else {
			v, err := Eval(nameNode, env)
			if err != nil {
				return core.Nil, err
			}
			name = v.ToStr()
		}

		if fn, ok := env.GetFn(name); ok {
			pipeEnv := core.NewEnv(env)
			pipeEnv.Stdout_ = stdout
			argVals := make([]core.Value, 0, len(node.Children)-1)
			for _, child := range node.Children[1:] {
				if child.Kind == ast.NWord && child.Tok.Val == "$@" {
					for _, arg := range env.PosArgs() {
						argVals = append(argVals, core.StringVal(arg))
					}
					continue
				}
				v, err := Eval(child, env)
				if err != nil {
					return core.Nil, err
				}
				argVals = append(argVals, v)
			}
			return CallFn(fn, argVals, pipeEnv)
		}

		if nfn, ok := env.GetNativeFn(name); ok {
			pipeEnv := core.NewEnv(env)
			pipeEnv.Stdout_ = stdout
			argVals := make([]core.Value, 0, len(node.Children)-1)
			for _, child := range node.Children[1:] {
				if child.Kind == ast.NWord && child.Tok.Val == "$@" {
					for _, arg := range env.PosArgs() {
						argVals = append(argVals, core.StringVal(arg))
					}
					continue
				}
				v, err := Eval(child, env)
				if err != nil {
					return core.Nil, err
				}
				argVals = append(argVals, v)
			}
			return nfn(argVals, pipeEnv)
		}

		var strArgs []string
		for _, child := range node.Children[1:] {
			if child.Kind == ast.NWord && child.Tok.Val == "$@" {
				strArgs = append(strArgs, env.PosArgs()...)
				continue
			}
			v, err := evalCmdArg(child, env)
			if err != nil {
				return core.Nil, err
			}
			strArgs = append(strArgs, v.ToStr())
		}
		expanded := expandGlobs(strArgs)

		if b, ok := builtin.Builtins[name]; ok {
			pipeEnv := core.NewEnv(env)
			pipeEnv.Stdout_ = stdout
			code, err := b(expanded, pipeEnv)
			env.SetExit(code)
			if err != nil && err != core.ErrReturn && err != core.ErrBreak && err != core.ErrContinue {
				fmt.Fprintln(os.Stderr, err)
			}
			return core.Nil, nil
		}

		cmd := exec.Command(name, expanded...)
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Env = env.BuildEnv()
		if envOut, ok := env.Stdout().(*os.File); ok && envOut == stdout {
			cmd.Stderr = stdout
		} else {
			cmd.Stderr = os.Stderr
		}
		cleanup, redirErr := applyRedirects(cmd, node.Redirs, env)
		if redirErr != nil {
			return core.Nil, redirErr
		}
		defer cleanup()
		err := cmd.Run()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				env.SetExit(exitErr.ExitCode())
			} else {
				env.SetExit(127)
			}
		} else {
			env.SetExit(0)
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
			_, err := evalWithIO(inner, env, stdin, pw2)
			pw2.Close()
			done <- err
		}()
		val, err2 := evalWithIO(right, env, pr2, stdout)
		pr2.Close()
		<-done
		return val, err2
	}

	pipeEnv := core.NewEnv(env)
	pipeEnv.Stdout_ = stdout
	return Eval(node, pipeEnv)
}

func evalPipeFn(node *ast.Node, env *core.Env) (core.Value, error) {
	leftNode := node.Children[0]
	right := node.Children[1]

	// Auto-coerce: if left is a command (produces bytes) and right is not
	// an explicit bridge function, capture stdout and apply from_lines
	var left core.Value
	if isCommandNode(leftNode, env) && !isBridgeFn(right) {
		pr, pw, err := os.Pipe()
		if err != nil {
			return core.Nil, err
		}
		done := make(chan error, 1)
		leftEnv := core.CopyEnv(env)
		go func() {
			_, err := evalWithIO(leftNode, leftEnv, os.Stdin, pw)
			pw.Close()
			done <- err
		}()
		output, _ := io.ReadAll(pr)
		pr.Close()
		<-done
		// Apply from_lines: split on newlines, trim trailing empty
		s := string(output)
		if strings.HasSuffix(s, "\n") {
			s = s[:len(s)-1]
		}
		if s == "" {
			left = core.ListVal()
		} else {
			lines := strings.Split(s, "\n")
			elems := make([]core.Value, len(lines))
			for i, line := range lines {
				elems[i] = core.StringVal(line)
			}
			left = core.ListVal(elems...)
		}
	} else if isCommandNode(leftNode, env) && isBridgeFn(right) {
		// Explicit bridge: capture stdout as raw string, pass to bridge fn
		pr, pw, err := os.Pipe()
		if err != nil {
			return core.Nil, err
		}
		done := make(chan error, 1)
		leftEnv := core.CopyEnv(env)
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
		left, err = Eval(leftNode, env)
		if err != nil {
			return core.Nil, err
		}
	}

	switch right.Kind {
	case ast.NCmd:
		if len(right.Children) == 0 {
			return core.Nil, fmt.Errorf("pipe arrow requires a function name on the right")
		}
		nameVal, err := Eval(right.Children[0], env)
		if err != nil {
			return core.Nil, err
		}
		name := nameVal.ToStr()

		argVals := []core.Value{left}
		for _, child := range right.Children[1:] {
			v, err := Eval(child, env)
			if err != nil {
				return core.Nil, err
			}
			argVals = append(argVals, v)
		}

		if fn, ok := env.GetFn(name); ok {
			if node.Tail {
				return core.TailCallVal(fn, argVals), nil
			}
			return CallFn(fn, argVals, env)
		}
		if nfn, ok := env.GetNativeFn(name); ok {
			return nfn(argVals, env)
		}
		strArgs := make([]string, len(argVals))
		for i, v := range argVals {
			strArgs[i] = v.ToStr()
		}
		if b, ok := builtin.Builtins[name]; ok {
			code, err := b(strArgs, env)
			env.SetExit(code)
			if err != nil {
				return core.Nil, err
			}
			return core.Nil, nil
		}
		return evalExternalCmd(name, strArgs, nil, env)

	case ast.NWord:
		name := right.Tok.Val
		argVals := []core.Value{left}
		if fn, ok := env.GetFn(name); ok {
			if node.Tail {
				return core.TailCallVal(fn, argVals), nil
			}
			return CallFn(fn, argVals, env)
		}
		if nfn, ok := env.GetNativeFn(name); ok {
			return nfn(argVals, env)
		}
		strArgs := []string{left.ToStr()}
		if b, ok := builtin.Builtins[name]; ok {
			code, err := b(strArgs, env)
			env.SetExit(code)
			if err != nil {
				return core.Nil, err
			}
			return core.Nil, nil
		}
		return evalExternalCmd(name, strArgs, nil, env)

	default:
		return core.Nil, fmt.Errorf("pipe arrow: right side must be a function or command")
	}
}
