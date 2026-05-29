package expand

import (
	"errors"
	"fmt"

	"ish/core"
)

// InstallKernel registers the core forms into tbl at the empty scope set.
// The empty set is a subset of every reference's scope set, so kernel
// bindings are visible everywhere by Flatt's rule unless shadowed by a
// strict-superset binding at the import region's scope.
func InstallKernel(tbl *BindingTable) {
	for _, ph := range []core.Phase{core.PhaseRuntime, core.PhaseExpand} {
		def := func(name string, h CoreFormHandler) {
			tbl.Define(core.Word(name), ph, DefaultSpace, core.ScopeSet{}, CoreFormBinding, h)
		}
		defC := func(name string, h CoreFormHandler) {
			tbl.Define(core.Word(name), ph, DefaultSpace, core.ScopeSet{}, CompileTimeFormBinding, h)
		}
		defT := func(name string, t Transformer) {
			tbl.Define(core.Word(name), ph, DefaultSpace, core.ScopeSet{}, TransformerBinding, &SyntaxTransformer{Fn: t})
		}
		def("quote", quoteForm)
		def("quasiquote", quasiquoteForm)
		def("unquote", invalidQuoteEscapeForm("unquote outside quasiquote"))
		def("unquote-splicing", invalidQuoteEscapeForm("unquote-splicing outside quasiquote"))
		def("syntax", syntaxForm)
		def("quasisyntax", quasisyntaxForm)
		def("syntax-case", syntaxCaseForm)
		def("syntax-parse", syntaxParseForm)
		def("unsyntax", invalidQuoteEscapeForm("unsyntax outside quasisyntax"))
		def("unsyntax-splicing", invalidQuoteEscapeForm("unsyntax-splicing outside quasisyntax"))
		def("fn", fnForm)
		def("do", doForm)
		def("bind", bindForm)
		def("receive", receiveForm)
		def("match", matchForm)
		def("macro", macroForm)
		def("protocol", protocolForm)
		defC("defmacro", defmacroForm)
		defC("defprotocol", defprotocolForm)
		defC("import", importForm)
		defC("use", useForm)
		defC("implements", implementsForm)
		defC("export", exportForm)
		defT("case", caseTransformer)
	}
}

func invalidQuoteEscapeForm(message string) CoreFormHandler {
	return func(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
		return nil, ctx.fail(stx.Span, DiagInvalidContext, message)
	}
}

func importForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || (len(elems) != 2 && len(elems) != 4) {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "import expects: package [as alias]")
	}
	var alias core.Word
	if len(elems) == 4 {
		as, ok := elems[2].Node.(core.Word)
		if !ok || as != "as" {
			return nil, ctx.fail(elems[2].Span, DiagSyntaxShape, "import alias must use: as alias")
		}
		var aliasOK bool
		alias, aliasOK = elems[3].Node.(core.Word)
		if !aliasOK {
			return nil, ctx.fail(elems[3].Span, DiagSyntaxShape, "import alias must be a word")
		}
	}
	if err := importPackage(elems[1], alias, ctx); err != nil {
		ctx.Diag.Error(elems[1].Span, DiagInvalidContext, err.Error())
		return nil, err
	}
	return &core.Syntax{Node: core.Nil{}, Span: stx.Span}, nil
}

func useForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) != 2 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "use expects: package")
	}
	if err := usePackage(elems[1], ctx); err != nil {
		ctx.Diag.Error(elems[1].Span, DiagInvalidContext, err.Error())
		return nil, err
	}
	return &core.Syntax{Node: core.Nil{}, Span: stx.Span}, nil
}

func implementsForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) < 2 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "implements expects: a package/protocol, or an inline `protocol`/`defprotocol`")
	}
	// Inline-protocol prefix forms: define-and-activate (`implements defprotocol
	// NAME do … end`) or activate-anonymous (`implements protocol do … end`),
	// activated in the current scope/phase like any user `implements`.
	if kw, ok := elems[1].Node.(core.Word); ok {
		switch kw {
		case "defprotocol":
			if len(elems) != 4 {
				return nil, ctx.fail(stx.Span, DiagSyntaxShape, "implements defprotocol expects: name body")
			}
			name, ok := elems[2].Node.(core.Word)
			if !ok {
				return nil, ctx.fail(elems[2].Span, DiagSyntaxShape, "defprotocol name must be a word")
			}
			p, err := parseProtocolBody(elems[3], ctx)
			if err != nil {
				return nil, err
			}
			defineProtocol(name, p, ctx)
			activateProtocol(p, ctx, ctx.Phase)
			return &core.Syntax{Node: core.Nil{}, Span: stx.Span}, nil
		case "protocol":
			if len(elems) != 3 {
				return nil, ctx.fail(stx.Span, DiagSyntaxShape, "implements protocol expects: body")
			}
			p, err := parseProtocolBody(elems[2], ctx)
			if err != nil {
				return nil, err
			}
			activateProtocol(p, ctx, ctx.Phase)
			return &core.Syntax{Node: core.Nil{}, Span: stx.Span}, nil
		}
	}
	if len(elems) != 2 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "implements expects a single package/protocol path")
	}
	if err := implementsPackage(elems[1], ctx); err != nil {
		ctx.Diag.Error(elems[1].Span, DiagInvalidContext, err.Error())
		return nil, err
	}
	return &core.Syntax{Node: core.Nil{}, Span: stx.Span}, nil
}

func exportForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) < 2 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "export expects at least one name")
	}
	for _, elem := range elems[1:] {
		name, ok := elem.Node.(core.Word)
		if !ok {
			return nil, ctx.fail(elem.Span, DiagSyntaxShape, "export name must be a word")
		}
		ctx.Exports[name] = true
	}
	return &core.Syntax{Node: core.Nil{}, Span: stx.Span}, nil
}

// macroForm implements `(macro NAME [STX] BODY)`. Compiles the body as a
// single-clause Lambda at phase-1, asks the context's MacroRunner to
// evaluate it to a Closure-wrapped Transformer, then installs that
// transformer as a TransformerBinding at the current scope set. The form
// itself emits no runtime syntax (returns Nil).
func defmacroForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok {
		return nil, ctx.fail(stx.Span, DiagSyntaxShape, "defmacro expects a proper form")
	}
	nameStx, macroStx, err := defmacroParts(stx, elems, ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := nameStx.Node.(core.Word); !ok {
		return nil, ctx.fail(nameStx.Span, DiagSyntaxShape, "defmacro name must be an identifier")
	}
	_, _, err = expandSequentialMatchBind(stx.Span, nameStx, macroStx, ctx)
	if err != nil {
		return nil, err
	}
	return &core.Syntax{Node: core.Nil{}, Span: stx.Span}, nil
}

func defmacroParts(stx *core.Syntax, elems []*core.Syntax, ctx *Context) (name, macroStx *core.Syntax, err error) {
	// `defmacro name params -> expr` or `defmacro name params do ... end`. Both
	// surfaces are delegated to `macro` (which parses arrow vs do body), so the
	// surface rule — pattern -> expr OR pattern do ... end, never `-> do` — holds
	// in one place.
	if len(elems) < 4 {
		return nil, nil, ctx.fail(stx.Span, DiagSyntaxShape, "defmacro expects: name params -> expr  or  name params do ... end")
	}
	parts := make([]*core.Syntax, 0, len(elems)-1)
	parts = append(parts, wordSyntax("macro", elems[0].Span))
	parts = append(parts, elems[2:]...)
	return elems[1], core.SyntaxList(stx.Span, parts...), nil
}

func macroForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok {
		return nil, ctx.fail(stx.Span, DiagSyntaxShape, "macro expects a proper form")
	}
	paramsStx, bodyStx, err := macroParts(stx, elems, ctx)
	if err != nil {
		return nil, err
	}
	defScopes := stx.Scopes
	if len(defScopes[ctx.Phase]) == 0 {
		if params, ok := paramsStx.Node.(core.SyntaxVector); ok && len(params) > 0 {
			defScopes = params[0].Scopes
		} else {
			defScopes = bodyStx.Scopes
		}
	}
	expandCtx := ctx.Sub(WithPhase(core.PhaseExpand))
	clause, err := compileFnClause(paramsStx, nil, bodyStx, expandCtx)
	if err != nil {
		return nil, err
	}
	return &core.Syntax{Node: core.Transformer{Clauses: []core.LambdaClause{clause}, DefScopes: core.PhaseScopes{ctx.Phase: defScopes[ctx.Phase]}}, Span: stx.Span}, nil
}

func installTransformerBinding(nameStx *core.Syntax, transformerCore core.Transformer, ctx *Context) error {
	if ctx.Macros == nil {
		return ctx.fail(nameStx.Span, DiagInvalidContext, "macro: no MacroRunner installed on context")
	}
	name, ok := nameStx.Node.(core.Word)
	if !ok {
		return ctx.fail(nameStx.Span, DiagSyntaxShape, "macro binding name must be an identifier")
	}
	lambda := &core.Syntax{Node: core.Lambda{Clauses: transformerCore.Clauses}, Span: nameStx.Span}
	transformer, err := ctx.Macros.EvaluateTransformer(lambda, ctx)
	if err != nil {
		ctx.Diag.Error(nameStx.Span, DiagBadMacroResult, err.Error())
		return err
	}
	ctx.Bindings.Define(name, ctx.Phase, ctx.Space, ctx.Scopes[ctx.Phase], TransformerBinding, &SyntaxTransformer{Fn: transformer, DefScopes: transformerCore.DefScopes})
	return nil
}

