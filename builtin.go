package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/term"
)

type BuiltinFunc func(args []string, env *Env) (int, error)

var builtins map[string]BuiltinFunc

func init() {
	builtins = map[string]BuiltinFunc{
		"echo":     builtinEcho,
		"cd":       builtinCd,
		"exit":     builtinExit,
		"export":   builtinExport,
		"unset":    builtinUnset,
		"set":      builtinSet,
		"shift":    builtinShift,
		"return":   builtinReturn,
		"break":    builtinBreak,
		"continue": builtinContinue,
		":":        builtinTrue,
		"true":     builtinTrue,
		"false":    builtinFalse,
		"test":     builtinTest,
		"[":        builtinTest,
		"read":     builtinRead,
		"exec":     builtinExec,
		"eval":     builtinEval,
		"source":   builtinSource,
		".":        builtinSource,
		"readonly": builtinReadonly,
		"trap":     builtinTrap,
		"times":    builtinTimes,
		"type":     builtinType,
		"pwd":      builtinPwd,
		"printf":   builtinPrintf,
		"wait":     builtinWait,
		"kill":     builtinKill,
		"getopts":  builtinGetopts,
		"umask":    builtinUmask,
		"ulimit":   builtinUlimit,
		"jobs":     builtinJobs,
		"fg":       builtinFg,
		"bg":       builtinBg,
		"local":    builtinLocal,
		"alias":    builtinAlias,
		"unalias":  builtinUnalias,
		"command":  builtinCommand,
	}
}

func builtinEcho(args []string, env *Env) (int, error) {
	newline := true
	escapes := false
	start := 0
	for start < len(args) {
		arg := args[start]
		if len(arg) < 2 || arg[0] != '-' {
			break
		}
		valid := true
		for _, ch := range arg[1:] {
			if ch != 'n' && ch != 'e' && ch != 'E' {
				valid = false
				break
			}
		}
		if !valid {
			break
		}
		for _, ch := range arg[1:] {
			switch ch {
			case 'n':
				newline = false
			case 'e':
				escapes = true
			case 'E':
				escapes = false
			}
		}
		start++
	}
	out := strings.Join(args[start:], " ")
	if escapes {
		out = expandEchoEscapes(out)
	}
	w := env.Stdout()
	if newline {
		fmt.Fprintln(w, out)
	} else {
		fmt.Fprint(w, out)
	}
	return 0, nil
}

func expandEchoEscapes(s string) string {
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
		case 'n':
			buf.WriteByte('\n')
		case 't':
			buf.WriteByte('\t')
		case 'r':
			buf.WriteByte('\r')
		case 'a':
			buf.WriteByte('\a')
		case 'b':
			buf.WriteByte('\b')
		case 'f':
			buf.WriteByte('\f')
		case 'v':
			buf.WriteByte('\v')
		case '\\':
			buf.WriteByte('\\')
		case '0':
			// \0NNN — octal
			val := byte(0)
			for j := 0; j < 3 && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '7'; j++ {
				i++
				val = val*8 + (s[i] - '0')
			}
			buf.WriteByte(val)
		case 'c':
			return buf.String() // \c stops output
		default:
			buf.WriteByte('\\')
			buf.WriteByte(s[i])
		}
		i++
	}
	return buf.String()
}

func builtinCd(args []string, env *Env) (int, error) {
	dir := ""
	if len(args) == 0 {
		if v, ok := env.Get("HOME"); ok {
			dir = v.ToStr()
		}
		if dir == "" {
			return 1, fmt.Errorf("cd: HOME not set")
		}
	} else {
		dir = args[0]
	}
	if dir == "-" {
		if v, ok := env.Get("OLDPWD"); ok {
			dir = v.ToStr()
		}
	}

	// CDPATH search for relative directory arguments
	if !strings.HasPrefix(dir, "/") && !strings.HasPrefix(dir, "./") && !strings.HasPrefix(dir, "../") && dir != "-" {
		if cdpath, ok := env.Get("CDPATH"); ok {
			for _, prefix := range strings.Split(cdpath.ToStr(), ":") {
				if prefix == "" {
					prefix = "."
				}
				candidate := prefix + "/" + dir
				if info, err := os.Stat(candidate); err == nil && info.IsDir() {
					dir = candidate
					fmt.Fprintln(env.Stdout(), dir)
					break
				}
			}
		}
	}

	old, _ := os.Getwd()
	err := os.Chdir(dir)
	if err != nil {
		return 1, fmt.Errorf("cd: %s: %s", dir, err)
	}
	env.Export("OLDPWD", old)
	cwd, _ := os.Getwd()
	env.Export("PWD", cwd)
	return 0, nil
}

func builtinExit(args []string, env *Env) (int, error) {
	code := 0
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "exit: %s: numeric argument required\n", args[0])
			os.Exit(2)
		}
		code = n
	}
	os.Exit(code)
	return code, nil
}

func builtinExport(args []string, env *Env) (int, error) {
	for _, arg := range args {
		if i := strings.IndexByte(arg, '='); i >= 0 {
			env.Export(arg[:i], arg[i+1:])
		} else {
			// Export existing variable
			env.ExportName(arg)
		}
	}
	return 0, nil
}

func builtinUnset(args []string, env *Env) (int, error) {
	unsetFn := false
	var names []string
	for _, arg := range args {
		switch arg {
		case "-f":
			unsetFn = true
		case "-v":
			unsetFn = false
		default:
			names = append(names, arg)
		}
	}
	for _, name := range names {
		if unsetFn {
			env.DeleteFn(name)
		} else {
			env.DeleteVar(name)
		}
	}
	return 0, nil
}

