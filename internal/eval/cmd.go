package eval

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"ish/internal/ast"
	"ish/internal/builtin"
	"ish/internal/core"
	"ish/internal/jobs"
	"ish/internal/lexer"
	"ish/internal/parser"
)

func evalCmd(node *ast.Node, env *core.Env) (core.Value, error) {
	if len(node.Children) == 0 {
		return core.Nil, nil
	}

	for _, assign := range node.Assigns {
		if _, err := evalPosixAssign(assign, env); err != nil {
			return core.Nil, err
		}
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

	if nameNode.Kind == ast.NWord && !strings.Contains(nameNode.Tok.Val, "$") {
		if aliasVal, ok := env.GetAlias(name); ok {
			firstWord := aliasVal
			if sp := strings.IndexByte(aliasVal, ' '); sp >= 0 {
				firstWord = aliasVal[:sp]
			}
			if firstWord != name {
				var argStr strings.Builder
				for _, child := range node.Children[1:] {
					argStr.WriteString(" ")
					argStr.WriteString(child.Tok.Val)
				}
				newSrc := aliasVal + argStr.String()
				newNode, err := parser.Parse(lexer.New(newSrc))
				if err != nil {
					return core.Nil, err
				}
				return Eval(newNode, env)
			}
		}
	}

	if fn, ok := env.GetFn(name); ok {
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
		if node.Tail {
			return core.TailCallVal(fn, argVals), nil
		}
		return CallFn(fn, argVals, env)
	}

	if nfn, ok := env.GetNativeFn(name); ok {
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
		return nfn(argVals, env)
	}

	// Check if the command name is a variable holding a function value.
	// Evaluate args as expressions (same as named functions), not as command strings.
	if v, ok := env.Get(name); ok && v.Kind == core.VFn {
		fnArgVals := make([]core.Value, 0, len(node.Children)-1)
		for _, child := range node.Children[1:] {
			if child.Kind == ast.NWord && child.Tok.Val == "$@" {
				for _, arg := range env.PosArgs() {
					fnArgVals = append(fnArgVals, core.StringVal(arg))
				}
				continue
			}
			av, err := Eval(child, env)
			if err != nil {
				return core.Nil, err
			}
			fnArgVals = append(fnArgVals, av)
		}
		return CallFn(v.Fn, fnArgVals, env)
	}

	argVals := make([]core.Value, 0, len(node.Children)-1)
	for _, child := range node.Children[1:] {
		v, err := evalCmdArg(child, env)
		if err != nil {
			return core.Nil, err
		}
		argVals = append(argVals, v)
	}

	strArgs := make([]string, 0, len(argVals))
	quotedFlags := make([]bool, 0, len(argVals))
	for i, v := range argVals {
		s := v.ToStr()
		argNode := node.Children[i+1]
		if argNode.Kind == ast.NLit && argNode.Tok.Type == ast.TString {
			strArgs = append(strArgs, s)
			quotedFlags = append(quotedFlags, true)
		} else if argNode.Kind == ast.NWord && argNode.Tok.Val == "$@" {
			for _, arg := range env.PosArgs() {
				strArgs = append(strArgs, arg)
				quotedFlags = append(quotedFlags, true)
			}
		} else if argNode.Kind == ast.NWord && !strings.Contains(argNode.Tok.Val, "$") {
			strArgs = append(strArgs, s)
			quotedFlags = append(quotedFlags, false)
		} else {
			fields := env.SplitFieldsIFS(s)
			for range fields {
				quotedFlags = append(quotedFlags, false)
			}
			strArgs = append(strArgs, fields...)
		}
	}
	expanded := expandGlobsSelective(strArgs, quotedFlags)

	if env.HasFlag('x') {
		fmt.Fprintf(os.Stderr, "+ %s\n", strings.Join(append([]string{name}, expanded...), " "))
	}

	if name == "exec" && len(expanded) == 0 && len(node.Redirs) > 0 {
		for _, r := range node.Redirs {
			target := env.Expand(r.Target)
			switch r.Op {
			case ast.TRedirOut:
				f, err := os.Create(target)
				if err != nil {
					return core.Nil, err
				}
				switch r.Fd {
				case 1:
					env.Stdout_ = f
				case 2:
					os.Stderr = f
				}
			case ast.TRedirAppend:
				f, err := os.OpenFile(target, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					return core.Nil, err
				}
				switch r.Fd {
				case 1:
					env.Stdout_ = f
				case 2:
					os.Stderr = f
				}
			case ast.TRedirIn:
				f, err := os.Open(target)
				if err != nil {
					return core.Nil, err
				}
				os.Stdin = f
			}
		}
		env.SetExit(0)
		return core.Nil, nil
	}

	if b, ok := builtin.Builtins[name]; ok {
		// Apply redirections for builtins
		if len(node.Redirs) > 0 {
			oldStdout := env.Stdout()
			var files []*os.File
			for _, r := range node.Redirs {
				target := env.Expand(r.Target)
				switch r.Op {
				case ast.TRedirOut:
					f, ferr := os.Create(target)
					if ferr != nil {
						return core.Nil, ferr
					}
					files = append(files, f)
					if r.Fd == 1 || r.Fd == 0 {
						env.Stdout_ = f
					}
				case ast.TRedirAppend:
					f, ferr := os.OpenFile(target, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if ferr != nil {
						return core.Nil, ferr
					}
					files = append(files, f)
					if r.Fd == 1 || r.Fd == 0 {
						env.Stdout_ = f
					}
				}
			}
			code, err := b(expanded, env)
			env.SetExit(code)
			env.Stdout_ = oldStdout
			for _, f := range files {
				f.Close()
			}
			if err != nil {
				if err == core.ErrReturn || err == core.ErrBreak || err == core.ErrContinue || err == core.ErrExit {
					return core.Nil, err
				}
				fmt.Fprintln(os.Stderr, err)
			}
			return core.Nil, nil
		}
		code, err := b(expanded, env)
		env.SetExit(code)
		if err != nil {
			if err == core.ErrReturn || err == core.ErrBreak || err == core.ErrContinue {
				return core.Nil, err
			}
			fmt.Fprintln(os.Stderr, err)
		}
		return core.Nil, nil
	}

	if len(expanded) == 0 {
		if v, ok := env.Get(name); ok {
			return v, nil
		}
		// Handle m.x dot access for maps
		if dotIdx := strings.IndexByte(name, '.'); dotIdx > 0 {
			objName := name[:dotIdx]
			field := name[dotIdx+1:]
			if obj, ok := env.Get(objName); ok && obj.Kind == core.VMap && obj.Map != nil {
				if v, ok := obj.Map.Get(field); ok {
					return v, nil
				}
			}
		}
	}

	result, err := evalExternalCmd(name, expanded, node.Redirs, env)
	return result, err
}

func evalCmdArg(node *ast.Node, env *core.Env) (core.Value, error) {
	switch node.Kind {
	case ast.NWord:
		name := node.Tok.Val
		switch name {
		case "nil":
			return core.Nil, nil
		case "true":
			return core.True, nil
		case "false":
			return core.False, nil
		case "self":
			if proc := env.GetProc(); proc != nil {
				return core.Value{Kind: core.VPid, Pid: proc}, nil
			}
			return core.Nil, nil
		}
		if strings.HasPrefix(name, "~") {
			return core.StringVal(env.ExpandTilde(name)), nil
		}
		if strings.HasPrefix(name, "$((") && strings.HasSuffix(name, "))") {
			return evalArithExpansion(name, env)
		}
		if strings.HasPrefix(name, "$(") && strings.HasSuffix(name, ")") {
			return evalCmdSub(name[2:len(name)-1], env)
		}
		if strings.Contains(name, "$") || strings.Contains(name, "#{") {
			return core.StringVal(env.Expand(name)), nil
		}
		if strings.ContainsAny(name, "'\"") {
			name = stripAssignQuotes(name)
		}
		return core.StringVal(name), nil
	case ast.NLit:
		return evalLit(node, env)
	default:
		return Eval(node, env)
	}
}

func applyRedirects(cmd *exec.Cmd, redirs []ast.Redir, env *core.Env) (cleanup func(), err error) {
	var files []*os.File
	cleanup = func() {
		for _, f := range files {
			f.Close()
		}
	}

	for _, r := range redirs {
		target := env.Expand(r.Target)

		if strings.HasPrefix(target, "&") {
			fdStr := target[1:]
			switch fdStr {
			case "1":
				if r.Fd == 2 {
					cmd.Stderr = cmd.Stdout
				}
			case "2":
				if r.Fd == 1 {
					cmd.Stdout = cmd.Stderr
				}
			}
			continue
		}

		switch r.Op {
		case ast.TRedirOut:
			f, ferr := os.Create(target)
			if ferr != nil {
				cleanup()
				return nil, ferr
			}
			files = append(files, f)
			switch r.Fd {
			case 0:
				cmd.Stdin = f
			case 1:
				cmd.Stdout = f
			case 2:
				cmd.Stderr = f
			}
		case ast.TRedirAppend:
			f, ferr := os.OpenFile(target, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if ferr != nil {
				cleanup()
				return nil, ferr
			}
			files = append(files, f)
			switch r.Fd {
			case 1:
				cmd.Stdout = f
			case 2:
				cmd.Stderr = f
			}
		case ast.TRedirIn:
			f, ferr := os.Open(target)
			if ferr != nil {
				cleanup()
				return nil, ferr
			}
			files = append(files, f)
			cmd.Stdin = f
		case ast.THeredoc:
			content := r.Target
			if !r.Quoted {
				content = env.Expand(content)
			}
			cmd.Stdin = strings.NewReader(content)
		case ast.THereString:
			content := r.Target
			if !r.Quoted {
				content = env.Expand(content)
			}
			cmd.Stdin = strings.NewReader(content + "\n")
		}
	}

	return cleanup, nil
}

func evalExternalCmd(name string, args []string, redirs []ast.Redir, env *core.Env) (core.Value, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	if f, ok := env.Stdout().(*os.File); ok {
		cmd.Stdout = f
	} else {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr
	cmd.Env = env.BuildEnv()

	ttyFd := int(os.Stdin.Fd())
	isTTY := term.IsTerminal(ttyFd)

	// Put child in its own process group for job control
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	cleanup, err := applyRedirects(cmd, redirs, env)
	if err != nil {
		return core.Nil, err
	}
	defer cleanup()

	// Reset job control signals to SIG_DFL so the child inherits default
	// disposition through exec. Go issue #20479.
	signal.Reset(syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)

	if err := cmd.Start(); err != nil {
		renotifyJobSignals()
		env.SetExit(127)
		fmt.Fprintf(os.Stderr, "ish: %s: %s\n", name, err)
		return core.Nil, nil
	}

	pid := cmd.Process.Pid

	// Give the child the terminal, wait, reclaim
	if isTTY {
		jobs.GiveTerm(ttyFd, pid)
	} else {
		renotifyJobSignals()
	}

	ws, waitErr := jobs.WaitFg(pid)

	if isTTY {
		jobs.ReclaimTerm(ttyFd)
	}

	if waitErr != nil {
		env.SetExit(127)
		fmt.Fprintf(os.Stderr, "ish: %s: %s\n", name, waitErr)
		return core.Nil, nil
	}

	if ws.Stopped() {
		cmdStr := name + " " + strings.Join(args, " ")
		jobID := jobs.AddJob(pid, strings.TrimSpace(cmdStr), cmd.Process)
		j := jobs.FindJob(jobID)
		j.Mu.Lock()
		j.Status = "Stopped"
		j.Mu.Unlock()
		fmt.Fprintf(os.Stderr, "\n[%d]+ Stopped\t%s\n", jobID, strings.TrimSpace(cmdStr))
		env.SetExit(148)
		return core.Nil, nil
	}

	env.SetExit(ws.ExitStatus())
	return core.Nil, nil
}

// jobSignalChan is the channel used to catch job control signals for the shell.
var jobSignalChan chan os.Signal

// InitJobSignals sets up job control signal handling. Called from main.
func InitJobSignals() {
	jobSignalChan = make(chan os.Signal, 1)
	signal.Notify(jobSignalChan, syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)
	go func() {
		for range jobSignalChan {
		}
	}()
	jobs.SetSignalChan(jobSignalChan)
}

// renotifyJobSignals re-establishes signal.Notify after a temporary Reset.
func renotifyJobSignals() {
	if jobSignalChan != nil {
		signal.Notify(jobSignalChan, syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)
	}
}


func stripAssignQuotes(s string) string {
	var buf strings.Builder
	i := 0
	for i < len(s) {
		// Skip over $(...) and ${...} — don't strip quotes inside them
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '(' {
			depth := 1
			buf.WriteByte(s[i])
			buf.WriteByte(s[i+1])
			i += 2
			for i < len(s) && depth > 0 {
				if s[i] == '(' {
					depth++
				} else if s[i] == ')' {
					depth--
				}
				if depth > 0 {
					buf.WriteByte(s[i])
					i++
				}
			}
			if i < len(s) {
				buf.WriteByte(s[i])
				i++
			}
		} else if s[i] == '$' && i+1 < len(s) && s[i+1] == '{' {
			depth := 1
			buf.WriteByte(s[i])
			buf.WriteByte(s[i+1])
			i += 2
			for i < len(s) && depth > 0 {
				if s[i] == '{' {
					depth++
				} else if s[i] == '}' {
					depth--
				}
				if depth > 0 {
					buf.WriteByte(s[i])
					i++
				}
			}
			if i < len(s) {
				buf.WriteByte(s[i])
				i++
			}
		} else if s[i] == '"' {
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				buf.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				i++
			}
		} else if s[i] == '\'' {
			i++
			for i < len(s) && s[i] != '\'' {
				buf.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				i++
			}
		} else {
			buf.WriteByte(s[i])
			i++
		}
	}
	return buf.String()
}

func evalPosixAssign(node *ast.Node, env *core.Env) (core.Value, error) {
	s := node.Tok.Val
	i := strings.IndexByte(s, '=')
	if i < 0 {
		return core.Nil, fmt.Errorf("invalid assignment: %s", s)
	}
	name := s[:i]
	raw := s[i+1:]
	raw = stripAssignQuotes(raw)
	val := raw
	if len(s) > i+1 && s[i+1] != '\'' {
		val = env.Expand(raw)
	}
	if err := env.Set(name, core.StringVal(val)); err != nil {
		return core.Nil, err
	}
	return core.Nil, nil
}

func evalPosixFnDef(node *ast.Node, env *core.Env) (core.Value, error) {
	name := node.Tok.Val
	fnVal := &core.FnValue{
		Name: name,
		Clauses: []core.FnClause{{
			Body: node.Children[0],
		}},
	}
	env.SetFn(name, fnVal)
	return core.Nil, nil
}

func evalBg(node *ast.Node, env *core.Env) (core.Value, error) {
	child := node.Children[0]

	if child.Kind == ast.NCmd && len(child.Children) > 0 {
		nameNode := child.Children[0]
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

		_, isFn := env.GetFn(name)
		_, isBuiltin := builtin.Builtins[name]

		if !isFn && !isBuiltin {
			var args []string
			for _, c := range child.Children[1:] {
				v, err := evalCmdArg(c, env)
				if err != nil {
					return core.Nil, err
				}
				args = append(args, v.ToStr())
			}
			expanded := expandGlobs(args)
			cmd := exec.Command(name, expanded...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Env = env.BuildEnv()
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

			err := cmd.Start()
			if err != nil {
				return core.Nil, fmt.Errorf("ish: %s: %s", name, err)
			}

			pid := cmd.Process.Pid
			cmdStr := name + " " + strings.Join(expanded, " ")
			jobID := jobs.AddJob(pid, strings.TrimSpace(cmdStr), cmd.Process)
			env.LastBg = pid
			fmt.Fprintf(os.Stderr, "[%d] %d\n", jobID, pid)

			j := jobs.FindJob(jobID)
			doneCh := j.Done
			go func() {
				state, _ := cmd.Process.Wait()
				jj := jobs.FindJobByPid(pid)
				if jj != nil {
					jj.Mu.Lock()
					jj.Status = "Done"
					if state != nil {
						jj.ExitCode = state.ExitCode()
					}
					jj.Mu.Unlock()
				}
				close(doneCh)
			}()

			return core.Nil, nil
		}
	}

	bgEnv := core.NewEnv(env)
	go func() {
		Eval(child, bgEnv)
	}()
	return core.Nil, nil
}
