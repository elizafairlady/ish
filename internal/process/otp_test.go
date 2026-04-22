package process

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ish/internal/core"
)

// ---------------------------------------------------------------------------
// CloseWithReason
// ---------------------------------------------------------------------------

func TestCloseWithReason(t *testing.T) {
	p := NewProcess()
	p.CloseWithReason(core.AtomVal("killed"))

	if p.reason.Kind != core.VAtom || p.reason.Str != "killed" {
		t.Errorf("reason = %s, want :killed", p.reason.Inspect())
	}
	select {
	case <-p.done:
	default:
		t.Fatal("done channel should be closed")
	}
}

func TestCloseWithReasonDefault(t *testing.T) {
	p := NewProcess()
	if p.reason.Kind != core.VAtom || p.reason.Str != "normal" {
		t.Errorf("default reason = %s, want :normal", p.reason.Inspect())
	}
	p.Close()
	if p.reason.Kind != core.VAtom || p.reason.Str != "normal" {
		t.Errorf("reason after close = %s, want :normal", p.reason.Inspect())
	}
}

// ---------------------------------------------------------------------------
// ReceiveTimeout
// ---------------------------------------------------------------------------

func TestReceiveTimeoutGetsMessage(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	p.Send(core.IntVal(42))
	val, ok := p.ReceiveTimeout(100 * time.Millisecond)
	if !ok {
		t.Fatal("expected to receive message")
	}
	if !val.Equal(core.IntVal(42)) {
		t.Errorf("got %s, want 42", val.Inspect())
	}
}

func TestReceiveTimeoutTimesOut(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	val, ok := p.ReceiveTimeout(10 * time.Millisecond)
	if ok {
		t.Fatalf("expected timeout, got %s", val.Inspect())
	}
	if val.Kind != core.VNil {
		t.Errorf("expected Nil on timeout, got %s", val.Inspect())
	}
}

func TestReceiveTimeoutOnClosed(t *testing.T) {
	p := NewProcess()
	p.Close()

	val, ok := p.ReceiveTimeout(100 * time.Millisecond)
	if ok {
		t.Fatal("expected false from closed process")
	}
	if val.Kind != core.VNil {
		t.Errorf("expected Nil, got %s", val.Inspect())
	}
}

// ---------------------------------------------------------------------------
// Link
// ---------------------------------------------------------------------------

func TestLinkAbnormalExitKillsLinked(t *testing.T) {
	a := NewProcess()
	b := NewProcess()
	a.Link(b)

	a.CloseWithReason(core.TupleVal(core.AtomVal("error"), core.StringVal("crash")))

	select {
	case <-b.done:
		// b was killed
	case <-time.After(time.Second):
		t.Fatal("linked process should have been killed")
	}

	if b.reason.Kind != core.VTuple {
		t.Errorf("linked process reason = %s, want {:error, ...}", b.reason.Inspect())
	}
}

func TestLinkNormalExitDoesNotKillLinked(t *testing.T) {
	a := NewProcess()
	b := NewProcess()
	defer b.Close()
	a.Link(b)

	a.Close()

	time.Sleep(20 * time.Millisecond)

	select {
	case <-b.done:
		t.Fatal("linked process should NOT be killed on normal exit")
	default:
		// correct
	}
}

func TestLinkIsBidirectional(t *testing.T) {
	a := NewProcess()
	b := NewProcess()
	a.Link(b)

	b.CloseWithReason(core.AtomVal("killed"))

	select {
	case <-a.done:
		// a was killed via the link
	case <-time.After(time.Second):
		t.Fatal("link should be bidirectional")
	}
}

// ---------------------------------------------------------------------------
// Monitor
// ---------------------------------------------------------------------------

func TestMonitorSendsDownMessage(t *testing.T) {
	target := NewProcess()
	watcher := NewProcess()
	defer watcher.Close()

	ref := target.Monitor(watcher)

	target.Close()

	msg, ok := watcher.ReceiveTimeout(time.Second)
	if !ok {
		t.Fatal("watcher should receive :DOWN message")
	}

	if msg.Kind != core.VTuple || len(msg.GetElems()) != 4 {
		t.Fatalf("expected 4-tuple, got %s", msg.Inspect())
	}
	if msg.GetElems()[0].Kind != core.VAtom || msg.GetElems()[0].Str != "DOWN" {
		t.Errorf("elem 0 = %s, want :DOWN", msg.GetElems()[0].Inspect())
	}
	if msg.GetElems()[1].Kind != core.VInt || msg.GetElems()[1].GetInt() != ref {
		t.Errorf("elem 1 = %s, want %d", msg.GetElems()[1].Inspect(), ref)
	}
	if msg.GetElems()[2].Kind != core.VPid {
		t.Errorf("elem 2 = %s, want PID", msg.GetElems()[2].Inspect())
	}
	if msg.GetElems()[3].Kind != core.VAtom || msg.GetElems()[3].Str != "normal" {
		t.Errorf("elem 3 = %s, want :normal", msg.GetElems()[3].Inspect())
	}
}

func TestMonitorWithAbnormalReason(t *testing.T) {
	target := NewProcess()
	watcher := NewProcess()
	defer watcher.Close()

	target.Monitor(watcher)
	target.CloseWithReason(core.TupleVal(core.AtomVal("error"), core.StringVal("boom")))

	msg, ok := watcher.ReceiveTimeout(time.Second)
	if !ok {
		t.Fatal("should receive DOWN")
	}
	reason := msg.GetElems()[3]
	if reason.Kind != core.VTuple || reason.GetElems()[0].Str != "error" {
		t.Errorf("reason = %s, want {:error, ...}", reason.Inspect())
	}
}

