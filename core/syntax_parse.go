package core

// Syntax-parse pattern combinators. They are written in source with the `%~`
// reader prefix (e.g. `(%~or a b)`) which the reader tokenizes as the head
// word `~or`. The expander and evaluator share these names so a pattern's
// attribute set is computed identically on both sides.
const (
	CombLiteral  Word = "~literal"
	CombDatum    Word = "~datum"
	CombAnd      Word = "~and"
	CombOr       Word = "~or"
	CombNot      Word = "~not"
	CombDescribe Word = "~describe"
	CombFail     Word = "~fail"
	CombOptional Word = "~optional"
	CombSeq      Word = "~seq"
)

// IsSyntaxParseCombinator reports whether w names a known pattern combinator.
func IsSyntaxParseCombinator(w Word) bool {
	switch w {
	case CombLiteral, CombDatum, CombAnd, CombOr, CombNot, CombDescribe, CombFail, CombOptional, CombSeq:
		return true
	}
	return false
}

// SyntaxParseCombinator returns the combinator name and argument subpatterns
// when stx is a list whose head is a known combinator keyword.
func SyntaxParseCombinator(stx *Syntax) (Word, []*Syntax, bool) {
	if lowered, ok := LowerReaderListSyntax(stx); ok {
		stx = lowered
	}
	elems, ok := SyntaxListElems(stx)
	if !ok || len(elems) == 0 {
		return "", nil, false
	}
	w, ok := elems[0].Node.(Word)
	if !ok || !IsSyntaxParseCombinator(w) {
		return "", nil, false
	}
	return w, elems[1:], true
}

// SyntaxParseAttributes returns the attributes (pattern variables) a
// syntax-parse pattern binds, in left-to-right first-occurrence order. The
// traversal understands the combinators, so e.g. `~literal`/`~datum`/`~fail`
// bind nothing, `~describe` exposes its inner pattern, and `~and`/`~or`/
// `~optional`/`~seq` expose their subpatterns. The expander uses it to check
// that every `~or` alternative binds the same attribute set; the compiled
// pattern itself (not this list) carries depth and drives matching.
func SyntaxParseAttributes(pattern *Syntax) []*Syntax {
	seen := map[Word]bool{}
	var out []*Syntax
	var walk func(*Syntax)
	walkSeq := func(elems []*Syntax) {
		for _, e := range elems {
			if w, ok := e.Node.(Word); ok && w == "..." {
				continue
			}
			walk(e)
		}
	}
	walk = func(stx *Syntax) {
		if lowered, ok := LowerReaderListSyntax(stx); ok {
			stx = lowered
		}
		if name, args, ok := SyntaxParseCombinator(stx); ok {
			switch name {
			case CombLiteral, CombDatum, CombFail, CombNot:
				// bind nothing
			case CombDescribe:
				if len(args) > 0 {
					walk(args[len(args)-1])
				}
			default: // ~and, ~or, ~optional, ~seq
				walkSeq(args)
			}
			return
		}
		switch n := stx.Node.(type) {
		case Word:
			if n == "_" || n == "..." || IsSyntaxParseCombinator(n) || seen[n] {
				return
			}
			seen[n] = true
			out = append(out, stx)
		case SyntaxPair:
			if elems, ok := SyntaxListElems(stx); ok {
				walkSeq(elems)
			}
		case SyntaxVector:
			walkSeq([]*Syntax(n))
		case SyntaxTuple:
			walkSeq([]*Syntax(n))
		case SyntaxDict:
			for _, e := range n {
				walk(e.Key)
				walk(e.Value)
			}
		}
	}
	walk(pattern)
	return out
}
