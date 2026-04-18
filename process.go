package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var pidCounter int64
var procRegistry sync.Map // int64 -> *Process

type Process struct {
	id        int64
	mailbox   chan Value
	done      chan struct{}
	closeOnce sync.Once

	// Result of the process's evaluation (for await)
	result Value
	reason Value // exit reason (:normal, :error, or {:error, reason})

	// Selective receive: messages that didn't match are saved for later
	saveQueue []Value

	// Links: processes that die when we die
	mu       sync.Mutex
	links    []*Process
	monitors []*monitor
}

type monitor struct {
	ref     int64
	watcher *Process
}

var monitorCounter int64

func NewProcess() *Process {
	p := &Process{
		id:      atomic.AddInt64(&pidCounter, 1),
		mailbox: make(chan Value, 256),
		done:    make(chan struct{}),
		reason:  AtomVal("normal"),
	}
	procRegistry.Store(p.id, p)
	return p
}

func FindProcess(id int64) *Process {
	if v, ok := procRegistry.Load(id); ok {
		return v.(*Process)
	}
	return nil
}

func (p *Process) Send(msg Value) {
	select {
	case p.mailbox <- msg:
	case <-p.done:
	}
}

func (p *Process) Receive() Value {
	select {
	case msg := <-p.mailbox:
		return msg
	case <-p.done:
		return Nil
	}
}

func (p *Process) ReceiveTimeout(d time.Duration) (Value, bool) {
	select {
	case msg := <-p.mailbox:
		return msg, true
	case <-p.done:
		return Nil, false
	case <-time.After(d):
		return Nil, false
	}
}

const maxSaveQueueSize = 10000

// ReceiveSelective scans the save queue then the mailbox for the first message
// matching the predicate. Non-matching messages are saved for later receives.
func (p *Process) ReceiveSelective(match func(Value) bool) (Value, bool) {
	// 1. Check save queue first
	for i, msg := range p.saveQueue {
		if match(msg) {
			p.saveQueue = append(p.saveQueue[:i], p.saveQueue[i+1:]...)
			return msg, true
		}
	}
	// 2. Pull from channel, saving non-matches
	for {
		select {
		case msg := <-p.mailbox:
			if match(msg) {
				return msg, true
			}
			p.saveQueue = append(p.saveQueue, msg)
			if len(p.saveQueue) > maxSaveQueueSize {
				p.saveQueue = p.saveQueue[1:]
			}
		case <-p.done:
			return Nil, false
		}
	}
}

// ReceiveSelectiveTimeout is like ReceiveSelective but with a timeout.
func (p *Process) ReceiveSelectiveTimeout(match func(Value) bool, d time.Duration) (Value, bool) {
	// 1. Check save queue first
	for i, msg := range p.saveQueue {
		if match(msg) {
			p.saveQueue = append(p.saveQueue[:i], p.saveQueue[i+1:]...)
			return msg, true
		}
	}
	// 2. Pull from channel, saving non-matches, with timeout
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case msg := <-p.mailbox:
			if match(msg) {
				return msg, true
			}
			p.saveQueue = append(p.saveQueue, msg)
			if len(p.saveQueue) > maxSaveQueueSize {
				p.saveQueue = p.saveQueue[1:]
			}
		case <-p.done:
			return Nil, false
		case <-timer.C:
			return Nil, false
		}
	}
}

func (p *Process) Close() {
	p.closeOnce.Do(func() {
		close(p.done)
		procRegistry.Delete(p.id)
		p.notifyLinks()
		p.notifyMonitors()
	})
}

func (p *Process) CloseWithReason(reason Value) {
	p.closeOnce.Do(func() {
		p.reason = reason
		close(p.done)
		procRegistry.Delete(p.id)
		p.notifyLinks()
		p.notifyMonitors()
	})
}

// Link bidirectionally links two processes.
// Lock ordering by process ID prevents deadlock and ensures atomicity.
func (p *Process) Link(other *Process) {
	first, second := p, other
	if first.id > second.id {
		first, second = second, first
	}
	first.mu.Lock()
	second.mu.Lock()
	p.links = append(p.links, other)
	other.links = append(other.links, p)
	second.mu.Unlock()
	first.mu.Unlock()
}

// Monitor sets up a one-way monitor. Returns a ref ID.
// When p exits, watcher receives {:DOWN, ref, pid, reason}.
func (p *Process) Monitor(watcher *Process) int64 {
	ref := atomic.AddInt64(&monitorCounter, 1)
	p.mu.Lock()
	p.monitors = append(p.monitors, &monitor{ref: ref, watcher: watcher})
	p.mu.Unlock()
	return ref
}

func (p *Process) notifyLinks() {
	p.mu.Lock()
	links := p.links
	p.links = nil
	p.mu.Unlock()

	for _, linked := range links {
		if p.reason.Kind != VAtom || p.reason.Str != "normal" {
			// Remove the reverse link to avoid deadlock
			linked.mu.Lock()
			for i, l := range linked.links {
				if l == p {
					linked.links = append(linked.links[:i], linked.links[i+1:]...)
					break
				}
			}
			linked.mu.Unlock()
			// Close in a goroutine to avoid holding our closeOnce
			go linked.CloseWithReason(p.reason)
		}
	}
}

func (p *Process) notifyMonitors() {
	p.mu.Lock()
	monitors := p.monitors
	p.monitors = nil
	p.mu.Unlock()

	pid := Value{Kind: VPid, Pid: p}
	for _, mon := range monitors {
		// Send {:DOWN, ref, pid, reason} to the watcher
		msg := TupleVal(
			AtomVal("DOWN"),
			IntVal(mon.ref),
			pid,
			p.reason,
		)
		mon.watcher.Send(msg)
	}
}

