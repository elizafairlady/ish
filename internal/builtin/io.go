package builtin

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"

	"ish/internal/core"
)

func builtinRead(args []string, env *core.Env) (int, error) {
	var prompt string
	raw := false
	silent := false
	timeout := 0
	nchars := 0
	var varNames []string

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

	if prompt != "" {
		fmt.Fprint(os.Stderr, prompt)
	}

	if silent {
		fd := int(os.Stdin.Fd())
		if term.IsTerminal(fd) {
			oldState, err := term.MakeRaw(fd)
			if err == nil {
				defer func() {
					term.Restore(fd, oldState)
					fmt.Fprint(os.Stderr, "\n")
				}()
			}
		}
	}

	var line string
	var readErr error

	if nchars > 0 {
		buf := make([]byte, nchars)
		if timeout > 0 {
			fd := int(os.Stdin.Fd())
			var readSet syscall.FdSet
			readSet.Bits[fd/64] |= 1 << (uint(fd) % 64)
			tv := syscall.Timeval{Sec: int64(timeout)}
			sn, serr := syscall.Select(fd+1, &readSet, nil, nil, &tv)
			if serr != nil || sn == 0 {
				return 1, nil
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
		if timeout > 0 {
			fd := int(os.Stdin.Fd())
			var readSet syscall.FdSet
			readSet.Bits[fd/64] |= 1 << (uint(fd) % 64)
			tv := syscall.Timeval{Sec: int64(timeout)}
			sn, serr := syscall.Select(fd+1, &readSet, nil, nil, &tv)
			if serr != nil || sn == 0 {
				return 1, nil
			}
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

	if !raw {
		line = strings.ReplaceAll(line, "\\\n", "")
	}

	if len(varNames) == 0 {
		env.Set("REPLY", core.StringVal(line))
	} else if len(varNames) == 1 {
		env.Set(varNames[0], core.StringVal(line))
	} else {
		fields := env.SplitFieldsIFS(line)
		for idx, name := range varNames {
			if idx < len(varNames)-1 {
				if idx < len(fields) {
					env.Set(name, core.StringVal(fields[idx]))
				} else {
					env.Set(name, core.StringVal(""))
				}
			} else {
				if idx < len(fields) {
					remainder := strings.Join(fields[idx:], " ")
					env.Set(name, core.StringVal(remainder))
				} else {
					env.Set(name, core.StringVal(""))
				}
			}
		}
	}
	return 0, nil
}

func builtinExec(args []string, env *core.Env) (int, error) {
	if len(args) == 0 {
		return 0, nil
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		return 127, fmt.Errorf("exec: %s: %s", args[0], err)
	}
	err = syscall.Exec(path, args, env.BuildEnv())
	return 127, fmt.Errorf("exec: %s", err)
}

func builtinEval(args []string, env *core.Env) (int, error) {
	if len(args) == 0 {
		return 0, nil
	}
	line := strings.Join(args, " ")
	val := evalCtx.RunSource(line, env)
	if val.Kind != core.VNil {
		fmt.Fprintln(env.Stdout(), val.String())
	}
	return env.LastExit, nil
}

func builtinSource(args []string, env *core.Env) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("source: filename argument required")
	}
	filename := args[0]
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

	savedArgs := env.Args
	if len(args) > 1 {
		env.Args = args[1:]
	}

	evalCtx.RunSource(string(data), env)

	env.Args = savedArgs

	return env.LastExit, nil
}

func builtinPrintf(args []string, env *core.Env) (int, error) {
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
			spec := "%"
			for i < len(format) && (format[i] == '-' || format[i] == '+' || format[i] == '0' || format[i] == ' ' || format[i] == '#') {
				spec += string(format[i])
				i++
			}
			for i < len(format) && format[i] >= '0' && format[i] <= '9' {
				spec += string(format[i])
				i++
			}
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
