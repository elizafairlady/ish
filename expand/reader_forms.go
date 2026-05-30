package expand

import (
	"fmt"

	"ish/core"
)

func expandReaderExpr(stx *core.Syntax, head *core.Syntax, ctx *Context) (*core.Syntax, error) {
	parts, err := protocolArgs(stx, "%-expr")
	if err != nil {
		ctx.Diag.Error(stx.Span, DiagSyntaxShape, err.Error())
		return nil, err
	}
	if len(parts) == 0 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "%-expr requires at least one part")
	}
	// A clause-binder head (`fn`, `macro`) owns the whole expression: it parses
	// `PARAMS -> BODY` (or `PARAMS do … end`) itself and expands BODY as a
	// sub-expression. Enforesting first would split the body at the first
	// operator (`fn x -> x + 1` → `(fn x -> x) + 1`), so skip it here.
	if isClauseBinderHead(parts[0], ctx) {
		return expandApplicationCandidate(stx, head, parts, ctx)
	}
	candidate, usedOperator, err := enforestReaderExpr(stx, parts, ctx)
	if err != nil {
		if result, claimed, claimErr := tryImplementedMode(stx, head, ctx); claimed {
			return result, claimErr
		}
		return nil, err
	}
	if usedOperator {
		return Expand(candidate, ctx)
	}
	return expandApplicationCandidate(stx, head, parts, ctx)
}

func expandReaderGroup(stx *core.Syntax, _ *core.Syntax, ctx *Context) (*core.Syntax, error) {
	parts, err := protocolArgs(stx, "%-group")
	if err != nil {
		ctx.Diag.Error(stx.Span, DiagSyntaxShape, err.Error())
		return nil, err
	}
	if len(parts) != 1 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "expression group expects exactly one expression")
	}
	return Expand(parts[0], ctx)
}

func expandReaderExpression(stx *core.Syntax, _ *core.Syntax, ctx *Context) (*core.Syntax, error) {
	parts, err := protocolArgs(stx, "%-expression")
	if err != nil {
		ctx.Diag.Error(stx.Span, DiagSyntaxShape, err.Error())
		return nil, err
	}
	if len(parts) != 1 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "%-expression expects exactly one expression")
	}
	expanded, err := Expand(parts[0], ctx.Sub(WithKind(ExpressionCtx)))
	if err != nil {
		return nil, err
	}
	// `&` is the value escape: a bare identifier that would expand to a zero-arg
	// call is taken as its value (the reference) instead. Compound expressions
	// (calls with arguments) are unaffected.
	if app, ok := expanded.Node.(core.App); ok && len(app.Args) == 0 {
		return app.Callee, nil
	}
	return expanded, nil
}

func expandApplicationCandidate(stx, protocolHead *core.Syntax, parts []*core.Syntax, ctx *Context) (*core.Syntax, error) {
	if len(parts) == 0 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "application candidate requires a head")
	}
	return expandHeadedForm(stx, parts, protocolHead, ctx)
}

func readerWord(stx *core.Syntax) core.Word {
	if w, ok := stx.Node.(core.Word); ok {
		return w
	}
	return ""
}

// isClauseBinderHead reports whether stx names the `fn` or `macro` core form —
// the expression-level binders whose argument is a clause (`PARAMS -> BODY` or
// `PARAMS do … end`) with a body that is any expression. Such a form is not an
// operator expression and must reach its handler un-enforested.
func isClauseBinderHead(stx *core.Syntax, ctx *Context) bool {
	w, ok := stx.Node.(core.Word)
	if !ok || (w != "fn" && w != "macro") {
		return false
	}
	b, r := ctx.Bindings.Resolve(w, ctx.Phase, ctx.Space, stx.Scopes[ctx.Phase])
	return r == ResolveFound && b.Kind == CoreFormBinding
}

// tryImplementedMode offers stx to the active %-expr handlers. It returns
// claimed=false (with no diagnostic recorded) when no handler claims the form,
// so the caller can surface its own terminal error — this is what keeps a single
// diagnostic per failure instead of double-recording. When a handler claims,
// claimed=true and err carries any transform/ambiguity failure (already
// diagnosed).
func tryImplementedMode(stx, head *core.Syntax, ctx *Context) (result *core.Syntax, claimed bool, err error) {
	h, r := resolveProtocolHandler(ctx, "%-expr", head.Scopes[ctx.Phase], stx)
	switch r {
	case ResolveFound:
		out, err := invokeTransformerNoReexpand(stx, h.Transformer, h.DefScopes, ctx)
		if err != nil {
			return nil, true, err
		}
		out, err = Expand(out, ctx)
		return out, true, err
	case ResolveAmbiguous:
		return nil, true, ctx.fail(head.Span, DiagProtocolAmbiguous, fmt.Sprintf("ambiguous active implementations for %%-expr in %v context", ctx.Kind))
	default:
		return nil, false, nil
	}
}

