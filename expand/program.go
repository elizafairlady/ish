package expand

import "ish/core"

func expandProgram(forms []*core.Syntax, ctx *Context) (*core.Syntax, error) {
	_, child := ctx.IntroduceScope()
	child = child.Sub(WithKind(PackageCtx))
	return expandDoBody(core.Span{}, forms, child)
}

func expandPackageBegin(stx *core.Syntax, ctx *Context) (*core.Syntax, error) {
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) == 0 {
		return nil, ctx.fail(stx.Span, DiagSyntaxShape, "%-package-begin must be a proper form")
	}
	forms := elems[1:]
	for len(forms) >= 2 {
		if _, ok := forms[0].Node.(core.Meta); !ok {
			break
		}
		forms = forms[2:]
	}
	return expandProgram(forms, ctx)
}
