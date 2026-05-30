package expand

import (
	"errors"
	"fmt"

	"ish/core"
)

// Expand performs one round of expansion of stx in ctx. Diagnostics are
// written to ctx.Diag for spec-named cases; the returned error is non-nil
// when expansion cannot proceed, so callers can short-circuit on first
// failure without scanning the diagnostic sink.
func Expand(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	if stx == nil {
		return nil, errors.New("expand: nil syntax")
	}
	switch n := stx.Node.(type) {
	case core.SyntaxPair:
		return expandPair(stx, n, ctx)
	case core.Word:
		return expandIdentifier(stx, n, ctx)
	case core.Int, core.Float, core.String, core.Bytes, core.Atom, core.Nil:
		return stx, nil
	case core.Resolved:
		// Expansion is idempotent on a *valid* reference. A protocol or macro may
		// splice a genuine resolution result — e.g. a cross-space reference from
		// `ref->syntax` — into its output; re-expanding it is the identity, and
		// this is what lets a reader container like `{:found ref}` carry one. A
		// zero-ID Resolved is not a real reference but hand-built core IR, which
		// (like a raw App below) the expander still refuses to smuggle through.
		if n.ID == 0 {
			return nil, fmt.Errorf("expand: malformed resolved reference (no binding id)")
		}
		return stx, nil
	case core.SyntaxVector:
		return expandSyntaxVectorLiteral(stx, n, ctx)
	case core.SyntaxTuple:
		return expandSyntaxTupleLiteral(stx, n, ctx)
	case core.SyntaxDict:
		return expandSyntaxDictLiteral(stx, n, ctx)
	default:
		return nil, fmt.Errorf("expand: unsupported node kind %T", n)
	}
}

func expandIdentifier(stx *core.Syntax, w core.Word, ctx *Context) (*core.Syntax, error) {
	b, r := ctx.Bindings.Resolve(w, ctx.Phase, ctx.Space, stx.Scopes[ctx.Phase])
	switch r {
	case ResolveUnbound:
		return nil, ctx.fail(stx.Span, DiagUnbound, fmt.Sprintf("unbound identifier: %s", w))
	case ResolveAmbiguous:
		return nil, ctx.fail(stx.Span, DiagAmbiguous, fmt.Sprintf("ambiguous binding for %s", w))
	}
	if b.Kind == TransformerBinding {
		t, defScopes, ok := transformerValue(b.Value)
		if !ok {
			return nil, fmt.Errorf("%s has invalid transformer binding", w)
		}
		// A bare macro identifier is a zero-argument invocation. Present it to
		// the transformer as the one-element form `(m)` so a clause pattern
		// sees the macro keyword as the head exactly as a with-arguments call
		// `(m a b)` does — a variadic `(_ a ...)` then matches zero arguments
		// uniformly, and `(_)` matches the nullary use.
		return invokeTransformer(core.SyntaxList(stx.Span, stx), t, defScopes, ctx)
	}
	if b.Kind == AttributeBinding {
		// A pattern attribute's capture is reachable as an ordinary value only
		// at depth 0 — a single captured syntax, which fenders and bodies
		// inspect. A deeper attribute is a sequence that only a quasisyntax
		// template can iterate, so referencing it bare is rejected.
		if attr, _ := b.Value.(Attribute); attr.Depth != 0 {
			return nil, ctx.fail(stx.Span, DiagInvalidContext, fmt.Sprintf("attribute %s (ellipsis depth %d) can only be used inside a quasisyntax template", w, attr.Depth))
		}
		ref := &core.Syntax{Node: b.Ref(), Span: stx.Span, Scopes: stx.Scopes, Properties: stx.Properties}
		return &core.Syntax{Node: core.App{Callee: ref}, Span: stx.Span}, nil
	}
	if b.Kind != ValueBinding {
		return nil, ctx.fail(stx.Span, DiagInvalidContext, fmt.Sprintf("%s names a %v, not a value", w, b.Kind))
	}
	// A bare value identifier is a zero-argument call. References to functions
	// invoke them; references to ordinary values short-circuit to the value at
	// eval (see evalApp). `&expr` is the value escape (see expandReaderExpression),
	// and an application head is built as a reference, not via this path — so this
	// wrapping applies to standalone, argument, and operand positions uniformly.
	ref := &core.Syntax{
		Node:       b.Ref(),
		Span:       stx.Span,
		Scopes:     stx.Scopes,
		Properties: stx.Properties,
	}
	return &core.Syntax{Node: core.App{Callee: ref}, Span: stx.Span}, nil
}