func TestMonitorRefsAreUnique(t *testing.T) {
	target := NewProcess()
	defer target.Close()
	w1 := NewProcess()
	defer w1.Close()
	w2 := NewProcess()
	defer w2.Close()

	ref1 := target.Monitor(w1)
	ref2 := target.Monitor(w2)
	if ref1 == ref2 {
		t.Errorf("monitor refs should be unique, got %d and %d", ref1, ref2)
	}
}

func TestMonitorMultipleWatchers(t *testing.T) {
	target := NewProcess()
	w1 := NewProcess()
	defer w1.Close()
	w2 := NewProcess()
	defer w2.Close()

	target.Monitor(w1)
	target.Monitor(w2)
	target.Close()

	_, ok1 := w1.ReceiveTimeout(time.Second)
	_, ok2 := w2.ReceiveTimeout(time.Second)
	if !ok1 {
		t.Error("watcher 1 should receive DOWN")
	}
	if !ok2 {
		t.Error("watcher 2 should receive DOWN")
	}
}

// ---------------------------------------------------------------------------
// Await / AwaitTimeout
// ---------------------------------------------------------------------------

func TestAwait(t *testing.T) {
	p := NewProcess()
	p.result = core.IntVal(99)

	go func() {
		time.Sleep(10 * time.Millisecond)
		p.Close()
	}()

	val := p.Await()
	if !val.Equal(core.IntVal(99)) {
		t.Errorf("Await = %s, want 99", val.Inspect())
	}
}

func TestAwaitAlreadyClosed(t *testing.T) {
	p := NewProcess()
	p.result = core.StringVal("done")
	p.Close()

	done := make(chan core.Value, 1)
	go func() { done <- p.Await() }()

	select {
	case v := <-done:
		if v.Str != "done" {
			t.Errorf("got %s, want done", v.Inspect())
		}
	case <-time.After(time.Second):
		t.Fatal("Await on closed process should not block")
	}
}

func TestAwaitTimeoutSuccess(t *testing.T) {
	p := NewProcess()
	p.result = core.IntVal(7)

	go func() {
		time.Sleep(10 * time.Millisecond)
		p.Close()
	}()

	val, ok := p.AwaitTimeout(time.Second)
	if !ok {
		t.Fatal("should have completed before timeout")
	}
	if !val.Equal(core.IntVal(7)) {
		t.Errorf("got %s, want 7", val.Inspect())
	}
}

func TestAwaitTimeoutExpires(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	val, ok := p.AwaitTimeout(10 * time.Millisecond)
	if ok {
		t.Fatalf("should have timed out, got %s", val.Inspect())
	}
}

// ---------------------------------------------------------------------------
// Supervisor: basic lifecycle
// ---------------------------------------------------------------------------

func TestSupervisorFindChild(t *testing.T) {
	sup := NewSupervisor(core.AtomVal("one_for_one"))
	sup.children = []SupervisorChild{
		{Name: "a", alive: true},
		{Name: "b", alive: true},
		{Name: "c", alive: true},
	}

	if idx := sup.findChild("b"); idx != 1 {
		t.Errorf("findChild(b) = %d, want 1", idx)
	}
	if idx := sup.findChild("z"); idx != -1 {
		t.Errorf("findChild(z) = %d, want -1", idx)
	}
}

func TestSupervisorMarkDeadAndAllDead(t *testing.T) {
	sup := NewSupervisor(core.AtomVal("one_for_one"))
	sup.children = []SupervisorChild{
		{Name: "a", alive: true},
		{Name: "b", alive: true},
	}

	if sup.allDead() {
		t.Error("should not be all dead yet")
	}

	sup.markDead("a")
	if sup.allDead() {
		t.Error("only a is dead, not all")
	}

	sup.markDead("b")
	if !sup.allDead() {
		t.Error("both should be dead now")
	}
}

func TestSupervisorAllDeadEmpty(t *testing.T) {
	sup := NewSupervisor(core.AtomVal("one_for_one"))
	if !sup.allDead() {
		t.Error("empty supervisor should report allDead")
	}
}

func TestSupervisorStop(t *testing.T) {
	sup := NewSupervisor(core.AtomVal("one_for_one"))

	go sup.Run()

	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		sup.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop should not hang")
	}
}

// ---------------------------------------------------------------------------
// Concurrency: link + monitor don't race
// ---------------------------------------------------------------------------

func TestLinkMonitorConcurrentClose(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := NewProcess()
			b := NewProcess()
			w := NewProcess()
			a.Link(b)
			a.Monitor(w)
			b.Monitor(w)
			go a.CloseWithReason(core.AtomVal("test"))
			go b.Close()
			time.Sleep(5 * time.Millisecond)
			w.Close()
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent link/monitor test hung")
	}
}

func TestMonitorCounterIncreases(t *testing.T) {
	target := NewProcess()
	defer target.Close()
	w := NewProcess()
	defer w.Close()

	before := atomic.LoadInt64(&monitorCounter)
	target.Monitor(w)
	after := atomic.LoadInt64(&monitorCounter)
	if after <= before {
		t.Errorf("monitor counter didn't increase: %d -> %d", before, after)
	}
}