func builtinSet(args []string, env *Env) (int, error) {
	if len(args) == 0 {
		// Print all variables
		for k, v := range env.bindings {
			fmt.Fprintf(env.Stdout(), "%s=%s\n", k, v.ToStr())
		}
		return 0, nil
	}
	// set -- args sets positional parameters
	if args[0] == "--" {
		env.args = args[1:]
		return 0, nil
	}
	// Handle flags: -e, -u, -x, +e, +u, +x, -o pipefail, +o pipefail
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "-o" && i+1 < len(args) {
			opt := args[i+1]
			switch opt {
			case "pipefail":
				env.SetFlag('P', true) // P for pipefail
			default:
				return 1, fmt.Errorf("set: invalid option: -o %s", opt)
			}
			i += 2
			continue
		}
		if arg == "+o" && i+1 < len(args) {
			opt := args[i+1]
			switch opt {
			case "pipefail":
				env.SetFlag('P', false)
			default:
				return 1, fmt.Errorf("set: invalid option: +o %s", opt)
			}
			i += 2
			continue
		}
		if len(arg) >= 2 && arg[0] == '-' {
			for _, ch := range arg[1:] {
				switch ch {
				case 'e', 'u', 'x':
					env.SetFlag(byte(ch), true)
				default:
					return 1, fmt.Errorf("set: invalid option: -%c", ch)
				}
			}
			i++
			continue
		}
		if len(arg) >= 2 && arg[0] == '+' {
			for _, ch := range arg[1:] {
				switch ch {
				case 'e', 'u', 'x':
					env.SetFlag(byte(ch), false)
				default:
					return 1, fmt.Errorf("set: invalid option: +%c", ch)
				}
			}
			i++
			continue
		}
		// Unrecognized, stop processing
		break
	}
	return 0, nil
}

func builtinShift(args []string, env *Env) (int, error) {
	n := 1
	if len(args) > 0 {
		var err error
		n, err = strconv.Atoi(args[0])
		if err != nil {
			return 1, fmt.Errorf("shift: %s: numeric argument required", args[0])
		}
	}
	if n < 0 {
		return 1, fmt.Errorf("shift: %d: shift count out of range", n)
	}
	posArgs := env.posArgs()
	if n > len(posArgs) {
		return 1, fmt.Errorf("shift: %d: shift count out of range", n)
	}
	env.args = posArgs[n:]
	return 0, nil
}

func builtinLocal(args []string, env *Env) (int, error) {
	for _, arg := range args {
		if idx := strings.IndexByte(arg, '='); idx >= 0 {
			name := arg[:idx]
			val := arg[idx+1:]
			env.SetLocal(name, StringVal(val))
		} else {
			// Declare local without assignment — set to empty string
			env.SetLocal(arg, StringVal(""))
		}
	}
	return 0, nil
}

var errReturn = fmt.Errorf("return")
var errBreak = fmt.Errorf("break")
var errContinue = fmt.Errorf("continue")
var errSetE = fmt.Errorf("set -e exit")

func builtinReturn(args []string, env *Env) (int, error) {
	code := 0
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return 1, fmt.Errorf("return: %s: numeric argument required", args[0])
		}
		code = n
	}
	env.setExit(code)
	return code, errReturn
}

func builtinBreak(args []string, env *Env) (int, error) {
	return 0, errBreak
}

func builtinContinue(args []string, env *Env) (int, error) {
	return 0, errContinue
}

func builtinTrue(args []string, env *Env) (int, error) {
	return 0, nil
}

func builtinFalse(args []string, env *Env) (int, error) {
	return 1, nil
}

func builtinTest(args []string, env *Env) (int, error) {
	// Strip trailing ] if invoked as [
	if len(args) > 0 && args[len(args)-1] == "]" {
		args = args[:len(args)-1]
	}
	if len(args) == 0 {
		return 1, nil
	}
	pos := 0
	result := evalTestOr(args, &pos)
	if pos != len(args) {
		// Trailing junk — syntax error, return false
		return 2, fmt.Errorf("test: unexpected argument: %s", args[pos])
	}
	if result {
		return 0, nil
	}
	return 1, nil
}

// Recursive descent evaluator for test expressions.
// Grammar:
//   or_expr   := and_expr ( -o and_expr )*
//   and_expr  := not_expr ( -a not_expr )*
//   not_expr  := ! not_expr | primary
//   primary   := ( or_expr ) | unary_op operand | operand binary_op operand | operand

func evalTestOr(args []string, pos *int) bool {
	result := evalTestAnd(args, pos)
	for *pos < len(args) && args[*pos] == "-o" {
		*pos++ // consume -o
		right := evalTestAnd(args, pos)
		result = result || right
	}
	return result
}

func evalTestAnd(args []string, pos *int) bool {
	result := evalTestNot(args, pos)
	for *pos < len(args) && args[*pos] == "-a" {
		*pos++ // consume -a
		right := evalTestNot(args, pos)
		result = result && right
	}
	return result
}

func evalTestNot(args []string, pos *int) bool {
	if *pos < len(args) && args[*pos] == "!" {
		*pos++ // consume !
		return !evalTestNot(args, pos)
	}
	return evalTestPrimary(args, pos)
}

