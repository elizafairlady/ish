package expand

import "ish/core"

func expandSyntaxVectorLiteral(stx *core.Syntax, elems core.SyntaxVector, ctx *Context) (*core.Syntax, error) {
	parts := make([]*core.Syntax, 0, len(elems)+1)
	parts = append(parts, wordSyntax("vector", stx.Span))
	parts = append(parts, elems...)
	return Expand(core.SyntaxList(stx.Span, parts...), ctx)
}

func expandSyntaxTupleLiteral(stx *core.Syntax, elems core.SyntaxTuple, ctx *Context) (*core.Syntax, error) {
	parts := make([]*core.Syntax, 0, len(elems)+1)
	parts = append(parts, wordSyntax("tuple", stx.Span))
	parts = append(parts, elems...)
	return Expand(core.SyntaxList(stx.Span, parts...), ctx)
}

func expandSyntaxDictLiteral(stx *core.Syntax, entries core.SyntaxDict, ctx *Context) (*core.Syntax, error) {
	parts := make([]*core.Syntax, 0, len(entries)*2+1)
	parts = append(parts, wordSyntax("dict", stx.Span))
	for _, entry := range entries {
		parts = append(parts, entry.Key, entry.Value)
	}
	return Expand(core.SyntaxList(stx.Span, parts...), ctx)
}
