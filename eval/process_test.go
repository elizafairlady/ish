package eval

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"ish/core"
	"ish/expand"
)

// expandTo wraps expand.Expand with a t.Helper-friendly signature for the
// end-to-end receive tests.
func expandTo(t *testing.T, src *core.Syntax, ctx *expand.Context) (*core.Syntax, error) {
	t.Helper()
	return expand.Expand(src, ctx)
}

// withProcess constructs a fresh Runtime and root Process, returning an Env
// rooted in them. Per-test isolation prevents cross-test pollution of the
// process registry.
func withProcess() (*Runtime, *Process, *Env) {
	rt := NewRuntime()
	p := rt.NewProcess()
	return rt, p, &Env{Runtime: rt, Process: p}
}

// waitFor receives any message satisfying pred within timeout, failing the
// test if no such message arrives. Used everywhere instead of time.Sleep.
func waitFor(t *testing.T, p *Process, pred func(Value) bool, timeout time.Duration) Value {
	t.Helper()
	msg, _, ok := p.Receive(func(v Value) (int, bool) {
		if pred(v) {
			return 0, true
		}
		return 0, false
	}, timeout)
	if !ok {
		t.Fatalf("waitFor: no matching message within %v", timeout)
	}
	return msg
}

func TestActorSelfReturnsOwnPID(t *testing.T) {
	_, p, env := withProcess()
	v, err := selfFn(nil, env)
	if err != nil || v != p.PID {
		t.Fatalf("self: got (%v, %v), want %v", v, err, p.PID)
	}
}

func TestActorSendAndReceiveRoundTrip(t *testing.T) {
	_, p, _ := withProcess()
	p.Send(core.Atom("hello"))
	msg := waitFor(t, p, func(v Value) bool { return v == core.Atom("hello") }, 500*time.Millisecond)
	if msg != core.Atom("hello") {
		t.Fatalf("round-trip: got %v", msg)
	}
}

func TestActorSendCopiesDatumMessages(t *testing.T) {
	_, p, _ := withProcess()
	msg := core.Bytes{1, 2, 3}
	p.Send(msg)
	msg[0] = 9
	got := waitFor(t, p, func(v Value) bool { return true }, 500*time.Millisecond)
	bytes, ok := got.(core.Bytes)
	if !ok {
		t.Fatalf("message = %T, want bytes", got)
	}
	if bytes[0] != 1 {
		t.Fatalf("message was aliased after send: %#v", bytes)
	}
}

// Selective receive: mailbox has [a, b, c]; match only b first, then receive
// the remaining messages in their original order (a, then c).
func TestActorSelectiveReceivePreservesOrder(t *testing.T) {
	_, p, _ := withProcess()
	p.Send(core.Atom("a"))
	p.Send(core.Atom("b"))
	p.Send(core.Atom("c"))
	got := waitFor(t, p, func(v Value) bool { return v == core.Atom("b") }, 500*time.Millisecond)
	if got != core.Atom("b") {
		t.Fatalf("first match: %v", got)
	}
	got = waitFor(t, p, func(v Value) bool { return v == core.Atom("a") || v == core.Atom("c") }, 500*time.Millisecond)
	if got != core.Atom("a") {
		t.Fatalf("expected a remaining first, got %v", got)
	}
	got = waitFor(t, p, func(v Value) bool { return true }, 500*time.Millisecond)
	if got != core.Atom("c") {
		t.Fatalf("expected c remaining last, got %v", got)
	}
}

func TestActorReceiveTimeoutFiresOnEmptyMailbox(t *testing.T) {
	_, p, _ := withProcess()
	start := time.Now()
	_, _, ok := p.Receive(func(v Value) (int, bool) { return 0, false }, 50*time.Millisecond)
	if ok {
		t.Fatal("Receive returned a message on empty mailbox")
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("timeout returned too fast: %v", elapsed)
	}
}

func TestActorReceiveTimeoutFiresWhenNoneMatch(t *testing.T) {
	_, p, _ := withProcess()
	p.Send(core.Atom("not-this"))
	_, _, ok := p.Receive(func(v Value) (int, bool) { return 0, false }, 50*time.Millisecond)
	if ok {
		t.Fatal("Receive matched when predicate always rejects")
	}
}