func macroParts(stx *core.Syntax, elems []*core.Syntax, ctx *Context) (params, body *core.Syntax, err error) {
	// Arrow form first: `macro params -> expr`. The single body expression may
	// itself be a `do ... end` block (e.g. `-> syntax-case x do ... end`), so a
	// trailing do block here belongs to that expression, not to the macro — which
	// is exactly why the arrow check must precede the do-body check.
	arrow := findArrow(elems, 1)
	if arrow >= 0 {
		if arrow == 1 || arrow == len(elems)-1 {
			return nil, nil, ctx.fail(stx.Span, DiagSyntaxShape, "macro arrow form expects parameters before -> and body after")
		}
		return &core.Syntax{Node: core.SyntaxVector(elems[1:arrow]), Span: stx.Span}, readerExprCandidate(stx.Span, elems[arrow+1:]), nil
	}
	// do-body form: `macro params do ... end` (one or more params, then a body).
	if len(elems) >= 3 {
		if forms, ok := bodyForms(elems[len(elems)-1]); ok {
			params = &core.Syntax{Node: core.SyntaxVector(elems[1 : len(elems)-1]), Span: stx.Span}
			return params, readerDoExpr(elems[len(elems)-1].Span, forms), nil
		}
	}
	return nil, nil, ctx.fail(stx.Span, DiagSyntaxShape, "macro expects: params -> expr  or  params do ... end")
}

func protocolForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) != 2 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "protocol expects a body")
	}
	p, err := parseProtocolBody(elems[1], ctx)
	if err != nil {
		return nil, err
	}
	return &core.Syntax{Node: core.Protocol{Value: p}, Span: stx.Span}, nil
}

func defprotocolForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) != 3 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "defprotocol expects: name body")
	}
	name, ok := elems[1].Node.(core.Word)
	if !ok {
		return nil, ctx.fail(elems[1].Span, DiagSyntaxShape, "defprotocol name must be a word")
	}
	p, err := parseProtocolBody(elems[2], ctx)
	if err != nil {
		return nil, err
	}
	defineProtocol(name, p, ctx)
	return &core.Syntax{Node: core.Nil{}, Span: stx.Span}, nil
}

// defineProtocol binds a protocol value to name in the current context. Used
// by both `defprotocol` and the anonymous-protocol-bound-to-name path so the
// two cannot drift in how a protocol binding is installed.
func defineProtocol(name core.Word, p ProtocolExport, ctx *Context) {
	ctx.Bindings.Define(name, ctx.Phase, ctx.Space, ctx.Scopes[ctx.Phase], ProtocolBinding, p)
}

func parseProtocolBody(body *core.Syntax, ctx *Context) (ProtocolExport, error) {
	elems, ok := bodyForms(body)
	if !ok {
		return ProtocolExport{}, ctx.fail(body.Span, DiagSyntaxShape, "protocol body must be do block")
	}
	var p ProtocolExport
	for _, form := range elems {
		switch {
		case isProtocolDecl(form, "handler"):
			handlers, err := parseProtocolHandlerDecl(form, ctx)
			if err != nil {
				return ProtocolExport{}, err
			}
			p.Handlers = append(p.Handlers, handlers...)
		case isProtocolDecl(form, "operator"):
			op, err := parseOperatorDecl(form, ctx)
			if err != nil {
				return ProtocolExport{}, err
			}
			p.Operators = append(p.Operators, op...)
		default:
			return ProtocolExport{}, ctx.fail(form.Span, DiagSyntaxShape, "protocol declaration must be `operator TOKEN %:... -> target` or `handler FORM claims PRED -> TRANSFORM`")
		}
	}
	return p, nil
}

// parseOperatorDecl parses a metadata-tagged operator declaration:
//
//	operator TOKEN %:precedence N [%:assoc left|right|none] [%:fixity infix|prefix|postfix] [%:context all|expr|body|package] -> TARGET
//
// `%:precedence` is required; assoc defaults to left, fixity to infix, context
// to all (the operator is active in expression, body, and package contexts).
// The metadata-tag form is the operator analogue of the `handler` declaration:
// every configurable facet is a named tag, so new facets are additive rather
// than positional. TARGET is an unqualified identifier resolved in the consumer
// via provider def-scopes; the lowering is `(TARGET operands...)`.
func parseOperatorDecl(stx *core.Syntax, ctx *Context) ([]OperatorEntry, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) < 4 {
		return nil, ctx.fail(stx.Span, DiagSyntaxShape, "operator expects: operator TOKEN %:... -> target  or  operator TOKEN do ... end")
	}
	token, ok := elems[2].Node.(core.Word)
	if !ok {
		return nil, ctx.fail(elems[2].Span, DiagSyntaxShape, "operator token must be a word")
	}
	// Clause form: `operator TOKEN do <clause>... end`. Each clause is one
	// fixity, so a single token can be e.g. both infix (`a - b` -> sub) and
	// prefix (`- b` -> neg); the enforestation selects by position. At most one
	// clause per fixity.
	if forms, ok := bodyForms(elems[len(elems)-1]); ok && len(elems) == 4 {
		var out []OperatorEntry
		seenFixity := map[OperatorFixity]bool{}
		for _, clause := range forms {
			cElems, ok := core.SyntaxListElems(clause)
			if !ok || len(cElems) < 2 || readerWord(cElems[0]) != "%-expr" {
				return nil, ctx.fail(clause.Span, DiagSyntaxShape, "operator clause must be `%:... -> target`")
			}
			entries, fixity, err := parseOperatorClause(token, clause, cElems[1:], ctx)
			if err != nil {
				return nil, err
			}
			if seenFixity[fixity] {
				return nil, ctx.fail(clause.Span, DiagSyntaxShape, fmt.Sprintf("duplicate %s clause for operator %s", fixityName(fixity), token))
			}
			seenFixity[fixity] = true
			out = append(out, entries...)
		}
		if len(out) == 0 {
			return nil, ctx.fail(stx.Span, DiagSyntaxShape, "operator do-block needs at least one clause")
		}
		return out, nil
	}
	// Single-clause form: `operator TOKEN %:... -> target`.
	entries, _, err := parseOperatorClause(token, stx, elems[3:], ctx)
	return entries, err
}

func fixityName(f OperatorFixity) string {
	switch f {
	case FixityPrefix:
		return "prefix"
	case FixityPostfix:
		return "postfix"
	default:
		return "infix"
	}
}

// parseOperatorClause parses one operator clause: `%:tag value ... -> TARGET`.
// content is the clause tokens (the metadata tags, the `->`, and the target).
// It returns one OperatorEntry per active ContextKind plus the clause's fixity.
func parseOperatorClause(token core.Word, spanStx *core.Syntax, content []*core.Syntax, ctx *Context) ([]OperatorEntry, OperatorFixity, error) {
	arrow := findArrow(content, 0)
	if arrow < 0 || arrow+2 != len(content) {
		return nil, 0, ctx.fail(spanStx.Span, DiagSyntaxShape, "operator clause expects metadata tags then `-> target`")
	}
	target, ok := content[arrow+1].Node.(core.Word)
	if !ok {
		return nil, 0, ctx.fail(content[arrow+1].Span, DiagSyntaxShape, "operator target must be a word")
	}
	prec := -1
	assoc := AssocLeft
	fixity := FixityInfix
	kinds := []ContextKind{PackageCtx, BodyCtx, ExpressionCtx}
	seen := map[string]bool{}
	for i := 0; i < arrow; i += 2 {
		meta, ok := content[i].Node.(core.Meta)
		if !ok {
			return nil, 0, ctx.fail(content[i].Span, DiagSyntaxShape, "operator metadata must be a %:tag")
		}
		if i+1 >= arrow {
			return nil, 0, ctx.fail(content[i].Span, DiagSyntaxShape, fmt.Sprintf("operator metadata %%:%s needs a value", meta))
		}
		if seen[string(meta)] {
			return nil, 0, ctx.fail(content[i].Span, DiagSyntaxShape, fmt.Sprintf("duplicate operator metadata %%:%s", meta))
		}
		seen[string(meta)] = true
		val := content[i+1]
		switch string(meta) {
		case "precedence":
			n, ok := val.Node.(core.Int)
			if !ok {
				return nil, 0, ctx.fail(val.Span, DiagSyntaxShape, "%:precedence value must be an integer")
			}
			prec = int(n)
		case "assoc":
			w, _ := val.Node.(core.Word)
			switch w {
			case "left":
				assoc = AssocLeft
			case "right":
				assoc = AssocRight
			case "none":
				assoc = AssocNone
			default:
				return nil, 0, ctx.fail(val.Span, DiagSyntaxShape, "%:assoc value must be left, right, or none")
			}
		case "fixity":
			w, _ := val.Node.(core.Word)
			switch w {
			case "infix":
				fixity = FixityInfix
			case "prefix":
				fixity = FixityPrefix
			case "postfix":
				fixity = FixityPostfix
			default:
				return nil, 0, ctx.fail(val.Span, DiagSyntaxShape, "%:fixity value must be infix, prefix, or postfix")
			}
		case "context":
			w, _ := val.Node.(core.Word)
			switch w {
			case "all":
				kinds = []ContextKind{PackageCtx, BodyCtx, ExpressionCtx}
			case "expr", "expression":
				kinds = []ContextKind{ExpressionCtx}
			case "body":
				kinds = []ContextKind{BodyCtx}
			case "package":
				kinds = []ContextKind{PackageCtx}
			default:
				return nil, 0, ctx.fail(val.Span, DiagSyntaxShape, "%:context value must be all, expr, body, or package")
			}
		default:
			return nil, 0, ctx.fail(content[i].Span, DiagSyntaxShape, fmt.Sprintf("unknown operator metadata %%:%s", meta))
		}
	}
	if prec < 0 {
		return nil, 0, ctx.fail(spanStx.Span, DiagSyntaxShape, "operator requires %:precedence")
	}
	// prefix/postfix take a single operand, so associativity is meaningless;
	// keep it AssocNone-ish by leaving the parsed value unused there.
	transformer := operatorTargetTransformer(target)
	out := make([]OperatorEntry, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, OperatorEntry{Token: token, Kind: kind, Phase: ctx.Phase, Precedence: prec, Assoc: assoc, Fixity: fixity, Transformer: transformer, DefScopes: ctx.Scopes})
	}
	return out, fixity, nil
}