func expandPair(stx *core.Syntax, n core.SyntaxPair, ctx *Context) (*core.Syntax, error) {
	head := n.Head
	if w, ok := head.Node.(core.Word); ok {
		if isReaderProtocolWord(w) {
			return expandProtocolForm(stx, w, head, ctx)
		}
	}
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) == 0 {
		return nil, errors.New("application: improper list")
	}
	return expandHeadedForm(stx, elems, nil, ctx)
}

func isReaderProtocolWord(w core.Word) bool {
	s := string(w)
	return len(s) >= 2 && s[0] == '%' && s[1] == '-'
}

func expandProtocolForm(stx *core.Syntax, form core.Word, head *core.Syntax, ctx *Context) (*core.Syntax, error) {
	switch form {
	case "%-expr":
		return expandReaderExpr(stx, head, ctx)
	case "%-group":
		return expandReaderGroup(stx, head, ctx)
	case "%-expression":
		return expandReaderExpression(stx, head, ctx)
	case "%-package-begin":
		return expandPackageBegin(stx, ctx)
	}
	h, r := resolveProtocolHandler(ctx, form, head.Scopes[ctx.Phase], stx)
	switch r {
	case ResolveUnbound:
		return nil, ctx.fail(head.Span, DiagProtocolUnhandled, fmt.Sprintf("no active protocol handles %s in %v context", form, ctx.Kind))
	case ResolveAmbiguous:
		return nil, ctx.fail(head.Span, DiagProtocolAmbiguous, fmt.Sprintf("ambiguous active protocols for %s in %v context", form, ctx.Kind))
	}
	result, err := invokeTransformerNoReexpand(stx, h.Transformer, h.DefScopes, ctx)
	if err != nil {
		return nil, err
	}
	return Expand(result, ctx)
}

// expandCallArgs expands an application's arguments. A clause binder (`fn`/
// `macro`) in argument position consumes the rest of the arguments as its
// clause, so it is the final argument and needs no surrounding parentheses
// (`spawn fn do … end`, `reduce xs 0 fn x a -> x + a`). Parenthesize it only
// when a further argument must follow.
func expandCallArgs(elems []*core.Syntax, ctx *Context) ([]*core.Syntax, error) {
	args := make([]*core.Syntax, 0, len(elems))
	for i := 0; i < len(elems); i++ {
		if isClauseBinderHead(elems[i], ctx) {
			grouped, err := Expand(readerExprCandidate(elems[i].Span, elems[i:]), ctx)
			if err != nil {
				return nil, err
			}
			return append(args, grouped), nil
		}
		ee, err := Expand(elems[i], ctx)
		if err != nil {
			return nil, err
		}
		args = append(args, ee)
	}
	return args, nil
}

func expandHeadedForm(stx *core.Syntax, elems []*core.Syntax, fallbackHead *core.Syntax, ctx *Context) (*core.Syntax, error) {
	if len(elems) == 0 {
		return nil, errors.New("application: empty")
	}
	if w, ok := elems[0].Node.(core.Word); ok {
		b, r := ctx.Bindings.Resolve(w, ctx.Phase, ctx.Space, elems[0].Scopes[ctx.Phase])
		switch r {
		case ResolveFound:
			switch b.Kind {
			case CompileTimeFormBinding:
				if ctx.Kind != PackageCtx && ctx.Kind != BodyCtx {
					return nil, ctx.fail(elems[0].Span, DiagInvalidContext, fmt.Sprintf("%s is only valid in package/body context", w))
				}
				return b.Value.(CoreFormHandler)(core.SyntaxList(stx.Span, elems...), ctx)
			case CoreFormBinding:
				return b.Value.(CoreFormHandler)(core.SyntaxList(stx.Span, elems...), ctx)
			case TransformerBinding:
				t, defScopes, ok := transformerValue(b.Value)
				if !ok {
					return nil, fmt.Errorf("%s has invalid transformer binding", w)
				}
				return invokeTransformer(core.SyntaxList(stx.Span, elems...), t, defScopes, ctx)
			case ValueBinding:
				// The head is the function being applied — a reference, not a
				// zero-arg call of itself. Build it directly (a single element is a
				// bare identifier, which becomes a zero-arg call here).
				callee := &core.Syntax{
					Node:       b.Ref(),
					Span:       elems[0].Span,
					Scopes:     elems[0].Scopes,
					Properties: elems[0].Properties,
				}
				args, err := expandCallArgs(elems[1:], ctx)
				if err != nil {
					return nil, err
				}
				return &core.Syntax{Node: core.App{Callee: callee, Args: args}, Span: stx.Span}, nil
			default:
				return nil, ctx.fail(elems[0].Span, DiagInvalidContext, fmt.Sprintf("%s names a %v, not an applicable binding", w, b.Kind))
			}
		case ResolveAmbiguous:
			return nil, ctx.fail(elems[0].Span, DiagAmbiguous, fmt.Sprintf("ambiguous binding for %s", w))
		case ResolveUnbound:
			if fallbackHead != nil {
				if result, claimed, claimErr := tryImplementedMode(stx, fallbackHead, ctx); claimed {
					return result, claimErr
				}
			}
			return nil, ctx.fail(elems[0].Span, DiagUnbound, fmt.Sprintf("unbound identifier: %s", w))
		}
	}
	return expandApplicationParts(stx.Span, elems, ctx)
}