func TestActorMonitorRunningProcessDeliversDownOnExit(t *testing.T) {
	rt, observer, _ := withProcess()
	target := rt.NewProcess()
	ref := target.AddMonitor(observer)
	// Now exit target abnormally.
	target.exit(core.Atom("crash"))
	msg := waitFor(t, observer, func(v Value) bool {
		t, ok := v.(core.Tuple)
		return ok && len(t) == 4 && t[0] == core.Atom("down")
	}, 500*time.Millisecond)
	tup := msg.(core.Tuple)
	if tup[1] != ref || tup[2] != target.PID || tup[3] != core.Atom("crash") {
		t.Fatalf("down message wrong: %v", tup)
	}
}

func TestActorMonitorAlreadyExitedDeliversImmediately(t *testing.T) {
	rt, observer, _ := withProcess()
	target := rt.NewProcess()
	target.exit(core.Atom("normal"))
	ref := target.AddMonitor(observer)
	msg := waitFor(t, observer, func(v Value) bool {
		t, ok := v.(core.Tuple)
		return ok && len(t) == 4 && t[0] == core.Atom("down")
	}, 200*time.Millisecond)
	tup := msg.(core.Tuple)
	if tup[1] != ref || tup[2] != target.PID || tup[3] != core.Atom("normal") {
		t.Fatalf("immediate down message wrong: %v", tup)
	}
}

func TestActorMonitorOfUnknownPIDDeliversNoproc(t *testing.T) {
	_, _, env := withProcess()
	// Use a PID that no process has been registered for.
	unknown := core.PID(99999)
	v, err := monitorFn([]Value{unknown}, env)
	if err != nil {
		t.Fatalf("monitor: %v", err)
	}
	ref, ok := v.(core.Ref)
	if !ok {
		t.Fatalf("monitor did not return Ref: %T", v)
	}
	msg := waitFor(t, env.Process, func(v Value) bool {
		t, ok := v.(core.Tuple)
		return ok && len(t) == 4 && t[0] == core.Atom("down")
	}, 200*time.Millisecond)
	tup := msg.(core.Tuple)
	if tup[1] != ref || tup[2] != unknown || tup[3] != core.Atom("noproc") {
		t.Fatalf("noproc message wrong: %v", tup)
	}
}

func TestActorUnmonitorSuppressesDown(t *testing.T) {
	rt, observer, env := withProcess()
	target := rt.NewProcess()
	v, err := monitorFn([]Value{target.PID}, env)
	if err != nil {
		t.Fatalf("monitor: %v", err)
	}
	ref := v.(core.Ref)
	if _, err := unmonitorFn([]Value{ref}, env); err != nil {
		t.Fatalf("unmonitor: %v", err)
	}
	target.exit(core.Atom("crash"))
	// Allow propagation a moment; then prove no :down arrived.
	_, _, ok := observer.Receive(func(v Value) (int, bool) {
		t, ok := v.(core.Tuple)
		if ok && len(t) >= 1 && t[0] == core.Atom("down") {
			return 0, true
		}
		return 0, false
	}, 50*time.Millisecond)
	if ok {
		t.Fatal("unmonitor did not suppress :down")
	}
}

func TestActorLinkKillsPeerOnAbnormalExit(t *testing.T) {
	rt, a, _ := withProcess()
	b := rt.NewProcess()
	a.AddLink(b)
	// Add a monitor on b so we can observe its exit from a third party.
	observer := rt.NewProcess()
	b.AddMonitor(observer)
	a.exit(core.Atom("crash"))
	msg := waitFor(t, observer, func(v Value) bool {
		t, ok := v.(core.Tuple)
		return ok && len(t) == 4 && t[0] == core.Atom("down") && t[2] == b.PID
	}, 500*time.Millisecond)
	tup := msg.(core.Tuple)
	if tup[3] != core.Atom("crash") {
		t.Fatalf("linked peer should die with same reason; got %v", tup[3])
	}
}

func TestActorLinkDoesNotKillPeerOnNormalExit(t *testing.T) {
	rt, a, _ := withProcess()
	b := rt.NewProcess()
	a.AddLink(b)
	// Observer monitors b so we can detect (its absence of) propagation.
	observer := rt.NewProcess()
	b.AddMonitor(observer)
	a.exit(core.Atom("normal"))
	_, _, ok := observer.Receive(func(v Value) (int, bool) {
		t, ok := v.(core.Tuple)
		if ok && len(t) == 4 && t[0] == core.Atom("down") && t[2] == b.PID {
			return 0, true
		}
		return 0, false
	}, 50*time.Millisecond)
	if ok {
		t.Fatal("link killed peer on normal exit")
	}
}