// isProtocolDecl reports whether a protocol-body form is a `keyword ...`
// declaration (the keyword is the word after the `%-expr` reader head).
func isProtocolDecl(stx *core.Syntax, keyword core.Word) bool {
	parts, ok := core.ReaderExprElems(stx)
	if !ok || len(parts) < 1 {
		return false
	}
	w, ok := parts[0].Node.(core.Word)
	return ok && w == keyword
}

// readerFormNames maps the friendly handler-declaration spelling to the reader
// protocol word it competes for. `%-...` words don't tokenize as identifiers,
// so the surface uses short aliases.
var readerFormNames = map[core.Word]core.Word{
	"expr":       "%-expr",
	"group":      "%-group",
	"body":       "%-body",
	"expression": "%-expression",
}

// parseProtocolHandlerDecl parses `handler FORM claims PRED -> TRANSFORM`, the
// reader-form-handler analogue of an operator declaration. PRED and TRANSFORM
// are ordinary transformer bindings (defmacro) in the provider context: PRED is
// a syntax -> boolean-syntax predicate consulted to decide whether to claim an
// uninterpreted form, TRANSFORM is the syntax -> syntax rewrite. The handler is
// reached when the expander fails to resolve a form's head and offers the
// leftover syntax to active protocols.
func parseProtocolHandlerDecl(stx *core.Syntax, ctx *Context) ([]ProtocolHandler, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) != 7 {
		return nil, ctx.fail(stx.Span, DiagSyntaxShape, "protocol handler expects: handler FORM claims PRED -> TRANSFORM")
	}
	if w, ok := elems[3].Node.(core.Word); !ok || w != "claims" {
		return nil, ctx.fail(stx.Span, DiagSyntaxShape, "protocol handler expects: handler FORM claims PRED -> TRANSFORM")
	}
	if w, ok := elems[5].Node.(core.Word); !ok || w != "->" {
		return nil, ctx.fail(stx.Span, DiagSyntaxShape, "protocol handler expects: handler FORM claims PRED -> TRANSFORM")
	}
	formName, ok := elems[2].Node.(core.Word)
	if !ok {
		return nil, ctx.fail(elems[2].Span, DiagSyntaxShape, "protocol handler form must be a reader-form name")
	}
	form, ok := readerFormNames[formName]
	if !ok {
		return nil, ctx.fail(elems[2].Span, DiagSyntaxShape, fmt.Sprintf("unknown reader form %s (want expr/group/body/expression)", formName))
	}
	claim, _, err := resolveProtocolTransformer(elems[4], ctx)
	if err != nil {
		return nil, err
	}
	transform, defScopes, err := resolveProtocolTransformer(elems[6], ctx)
	if err != nil {
		return nil, err
	}
	claimPred := func(use *core.Syntax) bool {
		result, err := claim(use, ctx)
		return err == nil && result != nil && core.SyntaxToDatum(result) == core.Datum(core.Atom("true"))
	}
	var handlers []ProtocolHandler
	for _, kind := range []ContextKind{PackageCtx, BodyCtx, ExpressionCtx} {
		handlers = append(handlers, ProtocolHandler{
			Form: form, Kind: kind, Phase: ctx.Phase,
			Claim: claimPred, Transformer: transform, DefScopes: defScopes,
		})
	}
	return handlers, nil
}

func resolveProtocolTransformer(nameStx *core.Syntax, ctx *Context) (Transformer, core.PhaseScopes, error) {
	name, ok := nameStx.Node.(core.Word)
	if !ok {
		return nil, nil, ctx.fail(nameStx.Span, DiagSyntaxShape, "protocol handler predicate/transform must be an identifier")
	}
	b, r := ctx.Bindings.Resolve(name, ctx.Phase, ctx.Space, nameStx.Scopes[ctx.Phase])
	if r != ResolveFound || b.Kind != TransformerBinding {
		return nil, nil, ctx.fail(nameStx.Span, DiagUnbound, fmt.Sprintf("%s is not a transformer; handler predicates and transforms are defmacro bindings", name))
	}
	t, defScopes, ok := transformerValue(b.Value)
	if !ok {
		return nil, nil, ctx.fail(nameStx.Span, DiagInvalidContext, fmt.Sprintf("%s has an invalid transformer binding", name))
	}
	return t, defScopes, nil
}

// matchForm implements `(match scrutinee [pat body] ...)`. Compiles to a
// multi-clause Lambda applied to the scrutinee — exactly the same machinery
// as bind, with an arbitrary number of clauses and optional guards.
func matchForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) < 3 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "match expects a scrutinee and at least one clause")
	}
	if len(elems) == 3 {
		if bodyClauses, ok, err := surfaceClausesFromDo(elems[2], ctx, false); ok || err != nil {
			if err != nil {
				return nil, err
			}
			elems = append([]*core.Syntax{elems[0], elems[1]}, bodyClauses...)
		}
	}
	expandedScrutinee, err := Expand(elems[1], ctx)
	if err != nil {
		return nil, err
	}
	clauses := make([]core.LambdaClause, 0, len(elems)-2)
	for _, clauseStx := range elems[2:] {
		head, guard, body, err := splitClause(clauseStx)
		if err != nil {
			ctx.Diag.Error(clauseStx.Span, DiagSyntaxShape, fmt.Sprintf("match clause: %v", err))
			return nil, err
		}
		paramsVec := &core.Syntax{Node: core.SyntaxVector{head}}
		c, err := compileFnClause(paramsVec, guard, body, ctx)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, c)
	}
	lambda := &core.Syntax{Node: core.Lambda{Clauses: clauses}, Span: stx.Span}
	return core.SyntaxApp(stx.Span, lambda, expandedScrutinee), nil
}

func surfaceClausesFromDo(stx *core.Syntax, ctx *Context, allowAfter bool) ([]*core.Syntax, bool, error) {
	elems, ok := bodyForms(stx)
	if !ok {
		return nil, false, nil
	}
	out := make([]*core.Syntax, 0, len(elems))
	for _, form := range elems {
		clause, err := arrowClauseFromReaderExpr(form, ctx)
		if err != nil {
			return nil, true, err
		}
		if vec, ok := clause.Node.(core.SyntaxVector); ok && len(vec) > 0 {
			if w, ok := vec[0].Node.(core.Word); ok && w == "after" {
				if !allowAfter {
					return nil, true, ctx.fail(clause.Span, DiagSyntaxShape, "after clause is only valid in receive")
				}
			}
		}
		out = append(out, clause)
	}
	return out, true, nil
}

func bodyForms(stx *core.Syntax) ([]*core.Syntax, bool) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) == 0 {
		return nil, false
	}
	head, ok := elems[0].Node.(core.Word)
	if !ok {
		return nil, false
	}
	switch head {
	case "%-body":
		return elems[1:], true
	case "do":
		// A macro emits a do-block via syntax-list as `(do (%-body …))` (the
		// reader `%-expr do …` wrapper is not reconstructed by quasisyntax), so
		// accept that shape too: unwrap an inner %-body, else take the forms
		// directly.
		if len(elems) == 2 {
			if inner, ok := bodyForms(elems[1]); ok {
				return inner, true
			}
		}
		return elems[1:], true
	case "%-expr":
		if len(elems) == 3 {
			if w, ok := elems[1].Node.(core.Word); ok && w == "do" {
				return bodyForms(elems[2])
			}
		}
	}
	return nil, false
}

func readerDoExpr(span core.Span, body []*core.Syntax) *core.Syntax {
	bodyForm := core.SyntaxList(span, append([]*core.Syntax{wordSyntax("%-body", span)}, body...)...)
	return core.SyntaxList(span, wordSyntax("%-expr", span), wordSyntax("do", span), bodyForm)
}

// caseTransformer rewrites (case x clauses...) to (match x clauses...).
// Installed as a TransformerBinding so the same scope-shadow mechanism a
// user-defined macro will use is exercised by the kernel itself.
func caseTransformer(stx *core.Syntax, _ *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) < 1 {
		return nil, errors.New("case: improper list")
	}
	out := make([]*core.Syntax, len(elems))
	out[0] = &core.Syntax{Node: core.Word("match"), Span: elems[0].Span}
	copy(out[1:], elems[1:])
	return core.SyntaxList(stx.Span, out...), nil
}

