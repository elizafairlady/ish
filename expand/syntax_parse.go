package expand

import (
	"errors"
	"fmt"

	"ish/core"
)

// syntaxParseForm implements the `syntax-parse` core form:
//
//	syntax-parse TARGET do
//	  PATTERN -> BODY
//	  PATTERN when GUARD -> BODY
//	  ...
//	end
//
// It lowers to a `syntax-parse*` application carrying the target followed by
// (pattern, guard, handler) triples. The pattern's attributes become the
// parameter list of both the guard and the handler, so a fender guard sees the
// same bindings the body does. Unlike syntax-case there is no positional
// literal vector and no implicit head keyword: identifiers are matched with
// the `%~literal` combinator, and the macro keyword position is matched with
// `_`.
func syntaxParseForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) != 3 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "syntax-parse expects: target do-clauses")
	}
	target, err := Expand(elems[1], ctx)
	if err != nil {
		return nil, err
	}
	clauses, ok, err := surfaceClausesFromDo(elems[2], ctx, false)
	if !ok || err != nil {
		if err == nil {
			err = errors.New("syntax-parse: clauses must be a do block")
			ctx.Diag.Error(elems[2].Span, DiagSyntaxShape, err.Error())
		}
		return nil, err
	}
	var spClauses []core.SyntaxParseClause
	for _, clause := range clauses {
		pattern, guard, body, err := splitClause(clause)
		if err != nil {
			ctx.Diag.Error(clause.Span, DiagSyntaxShape, fmt.Sprintf("syntax-parse clause: %v", err))
			return nil, err
		}
		if err := validateSyntaxParsePattern(pattern, ctx); err != nil {
			return nil, err
		}
		spc, err := compileSyntaxParseClause(pattern, guard, body, ctx)
		if err != nil {
			return nil, err
		}
		spClauses = append(spClauses, spc)
	}
	return &core.Syntax{Node: core.SyntaxParse{Target: target, Clauses: spClauses}, Span: stx.Span}, nil
}

// compileSyntaxParseClause compiles one syntax-parse/syntax-case clause: a
// fresh scope carries the pattern's attribute bindings (with depth) into the
// guard and body, which are then expanded under them.
func compileSyntaxParseClause(pattern, guard, body *core.Syntax, ctx *Context) (core.SyntaxParseClause, error) {
	s, child := ctx.IntroduceScope()
	patCtx := child.Sub(WithKind(PatternCtx))
	pat, err := CompileSyntaxPattern(core.AddScope(pattern, ctx.Phase, s), patCtx)
	if err != nil {
		return core.SyntaxParseClause{}, err
	}
	var guardOut *core.Syntax
	if guard != nil {
		guardOut, err = Expand(core.AddScope(guard, ctx.Phase, s), child)
		if err != nil {
			return core.SyntaxParseClause{}, err
		}
	}
	bodyOut, err := Expand(core.AddScope(body, ctx.Phase, s), child)
	if err != nil {
		return core.SyntaxParseClause{}, err
	}
	return core.SyntaxParseClause{Pattern: pat, Guard: guardOut, Body: bodyOut}, nil
}

