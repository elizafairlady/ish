package builtin

import (
	"fmt"
	"strconv"
	"strings"

	"ish/internal/core"
	"ish/internal/debug"
)

func builtinExport(args []string, env *core.Env) (int, error) {
	for _, arg := range args {
		if i := strings.IndexByte(arg, '='); i >= 0 {
			env.Export(arg[:i], arg[i+1:])
		} else {
			env.ExportName(arg)
		}
	}
	return 0, nil
}

func builtinUnset(args []string, env *core.Env) (int, error) {
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

func builtinSet(args []string, env *core.Env) (int, error) {
	if len(args) == 0 {
		for k, v := range env.Bindings {
			fmt.Fprintf(env.Stdout(), "%s=%s\n", k, v.ToStr())
		}
		return 0, nil
	}
	if args[0] == "--" {
		env.Args = args[1:]
		return 0, nil
	}
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "-o" && i+1 < len(args) {
			opt := args[i+1]
			switch opt {
			case "pipefail":
				env.SetFlag('P', true)
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
				case 'X':
					ensureDebugger(env)
					env.SetFlag('X', true)
					env.SetFlag('x', true)
					if d, ok := env.Debugger.(*debug.Debugger); ok {
						d.TraceAll = true
					}
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
				case 'X':
					env.SetFlag('X', false)
					env.SetFlag('x', false)
					if d, ok := env.Debugger.(*debug.Debugger); ok {
						d.TraceAll = false
					}
				default:
					return 1, fmt.Errorf("set: invalid option: +%c", ch)
				}
			}
			i++
			continue
		}
		break
	}
	return 0, nil
}

// ensureDebugger lazily creates a Debugger if one doesn't exist yet.
func ensureDebugger(env *core.Env) {
	if env.Debugger != nil {
		return
	}
	d := debug.New()
	env.Debugger = d
	// Build source map from current source if available
	if env.Source != "" {
		name := env.SourceName
		if name == "" {
			name = "<eval>"
		}
		sm := debug.NewSourceMap(name, env.Source)
		d.PushSource(sm)
	}
}

func builtinShift(args []string, env *core.Env) (int, error) {
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
	posArgs := env.PosArgs()
	if n > len(posArgs) {
		return 1, fmt.Errorf("shift: %d: shift count out of range", n)
	}
	env.Args = posArgs[n:]
	return 0, nil
}

func builtinLocal(args []string, env *core.Env) (int, error) {
	for _, arg := range args {
		if idx := strings.IndexByte(arg, '='); idx >= 0 {
			name := arg[:idx]
			val := arg[idx+1:]
			env.SetLocal(name, core.StringVal(val))
		} else {
			env.SetLocal(arg, core.StringVal(""))
		}
	}
	return 0, nil
}

func builtinReadonly(args []string, env *core.Env) (int, error) {
	if len(args) == 0 || (len(args) == 1 && args[0] == "-p") {
		w := env.Stdout()
		for c := env; c != nil; c = c.Parent {
			if c.ReadonlySet != nil {
				for name := range c.ReadonlySet {
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
			if err := env.Set(name, core.StringVal(val)); err != nil {
				return 1, fmt.Errorf("readonly: %s", err)
			}
			env.SetReadonly(name)
		} else {
			env.SetReadonly(arg)
		}
	}
	return 0, nil
}