// receiveForm implements selective receive. Each message clause is
// `[pattern body]` and reuses the LambdaClause shape (one pattern, one
// body). A trailing `[after timeout body]` (recognized by the literal word
// `after` as its first element) becomes the timeout fallback.
func receiveForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) < 2 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "receive requires at least one clause")
	}
	if len(elems) == 2 {
		if bodyClauses, ok, err := surfaceClausesFromDo(elems[1], ctx, true); ok || err != nil {
			if err != nil {
				return nil, err
			}
			elems = append([]*core.Syntax{elems[0]}, bodyClauses...)
		}
	}
	var clauses []core.LambdaClause
	var afterTimeout, afterBody *core.Syntax
	for _, clauseStx := range elems[1:] {
		vec, ok := clauseStx.Node.(core.SyntaxVector)
		if !ok || len(vec) < 2 {
			return nil, ctx.fail(clauseStx.Span, DiagSyntaxShape, "receive clause must be a vector")
		}
		if w, isW := vec[0].Node.(core.Word); isW && w == "after" {
			if afterTimeout != nil {
				return nil, ctx.fail(clauseStx.Span, DiagSyntaxShape, "receive: multiple after clauses")
			}
			if len(vec) != 3 {
				return nil, ctx.fail(clauseStx.Span, DiagSyntaxShape, "after clause must be [after timeout body]")
			}
			t, err := Expand(vec[1], ctx)
			if err != nil {
				return nil, err
			}
			_, child := ctx.IntroduceScope()
			b, err := Expand(vec[2], child)
			if err != nil {
				return nil, err
			}
			afterTimeout, afterBody = t, b
			continue
		}
		head, guard, body, err := splitClause(clauseStx)
		if err != nil {
			ctx.Diag.Error(clauseStx.Span, DiagSyntaxShape, fmt.Sprintf("receive clause: %v", err))
			return nil, err
		}
		// A receive clause is a single-parameter fn clause: the parameter is
		// the message pattern, the body runs with its bindings, and a guard
		// (if present) is evaluated under those bindings before the body.
		paramsVec := &core.Syntax{Node: core.SyntaxVector{head}}
		c, err := compileFnClause(paramsVec, guard, body, ctx)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, c)
	}
	return &core.Syntax{
		Node: core.Receive{Clauses: clauses, AfterTimeout: afterTimeout, AfterBody: afterBody},
		Span: stx.Span,
	}, nil
}

func arrowClauseFromReaderExpr(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) < 4 {
		return nil, ctx.fail(stx.Span, DiagSyntaxShape, "clause must be a reader expression with ->")
	}
	if head, ok := elems[0].Node.(core.Word); !ok || head != "%-expr" {
		return nil, ctx.fail(stx.Span, DiagSyntaxShape, "clause must be a reader expression")
	}
	arrow := findArrow(elems, 1)
	if w, ok := elems[1].Node.(core.Word); ok && w == "after" {
		if arrow < 0 && len(elems) == 4 {
			if _, ok := bodyForms(elems[3]); ok {
				return &core.Syntax{Node: core.SyntaxVector{wordSyntax("after", elems[1].Span), elems[2], elems[3]}, Span: stx.Span}, nil
			}
		}
		if arrow != 3 {
			return nil, ctx.fail(stx.Span, DiagSyntaxShape, "after clause expects: after timeout -> body")
		}
		return &core.Syntax{Node: core.SyntaxVector{wordSyntax("after", elems[1].Span), elems[2], readerExprCandidate(stx.Span, elems[arrow+1:])}, Span: stx.Span}, nil
	}
	if arrow < 2 || arrow == len(elems)-1 {
		return nil, ctx.fail(stx.Span, DiagSyntaxShape, "clause expects pattern -> body")
	}
	when := -1
	for i := 1; i < arrow; i++ {
		if w, ok := elems[i].Node.(core.Word); ok && w == "when" {
			when = i
			break
		}
	}
	if when >= 0 {
		if when == 1 || when == arrow-1 {
			return nil, ctx.fail(stx.Span, DiagSyntaxShape, "guarded clause expects pattern when guard -> body")
		}
		return &core.Syntax{Node: core.SyntaxVector{readerExprCandidate(stx.Span, elems[1:when]), wordSyntax("when", elems[when].Span), readerExprCandidate(stx.Span, elems[when+1:arrow]), readerExprCandidate(stx.Span, elems[arrow+1:])}, Span: stx.Span}, nil
	}
	return &core.Syntax{Node: core.SyntaxVector{readerExprCandidate(stx.Span, elems[1:arrow]), readerExprCandidate(stx.Span, elems[arrow+1:])}, Span: stx.Span}, nil
}

// syntaxForm implements the `syntax` core form (specp2:315, 346-348). Its
// argument is preserved unchanged as a runtime *core.Syntax value carrying
// its original scopes, span, and properties — the substrate user-written
// transformers need to produce hygienic output. The result's Node IS the
// input syntax; eval recognizes this shape and returns the inner *Syntax.
func syntaxForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) != 2 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "syntax expects exactly one argument")
	}
	return &core.Syntax{Node: elems[1], Span: stx.Span}, nil
}

func quasisyntaxForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) != 2 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "quasisyntax expects exactly one argument")
	}
	compiled, err := compileQuasisyntaxTemplate(elems[1], ctx)
	if err != nil {
		return nil, err
	}
	return Expand(compiled, ctx)
}

// syntaxCaseForm implements `syntax-case` as a thin surface over syntax-parse:
// a syntax-case clause is a syntax-parse clause whose literals are rewritten to
// `~literal` combinators and whose top-level head keyword is rewritten to a
// `~datum` name match. There is no separate runtime matcher — every pattern in
// the language is matched by the single syntax-parse engine.
func syntaxCaseForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) != 4 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "syntax-case expects: target literals do-clauses")
	}
	target, err := Expand(elems[1], ctx)
	if err != nil {
		return nil, err
	}
	_, literalSyntax, err := syntaxCaseLiteralVector(elems[2], ctx)
	if err != nil {
		return nil, err
	}
	literalMap := make(map[core.Word]*core.Syntax, len(literalSyntax))
	for _, lit := range literalSyntax {
		if w, ok := lit.Node.(core.Word); ok {
			literalMap[w] = lit
		}
	}
	clauses, ok, err := surfaceClausesFromDo(elems[3], ctx, false)
	if !ok || err != nil {
		if err == nil {
			err = errors.New("syntax-case: clauses must be a do block")
			ctx.Diag.Error(elems[3].Span, DiagSyntaxShape, err.Error())
		}
		return nil, err
	}
	var spClauses []core.SyntaxParseClause
	for _, clause := range clauses {
		vec, ok := clause.Node.(core.SyntaxVector)
		if !ok || len(vec) != 2 {
			return nil, ctx.fail(clause.Span, DiagSyntaxShape, "syntax-case clause must be pattern -> body")
		}
		pattern := syntaxCaseToParsePattern(vec[0], literalMap, true)
		spc, err := compileSyntaxParseClause(pattern, nil, vec[1], ctx)
		if err != nil {
			return nil, err
		}
		spClauses = append(spClauses, spc)
	}
	return &core.Syntax{Node: core.SyntaxParse{Target: target, Clauses: spClauses}, Span: stx.Span}, nil
}

// syntaxCaseToParsePattern rewrites a syntax-case pattern into the equivalent
// syntax-parse pattern: declared literals become `(~literal L)` (matched by
// free-identifier), and the top-level list head keyword becomes `(~datum H)`
// (matched by name, exactly as syntax-case's head-literal rule). Everything
// else — pattern variables, `_`, ellipsis, and list/vector/tuple/dict
// structure — is already common to both forms.
func syntaxCaseToParsePattern(stx *core.Syntax, literals map[core.Word]*core.Syntax, root bool) *core.Syntax {
	if lowered, ok := core.LowerReaderListSyntax(stx); ok {
		return syntaxCaseToParsePattern(lowered, literals, root)
	}
	if w, ok := stx.Node.(core.Word); ok {
		if lit, isLit := literals[w]; isLit {
			return core.SyntaxList(stx.Span, wordSyntax(string(core.CombLiteral), stx.Span), lit)
		}
		return stx
	}
	if elems, ok := core.SyntaxListElems(stx); ok {
		out := make([]*core.Syntax, 0, len(elems))
		for i, e := range elems {
			if root && i == 0 {
				if w, isWord := e.Node.(core.Word); isWord && w != "_" && w != "..." {
					out = append(out, core.SyntaxList(e.Span, wordSyntax(string(core.CombDatum), e.Span), wordSyntax(string(w), e.Span)))
					continue
				}
			}
			if w, isWord := e.Node.(core.Word); isWord && w == "..." {
				out = append(out, e)
				continue
			}
			out = append(out, syntaxCaseToParsePattern(e, literals, false))
		}
		return core.SyntaxList(stx.Span, out...)
	}
	switch n := stx.Node.(type) {
	case core.SyntaxVector:
		return &core.Syntax{Node: core.SyntaxVector(rewriteSyntaxCaseSeq([]*core.Syntax(n), literals)), Span: stx.Span}
	case core.SyntaxTuple:
		return &core.Syntax{Node: core.SyntaxTuple(rewriteSyntaxCaseSeq([]*core.Syntax(n), literals)), Span: stx.Span}
	case core.SyntaxDict:
		out := make(core.SyntaxDict, len(n))
		for i, e := range n {
			out[i] = core.SyntaxDictEntry{Key: e.Key, Value: syntaxCaseToParsePattern(e.Value, literals, false)}
		}
		return &core.Syntax{Node: out, Span: stx.Span}
	}
	return stx
}

func rewriteSyntaxCaseSeq(elems []*core.Syntax, literals map[core.Word]*core.Syntax) []*core.Syntax {
	out := make([]*core.Syntax, 0, len(elems))
	for _, e := range elems {
		if w, ok := e.Node.(core.Word); ok && w == "..." {
			out = append(out, e)
			continue
		}
		out = append(out, syntaxCaseToParsePattern(e, literals, false))
	}
	return out
}