// Await blocks until the process finishes and returns its result.
func (p *Process) Await() Value {
	<-p.done
	return p.result
}

// AwaitTimeout blocks until the process finishes or timeout.
func (p *Process) AwaitTimeout(d time.Duration) (Value, bool) {
	select {
	case <-p.done:
		return p.result, true
	case <-time.After(d):
		return Nil, false
	}
}

// Supervisor manages child processes with restart strategies.
type Supervisor struct {
	proc        *Process
	strategy    Value // :one_for_one, :one_for_all, :rest_for_one
	children    []SupervisorChild
	mu          sync.Mutex
	maxRestarts int       // max restarts in time window (default: 3)
	maxSeconds  int       // time window in seconds (default: 5)
	restartLog  []time.Time // timestamps of recent restarts
}

type SupervisorChild struct {
	name  string
	fn    *FnValue
	proc  *Process
	env   *Env
	alive bool
}

func NewSupervisor(strategy Value) *Supervisor {
	return &Supervisor{
		proc:        NewProcess(),
		strategy:    strategy,
		maxRestarts: 3,
		maxSeconds:  5,
	}
}

func (s *Supervisor) AddChild(name string, fn *FnValue, env *Env) {
	s.mu.Lock()
	defer s.mu.Unlock()
	child := SupervisorChild{name: name, fn: fn, env: env, alive: true}
	child.proc = s.startChild(child)
	s.children = append(s.children, child)
}

func (s *Supervisor) startChild(child SupervisorChild) *Process {
	proc := NewProcess()
	childEnv := CopyEnv(child.env)
	childEnv.proc = proc

	go func() {
		reason := AtomVal("normal")
		defer func() {
			if r := recover(); r != nil {
				reason = TupleVal(AtomVal("error"), StringVal(fmt.Sprintf("%v", r)))
			}
			proc.CloseWithReason(reason)
			// Notify supervisor that this child exited
			s.proc.Send(TupleVal(AtomVal("child_exit"), StringVal(child.name), reason))
		}()

		val, err := callFn(child.fn, nil, childEnv)
		proc.result = val
		if err != nil {
			reason = TupleVal(AtomVal("error"), StringVal(err.Error()))
		}
	}()

	return proc
}

func (s *Supervisor) Run() {
	for {
		// Block on supervisor's mailbox for child exit notifications
		msg := s.proc.Receive()
		if msg.Kind == VNil {
			return // supervisor stopped
		}

		// Expect {:child_exit, name, reason}
		if msg.Kind != VTuple || len(msg.Elems) != 3 {
			continue
		}
		if msg.Elems[0].Kind != VAtom || msg.Elems[0].Str != "child_exit" {
			continue
		}

		childName := msg.Elems[1].ToStr()
		reason := msg.Elems[2]

		// Normal exit — mark dead, don't restart
		isNormal := reason.Kind == VAtom && reason.Str == "normal"
		if isNormal {
			s.markDead(childName)
			if s.allDead() {
				s.proc.Close()
				return
			}
			continue
		}

		// Abnormal exit — check restart rate before restarting
		if s.tooManyRestarts() {
			s.proc.CloseWithReason(TupleVal(AtomVal("shutdown"), AtomVal("too_many_restarts")))
			s.Stop()
			return
		}

		s.mu.Lock()
		strategy := s.strategy
		s.mu.Unlock()

		idx := s.findChild(childName)
		if idx < 0 {
			continue
		}

		switch {
		case strategy.Kind == VAtom && strategy.Str == "one_for_one":
			s.restartChild(idx)
		case strategy.Kind == VAtom && strategy.Str == "one_for_all":
			s.restartAll()
		case strategy.Kind == VAtom && strategy.Str == "rest_for_one":
			s.restartFrom(idx)
		default:
			s.restartChild(idx)
		}
	}
}

// tooManyRestarts checks if we've exceeded the restart rate limit.
func (s *Supervisor) tooManyRestarts() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.restartLog = append(s.restartLog, now)

	// Trim old entries outside the window
	cutoff := now.Add(-time.Duration(s.maxSeconds) * time.Second)
	start := 0
	for start < len(s.restartLog) && s.restartLog[start].Before(cutoff) {
		start++
	}
	s.restartLog = s.restartLog[start:]

	return len(s.restartLog) > s.maxRestarts
}

func (s *Supervisor) findChild(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.children {
		if c.name == name {
			return i
		}
	}
	return -1
}

func (s *Supervisor) markDead(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.children {
		if s.children[i].name == name {
			s.children[i].alive = false
		}
	}
}

func (s *Supervisor) allDead() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.children {
		if c.alive {
			return false
		}
	}
	return true
}

func (s *Supervisor) restartChild(idx int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx < len(s.children) {
		s.children[idx].proc = s.startChild(s.children[idx])
		s.children[idx].alive = true
	}
}

func (s *Supervisor) restartAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.children {
		if s.children[i].alive {
			s.children[i].proc.Close()
		}
		s.children[i].proc = s.startChild(s.children[i])
		s.children[i].alive = true
	}
}

func (s *Supervisor) restartFrom(idx int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := idx; i < len(s.children); i++ {
		if s.children[i].alive {
			s.children[i].proc.Close()
		}
		s.children[i].proc = s.startChild(s.children[i])
		s.children[i].alive = true
	}
}

func (s *Supervisor) Stop() {
	s.mu.Lock()
	children := make([]SupervisorChild, len(s.children))
	copy(children, s.children)
	s.mu.Unlock()

	for _, child := range children {
		if child.alive {
			child.proc.Close()
		}
	}
	s.proc.Close()
}
