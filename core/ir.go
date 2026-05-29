package core

// BindingID is an opaque, stable identity for one binding occurrence in
// expanded core. The expander owns how IDs are allocated; the evaluator only
// needs equality and map lookup by this ID.
type BindingID uint64

// Resolved is an expanded identifier reference. Name preserves the source word
// for diagnostics; ID is the stable binding identity used by runtime frames;
// Value is the static binding payload for top-level/kernel bindings that are
// not present in a runtime frame. A Resolved appears only in expanded core
// syntax, never in reader output.
type Resolved struct {
	Name  Word
	ID    BindingID
	Value any
}

// Lambda is an expanded function. Every function — single-clause or
// multi-clause, anonymous or named, user-defined or as the target of `bind` or
// `match` — has the same shape: a list of LambdaClauses. At call time the
// evaluator tries each in order, selecting the first whose param patterns all
// match the arguments.
type Lambda struct{ Clauses []LambdaClause }

type Transformer struct {
	Clauses   []LambdaClause
	DefScopes PhaseScopes
}

type Protocol struct{ Value any }

// App is explicit expanded application. Reader syntax and quoted list data use
// SyntaxPair/Pair; only the expander emits App after choosing ordinary runtime
// function application.
type App struct {
	Callee *Syntax
	Args   []*Syntax
}

type LambdaClause struct {
	Params []Pattern
	Guard  *Syntax // nil = no guard; otherwise evaluated under pattern bindings
	Body   *Syntax
}

// Begin is an expanded sequence form: evaluate each Body element in order,
// yielding the value of the last. Empty Begin evaluates to Nil.
type Begin struct{ Body []*Syntax }

type Bind struct {
	Pattern Pattern
	Value   *Syntax
}

// LetRec binds a group of names mutually-recursively: every binding's value is
// evaluated in an environment that already contains all of the group's names,
// so the values (functions) can reference one another and themselves. The
// evaluator realizes this by creating the group's frame, letting each value
// (a closure) capture it, then filling it — one construction-time write, after
// which the frame is immutable. This is how recursion coexists with immutable
// environments (the letrec knot), and the sole shape `defn` groups compile to.
type LetRec struct {
	Bindings []LetRecBinding
}

type LetRecBinding struct {
	Ref   Resolved
	Value *Syntax
}

// Receive is an expanded selective-receive form. Each clause is a LambdaClause
// whose Params holds exactly one Pattern — the message pattern. The evaluator
// scans the current process's mailbox for the first message matching any
// clause's pattern; when matched, the message is removed and the clause body
// runs with the pattern's bindings. If AfterTimeout is non-nil and no message
// matches before it elapses, AfterBody runs instead.
type Receive struct {
	Clauses      []LambdaClause
	AfterTimeout *Syntax
	AfterBody    *Syntax
}

// SyntaxParse is an expanded syntax-parse/syntax-case form. The evaluator
// matches Target's value (a syntax object) against each clause's compiled
// Pattern in order; the first clause whose pattern matches — and whose Guard,
// if present, is truthy — binds its attributes into a frame and runs Body.
// Attributes are the pattern's variables, bound by the matcher and referenced
// by Guard/Body (only inside quasisyntax templates). Unlike fn, a non-matching
// or failing clause falls through to the next; if none match it is an error.
type SyntaxParse struct {
	Target  *Syntax
	Clauses []SyntaxParseClause
}

// SyntaxParseClause is one clause of a SyntaxParse: a compiled Pattern, an
// optional Guard (nil = none), and a Body, both evaluated under the attributes
// the Pattern binds.
type SyntaxParseClause struct {
	Pattern Pattern
	Guard   *Syntax
	Body    *Syntax
}