func syntaxCaseLiteralVector(stx *core.Syntax, ctx *Context) (core.Vector, core.SyntaxVector, error) {
	vec, ok := stx.Node.(core.SyntaxVector)
	if !ok {
		return nil, nil, ctx.fail(stx.Span, DiagSyntaxShape, "syntax-case literals must be a vector")
	}
	out := make(core.Vector, 0, len(vec))
	literalSyntax := make(core.SyntaxVector, 0, len(vec))
	for _, elem := range vec {
		w, ok := elem.Node.(core.Word)
		if !ok {
			return nil, nil, ctx.fail(elem.Span, DiagSyntaxShape, "syntax-case literal must be an identifier")
		}
		out = append(out, w)
		lit := elem
		if b, r := ctx.Bindings.Resolve(w, ctx.Phase, ctx.Space, elem.Scopes[ctx.Phase]); r == ResolveFound {
			lit = &core.Syntax{Node: w, Span: elem.Span, Scopes: core.PhaseScopes{ctx.Phase: b.Scopes}, Properties: elem.Properties, Origin: elem.Origin}
		}
		literalSyntax = append(literalSyntax, lit)
	}
	return out, literalSyntax, nil
}

func compileQuasisyntaxTemplate(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	if elems, ok := core.SyntaxListElems(stx); ok && len(elems) > 0 {
		if w, isWord := elems[0].Node.(core.Word); isWord {
			switch w {
			case "unsyntax":
				if len(elems) != 2 {
					return nil, ctx.fail(stx.Span, DiagSyntaxArity, "unsyntax expects exactly one expression")
				}
				return templateExpr(elems[1], 0, ctx)
			case "unsyntax-splicing":
				return nil, ctx.fail(stx.Span, DiagInvalidContext, "unsyntax-splicing outside sequence template")
			case "%-group":
				if len(elems) == 1 {
					return core.SyntaxList(stx.Span, wordSyntax("syntax-list", stx.Span)), nil
				}
				if len(elems) == 2 {
					if isUnsyntaxSplicingForm(elems[1]) {
						return compileQuasisyntaxSequence(stx.Span, "syntax-list", []*core.Syntax{elems[1]}, ctx)
					}
					return compileQuasisyntaxTemplate(elems[1], ctx)
				}
			case "%-expr":
				return compileQuasisyntaxSequence(stx.Span, "syntax-list", elems[1:], ctx)
			case "%-body":
				// Preserve the %-body head so a templated do-block re-reads as a
				// block; compile the body forms (processing any unsyntax) after it.
				return compileQuasisyntaxSequence(stx.Span, "syntax-list", elems, ctx)
			}
		}
	}
	switch n := stx.Node.(type) {
	case core.SyntaxVector:
		return compileQuasisyntaxSequence(stx.Span, "syntax-vector", []*core.Syntax(n), ctx)
	case core.SyntaxTuple:
		return compileQuasisyntaxSequence(stx.Span, "syntax-tuple", []*core.Syntax(n), ctx)
	case core.SyntaxDict:
		parts := make([]*core.Syntax, 0, len(n)*2+1)
		parts = append(parts, wordSyntax("syntax-dict", stx.Span))
		for _, entry := range n {
			k, err := compileQuasisyntaxTemplate(entry.Key, ctx)
			if err != nil {
				return nil, err
			}
			v, err := compileQuasisyntaxTemplate(entry.Value, ctx)
			if err != nil {
				return nil, err
			}
			parts = append(parts, k, v)
		}
		return core.SyntaxList(stx.Span, parts...), nil
	}
	return core.SyntaxList(stx.Span, wordSyntax("syntax", stx.Span), stx), nil
}

func compileQuasisyntaxSequence(span core.Span, constructor string, elems []*core.Syntax, ctx *Context) (*core.Syntax, error) {
	parts := make([]*core.Syntax, 0, len(elems)+1)
	parts = append(parts, wordSyntax(constructor, span))
	for i := 0; i < len(elems); i++ {
		elem := elems[i]
		if i+1 < len(elems) && readerWord(elems[i+1]) == "..." {
			if unsyntaxElems, ok := core.SyntaxListElems(elem); ok && len(unsyntaxElems) == 2 && readerWord(unsyntaxElems[0]) == "unsyntax" {
				depth := 0
				for i+1 < len(elems) && readerWord(elems[i+1]) == "..." {
					depth++
					i++
				}
				spliced, err := templateExpr(unsyntaxElems[1], depth, ctx)
				if err != nil {
					return nil, err
				}
				parts = append(parts, core.SyntaxList(elem.Span, wordSyntax("syntax-splice", elem.Span), spliced, &core.Syntax{Node: core.Int(depth), Span: elem.Span}))
				continue
			}
			depth := 0
			for i+1 < len(elems) && readerWord(elems[i+1]) == "..." {
				depth++
				i++
			}
			repeat, err := compileQuasisyntaxRepeat(elem, depth, ctx)
			if err != nil {
				return nil, err
			}
			parts = append(parts, repeat)
			continue
		}
		if spliceExpr, ok, err := quasisyntaxSpliceExpr(elem, ctx); ok || err != nil {
			if err != nil {
				return nil, err
			}
			parts = append(parts, spliceExpr)
			continue
		}
		compiled, err := compileQuasisyntaxTemplate(elem, ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, compiled)
	}
	return core.SyntaxList(span, parts...), nil
}

func compileQuasisyntaxRepeat(template *core.Syntax, depth int, ctx *Context) (*core.Syntax, error) {
	parts := []*core.Syntax{wordSyntax("syntax-repeat", template.Span), core.SyntaxList(template.Span, wordSyntax("syntax", template.Span), template), &core.Syntax{Node: core.Int(depth), Span: template.Span}}
	// The repeated sub-template keeps its (unsyntax ...) markers; the values
	// come from these collected expressions. An attribute used inside a repeat
	// is depth-validated structurally at runtime, so skip the static check here.
	for _, expr := range collectUnsyntaxExpressions(template) {
		e, err := templateExpr(expr, -1, ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, e)
	}
	return core.SyntaxList(template.Span, parts...), nil
}

// templateExpr lowers one unsyntax/unsyntax-splicing argument inside a
// quasisyntax template. A bare identifier naming a pattern attribute is emitted
// as a direct frame reference — the only context in which an attribute resolves
// (elsewhere expandIdentifier rejects it as a non-value) — after checking that
// the ellipsis depth it is used at equals the depth it was bound at.
// requiredDepth < 0 skips that check (nested repeats, validated at runtime).
// Any other expression is returned unchanged for ordinary expansion.
func templateExpr(arg *core.Syntax, requiredDepth int, ctx *Context) (*core.Syntax, error) {
	ident := arg
	if lowered, ok := core.LowerReaderListSyntax(ident); ok {
		ident = lowered
	}
	w, ok := ident.Node.(core.Word)
	if !ok {
		return arg, nil
	}
	b, r := ctx.Bindings.Resolve(w, ctx.Phase, ctx.Space, ident.Scopes[ctx.Phase])
	if r != ResolveFound || b.Kind != AttributeBinding {
		return arg, nil
	}
	attr, _ := b.Value.(Attribute)
	if requiredDepth >= 0 && attr.Depth != requiredDepth {
		return nil, ctx.fail(arg.Span, DiagSyntaxShape, fmt.Sprintf("attribute %s is bound at ellipsis depth %d but used at depth %d in template", w, attr.Depth, requiredDepth))
	}
	return &core.Syntax{Node: b.Ref(), Span: ident.Span, Scopes: ident.Scopes, Properties: ident.Properties, Origin: ident.Origin}, nil
}

func collectUnsyntaxExpressions(stx *core.Syntax) []*core.Syntax {
	var out []*core.Syntax
	var walk func(*core.Syntax)
	walk = func(s *core.Syntax) {
		if elems, ok := core.SyntaxListElems(s); ok && len(elems) > 0 {
			if readerWord(elems[0]) == "unsyntax" && len(elems) == 2 {
				out = append(out, elems[1])
				return
			}
			for _, elem := range elems {
				walk(elem)
			}
			return
		}
		switch n := s.Node.(type) {
		case core.SyntaxVector:
			for _, elem := range n {
				walk(elem)
			}
		case core.SyntaxTuple:
			for _, elem := range n {
				walk(elem)
			}
		case core.SyntaxDict:
			for _, entry := range n {
				walk(entry.Key)
				walk(entry.Value)
			}
		}
	}
	walk(stx)
	return out
}

func quasisyntaxSpliceExpr(stx *core.Syntax, ctx *Context) (*core.Syntax, bool, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) == 0 {
		return nil, false, nil
	}
	w, ok := elems[0].Node.(core.Word)
	if !ok || w != "unsyntax-splicing" {
		return nil, false, nil
	}
	if len(elems) != 2 {
		return nil, true, ctx.fail(stx.Span, DiagSyntaxArity, "unsyntax-splicing expects exactly one expression")
	}
	spliced, err := templateExpr(elems[1], 1, ctx)
	if err != nil {
		return nil, true, err
	}
	return core.SyntaxList(stx.Span, wordSyntax("syntax-splice", stx.Span), spliced), true, nil
}

func isUnsyntaxSplicingForm(stx *core.Syntax) bool {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) == 0 {
		return false
	}
	w, ok := elems[0].Node.(core.Word)
	return ok && w == "unsyntax-splicing"
}

// quoteForm implements the `quote` core form: its single argument is
// preserved as runtime datum, with compound syntax kinds lowered to their
// datum counterparts. The result is syntax whose Node is bare datum, which
// the evaluator will return as-is (literal).
func quoteForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok {
		return nil, errors.New("quote: improper list")
	}
	if len(elems) != 2 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "quote expects exactly one argument")
	}
	if core.ContainsExpandedCore(elems[1]) {
		return nil, ctx.fail(elems[1].Span, DiagInvalidContext, "quote cannot contain expanded core syntax")
	}
	return &core.Syntax{Node: core.SyntaxToDatum(core.LowerReaderSyntax(elems[1])), Span: stx.Span}, nil
}

func quasiquoteForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok {
		return nil, errors.New("quasiquote: improper list")
	}
	if len(elems) != 2 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "quasiquote expects exactly one argument")
	}
	return expandQuasiquote(elems[1], ctx)
}

