package builtin

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"ish/internal/core"
)

var trapSignalMap = map[string]os.Signal{
	"INT":  syscall.SIGINT,
	"TERM": syscall.SIGTERM,
	"HUP":  syscall.SIGHUP,
	"QUIT": syscall.SIGQUIT,
	"USR1": syscall.SIGUSR1,
	"USR2": syscall.SIGUSR2,
}

var (
	trapMu     sync.Mutex
	trapChans  = make(map[string]chan os.Signal)
	trapEnvRef *core.Env
)

func registerSignalTrap(sigName string, env *core.Env) {
	trapMu.Lock()
	defer trapMu.Unlock()

	osSig, ok := trapSignalMap[sigName]
	if !ok {
		return
	}

	trapEnvRef = env

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
				if cmd, ok := e.Ctx.Shell.GetTrap(sigName); ok {
					if cmd != "" {
						evalCtx.RunSource(cmd, e) //nolint: errcheck
					}
				}
			}
		}
	}()
}

func unregisterSignalTrap(sigName string) {
	trapMu.Lock()
	defer trapMu.Unlock()

	if ch, exists := trapChans[sigName]; exists {
		signal.Stop(ch)
		close(ch)
		delete(trapChans, sigName)
	}

	if osSig, ok := trapSignalMap[sigName]; ok {
		signal.Reset(osSig)
	}
}

// RunExitTraps fires EXIT traps. Called at shell exit.
func RunExitTraps(env *core.Env) {
	if cmd, ok := env.Ctx.Shell.GetTrap("EXIT"); ok && cmd != "" {
		evalCtx.RunSource(cmd, env) //nolint: errcheck
	}
}

// CheckErrTrap fires the ERR trap if the last command failed.
func CheckErrTrap(env *core.Env) {
	if env.Ctx.ExitCode() != 0 {
		if cmd, ok := env.Ctx.Shell.GetTrap("ERR"); ok && cmd != "" {
			evalCtx.RunSource(cmd, env) //nolint: errcheck
		}
	}
}

func builtinTrap(args []string, scope core.Scope) (int, error) {
	ctx := scope.GetCtx()
	if len(args) == 0 {
		ctx.Shell.AllTraps(func(sig, cmd string) {
			fmt.Fprintf(ctx.Stdout, "trap -- %q %s\n", cmd, sig)
		})
		return 0, nil
	}

	validSignals := map[string]bool{
		"INT": true, "TERM": true, "HUP": true,
		"EXIT": true, "ERR": true, "QUIT": true,
		"USR1": true, "USR2": true,
	}

	if args[0] == "-l" {
		fmt.Fprintln(ctx.Stdout, "EXIT INT TERM HUP QUIT USR1 USR2 ERR")
		return 0, nil
	}

	if args[0] == "-" {
		for _, sig := range args[1:] {
			sig = strings.ToUpper(sig)
			sig = strings.TrimPrefix(sig, "SIG")
			if !validSignals[sig] {
				fmt.Fprintf(os.Stderr, "trap: %s: invalid signal specification\n", sig)
				continue
			}
			ctx.Shell.DeleteTrap(sig)
			unregisterSignalTrap(sig)
		}
		return 0, nil
	}

	if len(args) < 2 {
		return 1, fmt.Errorf("trap: usage: trap command signal [signal ...]")
	}
	cmd := args[0]
	env := scope.NearestEnv()
	for _, sig := range args[1:] {
		sig = strings.ToUpper(sig)
		sig = strings.TrimPrefix(sig, "SIG")
		if !validSignals[sig] {
			fmt.Fprintf(os.Stderr, "trap: %s: invalid signal specification\n", sig)
			continue
		}
		ctx.Shell.SetTrap(sig, cmd)
		registerSignalTrap(sig, env)
	}
	return 0, nil
}