func evalTestPrimary(args []string, pos *int) bool {
	if *pos >= len(args) {
		return false
	}

	// Parenthesized expression: ( expr )
	if args[*pos] == "(" {
		*pos++ // consume (
		result := evalTestOr(args, pos)
		if *pos < len(args) && args[*pos] == ")" {
			*pos++ // consume )
		}
		return result
	}

	// Check for binary operator: if we have at least 3 tokens from current
	// position and the second token is a known binary operator, parse as binary.
	// This resolves POSIX ambiguity where a unary-op-looking string is actually
	// a left operand of a binary expression (e.g. test -f = -f).
	if *pos+2 < len(args) && isBinaryOp(args[*pos+1]) {
		return evalBinaryOp(args, pos)
	}

	// Unary operators (require one operand after them)
	if *pos+1 < len(args) {
		op := args[*pos]
		switch op {
		case "-n":
			*pos++
			operand := args[*pos]
			*pos++
			return operand != ""
		case "-z":
			*pos++
			operand := args[*pos]
			*pos++
			return operand == ""
		case "-f":
			*pos++
			operand := args[*pos]
			*pos++
			info, err := os.Stat(operand)
			return err == nil && info.Mode().IsRegular()
		case "-d":
			*pos++
			operand := args[*pos]
			*pos++
			info, err := os.Stat(operand)
			return err == nil && info.IsDir()
		case "-e":
			*pos++
			operand := args[*pos]
			*pos++
			_, err := os.Stat(operand)
			return err == nil
		case "-s":
			*pos++
			operand := args[*pos]
			*pos++
			info, err := os.Stat(operand)
			return err == nil && info.Size() > 0
		case "-r":
			*pos++
			operand := args[*pos]
			*pos++
			return syscall.Access(operand, 0x4) == nil // R_OK
		case "-w":
			*pos++
			operand := args[*pos]
			*pos++
			return syscall.Access(operand, 0x2) == nil // W_OK
		case "-x":
			*pos++
			operand := args[*pos]
			*pos++
			return syscall.Access(operand, 0x1) == nil // X_OK
		case "-L", "-h":
			*pos++
			operand := args[*pos]
			*pos++
			info, err := os.Lstat(operand)
			return err == nil && info.Mode()&os.ModeSymlink != 0
		case "-p":
			*pos++
			operand := args[*pos]
			*pos++
			info, err := os.Stat(operand)
			return err == nil && info.Mode()&os.ModeNamedPipe != 0
		case "-S":
			*pos++
			operand := args[*pos]
			*pos++
			info, err := os.Stat(operand)
			return err == nil && info.Mode()&os.ModeSocket != 0
		case "-t":
			*pos++
			operand := args[*pos]
			*pos++
			fd, err := strconv.Atoi(operand)
			if err != nil {
				return false
			}
			return termIsTerminal(fd)
		}
	}

	// Consume the token as a string operand.
	operand := args[*pos]
	*pos++

	// Look ahead for binary operators.
	if *pos+1 < len(args) && isBinaryOp(args[*pos]) {
		return evalBinaryOpWith(operand, args, pos)
	}

	// Bare string: true if non-empty
	return operand != ""
}

func isBinaryOp(s string) bool {
	switch s {
	case "=", "==", "!=", "-eq", "-ne", "-lt", "-le", "-gt", "-ge":
		return true
	}
	return false
}

func evalBinaryOp(args []string, pos *int) bool {
	operand := args[*pos]
	*pos++
	return evalBinaryOpWith(operand, args, pos)
}

func evalBinaryOpWith(operand string, args []string, pos *int) bool {
	op := args[*pos]
	*pos++
	right := args[*pos]
	*pos++
	switch op {
	case "=", "==":
		return operand == right
	case "!=":
		return operand != right
	case "-eq":
		return testNumCmpBool(operand, right, func(a, b int) bool { return a == b })
	case "-ne":
		return testNumCmpBool(operand, right, func(a, b int) bool { return a != b })
	case "-lt":
		return testNumCmpBool(operand, right, func(a, b int) bool { return a < b })
	case "-le":
		return testNumCmpBool(operand, right, func(a, b int) bool { return a <= b })
	case "-gt":
		return testNumCmpBool(operand, right, func(a, b int) bool { return a > b })
	case "-ge":
		return testNumCmpBool(operand, right, func(a, b int) bool { return a >= b })
	}
	return false
}

func testNumCmpBool(a, b string, cmp func(int, int) bool) bool {
	ai, _ := strconv.Atoi(a)
	bi, _ := strconv.Atoi(b)
	return cmp(ai, bi)
}

func termIsTerminal(fd int) bool {
	return term.IsTerminal(fd)
}

// ---------------------------------------------------------------------------
// read - with -p, -r, -s, -t, -n, and multi-variable splitting
// ---------------------------------------------------------------------------

func builtinRead(args []string, env *Env) (int, error) {
	var prompt string
	raw := false
	silent := false
	timeout := 0
	nchars := 0
	var varNames []string

	// Parse options
	i := 0
	for i < len(args) {
		switch args[i] {
		case "-p":
			if i+1 < len(args) {
				prompt = args[i+1]
				i += 2
			} else {
				return 1, fmt.Errorf("read: -p: option requires an argument")
			}
		case "-r":
			raw = true
			i++
		case "-s":
			silent = true
			i++
		case "-t":
			if i+1 < len(args) {
				n, err := strconv.Atoi(args[i+1])
				if err != nil {
					return 1, fmt.Errorf("read: %s: invalid timeout", args[i+1])
				}
				timeout = n
				i += 2
			} else {
				return 1, fmt.Errorf("read: -t: option requires an argument")
			}
		case "-n":
			if i+1 < len(args) {
				n, err := strconv.Atoi(args[i+1])
				if err != nil {
					return 1, fmt.Errorf("read: %s: invalid count", args[i+1])
				}
				nchars = n
				i += 2
			} else {
				return 1, fmt.Errorf("read: -n: option requires an argument")
			}
		default:
			varNames = append(varNames, args[i])
			i++
		}
	}

	// Print prompt to stderr
	if prompt != "" {
		fmt.Fprint(os.Stderr, prompt)
	}

	// Silent mode: suppress echo by putting terminal in raw mode
	if silent {
		fd := int(os.Stdin.Fd())
		if term.IsTerminal(fd) {
			oldState, err := term.MakeRaw(fd)
			if err == nil {
				defer func() {
					term.Restore(fd, oldState)
					fmt.Fprint(os.Stderr, "\n") // print newline after silent input
				}()
			}
		}
	}

	var line string
	var readErr error

	if nchars > 0 {
		// Read exactly N characters
		buf := make([]byte, nchars)
		if timeout > 0 {
			fd := int(os.Stdin.Fd())
			var readSet syscall.FdSet
			readSet.Bits[fd/64] |= 1 << (uint(fd) % 64)
			tv := syscall.Timeval{Sec: int64(timeout)}
			sn, serr := syscall.Select(fd+1, &readSet, nil, nil, &tv)
			if serr != nil || sn == 0 {
				return 1, nil // timeout or error
			}
			n, err := os.Stdin.Read(buf)
			if err != nil && n == 0 {
				return 1, nil
			}
			line = string(buf[:n])
		} else {
			n, err := os.Stdin.Read(buf)
			if err != nil && n == 0 {
				return 1, nil
			}
			line = string(buf[:n])
		}
	} else {
		// Read a line
		if timeout > 0 {
			fd := int(os.Stdin.Fd())
			// Use select(2) to poll stdin with timeout
			var readSet syscall.FdSet
			readSet.Bits[fd/64] |= 1 << (uint(fd) % 64)
			tv := syscall.Timeval{Sec: int64(timeout)}
			sn, serr := syscall.Select(fd+1, &readSet, nil, nil, &tv)
			if serr != nil || sn == 0 {
				return 1, nil // timeout or error
			}
			// stdin is ready — read synchronously (won't block)
			reader := bufio.NewReader(os.Stdin)
			line, readErr = reader.ReadString('\n')
			if readErr != nil && len(line) == 0 {
				return 1, nil
			}
		} else {
			reader := bufio.NewReader(os.Stdin)
			line, readErr = reader.ReadString('\n')
			if readErr != nil && len(line) == 0 {
				return 1, nil
			}
		}
		line = strings.TrimRight(line, "\n\r")
	}

	// Process backslash escapes unless -r
	if !raw {
		line = strings.ReplaceAll(line, "\\\n", "")
	}

	if len(varNames) == 0 {
		// No var names: set REPLY
		env.Set("REPLY", StringVal(line))
	} else if len(varNames) == 1 {
		env.Set(varNames[0], StringVal(line))
	} else {
		// Multiple variable names: split line on IFS, last var gets remainder
		fields := env.SplitFieldsIFS(line)
		for idx, name := range varNames {
			if idx < len(varNames)-1 {
				if idx < len(fields) {
					env.Set(name, StringVal(fields[idx]))
				} else {
					env.Set(name, StringVal(""))
				}
			} else {
				// Last var gets remainder
				if idx < len(fields) {
					remainder := strings.Join(fields[idx:], " ")
					env.Set(name, StringVal(remainder))
				} else {
					env.Set(name, StringVal(""))
				}
			}
		}
	}
	return 0, nil
}