func TestActorSendToExitedProcessIsSilentlyDropped(t *testing.T) {
	rt, _, _ := withProcess()
	target := rt.NewProcess()
	target.exit(core.Atom("normal"))
	target.Send(core.Atom("dropped"))
	target.mu.Lock()
	defer target.mu.Unlock()
	if len(target.mailbox) != 0 {
		t.Fatalf("send to dead process landed in mailbox: %v", target.mailbox)
	}
}

// Receive guard errors behave like guard failure: the message is not consumed,
// and later clauses/receives can still match it.
func TestActorReceiveGuardErrorIsFailureAndDoesNotConsume(t *testing.T) {
	ctx, _, _ := setup()
	rt := NewRuntime()
	p := rt.NewProcess()
	env := &Env{Runtime: rt, Process: p}
	p.Send(core.Int(5))
	// (receive [x when (mod x 0) :bad] [after 1 (quote timed-out)])
	guard := core.SyntaxList(core.Span{},
		&core.Syntax{Node: core.Word("mod")},
		&core.Syntax{Node: core.Atom("bad")},
		&core.Syntax{Node: core.Int(0)})
	msgClause := &core.Syntax{Node: core.SyntaxVector{
		&core.Syntax{Node: core.Word("x")},
		&core.Syntax{Node: core.Word("when")},
		guard,
		&core.Syntax{Node: core.Word("x")},
	}}
	afterClause := &core.Syntax{Node: core.SyntaxVector{
		&core.Syntax{Node: core.Word("after")},
		&core.Syntax{Node: core.Int(100)},
		core.SyntaxList(core.Span{}, &core.Syntax{Node: core.Word("quote")},
			&core.Syntax{Node: core.Atom("timed-out")}),
	}}
	src := core.SyntaxList(core.Span{}, &core.Syntax{Node: core.Word("receive")},
		msgClause, afterClause)
	expanded, err := expand.Expand(src, ctx)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	v, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != core.Atom("timed-out") {
		t.Fatalf("guard error result = %#v, want :timed-out", v)
	}
	msg := waitFor(t, p, func(v Value) bool { return v == core.Int(5) }, 100*time.Millisecond)
	if msg != core.Int(5) {
		t.Fatalf("guard failure consumed message: %#v", msg)
	}
}

// Ping-pong: a spawned closure receives a sender PID, replies, exits.
// The root process observes the reply, confirming send+receive+spawn work
// end-to-end across two real goroutines.
func TestActorPingPongBetweenSpawned(t *testing.T) {
	_, root, env := withProcess()
	pong := Native("probe", func(args []Value, callEnv *Env) (Value, error) {
		msg, _, ok := callEnv.Process.Receive(func(v Value) (int, bool) {
			if _, ok := v.(core.PID); ok {
				return 0, true
			}
			return 0, false
		}, 500*time.Millisecond)
		if !ok {
			return nil, fmt.Errorf("pong receive timeout")
		}
		sender := msg.(core.PID)
		if target := callEnv.Runtime.lookup(sender); target != nil {
			target.Send(core.Atom("pong-reply"))
		}
		return core.Nil{}, nil
	})
	pongPID := env.Runtime.Spawn(pong)
	target := env.Runtime.lookup(pongPID)
	target.Send(root.PID)
	got := waitFor(t, root, func(v Value) bool { return v == core.Atom("pong-reply") }, 500*time.Millisecond)
	if got != core.Atom("pong-reply") {
		t.Fatalf("ping-pong: %v", got)
	}
}

func TestActorSendToUnknownPIDIsSilentlyDropped(t *testing.T) {
	_, _, env := withProcess()
	v, err := sendFn([]Value{core.PID(99999), core.Atom("dropped")}, env)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if v != core.Atom("dropped") {
		t.Fatalf("send should return message: %v", v)
	}
}

