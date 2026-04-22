package builtin

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"ish/internal/core"
)

func builtinType(args []string, scope core.Scope) (int, error) {
	env := scope.NearestEnv()
	code := 0
	for _, name := range args {
		if _, ok := Builtins[name]; ok {
			fmt.Fprintf(scope.GetCtx().Stdout, "%s is a shell builtin\n", name)
		} else if _, ok := env.GetFn(name); ok {
			fmt.Fprintf(scope.GetCtx().Stdout, "%s is a function\n", name)
		} else if _, ok := env.GetNativeFn(name); ok {
			fmt.Fprintf(scope.GetCtx().Stdout, "%s is a function\n", name)
		} else if path, err := exec.LookPath(name); err == nil {
			fmt.Fprintf(scope.GetCtx().Stdout, "%s is %s\n", name, path)
		} else {
			fmt.Fprintf(os.Stderr, "ish: type: %s: not found\n", name)
			code = 1
		}
	}
	return code, nil
}

func builtinPwd(args []string, scope core.Scope) (int, error) {
	dir, _ := os.Getwd()
	fmt.Fprintln(scope.GetCtx().Stdout, dir)
	return 0, nil
}

func builtinTimes(args []string, scope core.Scope) (int, error) {
	w := scope.GetCtx().Stdout

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

func builtinGetopts(args []string, scope core.Scope) (int, error) {
	env := scope.NearestEnv()
	if len(args) < 2 {
		return 1, fmt.Errorf("getopts: usage: getopts optstring name [args]")
	}

	optstring := args[0]
	name := args[1]

	var optArgs []string
	if len(args) > 2 {
		optArgs = args[2:]
	} else {
		optArgs = env.PosArgs()
	}

	optind := 1
	if v, ok := scope.Get("OPTIND"); ok {
		if n, err := strconv.Atoi(v.ToStr()); err == nil {
			optind = n
		}
	}

	if optind < 1 {
		optind = 1
	}

	if optind > len(optArgs) {
		scope.Set(name, core.StringVal("?"))
		return 1, nil
	}

	current := optArgs[optind-1]
	if !strings.HasPrefix(current, "-") || current == "-" || current == "--" {
		scope.Set(name, core.StringVal("?"))
		if current == "--" {
			scope.Set("OPTIND", core.StringVal(strconv.Itoa(optind+1)))
		}
		return 1, nil
	}

	optOfs := 1
	if v, ok := scope.Get("__ISH_OPTOFS"); ok {
		if n, err := strconv.Atoi(v.ToStr()); err == nil {
			optOfs = n
		}
	}

	if optOfs >= len(current) {
		optind++
		optOfs = 1
		if optind > len(optArgs) {
			scope.Set(name, core.StringVal("?"))
			scope.Set("OPTIND", core.StringVal(strconv.Itoa(optind)))
			return 1, nil
		}
		current = optArgs[optind-1]
		if !strings.HasPrefix(current, "-") || current == "-" {
			scope.Set(name, core.StringVal("?"))
			return 1, nil
		}
	}

	ch := current[optOfs]
	optOfs++

	idx := strings.IndexByte(optstring, ch)
	if idx < 0 {
		scope.Set(name, core.StringVal("?"))
		scope.Set("OPTARG", core.StringVal(string(ch)))
		if optOfs >= len(current) {
			optind++
			scope.Set("__ISH_OPTOFS", core.StringVal("1"))
		} else {
			scope.Set("__ISH_OPTOFS", core.StringVal(strconv.Itoa(optOfs)))
		}
		scope.Set("OPTIND", core.StringVal(strconv.Itoa(optind)))
		return 0, nil
	}

	scope.Set(name, core.StringVal(string(ch)))

	if idx+1 < len(optstring) && optstring[idx+1] == ':' {
		if optOfs < len(current) {
			scope.Set("OPTARG", core.StringVal(current[optOfs:]))
			optind++
			optOfs = 1
		} else {
			optind++
			if optind > len(optArgs) {
				scope.Set(name, core.StringVal("?"))
				scope.Set("OPTIND", core.StringVal(strconv.Itoa(optind)))
				return 0, nil
			}
			scope.Set("OPTARG", core.StringVal(optArgs[optind-1]))
			optind++
			optOfs = 1
		}
	} else {
		if optOfs >= len(current) {
			optind++
			optOfs = 1
		}
	}

	scope.Set("OPTIND", core.StringVal(strconv.Itoa(optind)))
	scope.Set("__ISH_OPTOFS", core.StringVal(strconv.Itoa(optOfs)))
	return 0, nil
}

func builtinUmask(args []string, scope core.Scope) (int, error) {
	if len(args) == 0 {
		old := syscall.Umask(0)
		syscall.Umask(old)
		fmt.Fprintf(scope.GetCtx().Stdout, "%04o\n", old)
		return 0, nil
	}

	mask, err := strconv.ParseUint(args[0], 8, 32)
	if err != nil {
		return 1, fmt.Errorf("umask: %s: invalid octal number", args[0])
	}
	syscall.Umask(int(mask))
	return 0, nil
}

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
	{'u', "max user processes", 6, 1},
	{'v', "virtual memory", syscall.RLIMIT_AS, 1024},
}

func builtinUlimit(args []string, scope core.Scope) (int, error) {
	w := scope.GetCtx().Stdout

	if len(args) == 0 {
		return showUlimit(w, syscall.RLIMIT_FSIZE, 512)
	}

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

	for _, e := range ulimitTable {
		flag := fmt.Sprintf("-%c", e.flag)
		if args[0] == flag {
			if len(args) == 1 {
				return showUlimit(w, e.rlim, e.divisor)
			}
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
