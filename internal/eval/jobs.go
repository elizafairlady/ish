package eval

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"
)

// Job represents a background or stopped process — system or OTP.
type Job struct {
	ID       int
	Pid      int         // OS pid (0 for OTP-only jobs)
	Pgid     int         // process group
	Command  string      // display string
	Process  *os.Process // OS process handle (nil for OTP)
	OTPProc  *Process    // OTP process (nil for system jobs)
	Done     chan struct{}
	ExitCode int
	mu       sync.Mutex
	status   string // "Running", "Stopped", "Done"
}

func (j *Job) Status() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status
}

func (j *Job) SetStatus(s string) {
	j.mu.Lock()
	j.status = s
	j.mu.Unlock()
}

func (j *Job) SetDone(code int) {
	j.mu.Lock()
	j.status = "Done"
	j.ExitCode = code
	j.mu.Unlock()
	select {
	case <-j.Done:
	default:
		close(j.Done)
	}
}

// JobTable manages all background jobs.
type JobTable struct {
	jobs []*Job
	mu   sync.Mutex
}

func NewJobTable() *JobTable {
	return &JobTable{}
}

func (jt *JobTable) Add(pid int, cmd string, proc *os.Process) *Job {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	id := 1
	for {
		used := false
		for _, j := range jt.jobs {
			if j.ID == id {
				used = true
				break
			}
		}
		if !used {
			break
		}
		id++
	}
	j := &Job{
		ID:      id,
		Pid:     pid,
		Pgid:    pid,
		Command: cmd,
		Process: proc,
		Done:    make(chan struct{}),
		status:  "Running",
	}
	jt.jobs = append(jt.jobs, j)
	return j
}

func (jt *JobTable) AddOTP(cmd string, proc *Process) *Job {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	id := 1
	for {
		used := false
		for _, j := range jt.jobs {
			if j.ID == id {
				used = true
				break
			}
		}
		if !used {
			break
		}
		id++
	}
	j := &Job{
		ID:      id,
		Command: cmd,
		OTPProc: proc,
		Done:    make(chan struct{}),
		status:  "Running",
	}
	jt.jobs = append(jt.jobs, j)
	return j
}

func (jt *JobTable) Find(id int) *Job {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	for _, j := range jt.jobs {
		if j.ID == id {
			return j
		}
	}
	return nil
}

func (jt *JobTable) FindByPid(pid int) *Job {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	for _, j := range jt.jobs {
		if j.Pid == pid {
			return j
		}
	}
	return nil
}

func (jt *JobTable) Remove(id int) {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	for i, j := range jt.jobs {
		if j.ID == id {
			jt.jobs = append(jt.jobs[:i], jt.jobs[i+1:]...)
			return
		}
	}
}

func (jt *JobTable) Last() *Job {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	if len(jt.jobs) == 0 {
		return nil
	}
	return jt.jobs[len(jt.jobs)-1]
}

func (jt *JobTable) All() []*Job {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	result := make([]*Job, len(jt.jobs))
	copy(result, jt.jobs)
	return result
}

func (jt *JobTable) WaitAll() {
	for _, j := range jt.All() {
		<-j.Done
	}
}

func (jt *JobTable) Resolve(spec string) *Job {
	if len(spec) > 0 && spec[0] == '%' {
		var id int
		fmt.Sscanf(spec[1:], "%d", &id)
		return jt.Find(id)
	}
	var pid int
	fmt.Sscanf(spec, "%d", &pid)
	if pid > 0 {
		return jt.FindByPid(pid)
	}
	return nil
}

// Terminal control

func tcsetpgrp(fd int, pgid int) {
	syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCSPGRP), uintptr(unsafe.Pointer(&pgid)))
}

var jobSigChan chan os.Signal

func InitJobSignals() {
	jobSigChan = make(chan os.Signal, 1)
	signal.Notify(jobSigChan, syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)
	go func() {
		for range jobSigChan {
		}
	}()
}

func GiveTerm(ttyFd int, pgid int) {
	signal.Ignore(syscall.SIGTTOU)
	tcsetpgrp(ttyFd, pgid)
	if jobSigChan != nil {
		signal.Notify(jobSigChan, syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)
	}
}

func ReclaimTerm(ttyFd int) {
	signal.Ignore(syscall.SIGTTOU)
	tcsetpgrp(ttyFd, syscall.Getpgrp())
	if jobSigChan != nil {
		signal.Notify(jobSigChan, syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)
	}
}

func WaitFg(pid int) (syscall.WaitStatus, error) {
	var ws syscall.WaitStatus
	_, err := syscall.Wait4(pid, &ws, syscall.WUNTRACED, nil)
	return ws, err
}

func ResetJobSignals() {
	signal.Reset(syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)
}

func RenotifyJobSignals() {
	if jobSigChan != nil {
		signal.Notify(jobSigChan, syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)
	}
}
