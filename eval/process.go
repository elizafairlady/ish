package eval

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"ish/core"
)

// requireProcess and requireRuntime validate that an actor primitive has the
// dynamic authority it needs, so the `env == nil || env.X == nil` guard is
// worded one way across every actor primitive.
func requireProcess(env *Env, name string) error {
	if env == nil || env.Process == nil {
		return fmt.Errorf("%s: no current process", name)
	}
	return nil
}

func requireRuntime(env *Env, name string) error {
	if env == nil || env.Runtime == nil {
		return fmt.Errorf("%s: no runtime in scope", name)
	}
	return nil
}

// Runtime owns one isolated universe of processes. Tests construct a fresh
// Runtime to get leak-proof process registries; a REPL or main program
// constructs one at startup and threads it through Env. The Runtime holds
// the PID/Ref counters, the process registry, and the name registry —
// nothing is global.
type Runtime struct {
	pidCounter atomic.Uint64
	refCounter atomic.Uint64
	mu         sync.RWMutex
	processes  map[core.PID]*Process
	names      map[core.Atom]core.PID
}

func NewRuntime() *Runtime {
	return &Runtime{processes: map[core.PID]*Process{}, names: map[core.Atom]core.PID{}}
}

func (r *Runtime) newPID() core.PID { return core.PID(r.pidCounter.Add(1)) }
func (r *Runtime) newRef() core.Ref { return core.Ref(r.refCounter.Add(1)) }

func (r *Runtime) lookup(pid core.PID) *Process {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.processes[pid]
}

// unregister removes pid from the process map and drops every registered
// name pointing at it, so a dead process leaves no registry residue.
func (r *Runtime) unregister(pid core.PID) {
	r.mu.Lock()
	delete(r.processes, pid)
	for name, p := range r.names {
		if p == pid {
			delete(r.names, name)
		}
	}
	r.mu.Unlock()
}

// registerName binds name to pid, failing if name is already taken.
func (r *Runtime) registerName(name core.Atom, pid core.PID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, taken := r.names[name]; taken {
		return false
	}
	r.names[name] = pid
	return true
}

func (r *Runtime) unregisterName(name core.Atom) { r.mu.Lock(); delete(r.names, name); r.mu.Unlock() }

func (r *Runtime) whereis(name core.Atom) (core.PID, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pid, ok := r.names[name]
	return pid, ok
}

// NewProcess allocates and registers a fresh Process in r without starting a
// goroutine; the REPL's root process is built this way. Spawn starts a
// goroutine immediately for child processes.
func (r *Runtime) NewProcess() *Process {
	p := &Process{
		runtime:          r,
		PID:              r.newPID(),
		wake:             make(chan struct{}, 1),
		monitors:         map[core.Ref]core.PID{},
		outgoingMonitors: map[core.Ref]core.PID{},
		links:            map[core.PID]bool{},
	}
	r.mu.Lock()
	r.processes[p.PID] = p
	r.mu.Unlock()
	return p
}

// Spawn allocates a Process and starts a goroutine calling fn() with no args.
// Panics in fn are recovered and reported as {:error "panic: ..."} exits, so
// monitors and links always see a terminal signal.
func (r *Runtime) Spawn(fn Value) core.PID {
	p := r.NewProcess()
	r.startProcess(p, fn)
	return p.PID
}

func (r *Runtime) startProcess(p *Process, fn Value) {
	env := &Env{Runtime: r, Process: p}
	go func() {
		var reason core.Datum = core.Atom("normal")
		defer func() {
			if rec := recover(); rec != nil {
				reason = core.Tuple{core.Atom("error"), core.String(fmt.Sprintf("panic: %v", rec))}
			}
			p.exit(reason)
		}()
		if _, err := apply(fn, nil, env); err != nil {
			reason = core.Tuple{core.Atom("error"), core.String(err.Error())}
		}
	}()
}

func (r *Runtime) SpawnMonitor(fn Value, observer *Process) (core.PID, core.Ref) {
	p := r.NewProcess()
	ref := p.AddMonitor(observer)
	r.startProcess(p, fn)
	return p.PID, ref
}

func (r *Runtime) SpawnLink(fn Value, peer *Process) core.PID {
	p := r.NewProcess()
	p.AddLink(peer)
	r.startProcess(p, fn)
	return p.PID
}

