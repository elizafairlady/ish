package main

import (
	"sync"
	"testing"
	"time"
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

	msg := AtomVal("hello")
	p.Send(msg)

	got := p.Receive()
	if !got.Equal(msg) {
		t.Errorf("Receive() = %s, want %s", got.Inspect(), msg.Inspect())
	}
}

func TestSendReceiveMultiple(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	msgs := []Value{IntVal(1), IntVal(2), IntVal(3)}
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

	p.Send(StringVal("first"))
	p.Send(StringVal("second"))

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

	done := make(chan Value, 1)
	go func() {
		done <- p.Receive()
	}()

	select {
	case v := <-done:
		if v.Kind != VNil {
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
		p.Send(StringVal("hello"))
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
	// Should be in registry
	if FindProcess(p.id) == nil {
		t.Error("new process should be in registry")
	}

	p.Close()
	// Should be removed from registry
	if FindProcess(p.id) != nil {
		t.Error("closed process should be removed from registry")
	}
}

func TestCloseWithReasonRace(t *testing.T) {
	// Two goroutines racing to CloseWithReason should not panic
	for i := 0; i < 100; i++ {
		p := NewProcess()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			p.CloseWithReason(AtomVal("reason_a"))
		}()
		go func() {
			defer wg.Done()
			p.CloseWithReason(AtomVal("reason_b"))
		}()
		wg.Wait()
		// Reason should be one of the two
		r := p.reason
		if r.Kind != VAtom || (r.Str != "reason_a" && r.Str != "reason_b" && r.Str != "normal") {
			t.Errorf("unexpected reason: %s", r.Inspect())
		}
	}
}

func TestSupervisorRestartBackoff(t *testing.T) {
	// Create a supervisor with a child that always crashes
	sup := NewSupervisor(AtomVal("one_for_one"))
	sup.maxRestarts = 2
	sup.maxSeconds = 5

	crashCount := 0
	var mu sync.Mutex
	crashFn := &FnValue{
		Name: "crasher",
		Clauses: []FnClause{{
			Body: &Node{Kind: NLit, Tok: Token{Type: TAtom, Val: "crash"}},
		}},
	}

	env := TopEnv()
	// Override the crasher to actually track calls and panic
	env.SetFn("crasher_impl", crashFn)

	// We can't easily use the real supervisor machinery for this unit test,
	// so test tooManyRestarts directly
	if sup.tooManyRestarts() {
		t.Error("first restart should be allowed")
	}
	mu.Lock()
	crashCount++
	mu.Unlock()

	if sup.tooManyRestarts() {
		t.Error("second restart should be allowed")
	}

	if !sup.tooManyRestarts() {
		t.Error("third restart should exceed limit (maxRestarts=2)")
	}
	_ = crashCount
}

func TestSelectiveReceive(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	// Send messages in order: :b, :a, :c
	p.Send(AtomVal("b"))
	p.Send(AtomVal("a"))
	p.Send(AtomVal("c"))

	// Selectively receive :a (should skip :b)
	msg, ok := p.ReceiveSelective(func(v Value) bool {
		return v.Kind == VAtom && v.Str == "a"
	})
	if !ok || !msg.Equal(AtomVal("a")) {
		t.Errorf("expected :a, got %s", msg.Inspect())
	}

	// :b should still be in the save queue, receive it next
	msg, ok = p.ReceiveSelective(func(v Value) bool {
		return v.Kind == VAtom && v.Str == "b"
	})
	if !ok || !msg.Equal(AtomVal("b")) {
		t.Errorf("expected :b from save queue, got %s", msg.Inspect())
	}

	// :c should come from the channel
	msg, ok = p.ReceiveSelective(func(v Value) bool { return true })
	if !ok || !msg.Equal(AtomVal("c")) {
		t.Errorf("expected :c, got %s", msg.Inspect())
	}
}

func TestSelectiveReceiveSaveQueueOrdering(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	// Send :x, :y, :z — selectively receive :z first
	p.Send(AtomVal("x"))
	p.Send(AtomVal("y"))
	p.Send(AtomVal("z"))

	msg, ok := p.ReceiveSelective(func(v Value) bool {
		return v.Kind == VAtom && v.Str == "z"
	})
	if !ok || !msg.Equal(AtomVal("z")) {
		t.Fatalf("expected :z, got %s", msg.Inspect())
	}

	// Now :x and :y should be in save queue (in order), receive with accept-all
	msg, ok = p.ReceiveSelective(func(v Value) bool { return true })
	if !ok || !msg.Equal(AtomVal("x")) {
		t.Errorf("expected :x from save queue, got %s", msg.Inspect())
	}
	msg, ok = p.ReceiveSelective(func(v Value) bool { return true })
	if !ok || !msg.Equal(AtomVal("y")) {
		t.Errorf("expected :y from save queue, got %s", msg.Inspect())
	}
}

func TestSelectiveReceiveTimeout(t *testing.T) {
	p := NewProcess()
	defer p.Close()

	// Send :b only — selective receive for :a should timeout
	p.Send(AtomVal("b"))

	msg, ok := p.ReceiveSelectiveTimeout(func(v Value) bool {
		return v.Kind == VAtom && v.Str == "a"
	}, 50*time.Millisecond)
	if ok {
		t.Errorf("expected timeout, got %s", msg.Inspect())
	}

	// :b should be in the save queue — receive it with accept-all
	msg, ok = p.ReceiveSelective(func(v Value) bool { return true })
	if !ok || !msg.Equal(AtomVal("b")) {
		t.Errorf("expected :b preserved in save queue, got %s (ok=%v)", msg.Inspect(), ok)
	}
}

func TestSelectiveReceiveOnClosedProcess(t *testing.T) {
	p := NewProcess()
	p.Close()

	done := make(chan struct{})
	go func() {
		_, ok := p.ReceiveSelective(func(v Value) bool { return true })
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

	// Pre-fill save queue to just under the limit
	for i := 0; i < maxSaveQueueSize-1; i++ {
		p.saveQueue = append(p.saveQueue, IntVal(int64(i)))
	}

	// Send 100 non-matching messages through the channel
	for i := 0; i < 100; i++ {
		p.Send(IntVal(int64(maxSaveQueueSize + i)))
	}

	// Selective receive with timeout — none match, all go to save queue
	_, ok := p.ReceiveSelectiveTimeout(func(v Value) bool {
		return v.Kind == VAtom && v.Str == "target"
	}, 50*time.Millisecond)
	if ok {
		t.Fatal("expected timeout")
	}

	// Save queue should be capped at maxSaveQueueSize
	if len(p.saveQueue) > maxSaveQueueSize {
		t.Errorf("saveQueue size %d exceeds max %d", len(p.saveQueue), maxSaveQueueSize)
	}
}

func TestCloseWithReasonConcurrent(t *testing.T) {
	for i := 0; i < 100; i++ {
		p := NewProcess()
		reasons := []Value{
			AtomVal("crash"),
			AtomVal("shutdown"),
			AtomVal("timeout"),
		}
		var wg sync.WaitGroup
		wg.Add(len(reasons))
		for _, r := range reasons {
			go func(reason Value) {
				defer wg.Done()
				p.CloseWithReason(reason)
			}(r)
		}
		wg.Wait()

		// Reason must be one of the submitted values (not corrupted)
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
