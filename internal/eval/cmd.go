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
	"ish/internal/stdlib"
)

// evalRedirTarget evaluates a redirect's target node to a string.
// stringifyAccess reconstructs a dot-chain as a literal string (e.g., "README.md").
func stringifyAccess(node *ast.Node) string {
	if node.Kind == ast.NAccess {
		return stringifyAccess(node.Children[0]) + "." + node.Tok.Val
	}
	return node.Tok.Val
}

func evalRedirTarget(r ast.Redir, scope core.Scope) (string, error) {
	if r.TargetNode == nil {
		return "", nil
	}
	v, err := Eval(r.TargetNode, scope)
	if err != nil {
		return "", err
	}
	return v.ToStr(), nil
}

func evalCmd(node *ast.Node, scope core.Scope) (core.Value, error) {
	if len(node.Children) == 0 {
		return core.Nil, nil
	}

	// Prefix assignments: save old values and restore after the command
	// completes (POSIX: prefix assigns are scoped to the command).
	if len(node.Assigns) > 0 {
		type savedVar struct {
			name string
			val  core.Value
			had  bool
		}
		saved := make([]savedVar, 0, len(node.Assigns))
		for _, assign := range node.Assigns {
			varName := assign.Tok.Val
			oldVal, had := scope.Get(varName)
			saved = append(saved, savedVar{varName, oldVal, had})
			if _, err := evalPosixAssign(assign, scope); err != nil {
				return core.Nil, err
			}
		}
		defer func() {
			for _, s := range saved {
				if s.had {
					scope.Set(s.name, s.val)
				} else {
					scope.NearestEnv().DeleteVar(s.name) //nolint: errcheck
				}
			}
		}()
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

	// Alias expansion
	if nameNode.Kind == ast.NIdent {
		if aliasVal, ok := scope.GetCtx().Shell.GetAlias(name); ok {
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
				return Eval(newNode, scope)
			}
		}
	}

	// Handle `self` keyword — returns current process pid
	if name == "self" && len(node.Children) == 1 {
		if proc := scope.GetCtx().Proc; proc != nil {
			return core.PidVal(proc), nil
		}
		return core.Nil, nil
	}

	// Standalone variable reference: return the value if no args
	if v, ok := scope.Get(name); ok && len(node.Children) == 1 {
		return v, nil
	}

	// Function fallback: if the name resolves to a function at runtime
	// (dynamic definitions, sourced files, etc.), delegate to evalCall.
	// The parser handles the common case (NCall), this catches the rest.
	if fn := resolveFn(name, scope); fn != nil {
		return evalCallFromCmd(fn, node, scope)
	}

	// Build string arguments for builtins and external commands.
	// Each argument node evaluates to one or more string args depending on type:
	// - NLit (quoted string): one arg, no word splitting, no glob
	// - NIdent: literal string, eligible for glob expansion
	// - NVarRef ($var): expand variable, word split on IFS, glob eligible
	// - NPath, NFlag, NArg: literal string, glob eligible
	// - NAccess ($var.field): evaluate, word split, glob eligible
	// - NInterpString: quoted context, no word splitting
	// - Special: $@ expands to separate args preserving boundaries
	strArgs := make([]string, 0, len(node.Children)-1)
	quotedFlags := make([]bool, 0, len(node.Children)-1)
	for _, child := range node.Children[1:] {
		switch child.Kind {
		case ast.NLit:
			// In command context, use raw token text for numeric literals
			// to preserve formatting (e.g. 1.120 in IP addresses).
			if child.Tok.Type == ast.TFloat || child.Tok.Type == ast.TInt {
				strArgs = append(strArgs, child.Tok.Val)
				quotedFlags = append(quotedFlags, true)
			} else {
				v, err := Eval(child, scope)
				if err != nil {
					return core.Nil, err
				}
				strArgs = append(strArgs, v.ToStr())
				quotedFlags = append(quotedFlags, true)
			}
		case ast.NInterpString:
			// Interpolated strings: quoted context
			v, err := Eval(child, scope)
			if err != nil {
				return core.Nil, err
			}
			strArgs = append(strArgs, v.ToStr())
			quotedFlags = append(quotedFlags, true)
		case ast.NVarRef:
			// $@ expands to separate args
			if child.Tok.Type == ast.TSpecialVar && child.Tok.Val == "$@" {
				for _, arg := range scope.GetCtx().PosArgs() {
					strArgs = append(strArgs, arg)
					quotedFlags = append(quotedFlags, true)
				}
				continue
			}
			// Other variables: expand, word split, glob eligible
			v, err := Eval(child, scope)
			if err != nil {
				return core.Nil, err
			}
			s := v.ToStr()
			fields := scope.NearestEnv().SplitFieldsIFS(s)
			for range fields {
				quotedFlags = append(quotedFlags, false)
			}
			strArgs = append(strArgs, fields...)
		case ast.NIdent:
			// Bare identifiers in command args are literal strings, not variable lookups
			strArgs = append(strArgs, child.Tok.Val)
			quotedFlags = append(quotedFlags, false)
		case ast.NAccess:
			// Dot-chain in command context: could be Module.func or filename.ext
			// Try evaluating as expression; fall back to literal string on error
			v, err := Eval(child, scope)
			if err != nil {
				strArgs = append(strArgs, stringifyAccess(child))
				quotedFlags = append(quotedFlags, false)
			} else {
				strArgs = append(strArgs, v.ToStr())
				quotedFlags = append(quotedFlags, false)
			}
		case ast.NPath:
			strArgs = append(strArgs, expandTilde(child.Tok.Val))
			quotedFlags = append(quotedFlags, false)
		case ast.NIPv4, ast.NIPv6:
			strArgs = append(strArgs, child.Tok.Val)
			quotedFlags = append(quotedFlags, true) // no glob expansion on IPs
		case ast.NFlag:
			strArgs = append(strArgs, child.Tok.Val)
			quotedFlags = append(quotedFlags, false)
		default:
			// Everything else: evaluate, word split, glob eligible
			v, err := Eval(child, scope)
			if err != nil {
				return core.Nil, err
			}
			s := v.ToStr()
			fields := scope.NearestEnv().SplitFieldsIFS(s)
			for range fields {
				quotedFlags = append(quotedFlags, false)
			}
			strArgs = append(strArgs, fields...)
		}
	}
	expanded := expandGlobsSelective(strArgs, quotedFlags)

	if scope.GetCtx().Shell.HasFlag('x') {
		fmt.Fprintf(os.Stderr, "+ %s\n", strings.Join(append([]string{name}, expanded...), " "))
	}

	if name == "exec" && len(expanded) == 0 && len(node.Redirs) > 0 {
		for _, r := range node.Redirs {
			target, _ := evalRedirTarget(r, scope)
			switch r.Op {
			case ast.TGt:
				f, err := os.Create(target)
				if err != nil {
					return core.Nil, err
				}
				switch r.Fd {
				case 1:
					scope.GetCtx().Stdout = f
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
					scope.GetCtx().Stdout = f
				case 2:
					os.Stderr = f
				}
			case ast.TLt:
				f, err := os.Open(target)
				if err != nil {
					return core.Nil, err
				}
				os.Stdin = f
			}
		}
		scope.GetCtx().SetExit(0)
		return core.Nil, nil
	}

	if b, ok := builtin.Builtins[name]; ok {
		// Apply redirections for builtins: create child env with redirected stdout
		if len(node.Redirs) > 0 {
			redirectW := scope.GetCtx().Stdout
			var files []*os.File
			for _, r := range node.Redirs {
				target, _ := evalRedirTarget(r, scope)
				if strings.HasPrefix(target, "&") {
					fdStr := target[1:]
					switch fdStr {
					case "2":
						if r.Fd == 1 || r.Fd == 0 { redirectW = os.Stderr }
					}
					continue
				}
				switch r.Op {
				case ast.TGt:
					f, ferr := os.Create(target)
					if ferr != nil { return core.Nil, ferr }
					files = append(files, f)
					if r.Fd == 1 || r.Fd == 0 { redirectW = f }
				case ast.TRedirAppend:
					f, ferr := os.OpenFile(target, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if ferr != nil { return core.Nil, ferr }
					files = append(files, f)
					if r.Fd == 1 || r.Fd == 0 { redirectW = f }
				}
			}
			// Create child env with redirected Ctx — no save/restore needed
			redirEnv := core.NewEnv(scope)
			redirEnv.Ctx = scope.GetCtx().ForRedirect(redirectW)
			code, err := b(expanded, redirEnv)
			scope.GetCtx().SetExit(code)
			for _, f := range files { f.Close() }
			if err != nil {
				if err == core.ErrReturn || err == core.ErrBreak || err == core.ErrContinue || err == core.ErrExit {
					return core.Nil, err
				}
				fmt.Fprintln(os.Stderr, err)
			}
			return core.Nil, nil
		}
		code, err := b(expanded, scope)
		scope.GetCtx().SetExit(code)
		if err != nil {
			if err == core.ErrReturn || err == core.ErrBreak || err == core.ErrContinue {
				return core.Nil, err
			}
			fmt.Fprintln(os.Stderr, err)
		}
		return core.Nil, nil
	}

	if len(expanded) == 0 {
		if v, ok := scope.Get(name); ok {
			return v, nil
		}
	}

	result, err := evalExternalCmd(name, expanded, node.Redirs, scope)
	return result, err
}

// evalFnArgs evaluates command arguments as expressions for function calls.
func evalFnArgs(node *ast.Node, scope core.Scope) ([]core.Value, error) {
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
			return nil, err
		}
		argVals = append(argVals, v)
	}
	return argVals, nil
}