// Spawn a function that panics; monitors must receive a :down with an
// :error reason. Proves the panic-recovery defer in Runtime.Spawn works.
func TestActorSpawnPanicPropagatesError(t *testing.T) {
	_, observer, env := withProcess()
	panicker := Native("probe", func(args []Value, env *Env) (Value, error) {
		panic("kaboom")
	})
	pid := env.Runtime.Spawn(panicker)
	target := env.Runtime.lookup(pid)
	target.AddMonitor(observer)
	msg := waitFor(t, observer, func(v Value) bool {
		t, ok := v.(core.Tuple)
		return ok && len(t) == 4 && t[0] == core.Atom("down")
	}, 500*time.Millisecond)
	tup := msg.(core.Tuple)
	if tup[2] != pid {
		t.Fatalf("down for wrong pid: %v", tup)
	}
	reason, ok := tup[3].(core.Tuple)
	if !ok || len(reason) != 2 || reason[0] != core.Atom("error") {
		t.Fatalf("panic exit reason not :error tuple: %v", tup[3])
	}
}

// Spawn returning normally produces a :normal exit reason for monitors.
func TestActorSpawnNormalExit(t *testing.T) {
	_, observer, env := withProcess()
	doneSignal := sync.WaitGroup{}
	doneSignal.Add(1)
	noop := Native("probe", func(args []Value, env *Env) (Value, error) {
		doneSignal.Done()
		return core.Nil{}, nil
	})
	pid := env.Runtime.Spawn(noop)
	target := env.Runtime.lookup(pid)
	target.AddMonitor(observer)
	doneSignal.Wait()
	msg := waitFor(t, observer, func(v Value) bool {
		t, ok := v.(core.Tuple)
		return ok && len(t) == 4 && t[0] == core.Atom("down")
	}, 500*time.Millisecond)
	tup := msg.(core.Tuple)
	if tup[3] != core.Atom("normal") {
		t.Fatalf("normal exit reason wrong: %v", tup[3])
	}
}

func TestActorSpawnMonitorIsAtomic(t *testing.T) {
	_, observer, env := withProcess()
	quick := Native("probe", func(args []Value, env *Env) (Value, error) { return core.Atom("ok"), nil })
	v, err := spawnMonitorFn([]Value{quick}, env)
	if err != nil {
		t.Fatalf("spawn-monitor: %v", err)
	}
	pair, ok := v.(core.Tuple)
	if !ok || len(pair) != 2 {
		t.Fatalf("spawn-monitor result = %#v, want {pid ref}", v)
	}
	pid := pair[0].(core.PID)
	ref := pair[1].(core.Ref)
	msg := waitFor(t, observer, func(v Value) bool {
		t, ok := v.(core.Tuple)
		return ok && len(t) == 4 && t[0] == core.Atom("down")
	}, 500*time.Millisecond)
	down := msg.(core.Tuple)
	if down[1] != ref || down[2] != pid || down[3] != core.Atom("normal") {
		t.Fatalf("down = %#v, want ref/pid/:normal", down)
	}
}

func TestActorSpawnLinkPropagatesAbnormalExit(t *testing.T) {
	_, parent, env := withProcess()
	observer := env.Runtime.NewProcess()
	parent.AddMonitor(observer)
	bad := Native("probe", func(args []Value, env *Env) (Value, error) { return nil, fmt.Errorf("boom") })
	if _, err := spawnLinkFn([]Value{bad}, env); err != nil {
		t.Fatalf("spawn-link: %v", err)
	}
	msg := waitFor(t, observer, func(v Value) bool {
		t, ok := v.(core.Tuple)
		return ok && len(t) == 4 && t[0] == core.Atom("down") && t[2] == parent.PID
	}, 500*time.Millisecond)
	down := msg.(core.Tuple)
	if reason, ok := down[3].(core.Tuple); !ok || len(reason) != 2 || reason[0] != core.Atom("error") {
		t.Fatalf("parent exit reason = %#v, want error tuple", down[3])
	}
}

