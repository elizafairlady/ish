package process

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"ish/internal/core"
)

var pidCounter int64
var procRegistry sync.Map // int64 -> *Process

func init() {
	core.FindPid = func(id int64) core.Pid {
		p := FindProcess(id)
		if p == nil {
			return nil
		}
		return p
	}
}

type Process struct {
	id        int64
	mailbox   chan core.Value
	done      chan struct{}
	closeOnce sync.Once

	result core.Value
	reason core.Value

	saveQueue []core.Value

	mu       sync.Mutex
	links    []core.Pid
	monitors []*monitor
}

type monitor struct {
	ref     int64
	watcher core.Pid
}

var monitorCounter int64

const maxSaveQueueSize = 10000

func NewProcess() *Process {
	p := &Process{
		id:      atomic.AddInt64(&pidCounter, 1),
		mailbox: make(chan core.Value, 256),
		done:    make(chan struct{}),
		reason:  core.AtomVal("normal"),
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

func (p *Process) ID() int64 { return p.id }

func (p *Process) SetResult(v core.Value) {
	p.result = v
}

func (p *Process) Send(msg core.Value) {
	select {
	case p.mailbox <- msg:
	case <-p.done:
	}
}

func (p *Process) Receive() core.Value {
	select {
	case msg := <-p.mailbox:
		return msg
	case <-p.done:
		return core.Nil
	}
}

func (p *Process) ReceiveSelective(match func(core.Value) bool) (core.Value, bool) {
	for i, msg := range p.saveQueue {
		if match(msg) {
			p.saveQueue = append(p.saveQueue[:i], p.saveQueue[i+1:]...)
			return msg, true
		}
	}
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
			return core.Nil, false
		}
	}
}

