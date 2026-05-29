// Package eval interprets expanded core syntax. Inputs are *core.Syntax
// trees produced by the expander; outputs are runtime Values.
package eval

import (
	"fmt"

	"ish/core"
	"ish/expand"
)

// Value is any runtime value. Concrete kinds are core.Datum literals/compounds,
// *core.Closure (user functions), and *core.Native (primitives). It is an alias
// for any so frame maps are interchangeable with core.Env's.
type Value = any

// GoFunc is the host implementation type of a primitive. env carries the
// caller's dynamic state — current process, runtime — so primitives like `self`
// and `send` can reach it. A GoFunc is wrapped in a *core.Native to become a
// first-class value.
type GoFunc = func(args []Value, env *Env) (Value, error)

type IdentifierResolver interface {
	FreeIdentifierEqual(a, b *core.Syntax) bool
	BoundIdentifierEqual(a, b *core.Syntax) bool
	SpaceOf(stx *core.Syntax) (expand.SpaceValue, bool)
	ResolveMember(member *core.Syntax, space expand.SpaceValue) (*expand.Binding, expand.ResolveResult)
}

type DynamicValue interface {
	Value(*Env) (Value, error)
}

// Expander gives a macro transformer body access to the use-site expansion
// context: local-expand expands a sub-form, bind! mints a fresh hygienic
// binding identifier. It is non-nil only while a macro body runs (phase 1).
type Expander interface {
	LocalExpand(stx *core.Syntax) (*core.Syntax, error)
	FreshIdentifier(name core.Word) *core.Syntax
}

// Env is the evaluation context. The lexical bindings (lex) are an immutable
// core.Env shared structurally; Process, Runtime, Resolver, and Expander are
// dynamic context threaded through calls (not captured by closures). An Env is
// never mutated: binding produces a new Env via extend.
type Env struct {
	lex      *core.Env
	Process  *Process
	Runtime  *Runtime
	Resolver IdentifierResolver
	Expander Expander
}

func NewEnv() *Env { return &Env{} }

func (e *Env) lookup(id core.BindingID) (Value, bool) {
	if e == nil {
		return nil, false
	}
	return e.lex.Lookup(id)
}

func (e *Env) Lookup(id core.BindingID) (Value, bool) { return e.lookup(id) }

// extend returns a new Env with frame layered over e's lexical bindings,
// carrying the same dynamic context.
func (e *Env) extend(frame map[core.BindingID]Value) *Env {
	return &Env{lex: e.lex.Extend(frame), Process: e.Process, Runtime: e.Runtime, Resolver: e.Resolver, Expander: e.Expander}
}

// withLex returns a new Env using lex for lexical bindings and e's dynamic
// context — used to enter a closure's captured environment at call time.
func (e *Env) withLex(lex *core.Env) *Env {
	return &Env{lex: lex, Process: e.Process, Runtime: e.Runtime, Resolver: e.Resolver, Expander: e.Expander}
}

// truthy is the language's truth predicate for guards and conditionals.
// Only the atom :true is truthy.
func truthy(v Value) bool { return v == core.Atom("true") }

