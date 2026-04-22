package builtin

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"ish/internal/core"
	"ish/internal/jobs"
	"ish/internal/process"
)

func builtinWait(args []string, scope core.Scope) (int, error) {
	if len(args) == 0 {
		jl := jobs.ListJobs()
		for _, j := range jl {
			if j.Done != nil {
				<-j.Done
			}
			jobs.RemoveJob(j.ID)
		}
		return 0, nil
	}

	spec := args[0]
	if strings.HasPrefix(spec, "%") {
		j := jobs.ResolveJob(spec)
		if j == nil {
			return 1, fmt.Errorf("wait: %s: no such job", spec)
		}
		if j.Done != nil {
			<-j.Done
		}
		code := j.ExitCode
		jobs.RemoveJob(j.ID)
		return code, nil
	}

	pid, err := strconv.Atoi(spec)
	if err != nil {
		return 1, fmt.Errorf("wait: %s: not a pid", spec)
	}

	proc := process.FindProcess(int64(pid))
	if proc != nil {
		proc.Await()
		return 0, nil
	}

	j := jobs.FindJobByPid(pid)
	if j != nil {
		if j.Done != nil {
			<-j.Done
		}
		code := j.ExitCode
		jobs.RemoveJob(j.ID)
		return code, nil
	}

	return 127, fmt.Errorf("wait: pid %d is not a child of this shell", pid)
}

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

func builtinKill(args []string, scope core.Scope) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("kill: usage: kill [-signal] pid")
	}

	if args[0] == "-l" {
		w := scope.GetCtx().Stdout
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
			j := jobs.ResolveJob(pidStr)
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