func builtinExec(args []string, env *Env) (int, error) {
	if len(args) == 0 {
		return 0, nil
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		return 127, fmt.Errorf("exec: %s: %s", args[0], err)
	}
	err = syscall.Exec(path, args, env.BuildEnv())
	// If syscall.Exec returns, it failed
	return 127, fmt.Errorf("exec: %s", err)
}

func builtinEval(args []string, env *Env) (int, error) {
	if len(args) == 0 {
		return 0, nil
	}
	line := strings.Join(args, " ")
	tokens := Lex(line)
	node, err := Parse(tokens)
	if err != nil {
		return 1, err
	}
	val, err := Eval(node, env)
	if err != nil {
		return 1, err
	}
	if val.Kind != VNil {
		fmt.Fprintln(env.Stdout(), val.String())
	}
	return env.lastExit, nil
}

// ---------------------------------------------------------------------------
// source / . - with positional args
// ---------------------------------------------------------------------------

func builtinSource(args []string, env *Env) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("source: filename argument required")
	}
	filename := args[0]
	// If filename doesn't contain /, search PATH
	if !strings.Contains(filename, "/") {
		if pathVal, ok := env.Get("PATH"); ok {
			for _, dir := range strings.Split(pathVal.ToStr(), ":") {
				candidate := dir + "/" + filename
				if _, err := os.Stat(candidate); err == nil {
					filename = candidate
					break
				}
			}
		}
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return 1, err
	}

	// Save and set positional args
	savedArgs := env.args
	if len(args) > 1 {
		env.args = args[1:]
	}

	tokens := Lex(string(data))
	node, err := Parse(tokens)
	if err != nil {
		env.args = savedArgs
		return 1, err
	}
	_, err = Eval(node, env)

	// Restore positional args
	env.args = savedArgs

	if err != nil {
		return 1, err
	}
	return env.lastExit, nil
}

// ---------------------------------------------------------------------------
// readonly
// ---------------------------------------------------------------------------

func builtinReadonly(args []string, env *Env) (int, error) {
	// No args or -p: print all readonly vars
	if len(args) == 0 || (len(args) == 1 && args[0] == "-p") {
		w := env.Stdout()
		for c := env; c != nil; c = c.parent {
			if c.readonlySet != nil {
				for name := range c.readonlySet {
					if v, ok := env.Get(name); ok {
						fmt.Fprintf(w, "declare -r %s=%s\n", name, v.ToStr())
					} else {
						fmt.Fprintf(w, "declare -r %s\n", name)
					}
				}
			}
		}
		return 0, nil
	}

	for _, arg := range args {
		if arg == "-p" {
			continue
		}
		if idx := strings.IndexByte(arg, '='); idx >= 0 {
			name := arg[:idx]
			val := arg[idx+1:]
			if err := env.Set(name, StringVal(val)); err != nil {
				return 1, fmt.Errorf("readonly: %s", err)
			}
			env.SetReadonly(name)
		} else {
			env.SetReadonly(arg)
		}
	}
	return 0, nil
}

// ---------------------------------------------------------------------------
// trap
// ---------------------------------------------------------------------------

// Signal-to-syscall mapping for trap registration.
var trapSignalMap = map[string]os.Signal{
	"INT":  syscall.SIGINT,
	"TERM": syscall.SIGTERM,
	"HUP":  syscall.SIGHUP,
	"QUIT": syscall.SIGQUIT,
	"USR1": syscall.SIGUSR1,
	"USR2": syscall.SIGUSR2,
}

var (
	trapMu      sync.Mutex
	trapChans   = make(map[string]chan os.Signal) // signal name -> notification channel
	trapEnvRef  *Env                              // env to evaluate trap commands in
)

// registerSignalTrap wires an OS signal to invoke the trap command.
func registerSignalTrap(sigName string, env *Env) {
	trapMu.Lock()
	defer trapMu.Unlock()

	osSig, ok := trapSignalMap[sigName]
	if !ok {
		return // pseudo-signal (EXIT, ERR) — handled elsewhere
	}

	trapEnvRef = env

	// Stop previous registration for this signal
	if ch, exists := trapChans[sigName]; exists {
		signal.Stop(ch)
		close(ch)
	}

	ch := make(chan os.Signal, 1)
	trapChans[sigName] = ch
	signal.Notify(ch, osSig)

	go func() {
		for range ch {
			trapMu.Lock()
			e := trapEnvRef
			trapMu.Unlock()
			if e != nil {
				if cmd, ok := e.GetTrap(sigName); ok {
					if cmd != "" {
						runSource(cmd, e)
					}
					// Empty cmd means ignore the signal
				}
			}
		}
	}()
}