func expandQuasiquote(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	if elems, ok := core.SyntaxListElems(stx); ok && len(elems) > 0 {
		if w, isWord := elems[0].Node.(core.Word); isWord {
			switch w {
			case "unquote":
				if len(elems) != 2 {
					return nil, ctx.fail(stx.Span, DiagSyntaxArity, "unquote expects exactly one expression")
				}
				return Expand(elems[1], ctx)
			case "unquote-splicing":
				return nil, ctx.fail(stx.Span, DiagInvalidContext, "unquote-splicing outside list template")
			case "%-group":
				if len(elems) == 1 {
					return quoteDatumSyntax(stx.Span, core.Nil{}), nil
				}
				if len(elems) == 2 {
					return expandQuasiquote(elems[1], ctx)
				}
			case "%-expr":
				return expandQuasiquoteList(stx.Span, elems[1:], ctx)
			}
		}
	}
	switch n := stx.Node.(type) {
	case core.SyntaxVector:
		return compileQuasiquoteSequence(stx.Span, "vector", []*core.Syntax(n), ctx)
	case core.SyntaxTuple:
		return compileQuasiquoteSequence(stx.Span, "tuple", []*core.Syntax(n), ctx)
	}
	return quoteDatumSyntax(stx.Span, core.SyntaxToDatum(core.LowerReaderSyntax(stx))), nil
}

func expandQuasiquoteList(span core.Span, elems []*core.Syntax, ctx *Context) (*core.Syntax, error) {
	return compileQuasiquoteSequence(span, "list", elems, ctx)
}

func compileQuasiquoteSequence(span core.Span, constructor string, elems []*core.Syntax, ctx *Context) (*core.Syntax, error) {
	if constructor != "list" {
		parts := make([]*core.Syntax, 0, len(elems)+1)
		parts = append(parts, wordSyntax(constructor, span))
		for _, elem := range elems {
			compiled, err := quasiquoteSequenceElem(elem, ctx)
			if err != nil {
				return nil, err
			}
			parts = append(parts, compiled)
		}
		return Expand(core.SyntaxList(span, parts...), ctx)
	}
	cur := core.SyntaxList(span, wordSyntax("quote", span), &core.Syntax{Node: core.Nil{}, Span: span})
	for i := len(elems) - 1; i >= 0; i-- {
		if splice, ok, err := quasiquoteSpliceExpr(elems[i], ctx); ok || err != nil {
			if err != nil {
				return nil, err
			}
			cur = core.SyntaxList(span, wordSyntax("append", span), splice, cur)
			continue
		}
		head, err := quasiquoteSequenceElem(elems[i], ctx)
		if err != nil {
			return nil, err
		}
		cur = core.SyntaxList(span, wordSyntax("cons", span), head, cur)
	}
	return Expand(cur, ctx)
}

func quasiquoteSequenceElem(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	if isUnquoteForm(stx) {
		unquoteElems, _ := core.SyntaxListElems(stx)
		if len(unquoteElems) != 2 {
			return nil, ctx.fail(stx.Span, DiagSyntaxArity, "unquote expects exactly one expression")
		}
		return unquoteElems[1], nil
	}
	return core.SyntaxList(stx.Span, wordSyntax("quote", stx.Span), &core.Syntax{Node: core.SyntaxToDatum(core.LowerReaderSyntax(stx)), Span: stx.Span}), nil
}

func quasiquoteSpliceExpr(stx *core.Syntax, ctx *Context) (*core.Syntax, bool, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) == 0 {
		return nil, false, nil
	}
	w, ok := elems[0].Node.(core.Word)
	if !ok || w != "unquote-splicing" {
		return nil, false, nil
	}
	if len(elems) != 2 {
		return nil, true, ctx.fail(stx.Span, DiagSyntaxArity, "unquote-splicing expects exactly one expression")
	}
	return core.SyntaxList(stx.Span, wordSyntax("list-splice", stx.Span), elems[1]), true, nil
}

func isUnquoteForm(stx *core.Syntax) bool {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) == 0 {
		return false
	}
	w, ok := elems[0].Node.(core.Word)
	return ok && w == "unquote"
}

func quoteDatumSyntax(span core.Span, datum core.Datum) *core.Syntax {
	return &core.Syntax{Node: datum, Span: span}
}

// fnForm implements `fn`. Always clause-based: `(fn [[p...] body] ...)`.
// Each clause is a 2-element vector `[params-vector body]`. No single-clause
// shortcut — a function with one clause is `(fn [[p] body])`. Empty param
// vector is permitted (a 0-arg thunk).
func fnForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) < 2 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "fn requires at least one clause")
	}
	if clause, ok, err := parseFnArrowSurface(stx, elems, ctx); ok || err != nil {
		if err != nil {
			return nil, err
		}
		return &core.Syntax{Node: core.Lambda{Clauses: []core.LambdaClause{clause}}, Span: stx.Span}, nil
	}
	if clauses, ok, err := parseFnDoSurface(elems, ctx); ok || err != nil {
		if err != nil {
			return nil, err
		}
		return &core.Syntax{Node: core.Lambda{Clauses: clauses}, Span: stx.Span}, nil
	}
	clauses := make([]core.LambdaClause, 0, len(elems)-1)
	for _, clauseStx := range elems[1:] {
		head, guard, body, err := splitClause(clauseStx)
		if err != nil {
			ctx.Diag.Error(clauseStx.Span, DiagSyntaxShape, fmt.Sprintf("fn clause: %v", err))
			return nil, err
		}
		c, err := compileFnClause(head, guard, body, ctx)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, c)
	}
	return &core.Syntax{Node: core.Lambda{Clauses: clauses}, Span: stx.Span}, nil
}

func parseFnDoSurface(elems []*core.Syntax, ctx *Context) ([]core.LambdaClause, bool, error) {
	if len(elems) >= 3 {
		body, ok := bodyForms(elems[len(elems)-1])
		if !ok {
			return nil, false, nil
		}
		params := &core.Syntax{Node: core.SyntaxVector(elems[1 : len(elems)-1]), Span: elems[0].Span}
		bodyStx := readerDoExpr(elems[len(elems)-1].Span, body)
		c, err := compileFnClause(params, nil, bodyStx, ctx)
		if err != nil {
			return nil, true, err
		}
		return []core.LambdaClause{c}, true, nil
	}
	if len(elems) != 2 {
		return nil, false, nil
	}
	clauseStxs, ok, err := surfaceClausesFromDo(elems[1], ctx, false)
	if !ok || err != nil {
		return nil, ok, err
	}
	clauses := make([]core.LambdaClause, 0, len(clauseStxs))
	for _, clauseStx := range clauseStxs {
		// Decode the clause through the one canonical clause splitter, which
		// handles both `pattern -> body` and guarded `pattern when g -> body`
		// shapes — the same as match/receive/bind, so a guard works here too.
		head, guard, body, err := splitClause(clauseStx)
		if err != nil {
			return nil, true, ctx.fail(clauseStx.Span, DiagSyntaxShape, fmt.Sprintf("fn clause: %v", err))
		}
		params := paramsVectorFromClauseHead(head)
		clause, err := compileFnClause(params, guard, body, ctx)
		if err != nil {
			return nil, true, err
		}
		clauses = append(clauses, clause)
	}
	return clauses, true, nil
}

func paramsVectorFromClauseHead(head *core.Syntax) *core.Syntax {
	if parts, ok := core.ReaderExprElems(head); ok {
		return &core.Syntax{Node: core.SyntaxVector(parts), Span: head.Span}
	}
	return &core.Syntax{Node: core.SyntaxVector{head}, Span: head.Span}
}

func parseFnArrowSurface(stx *core.Syntax, elems []*core.Syntax, ctx *Context) (core.LambdaClause, bool, error) {
	arrow := findArrow(elems, 1)
	if arrow < 0 {
		return core.LambdaClause{}, false, nil
	}
	if arrow == 1 || arrow == len(elems)-1 {
		return core.LambdaClause{}, true, ctx.fail(stx.Span, DiagSyntaxShape, "fn arrow form expects parameters before -> and body after")
	}
	params := &core.Syntax{Node: core.SyntaxVector(elems[1:arrow]), Span: stx.Span}
	body := readerExprCandidate(stx.Span, elems[arrow+1:])
	clause, err := compileFnClause(params, nil, body, ctx)
	return clause, true, err
}

// splitClause unpacks the two clause shapes used everywhere:
//
//	[head body]                — no guard
//	[head when guard body]     — with guard (literal `when` keyword)
//
// head is whatever the calling form treats as the pattern position (a
// params-vector for fn, a single pattern for receive/bind).
func splitClause(clauseStx *core.Syntax) (head, guard, body *core.Syntax, err error) {
	vec, ok := clauseStx.Node.(core.SyntaxVector)
	if !ok {
		return nil, nil, nil, errors.New("clause must be a vector")
	}
	switch len(vec) {
	case 2:
		return vec[0], nil, vec[1], nil
	case 4:
		w, isWord := vec[1].Node.(core.Word)
		if !isWord || w != "when" {
			return nil, nil, nil, errors.New("4-element clause must be [head when guard body]")
		}
		return vec[0], vec[2], vec[3], nil
	}
	return nil, nil, nil, errors.New("clause must have 2 or 4 elements")
}