func expandApplicationParts(span core.Span, elems []*core.Syntax, ctx *Context) (*core.Syntax, error) {
	callee, err := Expand(elems[0], ctx)
	if err != nil {
		return nil, err
	}
	args := make([]*core.Syntax, 0, len(elems)-1)
	for _, e := range elems[1:] {
		ee, err := Expand(e, ctx)
		if err != nil {
			return nil, err
		}
		args = append(args, ee)
	}
	return core.SyntaxApp(span, callee, args...), nil
}

// invokeTransformer executes the Flatt set-of-scopes hygiene protocol:
// allocate a fresh macro-introduction scope, flip it through the use-site
// syntax, invoke the transformer, flip it through the result, and re-expand.
// After the round trip, use-site identifiers carry their original scope sets
// (s flipped in then out) while transformer-introduced identifiers carry s
// (flipped in only on the way out), marking them as distinct in resolution.
// defScopes, when non-empty, are the transformer's definition-site scopes,
// applied to introduced identifiers so they resolve in the provider context.
func invokeTransformer(stx *core.Syntax, t Transformer, defScopes core.PhaseScopes, ctx *Context) (*core.Syntax, error) {
	result, err := invokeTransformerNoReexpand(stx, t, defScopes, ctx)
	if err != nil {
		recordMacroError(stx, err, ctx)
		return nil, err
	}
	return Expand(result, ctx)
}

// recordMacroError writes a transformer failure to the diagnostics sink at the
// most precise span available — the failing sub-syntax for a SyntaxError,
// otherwise the use site.
func recordMacroError(stx *core.Syntax, err error, ctx *Context) {
	var syntaxErr *core.SyntaxError
	if errors.As(err, &syntaxErr) && syntaxErr.Syntax != nil {
		ctx.Diag.Error(syntaxErr.Syntax.Span, DiagBadMacroResult, syntaxErr.Message)
	} else {
		ctx.Diag.Error(stx.Span, DiagBadMacroResult, err.Error())
	}
}

// invokeTransformerNoReexpand runs one hygiene round trip without re-expanding
// the result. Definition-site scopes are applied to introduced identifiers
// before the outgoing flip — an identifier is "introduced" exactly when it does
// not already carry the macro-introduction scope s.
func invokeTransformerNoReexpand(stx *core.Syntax, t Transformer, defScopes core.PhaseScopes, ctx *Context) (*core.Syntax, error) {
	s := core.NewScope()
	flipped := core.FlipScope(stx, ctx.Phase, s)
	result, err := t(flipped, ctx)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, ctx.fail(stx.Span, DiagBadMacroResult, "transformer returned nil syntax")
	}
	if scopes := defScopes[ctx.Phase]; len(scopes) != 0 {
		result = core.WalkScopes(result, func(existing core.PhaseScopes) core.PhaseScopes {
			if existing[ctx.Phase].Has(s) {
				return existing
			}
			out := existing
			for _, def := range scopes {
				out = out.Add(ctx.Phase, def)
			}
			return out
		})
	}
	return core.FlipScope(result, ctx.Phase, s), nil
}

// addOperatorTargetScopes applies a protocol's definition-site scopes to the
// operator target identifier an OperatorTransformer injects. The target is the
// only freshly built node (its scope set is empty) while the operands carry
// the user's scopes, so "introduced" here means "carries no scope yet". This is
// the operator analogue of the macro-introduction flip, which operators do not
// use because they splice user operands directly rather than round-tripping.
func addOperatorTargetScopes(stx *core.Syntax, ph core.Phase, scopes core.ScopeSet) *core.Syntax {
	if stx == nil || len(scopes) == 0 {
		return stx
	}
	return core.WalkScopes(stx, func(existing core.PhaseScopes) core.PhaseScopes {
		if len(existing[ph]) != 0 {
			return existing
		}
		out := existing
		for _, s := range scopes {
			out = out.Add(ph, s)
		}
		return out
	})
}
