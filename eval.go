package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// osMu protects process-global OS state (cwd, umask) during subshell execution.
var osMu sync.Mutex

func Eval(node *Node, env *Env) (Value, error) {
	if node == nil {
		return Nil, nil
	}

	switch node.Kind {
	case NBlock:
		return evalBlock(node, env)
	case NCmd:
		return evalCmd(node, env)
	case NAssign:
		return evalPosixAssign(node, env)
	case NMatch:
		return evalMatch(node, env)
	case NPipe:
		return evalPipe(node, env)
	case NPipeFn:
		return evalPipeFn(node, env)
	case NAndList:
		return evalAndList(node, env)
	case NOrList:
		return evalOrList(node, env)
	case NBg:
		return evalBg(node, env)
	case NSubshell:
		return evalSubshell(node, env)
	case NGroup:
		return evalGroup(node, env)
	case NRedir:
		return evalRedir(node, env)
	case NIf:
		return evalIf(node, env)
	case NFor:
		return evalFor(node, env)
	case NWhile:
		return evalWhileUntil(node, env, false)
	case NUntil:
		return evalWhileUntil(node, env, true)
	case NCase:
		return evalCase(node, env)
	case NLit:
		return evalLit(node, env)
	case NWord:
		return evalWord(node, env)
	case NBinOp:
		return evalBinOp(node, env)
	case NUnary:
		return evalUnary(node, env)
	case NTuple:
		return evalTuple(node, env)
	case NList:
		return evalList(node, env)
	case NMap:
		return evalMap(node, env)
	case NAccess:
		return evalAccess(node, env)
	case NIshFn:
		return evalIshFn(node, env)
	case NIshMatch:
		return evalIshMatch(node, env)
	case NIshSpawn:
		return evalIshSpawn(node, env)
	case NIshSpawnLink:
		return evalIshSpawnLink(node, env)
	case NIshSend:
		return evalIshSend(node, env)
	case NIshReceive:
		return evalIshReceive(node, env)
	case NIshMonitor:
		return evalIshMonitor(node, env)
	case NIshAwait:
		return evalIshAwait(node, env)
	case NIshSupervise:
		return evalIshSupervise(node, env)
	case NIshTry:
		return evalIshTry(node, env)
	case NFnDef:
		return evalPosixFnDef(node, env)
	default:
		return Nil, fmt.Errorf("unknown node kind: %d", node.Kind)
	}
}

// evalCmdArg evaluates a node in command-argument context.
// Bare words are literal strings (with $var expansion), NOT variable references.
// This is what makes `getopts "abc" opt` pass "opt" as a literal,
// and `echo hello` pass "hello" as a literal.
// Expression nodes (tuples, parens, binops) are still evaluated as expressions.
func evalCmdArg(node *Node, env *Env) (Value, error) {
	switch node.Kind {
	case NWord:
		name := node.Tok.Val
		// Handle special words
		switch name {
		case "nil":
			return Nil, nil
		case "true":
			return True, nil
		case "false":
			return False, nil
		case "self":
			if proc := env.getProc(); proc != nil {
				return Value{Kind: VPid, Pid: proc}, nil
			}
			return Nil, nil
		}
		// Tilde expansion
		if strings.HasPrefix(name, "~") {
			return StringVal(env.expandTilde(name)), nil
		}
		// Arithmetic expansion
		if strings.HasPrefix(name, "$((") && strings.HasSuffix(name, "))") {
			return evalArithExpansion(name, env)
		}
		// Command substitution
		if strings.HasPrefix(name, "$(") && strings.HasSuffix(name, ")") {
			return evalCmdSub(name[2:len(name)-1], env)
		}
		// Parameter expansion ($var, ${var}, etc.)
		if strings.Contains(name, "$") || strings.Contains(name, "#{") {
			return StringVal(env.Expand(name)), nil
		}
		// Bare word in command context — LITERAL string, no variable lookup.
		// Strip embedded quotes (e.g., ll='ls -la' → ll=ls -la)
		if strings.ContainsAny(name, "'\"") {
			name = stripAssignQuotes(name)
		}
		return StringVal(name), nil
	case NLit:
		return evalLit(node, env)
	default:
		// Expression nodes (tuples, parens, binops, etc.) — full evaluation
		return Eval(node, env)
	}
}

func evalBlock(node *Node, env *Env) (Value, error) {
	var last Value
	for _, child := range node.Children {
		v, err := Eval(child, env)
		if err != nil {
			return v, err
		}
		last = v
		// set -e: exit on error (skip for if/while/until conditions, &&/|| chains)
		if env.shouldExitOnError() {
			if child.Kind != NIf && child.Kind != NWhile && child.Kind != NUntil &&
				child.Kind != NAndList && child.Kind != NOrList {
				return last, errSetE
			}
		}
	}
	return last, nil
}

