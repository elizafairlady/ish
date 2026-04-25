package eval

import (
	"sync"
	"sync/atomic"
	"time"

	"ish/internal/value"
)

var pidCounter int64
var procRegistry sync.Map

type Process struct {
	id        int64
	mailbox   chan value.Value
	done      chan struct{}
	closeOnce sync.Once
	result    value.Value
	err       error          // non-nil if the process crashed
	saveQueue []value.Value  // non-matching messages saved for selective receive
	monitors  []int64        // pids monitoring this process
	monMu     sync.Mutex
}

func newProcess() *Process {
	p := &Process{
		id:      atomic.AddInt64(&pidCounter, 1),
		mailbox: make(chan value.Value, 256),
		done:    make(chan struct{}),
	}
	procRegistry.Store(p.id, p)
	return p
}

func findProcess(id int64) *Process {
	if v, ok := procRegistry.Load(id); ok {
		return v.(*Process)
	}
	return nil
}

func (p *Process) send(msg value.Value) {
	select {
	case p.mailbox <- msg:
	case <-p.done:
	}
}

func (p *Process) receive(match func(value.Value) bool) (value.Value, bool) {
	// Check save queue first
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
		case <-p.done:
			return value.Nil, false
		}
	}
}

func (p *Process) await() value.Value {
	<-p.done
	return p.result
}

func (p *Process) receiveTimeout(match func(value.Value) bool, d time.Duration) (value.Value, bool) {
	// Check save queue first
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
		case <-p.done:
			return value.Nil, false
		case <-timer.C:
			return value.Nil, false
		}
	}
}

func (p *Process) monitor(watcherPid int64) {
	// Check if already done
	select {
	case <-p.done:
		// Already dead — send DOWN immediately
		if watcher := findProcess(watcherPid); watcher != nil {
			watcher.send(value.TupleVal(value.AtomVal("DOWN"), value.PidVal(p.id), value.AtomVal("normal")))
		}
		return
	default:
	}
	p.monMu.Lock()
	p.monitors = append(p.monitors, watcherPid)
	p.monMu.Unlock()
}

func (p *Process) close(result value.Value) {
	p.closeOnce.Do(func() {
		p.result = result
		close(p.done)
		// Send :DOWN to all monitors
		p.monMu.Lock()
		monitors := append([]int64{}, p.monitors...)
		p.monMu.Unlock()
		down := value.TupleVal(value.AtomVal("DOWN"), value.PidVal(p.id), value.AtomVal("normal"))
		for _, mid := range monitors {
			if watcher := findProcess(mid); watcher != nil {
				watcher.send(down)
			}
		}
		// Don't delete from registry — await/monitor may still need to find us
	})
}