// Process is the runtime state of an actor: identity, mailbox, exit status,
// monitor records (both incoming — observers watching this process — and
// outgoing — refs this process holds for processes it monitors), and link
// set. Each spawned process runs in its own goroutine.
type Process struct {
	runtime *Runtime
	PID     core.PID

	mu               sync.Mutex
	mailbox          []mailboxMessage
	msgSeq           uint64
	wake             chan struct{}
	exited           bool
	trapExit         bool
	reason           core.Datum
	monitors         map[core.Ref]core.PID // observers watching me, keyed by ref
	outgoingMonitors map[core.Ref]core.PID // targets I monitor, keyed by ref
	links            map[core.PID]bool
}

// mailboxMessage pairs a message with a per-process monotonic sequence id, so a
// matched message can be removed by identity even if a guard (which runs
// outside the mailbox lock) performs a nested receive that shifts positions.
type mailboxMessage struct {
	seq uint64
	msg Value
}

// IsExited reports whether p has terminated. Used by evalReceive to
// distinguish "timeout fired" from "process died mid-receive."
func (p *Process) IsExited() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exited
}

// Send appends a message to p's mailbox and wakes any receiver. A send to a
// terminated process is silently dropped (Erlang convention).
func (p *Process) Send(msg Value) {
	if datum, ok := msg.(core.Datum); ok {
		msg = core.CloneDatum(datum)
	}
	p.mu.Lock()
	if p.exited {
		p.mu.Unlock()
		return
	}
	p.mailbox = append(p.mailbox, mailboxMessage{seq: p.msgSeq, msg: msg})
	p.msgSeq++
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Receive scans p's mailbox for the first message satisfying match. The
// matching message is removed and (msg, clauseIdx, true) returned. On
// timeout or on the process exiting, returns (nil, 0, false). timeout < 0
// means wait forever (only interruptible by exit).
func (p *Process) Receive(match func(Value) (int, bool), timeout time.Duration) (Value, int, bool) {
	var timerC <-chan time.Time
	if timeout >= 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timerC = t.C
	}
	for {
		p.mu.Lock()
		if p.exited {
			p.mu.Unlock()
			return nil, 0, false
		}
		snapshot := append([]mailboxMessage(nil), p.mailbox...)
		p.mu.Unlock()
		// Guards run outside the lock, so a guard may itself receive and remove
		// messages. Match on the snapshot, then remove the matched message by
		// its sequence id under the lock — never by position, which a nested
		// receive could have invalidated. If the id is gone (already consumed),
		// re-snapshot and try again.
		for _, entry := range snapshot {
			idx, ok := match(entry.msg)
			if !ok {
				continue
			}
			p.mu.Lock()
			removed := false
			for i := range p.mailbox {
				if p.mailbox[i].seq == entry.seq {
					p.mailbox = append(p.mailbox[:i], p.mailbox[i+1:]...)
					removed = true
					break
				}
			}
			p.mu.Unlock()
			if removed {
				return entry.msg, idx, true
			}
			break
		}
		select {
		case <-p.wake:
		case <-timerC:
			return nil, 0, false
		}
	}
}

// lockTwo locks two process mutexes in a fixed PID order so concurrent
// link/monitor operations on the same pair can never deadlock. Locking a
// process against itself takes the mutex once.
func lockTwo(a, b *Process) {
	switch {
	case a == b:
		a.mu.Lock()
	case a.PID < b.PID:
		a.mu.Lock()
		b.mu.Lock()
	default:
		b.mu.Lock()
		a.mu.Lock()
	}
}

func unlockTwo(a, b *Process) {
	a.mu.Unlock()
	if a != b {
		b.mu.Unlock()
	}
}

// signalExit delivers a linked peer's termination to p: a trapping process
// receives {:exit from reason} as an ordinary message (for any reason); a
// non-trapping process is killed by abnormal reasons and ignores normal ones.
// The tag is lowercase to match the monitor :down convention.
func (p *Process) signalExit(from core.PID, reason core.Datum) {
	p.mu.Lock()
	trap := p.trapExit
	p.mu.Unlock()
	if trap {
		p.Send(core.Tuple{core.Atom("exit"), from, reason})
		return
	}
	if !atomEq(reason, "normal") {
		p.exit(reason)
	}
}

