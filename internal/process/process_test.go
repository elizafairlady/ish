package process

import (
	"sync"
	"testing"
	"time"

	"ish/internal/core"
)

func TestNewProcessUniqueIDs(t *testing.T) {
	p1 := NewProcess()
	p2 := NewProcess()
	p3 := NewProcess()
	defer p1.Close()
	defer p2.Close()
	defer p3.Close()

	if p1.id == p2.id || p2.id == p3.id || p1.id == p3.id {
		t.Errorf("expected unique IDs, got %d, %d, %d", p1.id, p2.id, p3.id)
	}
}

func TestNewProcessIncrementsID(t *testing.T) {
	p1 := NewProcess()
	p2 := NewProcess()
	defer p1.Close()
	defer p2.Close()

	if p2.id <= p1.id {
		t.Errorf("expected p2.id > p1.id, got %d <= %d", p2.id, p1.id)
	}
}

func TestFindProcess(t *testing.T) {
	t.Run("finds existing", func(t *testing.T) {
		p := NewProcess()
		defer p.Close()

		found := FindProcess(p.id)
		if found == nil {
			t.Fatal("expected to find process")
		}
		if found.id != p.id {
			t.Errorf("found wrong process: got id %d, want %d", found.id, p.id)
		}
	})

	t.Run("returns nil for unknown", func(t *testing.T) {
		found := FindProcess(-999)
		if found != nil {
			t.Error("expected nil for unknown process ID")
		}
	})

	t.Run("returns nil after close", func(t *testing.T) {
		p := NewProcess()
		id := p.id
		p.Close()

		found := FindProcess(id)
		if found != nil {
			t.Error("expected nil after process closed")
		}
	})
}

func TestSendReceive(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	msg := core.AtomVal("hello")
	p.Send(msg)

	got := p.Receive()
	if !got.Equal(msg) {
		t.Errorf("Receive() = %s, want %s", got.Inspect(), msg.Inspect())
	}
}

func TestSendReceiveMultiple(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	msgs := []core.Value{core.IntVal(1), core.IntVal(2), core.IntVal(3)}
	for _, m := range msgs {
		p.Send(m)
	}
	for _, want := range msgs {
		got := p.Receive()
		if !got.Equal(want) {
			t.Errorf("Receive() = %s, want %s", got.Inspect(), want.Inspect())
		}
	}
}

func TestSendReceiveOrdering(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	p.Send(core.StringVal("first"))
	p.Send(core.StringVal("second"))

	got1 := p.Receive()
	got2 := p.Receive()

	if got1.Str != "first" {
		t.Errorf("expected first message first, got %q", got1.Str)
	}
	if got2.Str != "second" {
		t.Errorf("expected second message second, got %q", got2.Str)
	}
}

func TestCloseIdempotent(t *testing.T) {
	p := NewProcess()

	// Should not panic on double close
	p.Close()
	p.Close()
	p.Close()
}

func TestReceiveOnClosed(t *testing.T) {
	p := NewProcess()
	p.Close()

	done := make(chan core.Value, 1)
	go func() {
		done <- p.Receive()
	}()

	select {
	case v := <-done:
		if v.Kind != core.VNil {
			t.Errorf("expected Nil from closed process, got %s", v.Inspect())
		}
	case <-time.After(time.Second):
		t.Fatal("Receive on closed process should not block")
	}
}

func TestSendToClosedDoesNotBlock(t *testing.T) {
	p := NewProcess()
	p.Close()

	done := make(chan struct{})
	go func() {
		p.Send(core.StringVal("hello"))
		close(done)
	}()

	select {
	case <-done:
		// success - didn't block
	case <-time.After(time.Second):
		t.Fatal("Send to closed process should not block")
	}
}

func TestProcessRegistration(t *testing.T) {
	p := NewProcess()
	if FindProcess(p.id) == nil {
		t.Error("new process should be in registry")
	}

	p.Close()
	if FindProcess(p.id) != nil {
		t.Error("closed process should be removed from registry")
	}
}

func TestCloseWithReasonRace(t *testing.T) {
	for i := 0; i < 100; i++ {
		p := NewProcess()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			p.CloseWithReason(core.AtomVal("reason_a"))
		}()
		go func() {
			defer wg.Done()
			p.CloseWithReason(core.AtomVal("reason_b"))
		}()
		wg.Wait()
		r := p.reason
		if r.Kind != core.VAtom || (r.Str != "reason_a" && r.Str != "reason_b" && r.Str != "normal") {
			t.Errorf("unexpected reason: %s", r.Inspect())
		}
	}
}

