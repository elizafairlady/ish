package eval

import (
	"fmt"
	"os"
	"os/exec"

	"ish/internal/ast"
	"ish/internal/builtin"
	"ish/internal/core"
)

func evalPipe(node *ast.Node, env *core.Env) (core.Value, error) {
	left := node.Children[0]
	right := node.Children[1]
	pipeStderr := node.Tok.Val == "|&"

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
	left, err := Eval(node.Children[0], env)
	if err != nil {
		return core.Nil, err
	}
	right := node.Children[1]

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