// unregisterSignalTrap removes the OS signal handler for a trap.
func unregisterSignalTrap(sigName string) {
	trapMu.Lock()
	defer trapMu.Unlock()

	if ch, exists := trapChans[sigName]; exists {
		signal.Stop(ch)
		close(ch)
		delete(trapChans, sigName)
	}

	// Reset to default behavior
	if osSig, ok := trapSignalMap[sigName]; ok {
		signal.Reset(osSig)
	}
}

// RunExitTraps fires EXIT traps. Called at shell exit.
func RunExitTraps(env *Env) {
	if cmd, ok := env.GetTrap("EXIT"); ok && cmd != "" {
		runSource(cmd, env)
	}
}

// CheckErrTrap fires the ERR trap if the last command failed.
func CheckErrTrap(env *Env) {
	if env.exitCode() != 0 {
		if cmd, ok := env.GetTrap("ERR"); ok && cmd != "" {
			runSource(cmd, env)
		}
	}
}

func builtinTrap(args []string, env *Env) (int, error) {
	// No args: list all traps
	if len(args) == 0 {
		w := env.Stdout()
		for c := env; c != nil; c = c.parent {
			if c.traps != nil {
				for sig, cmd := range c.traps {
					fmt.Fprintf(w, "trap -- %q %s\n", cmd, sig)
				}
			}
		}
		return 0, nil
	}

	validSignals := map[string]bool{
		"INT": true, "TERM": true, "HUP": true,
		"EXIT": true, "ERR": true, "QUIT": true,
		"USR1": true, "USR2": true,
	}

	// trap -l: list signals
	if args[0] == "-l" {
		w := env.Stdout()
		fmt.Fprintln(w, "EXIT INT TERM HUP QUIT USR1 USR2 ERR")
		return 0, nil
	}

	// trap - SIG...: reset to default
	if args[0] == "-" {
		for _, sig := range args[1:] {
			sig = strings.ToUpper(sig)
			sig = strings.TrimPrefix(sig, "SIG")
			if !validSignals[sig] {
				fmt.Fprintf(os.Stderr, "trap: %s: invalid signal specification\n", sig)
				continue
			}
			env.DeleteTrap(sig)
			unregisterSignalTrap(sig)
		}
		return 0, nil
	}

	// trap 'cmd' SIG...: set handler for signals
	if len(args) < 2 {
		return 1, fmt.Errorf("trap: usage: trap command signal [signal ...]")
	}
	cmd := args[0]
	for _, sig := range args[1:] {
		sig = strings.ToUpper(sig)
		sig = strings.TrimPrefix(sig, "SIG")
		if !validSignals[sig] {
			fmt.Fprintf(os.Stderr, "trap: %s: invalid signal specification\n", sig)
			continue
		}
		env.SetTrap(sig, cmd)
		registerSignalTrap(sig, env)
	}
	return 0, nil
}

// ---------------------------------------------------------------------------
// times
// ---------------------------------------------------------------------------

func builtinTimes(args []string, env *Env) (int, error) {
	w := env.Stdout()

	var usage syscall.Rusage
	err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
	if err != nil {
		fmt.Fprintln(w, "0m0.000s 0m0.000s")
		fmt.Fprintln(w, "0m0.000s 0m0.000s")
		return 0, nil
	}

	utime := usage.Utime
	stime := usage.Stime
	fmt.Fprintf(w, "%dm%d.%03ds %dm%d.%03ds\n",
		utime.Sec/60, utime.Sec%60, utime.Usec/1000,
		stime.Sec/60, stime.Sec%60, stime.Usec/1000)

	var childUsage syscall.Rusage
	err = syscall.Getrusage(syscall.RUSAGE_CHILDREN, &childUsage)
	if err != nil {
		fmt.Fprintln(w, "0m0.000s 0m0.000s")
		return 0, nil
	}

	cutime := childUsage.Utime
	cstime := childUsage.Stime
	fmt.Fprintf(w, "%dm%d.%03ds %dm%d.%03ds\n",
		cutime.Sec/60, cutime.Sec%60, cutime.Usec/1000,
		cstime.Sec/60, cstime.Sec%60, cstime.Usec/1000)

	return 0, nil
}

func builtinType(args []string, env *Env) (int, error) {
	code := 0
	for _, name := range args {
		if _, ok := builtins[name]; ok {
			fmt.Fprintf(env.Stdout(), "%s is a shell builtin\n", name)
		} else if _, ok := env.GetFn(name); ok {
			fmt.Fprintf(env.Stdout(), "%s is a function\n", name)
		} else if _, ok := env.GetNativeFn(name); ok {
			fmt.Fprintf(env.Stdout(), "%s is a function\n", name)
		} else if path, err := exec.LookPath(name); err == nil {
			fmt.Fprintf(env.Stdout(), "%s is %s\n", name, path)
		} else {
			fmt.Fprintf(env.Stdout(), "ish: type: %s: not found\n", name)
			code = 1
		}
	}
	return code, nil
}

func builtinPwd(args []string, env *Env) (int, error) {
	dir, _ := os.Getwd()
	fmt.Fprintln(env.Stdout(), dir)
	return 0, nil
}

// ---------------------------------------------------------------------------
// printf
// ---------------------------------------------------------------------------

func builtinPrintf(args []string, env *Env) (int, error) {
	if len(args) == 0 {
		return 0, nil
	}

	format := args[0]
	fmtArgs := args[1:]
	w := env.Stdout()

	argIdx := 0
	getArg := func() string {
		if argIdx < len(fmtArgs) {
			s := fmtArgs[argIdx]
			argIdx++
			return s
		}
		return ""
	}

	for {
		startIdx := argIdx
		printfExpand(w, format, getArg)
		// If more args than format specifiers, repeat the format
		if argIdx >= len(fmtArgs) || argIdx == startIdx {
			break
		}
	}

	return 0, nil
}

