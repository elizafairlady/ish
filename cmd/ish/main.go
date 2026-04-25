package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"ish/internal/eval"
	"ish/internal/lexer"
	"ish/internal/parser"
	"ish/internal/readline"
	"ish/internal/value"
)

var Version = "0.7.0"

func main() {
	env := eval.NewEnv()
	env.Ctx.ShellName = os.Args[0]

	// Set $SHELL
	if exe, err := os.Executable(); err == nil {
		env.Export("SHELL", exe)
	}

	// Detect login shell: argv[0] starts with '-' or -l/--login flag
	loginShell := strings.HasPrefix(os.Args[0], "-")
	args := os.Args[1:]
	var filteredArgs []string
	for _, a := range args {
		switch a {
		case "-l", "--login":
			loginShell = true
		case "--version":
			fmt.Printf("ish %s\n", Version)
			os.Exit(0)
		default:
			filteredArgs = append(filteredArgs, a)
		}
	}
	args = filteredArgs
	env.Ctx.IsLoginShell = loginShell

	// Non-interactive modes: -c command or script file
	if len(args) > 0 {
		if args[0] == "-c" && len(args) > 1 {
			env.Ctx.SourceName = "<stdin>"
			eval.Run(args[1], env)
			shellExit(env)
			os.Exit(env.Ctx.ExitCode())
		}
		data, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "ish: %s\n", err)
			os.Exit(1)
		}
		env.Ctx.ShellName = args[0]
		env.Ctx.SourceName = args[0]
		env.Ctx.Args = args[1:]
		eval.Run(string(data), env)
		shellExit(env)
		os.Exit(env.Ctx.ExitCode())
	}

	// Piped stdin: read and execute as a script
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ish: %s\n", err)
			os.Exit(1)
		}
		env.Ctx.SourceName = "<stdin>"
		eval.Run(string(data), env)
		shellExit(env)
		os.Exit(env.Ctx.ExitCode())
	}

	// Interactive mode — source startup files
	home := homeDir(env)
	if loginShell {
		sourceIfExists("/etc/profile", env)
		if !sourceIfExists(home+"/.ish_profile", env) {
			sourceIfExists(home+"/.profile", env)
		}
	} else {
		sourceIfExists(home+"/.ishrc", env)
	}

	// Job control
	eval.InitJobSignals()
	shellPid := os.Getpid()
	syscall.Setpgid(0, shellPid)
	ttyFd := int(os.Stdin.Fd())
	if term.IsTerminal(ttyFd) {
		eval.GiveTerm(ttyFd, shellPid)
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
		shellExit(env)
		if s, ok := sig.(syscall.Signal); ok {
			os.Exit(128 + int(s))
		}
		os.Exit(1)
	}()

	// SIGHUP: send HUP to all jobs, then exit
	sigHup := make(chan os.Signal, 1)
	signal.Notify(sigHup, syscall.SIGHUP)
	go func() {
		<-sigHup
		if env.Ctx.Jobs != nil {
			for _, j := range env.Ctx.Jobs.All() {
				if j.Process != nil {
					syscall.Kill(-j.Pid, syscall.SIGHUP)
				}
			}
		}
		shellExit(env)
		os.Exit(129)
	}()

	env.Ctx.SourceName = "<repl>"
	repl(env)
	shellExit(env)
}

func shellExit(env *eval.Env) {
	eval.RunExitTraps(env)
	if env.Ctx.IsLoginShell {
		home := homeDir(env)
		sourceIfExists(home+"/.ish_logout", env)
		if env.Ctx.Jobs != nil {
			for _, j := range env.Ctx.Jobs.All() {
				if j.Process != nil {
					syscall.Kill(-j.Pid, syscall.SIGHUP)
				}
			}
		}
	}
}

func homeDir(env *eval.Env) string {
	if v, ok := env.Get("HOME"); ok {
		return v.ToStr()
	}
	return ""
}

func sourceIfExists(path string, env *eval.Env) bool {
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	eval.Run(string(data), env)
	return true
}

func repl(env *eval.Env) {
	rl := readline.NewReadline()
	rl.Complete = makeCompleter(env)
	exitWarned := false

	for {
		prompt := getPrompt(env)
		line, ok := rl.ReadLine(prompt)
		if !ok {
			if !exitWarned && hasStoppedJobs(env) {
				fmt.Fprintln(os.Stderr, "There are stopped jobs.")
				exitWarned = true
				continue
			}
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		exitWarned = false

		line = readMultilineRL(line, rl)
		rl.AddHistory(line)

		val := eval.Run(line, env)
		if val.Kind != value.VNil {
			fmt.Fprintln(env.Ctx.Stdout, val.String())
		}
	}
}

func hasStoppedJobs(env *eval.Env) bool {
	if env.Ctx.Jobs == nil {
		return false
	}
	for _, j := range env.Ctx.Jobs.All() {
		if j.Status() == "Stopped" {
			return true
		}
	}
	return false
}

func needsMore(input string) bool {
	tokens := lexer.Lex(input)
	p := parser.New(tokens)
	_, err := p.Parse()
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

func getPrompt(env *eval.Env) string {
	if v, ok := env.Get("PS1"); ok {
		return expandPromptEscapes(v.ToStr(), env)
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

func expandPromptEscapes(s string, env *eval.Env) string {
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

func makeCompleter(env *eval.Env) readline.CompleteFn {
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
			for s := eval.Scope(env); s != nil; s = s.GetParent() {
				if sc, ok := s.(*eval.Env); ok {
					for k := range sc.Bindings {
						if strings.HasPrefix(k, varPrefix) {
							candidates = append(candidates, "$"+k)
						}
					}
				}
			}
			sort.Strings(candidates)
			return candidates
		}

		// Module-qualified completion: "List." or "List.ma"
		if dotIdx := strings.IndexByte(prefix, '.'); dotIdx > 0 {
			modName := prefix[:dotIdx]
			fnPrefix := prefix[dotIdx+1:]
			if mod, ok := env.GetModule(modName); ok {
				for _, name := range mod.Keys {
					if strings.HasPrefix(name, fnPrefix) {
						candidates = append(candidates, modName+"."+name)
					}
				}
				sort.Strings(candidates)
				return candidates
			}
		}

		if strings.ContainsAny(prefix, "/~.") {
			expanded := prefix
			if strings.HasPrefix(expanded, "~") {
				home := homeDir(env)
				if home != "" && strings.HasPrefix(expanded, "~/") {
					expanded = home + expanded[1:]
				}
			}
			matches, _ := filepath.Glob(expanded + "*")
			for _, m := range matches {
				if strings.HasPrefix(prefix, "./") && !strings.HasPrefix(m, "./") {
					m = "./" + m
				}
				display := m
				if strings.HasPrefix(prefix, "~") {
					home := homeDir(env)
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
			for s := eval.Scope(env); s != nil; s = s.GetParent() {
				if c, ok := s.(*eval.Env); ok {
					for name := range c.Fns {
						if strings.HasPrefix(name, prefix) {
							candidates = append(candidates, name)
						}
					}
					for name := range c.Modules {
						if strings.HasPrefix(name, prefix) {
							candidates = append(candidates, name+".")
						}
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