func applyRedirects(cmd *exec.Cmd, redirs []ast.Redir, scope core.Scope) (cleanup func(), err error) {
	var files []*os.File
	cleanup = func() {
		for _, f := range files {
			f.Close()
		}
	}

	for _, r := range redirs {
		target, _ := evalRedirTarget(r, scope)

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
		case ast.TGt:
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
		case ast.TLt:
			f, ferr := os.Open(target)
			if ferr != nil {
				cleanup()
				return nil, ferr
			}
			files = append(files, f)
			cmd.Stdin = f
		case ast.THeredoc:
			content, _ := evalRedirTarget(r, scope)
			cmd.Stdin = strings.NewReader(content)
		case ast.THereString:
			content, _ := evalRedirTarget(r, scope)
			cmd.Stdin = strings.NewReader(content + "\n")
		}
	}

	return cleanup, nil
}

// writeValueRedirs applies IO.unlines to a value and writes the result to
// redirect targets. Used when a value-producing expression (function call,
// pipeline, etc.) has shell redirects attached.
func writeValueRedirs(val core.Value, redirs []ast.Redir, scope core.Scope) error {
	text := stdlib.Lines(val)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}

	for _, r := range redirs {
		target, err := evalRedirTarget(r, scope)
		if err != nil {
			return err
		}

		switch r.Op {
		case ast.TGt:
			f, ferr := os.Create(target)
			if ferr != nil {
				return ferr
			}
			fmt.Fprint(f, text)
			f.Close()
		case ast.TRedirAppend:
			f, ferr := os.OpenFile(target, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if ferr != nil {
				return ferr
			}
			fmt.Fprint(f, text)
			f.Close()
		}
	}
	return nil
}

