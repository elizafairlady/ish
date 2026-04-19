package core

import "time"

// Pid is the interface for process-like entities (breaks the value <-> process import cycle).
type Pid interface {
	ID() int64
	Send(msg Value)
	Receive() Value
	ReceiveSelective(match func(Value) bool) (Value, bool)
	ReceiveSelectiveTimeout(match func(Value) bool, d time.Duration) (Value, bool)
	Await() Value
	Monitor(watcher Pid) int64
	Link(other Pid)
	Close()
	CloseWithReason(reason Value)
	SetResult(v Value)
}

// FindPid looks up a process by id. Set by the process package during init.
var FindPid func(id int64) Pid