func evalCmd(node *Node, env *Env) (Value, error) {
	if len(node.Children) == 0 {
		return Nil, nil
	}

	// Process prefix assignments (FOO=bar cmd args...)
	// POSIX: prefix assignments are exported to the command's environment
	for _, assign := range node.Assigns {
		if _, err := evalPosixAssign(assign, env); err != nil {
			return Nil, err
		}
	}

	// Get the command name from the first child without full evaluation
	// (to avoid "true" -> :true atom conversion)
	nameNode := node.Children[0]
	var name string
	if nameNode.Kind == NWord {
		name = env.Expand(nameNode.Tok.Val)
	} else {
		v, err := Eval(nameNode, env)
		if err != nil {
			return Nil, err
		}
		name = v.ToStr()
	}

	// Check alias expansion (only for simple word names, not $var names)
	if nameNode.Kind == NWord && !strings.Contains(nameNode.Tok.Val, "$") {
		if aliasVal, ok := env.GetAlias(name); ok {
			// Avoid infinite recursion: don't expand if the expansion starts with the same word
			firstWord := aliasVal
			if sp := strings.IndexByte(aliasVal, ' '); sp >= 0 {
				firstWord = aliasVal[:sp]
			}
			if firstWord != name {
				// Re-parse the alias expansion with the original args
				var argStr strings.Builder
				for _, child := range node.Children[1:] {
					argStr.WriteString(" ")
					argStr.WriteString(child.Tok.Val)
				}
				newSrc := aliasVal + argStr.String()
				tokens := Lex(newSrc)
				newNode, err := Parse(tokens)
				if err != nil {
					return Nil, err
				}
				return Eval(newNode, env)
			}
		}
	}

	// Resolution order determines how args are evaluated:
	// - User functions: args evaluated as expressions (variable lookup)
	// - Builtins/external: args evaluated as literals (no variable lookup)

	// 1. User function — evaluate args as expressions, pass Values directly
	if fn, ok := env.GetFn(name); ok {
		argVals := make([]Value, 0, len(node.Children)-1)
		for _, child := range node.Children[1:] {
			if child.Kind == NWord && child.Tok.Val == "$@" {
				// "$@" expands to separate words, one per positional param
				for _, arg := range env.posArgs() {
					argVals = append(argVals, StringVal(arg))
				}
				continue
			}
			v, err := Eval(child, env)
			if err != nil {
				return Nil, err
			}
			argVals = append(argVals, v)
		}
		return callFn(fn, argVals, env)
	}

	// 1b. Native (stdlib) function — evaluate args as expressions, like user fns
	if nfn, ok := env.GetNativeFn(name); ok {
		argVals := make([]Value, 0, len(node.Children)-1)
		for _, child := range node.Children[1:] {
			if child.Kind == NWord && child.Tok.Val == "$@" {
				for _, arg := range env.posArgs() {
					argVals = append(argVals, StringVal(arg))
				}
				continue
			}
			v, err := Eval(child, env)
			if err != nil {
				return Nil, err
			}
			argVals = append(argVals, v)
		}
		return nfn(argVals, env)
	}

	// 2. Builtins and external commands — args are literal (command context)
	argVals := make([]Value, 0, len(node.Children)-1)
	for _, child := range node.Children[1:] {
		v, err := evalCmdArg(child, env)
		if err != nil {
			return Nil, err
		}
		argVals = append(argVals, v)
	}

	// Convert to strings for builtins and external commands.
	// Don't re-expand values — evalWord already handled $var expansion.
	// Apply IFS splitting to unquoted expanded arguments.
	strArgs := make([]string, 0, len(argVals))
	quotedFlags := make([]bool, 0, len(argVals))
	for i, v := range argVals {
		s := v.ToStr()
		argNode := node.Children[i+1]
		// Don't split quoted strings (single or double quoted)
		if argNode.Kind == NLit && argNode.Tok.Type == TString {
			strArgs = append(strArgs, s)
			quotedFlags = append(quotedFlags, true)
		} else if argNode.Kind == NWord && argNode.Tok.Val == "$@" {
			// "$@" expands to separate words, one per positional param
			for _, arg := range env.posArgs() {
				strArgs = append(strArgs, arg)
				quotedFlags = append(quotedFlags, true) // each param is a separate quoted word
			}
		} else if argNode.Kind == NWord && !strings.Contains(argNode.Tok.Val, "$") {
			// Bare word with no variable expansion — already a single field from the lexer
			strArgs = append(strArgs, s)
			quotedFlags = append(quotedFlags, false)
		} else {
			// Contains variable expansion — field split the expanded result on IFS
			fields := env.SplitFieldsIFS(s)
			for range fields {
				quotedFlags = append(quotedFlags, false)
			}
			strArgs = append(strArgs, fields...)
		}
	}
	expanded := expandGlobsSelective(strArgs, quotedFlags)

	// set -x: trace commands to stderr
	if env.HasFlag('x') {
		fmt.Fprintf(os.Stderr, "+ %s\n", strings.Join(append([]string{name}, expanded...), " "))
	}

	// Special handling for exec with redirections but no command args
	if name == "exec" && len(expanded) == 0 && len(node.Redirs) > 0 {
		for _, r := range node.Redirs {
			target := env.Expand(r.Target)
			switch r.Op {
			case TRedirOut:
				f, err := os.Create(target)
				if err != nil {
					return Nil, err
				}
				switch r.Fd {
				case 1:
					env.stdout = f
				case 2:
					os.Stderr = f
				}
			case TRedirAppend:
				f, err := os.OpenFile(target, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					return Nil, err
				}
				switch r.Fd {
				case 1:
					env.stdout = f
				case 2:
					os.Stderr = f
				}
			case TRedirIn:
				f, err := os.Open(target)
				if err != nil {
					return Nil, err
				}
				os.Stdin = f
			}
		}
		env.setExit(0)
		return Nil, nil
	}

	// 2. Builtin
	if b, ok := builtins[name]; ok {
		code, err := b(expanded, env)
		env.setExit(code)
		if err != nil {
			if err == errReturn || err == errBreak || err == errContinue {
				return Nil, err
			}
			fmt.Fprintln(os.Stderr, err)
		}
		return Nil, nil
	}

	// 3. Single-word command that is a known variable — return its value.
	// This handles cases like clause bodies where `msg` is a bound variable
	// that should evaluate to its value, not be executed as a command.
	if len(expanded) == 0 {
		if v, ok := env.Get(name); ok {
			return v, nil
		}
	}

	// 4. External command
	result, err := evalExternalCmd(name, expanded, node.Redirs, env)
	return result, err
}

// applyRedirects applies redirect operations to an exec.Cmd. Returns a cleanup
// function that should be deferred to close any opened files.
func applyRedirects(cmd *exec.Cmd, redirs []Redir, env *Env) (cleanup func(), err error) {
	var files []*os.File
	cleanup = func() {
		for _, f := range files {
			f.Close()
		}
	}

	for _, r := range redirs {
		target := env.Expand(r.Target)

		// Handle fd duplication: >&2, 2>&1
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
		case TRedirOut:
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
		case TRedirAppend:
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
		case TRedirIn:
			f, ferr := os.Open(target)
			if ferr != nil {
				cleanup()
				return nil, ferr
			}
			files = append(files, f)
			cmd.Stdin = f
		case THeredoc:
			// target is the heredoc content — pipe it to stdin
			// Use raw r.Target and expand only for unquoted heredocs,
			// since line-level env.Expand(r.Target) already ran on target.
			content := r.Target
			if !r.Quoted {
				content = env.Expand(content)
			}
			cmd.Stdin = strings.NewReader(content)
		case THereString:
			// <<< string — pipe the string + newline to stdin
			content := r.Target
			if !r.Quoted {
				content = env.Expand(content)
			}
			cmd.Stdin = strings.NewReader(content + "\n")
		}
	}

	return cleanup, nil
}

func evalExternalCmd(name string, args []string, redirs []Redir, env *Env) (Value, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	// Use env's stdout if it's an *os.File, otherwise fall back to os.Stdout
	if f, ok := env.Stdout().(*os.File); ok {
		cmd.Stdout = f
	} else {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr
	cmd.Env = env.BuildEnv()

	// Apply redirections
	cleanup, err := applyRedirects(cmd, redirs, env)
	if err != nil {
		return Nil, err
	}
	defer cleanup()

	err = cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			env.setExit(exitErr.ExitCode())
			return Nil, nil
		}
		env.setExit(127)
		fmt.Fprintf(os.Stderr, "ish: %s: %s\n", name, err)
		return Nil, nil
	}
	env.setExit(0)
	return Nil, nil
}

