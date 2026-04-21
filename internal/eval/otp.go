package eval

import (
	"fmt"
	"time"

	"ish/internal/ast"
	"ish/internal/core"
	"ish/internal/process"
)

func evalLambda(node *ast.Node, env *core.Env) (core.Value, error) {
	var params []ast.Node
	for _, child := range node.Children {
		params = append(params, *child)
	}
	fnVal := &core.FnValue{
		Name: "<lambda>",
		Env:  env,
		Clauses: []core.FnClause{{
			Params: params,
			Body:   node.Clauses[0].Body,
		}},
	}
	return core.Value{Kind: core.VFn, Fn: fnVal}, nil
}

func evalIshFn(node *ast.Node, env *core.Env) (core.Value, error) {
	name := node.Tok.Val

	if len(node.Children) == 0 && len(node.Clauses) > 0 && node.Clauses[0].Pattern != nil {
		var fnClauses []core.FnClause
		for _, clause := range node.Clauses {
			var params []ast.Node
			if clause.Pattern != nil {
				if clause.Pattern.Kind == ast.NBlock {
					for _, child := range clause.Pattern.Children {
						params = append(params, *child)
					}
				} else {
					params = append(params, *clause.Pattern)
				}
			}
			fnClauses = append(fnClauses, core.FnClause{
				Params: params,
				Guard:  clause.Guard,
				Body:   clause.Body,
			})
		}
		fnVal := &core.FnValue{Name: name, Clauses: fnClauses, Env: env}
		if name == "<anon>" {
			return core.Value{Kind: core.VFn, Fn: fnVal}, nil
		}
		// Arrow-clause form provides a complete dispatch table — replace.
		env.SetFnClauses(name, fnVal)
		return core.Nil, nil
	}

	var params []ast.Node
	for _, child := range node.Children {
		params = append(params, *child)
	}

	clause := core.FnClause{
		Params: params,
		Guard:  node.Clauses[0].Guard,
		Body:   node.Clauses[0].Body,
	}

	fnVal := &core.FnValue{Name: name, Clauses: []core.FnClause{clause}, Env: env}

	if name == "<anon>" {
		return core.Value{Kind: core.VFn, Fn: fnVal}, nil
	}

	env.AddFnClauses(name, fnVal)
	return core.Nil, nil
}

func evalIshMatch(node *ast.Node, env *core.Env) (core.Value, error) {
	subject, err := Eval(node.Children[0], env)
	if err != nil {
		return core.Nil, err
	}

	for _, clause := range node.Clauses {
		if PatternMatches(clause.Pattern, subject, env) {
			matchEnv := core.NewEnv(env)
			PatternBind(clause.Pattern, subject, matchEnv)
			return Eval(clause.Body, matchEnv)
		}
	}
	return core.Nil, fmt.Errorf("no matching clause for %s", subject.Inspect())
}

