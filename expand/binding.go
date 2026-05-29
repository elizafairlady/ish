package expand

import (
	"sync/atomic"

	"ish/core"
)

// BindingID is an opaque, stable identity for a binding. Distinct from
// core.Scope: a binding's scope *set* records where it is visible; its ID
// records who it is. The type is defined in core so expanded syntax and eval
// do not depend on expander binding records.
type BindingID = core.BindingID

// bindingCounter is intentionally process-wide; see the note on
// core.scopeCounter for the rationale.
var bindingCounter atomic.Uint64

func newBindingID() BindingID { return BindingID(bindingCounter.Add(1)) }

// BindingKind classifies what expansion does after finding a binding. Domain
// extensions install TransformerBindings to override CoreFormBindings via
// scope-set specificity rather than adding new kinds here.
type BindingKind int

const (
	ValueBinding BindingKind = iota
	CoreFormBinding
	CompileTimeFormBinding
	TransformerBinding
	ProtocolBinding
	ProtocolHandlerBinding
	OperatorBinding
	PackageBinding
	// AttributeBinding is a syntax-parse/syntax-case pattern variable. Its
	// Value is an Attribute carrying the variable's ellipsis depth. It resolves
	// only inside a quasisyntax template (via %,/%,@); used as an ordinary
	// expression it is rejected by the non-value-binding check in expand.
	AttributeBinding
)

// Attribute is the payload of an AttributeBinding: the ellipsis depth at which
// the pattern variable was bound (0 = matched a single term, 1 = under one
// ellipsis, and so on). Templates check trailing-`...` counts against it.
type Attribute struct{ Depth int }

// Binding records a single lexical binding in the central table.
//
// Value is the binding's payload. Its dynamic type depends on Kind:
//   - ValueBinding:       a resolved runtime reference (caller-defined)
//   - CoreFormBinding:    a CoreFormHandler closure
//   - TransformerBinding: a Transformer closure
//   - PackageBinding:     a PackageID (or richer package descriptor)
type Binding struct {
	ID     BindingID
	Name   core.Word
	Phase  core.Phase
	Space  BindingSpace
	Scopes core.ScopeSet
	Kind   BindingKind
	Value  any
}

// BindingTable is the central binding store. Multiple bindings may share
// (name, phase, space); resolution disambiguates by scope-set specificity
// using Flatt's set-of-scopes rule. The table is append-only: shadowing is
// expressed by introducing a binding with a strict-superset scope set.
type BindingTable struct{ bindings []*Binding }

func NewBindingTable() *BindingTable { return &BindingTable{} }

func (t *BindingTable) Len() int { return len(t.bindings) }

func (t *BindingTable) BindingsSince(n int) []*Binding {
	if n < 0 || n > len(t.bindings) {
		return nil
	}
	out := make([]*Binding, len(t.bindings[n:]))
	copy(out, t.bindings[n:])
	return out
}

func (t *BindingTable) Bindings() []*Binding {
	out := make([]*Binding, len(t.bindings))
	copy(out, t.bindings)
	return out
}

// Define appends a new binding and returns it. No validation: shadowing,
// duplicates, and ambiguity are decided at resolution time.
func (t *BindingTable) Define(name core.Word, ph core.Phase, sp BindingSpace, scopes core.ScopeSet, kind BindingKind, value any) *Binding {
	b := &Binding{
		ID: newBindingID(), Name: name, Phase: ph, Space: sp,
		Scopes: scopes, Kind: kind, Value: value,
	}
	t.bindings = append(t.bindings, b)
	return b
}

// Ref builds the expanded reference node for this binding. It is the single
// mapping from a resolved binding to a core.Resolved, so the reference shape is
// defined in exactly one place.
func (b *Binding) Ref() core.Resolved {
	return core.Resolved{Name: b.Name, ID: b.ID, Value: b.Value}
}

// ResolveResult reports the outcome of a resolution attempt.
type ResolveResult int

const (
	ResolveUnbound ResolveResult = iota
	ResolveAmbiguous
	ResolveFound
)

