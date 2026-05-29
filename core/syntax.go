// Package core defines ISH's syntax substrate: datum kinds, syntax objects,
// scope sets, and the operations that compose them.
package core

import "sync/atomic"

// Syntax-bearing compound kinds. Children are *Syntax. SyntaxPair permits an
// improper tail (any *Syntax, not necessarily another SyntaxPair) — pairs are
// the primitive, lists are pairs ending in Nil, and dotted forms are pairs
// ending in anything else.
type (
	SyntaxPair      struct{ Head, Tail *Syntax }
	SyntaxVector    []*Syntax
	SyntaxTuple     []*Syntax
	SyntaxDict      []SyntaxDictEntry
	SyntaxDictEntry struct{ Key, Value *Syntax }
)

type Syntax struct {
	Node       any
	Span       Span
	Scopes     PhaseScopes
	Properties Properties
	Origin     []Origin
}

type Origin struct {
	Kind string
	Span Span
}

type Pos struct{ Line, Col, Byte int }

type Span struct {
	File       string
	Start, End Pos
}

// Scope is an opaque identity. Equality is the only meaningful operation.
type Scope uint64

// scopeCounter is intentionally process-wide and monotonic. Scope IDs are
// opaque equality tokens; stale IDs from one test don't poison another, so
// per-test isolation isn't needed. If reproducible serialized compilation
// artifacts ever become a goal, thread an ID source through Context.
var scopeCounter atomic.Uint64

// NewScope allocates a fresh scope distinct from any previously allocated.
func NewScope() Scope { return Scope(scopeCounter.Add(1)) }

// ScopeSet is a sorted, deduplicated, immutable set of scopes.
type ScopeSet []Scope

