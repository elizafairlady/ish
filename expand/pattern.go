package expand

import (
	"errors"
	"fmt"

	"ish/core"
)

// CompilePattern compiles a value pattern (fn/match/bind/receive): the
// structural subset of the IR, introducing a ValueBinding per pattern variable.
// The caller must be in a PatternCtx context (specp2:329).
func CompilePattern(stx *core.Syntax, ctx *Context) (core.Pattern, error) {
	if ctx.Kind != PatternCtx {
		return nil, fmt.Errorf("pattern: caller must use PatternCtx, got %v", ctx.Kind)
	}
	return newPatCompiler(ctx, false).compile(stx, 0)
}

// CompilePatternList compiles a sequence of value patterns (one fn clause's
// parameters) sharing duplicate-variable detection across them, so `[x x]`
// across two parameters is rejected just as it is within one.
func CompilePatternList(stxs []*core.Syntax, ctx *Context) ([]core.Pattern, error) {
	if ctx.Kind != PatternCtx {
		return nil, fmt.Errorf("pattern: caller must use PatternCtx, got %v", ctx.Kind)
	}
	pc := newPatCompiler(ctx, false)
	out := make([]core.Pattern, 0, len(stxs))
	for _, s := range stxs {
		p, err := pc.compile(s, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// CompileSyntaxPattern compiles a syntax-parse/syntax-case pattern: the full IR
// including ellipsis/optional repetition, ~seq groups, and the combinator
// nodes. Each pattern variable becomes an AttributeBinding carrying its
// ellipsis depth.
func CompileSyntaxPattern(stx *core.Syntax, ctx *Context) (core.Pattern, error) {
	if ctx.Kind != PatternCtx {
		return nil, fmt.Errorf("pattern: caller must use PatternCtx, got %v", ctx.Kind)
	}
	return newPatCompiler(ctx, true).compile(stx, 0)
}

// patCompiler carries the per-pattern state shared by both modes: syntax mode
// enables combinators/ellipsis and binds attributes; seen records each
// variable's binding (reused for non-linear syntax patterns, rejected as a
// duplicate in value patterns) and the depth it was first bound at; noBind
// suppresses binding inside ~not, where sub-pattern variables capture nothing.
type patCompiler struct {
	ctx    *Context
	syntax bool
	seen   map[core.Word]*Binding
	depth  map[core.Word]int
	noBind bool
}

func newPatCompiler(ctx *Context, syntax bool) *patCompiler {
	return &patCompiler{ctx: ctx, syntax: syntax, seen: map[core.Word]*Binding{}, depth: map[core.Word]int{}}
}

func (pc *patCompiler) compile(stx *core.Syntax, depth int) (core.Pattern, error) {
	if stx == nil {
		return nil, errors.New("pattern: nil syntax")
	}
	if pc.syntax {
		if lowered, ok := core.LowerReaderListSyntax(stx); ok {
			stx = lowered
		}
		if name, args, ok := core.SyntaxParseCombinator(stx); ok {
			return pc.combinator(stx, name, args, depth)
		}
	}
	switch n := stx.Node.(type) {
	case core.Word:
		if n == "_" {
			return core.PatWild{}, nil
		}
		if n == "..." {
			if pc.syntax {
				return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, "unexpected ellipsis in pattern")
			}
			return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, "`...` is only meaningful in syntax patterns; use a `.` rest pattern (e.g. (h . t), [h . t]) to bind the remainder")
		}
		if pc.syntax {
			if core.IsSyntaxParseCombinator(n) {
				return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, fmt.Sprintf("combinator %s needs arguments", n))
			}
			return pc.variable(stx, n, depth)
		}
		return pc.variable(stx, n, depth)
	case core.Int, core.Float, core.String, core.Bytes, core.Atom, core.Nil:
		return core.PatLit{Value: n.(core.Datum)}, nil
	case core.SyntaxVector:
		return pc.compileSeq(core.SeqVector, []*core.Syntax(n), stx, depth)
	case core.SyntaxTuple:
		return pc.compileSeq(core.SeqTuple, []*core.Syntax(n), stx, depth)
	case core.SyntaxDict:
		return pc.compileDict(n, depth)
	case core.SyntaxPair:
		return pc.compileList(stx, depth)
	}
	return nil, fmt.Errorf("pattern: unsupported %T", stx.Node)
}

