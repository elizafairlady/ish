package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

func main() {
	env := TopEnv()
	env.shellName = os.Args[0]
	env.cmdSub = func(cmd string, e *Env) (string, error) {
		val, err := evalCmdSub(cmd, e)
		if err != nil {
			return "", err
		}
		return val.ToStr(), nil
	}

	// Set $SHELL to our own path
	if exe, err := os.Executable(); err == nil {
		env.Export("SHELL", exe)
	}

	// Load ~/.ishrc if it exists
	home := ""
	if v, ok := env.Get("HOME"); ok {
		home = v.ToStr()
	}
	if home != "" {
		rcPath := home + "/.ishrc"
		if _, err := os.Stat(rcPath); err == nil {
			data, err := os.ReadFile(rcPath)
			if err == nil {
				runSource(string(data), env)
			}
		}
	}

	if len(os.Args) > 1 {
		// Script mode
		if os.Args[1] == "-c" && len(os.Args) > 2 {
			runSource(os.Args[2], env)
			RunExitTraps(env)
			os.Exit(env.lastExit)
		}
		data, err := os.ReadFile(os.Args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "ish: %s\n", err)
			os.Exit(1)
		}
		env.shellName = os.Args[1]
		env.args = os.Args[2:]
		runSource(string(data), env)
		RunExitTraps(env)
		os.Exit(env.lastExit)
	}

	// Signal handling
	sigInt := make(chan os.Signal, 1)
	signal.Notify(sigInt, syscall.SIGINT)
	go func() {
		for range sigInt {
			fmt.Fprintln(os.Stderr)
		}
	}()

	sigTerm := make(chan os.Signal, 1)
	signal.Notify(sigTerm, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		sig := <-sigTerm
		RunExitTraps(env)
		// Exit with 128 + signal number
		if s, ok := sig.(syscall.Signal); ok {
			os.Exit(128 + int(s))
		}
		os.Exit(1)
	}()

	// Interactive REPL
	repl(env)
	RunExitTraps(env)
}

func repl(env *Env) {
	rl := NewReadline()
	rl.Complete = makeCompleter(env)

	for {
		prompt := getPrompt(env)
		line, ok := rl.ReadLine(prompt)
		if !ok {
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Check for unterminated constructs (do/end, then/fi, etc.)
		line = readMultilineRL(line, rl)

		rl.addHistory(line)

		val := runSource(line, env)
		// Print non-nil values in interactive mode (ish expression results)
		if val.Kind != VNil {
			fmt.Fprintln(env.Stdout(), val.String())
		}
	}
}

// needsMore attempts a speculative parse to determine if the input is an
// unterminated construct that needs more lines. This uses the real parser
// rather than a token-counting heuristic, so it correctly handles keywords
// as arguments (e.g., "echo then" parses fine and doesn't wait for "fi").
func needsMore(input string) bool {
	tokens := Lex(input)
	_, err := Parse(tokens)
	if err == nil {
		return false
	}
	msg := err.Error()
	// Check for unterminated construct errors from the parser.
	return strings.Contains(msg, "expected 'fi'") ||
		strings.Contains(msg, "expected 'done'") ||
		strings.Contains(msg, "expected 'end'") ||
		strings.Contains(msg, "expected 'esac'") ||
		strings.Contains(msg, "expected '}'") ||
		strings.Contains(msg, "expected ')'") ||
		strings.Contains(msg, "expected 'do'") ||
		strings.Contains(msg, "expected 'then'") ||
		strings.Contains(msg, "expected '{' in function definition") ||
		// expect() failures at EOF
		strings.Contains(msg, "unexpected end of input")
}

func readMultilineRL(line string, rl *Readline) string {
	for needsMore(line) {
		next, ok := rl.ReadLine("... ")
		if !ok {
			break
		}
		line += "\n" + next
	}
	return line
}


func runSource(src string, env *Env) Value {
	tokens, lexErr := LexCheck(src)
	if lexErr != nil {
		fmt.Fprintf(os.Stderr, "ish: %s\n", lexErr)
		env.setExit(2)
		return Nil
	}
	node, err := Parse(tokens)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ish: parse error: %s\n", err)
		env.setExit(2)
		return Nil
	}
	val, err := Eval(node, env)
	if err != nil {
		if err == errSetE {
			// set -e triggered — exit code already set, just stop execution
			return Nil
		}
		if err != errReturn && err != errBreak && err != errContinue {
			fmt.Fprintf(os.Stderr, "ish: %s\n", err)
			env.setExit(1)
		}
		return Nil
	}
	return val
}

func getPrompt(env *Env) string {
	// Check PS1
	if v, ok := env.Get("PS1"); ok {
		s := v.ToStr()
		s = expandPromptEscapes(s, env)
		s = env.Expand(s)
		return s
	}
	cwd, _ := os.Getwd()
	home := ""
	if v, ok := env.Get("HOME"); ok {
		home = v.ToStr()
	}
	if home != "" && strings.HasPrefix(cwd, home) {
		cwd = "~" + cwd[len(home):]
	}
	return cwd + " ish> "
}