func (s ScopeSet) index(x Scope) int {
	lo, hi := 0, len(s)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if s[mid] < x {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func (s ScopeSet) Has(x Scope) bool {
	i := s.index(x)
	return i < len(s) && s[i] == x
}

func (s ScopeSet) Add(x Scope) ScopeSet {
	i := s.index(x)
	if i < len(s) && s[i] == x {
		return s
	}
	out := make(ScopeSet, len(s)+1)
	copy(out, s[:i])
	out[i] = x
	copy(out[i+1:], s[i:])
	return out
}

func (s ScopeSet) Remove(x Scope) ScopeSet {
	i := s.index(x)
	if i >= len(s) || s[i] != x {
		return s
	}
	out := make(ScopeSet, len(s)-1)
	copy(out, s[:i])
	copy(out[i:], s[i+1:])
	return out
}

func (s ScopeSet) Flip(x Scope) ScopeSet {
	if s.Has(x) {
		return s.Remove(x)
	}
	return s.Add(x)
}

// Subset reports whether every scope in s is also in t.
func (s ScopeSet) Subset(t ScopeSet) bool {
	if len(s) > len(t) {
		return false
	}
	i, j := 0, 0
	for i < len(s) && j < len(t) {
		switch {
		case s[i] == t[j]:
			i++
			j++
		case s[i] > t[j]:
			j++
		default:
			return false
		}
	}
	return i == len(s)
}

func (s ScopeSet) Equal(t ScopeSet) bool {
	if len(s) != len(t) {
		return false
	}
	for i := range s {
		if s[i] != t[i] {
			return false
		}
	}
	return true
}

type Phase int

const (
	PhaseRuntime Phase = 0
	PhaseExpand  Phase = 1
)

// PhaseScopes is the per-phase scope set on a Syntax node. A missing phase
// behaves as the empty set. Treated as immutable; updates return a copy.
type PhaseScopes map[Phase]ScopeSet

func (p PhaseScopes) with(ph Phase, set ScopeSet) PhaseScopes {
	out := make(PhaseScopes, len(p)+1)
	for k, v := range p {
		out[k] = v
	}
	if len(set) == 0 {
		delete(out, ph)
	} else {
		out[ph] = set
	}
	return out
}

func (p PhaseScopes) Add(ph Phase, s Scope) PhaseScopes    { return p.with(ph, p[ph].Add(s)) }
func (p PhaseScopes) Remove(ph Phase, s Scope) PhaseScopes { return p.with(ph, p[ph].Remove(s)) }
func (p PhaseScopes) Flip(ph Phase, s Scope) PhaseScopes   { return p.with(ph, p[ph].Flip(s)) }

// Properties is the open per-syntax-object metadata store. Keys are strings;
// values are Datums. Preserved-vs-non-preserved is not tracked at this layer;
// retrofit when serialization or cross-tool consumption demands it.
type Properties map[string]Datum

// Property keys with current writers. Add spec-named classes here only when a
// producer actually writes them.
const (
	PropReaderShape       = "reader-shape"
	PropTokenRaw          = "token-raw"
	PropTokenKind         = "token-kind"
	PropTokenLeadingSpace = "token-leading-space"
	PropTokenAdjacentPrev = "token-adjacent-previous"
)

type SyntaxError struct {
	Syntax  *Syntax
	Message string
}

func (e *SyntaxError) Error() string { return e.Message }

func (p Properties) Get(key string) (Datum, bool) { v, ok := p[key]; return v, ok }

func (p Properties) With(key string, value Datum) Properties {
	out := make(Properties, len(p)+1)
	for k, v := range p {
		out[k] = v
	}
	out[key] = value
	return out
}

// WalkScopes returns a new syntax tree with f applied to the PhaseScopes of
// every node. f must return the new PhaseScopes; inputs are left intact.
// Property values are not traversed: properties are metadata about the
// surrounding syntax, not part of the program, and per-property scope
// semantics belong to whoever defines the property.
func WalkScopes(stx *Syntax, f func(PhaseScopes) PhaseScopes) *Syntax {
	if stx == nil {
		return nil
	}
	out := &Syntax{
		Node:       stx.Node,
		Span:       stx.Span,
		Scopes:     f(stx.Scopes),
		Properties: stx.Properties,
		Origin:     append([]Origin(nil), stx.Origin...),
	}
	switch n := stx.Node.(type) {
	case App:
		args := make([]*Syntax, len(n.Args))
		for i, arg := range n.Args {
			args[i] = WalkScopes(arg, f)
		}
		out.Node = App{Callee: WalkScopes(n.Callee, f), Args: args}
	case Lambda:
		clauses := make([]LambdaClause, len(n.Clauses))
		for i, clause := range n.Clauses {
			clauses[i] = LambdaClause{
				Params: clause.Params,
				Guard:  WalkScopes(clause.Guard, f),
				Body:   WalkScopes(clause.Body, f),
			}
		}
		out.Node = Lambda{Clauses: clauses}
	case Bind:
		out.Node = Bind{Pattern: n.Pattern, Value: WalkScopes(n.Value, f)}
	case Begin:
		body := make([]*Syntax, len(n.Body))
		for i, e := range n.Body {
			body[i] = WalkScopes(e, f)
		}
		out.Node = Begin{Body: body}
	case Receive:
		clauses := make([]LambdaClause, len(n.Clauses))
		for i, clause := range n.Clauses {
			clauses[i] = LambdaClause{
				Params: clause.Params,
				Guard:  WalkScopes(clause.Guard, f),
				Body:   WalkScopes(clause.Body, f),
			}
		}
		out.Node = Receive{
			Clauses:      clauses,
			AfterTimeout: WalkScopes(n.AfterTimeout, f),
			AfterBody:    WalkScopes(n.AfterBody, f),
		}
	case SyntaxPair:
		out.Node = SyntaxPair{Head: WalkScopes(n.Head, f), Tail: WalkScopes(n.Tail, f)}
	case SyntaxVector:
		v := make(SyntaxVector, len(n))
		for i, e := range n {
			v[i] = WalkScopes(e, f)
		}
		out.Node = v
	case SyntaxTuple:
		v := make(SyntaxTuple, len(n))
		for i, e := range n {
			v[i] = WalkScopes(e, f)
		}
		out.Node = v
	case SyntaxDict:
		v := make(SyntaxDict, len(n))
		for i, e := range n {
			v[i] = SyntaxDictEntry{Key: WalkScopes(e.Key, f), Value: WalkScopes(e.Value, f)}
		}
		out.Node = v
	}
	return out
}

func CloneSyntax(stx *Syntax) *Syntax {
	if stx == nil {
		return nil
	}
	return WalkScopes(stx, func(scopes PhaseScopes) PhaseScopes { return scopes })
}

func AddScope(stx *Syntax, ph Phase, s Scope) *Syntax {
	return WalkScopes(stx, func(p PhaseScopes) PhaseScopes { return p.Add(ph, s) })
}

// FlipScope returns stx with scope s flipped at every node at phase ph.
// FlipScope is its own inverse: FlipScope(FlipScope(x, ph, s), ph, s) == x.
func FlipScope(stx *Syntax, ph Phase, s Scope) *Syntax {
	return WalkScopes(stx, func(p PhaseScopes) PhaseScopes { return p.Flip(ph, s) })
}

// BoundIdentEqual is the Flatt bound-identifier=? predicate: same word and
// same scope set at the given phase. Distinct from free-identifier=? (which
// resolves through bindings and lives on the resolver).
func BoundIdentEqual(a, b *Syntax, ph Phase) bool {
	wa, ok := a.Node.(Word)
	if !ok {
		return false
	}
	wb, ok := b.Node.(Word)
	if !ok {
		return false
	}
	return wa == wb && a.Scopes[ph].Equal(b.Scopes[ph])
}

// SyntaxToDatum lowers a syntax tree to a runtime datum, discarding scopes,
// span, and properties. It is a PURE structural conversion: it does not lower
// reader grouping forms (%-group/%-expr). Callers that convert reader output to
// data (quote, syntax->datum) apply LowerReaderSyntax first, so the reader-shape
// policy lives at those boundaries, not inside the substrate conversion.
func SyntaxToDatum(stx *Syntax) Datum {
	if stx == nil {
		return Nil{}
	}
	switch n := stx.Node.(type) {
	case SyntaxPair:
		return Pair{Head: SyntaxToDatum(n.Head), Tail: SyntaxToDatum(n.Tail)}
	case SyntaxVector:
		out := make(Vector, len(n))
		for i, e := range n {
			out[i] = SyntaxToDatum(e)
		}
		return out
	case SyntaxTuple:
		out := make(Tuple, len(n))
		for i, e := range n {
			out[i] = SyntaxToDatum(e)
		}
		return out
	case SyntaxDict:
		out := make(Dict, len(n))
		for i, e := range n {
			out[i] = DictEntry{Key: SyntaxToDatum(e.Key), Value: SyntaxToDatum(e.Value)}
		}
		return out
	default:
		if d, ok := n.(Datum); ok {
			return d
		}
		return Nil{}
	}
}

// LowerReaderSyntax recursively rewrites reader grouping forms (%-group, %-expr,
// and the dotted-`.` improper-list convention) into plain list/improper-list
// syntax throughout the tree. It is the explicit reader-lowering step applied at
// the syntax→datum boundary (quote, syntax->datum); SyntaxToDatum itself does no
// lowering.
func LowerReaderSyntax(stx *Syntax) *Syntax {
	if stx == nil {
		return nil
	}
	if lowered, ok := LowerReaderListSyntax(stx); ok {
		stx = lowered
	}
	rewrap := func(node any) *Syntax {
		return &Syntax{Node: node, Span: stx.Span, Scopes: stx.Scopes, Properties: stx.Properties, Origin: append([]Origin(nil), stx.Origin...)}
	}
	switch n := stx.Node.(type) {
	case SyntaxPair:
		return rewrap(SyntaxPair{Head: LowerReaderSyntax(n.Head), Tail: LowerReaderSyntax(n.Tail)})
	case SyntaxVector:
		out := make(SyntaxVector, len(n))
		for i, e := range n {
			out[i] = LowerReaderSyntax(e)
		}
		return rewrap(out)
	case SyntaxTuple:
		out := make(SyntaxTuple, len(n))
		for i, e := range n {
			out[i] = LowerReaderSyntax(e)
		}
		return rewrap(out)
	case SyntaxDict:
		out := make(SyntaxDict, len(n))
		for i, e := range n {
			out[i] = SyntaxDictEntry{Key: LowerReaderSyntax(e.Key), Value: LowerReaderSyntax(e.Value)}
		}
		return rewrap(out)
	default:
		return stx
	}
}

// DatumToSyntax wraps a datum as syntax, inheriting span, scopes, and
// properties from ctx. Runtime compound kinds become their syntax counterparts.
func DatumToSyntax(ctx *Syntax, d Datum) *Syntax {
	var span Span
	var scopes PhaseScopes
	var props Properties
	var origin []Origin
	if ctx != nil {
		span = ctx.Span
		scopes = ctx.Scopes
		props = ctx.Properties
		origin = append([]Origin(nil), ctx.Origin...)
	}
	wrap := func(node any) *Syntax {
		return &Syntax{Node: node, Span: span, Scopes: scopes, Properties: props, Origin: origin}
	}
	switch v := d.(type) {
	case *Syntax:
		return v
	case Pair:
		return wrap(SyntaxPair{Head: DatumToSyntax(ctx, v.Head), Tail: DatumToSyntax(ctx, v.Tail)})
	case Vector:
		out := make(SyntaxVector, len(v))
		for i, e := range v {
			out[i] = DatumToSyntax(ctx, e)
		}
		return wrap(out)
	case Tuple:
		out := make(SyntaxTuple, len(v))
		for i, e := range v {
			out[i] = DatumToSyntax(ctx, e)
		}
		return wrap(out)
	case Dict:
		out := make(SyntaxDict, len(v))
		for i, e := range v {
			out[i] = SyntaxDictEntry{Key: DatumToSyntax(ctx, e.Key), Value: DatumToSyntax(ctx, e.Value)}
		}
		return wrap(out)
	default:
		return wrap(v)
	}
}

// SyntaxList builds a proper-list SyntaxPair chain terminated by Nil. The
// caller supplies the span explicitly because a synthesized list has no
// natural span of its own — it belongs to whatever site requested it.
func SyntaxList(span Span, elems ...*Syntax) *Syntax {
	cur := &Syntax{Node: Nil{}, Span: span}
	for i := len(elems) - 1; i >= 0; i-- {
		cur = &Syntax{Node: SyntaxPair{Head: elems[i], Tail: cur}, Span: span}
	}
	return cur
}

func SyntaxImproperList(span Span, elems []*Syntax, tail *Syntax) *Syntax {
	cur := tail
	for i := len(elems) - 1; i >= 0; i-- {
		cur = &Syntax{Node: SyntaxPair{Head: elems[i], Tail: cur}, Span: span}
	}
	return cur
}

// ReaderExprElems returns the parts of a `%-expr` reader whitespace-expression
// form — the elements after the `%-expr` head — or false if stx is not a
// `%-expr` list. It is the single definition of "unwrap a `%-expr` form".
func ReaderExprElems(stx *Syntax) ([]*Syntax, bool) {
	elems, ok := SyntaxListElems(stx)
	if !ok || len(elems) == 0 {
		return nil, false
	}
	if w, ok := elems[0].Node.(Word); !ok || w != "%-expr" {
		return nil, false
	}
	return elems[1:], true
}

func LowerReaderListSyntax(stx *Syntax) (*Syntax, bool) {
	elems, ok := SyntaxListElems(stx)
	if !ok || len(elems) == 0 {
		return nil, false
	}
	head, ok := elems[0].Node.(Word)
	if !ok {
		return nil, false
	}
	switch head {
	case "%-group":
		if len(elems) == 1 {
			return &Syntax{Node: Nil{}, Span: stx.Span, Scopes: stx.Scopes, Properties: stx.Properties, Origin: append([]Origin(nil), stx.Origin...)}, true
		}
		if len(elems) == 2 {
			inner := elems[1]
			// Parentheses delimiting a whitespace-expression ARE that list:
			// `(a b c)` reads as a %-group wrapping a %-expr, so unwrap to the
			// list of its parts.
			if innerElems, isExpr := ReaderExprElems(inner); isExpr {
				return LowerReaderExprSyntax(inner.Span, innerElems), true
			}
			// Parentheses enclosing a single form are a one-element list in
			// data and pattern position: `(x)` is the list (x), and `((a b))`
			// is ((a b)). Expression grouping — where `(x)` denotes the value
			// x — is a distinct interpretation handled by the expander's
			// reader-group form (expandReaderGroup), not by this data lowering.
			return SyntaxList(stx.Span, inner), true
		}
	case "%-expr":
		return LowerReaderExprSyntax(stx.Span, elems[1:]), true
	}
	return nil, false
}

func LowerReaderExprSyntax(span Span, elems []*Syntax) *Syntax {
	for i, elem := range elems {
		if w, ok := elem.Node.(Word); ok && w == "." && i > 0 && i < len(elems)-1 {
			return SyntaxImproperList(span, elems[:i], elems[i+1])
		}
	}
	return SyntaxList(span, elems...)
}

// SyntaxApp builds explicit expanded application syntax.
func SyntaxApp(span Span, callee *Syntax, args ...*Syntax) *Syntax {
	return &Syntax{Node: App{Callee: callee, Args: append([]*Syntax(nil), args...)}, Span: span}
}

// ContainsExpandedCore reports whether stx contains executable expanded core
// IR rather than reader/data syntax. Quote and macro/template boundaries use
// this to prevent already-expanded evaluator nodes from being smuggled through
// places that expect source-shaped syntax.
func ContainsExpandedCore(stx *Syntax) bool {
	if stx == nil {
		return false
	}
	switch n := stx.Node.(type) {
	case App, Resolved, Lambda, Transformer, Bind, Begin, Receive, SyntaxParse, LetRec:
		return true
	case *Syntax:
		return ContainsExpandedCore(n)
	case Pair:
		return containsExpandedCoreDatum(n.Head) || containsExpandedCoreDatum(n.Tail)
	case Vector:
		for _, e := range n {
			if containsExpandedCoreDatum(e) {
				return true
			}
		}
	case Tuple:
		for _, e := range n {
			if containsExpandedCoreDatum(e) {
				return true
			}
		}
	case Dict:
		for _, e := range n {
			if containsExpandedCoreDatum(e.Key) || containsExpandedCoreDatum(e.Value) {
				return true
			}
		}
	case SyntaxPair:
		return ContainsExpandedCore(n.Head) || ContainsExpandedCore(n.Tail)
	case SyntaxVector:
		for _, e := range n {
			if ContainsExpandedCore(e) {
				return true
			}
		}
	case SyntaxTuple:
		for _, e := range n {
			if ContainsExpandedCore(e) {
				return true
			}
		}
	case SyntaxDict:
		for _, e := range n {
			if ContainsExpandedCore(e.Key) || ContainsExpandedCore(e.Value) {
				return true
			}
		}
	}
	return false
}

func containsExpandedCoreDatum(d Datum) bool {
	switch v := d.(type) {
	case nil:
		return false
	case *Syntax:
		return ContainsExpandedCore(v)
	case Pair:
		return containsExpandedCoreDatum(v.Head) || containsExpandedCoreDatum(v.Tail)
	case Vector:
		for _, e := range v {
			if containsExpandedCoreDatum(e) {
				return true
			}
		}
	case Tuple:
		for _, e := range v {
			if containsExpandedCoreDatum(e) {
				return true
			}
		}
	case Dict:
		for _, e := range v {
			if containsExpandedCoreDatum(e.Key) || containsExpandedCoreDatum(e.Value) {
				return true
			}
		}
	}
	return false
}

// SyntaxListElems walks a proper-list syntax chain into a slice. Returns
// ok=false on an improper list (non-Nil tail that isn't a SyntaxPair).
func SyntaxListElems(stx *Syntax) (elems []*Syntax, ok bool) {
	for {
		if stx == nil {
			return elems, true
		}
		switch n := stx.Node.(type) {
		case Nil:
			return elems, true
		case SyntaxPair:
			elems = append(elems, n.Head)
			stx = n.Tail
		default:
			return elems, false
		}
	}
}