// variable binds (or reuses) a pattern variable. Inside ~not nothing is
// captured, so a variable degenerates to a wildcard.
func (pc *patCompiler) variable(stx *core.Syntax, n core.Word, depth int) (core.Pattern, error) {
	if pc.noBind {
		return core.PatWild{}, nil
	}
	if b := pc.seen[n]; b != nil {
		if !pc.syntax {
			return nil, pc.ctx.fail(stx.Span, DiagDuplicatePattern, fmt.Sprintf("duplicate binding %s in pattern", n))
		}
		if pc.depth[n] != depth {
			return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, fmt.Sprintf("attribute %s bound at conflicting ellipsis depths", n))
		}
		return core.PatVar{Ref: b.Ref()}, nil
	}
	kind := ValueBinding
	var value any
	if pc.syntax {
		kind = AttributeBinding
		value = Attribute{Depth: depth}
	}
	b := pc.ctx.Bindings.Define(n, pc.ctx.Phase, pc.ctx.Space, stx.Scopes[pc.ctx.Phase], kind, value)
	pc.seen[n] = b
	pc.depth[n] = depth
	return core.PatVar{Ref: b.Ref()}, nil
}

// compileSeq compiles a vector or tuple pattern, recognizing a trailing `.`
// rest marker (`[a b . rest]`, `{a . rest}`) the same way a list does. Without a
// rest the Tail stays nil (exact-length match); with one, Tail is the remainder
// pattern and the matcher rebinds the rest as the same kind of sequence.
func (pc *patCompiler) compileSeq(kind core.SeqKind, elems []*core.Syntax, stx *core.Syntax, depth int) (core.Pattern, error) {
	lead, tail, err := splitDotTail(elems)
	if err != nil {
		return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, err.Error())
	}
	seqElems, err := pc.compileSeqElems(lead, depth)
	if err != nil {
		return nil, err
	}
	pat := core.PatSeq{Kind: kind, Elems: seqElems}
	if tail != nil {
		if pc.syntax {
			return nil, pc.ctx.fail(tail.Span, DiagSyntaxShape, "rest pattern not allowed in syntax pattern")
		}
		tp, err := pc.compile(tail, depth)
		if err != nil {
			return nil, err
		}
		pat.Tail = tp
	}
	return pat, nil
}

// splitDotTail splits sequence elements on a `.` rest marker: `[a b . rest]`
// yields leading [a b] and tail rest. A `.` must be the second-to-last element
// (exactly one pattern after it); anywhere else is an error. With no `.`, the
// elements are returned unchanged and tail is nil.
func splitDotTail(elems []*core.Syntax) (lead []*core.Syntax, tail *core.Syntax, err error) {
	for i, e := range elems {
		if w, ok := e.Node.(core.Word); ok && w == "." {
			if i != len(elems)-2 {
				return nil, nil, errors.New("a `.` rest marker must be followed by exactly one pattern")
			}
			return elems[:i], elems[i+1], nil
		}
	}
	return elems, nil, nil
}

func (pc *patCompiler) compileList(stx *core.Syntax, depth int) (core.Pattern, error) {
	if elems, ok := core.SyntaxListElems(stx); ok && len(elems) == 2 {
		if w, isW := elems[0].Node.(core.Word); isW && w == "pin" {
			return pc.compilePin(elems[1])
		}
	}
	elems, tail := listParts(stx)
	seqElems, err := pc.compileSeqElems(elems, depth)
	if err != nil {
		return nil, err
	}
	tailPat := core.Pattern(core.PatLit{Value: core.Nil{}})
	if tail != nil {
		if pc.syntax {
			return nil, pc.ctx.fail(tail.Span, DiagSyntaxShape, "improper list not allowed in syntax pattern")
		}
		tailPat, err = pc.compile(tail, depth)
		if err != nil {
			return nil, err
		}
	}
	return core.PatSeq{Kind: core.SeqList, Elems: seqElems, Tail: tailPat}, nil
}

