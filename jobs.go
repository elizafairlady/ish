package main

import (
	"fmt"
	"os"
	"sync"
	"syscall"
)

// Job represents a background or stopped job.
type Job struct {
	ID      int
	Pid     int
	Pgid    int
	Command string
	Status  string // "Running", "Stopped", "Done"
	Process *os.Process
	done    chan struct{} // closed when the process exits
	exitCode int
	mu       sync.Mutex // protects Status and exitCode for concurrent access
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
		done:    make(chan struct{}),
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

// builtinJobs lists all jobs.
func builtinJobs(args []string, env *Env) (int, error) {
	w := env.Stdout()
	jobs := ListJobs()
	for _, j := range jobs {
		j.mu.Lock()
		status := j.Status
		j.mu.Unlock()
		fmt.Fprintf(w, "[%d] %s\t%s\n", j.ID, status, j.Command)
	}
	return 0, nil
}

// builtinFg brings a job to the foreground.
func builtinFg(args []string, env *Env) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("fg: no current job")
	}
	j := resolveJob(args[0])
	if j == nil {
		return 1, fmt.Errorf("fg: %s: no such job", args[0])
	}

	// Send SIGCONT in case it's stopped
	if j.Process != nil {
		syscall.Kill(j.Pid, syscall.SIGCONT)
	}
	j.mu.Lock()
	j.Status = "Running"
	j.mu.Unlock()

	// Wait for the process to finish via the done channel
	<-j.done
	j.mu.Lock()
	code := j.exitCode
	j.mu.Unlock()
	RemoveJob(j.ID)
	return code, nil
}

// builtinBg resumes a stopped job in the background.
func builtinBg(args []string, env *Env) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("bg: no current job")
	}
	j := resolveJob(args[0])
	if j == nil {
		return 1, fmt.Errorf("bg: %s: no such job", args[0])
	}

	// Send SIGCONT
	if j.Process != nil {
		syscall.Kill(j.Pid, syscall.SIGCONT)
	}
	j.mu.Lock()
	j.Status = "Running"
	j.mu.Unlock()
	return 0, nil
}

// resolveJob resolves a job spec like "%1" or a PID string.
func resolveJob(spec string) *Job {
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