func spawnProcess(node *ast.Node, env *core.Env) (*process.Process, error) {
	proc := process.NewProcess()
	childEnv := core.CopyEnv(env)
	childEnv.Shell.Proc = proc
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
		if val.Kind == core.VFn && val.Fn != nil {
			result, err := CallFn(val.Fn, nil, childEnv)
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

func evalIshSpawn(node *ast.Node, env *core.Env) (core.Value, error) {
	proc, err := spawnProcess(node, env)
	if err != nil {
		return core.Nil, err
	}
	return core.Value{Kind: core.VPid, Pid: proc}, nil
}

func evalIshSpawnLink(node *ast.Node, env *core.Env) (core.Value, error) {
	proc, err := spawnProcess(node, env)
	if err != nil {
		return core.Nil, err
	}
	parentProc := env.GetProc()
	if parentProc != nil {
		parentProc.Link(proc)
	}
	return core.Value{Kind: core.VPid, Pid: proc}, nil
}

func evalIshSend(node *ast.Node, env *core.Env) (core.Value, error) {
	target, err := Eval(node.Children[0], env)
	if err != nil {
		return core.Nil, err
	}
	msg, err := Eval(node.Children[1], env)
	if err != nil {
		return core.Nil, err
	}

	if target.Kind != core.VPid || target.Pid == nil {
		return core.Nil, fmt.Errorf("send: first argument must be a pid, got %s", target.Inspect())
	}
	target.Pid.Send(msg)
	return msg, nil
}

func evalIshReceive(node *ast.Node, env *core.Env) (core.Value, error) {
	proc := env.GetProc()
	if proc == nil {
		return core.Nil, fmt.Errorf("receive: not in a process")
	}

	matchFn := func(msg core.Value) bool {
		for _, clause := range node.Clauses {
			if PatternMatches(clause.Pattern, msg, env) {
				return true
			}
		}
		return false
	}

	var msg core.Value
	var ok bool
	if node.Timeout != nil {
		timeoutVal, err := Eval(node.Timeout, env)
		if err != nil {
			return core.Nil, err
		}
		if timeoutVal.Kind != core.VInt {
			return core.Nil, fmt.Errorf("receive: timeout must be an integer (milliseconds), got %s", timeoutVal.Inspect())
		}
		ms := timeoutVal.Int
		msg, ok = proc.ReceiveSelectiveTimeout(matchFn, time.Duration(ms)*time.Millisecond)
		if !ok {
			if node.TimeoutBody != nil {
				return Eval(node.TimeoutBody, env)
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
		if PatternMatches(clause.Pattern, msg, env) {
			matchEnv := core.NewEnv(env)
			PatternBind(clause.Pattern, msg, matchEnv)
			return Eval(clause.Body, matchEnv)
		}
	}
	return core.Nil, fmt.Errorf("no matching receive clause for %s", msg.Inspect())
}

func evalIshMonitor(node *ast.Node, env *core.Env) (core.Value, error) {
	target, err := Eval(node.Children[0], env)
	if err != nil {
		return core.Nil, err
	}
	if target.Kind != core.VPid || target.Pid == nil {
		return core.Nil, fmt.Errorf("monitor: expected pid, got %s", target.Inspect())
	}
	watcher := env.GetProc()
	if watcher == nil {
		return core.Nil, fmt.Errorf("monitor: not in a process")
	}
	ref := target.Pid.Monitor(watcher)
	return core.IntVal(ref), nil
}

func evalIshAwait(node *ast.Node, env *core.Env) (core.Value, error) {
	target, err := Eval(node.Children[0], env)
	if err != nil {
		return core.Nil, err
	}
	if target.Kind != core.VPid || target.Pid == nil {
		return core.Nil, fmt.Errorf("await: expected pid, got %s", target.Inspect())
	}
	result := target.Pid.Await()
	return result, nil
}

func evalIshSupervise(node *ast.Node, env *core.Env) (core.Value, error) {
	strategy, err := Eval(node.Children[0], env)
	if err != nil {
		return core.Nil, err
	}

	sup := process.NewSupervisor(strategy)

	for _, workerNode := range node.Children[1:] {
		if workerNode.Kind == ast.NCmd && len(workerNode.Children) == 2 {
			nameVal, err := Eval(workerNode.Children[0], env)
			if err != nil {
				return core.Nil, err
			}
			fnVal, err := Eval(workerNode.Children[1], env)
			if err != nil {
				return core.Nil, err
			}
			if fnVal.Kind == core.VFn && fnVal.Fn != nil {
				sup.AddChild(nameVal.ToStr(), fnVal.Fn, core.CopyEnv(env))
			} else {
				return core.Nil, fmt.Errorf("supervise: worker %s is not a function", nameVal.ToStr())
			}
		}
	}

	go sup.Run()

	return core.Value{Kind: core.VPid, Pid: sup.Proc}, nil
}

func evalIshTry(node *ast.Node, env *core.Env) (core.Value, error) {
	val, err := Eval(node.Children[0], env)
	if err == nil {
		return val, nil
	}
	if err == core.ErrReturn || err == core.ErrBreak || err == core.ErrContinue || err == core.ErrSetE {
		return val, err
	}
	errVal := core.TupleVal(core.AtomVal("error"), core.StringVal(err.Error()))

	for _, clause := range node.Clauses {
		if PatternMatches(clause.Pattern, errVal, env) {
			matchEnv := core.NewEnv(env)
			PatternBind(clause.Pattern, errVal, matchEnv)
			return Eval(clause.Body, matchEnv)
		}
	}
	return core.Nil, err
}