// listParts flattens a (possibly reader-wrapped, possibly improper) list into
// its element heads and a final non-nil improper tail (nil for a proper list).
func listParts(stx *core.Syntax) (elems []*core.Syntax, tail *core.Syntax) {
	cur := stx
	for cur != nil {
		if lowered, ok := core.LowerReaderListSyntax(cur); ok {
			cur = lowered
		}
		pair, ok := cur.Node.(core.SyntaxPair)
		if !ok {
			if _, isNil := cur.Node.(core.Nil); isNil {
				return elems, nil
			}
			return elems, cur
		}
		elems = append(elems, pair.Head)
		cur = pair.Tail
	}
	return elems, nil
}

// compileSeqElems compiles the elements of a list/vector/tuple, recognizing —
// in syntax mode only — a trailing `...` (ellipsis repetition), and the
// ~optional/~seq combinator elements.
func (pc *patCompiler) compileSeqElems(elems []*core.Syntax, depth int) ([]core.PatSeqElem, error) {
	out := make([]core.PatSeqElem, 0, len(elems))
	for i := 0; i < len(elems); i++ {
		elem := elems[i]
		ellipsis := pc.syntax && i+1 < len(elems) && isEllipsisWord(elems[i+1])
		if pc.syntax {
			if name, cargs, ok := core.SyntaxParseCombinator(elem); ok && (name == core.CombOptional || name == core.CombSeq) {
				e, err := pc.compileRepeatCombinator(elem, name, cargs, depth, ellipsis)
				if err != nil {
					return nil, err
				}
				out = append(out, e)
				if ellipsis {
					i++
				}
				continue
			}
		}
		subDepth, rep := depth, core.RepOne
		if ellipsis {
			subDepth, rep = depth+1, core.RepEllipsis
		}
		sub, err := pc.compile(elem, subDepth)
		if err != nil {
			return nil, err
		}
		out = append(out, core.PatSeqElem{Sub: sub, Rep: rep})
		if ellipsis {
			i++
		}
	}
	return out, nil
}

func (pc *patCompiler) compileRepeatCombinator(elem *core.Syntax, name core.Word, cargs []*core.Syntax, depth int, ellipsis bool) (core.PatSeqElem, error) {
	switch name {
	case core.CombOptional:
		if len(cargs) != 1 {
			return core.PatSeqElem{}, pc.ctx.fail(elem.Span, DiagSyntaxShape, "~optional expects one pattern")
		}
		sub, err := pc.compile(cargs[0], depth)
		if err != nil {
			return core.PatSeqElem{}, err
		}
		return core.PatSeqElem{Sub: sub, Rep: core.RepOptional}, nil
	case core.CombSeq:
		subDepth, rep := depth, core.RepOne
		if ellipsis {
			subDepth, rep = depth+1, core.RepEllipsis
		}
		group := make(core.PatGroup, len(cargs))
		for j, ip := range cargs {
			p, err := pc.compile(ip, subDepth)
			if err != nil {
				return core.PatSeqElem{}, err
			}
			group[j] = p
		}
		return core.PatSeqElem{Sub: group, Rep: rep}, nil
	}
	return core.PatSeqElem{}, pc.ctx.fail(elem.Span, DiagSyntaxShape, fmt.Sprintf("unexpected combinator %s", name))
}

func (pc *patCompiler) compileDict(n core.SyntaxDict, depth int) (core.Pattern, error) {
	entries := make(core.PatDict, 0, len(n))
	for _, e := range n {
		key, ok := literalDatum(e.Key)
		if !ok {
			return nil, pc.ctx.fail(e.Key.Span, DiagSyntaxShape, "dict pattern key must be a literal datum")
		}
		for _, prior := range entries {
			if core.DatumEqual(prior.Key, key) {
				return nil, pc.ctx.fail(e.Key.Span, DiagDuplicatePattern, fmt.Sprintf("duplicate key %v in dict pattern", key))
			}
		}
		val, err := pc.compile(e.Value, depth)
		if err != nil {
			return nil, err
		}
		entries = append(entries, core.PatDictEntry{Key: key, Value: val})
	}
	return entries, nil
}