// AddMonitor records observer as monitoring p. If p has already exited, the
// :down message is delivered to observer immediately and no monitor record is
// kept (there is nothing left to unmonitor). The returned Ref is recorded on
// observer's outgoingMonitors so unmonitor is O(1).
func (p *Process) AddMonitor(observer *Process) core.Ref {
	ref := p.runtime.newRef()
	lockTwo(p, observer)
	if p.exited {
		reason := p.reason
		unlockTwo(p, observer)
		observer.Send(core.Tuple{core.Atom("down"), ref, p.PID, reason})
		return ref
	}
	p.monitors[ref] = observer.PID
	observer.outgoingMonitors[ref] = p.PID
	unlockTwo(p, observer)
	return ref
}

// RemoveMonitor removes the (ref, observer) entry from p's incoming monitor
// table. Idempotent for unknown refs.
func (p *Process) RemoveMonitor(ref core.Ref) {
	p.mu.Lock()
	delete(p.monitors, ref)
	p.mu.Unlock()
}

// AddLink registers a bidirectional link to peer under a fixed lock order. If
// peer has already exited, peer's termination is signalled to p immediately.
func (p *Process) AddLink(peer *Process) {
	if peer == nil || peer.PID == p.PID {
		return
	}
	lockTwo(p, peer)
	if peer.exited {
		reason := peer.reason
		unlockTwo(p, peer)
		p.signalExit(peer.PID, reason)
		return
	}
	if p.exited {
		unlockTwo(p, peer)
		return
	}
	p.links[peer.PID] = true
	peer.links[p.PID] = true
	unlockTwo(p, peer)
}

// RemoveLink removes the bidirectional link to peer if present.
func (p *Process) RemoveLink(peer *Process) {
	if peer == nil {
		return
	}
	lockTwo(p, peer)
	delete(p.links, peer.PID)
	delete(peer.links, p.PID)
	unlockTwo(p, peer)
}

// exit terminates p, fires monitor down-messages, signals linked peers, and
// clears all peer bookkeeping that referenced p so no stale link/monitor
// state survives. Re-entrant exits are a no-op.
func (p *Process) exit(reason core.Datum) {
	p.mu.Lock()
	if p.exited {
		p.mu.Unlock()
		return
	}
	p.exited = true
	p.reason = reason
	monitors := make(map[core.Ref]core.PID, len(p.monitors))
	for ref, observer := range p.monitors {
		monitors[ref] = observer
	}
	outgoing := make(map[core.Ref]core.PID, len(p.outgoingMonitors))
	for ref, target := range p.outgoingMonitors {
		outgoing[ref] = target
	}
	links := make([]core.PID, 0, len(p.links))
	for linked := range p.links {
		links = append(links, linked)
	}
	p.mu.Unlock()

	select {
	case p.wake <- struct{}{}:
	default:
	}

	for ref, observerPID := range monitors {
		if obs := p.runtime.lookup(observerPID); obs != nil {
			obs.Send(core.Tuple{core.Atom("down"), ref, p.PID, reason})
			obs.mu.Lock()
			delete(obs.outgoingMonitors, ref)
			obs.mu.Unlock()
		}
	}
	for ref, targetPID := range outgoing {
		if t := p.runtime.lookup(targetPID); t != nil {
			t.RemoveMonitor(ref)
		}
	}
	for _, linkedPID := range links {
		lp := p.runtime.lookup(linkedPID)
		if lp == nil {
			continue
		}
		lp.mu.Lock()
		delete(lp.links, p.PID)
		lp.mu.Unlock()
		lp.signalExit(p.PID, reason)
	}
	p.runtime.unregister(p.PID)
}

func atomEq(v core.Datum, name string) bool {
	a, ok := v.(core.Atom)
	return ok && string(a) == name
}