func compileFnClause(paramsStx, guardStx, bodyStx *core.Syntax, ctx *Context) (core.LambdaClause, error) {
	paramVec, ok := paramsStx.Node.(core.SyntaxVector)
	if !ok {
		return core.LambdaClause{}, ctx.fail(paramsStx.Span, DiagSyntaxShape, "fn parameters must be a vector")
	}
	s, child := ctx.IntroduceScope()
	patCtx := child.Sub(WithKind(PatternCtx))
	scoped := make([]*core.Syntax, len(paramVec))
	for i, p := range paramVec {
		scoped[i] = core.AddScope(p, ctx.Phase, s)
	}
	params, err := CompilePatternList(scoped, patCtx)
	if err != nil {
		// CompilePatternList already recorded a precise diagnostic via
		// ctx.fail; just propagate, don't author a second, vaguer one.
		return core.LambdaClause{}, err
	}
	var guard *core.Syntax
	if guardStx != nil {
		g, err := Expand(core.AddScope(guardStx, ctx.Phase, s), child)
		if err != nil {
			return core.LambdaClause{}, err
		}
		guard = g
	}
	expandedBody, err := Expand(core.AddScope(bodyStx, ctx.Phase, s), child)
	if err != nil {
		return core.LambdaClause{}, err
	}
	return core.LambdaClause{Params: params, Guard: guard, Body: expandedBody}, nil
}

// bindForm implements `(bind value [pattern body])` or
// `(bind value [pattern when guard body])`: single-clause match.
// Compiles to a single-clause Lambda applied to one argument. Multi-clause
// dispatch is `match`, which uses the same machinery with more clauses.
func bindForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) != 3 {
		return nil, ctx.fail(stx.Span, DiagSyntaxArity, "bind expects: value, [pattern body]")
	}
	valueStx, clauseStx := elems[1], elems[2]
	head, guard, body, err := splitClause(clauseStx)
	if err != nil {
		ctx.Diag.Error(clauseStx.Span, DiagSyntaxShape, fmt.Sprintf("bind clause: %v", err))
		return nil, err
	}
	expandedValue, err := Expand(valueStx, ctx)
	if err != nil {
		return nil, err
	}
	paramsVec := &core.Syntax{Node: core.SyntaxVector{head}}
	c, err := compileFnClause(paramsVec, guard, body, ctx)
	if err != nil {
		return nil, err
	}
	lambda := &core.Syntax{
		Node: core.Lambda{Clauses: []core.LambdaClause{c}},
		Span: stx.Span,
	}
	return core.SyntaxApp(stx.Span, lambda, expandedValue), nil
}

// doForm implements `(do form...)`: expand each form in a fresh body scope
// and emit core.Begin for sequential evaluation. Empty `(do)` evaluates to
// Nil per the evaluator's Begin handling.
func doForm(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok {
		return nil, errors.New("do: improper list")
	}
	body := elems[1:]
	if len(body) == 1 {
		if bodyElems, ok := bodyForms(body[0]); ok {
			body = bodyElems
		}
	}
	_, child := ctx.IntroduceScope()
	child = child.Sub(WithKind(BodyCtx))
	return expandDoBody(stx.Span, body, child)
}

type recDef struct{ lhs, rhs *core.Syntax }

// collectDefnRun gathers a maximal run of consecutive literal `defn` forms
// starting at body[start], returning each one's (name, value-expression), the
// names to export, and the index just past the run. Only literal `defn` forms
// group into a recursive scope; `=` bindings, expressions, and
// macro-introduced definitions are left to sequential processing.
func collectDefnRun(body []*core.Syntax, start int, ctx *Context) (defs []recDef, exports []core.Word, end int, ok bool) {
	j := start
	for j < len(body) {
		lhs, rhs, exps, isDefn := literalDefnParts(body[j], ctx)
		if !isDefn {
			break
		}
		defs = append(defs, recDef{lhs: lhs, rhs: rhs})
		exports = append(exports, exps...)
		j++
	}
	return defs, exports, j, len(defs) > 0
}

// literalDefnParts decodes a body form that is a literal `defn` (optionally
// `export`-prefixed), returning the name, the `fn` value expression, and any
// exported names. It applies the body's scope set so the returned syntax is
// ready for recursive-group binding.
func literalDefnParts(e *core.Syntax, ctx *Context) (lhs, rhs *core.Syntax, exports []core.Word, ok bool) {
	es := addScopeSet(e, ctx.Phase, ctx.Scopes[ctx.Phase])
	if inner, names, isExp := exportDefinitionPrefix(es); isExp {
		es = inner
		exports = names
	}
	l, r, isDefn := defnDefinitionParts(es)
	if !isDefn {
		return nil, nil, nil, false
	}
	return l, r, exports, true
}

// expandRecursiveGroup binds every definition's name in one fresh shared scope
// before expanding any right-hand side, so the group's functions can reference
// one another. It emits a single LetRec node and returns the context carrying
// the shared scope for subsequent forms.
func expandRecursiveGroup(defs []recDef, ctx *Context) ([]*core.Syntax, *Context, error) {
	s, next := ctx.IntroduceScope()
	bindings := make([]core.LetRecBinding, len(defs))
	for i, d := range defs {
		name, ok := d.lhs.Node.(core.Word)
		if !ok {
			return nil, ctx, ctx.fail(d.lhs.Span, DiagSyntaxShape, "recursive definition name must be an identifier")
		}
		scopedName := core.AddScope(d.lhs, ctx.Phase, s)
		b := ctx.Bindings.Define(name, ctx.Phase, ctx.Space, scopedName.Scopes[ctx.Phase], ValueBinding, nil)
		bindings[i].Ref = b.Ref()
	}
	for i, d := range defs {
		expandedValue, err := Expand(core.AddScope(d.rhs, ctx.Phase, s), next)
		if err != nil {
			return nil, ctx, err
		}
		bindings[i].Value = expandedValue
	}
	span := core.Span{}
	if len(defs) > 0 {
		span = defs[0].lhs.Span
	}
	return []*core.Syntax{{Node: core.LetRec{Bindings: bindings}, Span: span}}, next, nil
}

func expandDoBody(span core.Span, body []*core.Syntax, child *Context) (*core.Syntax, error) {
	out := make([]*core.Syntax, 0, len(body))
	current := child
	i := 0
	for i < len(body) {
		// A maximal run of consecutive `defn` forms is a recursive group: all
		// their names are bound in one shared scope before any body is expanded,
		// so they may reference one another (letrec) — enabling mutual
		// recursion. Non-recursive definitions (`=`) and expressions keep the
		// sequential, forward-only threading (let*).
		if defs, exports, j, ok := collectDefnRun(body, i, current); ok {
			for _, n := range exports {
				current.Exports[n] = true
			}
			binds, next, err := expandRecursiveGroup(defs, current)
			if err != nil {
				return nil, err
			}
			out = append(out, binds...)
			current = next
			i = j
			continue
		}
		e := body[i]
		i++
		es := addScopeSet(e, current.Phase, current.Scopes[current.Phase])
		// `export <definition>` prefix: record the exported name(s) and process
		// the inner definition normally (so defn/=/defmacro/defprotocol all work
		// the same with or without `export` in front).
		if inner, names, ok := exportDefinitionPrefix(es); ok {
			for _, n := range names {
				current.Exports[n] = true
			}
			es = inner
		}
		// Partially expand a macro-headed form to reveal whether it is a
		// definition, so a macro that produces `x = 5` or `defn …` binds for
		// later forms in this body (the body-context analogue of local-expand).
		es, err := partialExpandDefinition(es, current)
		if err != nil {
			return nil, err
		}
		if def, ok := definitionParts(es); ok {
			ee, next, err := expandDefinition(es.Span, def, current)
			if err != nil {
				return nil, err
			}
			if ee != nil {
				out = append(out, ee)
			}
			current = next
			continue
		}
		ee, emit, next, err := expandBodyForm(es, current)
		if err != nil {
			return nil, err
		}
		if emit {
			out = append(out, ee)
		}
		current = next
	}
	return &core.Syntax{Node: core.Begin{Body: out}, Span: span}, nil
}

type bodyDefinition struct {
	lhs       *core.Syntax
	rhs       *core.Syntax
	recursive bool
}

// partialExpandDefinition expands a body form's macro head one step at a time
// (without fully re-expanding) until the head is no longer a macro. This lets a
// macro that produces a definition (`x = 5`, `defn …`) be recognized and bound
// for the rest of the body, rather than mis-handled as an expression — the
// body-context analogue of Racket's local-expand in an internal-definition
// context. Non-macro forms (core forms, plain applications, literal definition
// keywords like `defn`/`=`) are returned unchanged for normal classification.
func partialExpandDefinition(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	for {
		t, defScopes, ok := bodyFormMacro(stx, ctx)
		if !ok {
			return stx, nil
		}
		// Invoke the macro on the same shape the ordinary head path passes: a
		// reader expression/group lowered to its application list, and a bare
		// identifier as the one-element form (m).
		use := stx
		if lowered, ok := core.LowerReaderListSyntax(stx); ok {
			use = lowered
		} else if _, isWord := stx.Node.(core.Word); isWord {
			use = core.SyntaxList(stx.Span, stx)
		}
		next, err := invokeTransformerNoReexpand(use, t, defScopes, ctx)
		if err != nil {
			recordMacroError(stx, err, ctx)
			return nil, err
		}
		stx = next
	}
}

// bodyFormMacro reports the transformer a body form's head names, if any.
func bodyFormMacro(stx *core.Syntax, ctx *Context) (Transformer, core.PhaseScopes, bool) {
	w, scopes, ok := bodyFormHead(stx, ctx)
	if !ok {
		return nil, nil, false
	}
	b, r := ctx.Bindings.Resolve(w, ctx.Phase, ctx.Space, scopes)
	if r != ResolveFound || b.Kind != TransformerBinding {
		return nil, nil, false
	}
	return transformerValue(b.Value)
}