// expandPromptEscapes handles bash-compatible PS1 backslash escapes.
// Supports: \u \h \H \w \W \$ \n \t \T \@ \d \e \[ \] \a \\
// Also passes through $var and #{expr} for env.Expand to handle.
func expandPromptEscapes(s string, env *Env) string {
	var buf strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '\\' || i+1 >= len(s) {
			buf.WriteByte(s[i])
			i++
			continue
		}
		i++ // skip backslash
		switch s[i] {
		case 'u':
			u := ""
			if v, ok := env.Get("USER"); ok {
				u = v.ToStr()
			}
			if u == "" {
				if v, ok := env.Get("LOGNAME"); ok {
					u = v.ToStr()
				}
			}
			buf.WriteString(u)
		case 'h':
			h, _ := os.Hostname()
			if dot := strings.IndexByte(h, '.'); dot >= 0 {
				h = h[:dot]
			}
			buf.WriteString(h)
		case 'H':
			h, _ := os.Hostname()
			buf.WriteString(h)
		case 'w':
			cwd, _ := os.Getwd()
			home := ""
			if v, ok := env.Get("HOME"); ok {
				home = v.ToStr()
			}
			if home != "" && strings.HasPrefix(cwd, home) {
				cwd = "~" + cwd[len(home):]
			}
			buf.WriteString(cwd)
		case 'W':
			cwd, _ := os.Getwd()
			base := cwd
			if last := strings.LastIndexByte(cwd, '/'); last >= 0 {
				base = cwd[last+1:]
			}
			if base == "" {
				base = "/"
			}
			buf.WriteString(base)
		case '$':
			if os.Getuid() == 0 {
				buf.WriteByte('#')
			} else {
				buf.WriteByte('$')
			}
		case 'n':
			buf.WriteByte('\n')
		case 't':
			buf.WriteString(time.Now().Format("15:04:05"))
		case 'T':
			buf.WriteString(time.Now().Format("03:04:05"))
		case '@':
			buf.WriteString(time.Now().Format("03:04 PM"))
		case 'd':
			buf.WriteString(time.Now().Format("Mon Jan 02"))
		case 'e':
			buf.WriteByte(0x1b) // ESC
		case '[':
			// Start non-printing sequence (for terminal width calc)
			// Pass through — terminal handles this
		case ']':
			// End non-printing sequence
		case 'a':
			buf.WriteByte(0x07) // BEL
		case '\\':
			buf.WriteByte('\\')
		default:
			buf.WriteByte('\\')
			buf.WriteByte(s[i])
		}
		i++
	}
	return buf.String()
}

// makeCompleter returns a CompleteFn that completes commands, paths, and variables.
func makeCompleter(env *Env) CompleteFn {
	// Cache PATH commands lazily
	var pathCommands []string
	var pathCached bool

	scanPath := func() []string {
		if pathCached {
			return pathCommands
		}
		pathCached = true
		if pathVal, ok := env.Get("PATH"); ok {
			seen := make(map[string]bool)
			for _, dir := range strings.Split(pathVal.ToStr(), ":") {
				entries, err := os.ReadDir(dir)
				if err != nil {
					continue
				}
				for _, e := range entries {
					if e.IsDir() {
						continue
					}
					name := e.Name()
					if !seen[name] {
						seen[name] = true
						pathCommands = append(pathCommands, name)
					}
				}
			}
			sort.Strings(pathCommands)
		}
		return pathCommands
	}

	return func(prefix string, isFirst bool) []string {
		var candidates []string

		// Variable completion: $PRE...
		if strings.HasPrefix(prefix, "$") {
			varPrefix := prefix[1:]
			for c := env; c != nil; c = c.parent {
				for k := range c.bindings {
					if strings.HasPrefix(k, varPrefix) {
						candidates = append(candidates, "$"+k)
					}
				}
			}
			sort.Strings(candidates)
			return candidates
		}

		// Path completion: contains / or starts with . or ~
		if strings.ContainsAny(prefix, "/~.") {
			expanded := prefix
			if strings.HasPrefix(expanded, "~") {
				expanded = env.expandTilde(expanded)
			}
			matches, _ := filepath.Glob(expanded + "*")
			for _, m := range matches {
				// Re-add ~ prefix if needed
				display := m
				if strings.HasPrefix(prefix, "~") {
					home := ""
					if v, ok := env.Get("HOME"); ok {
						home = v.ToStr()
					}
					if home != "" && strings.HasPrefix(m, home) {
						display = "~" + m[len(home):]
					}
				}
				info, err := os.Stat(m)
				if err == nil && info.IsDir() {
					display += "/"
				}
				candidates = append(candidates, display)
			}
			return candidates
		}

		// Command position: complete from builtins + functions + PATH
		if isFirst {
			for name := range builtins {
				if strings.HasPrefix(name, prefix) {
					candidates = append(candidates, name)
				}
			}
			for c := env; c != nil; c = c.parent {
				for name := range c.fns {
					if strings.HasPrefix(name, prefix) {
						candidates = append(candidates, name)
					}
				}
				for name := range c.nativeFns {
					if strings.HasPrefix(name, prefix) {
						candidates = append(candidates, name)
					}
				}
			}
			for _, cmd := range scanPath() {
				if strings.HasPrefix(cmd, prefix) {
					candidates = append(candidates, cmd)
				}
			}
			sort.Strings(candidates)
			// Deduplicate
			if len(candidates) > 1 {
				j := 0
				for i := 1; i < len(candidates); i++ {
					if candidates[i] != candidates[j] {
						j++
						candidates[j] = candidates[i]
					}
				}
				candidates = candidates[:j+1]
			}
			return candidates
		}

		// Argument position: complete from filesystem
		matches, _ := filepath.Glob(prefix + "*")
		for _, m := range matches {
			display := m
			info, err := os.Stat(m)
			if err == nil && info.IsDir() {
				display += "/"
			}
			candidates = append(candidates, display)
		}
		return candidates
	}
}