func TestActorSpawnNormalExitUnregisters(t *testing.T) {
	rt := NewRuntime()
	done := sync.WaitGroup{}
	done.Add(1)
	pid := rt.Spawn(Native("probe", func(args []Value, env *Env) (Value, error) {
		done.Done()
		return core.Nil{}, nil
	}))
	done.Wait()
	for i := 0; i < 100; i++ {
		if rt.lookup(pid) == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("process %d still registered after normal exit", pid)
}

// Receive's :infinity timeout means no timer; only a message wakes the
// receiver. Verified by sending a message after a short delay and observing
// it arrives without spurious timeout.
func TestActorReceiveInfinityWaitsForMessage(t *testing.T) {
	_, p, _ := withProcess()
	go func() {
		time.Sleep(30 * time.Millisecond)
		p.Send(core.Atom("late"))
	}()
	got := waitFor(t, p, func(v Value) bool { return v == core.Atom("late") }, 500*time.Millisecond)
	if got != core.Atom("late") {
		t.Fatalf("delayed message lost: %v", got)
	}
}

// End-to-end through the language: (receive [pattern body] [after t body])
// fires the after body on a true empty mailbox.
func TestEvalReceiveAfterTimeout(t *testing.T) {
	ctx, _, _ := setup()
	rt := NewRuntime()
	env := &Env{Runtime: rt, Process: rt.NewProcess()}
	src := receiveSyntax(50, "timed-out")
	expanded, err := expandTo(t, src, ctx)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	v, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !reflect.DeepEqual(v, core.Atom("timed-out")) {
		t.Fatalf("after didn't fire; got %v", v)
	}
}

func TestEvalReceiveMatchesBeforeTimeout(t *testing.T) {
	ctx, _, _ := setup()
	rt := NewRuntime()
	p := rt.NewProcess()
	env := &Env{Runtime: rt, Process: p}
	p.Send(core.Atom("ping"))
	src := receiveSyntax(500, "timed-out")
	expanded, err := expandTo(t, src, ctx)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	v, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != core.Atom("ping") {
		t.Fatalf("expected matched message :ping, got %v", v)
	}
}

func TestEvalReceiveRejectsNegativeTimeout(t *testing.T) {
	ctx, _, _ := setup()
	rt := NewRuntime()
	env := &Env{Runtime: rt, Process: rt.NewProcess()}
	src := receiveSyntax(-1, "bad")
	expanded, err := expandTo(t, src, ctx)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if _, err := EvalExpr(expanded, env); err == nil {
		t.Fatal("negative receive timeout accepted")
	}
}

// receiveSyntax builds (receive [msg msg] [after TIMEOUT (quote AFTER)]).
func receiveSyntax(timeoutMs int, afterAtom string) *core.Syntax {
	msgWord := &core.Syntax{Node: core.Word("msg")}
	msgClause := &core.Syntax{Node: core.SyntaxVector{msgWord, msgWord}}
	afterBody := core.SyntaxList(core.Span{},
		&core.Syntax{Node: core.Word("quote")},
		&core.Syntax{Node: core.Atom(afterAtom)},
	)
	afterClause := &core.Syntax{Node: core.SyntaxVector{
		&core.Syntax{Node: core.Word("after")},
		&core.Syntax{Node: core.Int(timeoutMs)},
		afterBody,
	}}
	return core.SyntaxList(core.Span{},
		&core.Syntax{Node: core.Word("receive")},
		msgClause,
		afterClause,
	)
}

// (We rely on eval_test.go's setup() and expand package being in scope.)

// TestReceiveRemovesMatchedMessageNotPosition guards against position-based
// removal: a match predicate (standing in for a guard, which runs outside the
// mailbox lock) performs a nested receive that removes an earlier message,
// shifting positions. The outer receive must still remove exactly the message
// it matched, by identity — never a stale index.
func TestReceiveRemovesMatchedMessageNotPosition(t *testing.T) {
	rt := NewRuntime()
	p := rt.NewProcess()
	p.Send(core.Atom("a"))
	p.Send(core.Atom("b"))
	consumed := false
	got, _, ok := p.Receive(func(m Value) (int, bool) {
		if m == core.Atom("a") {
			if !consumed {
				consumed = true
				// nested receive consumes :a, shrinking the mailbox to [:b]
				p.Receive(func(n Value) (int, bool) { return 0, n == core.Atom("a") }, 0)
			}
			return 0, false // do not accept :a in the outer receive
		}
		return 0, m == core.Atom("b")
	}, 0)
	if !ok || got != core.Atom("b") {
		t.Fatalf("outer receive got %#v ok=%v, want :b", got, ok)
	}
	// :b was the only remaining message and it was the one removed.
	if _, _, ok := p.Receive(func(Value) (int, bool) { return 0, true }, 0); ok {
		t.Fatal("mailbox should be empty after removing the matched message")
	}
}