// evalReceive runs a selective receive against the current process's
// mailbox. Each clause is a LambdaClause whose Params holds the single
// message pattern. Returns an error if the process exits mid-receive.
func evalReceive(r core.Receive, env *Env) (Value, error) {
	if err := requireProcess(env, "receive"); err != nil {
		return nil, err
	}
	timeout := time.Duration(-1)
	if r.AfterTimeout != nil {
		v, err := EvalExpr(r.AfterTimeout, env)
		if err != nil {
			return nil, err
		}
		switch t := v.(type) {
		case core.Atom:
			if t != "infinity" {
				return nil, fmt.Errorf("receive: timeout atom must be :infinity, got :%s", t)
			}
		case core.Int:
			if t < 0 {
				return nil, fmt.Errorf("receive: timeout must be non-negative or :infinity, got %d", t)
			}
			timeout = time.Duration(t) * time.Millisecond
		default:
			return nil, fmt.Errorf("receive: timeout must be Int (ms) or :infinity, got %T", v)
		}
	}
	var matchedFrame map[core.BindingID]Value
	var matchedIdx int
	match := func(msg Value) (int, bool) {
		for i, c := range r.Clauses {
			frame := map[core.BindingID]Value{}
			if ok, _ := match(c.Params[0], msg, env, frame); !ok {
				continue
			}
			if c.Guard != nil {
				guardEnv := env.extend(frame)
				g, gerr := EvalExpr(c.Guard, guardEnv)
				if gerr != nil {
					continue
				}
				if !truthy(g) {
					continue
				}
			}
			matchedFrame = frame
			matchedIdx = i
			return i, true
		}
		return 0, false
	}
	_, _, ok := env.Process.Receive(match, timeout)
	if !ok {
		if env.Process.IsExited() {
			return nil, fmt.Errorf("receive: process exited")
		}
		return EvalExpr(r.AfterBody, env)
	}
	clause := r.Clauses[matchedIdx]
	return EvalExpr(clause.Body, env.extend(matchedFrame))
}

// --- Primitives wired into the kernel ---

func selfFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("self", args, 0); err != nil {
		return nil, err
	}
	if err := requireProcess(env, "self"); err != nil {
		return nil, err
	}
	return env.Process.PID, nil
}

type selfValue struct{}

func (selfValue) Value(env *Env) (Value, error) {
	return selfFn(nil, env)
}

func spawnFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("spawn", args, 1); err != nil {
		return nil, err
	}
	if err := requireRuntime(env, "spawn"); err != nil {
		return nil, err
	}
	if !isCallable(args[0]) {
		return nil, fmt.Errorf("spawn: not callable")
	}
	return env.Runtime.Spawn(args[0]), nil
}

func spawnMonitorFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("spawn-monitor", args, 1); err != nil {
		return nil, err
	}
	if err := requireRuntime(env, "spawn-monitor"); err != nil {
		return nil, err
	}
	if err := requireProcess(env, "spawn-monitor"); err != nil {
		return nil, err
	}
	if !isCallable(args[0]) {
		return nil, fmt.Errorf("spawn-monitor: not callable")
	}
	pid, ref := env.Runtime.SpawnMonitor(args[0], env.Process)
	return core.Tuple{pid, ref}, nil
}

func spawnLinkFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("spawn-link", args, 1); err != nil {
		return nil, err
	}
	if err := requireRuntime(env, "spawn-link"); err != nil {
		return nil, err
	}
	if err := requireProcess(env, "spawn-link"); err != nil {
		return nil, err
	}
	if !isCallable(args[0]) {
		return nil, fmt.Errorf("spawn-link: not callable")
	}
	return env.Runtime.SpawnLink(args[0], env.Process), nil
}

func sendFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("send", args, 2); err != nil {
		return nil, err
	}
	if err := requireRuntime(env, "send"); err != nil {
		return nil, err
	}
	pid, err := resolveTarget(args[0], env)
	if err != nil {
		return nil, err
	}
	msg, ok := args[1].(core.Datum)
	if !ok {
		return nil, fmt.Errorf("send: message must be data")
	}
	if p := env.Runtime.lookup(pid); p != nil {
		p.Send(msg)
	}
	return msg, nil
}

// resolveTarget accepts a PID directly or a registered name atom.
func resolveTarget(v Value, env *Env) (core.PID, error) {
	switch t := v.(type) {
	case core.PID:
		return t, nil
	case core.Atom:
		if pid, ok := env.Runtime.whereis(t); ok {
			return pid, nil
		}
		return 0, fmt.Errorf("send: no process registered as :%s", t)
	}
	return 0, fmt.Errorf("send: first arg must be a PID or registered name")
}

func registerFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("register", args, 2); err != nil {
		return nil, err
	}
	if err := requireRuntime(env, "register"); err != nil {
		return nil, err
	}
	name, ok := args[0].(core.Atom)
	if !ok {
		return nil, fmt.Errorf("register: name must be an atom")
	}
	pid, ok := args[1].(core.PID)
	if !ok {
		return nil, fmt.Errorf("register: second arg must be a PID")
	}
	if !env.Runtime.registerName(name, pid) {
		return nil, fmt.Errorf("register: name :%s already taken", name)
	}
	return core.Atom("ok"), nil
}

func unregisterFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("unregister", args, 1); err != nil {
		return nil, err
	}
	if err := requireRuntime(env, "unregister"); err != nil {
		return nil, err
	}
	name, ok := args[0].(core.Atom)
	if !ok {
		return nil, fmt.Errorf("unregister: name must be an atom")
	}
	env.Runtime.unregisterName(name)
	return core.Atom("ok"), nil
}

func whereisFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("whereis", args, 1); err != nil {
		return nil, err
	}
	if err := requireRuntime(env, "whereis"); err != nil {
		return nil, err
	}
	name, ok := args[0].(core.Atom)
	if !ok {
		return nil, fmt.Errorf("whereis: name must be an atom")
	}
	if pid, ok := env.Runtime.whereis(name); ok {
		return pid, nil
	}
	return core.Nil{}, nil
}

func trapExitFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("trap-exit", args, 1); err != nil {
		return nil, err
	}
	if err := requireProcess(env, "trap-exit"); err != nil {
		return nil, err
	}
	p := env.Process
	p.mu.Lock()
	prev := p.trapExit
	p.trapExit = args[0] == core.Atom("true")
	p.mu.Unlock()
	if prev {
		return core.Atom("true"), nil
	}
	return core.Atom("false"), nil
}

func monitorFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("monitor", args, 1); err != nil {
		return nil, err
	}
	if err := requireProcess(env, "monitor"); err != nil {
		return nil, err
	}
	pid, ok := args[0].(core.PID)
	if !ok {
		return nil, fmt.Errorf("monitor: not a PID")
	}
	target := env.Runtime.lookup(pid)
	if target == nil {
		ref := env.Runtime.newRef()
		env.Process.Send(core.Tuple{core.Atom("down"), ref, pid, core.Atom("noproc")})
		return ref, nil
	}
	return target.AddMonitor(env.Process), nil
}

func unmonitorFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("unmonitor", args, 1); err != nil {
		return nil, err
	}
	if err := requireProcess(env, "unmonitor"); err != nil {
		return nil, err
	}
	ref, ok := args[0].(core.Ref)
	if !ok {
		return nil, fmt.Errorf("unmonitor: not a Ref")
	}
	env.Process.mu.Lock()
	targetPID, known := env.Process.outgoingMonitors[ref]
	delete(env.Process.outgoingMonitors, ref)
	env.Process.mu.Unlock()
	if !known {
		return core.Atom("ok"), nil
	}
	if target := env.Runtime.lookup(targetPID); target != nil {
		target.RemoveMonitor(ref)
	}
	return core.Atom("ok"), nil
}

func linkFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("link", args, 1); err != nil {
		return nil, err
	}
	if err := requireProcess(env, "link"); err != nil {
		return nil, err
	}
	pid, ok := args[0].(core.PID)
	if !ok {
		return nil, fmt.Errorf("link: not a PID")
	}
	target := env.Runtime.lookup(pid)
	if target == nil {
		// Linking to an unknown process delivers an immediate noproc exit
		// signal to the caller, mirroring Erlang and monitor's :noproc path.
		env.Process.signalExit(pid, core.Atom("noproc"))
		return core.Atom("ok"), nil
	}
	env.Process.AddLink(target)
	return core.Atom("ok"), nil
}

func unlinkFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("unlink", args, 1); err != nil {
		return nil, err
	}
	if err := requireProcess(env, "unlink"); err != nil {
		return nil, err
	}
	pid, ok := args[0].(core.PID)
	if !ok {
		return nil, fmt.Errorf("unlink: not a PID")
	}
	env.Process.RemoveLink(env.Runtime.lookup(pid))
	return core.Atom("ok"), nil
}