// bodyFormHead returns the head identifier of a body form (a bare identifier,
// or the first element of a reader expression/group) and its scope set.
func bodyFormHead(stx *core.Syntax, ctx *Context) (core.Word, core.ScopeSet, bool) {
	if lowered, ok := core.LowerReaderListSyntax(stx); ok {
		stx = lowered
	}
	if w, ok := stx.Node.(core.Word); ok {
		return w, stx.Scopes[ctx.Phase], true
	}
	if elems, ok := core.SyntaxListElems(stx); ok && len(elems) > 0 {
		if w, ok := elems[0].Node.(core.Word); ok {
			return w, elems[0].Scopes[ctx.Phase], true
		}
	}
	return "", nil, false
}

// exportDefinitionPrefix recognizes `export <definition>` in a body — a defn,
// defmacro, defprotocol, or `lhs = rhs` binding with an `export` prefix. It
// returns the inner definition form (to be processed exactly as if `export`
// were absent) and the name(s) to export. A bare `export name ...` with no
// definition is left for exportForm (ok=false), so both surfaces — fused
// `export defn f do … end` and separate `export f` / `defn f …` — work.
func exportDefinitionPrefix(stx *core.Syntax) (inner *core.Syntax, names []core.Word, ok bool) {
	parts, ok := core.ReaderExprElems(stx)
	if !ok || len(parts) < 2 {
		return nil, nil, false
	}
	if kw, ok := parts[0].Node.(core.Word); !ok || kw != "export" {
		return nil, nil, false
	}
	rest := parts[1:]
	if kw, ok := rest[0].Node.(core.Word); ok {
		switch kw {
		case "defn", "defmacro", "defprotocol":
			if len(rest) >= 2 {
				if name, ok := rest[1].Node.(core.Word); ok {
					return readerExprCandidate(stx.Span, rest), []core.Word{name}, true
				}
			}
			return nil, nil, false
		}
	}
	for i, e := range rest {
		if w, ok := e.Node.(core.Word); ok && w == "=" && i > 0 {
			return readerExprCandidate(stx.Span, rest), lhsNames(rest[:i]), true
		}
	}
	return nil, nil, false
}

// lhsNames collects the identifier names bound by a match-binding left-hand
// side, descending into tuple/vector/dict destructuring.
func lhsNames(lhs []*core.Syntax) []core.Word {
	var out []core.Word
	var walk func(*core.Syntax)
	walk = func(s *core.Syntax) {
		if s == nil {
			return
		}
		switch n := s.Node.(type) {
		case core.Word:
			out = append(out, n)
		case core.SyntaxTuple:
			for _, e := range n {
				walk(e)
			}
		case core.SyntaxVector:
			for _, e := range n {
				walk(e)
			}
		case core.SyntaxDict:
			for _, e := range n {
				walk(e.Value)
			}
		case core.SyntaxPair:
			walk(n.Head)
			walk(n.Tail)
		}
	}
	for _, s := range lhs {
		walk(s)
	}
	return out
}

func addScopeSet(stx *core.Syntax, ph core.Phase, scopes core.ScopeSet) *core.Syntax {
	out := stx
	for _, s := range scopes {
		out = core.AddScope(out, ph, s)
	}
	return out
}

func expandSequentialMatchBind(span core.Span, lhs, rhs *core.Syntax, ctx *Context) (*core.Syntax, *Context, error) {
	expandedValue, err := Expand(rhs, ctx)
	if err != nil {
		return nil, ctx, err
	}
	if transformer, ok := expandedValue.Node.(core.Transformer); ok {
		if err := installTransformerBinding(lhs, transformer, ctx); err != nil {
			return nil, ctx, err
		}
		_, next := ctx.IntroduceScope()
		return nil, next, nil
	}
	if protocol, ok := expandedValue.Node.(core.Protocol); ok {
		p, ok := protocol.Value.(ProtocolExport)
		if !ok {
			return nil, ctx, errors.New("protocol: malformed value")
		}
		name, ok := lhs.Node.(core.Word)
		if !ok {
			return nil, ctx, ctx.fail(lhs.Span, DiagSyntaxShape, "protocol binding name must be an identifier")
		}
		defineProtocol(name, p, ctx)
		_, next := ctx.IntroduceScope()
		return nil, next, nil
	}
	bindingScope, next := ctx.IntroduceScope()
	lhs = core.AddScope(lhs, ctx.Phase, bindingScope)
	pat, err := CompilePattern(lhs, next.Sub(WithKind(PatternCtx)))
	if err != nil {
		return nil, ctx, err
	}
	return &core.Syntax{Node: core.Bind{Pattern: pat, Value: expandedValue}, Span: span}, next, nil
}

func expandDefinition(span core.Span, def bodyDefinition, ctx *Context) (*core.Syntax, *Context, error) {
	if !def.recursive {
		return expandSequentialMatchBind(span, def.lhs, def.rhs, ctx)
	}
	name, ok := def.lhs.Node.(core.Word)
	if !ok {
		return nil, ctx, ctx.fail(def.lhs.Span, DiagSyntaxShape, "recursive definition name must be an identifier")
	}
	bindingScope, next := ctx.IntroduceScope()
	scopedName := core.AddScope(def.lhs, ctx.Phase, bindingScope)
	b := ctx.Bindings.Define(name, ctx.Phase, ctx.Space, scopedName.Scopes[ctx.Phase], ValueBinding, nil)
	expandedValue, err := Expand(core.AddScope(def.rhs, ctx.Phase, bindingScope), next)
	if err != nil {
		return nil, ctx, err
	}
	// A recursive definition compiles to a single-binding LetRec so its value
	// (a closure) is evaluated in an environment already containing its own
	// name — self-reference works under immutable environments.
	return &core.Syntax{Node: core.LetRec{Bindings: []core.LetRecBinding{{Ref: b.Ref(), Value: expandedValue}}}, Span: span}, next, nil
}

func definitionParts(stx *core.Syntax) (bodyDefinition, bool) {
	if lhs, rhs, ok := matchBindingParts(stx); ok {
		return bodyDefinition{lhs: lhs, rhs: rhs}, true
	}
	if lhs, rhs, ok := defnDefinitionParts(stx); ok {
		return bodyDefinition{lhs: lhs, rhs: rhs, recursive: true}, true
	}
	return bodyDefinition{}, false
}

func defnDefinitionParts(stx *core.Syntax) (lhs, rhs *core.Syntax, ok bool) {
	parts, partsOK := formElems(stx)
	if !partsOK || len(parts) < 3 {
		return nil, nil, false
	}
	if defnHead, ok := parts[0].Node.(core.Word); !ok || defnHead != "defn" {
		return nil, nil, false
	}
	if _, ok := parts[1].Node.(core.Word); !ok {
		return nil, nil, false
	}
	body, ok := bodyForms(parts[len(parts)-1])
	if !ok {
		return nil, nil, false
	}
	// parts[2:len-1] is the parameter list, which may be empty (a zero-arg fn).
	params := &core.Syntax{Node: core.SyntaxVector(parts[2 : len(parts)-1]), Span: stx.Span}
	clause := &core.Syntax{Node: core.SyntaxVector{params, readerDoExpr(parts[len(parts)-1].Span, body)}, Span: stx.Span}
	rhs = core.SyntaxList(stx.Span, wordSyntax("fn", stx.Span), clause)
	return parts[1], rhs, true
}

func matchBindingParts(stx *core.Syntax) (lhs, rhs *core.Syntax, ok bool) {
	parts, partsOK := formElems(stx)
	if !partsOK || len(parts) < 3 {
		return nil, nil, false
	}
	op, opOK := parts[1].Node.(core.Word)
	if !opOK || op != "=" {
		return nil, nil, false
	}
	return parts[0], readerExprCandidate(stx.Span, parts[2:]), true
}

// formElems returns the element sequence of a body form, treating both source
// reader forms (`%-expr`/`%-group`) and the plain application lists a macro
// emits via syntax-list uniformly — so a definition produced by a macro is
// classified exactly as a hand-written one.
func formElems(stx *core.Syntax) ([]*core.Syntax, bool) {
	if lowered, ok := core.LowerReaderListSyntax(stx); ok {
		stx = lowered
	}
	return core.SyntaxListElems(stx)
}

func wordSyntax(w string, span core.Span) *core.Syntax {
	return &core.Syntax{Node: core.Word(w), Span: span}
}

// findArrow returns the index of the first `->` word in elems at or after
// `from`, or -1. It is the single definition of arrow-clause splitting, shared
// by macro/fn/operator/receive clause parsers.
func findArrow(elems []*core.Syntax, from int) int {
	for i := from; i < len(elems); i++ {
		if w, ok := elems[i].Node.(core.Word); ok && w == "->" {
			return i
		}
	}
	return -1
}

func expandBodyForm(stx *core.Syntax, ctx *Context) (*core.Syntax, bool, *Context, error) {
	if isCompileTimeBodyForm(stx, ctx) {
		_, next := ctx.IntroduceScope()
		_, err := Expand(addScopeSet(stx, next.Phase, next.Scopes[next.Phase]), next)
		return nil, false, next, err
	}
	expanded, err := Expand(stx, ctx)
	return expanded, true, ctx, err
}

func isCompileTimeBodyForm(stx *core.Syntax, ctx *Context) bool {
	var head *core.Syntax
	if parts, ok := core.ReaderExprElems(stx); ok {
		if len(parts) == 0 {
			return false
		}
		head = parts[0]
	} else if elems, ok := core.SyntaxListElems(stx); ok && len(elems) > 0 {
		head = elems[0]
	} else {
		return false
	}
	w, ok := head.Node.(core.Word)
	if !ok {
		return false
	}
	b, r := ctx.Bindings.Resolve(w, ctx.Phase, ctx.Space, head.Scopes[ctx.Phase])
	return r == ResolveFound && b.Kind == CompileTimeFormBinding
}