func printfExpand(w interface{ Write([]byte) (int, error) }, format string, getArg func() string) {
	i := 0
	for i < len(format) {
		if format[i] == '\\' && i+1 < len(format) {
			i++
			switch format[i] {
			case 'n':
				fmt.Fprint(w, "\n")
			case 't':
				fmt.Fprint(w, "\t")
			case 'r':
				fmt.Fprint(w, "\r")
			case '\\':
				fmt.Fprint(w, "\\")
			case 'a':
				fmt.Fprint(w, "\a")
			case 'b':
				fmt.Fprint(w, "\b")
			case 'f':
				fmt.Fprint(w, "\f")
			case 'v':
				fmt.Fprint(w, "\v")
			case '0':
				// \0NNN — octal
				val := byte(0)
				for j := 0; j < 3 && i+1 < len(format) && format[i+1] >= '0' && format[i+1] <= '7'; j++ {
					i++
					val = val*8 + (format[i] - '0')
				}
				w.Write([]byte{val})
			default:
				fmt.Fprintf(w, "\\%c", format[i])
			}
			i++
			continue
		}
		if format[i] == '%' {
			i++
			if i >= len(format) {
				break
			}
			// Collect full format specifier: %[flags][width][.precision]verb
			spec := "%"
			// Flags: -, +, 0, ' ', #
			for i < len(format) && (format[i] == '-' || format[i] == '+' || format[i] == '0' || format[i] == ' ' || format[i] == '#') {
				spec += string(format[i])
				i++
			}
			// Width
			for i < len(format) && format[i] >= '0' && format[i] <= '9' {
				spec += string(format[i])
				i++
			}
			// Precision
			if i < len(format) && format[i] == '.' {
				spec += "."
				i++
				for i < len(format) && format[i] >= '0' && format[i] <= '9' {
					spec += string(format[i])
					i++
				}
			}
			if i >= len(format) {
				fmt.Fprint(w, spec)
				break
			}
			verb := format[i]
			i++
			switch verb {
			case 's':
				fmt.Fprintf(w, spec+"s", getArg())
			case 'd', 'i':
				arg := getArg()
				n, _ := strconv.ParseInt(arg, 0, 64)
				fmt.Fprintf(w, spec+"d", n)
			case 'o':
				arg := getArg()
				n, _ := strconv.ParseInt(arg, 0, 64)
				fmt.Fprintf(w, spec+"o", n)
			case 'x':
				arg := getArg()
				n, _ := strconv.ParseInt(arg, 0, 64)
				fmt.Fprintf(w, spec+"x", n)
			case 'X':
				arg := getArg()
				n, _ := strconv.ParseInt(arg, 0, 64)
				fmt.Fprintf(w, spec+"X", n)
			case 'c':
				arg := getArg()
				if len(arg) > 0 {
					w.Write([]byte{arg[0]})
				}
			case 'f':
				arg := getArg()
				f, _ := strconv.ParseFloat(arg, 64)
				fmt.Fprintf(w, spec+"f", f)
			case 'e':
				arg := getArg()
				f, _ := strconv.ParseFloat(arg, 64)
				fmt.Fprintf(w, spec+"e", f)
			case 'E':
				arg := getArg()
				f, _ := strconv.ParseFloat(arg, 64)
				fmt.Fprintf(w, spec+"E", f)
			case 'g':
				arg := getArg()
				f, _ := strconv.ParseFloat(arg, 64)
				fmt.Fprintf(w, spec+"g", f)
			case 'G':
				arg := getArg()
				f, _ := strconv.ParseFloat(arg, 64)
				fmt.Fprintf(w, spec+"G", f)
			case 'u':
				arg := getArg()
				n, _ := strconv.ParseUint(arg, 0, 64)
				fmt.Fprintf(w, spec+"d", n)
			case '%':
				fmt.Fprint(w, "%")
			default:
				fmt.Fprint(w, spec+string(verb))
			}
			continue
		}
		w.Write([]byte{format[i]})
		i++
	}
}

// ---------------------------------------------------------------------------
// wait
// ---------------------------------------------------------------------------

func builtinWait(args []string, env *Env) (int, error) {
	if len(args) == 0 {
		// Wait for all background jobs
		jobs := ListJobs()
		for _, j := range jobs {
			if j.done != nil {
				<-j.done
			}
			RemoveJob(j.ID)
		}
		return 0, nil
	}

	// Wait for specific pid or job spec
	spec := args[0]
	if strings.HasPrefix(spec, "%") {
		j := resolveJob(spec)
		if j == nil {
			return 1, fmt.Errorf("wait: %s: no such job", spec)
		}
		if j.done != nil {
			<-j.done
		}
		code := j.exitCode
		RemoveJob(j.ID)
		return code, nil
	}

	pid, err := strconv.Atoi(spec)
	if err != nil {
		return 1, fmt.Errorf("wait: %s: not a pid", spec)
	}

	// Try ish process registry first
	proc := FindProcess(int64(pid))
	if proc != nil {
		proc.Await()
		return 0, nil
	}

	// Try OS job table
	j := FindJobByPid(pid)
	if j != nil {
		if j.done != nil {
			<-j.done
		}
		code := j.exitCode
		RemoveJob(j.ID)
		return code, nil
	}

	return 127, fmt.Errorf("wait: pid %d is not a child of this shell", pid)
}

// ---------------------------------------------------------------------------
// kill
// ---------------------------------------------------------------------------

var signalMap = map[string]syscall.Signal{
	"HUP":  syscall.SIGHUP,
	"INT":  syscall.SIGINT,
	"QUIT": syscall.SIGQUIT,
	"KILL": syscall.SIGKILL,
	"TERM": syscall.SIGTERM,
	"USR1": syscall.SIGUSR1,
	"USR2": syscall.SIGUSR2,
	"STOP": syscall.SIGSTOP,
	"CONT": syscall.SIGCONT,
}

var signalNumbers = map[int]syscall.Signal{
	1:  syscall.SIGHUP,
	2:  syscall.SIGINT,
	3:  syscall.SIGQUIT,
	9:  syscall.SIGKILL,
	10: syscall.SIGUSR1,
	12: syscall.SIGUSR2,
	15: syscall.SIGTERM,
	18: syscall.SIGCONT,
	19: syscall.SIGSTOP,
}

