package ish

import (
	"ish/core"
	"ish/eval"
	"ish/expand"
	"ish/reader"
)

type Runtime struct {
	Registry *expand.PackageRegistry
	Context  *expand.Context
	Env      *eval.Env
}

func NewRuntime() *Runtime {
	tbl := expand.NewBindingTable()
	expand.InstallKernel(tbl)
	eval.InstallRuntimeKernel(tbl)
	diag := &expand.Diagnostics{}
	ctx := expand.NewContext("main", tbl, diag)
	rt := eval.NewRuntime()
	ctx.Macros = &eval.MacroRunner{Runtime: rt}
	r := &Runtime{Registry: ctx.Packages, Context: ctx, Env: &eval.Env{Runtime: rt, Process: rt.NewProcess(), Resolver: eval.NewResolver(ctx)}}
	if err := r.installDefaultImplementation(); err != nil {
		panic(err)
	}
	return r
}

// runSource is the single read → expand → eval pipeline. Callers supply the
// expansion context and run their own post-step (value return, export
// collection, protocol activation) on the returned expanded program and value.
func (r *Runtime) runSource(ctx *expand.Context, file, source string) (*core.Syntax, eval.Value, error) {
	program, err := reader.ReadProgram(file, source)
	if err != nil {
		return nil, nil, err
	}
	expanded, err := expand.Expand(program, ctx)
	if err != nil {
		return nil, nil, err
	}
	value, next, err := eval.Eval(expanded, r.Env)
	if err != nil {
		return nil, nil, err
	}
	// Environments are immutable; thread the program's resulting environment
	// back so its top-level definitions are visible to later programs (the
	// REPL/package-load accumulation that mutation used to provide).
	r.Env = next
	return expanded, value, nil
}

func (r *Runtime) EvalSource(file, source string) (eval.Value, error) {
	_, value, err := r.runSource(r.Context, file, source)
	return value, err
}

// LoadPackageSource compiles and evaluates a source package. It shares the
// runtime's binding table and eval environment: every package's bindings live
// in one table (isolated from each other only by the fresh package scope that
// expandProgram introduces) and one env (keyed by globally-unique binding ID).
// That shared substrate is what lets a macro or protocol authored in one
// package reference its own provider-private helpers when expanded in another —
// the helper resolves by scope in the shared table and its value is present in
// the shared env. The kernel and its default implementation protocol are
// installed once by NewRuntime, so they are not reinstalled here.
func (r *Runtime) LoadPackageSource(id expand.PackageID, file, source string) (*expand.Package, error) {
	tbl := r.Context.Bindings
	baseBindingCount := tbl.Len()
	ctx := expand.NewContext(id, tbl, &expand.Diagnostics{})
	ctx.Packages = r.Registry
	ctx.Macros = r.Context.Macros
	expanded, _, err := r.runSource(ctx, file, source)
	if err != nil {
		return nil, err
	}
	pkg := expand.NewPackage(id)
	exportProgramBindings(pkg, expanded, r.Env, ctx.Exports)
	exportTransformerBindings(pkg, tbl.BindingsSince(baseBindingCount), ctx.Exports)
	exportProtocolBindings(pkg, tbl.BindingsSince(baseBindingCount), ctx.Exports)
	expand.RegisterPackage(r.Context, pkg)
	return pkg, nil
}

func exportProgramBindings(pkg *expand.Package, stx *core.Syntax, env *eval.Env, exports map[core.Word]bool) {
	if stx == nil {
		return
	}
	switch n := stx.Node.(type) {
	case core.Begin:
		for _, form := range n.Body {
			exportProgramBindings(pkg, form, env, exports)
		}
	case core.Bind:
		exportPatternBindings(pkg, n.Pattern, env, exports)
	case core.LetRec:
		for _, b := range n.Bindings {
			if exports[b.Ref.Name] {
				if v, ok := env.Lookup(b.Ref.ID); ok {
					pkg.Exports[b.Ref.Name] = expand.PackageExport{Name: b.Ref.Name, ID: b.Ref.ID, Phase: core.PhaseRuntime, Space: expand.DefaultSpace, Kind: expand.ValueBinding, Value: v}
				}
			}
		}
	}
}

func exportTransformerBindings(pkg *expand.Package, bindings []*expand.Binding, exports map[core.Word]bool) {
	for _, b := range bindings {
		if b.Kind == expand.TransformerBinding && exports[b.Name] {
			pkg.Exports[b.Name] = expand.PackageExport{Name: b.Name, ID: b.ID, Phase: b.Phase, Space: b.Space, Kind: b.Kind, Value: b.Value}
		}
	}
}

func exportProtocolBindings(pkg *expand.Package, bindings []*expand.Binding, exports map[core.Word]bool) {
	for _, b := range bindings {
		if b.Kind != expand.ProtocolBinding || !exports[b.Name] {
			continue
		}
		protocol, ok := b.Value.(expand.ProtocolExport)
		if ok {
			pkg.ExportProtocol(b.Name, protocol)
		}
	}
}

func exportPatternBindings(pkg *expand.Package, pat core.Pattern, env *eval.Env, exports map[core.Word]bool) {
	switch p := pat.(type) {
	case core.PatVar:
		if exports[p.Ref.Name] {
			if v, ok := env.Lookup(p.Ref.ID); ok {
				pkg.Exports[p.Ref.Name] = expand.PackageExport{Name: p.Ref.Name, ID: p.Ref.ID, Phase: core.PhaseRuntime, Space: expand.DefaultSpace, Kind: expand.ValueBinding, Value: v}
			}
		}
	case core.PatSeq:
		for _, elem := range p.Elems {
			exportPatternBindings(pkg, elem.Sub, env, exports)
		}
		if p.Tail != nil {
			exportPatternBindings(pkg, p.Tail, env, exports)
		}
	case core.PatGroup:
		for _, elem := range p {
			exportPatternBindings(pkg, elem, env, exports)
		}
	case core.PatDict:
		for _, entry := range p {
			exportPatternBindings(pkg, entry.Value, env, exports)
		}
	}
}
