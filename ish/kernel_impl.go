package ish

import (
	"fmt"

	"ish/core"
	"ish/expand"
	"ish/std"
)

// installDefaultImplementation loads std/impl/kernel from its embedded ish
// source and activates its protocol in the runtime's base context, so default
// operators and qualified access are present for all subsequent programs.
//
// std/impl/kernel is authored in ish (see std/impl/kernel/), not Go: the default
// expander implementation policy — arithmetic/comparison operators and qualified
// package access — is one `defprotocol` over the generic expansion primitives.
// Access is the proof the default qualified-access behavior holds no privilege
// the primitives can't reproduce: its claim accepts a dotted access whose member
// `resolve`s to a `binding-kind :value` ref, and its lower emits that ref with
// `ref->syntax`. The kernel routes none of this; the runtime simply loads and
// activates this package at startup.
//
// The protocol's handler/operator bindings are installed at the base (empty)
// scope set — making them global — while its claim/lower bodies stay resolvable
// in their own definition scope via the protocol's DefScopes.
func (r *Runtime) installDefaultImplementation() error {
	if err := r.loadStdKernel(); err != nil {
		return err
	}
	source, err := std.PackageSource("impl/kernel")
	if err != nil {
		return err
	}
	base := r.Context.Bindings.Len()
	if _, _, err := r.runSource(r.Context, "std/impl/kernel", source); err != nil {
		return fmt.Errorf("std/impl/kernel: %w", err)
	}
	for _, b := range r.Context.Bindings.BindingsSince(base) {
		if b.Kind != expand.ProtocolBinding {
			continue
		}
		if p, ok := b.Value.(expand.ProtocolExport); ok {
			// Activate at both phases so the base language's expression syntax
			// (operators, qualified access) works in macro/transformer bodies
			// (phase 1) exactly as at runtime (phase 0) — mirroring the dual-phase
			// kernel value builtins.
			expand.ActivateProtocol(r.Context, p, core.PhaseRuntime)
			expand.ActivateProtocol(r.Context, p, core.PhaseExpand)
		}
	}
	return nil
}

// loadStdKernel loads the base standard library (std/kernel) from embedded ish
// source and installs its exports globally, so its macros (if/and/or) are
// available to every subsequent program without an explicit `use` — exactly
// like the Go-defined kernel builtins, and adding no Go expander/eval case.
// This proves base surface forms are extractable into the language itself.
func (r *Runtime) loadStdKernel() error {
	return r.loadGlobalPackage("kernel", "std/kernel")
}

// loadGlobalPackage loads an embedded std package directory and installs its
// exports at the base (empty) scope set, both phases — making them global. It
// is the single mechanism for adding a source-authored package to the base
// environment; new std packages drop in by calling it (in dependency order).
func (r *Runtime) loadGlobalPackage(dir string, id expand.PackageID) error {
	source, err := std.PackageSource(dir)
	if err != nil {
		return err
	}
	pkg, err := r.LoadPackageSource(id, string(id)+"/"+dir+".ish", source)
	if err != nil {
		return fmt.Errorf("%s: %w", id, err)
	}
	for _, exp := range pkg.Exports {
		for _, ph := range []core.Phase{core.PhaseRuntime, core.PhaseExpand} {
			r.Context.Bindings.Define(exp.Name, ph, exp.Space, core.ScopeSet{}, exp.Kind, exp.Value)
		}
	}
	return nil
}