func TestSupervisorRestartBackoff(t *testing.T) {
	sup := NewSupervisor(core.AtomVal("one_for_one"))
	sup.maxRestarts = 2
	sup.maxSeconds = 5

	if sup.tooManyRestarts() {
		t.Error("first restart should be allowed")
	}

	if sup.tooManyRestarts() {
		t.Error("second restart should be allowed")
	}

	if !sup.tooManyRestarts() {
		t.Error("third restart should exceed limit (maxRestarts=2)")
	}
}

func TestSelectiveReceive(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	p.Send(core.AtomVal("b"))
	p.Send(core.AtomVal("a"))
	p.Send(core.AtomVal("c"))

	msg, ok := p.ReceiveSelective(func(v core.Value) bool {
		return v.Kind == core.VAtom && v.Str == "a"
	})
	if !ok || !msg.Equal(core.AtomVal("a")) {
		t.Errorf("expected :a, got %s", msg.Inspect())
	}

	msg, ok = p.ReceiveSelective(func(v core.Value) bool {
		return v.Kind == core.VAtom && v.Str == "b"
	})
	if !ok || !msg.Equal(core.AtomVal("b")) {
		t.Errorf("expected :b from save queue, got %s", msg.Inspect())
	}

	msg, ok = p.ReceiveSelective(func(v core.Value) bool { return true })
	if !ok || !msg.Equal(core.AtomVal("c")) {
		t.Errorf("expected :c, got %s", msg.Inspect())
	}
}

func TestSelectiveReceiveSaveQueueOrdering(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	p.Send(core.AtomVal("x"))
	p.Send(core.AtomVal("y"))
	p.Send(core.AtomVal("z"))

	msg, ok := p.ReceiveSelective(func(v core.Value) bool {
		return v.Kind == core.VAtom && v.Str == "z"
	})
	if !ok || !msg.Equal(core.AtomVal("z")) {
		t.Fatalf("expected :z, got %s", msg.Inspect())
	}

	msg, ok = p.ReceiveSelective(func(v core.Value) bool { return true })
	if !ok || !msg.Equal(core.AtomVal("x")) {
		t.Errorf("expected :x from save queue, got %s", msg.Inspect())
	}
	msg, ok = p.ReceiveSelective(func(v core.Value) bool { return true })
	if !ok || !msg.Equal(core.AtomVal("y")) {
		t.Errorf("expected :y from save queue, got %s", msg.Inspect())
	}
}

func TestSelectiveReceiveTimeout(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	p.Send(core.AtomVal("b"))

	msg, ok := p.ReceiveSelectiveTimeout(func(v core.Value) bool {
		return v.Kind == core.VAtom && v.Str == "a"
	}, 50*time.Millisecond)
	if ok {
		t.Errorf("expected timeout, got %s", msg.Inspect())
	}

	msg, ok = p.ReceiveSelective(func(v core.Value) bool { return true })
	if !ok || !msg.Equal(core.AtomVal("b")) {
		t.Errorf("expected :b preserved in save queue, got %s (ok=%v)", msg.Inspect(), ok)
	}
}

func TestSelectiveReceiveOnClosedProcess(t *testing.T) {
	p := NewProcess()
	p.Close()

	done := make(chan struct{})
	go func() {
		_, ok := p.ReceiveSelective(func(v core.Value) bool { return true })
		if ok {
			t.Error("expected ok=false from closed process")
		}
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(time.Second):
		t.Fatal("ReceiveSelective on closed process should not block")
	}
}

func TestSaveQueueBounded(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	for i := 0; i < maxSaveQueueSize-1; i++ {
		p.saveQueue = append(p.saveQueue, core.IntVal(int64(i)))
	}

	for i := 0; i < 100; i++ {
		p.Send(core.IntVal(int64(maxSaveQueueSize + i)))
	}

	_, ok := p.ReceiveSelectiveTimeout(func(v core.Value) bool {
		return v.Kind == core.VAtom && v.Str == "target"
	}, 50*time.Millisecond)
	if ok {
		t.Fatal("expected timeout")
	}

	if len(p.saveQueue) > maxSaveQueueSize {
		t.Errorf("saveQueue size %d exceeds max %d", len(p.saveQueue), maxSaveQueueSize)
	}
}

func TestCloseWithReasonConcurrent(t *testing.T) {
	for i := 0; i < 100; i++ {
		p := NewProcess()
		reasons := []core.Value{
			core.AtomVal("crash"),
			core.AtomVal("shutdown"),
			core.AtomVal("timeout"),
		}
		var wg sync.WaitGroup
		wg.Add(len(reasons))
		for _, r := range reasons {
			go func(reason core.Value) {
				defer wg.Done()
				p.CloseWithReason(reason)
			}(r)
		}
		wg.Wait()

		got := p.reason
		valid := false
		for _, r := range reasons {
			if got.Equal(r) {
				valid = true
				break
			}
		}
		if !valid {
			t.Errorf("iteration %d: unexpected reason %s", i, got.Inspect())
		}
	}
}