func resolveProtocolHandler(ctx *Context, form core.Word, refScopes core.ScopeSet, stx *core.Syntax) (ProtocolHandler, ResolveResult) {
	name := protocolHandlerBindingName(ctx.Kind, form)
	b, r := ctx.Bindings.ResolveClaim(name, ctx.Phase, ProtocolHandlerSpace, refScopes, func(b *Binding) bool {
		h, ok := b.Value.(ProtocolHandler)
		return ok && (stx == nil || h.Claim == nil || h.Claim(stx))
	})
	if r != ResolveFound {
		return ProtocolHandler{}, r
	}
	h, ok := b.Value.(ProtocolHandler)
	if !ok {
		return ProtocolHandler{}, ResolveUnbound
	}
	return h, ResolveFound
}

func enforestReaderExpr(stx *core.Syntax, parts []*core.Syntax, ctx *Context) (*core.Syntax, bool, error) {
	p := &exprPratt{stx: stx, parts: parts, ctx: ctx}
	out, err := p.parse(0)
	if err != nil {
		return nil, false, err
	}
	if p.pos != len(parts) {
		part := parts[p.pos]
		return nil, false, ctx.fail(part.Span, DiagSyntaxShape, fmt.Sprintf("unexpected expression part %s", renderWordForError(part)))
	}
	return out, p.usedOperator, nil
}

type exprPratt struct {
	stx          *core.Syntax
	parts        []*core.Syntax
	ctx          *Context
	pos          int
	usedOperator bool
}

func (p *exprPratt) parse(minPrec int) (*core.Syntax, error) {
	left, err := p.operand()
	if err != nil {
		return nil, err
	}
	// lastNonassocPrec is the precedence of the most recently applied
	// non-associative operator at this level, or -1. It lives here as parser
	// state instead of on the syntax's Properties map, which is metadata about
	// the program, not a scratchpad for enforestation.
	lastNonassocPrec := -1
loop:
	for p.pos < len(p.parts) {
		opStx := p.parts[p.pos]
		entries, r := p.activeOperator(opStx)
		if r == ResolveUnbound {
			// Not an active operator in this context. Stop; the caller decides
			// what the leftover means (application, a claiming handler, an error).
			break
		}
		if r == ResolveAmbiguous {
			return nil, p.ctx.fail(opStx.Span, DiagProtocolAmbiguous, fmt.Sprintf("ambiguous active operators for %s in %v context", renderWordForError(opStx), p.ctx.Kind))
		}
		// Operator position: select the infix or postfix variant. A token with
		// only a prefix variant here is not consumable — stop and leave it.
		infix, hasInfix := fixityVariant(entries, FixityInfix)
		postfix, hasPostfix := fixityVariant(entries, FixityPostfix)
		if hasInfix && hasPostfix {
			return nil, p.ctx.fail(opStx.Span, DiagProtocolAmbiguous, fmt.Sprintf("operator %s is both infix and postfix here", renderWordForError(opStx)))
		}
		op := infix
		if hasPostfix {
			op = postfix
		}
		if !hasInfix && !hasPostfix {
			break loop
		}
		if op.Precedence < minPrec {
			break
		}
		if op.Assoc == AssocNone && lastNonassocPrec == op.Precedence {
			return nil, p.ctx.fail(opStx.Span, DiagSyntaxShape, fmt.Sprintf("non-associative operator %s cannot be chained", renderWordForError(opStx)))
		}
		p.usedOperator = true
		p.pos++
		if hasPostfix {
			if left, err = p.applyOperator(op, opStx, []*core.Syntax{left}); err != nil {
				return nil, err
			}
		} else {
			nextMin := op.Precedence + 1
			if op.Assoc == AssocRight {
				nextMin = op.Precedence
			}
			right, err := p.parse(nextMin)
			if err != nil {
				return nil, err
			}
			if left, err = p.applyOperator(op, opStx, []*core.Syntax{left, right}); err != nil {
				return nil, err
			}
		}
		if op.Assoc == AssocNone {
			lastNonassocPrec = op.Precedence
		} else {
			lastNonassocPrec = -1
		}
	}
	return left, nil
}