// validateSyntaxParsePattern rejects unknown `~`-combinators and the
// fixed-width restriction that ~seq groups carry no ellipsis/optional/seq
// element (so an ellipsis or ~optional over a ~seq has a static width).
func validateSyntaxParsePattern(stx *core.Syntax, ctx *Context) error {
	if lowered, ok := core.LowerReaderListSyntax(stx); ok {
		stx = lowered
	}
	if name, args, ok := core.SyntaxParseCombinator(stx); ok {
		if err := checkCombinatorArity(stx, name, args, ctx); err != nil {
			return err
		}
		if name == core.CombOr {
			if err := checkOrBranchesBindSameAttributes(stx, args, ctx); err != nil {
				return err
			}
		}
		if name == core.CombSeq {
			for _, a := range args {
				if err := validateSyntaxSeqElement(a, ctx); err != nil {
					return err
				}
			}
			return nil
		}
		for _, a := range args {
			if err := validateSyntaxParsePattern(a, ctx); err != nil {
				return err
			}
		}
		return nil
	}
	if w, ok := stx.Node.(core.Word); ok && len(w) > 0 && w[0] == '~' {
		return ctx.fail(stx.Span, DiagSyntaxShape, fmt.Sprintf("unknown pattern combinator %s", w))
	}
	switch n := stx.Node.(type) {
	case core.SyntaxPair:
		if elems, ok := core.SyntaxListElems(stx); ok {
			return validateSyntaxParseElems(elems, ctx)
		}
	case core.SyntaxVector:
		return validateSyntaxParseElems([]*core.Syntax(n), ctx)
	case core.SyntaxTuple:
		return validateSyntaxParseElems([]*core.Syntax(n), ctx)
	case core.SyntaxDict:
		for _, e := range n {
			if err := validateSyntaxParsePattern(e.Value, ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateSyntaxParseElems(elems []*core.Syntax, ctx *Context) error {
	for _, e := range elems {
		if w, ok := e.Node.(core.Word); ok && w == "..." {
			continue
		}
		if err := validateSyntaxParsePattern(e, ctx); err != nil {
			return err
		}
	}
	return nil
}

// validateSyntaxSeqElement enforces that a ~seq inner element is fixed-width:
// no ellipsis, ~optional, or nested ~seq, so the group's width is its length.
func validateSyntaxSeqElement(stx *core.Syntax, ctx *Context) error {
	if w, ok := stx.Node.(core.Word); ok && w == "..." {
		return ctx.fail(stx.Span, DiagSyntaxShape, "~seq elements must be fixed width: no ellipsis inside ~seq")
	}
	if name, _, ok := core.SyntaxParseCombinator(stx); ok && (name == core.CombSeq || name == core.CombOptional) {
		return ctx.fail(stx.Span, DiagSyntaxShape, "~seq elements must be fixed width: no nested ~seq or ~optional")
	}
	return validateSyntaxParsePattern(stx, ctx)
}

func checkCombinatorArity(stx *core.Syntax, name core.Word, args []*core.Syntax, ctx *Context) error {
	fail := func(msg string) error {
		return ctx.fail(stx.Span, DiagSyntaxShape, msg)
	}
	switch name {
	case core.CombLiteral:
		if len(args) != 1 {
			return fail("~literal expects one identifier")
		}
		if _, ok := args[0].Node.(core.Word); !ok {
			return fail("~literal argument must be an identifier")
		}
	case core.CombDatum, core.CombFail, core.CombOptional:
		if len(args) != 1 {
			return fail(fmt.Sprintf("%s expects one argument", name))
		}
	case core.CombDescribe:
		if len(args) != 2 {
			return fail("~describe expects a message string and a pattern")
		}
		if _, ok := args[0].Node.(core.String); !ok {
			return fail("~describe message must be a string")
		}
	case core.CombNot:
		if len(args) != 1 {
			return fail("~not expects one pattern")
		}
	case core.CombAnd, core.CombOr, core.CombSeq:
		if len(args) == 0 {
			return fail(fmt.Sprintf("%s expects at least one pattern", name))
		}
	}
	return nil
}

// checkOrBranchesBindSameAttributes enforces that every ~or alternative binds
// the identical set of attributes, so a match leaves no attribute undefined
// regardless of which branch fired.
func checkOrBranchesBindSameAttributes(stx *core.Syntax, branches []*core.Syntax, ctx *Context) error {
	nameSet := func(p *core.Syntax) map[core.Word]bool {
		set := map[core.Word]bool{}
		for _, v := range core.SyntaxParseAttributes(p) {
			set[v.Node.(core.Word)] = true
		}
		return set
	}
	first := nameSet(branches[0])
	for _, b := range branches[1:] {
		other := nameSet(b)
		if len(other) != len(first) {
			return ctx.fail(stx.Span, DiagSyntaxShape, "~or alternatives must bind the same attributes")
		}
		for name := range first {
			if !other[name] {
				return ctx.fail(stx.Span, DiagSyntaxShape, "~or alternatives must bind the same attributes")
			}
		}
	}
	return nil
}
