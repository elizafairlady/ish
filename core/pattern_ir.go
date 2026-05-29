package core

// Pattern is a compiled match form, shared by every place the language matches
// a term against a shape: fn parameters, bind, match/case clauses, receive
// clauses, and syntax-parse/syntax-case. One matcher interprets these against
// either a runtime value or a syntax object.
//
// Value patterns (fn/match/bind/receive) use only the structural subset:
// PatWild, PatLit, PatVar, PatPin, PatSeq (all-RepOne), and PatDict. Syntax
// patterns (syntax-parse/case) additionally use ellipsis/optional repetition,
// PatGroup, and the combinator nodes PatLiteral/PatAnd/PatOr/PatNot/
// PatDescribe/PatFail.
type Pattern interface {
	pattern()
}

// PatWild matches any term without binding (`_`).
type PatWild struct{}

// PatLit matches a term equal to Value. Against a value target the comparison
// is structural; against a syntax target the target is reduced to its datum
// first. It is also the compiled form of the `~datum` combinator.
type PatLit struct{ Value Datum }

// PatVar matches any term and binds it to Ref's binding. For value patterns the
// bound term is the runtime value; for syntax patterns it is the captured
// syntax (a nested SyntaxVector when Ref is an ellipsis attribute).
type PatVar struct{ Ref Resolved }

// PatPin matches a term equal to the current value of Ref's binding; binds
// nothing.
type PatPin struct{ Ref Resolved }

// SeqKind distinguishes the three ordered-sequence shapes a PatSeq matches.
type SeqKind int

const (
	// SeqList matches a proper/improper list (Pair/Nil or SyntaxPair/Nil).
	SeqList SeqKind = iota
	// SeqVector matches a Vector or SyntaxVector.
	SeqVector
	// SeqTuple matches a Tuple or SyntaxTuple.
	SeqTuple
)

// RepKind is how often a sequence element matches.
type RepKind int

const (
	// RepOne matches exactly one occurrence (the only kind in value patterns).
	RepOne RepKind = iota
	// RepEllipsis matches zero or more occurrences (`x ...`), binding each
	// contained attribute to a SyntaxVector one level deeper.
	RepEllipsis
	// RepOptional matches zero or one occurrence (`~optional`); contained
	// attributes bind to Nil when absent.
	RepOptional
)

// PatSeqElem is one element of a PatSeq together with its repetition.
type PatSeqElem struct {
	Sub Pattern
	Rep RepKind
}

// PatSeq matches an ordered sequence. Tail matches the improper tail of a
// SeqList (PatLit{Nil} for a proper list); it is nil for vectors and tuples.
type PatSeq struct {
	Kind  SeqKind
	Elems []PatSeqElem
	Tail  Pattern
}

// PatGroup is a fixed-width inline group (the `~seq` combinator): it matches
// len(PatGroup) consecutive sequence elements. It appears only as a
// PatSeqElem.Sub, so its width is known statically.
type PatGroup []Pattern

// PatDict matches a Dict/SyntaxDict containing each literal-keyed entry with a
// matching value; extra keys in the target do not disqualify the match.
type PatDict []PatDictEntry

// PatDictEntry is one element of a PatDict. Keys are literal datums
// (compile-time known); only values are patterns.
type PatDictEntry struct {
	Key   Datum
	Value Pattern
}

// PatLiteral matches an identifier that is free-identifier=? to Ident (the
// `~literal` combinator, and the compiled form of a syntax-case literal). Ident
// retains its scopes so the comparison resolves both sides.
type PatLiteral struct{ Ident *Syntax }

// PatAnd matches when every sub-pattern matches the same target (`~and`).
type PatAnd []Pattern

// PatOr matches when any alternative matches, committing the first that does
// (`~or`); the matcher backtracks across alternatives.
type PatOr []Pattern

// PatNot matches when Sub does not match, binding nothing (`~not`).
type PatNot struct{ Sub Pattern }

// PatDescribe matches Sub, replacing its failure message with Message
// (`~describe`).
type PatDescribe struct {
	Message string
	Sub     Pattern
}

// PatFail always fails with Message (`~fail`).
type PatFail struct{ Message string }

func (PatWild) pattern()     {}
func (PatLit) pattern()      {}
func (PatVar) pattern()      {}
func (PatPin) pattern()      {}
func (PatSeq) pattern()      {}
func (PatGroup) pattern()    {}
func (PatDict) pattern()     {}
func (PatLiteral) pattern()  {}
func (PatAnd) pattern()      {}
func (PatOr) pattern()       {}
func (PatNot) pattern()      {}
func (PatDescribe) pattern() {}
func (PatFail) pattern()     {}
