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

	"ish/internal/builtin"
	"ish/internal/core"
	"ish/internal/eval"
	"ish/internal/lexer"
	"ish/internal/parser"
	"ish/internal/process"
	"ish/internal/readline"
	"ish/internal/stdlib"
)

func main() {
	// Wire up eval <-> builtin cycle via Init
	builtin.Init(builtin.EvalContext{
		RunSource: func(src string, env *core.Env) core.Value {
			return eval.RunSource(src, env)
		},
	})

	env := core.TopEnv()
	env.ShellName = os.Args[0]
	env.CmdSub = eval.RunCmdSub

	// Create main process
	env.Proc = process.NewProcess()

	// Register stdlib
	stdlib.Register(env)

	// Set CallFn on env so stdlib/process can call user functions
	env.CallFn = eval.CallFn

	// Set $SHELL
	if exe, err := os.Executable(); err == nil {
		env.Export("SHELL", exe)
	}

	// Load ~/.ishrc
	home := ""
	if v, ok := env.Get("HOME"); ok {
		home = v.ToStr()
	}
	if home != "" {
		rcPath := home + "/.ishrc"
		if _, err := os.Stat(rcPath); err == nil {
			data, err := os.ReadFile(rcPath)
			if err == nil {
				eval.RunSource(string(data), env)
			}
		}
	}

	if len(os.Args) > 1 {
		if os.Args[1] == "-c" && len(os.Args) > 2 {
			eval.RunSource(os.Args[2], env)
			builtin.RunExitTraps(env)
			os.Exit(env.LastExit)
		}
		data, err := os.ReadFile(os.Args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "ish: %s\n", err)
			os.Exit(1)
		}
		env.ShellName = os.Args[1]
		env.Args = os.Args[2:]
		eval.RunSource(string(data), env)
		builtin.RunExitTraps(env)
		os.Exit(env.LastExit)
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
		builtin.RunExitTraps(env)
		if s, ok := sig.(syscall.Signal); ok {
			os.Exit(128 + int(s))
		}
		os.Exit(1)
	}()

	repl(env)
	builtin.RunExitTraps(env)
}

func repl(env *core.Env) {
	rl := readline.NewReadline()
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

		line = readMultilineRL(line, rl)

		rl.AddHistory(line)

		val := eval.RunSource(line, env)
		if val.Kind != core.VNil {
			fmt.Fprintln(env.Stdout(), val.String())
		}
	}
}

func needsMore(input string) bool {
	tokens := lexer.Lex(input)
	_, err := parser.Parse(tokens)
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "expected 'fi'") ||
		strings.Contains(msg, "expected 'done'") ||
		strings.Contains(msg, "expected 'end'") ||
		strings.Contains(msg, "expected 'esac'") ||
		strings.Contains(msg, "expected '}'") ||
		strings.Contains(msg, "expected ')'") ||
		strings.Contains(msg, "expected 'do'") ||
		strings.Contains(msg, "expected 'then'") ||
		strings.Contains(msg, "expected '{' in function definition") ||
		strings.Contains(msg, "unexpected end of input")
}

func readMultilineRL(line string, rl *readline.Readline) string {
	for needsMore(line) {
		next, ok := rl.ReadLine("... ")
		if !ok {
			break
		}
		line += "\n" + next
	}
	return line
}

func getPrompt(env *core.Env) string {
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
	return cwd + " $ "
}

func expandPromptEscapes(s string, env *core.Env) string {
	var buf strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '\\' || i+1 >= len(s) {
			buf.WriteByte(s[i])
			i++
			continue
		}
		i++
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
			buf.WriteByte(0x1b)
		case '[':
		case ']':
		case 'a':
			buf.WriteByte(0x07)
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

func makeCompleter(env *core.Env) readline.CompleteFn {
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

		if strings.HasPrefix(prefix, "$") {
			varPrefix := prefix[1:]
			for c := env; c != nil; c = c.Parent {
				for k := range c.Bindings {
					if strings.HasPrefix(k, varPrefix) {
						candidates = append(candidates, "$"+k)
					}
				}
			}
			sort.Strings(candidates)
			return candidates
		}

		if strings.ContainsAny(prefix, "/~.") {
			expanded := prefix
			if strings.HasPrefix(expanded, "~") {
				expanded = env.ExpandTilde(expanded)
			}
			matches, _ := filepath.Glob(expanded + "*")
			for _, m := range matches {
				// filepath.Glob strips "./" prefix; restore it to match the typed prefix
				if strings.HasPrefix(prefix, "./") && !strings.HasPrefix(m, "./") {
					m = "./" + m
				}
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

		if isFirst {
			for name := range builtin.Builtins {
				if strings.HasPrefix(name, prefix) {
					candidates = append(candidates, name)
				}
			}
			for c := env; c != nil; c = c.Parent {
				for name := range c.Fns {
					if strings.HasPrefix(name, prefix) {
						candidates = append(candidates, name)
					}
				}
				for name := range c.NativeFns {
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
