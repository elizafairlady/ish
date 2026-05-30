package eval

import (
	"errors"
	"fmt"

	"ish/core"
	"ish/expand"
)

// MacroRunner is eval's implementation of expand.MacroRunner. It evaluates
// an expanded macro body (a Lambda) at phase-1 to produce a Closure, then
// wraps that Closure as a Transformer the expander can invoke on macro
// use sites. Each macro use calls Closure.Call with the use-site syntax
// as its single argument.
//
// phaseEnv holds the accumulated definition environment for each phase above 0,
// populated by `for-syntax` blocks (EvaluateForSyntax). A transformer body runs
// one phase up from where its macro is defined, so it is evaluated against the
// raised phase's environment and can call any for-syntax-defined helper.
//
// The env handed to a macro body has no Process. Actor/process primitives
// are not installed at phase 1 by the default runtime kernel; expansion-time
// process authority must be introduced deliberately by a future package policy.
type MacroRunner struct {
	Runtime  *Runtime
	phaseEnv map[core.Phase]*Env
}

// phaseBase returns the accumulated environment for a raised phase, carrying a
// resolver bound to ctx so identifier comparisons work in the body. A phase
// with no for-syntax definitions yet starts from a bare environment.
func (r *MacroRunner) phaseBase(phase core.Phase, ctx *expand.Context) *Env {
	resolver := expanderResolver{ctx: ctx}
	if e, ok := r.phaseEnv[phase]; ok && e != nil {
		return &Env{lex: e.lex, Runtime: r.Runtime, Resolver: resolver}
	}
	return &Env{Runtime: r.Runtime, Resolver: resolver}
}

// EvaluateForSyntax evaluates an expanded for-syntax body at `phase`, threading
// the result back as that phase's accumulated environment so its definitions
// persist for later for-syntax blocks and for transformer bodies at that phase.
func (r *MacroRunner) EvaluateForSyntax(body *core.Syntax, phase core.Phase, ctx *expand.Context) error {
	_, next, err := Eval(body, r.phaseBase(phase, ctx))
	if err != nil {
		return err
	}
	if r.phaseEnv == nil {
		r.phaseEnv = map[core.Phase]*Env{}
	}
	r.phaseEnv[phase] = next
	return nil
}

func (r *MacroRunner) EvaluateTransformer(body *core.Syntax, ctx *expand.Context) (expand.Transformer, error) {
	// A macro body is compiled and resolved at phase 1, so it is evaluated in
	// the phase-1 environment — letting it call for-syntax-defined helpers.
	v, err := EvalExpr(body, r.phaseBase(core.PhaseExpand, ctx))
	if err != nil {
		return nil, fmt.Errorf("macro body evaluation: %w", err)
	}
	c, ok := v.(*core.Closure)
	if !ok {
		return nil, errors.New("macro body did not produce a closure")
	}
	return func(useStx *core.Syntax, useCtx *expand.Context) (*core.Syntax, error) {
		// The macro body runs with the use-site expansion context as its
		// Expander, so local-expand/bind! act in the context where the macro is
		// used (resolving use-site references, minting fresh names there).
		callEnv := &Env{Runtime: r.Runtime, Resolver: expanderResolver{ctx: useCtx}, Expander: expanderOps{ctx: useCtx}}
		result, err := apply(c, []Value{useStx}, callEnv)
		if err != nil {
			return nil, fmt.Errorf("macro invocation: %w", err)
		}
		s, ok := result.(*core.Syntax)
		if !ok {
			return nil, fmt.Errorf("macro returned %T, expected syntax", result)
		}
		return s, nil
	}, nil
}

// expanderOps backs the eval.Expander interface with a live expansion context,
// implementing local-expand (expand a sub-form in that context) and bind!
// (mint a fresh hygienic identifier carrying a unique scope at the context's
// phase, usable as a collision-free binding name in a macro's output).
type expanderOps struct{ ctx *expand.Context }

func (e expanderOps) LocalExpand(stx *core.Syntax) (*core.Syntax, error) {
	return expand.Expand(stx, e.ctx)
}

func (e expanderOps) FreshIdentifier(name core.Word) *core.Syntax {
	if name == "" {
		name = "tmp"
	}
	id := &core.Syntax{Node: name}
	return core.AddScope(id, e.ctx.Phase, core.NewScope())
}

type expanderResolver struct{ ctx *expand.Context }

// NewResolver builds an IdentifierResolver backed by ctx's binding table so
// free-identifier comparisons work in ordinary runtime code, not only while a
// macro body is being expanded.
func NewResolver(ctx *expand.Context) IdentifierResolver { return expanderResolver{ctx: ctx} }

func (r expanderResolver) FreeIdentifierEqual(a, b *core.Syntax) bool {
	aw, aok := a.Node.(core.Word)
	bw, bok := b.Node.(core.Word)
	if !aok || !bok || aw != bw || r.ctx == nil || r.ctx.Bindings == nil {
		return false
	}
	ab, ar := r.ctx.Bindings.Resolve(aw, r.ctx.Phase, r.ctx.Space, a.Scopes[r.ctx.Phase])
	bb, br := r.ctx.Bindings.Resolve(bw, r.ctx.Phase, r.ctx.Space, b.Scopes[r.ctx.Phase])
	if ar == expand.ResolveFound && br == expand.ResolveFound {
		return ab.ID == bb.ID
	}
	// Two identifiers that share a name and both resolve to no binding are
	// free-identifier=? (Flatt/Racket): neither is captured, so they denote
	// the same free reference.
	return ar == expand.ResolveUnbound && br == expand.ResolveUnbound
}

// BoundIdentifierEqual compares two identifiers at the resolver's phase: same
// word and same scope set. The phase comes from the live expansion context
// rather than a hardcoded constant.
func (r expanderResolver) BoundIdentifierEqual(a, b *core.Syntax) bool {
	ph := core.PhaseRuntime
	if r.ctx != nil {
		ph = r.ctx.Phase
	}
	return core.BoundIdentEqual(a, b, ph)
}

func (r expanderResolver) SpaceOf(stx *core.Syntax) (expand.SpaceValue, bool) {
	if r.ctx == nil {
		return expand.SpaceValue{}, false
	}
	return r.ctx.SpaceOf(stx)
}

func (r expanderResolver) ResolveMember(member *core.Syntax, sv expand.SpaceValue) (*expand.Binding, expand.ResolveResult) {
	if r.ctx == nil {
		return nil, expand.ResolveUnbound
	}
	return r.ctx.ResolveMember(member, sv)
}