func builtinKill(args []string, env *Env) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("kill: usage: kill [-signal] pid")
	}

	// kill -l: list signals
	if args[0] == "-l" {
		w := env.Stdout()
		fmt.Fprintln(w, " 1) HUP\t 2) INT\t 3) QUIT\t 9) KILL")
		fmt.Fprintln(w, "10) USR1\t12) USR2\t15) TERM\t18) CONT")
		fmt.Fprintln(w, "19) STOP")
		return 0, nil
	}

	sig := syscall.SIGTERM
	pidArgs := args

	if len(args) >= 2 && strings.HasPrefix(args[0], "-") {
		sigStr := args[0][1:]
		upper := strings.ToUpper(sigStr)
		upper = strings.TrimPrefix(upper, "SIG")
		if s, ok := signalMap[upper]; ok {
			sig = s
			pidArgs = args[1:]
		} else {
			// Try signal number
			n, numErr := strconv.Atoi(sigStr)
			if numErr == nil {
				if s, ok := signalNumbers[n]; ok {
					sig = s
				} else {
					sig = syscall.Signal(n)
				}
				pidArgs = args[1:]
			}
		}
	}

	for _, pidStr := range pidArgs {
		var pid int
		if strings.HasPrefix(pidStr, "%") {
			// Job spec: %1, %2, etc.
			j := resolveJob(pidStr)
			if j == nil {
				fmt.Fprintf(os.Stderr, "kill: %s: no such job\n", pidStr)
				continue
			}
			pid = j.Pid
		} else {
			var parseErr error
			pid, parseErr = strconv.Atoi(pidStr)
			if parseErr != nil {
				fmt.Fprintf(os.Stderr, "kill: %s: invalid pid\n", pidStr)
				continue
			}
		}
		if killErr := syscall.Kill(pid, sig); killErr != nil {
			fmt.Fprintf(os.Stderr, "kill: (%d) - %s\n", pid, killErr)
		}
	}
	return 0, nil
}

// ---------------------------------------------------------------------------
// getopts
// ---------------------------------------------------------------------------

func builtinGetopts(args []string, env *Env) (int, error) {
	if len(args) < 2 {
		return 1, fmt.Errorf("getopts: usage: getopts optstring name [args]")
	}

	optstring := args[0]
	name := args[1]

	// Determine the argument list to parse
	var optArgs []string
	if len(args) > 2 {
		optArgs = args[2:]
	} else {
		optArgs = env.posArgs()
	}

	// Get current OPTIND
	optind := 1
	if v, ok := env.Get("OPTIND"); ok {
		if n, err := strconv.Atoi(v.ToStr()); err == nil {
			optind = n
		}
	}

	if optind < 1 {
		optind = 1
	}

	// Check if we're done
	if optind > len(optArgs) {
		env.Set(name, StringVal("?"))
		return 1, nil
	}

	current := optArgs[optind-1]
	if !strings.HasPrefix(current, "-") || current == "-" || current == "--" {
		env.Set(name, StringVal("?"))
		if current == "--" {
			env.Set("OPTIND", StringVal(strconv.Itoa(optind+1)))
		}
		return 1, nil
	}

	// Parse the option character. Track position within bundled options.
	optOfs := 1
	if v, ok := env.Get("__ISH_OPTOFS"); ok {
		if n, err := strconv.Atoi(v.ToStr()); err == nil {
			optOfs = n
		}
	}

	if optOfs >= len(current) {
		// Move to next arg
		optind++
		optOfs = 1
		if optind > len(optArgs) {
			env.Set(name, StringVal("?"))
			env.Set("OPTIND", StringVal(strconv.Itoa(optind)))
			return 1, nil
		}
		current = optArgs[optind-1]
		if !strings.HasPrefix(current, "-") || current == "-" {
			env.Set(name, StringVal("?"))
			return 1, nil
		}
	}

	ch := current[optOfs]
	optOfs++

	// Look up in optstring
	idx := strings.IndexByte(optstring, ch)
	if idx < 0 {
		// Unknown option
		env.Set(name, StringVal("?"))
		env.Set("OPTARG", StringVal(string(ch)))
		if optOfs >= len(current) {
			optind++
			env.Set("__ISH_OPTOFS", StringVal("1"))
		} else {
			env.Set("__ISH_OPTOFS", StringVal(strconv.Itoa(optOfs)))
		}
		env.Set("OPTIND", StringVal(strconv.Itoa(optind)))
		return 0, nil
	}

	env.Set(name, StringVal(string(ch)))

	// Check if option takes an argument (followed by : in optstring)
	if idx+1 < len(optstring) && optstring[idx+1] == ':' {
		if optOfs < len(current) {
			// Rest of current arg is the argument
			env.Set("OPTARG", StringVal(current[optOfs:]))
			optind++
			optOfs = 1
		} else {
			// Next arg is the argument
			optind++
			if optind > len(optArgs) {
				env.Set(name, StringVal("?"))
				env.Set("OPTIND", StringVal(strconv.Itoa(optind)))
				return 0, nil
			}
			env.Set("OPTARG", StringVal(optArgs[optind-1]))
			optind++
			optOfs = 1
		}
	} else {
		if optOfs >= len(current) {
			optind++
			optOfs = 1
		}
	}

	env.Set("OPTIND", StringVal(strconv.Itoa(optind)))
	env.Set("__ISH_OPTOFS", StringVal(strconv.Itoa(optOfs)))
	return 0, nil
}

// ---------------------------------------------------------------------------
// umask
// ---------------------------------------------------------------------------

func builtinUmask(args []string, env *Env) (int, error) {
	if len(args) == 0 {
		// Print current mask in octal
		old := syscall.Umask(0)
		syscall.Umask(old) // restore
		fmt.Fprintf(env.Stdout(), "%04o\n", old)
		return 0, nil
	}

	// Parse octal mask
	mask, err := strconv.ParseUint(args[0], 8, 32)
	if err != nil {
		return 1, fmt.Errorf("umask: %s: invalid octal number", args[0])
	}
	syscall.Umask(int(mask))
	return 0, nil
}