// activeOperator returns the fixity variants bound to a token in the current
// context. Operator-ness is decided ENTIRELY by an active binding — the expander
// presupposes no operator character set. What counts as an operator, and with
// which fixities, is a per-context protocol decision: `<`/`>` are comparison
// operators in an expression context and may be redirect operators in a shell
// context; `-` can be both infix (sub) and prefix (neg). The binding value is
// the set of fixity variants for the token; the parser picks one by position.
func (p *exprPratt) activeOperator(stx *core.Syntax) ([]OperatorEntry, ResolveResult) {
	tok, ok := stx.Node.(core.Word)
	if !ok {
		return nil, ResolveUnbound
	}
	binding, r := p.ctx.Bindings.Resolve(operatorBindingName(p.ctx.Kind, tok), p.ctx.Phase, OperatorSpace, stx.Scopes[p.ctx.Phase])
	if r != ResolveFound {
		return nil, r
	}
	entries, ok := binding.Value.([]OperatorEntry)
	if !ok || binding.Kind != OperatorBinding || len(entries) == 0 {
		return nil, ResolveUnbound
	}
	return entries, ResolveFound
}

func fixityVariant(entries []OperatorEntry, f OperatorFixity) (OperatorEntry, bool) {
	for _, e := range entries {
		if e.Fixity == f {
			return e, true
		}
	}
	return OperatorEntry{}, false
}

// applyOperator runs an operator's lowering on its operands and applies the
// provider def-scopes to the freshly built target identifier.
func (p *exprPratt) applyOperator(op OperatorEntry, opStx *core.Syntax, operands []*core.Syntax) (*core.Syntax, error) {
	out, err := op.Transformer(p.stx, operands)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, p.ctx.fail(opStx.Span, DiagBadMacroResult, fmt.Sprintf("operator %s returned nil syntax", op.Token))
	}
	return addOperatorTargetScopes(out, p.ctx.Phase, op.DefScopes[p.ctx.Phase]), nil
}

func (p *exprPratt) operand() (*core.Syntax, error) {
	if p.pos >= len(p.parts) {
		return nil, p.ctx.fail(p.stx.Span, DiagSyntaxShape, "expected expression operand")
	}
	// A leading active operator is legal here only as a prefix operator; an
	// operator token with no prefix variant has no left operand.
	if entries, r := p.activeOperator(p.parts[p.pos]); r != ResolveUnbound {
		opStx := p.parts[p.pos]
		if r == ResolveAmbiguous {
			return nil, p.ctx.fail(opStx.Span, DiagProtocolAmbiguous, fmt.Sprintf("ambiguous active operators for %s in %v context", renderWordForError(opStx), p.ctx.Kind))
		}
		prefix, ok := fixityVariant(entries, FixityPrefix)
		if !ok {
			return nil, p.ctx.fail(opStx.Span, DiagSyntaxShape, fmt.Sprintf("operator %s has no left operand", renderWordForError(opStx)))
		}
		p.usedOperator = true
		p.pos++
		// Prefix binds its operand at its own precedence, so a same-or-higher
		// precedence operator (including another prefix) nests into it.
		inner, err := p.parse(prefix.Precedence)
		if err != nil {
			return nil, err
		}
		return p.applyOperator(prefix, opStx, []*core.Syntax{inner})
	}
	// Collect the maximal run of non-operator parts as one application candidate,
	// stopping at the first token that is an active operator in this context. A
	// clause binder (`fn`/`macro`) in the run owns the rest of the expression as
	// its clause body, so operators after it (e.g. `+` in `reduce xs 0 fn x a ->
	// x + a`) belong to that body and must not be enforested here — consume to
	// the end so the clause binder, not this loop, parses them.
	start := p.pos
	for p.pos < len(p.parts) {
		if isClauseBinderHead(p.parts[p.pos], p.ctx) {
			p.pos = len(p.parts)
			break
		}
		if _, r := p.activeOperator(p.parts[p.pos]); r != ResolveUnbound {
			break
		}
		p.pos++
	}
	return readerExprCandidate(p.stx.Span, p.parts[start:p.pos]), nil
}

func readerExprCandidate(span core.Span, parts []*core.Syntax) *core.Syntax {
	if len(parts) == 1 {
		return parts[0]
	}
	elems := make([]*core.Syntax, 0, len(parts)+1)
	elems = append(elems, &core.Syntax{Node: core.Word("%-expr"), Span: span})
	elems = append(elems, parts...)
	return core.SyntaxList(span, elems...)
}

func protocolArgs(stx *core.Syntax, name core.Word) ([]*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) == 0 {
		return nil, fmt.Errorf("%s: improper protocol form", name)
	}
	w, ok := elems[0].Node.(core.Word)
	if !ok || w != name {
		return nil, fmt.Errorf("expected %s protocol form", name)
	}
	return elems[1:], nil
}

func renderWordForError(stx *core.Syntax) string {
	if w, ok := stx.Node.(core.Word); ok {
		return string(w)
	}
	return fmt.Sprintf("%T", stx.Node)
}
