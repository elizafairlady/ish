package jobs

import (
	"fmt"
	"os"
	"sync"
	"syscall"

	"ish/internal/core"
)

// Job represents a background or stopped job.
type Job struct {
	ID       int
	Pid      int
	Pgid     int
	Command  string
	Status   string // "Running", "Stopped", "Done"
	Process  *os.Process
	Done     chan struct{} // closed when the process exits
	ExitCode int
	Mu       sync.Mutex // protects Status and ExitCode for concurrent access
}

var jobTable []*Job
var jobMu sync.Mutex
var nextJobID int

// AddJob adds a job to the table and returns its ID.
func AddJob(pid int, cmd string, proc *os.Process) int {
	jobMu.Lock()
	defer jobMu.Unlock()
	nextJobID++
	j := &Job{
		ID:      nextJobID,
		Pid:     pid,
		Pgid:    pid,
		Command: cmd,
		Status:  "Running",
		Process: proc,
		Done:    make(chan struct{}),
	}
	jobTable = append(jobTable, j)
	return j.ID
}

// FindJob finds a job by ID.
func FindJob(id int) *Job {
	jobMu.Lock()
	defer jobMu.Unlock()
	for _, j := range jobTable {
		if j.ID == id {
			return j
		}
	}
	return nil
}

// FindJobByPid finds a job by PID.
func FindJobByPid(pid int) *Job {
	jobMu.Lock()
	defer jobMu.Unlock()
	for _, j := range jobTable {
		if j.Pid == pid {
			return j
		}
	}
	return nil
}

// RemoveJob removes a job from the table.
func RemoveJob(id int) {
	jobMu.Lock()
	defer jobMu.Unlock()
	for i, j := range jobTable {
		if j.ID == id {
			jobTable = append(jobTable[:i], jobTable[i+1:]...)
			return
		}
	}
}

// ListJobs returns a copy of all jobs.
func ListJobs() []*Job {
	jobMu.Lock()
	defer jobMu.Unlock()
	result := make([]*Job, len(jobTable))
	copy(result, jobTable)
	return result
}

// ResolveJob resolves a job spec like "%1" or a PID string.
func ResolveJob(spec string) *Job {
	if len(spec) > 0 && spec[0] == '%' {
		var id int
		fmt.Sscanf(spec[1:], "%d", &id)
		return FindJob(id)
	}
	var pid int
	fmt.Sscanf(spec, "%d", &pid)
	if pid > 0 {
		return FindJobByPid(pid)
	}
	return nil
}

// BuiltinJobs lists all jobs.
func BuiltinJobs(args []string, env *core.Env) (int, error) {
	w := env.Stdout()
	jobs := ListJobs()
	for _, j := range jobs {
		j.Mu.Lock()
		status := j.Status
		j.Mu.Unlock()
		fmt.Fprintf(w, "[%d] %s\t%s\n", j.ID, status, j.Command)
	}
	return 0, nil
}

// BuiltinFg brings a job to the foreground.
func BuiltinFg(args []string, env *core.Env) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("fg: no current job")
	}
	j := ResolveJob(args[0])
	if j == nil {
		return 1, fmt.Errorf("fg: %s: no such job", args[0])
	}

	if j.Process != nil {
		syscall.Kill(j.Pid, syscall.SIGCONT)
	}
	j.Mu.Lock()
	j.Status = "Running"
	j.Mu.Unlock()

	<-j.Done
	j.Mu.Lock()
	code := j.ExitCode
	j.Mu.Unlock()
	RemoveJob(j.ID)
	return code, nil
}

// BuiltinBg resumes a stopped job in the background.
func BuiltinBg(args []string, env *core.Env) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("bg: no current job")
	}
	j := ResolveJob(args[0])
	if j == nil {
		return 1, fmt.Errorf("bg: %s: no such job", args[0])
	}

	if j.Process != nil {
		syscall.Kill(j.Pid, syscall.SIGCONT)
	}
	j.Mu.Lock()
	j.Status = "Running"
	j.Mu.Unlock()
	return 0, nil
}