// Resolve implements specp2 §Scopes And Hygiene resolution:
//  1. Candidates have matching (name, phase, space).
//  2. Filter to candidates whose binding scope set ⊆ reference scope set.
//  3. A candidate "dominates" iff every other candidate's scope set is a
//     (non-strict) subset of its own. The unique dominator wins.
//
// Two dominators can only exist if they share scope sets (since dominance
// requires mutual subset, which forces equality); distinct bindings with
// equal scope sets are ambiguous per spec line 105–107 ("strict superset").
func (t *BindingTable) Resolve(name core.Word, ph core.Phase, sp BindingSpace, refScopes core.ScopeSet) (*Binding, ResolveResult) {
	return t.ResolveClaim(name, ph, sp, refScopes, nil)
}

// ResolveClaim is Resolve with an extra candidate-acceptance predicate. A
// binding is a candidate only if it matches (name, phase, space), its scope set
// ⊆ refScopes, and accept reports true. The same scope-domination rule then
// picks the winner. This is the one resolver: ordinary name resolution passes
// accept == nil; reader-form handler resolution passes a predicate that asks
// whether the handler's Claim wants the candidate syntax. It stays pure — accept
// is caller-supplied and, for handlers, only reads the (shared, append-only)
// table — so there is no second dominance loop to drift.
func (t *BindingTable) ResolveClaim(name core.Word, ph core.Phase, sp BindingSpace, refScopes core.ScopeSet, accept func(*Binding) bool) (*Binding, ResolveResult) {
	var candidates []*Binding
	for _, b := range t.bindings {
		if b.Name == name && b.Phase == ph && b.Space == sp && b.Scopes.Subset(refScopes) {
			if accept != nil && !accept(b) {
				continue
			}
			candidates = append(candidates, b)
		}
	}
	return dominant(candidates)
}

// dominant selects the unique binding whose scope set is a (non-strict)
// superset of every other candidate's, or reports unbound/ambiguous. It is the
// single implementation of the set-of-scopes specificity rule, shared by every
// resolution path.
func dominant(candidates []*Binding) (*Binding, ResolveResult) {
	if len(candidates) == 0 {
		return nil, ResolveUnbound
	}
	var best *Binding
	for _, b := range candidates {
		dominates := true
		for _, other := range candidates {
			if other == b {
				continue
			}
			if !other.Scopes.Subset(b.Scopes) {
				dominates = false
				break
			}
		}
		if dominates {
			if best != nil {
				return nil, ResolveAmbiguous
			}
			best = b
		}
	}
	if best == nil {
		return nil, ResolveAmbiguous
	}
	return best, ResolveFound
}

// CoreFormHandler implements a core syntactic form. It receives the use-site
// syntax and the expansion context, and returns expanded syntax or an error.
type CoreFormHandler func(stx *core.Syntax, ctx *Context) (*core.Syntax, error)

// Transformer implements a user-defined macro. It receives the use-site
// syntax (after use-site scopes have been applied by the expander) and the
// use-site expansion context (so the macro body can reach local-expand/bind!),
// and returns rewritten syntax for re-expansion.
type Transformer func(stx *core.Syntax, ctx *Context) (*core.Syntax, error)

// SyntaxTransformer is the single dynamic shape of a TransformerBinding's value:
// the macro function plus its definition-site scopes (nil when none). Storing
// one shape means consumers never branch on representation.
type SyntaxTransformer struct {
	Fn        Transformer
	DefScopes core.PhaseScopes
}

func transformerValue(v any) (Transformer, core.PhaseScopes, bool) {
	t, ok := v.(*SyntaxTransformer)
	if !ok || t == nil {
		return nil, nil, false
	}
	return t.Fn, t.DefScopes, true
}

// MacroRunner bridges expand and eval without an import cycle. The macro
// core form expands the macro body at phase-1, then asks the runner to
// evaluate that body and wrap the resulting closure as a Transformer. The
// runner is supplied by whoever wires the language together (REPL, test
// harness) and stored on Context.
type MacroRunner interface {
	EvaluateTransformer(body *core.Syntax, ctx *Context) (Transformer, error)
}
