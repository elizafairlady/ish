// Package expand performs ISH's surface-to-core elaboration: hygiene,
// macro invocation, binding resolution, and dispatch over expansion contexts.
package expand

import (
	"errors"

	"ish/core"
)

// ContextKind names the expansion contexts the language core distinguishes.
// Shell, make-like task languages, and other domain extensions live in
// packages: they install transformers and binding-space inhabitants rather
// than introducing new context kinds.
type ContextKind int

const (
	PackageCtx ContextKind = iota
	BodyCtx
	ExpressionCtx
	PatternCtx
	QuoteCtx
	SyntaxQuoteCtx
)

// BindingSpace is an interned name separating categories of bindings that
// should not collide even when textual names match. The core defines only
// the two spaces it consumes itself; packages introduce their own.
type BindingSpace string

const (
	DefaultSpace         BindingSpace = "default"
	PackageSpace         BindingSpace = "package"
	OperatorSpace        BindingSpace = "operator"
	ProtocolHandlerSpace BindingSpace = "protocol-handler"
)

// PackageID identifies the current expansion's owning package by canonical
// path. The empty value names the anonymous top-level context.
type PackageID string

// Context carries the state every expansion step consults. It is treated as
// immutable: child contexts are produced via Sub with selective overrides,
// and the central BindingTable is the only mutable shared state.
type Context struct {
	Kind     ContextKind
	Phase    core.Phase
	Space    BindingSpace
	Package  PackageID
	Scopes   core.PhaseScopes
	Bindings *BindingTable
	Packages *PackageRegistry
	Exports  map[core.Word]bool
	Diag     *Diagnostics
	// Macros, when non-nil, lets the `macro` core form evaluate transformer
	// bodies at phase-1. Nil disables `macro` use; the diagnostic surfaces.
	Macros MacroRunner
}

// ProtocolHandler is one active expansion protocol entry. It claims reader
// protocol forms such as %-expr in a specific expansion context. Its claim
// predicate and rewrite transformer come from the source `defprotocol`
// declaration (parseProtocolHandlerDecl); DefScopes carry the provider's
// definition-site scopes so the bodies resolve their own helpers by hygiene.
type ProtocolHandler struct {
	Form        core.Word
	Kind        ContextKind
	Phase       core.Phase
	Scopes      core.ScopeSet
	Claim       func(*core.Syntax) bool
	Transformer Transformer
	DefScopes   core.PhaseScopes
}

type OperatorAssociativity int

const (
	AssocNone OperatorAssociativity = iota
	AssocLeft
	AssocRight
)

// OperatorFixity is the operand arrangement an operator declares: infix
// (`a + b`), prefix (`- a`), or postfix (`a !`). It is set from the `%:fixity`
// metadata tag and drives the enforestation loop.
type OperatorFixity int

const (
	FixityInfix OperatorFixity = iota
	FixityPrefix
	FixityPostfix
)

// OperatorTransformer lowers an operator application to a headed form. It
// receives the operator use-site syntax and the operands in source order (two
// for infix, one for prefix/postfix), so a single lowering shape serves every
// fixity.
type OperatorTransformer func(stx *core.Syntax, operands []*core.Syntax) (*core.Syntax, error)

type OperatorEntry struct {
	Token       core.Word
	Kind        ContextKind
	Phase       core.Phase
	Scopes      core.ScopeSet
	Precedence  int
	Assoc       OperatorAssociativity
	Fixity      OperatorFixity
	Transformer OperatorTransformer
	DefScopes   core.PhaseScopes
}

// NewContext constructs a fresh package-context root. Callers supply the
// binding table and diagnostic sink so distinct compilation units can share
// or isolate them as needed.
func NewContext(pkg PackageID, bindings *BindingTable, diag *Diagnostics) *Context {
	ctx := &Context{
		Kind:     PackageCtx,
		Phase:    core.PhaseRuntime,
		Space:    DefaultSpace,
		Package:  pkg,
		Scopes:   core.PhaseScopes{},
		Bindings: bindings,
		Packages: NewPackageRegistry(),
		Exports:  map[core.Word]bool{},
		Diag:     diag,
	}
	return ctx
}

// ContextOption configures a child Context produced by Sub.
type ContextOption func(*Context)

func WithKind(k ContextKind) ContextOption   { return func(c *Context) { c.Kind = k } }
func WithSpace(s BindingSpace) ContextOption { return func(c *Context) { c.Space = s } }
func WithPhase(p core.Phase) ContextOption   { return func(c *Context) { c.Phase = p } }
func WithAddedScope(s core.Scope) ContextOption {
	return func(c *Context) { c.Scopes = c.Scopes.Add(c.Phase, s) }
}

// Sub returns a child Context inheriting from c with the given overrides.
// All context transitions flow through this entry point so that future
// invariants (e.g., automatic phase shifts on quote entry) have one site to
// enforce them.
func (c *Context) Sub(opts ...ContextOption) *Context {
	out := *c
	for _, opt := range opts {
		opt(&out)
	}
	return &out
}

// IntroduceScope allocates a fresh scope and returns it together with a
// child context whose Scopes include it at the current phase. Used at every
// binding region entry: function bodies, let bindings, do blocks, package
// bodies, and macro use sites.
func (c *Context) IntroduceScope() (core.Scope, *Context) {
	s := core.NewScope()
	return s, c.Sub(WithAddedScope(s))
}

// Diagnostic is a single expansion error or warning bound to a source span.
type Diagnostic struct {
	Severity DiagnosticSeverity
	Span     core.Span
	Kind     DiagnosticKind
	Message  string
}

type DiagnosticSeverity int

const (
	SeverityError DiagnosticSeverity = iota
	SeverityWarning
)

// DiagnosticKind enumerates the language-core diagnostic cases. Only kinds
// with current producers are declared; add more when a producer arrives.
// Domain extensions mint their own kinds as string literals.
type DiagnosticKind string

const (
	DiagUnbound           DiagnosticKind = "unbound-identifier"
	DiagAmbiguous         DiagnosticKind = "ambiguous-binding"
	DiagInvalidContext    DiagnosticKind = "syntax-in-invalid-context"
	DiagSyntaxArity       DiagnosticKind = "syntax-arity"
	DiagSyntaxShape       DiagnosticKind = "syntax-shape"
	DiagBadMacroResult    DiagnosticKind = "macro-returned-invalid-syntax"
	DiagDuplicatePattern  DiagnosticKind = "duplicate-binding-in-pattern"
	DiagProtocolUnhandled DiagnosticKind = "protocol-unhandled"
	DiagProtocolAmbiguous DiagnosticKind = "protocol-ambiguous"
)

// Diagnostics collects expansion diagnostics. Callers append; consumers read.
type Diagnostics struct{ Items []Diagnostic }

func (d *Diagnostics) Error(span core.Span, kind DiagnosticKind, msg string) {
	d.Items = append(d.Items, Diagnostic{Severity: SeverityError, Span: span, Kind: kind, Message: msg})
}

// fail records a diagnostic and returns an error carrying the same message, so
// the user-facing diagnostic and the propagated error never drift apart. Every
// expansion failure site uses this single helper instead of authoring the
// message twice.
func (c *Context) fail(span core.Span, kind DiagnosticKind, msg string) error {
	c.Diag.Error(span, kind, msg)
	return errors.New(msg)
}

func (d *Diagnostics) HasErrors() bool {
	for _, item := range d.Items {
		if item.Severity == SeverityError {
			return true
		}
	}
	return false
}