// Eval interprets expanded core syntax, returning the value and the environment
// for the next sibling statement. Only a binding extends the environment (it is
// immutable); a sequence (Begin) threads that environment to its successors and
// every other form returns the environment unchanged.
func Eval(stx *core.Syntax, env *Env) (Value, *Env, error) {
	if stx == nil {
		return core.Nil{}, env, nil
	}
	switch n := stx.Node.(type) {
	case core.Resolved:
		if n.ID == 0 {
			return nil, env, fmt.Errorf("eval: malformed resolved reference")
		}
		if v, ok := env.lookup(n.ID); ok {
			return v, env, nil
		}
		if dyn, ok := n.Value.(DynamicValue); ok {
			v, err := dyn.Value(env)
			return v, env, err
		}
		return n.Value, env, nil
	case core.Lambda:
		return &core.Closure{Clauses: n.Clauses, Env: env.lex}, env, nil
	case core.Begin:
		cur := env
		var last Value = core.Nil{}
		for _, e := range n.Body {
			v, next, err := Eval(e, cur)
			if err != nil {
				return nil, env, err
			}
			last, cur = v, next
		}
		return last, cur, nil
	case core.Bind:
		v, _, err := Eval(n.Value, env)
		if err != nil {
			return nil, env, err
		}
		// Match into a fresh frame so a failed match never partially binds, then
		// push it as a new immutable frame for subsequent statements.
		frame := map[core.BindingID]Value{}
		if ok, _ := match(n.Pattern, v, env, frame); !ok {
			return nil, env, fmt.Errorf("bind: pattern did not match")
		}
		return v, env.extend(frame), nil
	case core.LetRec:
		// Tie the recursive knot: create the group's frame and the environment
		// that references it, evaluate each value (a closure) so it captures
		// that environment, then fill the frame. After this the frame is never
		// written again, so the bindings are mutually visible and the
		// environment stays effectively immutable.
		frame := map[core.BindingID]Value{}
		ext := env.extend(frame)
		for _, b := range n.Bindings {
			v, err := EvalExpr(b.Value, ext)
			if err != nil {
				return nil, env, err
			}
			frame[b.Ref.ID] = v
		}
		return core.Nil{}, ext, nil
	case core.Receive:
		v, err := evalReceive(n, env)
		return v, env, err
	case core.SyntaxParse:
		v, err := evalSyntaxParse(n, env)
		return v, env, err
	case core.App:
		v, err := evalApp(n, env)
		return v, env, err
	case *core.Syntax:
		// Emitted by the `syntax` core form: the inner *Syntax is the value.
		return n, env, nil
	case core.SyntaxVector, core.SyntaxTuple, core.SyntaxDict:
		return &core.Syntax{Node: n, Span: stx.Span, Scopes: stx.Scopes, Properties: stx.Properties, Origin: stx.Origin}, env, nil
	case core.Word, core.Int, core.Float, core.String, core.Bytes, core.Atom, core.Nil,
		core.Pair, core.Vector, core.Tuple, core.Dict, core.Tagged, *core.Closure, *core.Native:
		// Quoted data and already-realized values evaluate to themselves.
		return n, env, nil
	}
	return nil, env, fmt.Errorf("eval: unsupported node %T", stx.Node)
}

// EvalExpr evaluates a form whose bindings do not escape (an expression), and
// returns just its value. It is the one-result convenience over Eval used
// everywhere a result is needed but the threaded environment is not.
func EvalExpr(stx *core.Syntax, env *Env) (Value, error) {
	v, _, err := Eval(stx, env)
	return v, err
}

// Native wraps a host implementation as a first-class primitive value.
func Native(name string, fn GoFunc) *core.Native {
	return &core.Native{Name: core.Word(name), Impl: fn}
}

// isCallable reports whether v can be applied as a function.
func isCallable(v Value) bool {
	switch v.(type) {
	case *core.Closure, *core.Native:
		return true
	}
	return false
}

// apply invokes a function value on args using the caller's dynamic context.
func apply(callee Value, args []Value, env *Env) (Value, error) {
	switch f := callee.(type) {
	case *core.Closure:
		return applyClosure(f, args, env)
	case *core.Native:
		impl, ok := f.Impl.(GoFunc)
		if !ok {
			return nil, fmt.Errorf("eval: malformed native %s", f.Name)
		}
		return impl(args, env)
	}
	return nil, fmt.Errorf("eval: not callable: %T", callee)
}

// applyClosure tries each clause in order, selecting the first whose arity and
// param patterns match and whose guard (if any) is truthy. The closure runs in
// its captured lexical environment plus the caller's dynamic context.
func applyClosure(c *core.Closure, args []Value, env *Env) (Value, error) {
	base := env.withLex(c.Env)
	for _, clause := range c.Clauses {
		if len(clause.Params) != len(args) {
			continue
		}
		frame := map[core.BindingID]Value{}
		matched := true
		for i, p := range clause.Params {
			if ok, _ := match(p, args[i], base, frame); !ok {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		callEnv := base.extend(frame)
		if clause.Guard != nil {
			// A guard that errors or is non-truthy is a clause *failure*, not a
			// call abort: skip to the next clause (Erlang/Elixir semantics).
			g, err := EvalExpr(clause.Guard, callEnv)
			if err != nil || !truthy(g) {
				continue
			}
		}
		return EvalExpr(clause.Body, callEnv)
	}
	return nil, fmt.Errorf("fn: no matching clause for %d argument(s)", len(args))
}

// evalApp evaluates an application: evaluate the callee, then either apply it or
// short-circuit a zero-argument call of a non-callable to the value itself
// (so a bare identifier reference yields ordinary values and invokes functions
// uniformly).
func evalApp(app core.App, env *Env) (Value, error) {
	headVal, err := EvalExpr(app.Callee, env)
	if err != nil {
		return nil, err
	}
	if !isCallable(headVal) {
		if len(app.Args) == 0 {
			return headVal, nil
		}
		return nil, fmt.Errorf("eval: head is not callable: %T", headVal)
	}
	args := make([]Value, len(app.Args))
	for i, e := range app.Args {
		if args[i], err = EvalExpr(e, env); err != nil {
			return nil, err
		}
	}
	return apply(headVal, args, env)
}