func (p *Process) ReceiveSelectiveTimeout(match func(core.Value) bool, d time.Duration) (core.Value, bool) {
	for i, msg := range p.saveQueue {
		if match(msg) {
			p.saveQueue = append(p.saveQueue[:i], p.saveQueue[i+1:]...)
			return msg, true
		}
	}
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
			return core.Nil, false
		case <-timer.C:
			return core.Nil, false
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

func (p *Process) CloseWithReason(reason core.Value) {
	p.closeOnce.Do(func() {
		p.reason = reason
		close(p.done)
		procRegistry.Delete(p.id)
		p.notifyLinks()
		p.notifyMonitors()
	})
}

func (p *Process) Link(other core.Pid) {
	otherProc, ok := other.(*Process)
	if !ok {
		return
	}
	first, second := p, otherProc
	if first.id > second.id {
		first, second = second, first
	}
	first.mu.Lock()
	second.mu.Lock()
	p.links = append(p.links, other)
	otherProc.links = append(otherProc.links, p)
	second.mu.Unlock()
	first.mu.Unlock()
}

func (p *Process) Monitor(watcher core.Pid) int64 {
	ref := atomic.AddInt64(&monitorCounter, 1)
	p.mu.Lock()
	p.monitors = append(p.monitors, &monitor{ref: ref, watcher: watcher})
	p.mu.Unlock()
	return ref
}

func (p *Process) Await() core.Value {
	<-p.done
	return p.result
}

func (p *Process) notifyLinks() {
	p.mu.Lock()
	links := p.links
	p.links = nil
	p.mu.Unlock()

	for _, linked := range links {
		if p.reason.Kind != core.VAtom || p.reason.Str != "normal" {
			linkedProc, ok := linked.(*Process)
			if ok {
				linkedProc.mu.Lock()
				for i, l := range linkedProc.links {
					lp, ok2 := l.(*Process)
					if ok2 && lp == p {
						linkedProc.links = append(linkedProc.links[:i], linkedProc.links[i+1:]...)
						break
					}
				}
				linkedProc.mu.Unlock()
			}
			go linked.CloseWithReason(p.reason)
		}
	}
}

func (p *Process) notifyMonitors() {
	p.mu.Lock()
	monitors := p.monitors
	p.monitors = nil
	p.mu.Unlock()

	pid := core.Value{Kind: core.VPid, Pid: p}
	for _, mon := range monitors {
		msg := core.TupleVal(
			core.AtomVal("DOWN"),
			core.IntVal(mon.ref),
			pid,
			p.reason,
		)
		mon.watcher.Send(msg)
	}
}

// AwaitTimeout blocks until the process finishes or timeout.
func (p *Process) AwaitTimeout(d time.Duration) (core.Value, bool) {
	select {
	case <-p.done:
		return p.result, true
	case <-time.After(d):
		return core.Nil, false
	}
}

// Reason returns the exit reason of this process.
func (p *Process) Reason() core.Value {
	return p.reason
}

// DoneChan returns the done channel for external waiting.
func (p *Process) DoneChan() <-chan struct{} {
	return p.done
}

// Result returns the result value of this process.
func (p *Process) Result() core.Value {
	return p.result
}

// ReceiveTimeout receives with a timeout (non-selective).
func (p *Process) ReceiveTimeout(d time.Duration) (core.Value, bool) {
	select {
	case msg := <-p.mailbox:
		return msg, true
	case <-p.done:
		return core.Nil, false
	case <-time.After(d):
		return core.Nil, false
	}
}

// Supervisor manages child processes with restart strategies.
type Supervisor struct {
	Proc        *Process
	strategy    core.Value
	children    []SupervisorChild
	mu          sync.Mutex
	maxRestarts int
	maxSeconds  int
	restartLog  []time.Time
}

type SupervisorChild struct {
	Name  string
	Fn    *core.FnValue
	proc  *Process
	Env   *core.Env
	alive bool
}

func NewSupervisor(strategy core.Value) *Supervisor {
	return &Supervisor{
		Proc:        NewProcess(),
		strategy:    strategy,
		maxRestarts: 3,
		maxSeconds:  5,
	}
}

func (s *Supervisor) AddChild(name string, fn *core.FnValue, env *core.Env) {
	s.mu.Lock()
	defer s.mu.Unlock()
	child := SupervisorChild{Name: name, Fn: fn, Env: env, alive: true}
	child.proc = s.startChild(child)
	s.children = append(s.children, child)
}

func (s *Supervisor) startChild(child SupervisorChild) *Process {
	proc := NewProcess()
	childEnv := core.CopyEnv(child.Env)
	childEnv.Shell.Proc = proc

	// Copy closure env to avoid races with parent goroutine
	fn := child.Fn
	if fn.Env != nil {
		fnCopy := *fn
		fnCopy.Env = core.CopyEnv(fn.Env)
		fn = &fnCopy
	}

	go func() {
		reason := core.AtomVal("normal")
		defer func() {
			if r := recover(); r != nil {
				reason = core.TupleVal(core.AtomVal("error"), core.StringVal(fmt.Sprintf("%v", r)))
			}
			proc.CloseWithReason(reason)
			s.Proc.Send(core.TupleVal(core.AtomVal("child_exit"), core.StringVal(child.Name), reason))
		}()

		if childEnv.CallFn != nil {
			val, err := childEnv.CallFn(fn, nil, childEnv)
			proc.result = val
			if err != nil {
				reason = core.TupleVal(core.AtomVal("error"), core.StringVal(err.Error()))
			}
		}
	}()

	return proc
}

func (s *Supervisor) Run() {
	for {
		msg := s.Proc.Receive()
		if msg.Kind == core.VNil {
			return
		}

		if msg.Kind != core.VTuple || len(msg.Elems) != 3 {
			continue
		}
		if msg.Elems[0].Kind != core.VAtom || msg.Elems[0].Str != "child_exit" {
			continue
		}

		childName := msg.Elems[1].ToStr()
		reason := msg.Elems[2]

		isNormal := reason.Kind == core.VAtom && reason.Str == "normal"
		if isNormal {
			s.markDead(childName)
			if s.allDead() {
				s.Proc.Close()
				return
			}
			continue
		}

		if s.tooManyRestarts() {
			s.Proc.CloseWithReason(core.TupleVal(core.AtomVal("shutdown"), core.AtomVal("too_many_restarts")))
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
		case strategy.Kind == core.VAtom && strategy.Str == "one_for_one":
			s.restartChild(idx)
		case strategy.Kind == core.VAtom && strategy.Str == "one_for_all":
			s.restartAll()
		case strategy.Kind == core.VAtom && strategy.Str == "rest_for_one":
			s.restartFrom(idx)
		default:
			s.restartChild(idx)
		}
	}
}

func (s *Supervisor) tooManyRestarts() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.restartLog = append(s.restartLog, now)

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
		if c.Name == name {
			return i
		}
	}
	return -1
}

func (s *Supervisor) markDead(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.children {
		if s.children[i].Name == name {
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
	s.Proc.Close()
}