// resolveFn checks if a name resolves to a callable function at runtime.
// Returns nil if the name is a builtin, external command, or not found.
func resolveFn(name string, scope core.Scope) *core.FnValue {
	// Variable holding a function
	if v, ok := scope.Get(name); ok && v.Kind == core.VFn && v.GetFn() != nil {
		return v.GetFn()
	}
	r := ResolveCmd(name, scope)
	switch r.Kind {
	case KindModuleFn, KindModuleNativeFn, KindUserFn, KindVarFn:
		return r.Fn
	}
	return nil
}

// evalCallFromCmd dispatches an NCmd node as a function call. Used as
// fallback when the parser couldn't determine the name was a function
// (dynamic definitions, sourced files, etc.).
func evalCallFromCmd(fn *core.FnValue, node *ast.Node, scope core.Scope) (core.Value, error) {
	argVals, err := evalFnArgs(node, scope)
	if err != nil {
		return core.Nil, err
	}
	if fn.Native != nil {
		return fn.Native(argVals, scope)
	}
	if node.Tail {
		return core.TailCallVal(fn, argVals), nil
	}
	return CallFn(fn, argVals, scope)
}

func evalExternalCmd(name string, args []string, redirs []ast.Redir, scope core.Scope) (core.Value, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	if f, ok := scope.GetCtx().Stdout.(*os.File); ok {
		cmd.Stdout = f
	} else {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr
	cmd.Env = scope.NearestEnv().BuildEnv()

	ttyFd := int(os.Stdin.Fd())
	isTTY := term.IsTerminal(ttyFd)

	// Put child in its own process group for job control
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	cleanup, err := applyRedirects(cmd, redirs, scope)
	if err != nil {
		return core.Nil, err
	}
	defer cleanup()

	// Reset job control signals to SIG_DFL so the child inherits default
	// disposition through exec. Go issue #20479.
	signal.Reset(syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)

	if err := cmd.Start(); err != nil {
		renotifyJobSignals()
		scope.GetCtx().SetExit(127)
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
		scope.GetCtx().SetExit(127)
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
		scope.GetCtx().SetExit(148)
		return core.Nil, nil
	}

	scope.GetCtx().SetExit(ws.ExitStatus())
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


func evalBg(node *ast.Node, scope core.Scope) (core.Value, error) {
	child := node.Children[0]

	if child.Kind == ast.NCmd && len(child.Children) > 0 {
		nameNode := child.Children[0]
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
		if r.Kind == KindExternal || r.Kind == KindNotFound {
			var args []string
			for _, c := range child.Children[1:] {
				v, err := Eval(c, scope)
				if err != nil {
					return core.Nil, err
				}
				args = append(args, v.ToStr())
			}
			expanded := expandGlobs(args)
			cmd := exec.Command(name, expanded...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = scope.GetCtx().Stdout
			cmd.Stderr = os.Stderr
			cmd.Env = scope.NearestEnv().BuildEnv()
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

			err := cmd.Start()
			if err != nil {
				return core.Nil, fmt.Errorf("ish: %s: %s", name, err)
			}

			pid := cmd.Process.Pid
			cmdStr := name + " " + strings.Join(expanded, " ")
			jobID := jobs.AddJob(pid, strings.TrimSpace(cmdStr), cmd.Process)
		 scope.GetCtx().LastBg = pid
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

	bgEnv := core.NewEnv(scope)
	cmdStr := "builtin"
	if child.Kind == ast.NCmd && len(child.Children) > 0 {
		cmdStr = child.Children[0].Tok.Val
	}
	jobID := jobs.AddJob(0, cmdStr, nil)
	j := jobs.FindJob(jobID)
	go func() {
		Eval(child, bgEnv)
		if j != nil {
			j.Mu.Lock()
			j.Status = "Done"
			j.Mu.Unlock()
			close(j.Done)
		}
	}()
	return core.Nil, nil
}