// stripAssignQuotes strips quotes segment-by-segment from an assignment value.
// Handles mixed quotes: "hello"'world' -> helloworld
// Double-quoted segments support backslash escaping; single-quoted segments are literal.
func stripAssignQuotes(s string) string {
	var buf strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '"' {
			i++ // skip opening "
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					i++ // skip backslash
				}
				buf.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				i++ // skip closing "
			}
		} else if s[i] == '\'' {
			i++ // skip opening '
			for i < len(s) && s[i] != '\'' {
				buf.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				i++ // skip closing '
			}
		} else {
			buf.WriteByte(s[i])
			i++
		}
	}
	return buf.String()
}

func evalPosixAssign(node *Node, env *Env) (Value, error) {
	s := node.Tok.Val
	i := strings.IndexByte(s, '=')
	if i < 0 {
		return Nil, fmt.Errorf("invalid assignment: %s", s)
	}
	name := s[:i]
	raw := s[i+1:]
	// Strip quotes segment-by-segment (handles mixed quotes like "hello"'world')
	raw = stripAssignQuotes(raw)
	val := raw
	// Only expand if not single-quoted
	if len(s) > i+1 && s[i+1] != '\'' {
		val = env.Expand(raw)
	}
	if err := env.Set(name, StringVal(val)); err != nil {
		return Nil, err
	}
	return Nil, nil
}

func evalMatch(node *Node, env *Env) (Value, error) {
	if len(node.Children) != 2 {
		return Nil, fmt.Errorf("invalid match node")
	}
	lhs := node.Children[0]
	rhs := node.Children[1]

	val, err := Eval(rhs, env)
	if err != nil {
		return Nil, err
	}

	if err := patternBind(lhs, val, env); err != nil {
		return Nil, err
	}
	return val, nil
}

