package eval

import (
	"fmt"
	"time"

	"ish/internal/ast"
	"ish/internal/core"
	"ish/internal/process"
)

func spawnProcess(node *ast.Node, scope core.Scope) (*process.Process, error) {
	proc := process.NewProcess()
	childEnv := core.CopyEnv(scope.NearestEnv())
	childEnv.Proc = proc
	child := node.Children[0]

	go func() {
		defer func() {
			if r := recover(); r != nil {
				proc.SetResult(core.Nil)
				proc.CloseWithReason(core.TupleVal(core.AtomVal("error"), core.StringVal(fmt.Sprintf("%v", r))))
			} else {
				proc.Close()
			}
		}()

		val, err := Eval(child, childEnv)
		if err != nil {
			proc.SetResult(core.Nil)
			proc.CloseWithReason(core.TupleVal(core.AtomVal("error"), core.StringVal(err.Error())))
			return
		}
		if val.Kind == core.VFn && val.GetFn() != nil {
			result, err := CallFn(val.GetFn(), nil, childEnv)
			proc.SetResult(result)
			if err != nil {
				proc.CloseWithReason(core.TupleVal(core.AtomVal("error"), core.StringVal(err.Error())))
				return
			}
		} else {
			proc.SetResult(val)
		}
	}()

	return proc, nil
}

func evalIshSpawn(node *ast.Node, scope core.Scope) (core.Value, error) {
	proc, err := spawnProcess(node, scope)
	if err != nil {
		return core.Nil, err
	}
	return core.PidVal(proc), nil
}

func evalIshSpawnLink(node *ast.Node, scope core.Scope) (core.Value, error) {
	proc, err := spawnProcess(node, scope)
	if err != nil {
		return core.Nil, err
	}
	parentProc := scope.NearestEnv().GetProc()
	if parentProc != nil {
		parentProc.Link(proc)
	}
	return core.PidVal(proc), nil
}

func evalIshSend(node *ast.Node, scope core.Scope) (core.Value, error) {
	target, err := Eval(node.Children[0], scope)
	if err != nil {
		return core.Nil, err
	}
	msg, err := Eval(node.Children[1], scope)
	if err != nil {
		return core.Nil, err
	}

	if target.Kind != core.VPid || target.GetPid() == nil {
		return core.Nil, fmt.Errorf("send: first argument must be a pid, got %s", target.Inspect())
	}
	target.GetPid().Send(msg)
	return msg, nil
}

func evalIshReceive(node *ast.Node, scope core.Scope) (core.Value, error) {
	proc := scope.NearestEnv().GetProc()
	if proc == nil {
		return core.Nil, fmt.Errorf("receive: not in a process")
	}

	matchFn := func(msg core.Value) bool {
		for _, clause := range node.Clauses {
			if TryBind(clause.Pattern, msg, nil) {
				return true
			}
		}
		return false
	}

	var msg core.Value
	var ok bool
	if node.Timeout != nil {
		timeoutVal, err := Eval(node.Timeout, scope)
		if err != nil {
			return core.Nil, err
		}
		if timeoutVal.Kind != core.VInt {
			return core.Nil, fmt.Errorf("receive: timeout must be an integer (milliseconds), got %s", timeoutVal.Inspect())
		}
		ms := timeoutVal.GetInt()
		msg, ok = proc.ReceiveSelectiveTimeout(matchFn, time.Duration(ms)*time.Millisecond)
		if !ok {
			if node.TimeoutBody != nil {
				return Eval(node.TimeoutBody, scope)
			}
			return core.Nil, nil
		}
	} else {
		msg, ok = proc.ReceiveSelective(matchFn)
		if !ok {
			return core.Nil, nil
		}
	}

	for _, clause := range node.Clauses {
		matchEnv := core.NewEnv(scope)
		if TryBind(clause.Pattern, msg, matchEnv) {
			return Eval(clause.Body, matchEnv)
		}
	}
	return core.Nil, fmt.Errorf("no matching receive clause for %s", msg.Inspect())
}

func evalIshMonitor(node *ast.Node, scope core.Scope) (core.Value, error) {
	target, err := Eval(node.Children[0], scope)
	if err != nil {
		return core.Nil, err
	}
	if target.Kind != core.VPid || target.GetPid() == nil {
		return core.Nil, fmt.Errorf("monitor: expected pid, got %s", target.Inspect())
	}
	watcher := scope.NearestEnv().GetProc()
	if watcher == nil {
		return core.Nil, fmt.Errorf("monitor: not in a process")
	}
	ref := target.GetPid().Monitor(watcher)
	return core.IntVal(ref), nil
}

func evalIshAwait(node *ast.Node, scope core.Scope) (core.Value, error) {
	target, err := Eval(node.Children[0], scope)
	if err != nil {
		return core.Nil, err
	}
	if target.Kind != core.VPid || target.GetPid() == nil {
		return core.Nil, fmt.Errorf("await: expected pid, got %s", target.Inspect())
	}
	result := target.GetPid().Await()
	return result, nil
}

func evalIshSupervise(node *ast.Node, scope core.Scope) (core.Value, error) {
	strategy, err := Eval(node.Children[0], scope)
	if err != nil {
		return core.Nil, err
	}

	sup := process.NewSupervisor(strategy)

	for _, workerNode := range node.Children[1:] {
		if workerNode.Kind == ast.NCmd && len(workerNode.Children) == 2 {
			nameVal, err := Eval(workerNode.Children[0], scope)
			if err != nil {
				return core.Nil, err
			}
			fnVal, err := Eval(workerNode.Children[1], scope)
			if err != nil {
				return core.Nil, err
			}
			if fnVal.Kind == core.VFn && fnVal.GetFn() != nil {
				sup.AddChild(nameVal.ToStr(), fnVal.GetFn(), core.CopyEnv(scope.NearestEnv()))
			} else {
				return core.Nil, fmt.Errorf("supervise: worker %s is not a function", nameVal.ToStr())
			}
		}
	}

	go sup.Run()

	return core.PidVal(sup.Proc), nil
}

func evalIshTry(node *ast.Node, scope core.Scope) (core.Value, error) {
	val, err := Eval(node.Children[0], scope)
	if err == nil {
		return val, nil
	}
	if err == core.ErrReturn || err == core.ErrBreak || err == core.ErrContinue || err == core.ErrSetE {
		return val, err
	}
	errVal := core.TupleVal(core.AtomVal("error"), core.StringVal(err.Error()))

	for _, clause := range node.Clauses {
		matchEnv := core.NewEnv(scope)
		if TryBind(clause.Pattern, errVal, matchEnv) {
			return Eval(clause.Body, matchEnv)
		}
	}
	return core.Nil, err
}