func (pc *patCompiler) combinator(stx *core.Syntax, name core.Word, args []*core.Syntax, depth int) (core.Pattern, error) {
	switch name {
	case core.CombLiteral:
		if len(args) != 1 {
			return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, "~literal expects one identifier")
		}
		return core.PatLiteral{Ident: args[0]}, nil
	case core.CombDatum:
		if len(args) != 1 {
			return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, "~datum expects one datum")
		}
		return core.PatLit{Value: core.SyntaxToDatum(args[0])}, nil
	case core.CombAnd:
		subs, err := pc.compileAll(args, depth)
		if err != nil {
			return nil, err
		}
		return core.PatAnd(subs), nil
	case core.CombOr:
		subs, err := pc.compileAll(args, depth)
		if err != nil {
			return nil, err
		}
		return core.PatOr(subs), nil
	case core.CombNot:
		if len(args) != 1 {
			return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, "~not expects one pattern")
		}
		saved := pc.noBind
		pc.noBind = true
		sub, err := pc.compile(args[0], depth)
		pc.noBind = saved
		if err != nil {
			return nil, err
		}
		return core.PatNot{Sub: sub}, nil
	case core.CombDescribe:
		if len(args) != 2 {
			return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, "~describe expects a message and a pattern")
		}
		msg, ok := args[0].Node.(core.String)
		if !ok {
			return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, "~describe message must be a string")
		}
		sub, err := pc.compile(args[1], depth)
		if err != nil {
			return nil, err
		}
		return core.PatDescribe{Message: string(msg), Sub: sub}, nil
	case core.CombFail:
		if len(args) != 1 {
			return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, "~fail expects a message")
		}
		msg, ok := args[0].Node.(core.String)
		if !ok {
			return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, "~fail message must be a string")
		}
		return core.PatFail{Message: string(msg)}, nil
	case core.CombOptional, core.CombSeq:
		return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, fmt.Sprintf("%s is only valid as a sequence element", name))
	}
	return nil, pc.ctx.fail(stx.Span, DiagSyntaxShape, fmt.Sprintf("unknown combinator %s", name))
}

func (pc *patCompiler) compileAll(args []*core.Syntax, depth int) ([]core.Pattern, error) {
	out := make([]core.Pattern, len(args))
	for i, a := range args {
		p, err := pc.compile(a, depth)
		if err != nil {
			return nil, err
		}
		out[i] = p
	}
	return out, nil
}

func (pc *patCompiler) compilePin(inner *core.Syntax) (core.Pattern, error) {
	w, ok := inner.Node.(core.Word)
	if !ok {
		return nil, pc.ctx.fail(inner.Span, DiagSyntaxShape, "pin: argument must be an identifier")
	}
	b, r := pc.ctx.Bindings.Resolve(w, pc.ctx.Phase, DefaultSpace, inner.Scopes[pc.ctx.Phase])
	if r != ResolveFound {
		return nil, pc.ctx.fail(inner.Span, DiagUnbound, fmt.Sprintf("pin: unbound %s", w))
	}
	return core.PatPin{Ref: b.Ref()}, nil
}

func isEllipsisWord(stx *core.Syntax) bool {
	w, ok := stx.Node.(core.Word)
	return ok && w == "..."
}

// literalDatum reports whether stx wraps a scalar literal datum (atom, number,
// string, bytes, nil) — the only shapes permitted as dict pattern keys.
func literalDatum(stx *core.Syntax) (core.Datum, bool) {
	switch n := stx.Node.(type) {
	case core.Atom, core.Int, core.Float, core.String, core.Bytes, core.Nil:
		return n.(core.Datum), true
	}
	return nil, false
}