// patternBind matches a pattern node against a value and binds variables.
// Uses SetLocal so bindings stay in the current scope (ish match semantics).
func patternBind(pat *Node, val Value, env *Env) error {
	switch pat.Kind {
	case NWord:
		if pat.Tok.Val == "_" {
			return nil // wildcard
		}
		if err := env.SetLocal(pat.Tok.Val, val); err != nil {
			return err
		}
		return nil
	case NLit:
		expected, _ := litToValue(pat)
		if !expected.Equal(val) {
			return fmt.Errorf("match error: expected %s, got %s", expected.Inspect(), val.Inspect())
		}
		return nil
	case NTuple:
		if val.Kind != VTuple || len(val.Elems) != len(pat.Children) {
			return fmt.Errorf("match error: expected %d-tuple, got %s", len(pat.Children), val.Inspect())
		}
		for i, child := range pat.Children {
			if err := patternBind(child, val.Elems[i], env); err != nil {
				return err
			}
		}
		return nil
	case NList:
		if val.Kind != VList {
			return fmt.Errorf("match error: expected list, got %s", val.Inspect())
		}
		if pat.Rest != nil {
			// [h | t] or [a, b | rest] pattern — head elements + rest
			if len(val.Elems) < len(pat.Children) {
				return fmt.Errorf("match error: list has %d elements, need at least %d", len(val.Elems), len(pat.Children))
			}
			for i, child := range pat.Children {
				if err := patternBind(child, val.Elems[i], env); err != nil {
					return err
				}
			}
			remaining := val.Elems[len(pat.Children):]
			restVal := ListVal(remaining...)
			return patternBind(pat.Rest, restVal, env)
		}
		if len(pat.Children) != len(val.Elems) {
			return fmt.Errorf("match error: list length mismatch")
		}
		for i, child := range pat.Children {
			if err := patternBind(child, val.Elems[i], env); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported pattern kind: %d", pat.Kind)
}

func patternMatches(pat *Node, val Value, env *Env) bool {
	switch pat.Kind {
	case NWord:
		return true // variable always matches
	case NLit:
		expected, _ := litToValue(pat)
		return expected.Equal(val)
	case NTuple:
		if val.Kind != VTuple || len(val.Elems) != len(pat.Children) {
			return false
		}
		for i, child := range pat.Children {
			if !patternMatches(child, val.Elems[i], env) {
				return false
			}
		}
		return true
	case NList:
		if val.Kind != VList {
			return false
		}
		if pat.Rest != nil {
			// [h | t] or [a, b | rest] pattern — head elements + rest
			if len(val.Elems) < len(pat.Children) {
				return false
			}
			for i, child := range pat.Children {
				if !patternMatches(child, val.Elems[i], env) {
					return false
				}
			}
			// Rest always matches (it's a variable or _)
			return patternMatches(pat.Rest, ListVal(val.Elems[len(pat.Children):]...), env)
		}
		if len(val.Elems) != len(pat.Children) {
			return false
		}
		for i, child := range pat.Children {
			if !patternMatches(child, val.Elems[i], env) {
				return false
			}
		}
		return true
	}
	return false
}

func evalPipe(node *Node, env *Env) (Value, error) {
	left := node.Children[0]
	right := node.Children[1]
	pipeStderr := node.Tok.Val == "|&" // |& pipes both stdout and stderr

	pr, pw, err := os.Pipe()
	if err != nil {
		return Nil, err
	}

	done := make(chan error, 1)
	leftEnv := CopyEnv(env) // full copy for goroutine safety; CoW would optimize but adds complexity
	if pipeStderr {
		leftEnv.stdout = pw // stdout goes to pipe
		// stderr also goes to pipe — handled in evalWithIO via the env
	}
	go func() {
		_, err := evalWithIO(left, leftEnv, os.Stdin, pw)
		pw.Close()
		done <- err
	}()

	// Use env's stdout for the final stage — respects command substitution
	finalStdout := os.Stdout
	if f, ok := env.Stdout().(*os.File); ok {
		finalStdout = f
	}
	val, err2 := evalWithIO(right, env, pr, finalStdout)
	pr.Close()
	<-done

	return val, err2
}

func evalWithIO(node *Node, env *Env, stdin *os.File, stdout *os.File) (Value, error) {
	// For commands, we need to redirect their stdin/stdout
	if node.Kind == NCmd {
		if len(node.Children) == 0 {
			return Nil, nil
		}
		// Get the command name
		nameNode := node.Children[0]
		var name string
		if nameNode.Kind == NWord {
			name = env.Expand(nameNode.Tok.Val)
		} else {
			v, err := Eval(nameNode, env)
			if err != nil {
				return Nil, err
			}
			name = v.ToStr()
		}

		// User function — eval args as expressions, redirect stdout
		if fn, ok := env.GetFn(name); ok {
			pipeEnv := NewEnv(env)
			pipeEnv.stdout = stdout
			argVals := make([]Value, 0, len(node.Children)-1)
			for _, child := range node.Children[1:] {
				if child.Kind == NWord && child.Tok.Val == "$@" {
					for _, arg := range env.posArgs() {
						argVals = append(argVals, StringVal(arg))
					}
					continue
				}
				v, err := Eval(child, env)
				if err != nil {
					return Nil, err
				}
				argVals = append(argVals, v)
			}
			return callFn(fn, argVals, pipeEnv)
		}

		// Native (stdlib) function — eval args as expressions, redirect stdout
		if nfn, ok := env.GetNativeFn(name); ok {
			pipeEnv := NewEnv(env)
			pipeEnv.stdout = stdout
			argVals := make([]Value, 0, len(node.Children)-1)
			for _, child := range node.Children[1:] {
				if child.Kind == NWord && child.Tok.Val == "$@" {
					for _, arg := range env.posArgs() {
						argVals = append(argVals, StringVal(arg))
					}
					continue
				}
				v, err := Eval(child, env)
				if err != nil {
					return Nil, err
				}
				argVals = append(argVals, v)
			}
			return nfn(argVals, pipeEnv)
		}

		// Builtins and external: eval args as command args (literal context)
		var strArgs []string
		for _, child := range node.Children[1:] {
			if child.Kind == NWord && child.Tok.Val == "$@" {
				// "$@" expands to separate words, one per positional param
				strArgs = append(strArgs, env.posArgs()...)
				continue
			}
			v, err := evalCmdArg(child, env)
			if err != nil {
				return Nil, err
			}
			strArgs = append(strArgs, v.ToStr())
		}
		expanded := expandGlobs(strArgs)

		if b, ok := builtins[name]; ok {
			pipeEnv := NewEnv(env)
			pipeEnv.stdout = stdout
			code, err := b(expanded, pipeEnv)
			env.setExit(code)
			if err != nil && err != errReturn && err != errBreak && err != errContinue {
				fmt.Fprintln(os.Stderr, err)
			}
			return Nil, nil
		}

		cmd := exec.Command(name, expanded...)
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Env = env.BuildEnv()
		// If env.stdout was set to the pipe writer (|&), stderr goes there too
		if envOut, ok := env.Stdout().(*os.File); ok && envOut == stdout {
			cmd.Stderr = stdout
		} else {
			cmd.Stderr = os.Stderr
		}
		// Apply redirects from the AST node using the shared function
		cleanup, redirErr := applyRedirects(cmd, node.Redirs, env)
		if redirErr != nil {
			return Nil, redirErr
		}
		defer cleanup()
		err := cmd.Run()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				env.setExit(exitErr.ExitCode())
			} else {
				env.setExit(127)
			}
		} else {
			env.setExit(0)
		}
		return Nil, nil
	}

	// For pipes of pipes, recurse
	if node.Kind == NPipe {
		inner := node.Children[0]
		right := node.Children[1]

		pr2, pw2, err := os.Pipe()
		if err != nil {
			return Nil, err
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

	// For compound commands (groups, subshells, if/for/while, etc.),
	// redirect stdout so their output goes through the pipe.
	pipeEnv := NewEnv(env)
	pipeEnv.stdout = stdout
	return Eval(node, pipeEnv)
}

func evalPipeFn(node *Node, env *Env) (Value, error) {
	left, err := Eval(node.Children[0], env)
	if err != nil {
		return Nil, err
	}
	right := node.Children[1]

	// The right side should be a function call — pass left as first arg
	switch right.Kind {
	case NCmd:
		if len(right.Children) == 0 {
			return Nil, fmt.Errorf("pipe arrow requires a function name on the right")
		}
		nameVal, err := Eval(right.Children[0], env)
		if err != nil {
			return Nil, err
		}
		name := nameVal.ToStr()

		// Evaluate remaining args, prepend left
		argVals := []Value{left}
		for _, child := range right.Children[1:] {
			v, err := Eval(child, env)
			if err != nil {
				return Nil, err
			}
			argVals = append(argVals, v)
		}

		// Try user fn
		if fn, ok := env.GetFn(name); ok {
			return callFn(fn, argVals, env)
		}
		// Try native (stdlib) fn
		if nfn, ok := env.GetNativeFn(name); ok {
			return nfn(argVals, env)
		}
		// Builtins / external: convert to strings
		strArgs := make([]string, len(argVals))
		for i, v := range argVals {
			strArgs[i] = v.ToStr()
		}
		if b, ok := builtins[name]; ok {
			code, err := b(strArgs, env)
			env.setExit(code)
			if err != nil {
				return Nil, err
			}
			return Nil, nil
		}
		return evalExternalCmd(name, strArgs, nil, env)

	case NWord:
		name := right.Tok.Val
		argVals := []Value{left}
		if fn, ok := env.GetFn(name); ok {
			return callFn(fn, argVals, env)
		}
		if nfn, ok := env.GetNativeFn(name); ok {
			return nfn(argVals, env)
		}
		strArgs := []string{left.ToStr()}
		if b, ok := builtins[name]; ok {
			code, err := b(strArgs, env)
			env.setExit(code)
			if err != nil {
				return Nil, err
			}
			return Nil, nil
		}
		return evalExternalCmd(name, strArgs, nil, env)

	default:
		return Nil, fmt.Errorf("pipe arrow: right side must be a function or command")
	}
}

// syncExit updates lastExit based on expression truthiness.
// For commands, lastExit is already set. For ish expressions, we sync it.
func syncExit(val Value, env *Env) {
	if val.Kind != VNil {
		if val.Truthy() {
			env.setExit(0)
		} else {
			env.setExit(1)
		}
	}
}

func evalAndList(node *Node, env *Env) (Value, error) {
	left, err := Eval(node.Children[0], env)
	if err != nil {
		return left, err
	}
	syncExit(left, env)
	if env.exitCode() == 0 {
		right, err := Eval(node.Children[1], env)
		if err == nil {
			syncExit(right, env)
		}
		return right, err
	}
	return left, nil
}

func evalOrList(node *Node, env *Env) (Value, error) {
	left, err := Eval(node.Children[0], env)
	if err != nil {
		return left, err
	}
	syncExit(left, env)
	if env.exitCode() != 0 {
		right, err := Eval(node.Children[1], env)
		if err == nil {
			syncExit(right, env)
		}
		return right, err
	}
	return left, nil
}

func evalBg(node *Node, env *Env) (Value, error) {
	child := node.Children[0]

	// If the child is a simple external command, start it as an OS process
	// so we can track its PID for jobs/fg/bg/wait/kill.
	if child.Kind == NCmd && len(child.Children) > 0 {
		nameNode := child.Children[0]
		var name string
		if nameNode.Kind == NWord {
			name = env.Expand(nameNode.Tok.Val)
		} else {
			v, err := Eval(nameNode, env)
			if err != nil {
				return Nil, err
			}
			name = v.ToStr()
		}

		// Skip user fns and builtins — they run as goroutines
		_, isFn := env.GetFn(name)
		_, isBuiltin := builtins[name]

		if !isFn && !isBuiltin {
			// External command — start as OS process
			var args []string
			for _, c := range child.Children[1:] {
				v, err := evalCmdArg(c, env)
				if err != nil {
					return Nil, err
				}
				args = append(args, v.ToStr())
			}
			expanded := expandGlobs(args)
			cmd := exec.Command(name, expanded...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Env = env.BuildEnv()
			// Set process group so it can be managed independently
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

			err := cmd.Start()
			if err != nil {
				return Nil, fmt.Errorf("ish: %s: %s", name, err)
			}

			pid := cmd.Process.Pid
			cmdStr := name + " " + strings.Join(expanded, " ")
			jobID := AddJob(pid, strings.TrimSpace(cmdStr), cmd.Process)
			env.lastBg = pid
			fmt.Fprintf(os.Stderr, "[%d] %d\n", jobID, pid)

			// Wait in background goroutine to clean up.
			// Capture done channel so fg never hangs even if job is removed.
			j := FindJob(jobID)
			doneCh := j.done
			go func() {
				state, _ := cmd.Process.Wait()
				j := FindJobByPid(pid)
				if j != nil {
					j.mu.Lock()
					j.Status = "Done"
					if state != nil {
						j.exitCode = state.ExitCode()
					}
					j.mu.Unlock()
				}
				close(doneCh)
			}()

			return Nil, nil
		}
	}

	// User fn, builtin, or complex command — run as goroutine
	bgEnv := NewEnv(env)
	go func() {
		Eval(child, bgEnv)
	}()
	return Nil, nil
}

func evalSubshell(node *Node, env *Env) (Value, error) {
	// Lock process-global OS state to prevent concurrent subshells from clobbering
	osMu.Lock()
	origCwd, _ := os.Getwd()
	origMask := syscall.Umask(0)
	syscall.Umask(origMask) // restore immediately — we just wanted to read it

	subEnv := CopyEnv(env)
	val, err := Eval(node.Children[0], subEnv)

	// Restore OS state
	os.Chdir(origCwd)
	syscall.Umask(origMask)
	osMu.Unlock()

	// Propagate exit code from subshell
	env.setExit(subEnv.exitCode())

	return val, err
}

func evalGroup(node *Node, env *Env) (Value, error) {
	return Eval(node.Children[0], env)
}

func evalRedir(node *Node, env *Env) (Value, error) {
	// Handled inline in evalCmd for now
	return Eval(node.Children[0], env)
}

func evalIf(node *Node, env *Env) (Value, error) {
	for _, clause := range node.Clauses {
		if clause.Pattern == nil {
			// else branch
			return Eval(clause.Body, env)
		}

		// Evaluate condition
		condVal, err := Eval(clause.Pattern, env)
		if err != nil {
			return Nil, err
		}

		// Sync expression truthiness with exit code
		syncExit(condVal, env)

		if env.exitCode() == 0 {
			return Eval(clause.Body, env)
		}
	}
	return Nil, nil
}

func evalFor(node *Node, env *Env) (Value, error) {
	varName := node.Children[0].Tok.Val
	words := node.Children[1:]
	body := node.Clauses[0].Body

	var last Value
	for _, w := range words {
		v, err := evalCmdArg(w, env)
		if err != nil {
			return Nil, err
		}
		val := v.ToStr()
		// Field splitting: split on whitespace (IFS)
		fields := splitFields(val)
		for _, field := range fields {
			expanded := expandGlob(field)
			for _, v := range expanded {
				env.Set(varName, StringVal(v))
				var err error
				last, err = Eval(body, env)
				if err == errBreak {
					return last, nil
				}
				if err == errContinue {
					continue
				}
				if err != nil {
					return last, err
				}
			}
		}
	}
	return last, nil
}

func evalWhileUntil(node *Node, env *Env, invert bool) (Value, error) {
	cond := node.Children[0]
	body := node.Clauses[0].Body
	var last Value
	for {
		condVal, err := Eval(cond, env)
		if err != nil {
			return Nil, err
		}
		syncExit(condVal, env)
		shouldRun := env.exitCode() == 0
		if invert {
			shouldRun = !shouldRun
		}
		if !shouldRun {
			break
		}
		last, err = Eval(body, env)
		if err == errBreak {
			env.setExit(0)
			return last, nil
		}
		if err == errContinue {
			continue
		}
		if err != nil {
			return last, err
		}
	}
	// POSIX: while/until returns 0 on normal termination
	env.setExit(0)
	return last, nil
}

func evalCase(node *Node, env *Env) (Value, error) {
	word := env.Expand(node.Children[0].Tok.Val)
	for _, clause := range node.Clauses {
		patStr := clause.Pattern.Tok.Val
		// Support alternation: pattern may contain | separating alternatives
		alternatives := strings.Split(patStr, "|")
		for _, alt := range alternatives {
			if alt == "*" || matchPattern(alt, word) {
				return Eval(clause.Body, env)
			}
		}
	}
	return Nil, nil
}

func evalLit(node *Node, env *Env) (Value, error) {
	if node.Tok.Type == TString && !node.Tok.Quoted {
		// Double-quoted string — expand $var and #{}
		return StringVal(env.Expand(node.Tok.Val)), nil
	}
	return litToValue(node)
}

func litToValue(node *Node) (Value, error) {
	switch node.Tok.Type {
	case TInt:
		n, _ := strconv.ParseInt(node.Tok.Val, 10, 64)
		return IntVal(n), nil
	case TString:
		return StringVal(node.Tok.Val), nil
	case TAtom:
		return AtomVal(node.Tok.Val), nil
	default:
		return StringVal(node.Tok.Val), nil
	}
}

func evalWord(node *Node, env *Env) (Value, error) {
	name := node.Tok.Val
	if name == "nil" {
		return Nil, nil
	}
	if name == "true" {
		return True, nil
	}
	if name == "false" {
		return False, nil
	}
	if name == "self" {
		if proc := env.getProc(); proc != nil {
			return Value{Kind: VPid, Pid: proc}, nil
		}
		return Nil, nil
	}

	// Tilde expansion
	if strings.HasPrefix(name, "~") {
		return StringVal(env.expandTilde(name)), nil
	}

	// Arithmetic expansion $(( ))
	if strings.HasPrefix(name, "$((") && strings.HasSuffix(name, "))") {
		return evalArithExpansion(name, env)
	}

	// Command substitution $(...)
	if strings.HasPrefix(name, "$(") && strings.HasSuffix(name, ")") {
		inner := name[2 : len(name)-1]
		return evalCmdSub(inner, env)
	}

	// If contains $ or #{, expand parameters
	if strings.Contains(name, "$") || strings.Contains(name, "#{") {
		// set -u check: error on unset variables
		if env.HasFlag('u') {
			if err := checkUnsetVars(name, env); err != nil {
				return Nil, err
			}
		}
		expanded := env.Expand(name)
		return StringVal(expanded), nil
	}

	// Look up as variable
	if v, ok := env.Get(name); ok {
		return v, nil
	}

	// Handle dot access: m.field
	if dotIdx := strings.IndexByte(name, '.'); dotIdx > 0 {
		objName := name[:dotIdx]
		field := name[dotIdx+1:]
		if obj, ok := env.Get(objName); ok && obj.Kind == VMap && obj.Map != nil {
			if v, ok := obj.Map.Get(field); ok {
				return v, nil
			}
		}
	}

	// Bare word — return as literal string
	return StringVal(name), nil
}

func evalArithExpansion(name string, env *Env) (Value, error) {
	inner := name[3 : len(name)-2]
	inner = env.Expand(inner)
	tokens := Lex(inner)
	for i := range tokens {
		if tokens[i].Type == TWord {
			if v, ok := env.Get(tokens[i].Val); ok {
				tokens[i] = Token{Type: TInt, Val: v.ToStr(), Pos: tokens[i].Pos}
			}
		}
	}
	node, err := Parse(tokens)
	if err != nil {
		return Nil, err
	}
	val, err := Eval(node, env)
	if err != nil {
		return Nil, err
	}
	return StringVal(val.ToStr()), nil
}

func evalCmdSub(cmdStr string, env *Env) (Value, error) {
	tokens := Lex(cmdStr)
	node, err := Parse(tokens)
	if err != nil {
		return Nil, err
	}

	r, w, err := os.Pipe()
	if err != nil {
		return Nil, fmt.Errorf("command substitution: %w", err)
	}

	// Run in a child env with stdout redirected to the pipe
	childEnv := NewEnv(env)
	childEnv.stdout = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	val, evalErr := Eval(node, childEnv)
	// If the eval produced a non-nil value (ish expression), write it to the pipe
	if val.Kind != VNil && val.Kind != VString {
		fmt.Fprint(w, val.String())
	} else if val.Kind == VString && val.Str != "" {
		fmt.Fprint(w, val.Str)
	}
	w.Close()
	<-done
	r.Close()

	result := strings.TrimRight(buf.String(), "\n")
	return StringVal(result), evalErr
}

func evalBinOp(node *Node, env *Env) (Value, error) {
	left, err := Eval(node.Children[0], env)
	if err != nil {
		return Nil, err
	}
	right, err := Eval(node.Children[1], env)
	if err != nil {
		return Nil, err
	}

	// If both are ints, do integer arithmetic
	if left.Kind == VInt && right.Kind == VInt {
		switch node.Tok.Type {
		case TPlus:
			return IntVal(left.Int + right.Int), nil
		case TMinus:
			return IntVal(left.Int - right.Int), nil
		case TMul:
			return IntVal(left.Int * right.Int), nil
		case TDiv:
			if right.Int == 0 {
				return Nil, fmt.Errorf("division by zero")
			}
			return IntVal(left.Int / right.Int), nil
		case TEq:
			return boolVal(left.Int == right.Int), nil
		case TNe:
			return boolVal(left.Int != right.Int), nil
		case TRedirIn:
			return boolVal(left.Int < right.Int), nil
		case TRedirOut:
			return boolVal(left.Int > right.Int), nil
		case TLe:
			return boolVal(left.Int <= right.Int), nil
		case TGe:
			return boolVal(left.Int >= right.Int), nil
		}
	}

	// String concatenation with +
	if node.Tok.Type == TPlus && (left.Kind == VString || right.Kind == VString) {
		return StringVal(left.ToStr() + right.ToStr()), nil
	}

	// General equality
	switch node.Tok.Type {
	case TEq:
		return boolVal(left.Equal(right)), nil
	case TNe:
		return boolVal(!left.Equal(right)), nil
	}

	return Nil, fmt.Errorf("unsupported operation: %s %s %s", left.Inspect(), node.Tok.Val, right.Inspect())
}

func evalUnary(node *Node, env *Env) (Value, error) {
	operand, err := Eval(node.Children[0], env)
	if err != nil {
		return Nil, err
	}
	switch node.Tok.Type {
	case TBang:
		return boolVal(!operand.Truthy()), nil
	case TMinus:
		if operand.Kind == VInt {
			return IntVal(-operand.Int), nil
		}
		return Nil, fmt.Errorf("cannot negate %s", operand.Inspect())
	}
	return Nil, fmt.Errorf("unknown unary op: %s", node.Tok.Val)
}

func evalTuple(node *Node, env *Env) (Value, error) {
	elems := make([]Value, len(node.Children))
	for i, child := range node.Children {
		v, err := Eval(child, env)
		if err != nil {
			return Nil, err
		}
		elems[i] = v
	}
	return TupleVal(elems...), nil
}

func evalList(node *Node, env *Env) (Value, error) {
	elems := make([]Value, len(node.Children))
	for i, child := range node.Children {
		v, err := Eval(child, env)
		if err != nil {
			return Nil, err
		}
		elems[i] = v
	}
	return ListVal(elems...), nil
}

func evalMap(node *Node, env *Env) (Value, error) {
	m := NewOrdMap()
	for i := 0; i+1 < len(node.Children); i += 2 {
		key := node.Children[i].Tok.Val
		val, err := Eval(node.Children[i+1], env)
		if err != nil {
			return Nil, err
		}
		m.Set(key, val)
	}
	return Value{Kind: VMap, Map: m}, nil
}

func evalAccess(node *Node, env *Env) (Value, error) {
	obj, err := Eval(node.Children[0], env)
	if err != nil {
		return Nil, err
	}
	field := node.Tok.Val
	if obj.Kind == VMap && obj.Map != nil {
		if v, ok := obj.Map.Get(field); ok {
			return v, nil
		}
	}
	return Nil, fmt.Errorf("no field %s on %s", field, obj.Inspect())
}

func evalIshFn(node *Node, env *Env) (Value, error) {
	name := node.Tok.Val

	// Multi-clause fn: fn name do pattern -> body; pattern -> body; end
	// Detected by: no Children (params) but multiple clauses with Patterns
	if len(node.Children) == 0 && len(node.Clauses) > 0 && node.Clauses[0].Pattern != nil {
		var fnClauses []FnClause
		for _, clause := range node.Clauses {
			var params []Node
			if clause.Pattern != nil {
				if clause.Pattern.Kind == NBlock {
					for _, child := range clause.Pattern.Children {
						params = append(params, *child)
					}
				} else {
					params = append(params, *clause.Pattern)
				}
			}
			fnClauses = append(fnClauses, FnClause{
				Params: params,
				Guard:  clause.Guard,
				Body:   clause.Body,
			})
		}
		fnVal := &FnValue{Name: name, Clauses: fnClauses}
		if name == "<anon>" {
			return Value{Kind: VFn, Fn: fnVal}, nil
		}
		env.SetFn(name, fnVal)
		return Nil, nil
	}

	// Single-clause fn with explicit params
	var params []Node
	for _, child := range node.Children {
		params = append(params, *child)
	}

	clause := FnClause{
		Params: params,
		Guard:  node.Clauses[0].Guard,
		Body:   node.Clauses[0].Body,
	}

	fnVal := &FnValue{Name: name, Clauses: []FnClause{clause}}

	// Anonymous function — return as a value
	if name == "<anon>" {
		return Value{Kind: VFn, Fn: fnVal}, nil
	}

	// Named function — register in env
	env.SetFn(name, fnVal)
	return Nil, nil
}

func evalIshMatch(node *Node, env *Env) (Value, error) {
	subject, err := Eval(node.Children[0], env)
	if err != nil {
		return Nil, err
	}

	for _, clause := range node.Clauses {
		if patternMatches(clause.Pattern, subject, env) {
			matchEnv := NewEnv(env)
			patternBind(clause.Pattern, subject, matchEnv)
			return Eval(clause.Body, matchEnv)
		}
	}
	return Nil, fmt.Errorf("no matching clause for %s", subject.Inspect())
}

// spawnProcess is the shared implementation for spawn and spawn_link.
func spawnProcess(node *Node, env *Env) (*Process, error) {
	proc := NewProcess()
	childEnv := CopyEnv(env) // snapshot — process gets isolated state
	childEnv.proc = proc
	child := node.Children[0]

	go func() {
		defer func() {
			if r := recover(); r != nil {
				proc.result = Nil
				proc.CloseWithReason(TupleVal(AtomVal("error"), StringVal(fmt.Sprintf("%v", r))))
			} else {
				proc.Close()
			}
		}()

		val, err := Eval(child, childEnv)
		if err != nil {
			proc.result = Nil
			proc.CloseWithReason(TupleVal(AtomVal("error"), StringVal(err.Error())))
			return
		}
		// If the result is a function, call it with no args
		if val.Kind == VFn && val.Fn != nil {
			result, err := callFn(val.Fn, nil, childEnv)
			proc.result = result
			if err != nil {
				proc.CloseWithReason(TupleVal(AtomVal("error"), StringVal(err.Error())))
				return
			}
		} else {
			proc.result = val
		}
	}()

	return proc, nil
}

func evalIshSpawn(node *Node, env *Env) (Value, error) {
	proc, err := spawnProcess(node, env)
	if err != nil {
		return Nil, err
	}
	return Value{Kind: VPid, Pid: proc}, nil
}

func evalIshSpawnLink(node *Node, env *Env) (Value, error) {
	proc, err := spawnProcess(node, env)
	if err != nil {
		return Nil, err
	}
	// Link to the current process
	parentProc := env.getProc()
	if parentProc != nil {
		parentProc.Link(proc)
	}
	return Value{Kind: VPid, Pid: proc}, nil
}

func evalIshMonitor(node *Node, env *Env) (Value, error) {
	target, err := Eval(node.Children[0], env)
	if err != nil {
		return Nil, err
	}
	if target.Kind != VPid || target.Pid == nil {
		return Nil, fmt.Errorf("monitor: expected pid, got %s", target.Inspect())
	}
	watcher := env.getProc()
	if watcher == nil {
		return Nil, fmt.Errorf("monitor: not in a process")
	}
	ref := target.Pid.Monitor(watcher)
	return IntVal(ref), nil
}

func evalIshAwait(node *Node, env *Env) (Value, error) {
	target, err := Eval(node.Children[0], env)
	if err != nil {
		return Nil, err
	}
	if target.Kind != VPid || target.Pid == nil {
		return Nil, fmt.Errorf("await: expected pid, got %s", target.Inspect())
	}
	result := target.Pid.Await()
	return result, nil
}

func evalIshSupervise(node *Node, env *Env) (Value, error) {
	// Children[0] = strategy, Children[1:] = worker declarations
	strategy, err := Eval(node.Children[0], env)
	if err != nil {
		return Nil, err
	}

	sup := NewSupervisor(strategy)

	for _, workerNode := range node.Children[1:] {
		if workerNode.Kind == NCmd && len(workerNode.Children) == 2 {
			// worker :name fn_expr
			nameVal, err := Eval(workerNode.Children[0], env)
			if err != nil {
				return Nil, err
			}
			fnVal, err := Eval(workerNode.Children[1], env)
			if err != nil {
				return Nil, err
			}
			if fnVal.Kind == VFn && fnVal.Fn != nil {
				sup.AddChild(nameVal.ToStr(), fnVal.Fn, env)
			} else {
				return Nil, fmt.Errorf("supervise: worker %s is not a function", nameVal.ToStr())
			}
		}
	}

	// Run supervisor in background
	go sup.Run()

	return Value{Kind: VPid, Pid: sup.proc}, nil
}

func evalIshSend(node *Node, env *Env) (Value, error) {
	target, err := Eval(node.Children[0], env)
	if err != nil {
		return Nil, err
	}
	msg, err := Eval(node.Children[1], env)
	if err != nil {
		return Nil, err
	}

	if target.Kind != VPid || target.Pid == nil {
		return Nil, fmt.Errorf("send: first argument must be a pid, got %s", target.Inspect())
	}
	target.Pid.Send(msg)
	return msg, nil
}

func evalIshReceive(node *Node, env *Env) (Value, error) {
	proc := env.getProc()
	if proc == nil {
		return Nil, fmt.Errorf("receive: not in a process")
	}

	// Build a match function from the clause patterns (selective receive).
	// Scans the save queue then the mailbox for the first message matching
	// any clause pattern, leaving non-matching messages for later receives.
	matchFn := func(msg Value) bool {
		for _, clause := range node.Clauses {
			if patternMatches(clause.Pattern, msg, env) {
				return true
			}
		}
		return false
	}

	var msg Value
	var ok bool
	if node.Timeout != nil {
		// Evaluate timeout expression to get duration in milliseconds
		timeoutVal, err := Eval(node.Timeout, env)
		if err != nil {
			return Nil, err
		}
		if timeoutVal.Kind != VInt {
			return Nil, fmt.Errorf("receive: timeout must be an integer (milliseconds), got %s", timeoutVal.Inspect())
		}
		ms := timeoutVal.Int
		msg, ok = proc.ReceiveSelectiveTimeout(matchFn, time.Duration(ms)*time.Millisecond)
		if !ok {
			// Timeout fired — evaluate the timeout body
			if node.TimeoutBody != nil {
				return Eval(node.TimeoutBody, env)
			}
			return Nil, nil
		}
	} else {
		// No timeout — blocking selective receive
		msg, ok = proc.ReceiveSelective(matchFn)
		if !ok {
			return Nil, nil
		}
	}

	// Now match and eval the body for the received message
	for _, clause := range node.Clauses {
		if patternMatches(clause.Pattern, msg, env) {
			matchEnv := NewEnv(env)
			patternBind(clause.Pattern, msg, matchEnv)
			return Eval(clause.Body, matchEnv)
		}
	}
	return Nil, fmt.Errorf("no matching receive clause for %s", msg.Inspect())
}

func evalIshTry(node *Node, env *Env) (Value, error) {
	// Try to evaluate the body
	val, err := Eval(node.Children[0], env)
	if err == nil {
		return val, nil // success
	}
	// Don't catch control flow signals
	if err == errReturn || err == errBreak || err == errContinue || err == errSetE {
		return val, err
	}
	// Convert error to a value for pattern matching
	errVal := TupleVal(AtomVal("error"), StringVal(err.Error()))

	// Try rescue clauses
	for _, clause := range node.Clauses {
		if patternMatches(clause.Pattern, errVal, env) {
			matchEnv := NewEnv(env)
			patternBind(clause.Pattern, errVal, matchEnv)
			return Eval(clause.Body, matchEnv)
		}
	}
	// No rescue clause matched — re-raise
	return Nil, err
}

func evalPosixFnDef(node *Node, env *Env) (Value, error) {
	// POSIX function: name() { body }
	name := node.Tok.Val
	fnVal := &FnValue{
		Name: name,
		Clauses: []FnClause{{
			Body: node.Children[0],
		}},
	}
	env.SetFn(name, fnVal)
	return Nil, nil
}

// callFn calls a user-defined function with string arguments.
func callFn(fn *FnValue, vals []Value, env *Env) (Value, error) {
	// Convert vals to string args for POSIX-style functions
	strArgs := make([]string, len(vals))
	for i, v := range vals {
		strArgs[i] = v.ToStr()
	}

	for _, clause := range fn.Clauses {
		if len(clause.Params) == 0 {
			// POSIX-style function (no params, uses $1, $2, etc.)
			fnEnv := NewEnv(env)
			fnEnv.args = strArgs
			val, err := Eval(clause.Body, fnEnv)
			if err == errReturn {
				return val, nil
			}
			return val, err
		}

		if len(clause.Params) != len(vals) {
			continue
		}

		// Check if pattern matches
		matches := true
		for i, param := range clause.Params {
			if !patternMatches(&param, vals[i], env) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}

		// Evaluate guard if present
		if clause.Guard != nil {
			fnEnv := NewEnv(env)
			for i, param := range clause.Params {
				patternBind(&param, vals[i], fnEnv)
			}
			guardVal, err := Eval(clause.Guard, fnEnv)
			if err != nil {
				continue
			}
			if !guardVal.Truthy() {
				continue
			}
		}

		// Bind and execute
		fnEnv := NewEnv(env)
		fnEnv.args = strArgs
		for i, param := range clause.Params {
			patternBind(&param, vals[i], fnEnv)
		}
		val, err := Eval(clause.Body, fnEnv)
		if err == errReturn {
			return val, nil
		}
		return val, err
	}

	// Build a display string for the error
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = v.Inspect()
	}
	return Nil, fmt.Errorf("no matching clause for %s(%s)", fn.Name, strings.Join(parts, ", "))
}

func boolVal(b bool) Value {
	if b {
		return True
	}
	return False
}

// expandGlobs expands glob patterns in command arguments.
func expandGlobs(args []string) []string {
	var result []string
	for _, arg := range args {
		expanded := expandGlob(arg)
		result = append(result, expanded...)
	}
	return result
}

// expandGlobsSelective expands glob patterns only for unquoted arguments.
func expandGlobsSelective(args []string, quoted []bool) []string {
	var result []string
	for i, arg := range args {
		if i < len(quoted) && quoted[i] {
			result = append(result, arg) // quoted — no glob expansion
		} else {
			result = append(result, expandGlob(arg)...)
		}
	}
	return result
}

func expandGlob(pattern string) []string {
	if !strings.ContainsAny(pattern, "*?[") {
		return []string{pattern}
	}
	matches, err := filepath_Glob(pattern)
	if err != nil || len(matches) == 0 {
		return []string{pattern}
	}
	return matches
}

// matchPattern does simple glob matching for case statements.
func matchPattern(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	matched, _ := filepath_Match(pattern, s)
	return matched
}

// splitFields splits a string on whitespace (IFS-style field splitting).
func splitFields(s string) []string {
	return strings.Fields(s)
}

// checkUnsetVars scans a string for $VAR references and returns an error
// if any referenced variable is not set. Used for set -u enforcement.
func checkUnsetVars(s string, env *Env) error {
	i := 0
	for i < len(s) {
		if s[i] != '$' {
			i++
			continue
		}
		i++ // skip $
		if i >= len(s) {
			break
		}
		// Skip special variables: $?, $$, $!, $@, $*, $#, $0-$9
		ch := s[i]
		if ch == '?' || ch == '$' || ch == '!' || ch == '@' || ch == '*' || ch == '#' {
			i++
			continue
		}
		if ch >= '0' && ch <= '9' {
			i++
			continue
		}
		// ${VAR} or ${VAR:-default} etc.
		if ch == '{' {
			i++ // skip {
			start := i
			for i < len(s) && s[i] != '}' {
				i++
			}
			expr := s[start:i]
			if i < len(s) {
				i++ // skip }
			}
			// If it has an operator (:-  :+ := :?), skip the check
			if strings.ContainsAny(expr, ":-+?=") {
				continue
			}
			if _, ok := env.Get(expr); !ok {
				return fmt.Errorf("%s: unbound variable", expr)
			}
			continue
		}
		// $( is command substitution, skip
		if ch == '(' {
			i++
			continue
		}
		// Bare $VAR
		start := i
		for i < len(s) && isVarChar(s[i]) {
			i++
		}
		varName := s[start:i]
		if varName == "" {
			continue
		}
		if _, ok := env.Get(varName); !ok {
			return fmt.Errorf("%s: unbound variable", varName)
		}
	}
	return nil
}