// ---------------------------------------------------------------------------
// ulimit
// ---------------------------------------------------------------------------

type ulimitEntry struct {
	flag    byte
	name    string
	rlim    int
	divisor uint64
}

var ulimitTable = []ulimitEntry{
	{'c', "core file size", syscall.RLIMIT_CORE, 512},
	{'d', "data seg size", syscall.RLIMIT_DATA, 1024},
	{'f', "file size", syscall.RLIMIT_FSIZE, 512},
	{'n', "open files", syscall.RLIMIT_NOFILE, 1},
	{'s', "stack size", syscall.RLIMIT_STACK, 1024},
	{'t', "cpu time", syscall.RLIMIT_CPU, 1},
	{'u', "max user processes", 6, 1},  // RLIMIT_NPROC = 6 on linux
	{'v', "virtual memory", syscall.RLIMIT_AS, 1024},
}

func builtinUlimit(args []string, env *Env) (int, error) {
	w := env.Stdout()

	if len(args) == 0 {
		// Default: show file size limit
		return showUlimit(w, syscall.RLIMIT_FSIZE, 512)
	}

	// ulimit -a: show all limits
	if args[0] == "-a" {
		for _, e := range ulimitTable {
			val, err := getRlimit(e.rlim)
			if err != nil {
				fmt.Fprintf(w, "-%c: %-25s %s\n", e.flag, e.name, "error")
				continue
			}
			fmt.Fprintf(w, "-%c: %-25s %s\n", e.flag, e.name, formatRlimit(val, e.divisor))
		}
		return 0, nil
	}

	// Find the flag
	for _, e := range ulimitTable {
		flag := fmt.Sprintf("-%c", e.flag)
		if args[0] == flag {
			if len(args) == 1 {
				return showUlimit(w, e.rlim, e.divisor)
			}
			// Set the limit
			return setUlimit(args[1], e.rlim, e.divisor)
		}
	}

	return 1, fmt.Errorf("ulimit: invalid option: %s", args[0])
}

func getRlimit(resource int) (uint64, error) {
	var rlim syscall.Rlimit
	err := syscall.Getrlimit(resource, &rlim)
	if err != nil {
		return 0, err
	}
	return rlim.Cur, nil
}

func formatRlimit(val, divisor uint64) string {
	rlimInfinity := uint64(0xFFFFFFFFFFFFFFFF)
	if val == rlimInfinity || val == ^uint64(0) {
		return "unlimited"
	}
	if divisor > 1 {
		return strconv.FormatUint(val/divisor, 10)
	}
	return strconv.FormatUint(val, 10)
}

func showUlimit(w interface{ Write([]byte) (int, error) }, resource int, divisor uint64) (int, error) {
	val, err := getRlimit(resource)
	if err != nil {
		return 1, fmt.Errorf("ulimit: %s", err)
	}
	fmt.Fprintln(w, formatRlimit(val, divisor))
	return 0, nil
}

func setUlimit(valStr string, resource int, divisor uint64) (int, error) {
	rlimInfinity := ^uint64(0)
	var newVal uint64
	if valStr == "unlimited" {
		newVal = rlimInfinity
	} else {
		n, err := strconv.ParseUint(valStr, 10, 64)
		if err != nil {
			return 1, fmt.Errorf("ulimit: %s: invalid limit", valStr)
		}
		newVal = n * divisor
	}

	var rlim syscall.Rlimit
	err := syscall.Getrlimit(resource, &rlim)
	if err != nil {
		return 1, fmt.Errorf("ulimit: %s", err)
	}
	rlim.Cur = newVal
	err = syscall.Setrlimit(resource, &rlim)
	if err != nil {
		return 1, fmt.Errorf("ulimit: %s", err)
	}
	return 0, nil
}

// ---------------------------------------------------------------------------
// alias / unalias
// ---------------------------------------------------------------------------

func builtinAlias(args []string, env *Env) (int, error) {
	if len(args) == 0 {
		// List all aliases
		for k, v := range env.AllAliases() {
			fmt.Fprintf(env.Stdout(), "alias %s='%s'\n", k, v)
		}
		return 0, nil
	}
	for _, arg := range args {
		if idx := strings.IndexByte(arg, '='); idx >= 0 {
			env.SetAlias(arg[:idx], arg[idx+1:])
		} else {
			if v, ok := env.GetAlias(arg); ok {
				fmt.Fprintf(env.Stdout(), "alias %s='%s'\n", arg, v)
			} else {
				fmt.Fprintf(os.Stderr, "alias: %s: not found\n", arg)
			}
		}
	}
	return 0, nil
}

func builtinUnalias(args []string, env *Env) (int, error) {
	for _, arg := range args {
		if arg == "-a" {
			env.aliases = nil
		} else {
			env.DeleteAlias(arg)
		}
	}
	return 0, nil
}

// ---------------------------------------------------------------------------
// command
// ---------------------------------------------------------------------------

func builtinCommand(args []string, env *Env) (int, error) {
	if len(args) == 0 {
		return 0, nil
	}
	// command -v name: check if command exists
	if args[0] == "-v" && len(args) > 1 {
		name := args[1]
		if _, ok := builtins[name]; ok {
			fmt.Fprintln(env.Stdout(), name)
			return 0, nil
		}
		if path, err := exec.LookPath(name); err == nil {
			fmt.Fprintln(env.Stdout(), path)
			return 0, nil
		}
		return 1, nil
	}
	// command -V name: verbose
	if args[0] == "-V" && len(args) > 1 {
		return builtinType(args[1:], env)
	}
	// command name args...: run directly, bypassing aliases and functions
	name := args[0]
	cmdArgs := args[1:]
	if b, ok := builtins[name]; ok {
		return b(cmdArgs, env)
	}
	// External command
	cmd := exec.Command(name, cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = env.Stdout()
	cmd.Stderr = os.Stderr
	cmd.Env = env.BuildEnv()
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 127, fmt.Errorf("command: %s: %s", name, err)
	}
	return 0, nil
}
