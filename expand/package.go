package expand

import (
	"fmt"

	"ish/core"
)

type PackageExport struct {
	Name  core.Word
	ID    core.BindingID
	Phase core.Phase
	Space BindingSpace
	Kind  BindingKind
	Value any
}

type ProtocolExport struct {
	Handlers  []ProtocolHandler
	Operators []OperatorEntry
}

type Package struct {
	ID        PackageID
	Space     BindingSpace
	Exports   map[core.Word]PackageExport
	Protocols map[core.Word]ProtocolExport
}

// packageSpace is the binding space a package's exports inhabit. Each package
// gets a distinct space, so `base.member` is the ordinary resolution `resolve
// member (space-of base)` and exports of different packages never collide even
// when their member names match — isolation by space, not by map ownership.
func packageSpace(id PackageID) BindingSpace { return BindingSpace("pkg:" + string(id)) }

func NewPackage(id PackageID) *Package {
	return &Package{ID: id, Space: packageSpace(id), Exports: map[core.Word]PackageExport{}, Protocols: map[core.Word]ProtocolExport{}}
}

// SpaceValue reifies a binding space as a first-class value, so an ish protocol
// can carry it from `space-of` to `resolve`. A space is derived from a base
// (here, a package alias), per the "everything is resolution in a space" model.
type SpaceValue struct{ Space BindingSpace }

// RegisterPackage records pkg in the registry and materializes its exports as
// ordinary bindings in pkg's space. Access then resolves a member uniformly
// through the binding table — there is no separate export-map lookup path. The
// export's stable ID is preserved so a resolved member compares equal under
// free-identifier=? to its definition.
func RegisterPackage(ctx *Context, pkg *Package) {
	if ctx == nil || pkg == nil {
		return
	}
	ctx.Packages.Register(pkg)
	for _, e := range pkg.Exports {
		b := ctx.Bindings.Define(e.Name, e.Phase, pkg.Space, core.ScopeSet{}, e.Kind, e.Value)
		b.ID = e.ID
	}
}

// SpaceOf resolves an identifier to the space its binding introduces. Today the
// only space-introducing bindings are package aliases (resolved in PackageSpace
// at the runtime phase, since aliases live there); a base that is not a package
// alias has no space.
func (c *Context) SpaceOf(stx *core.Syntax) (SpaceValue, bool) {
	w, ok := stx.Node.(core.Word)
	if !ok {
		return SpaceValue{}, false
	}
	b, r := c.Bindings.Resolve(w, core.PhaseRuntime, PackageSpace, stx.Scopes[core.PhaseRuntime])
	if r != ResolveFound {
		return SpaceValue{}, false
	}
	pkg, ok := b.Value.(*Package)
	if !ok || pkg == nil {
		return SpaceValue{}, false
	}
	return SpaceValue{Space: pkg.Space}, true
}

// ResolveMember resolves a member identifier in the given space at the runtime
// phase. The returned binding is the generic resolution ref a protocol inspects
// with binding-kind / ref-value.
func (c *Context) ResolveMember(member *core.Syntax, sv SpaceValue) (*Binding, ResolveResult) {
	w, ok := member.Node.(core.Word)
	if !ok {
		return nil, ResolveUnbound
	}
	return c.Bindings.Resolve(w, core.PhaseRuntime, sv.Space, member.Scopes[core.PhaseRuntime])
}

func (p *Package) ExportValue(name core.Word, value any) {
	p.Exports[name] = PackageExport{Name: name, ID: newBindingID(), Phase: core.PhaseRuntime, Space: DefaultSpace, Kind: ValueBinding, Value: value}
}

func (p *Package) ExportTransformer(name core.Word, transformer Transformer) {
	p.Exports[name] = PackageExport{Name: name, ID: newBindingID(), Phase: core.PhaseRuntime, Space: DefaultSpace, Kind: TransformerBinding, Value: &SyntaxTransformer{Fn: transformer}}
}

func (p *Package) ExportProtocol(name core.Word, protocol ProtocolExport) {
	p.Protocols[name] = protocol
}

// operatorTargetTransformer lowers an operator application to a headed call
// `(target operands...)` — `(target left right)` for infix, `(target operand)`
// for prefix/postfix. Source-declared `defprotocol` operators (including
// std/impl/kernel's arithmetic/comparison, authored in ish) all use this one
// constructor so their lowering cannot drift.
func operatorTargetTransformer(target core.Word) OperatorTransformer {
	return func(stx *core.Syntax, operands []*core.Syntax) (*core.Syntax, error) {
		elems := make([]*core.Syntax, 0, len(operands)+1)
		elems = append(elems, &core.Syntax{Node: target, Span: stx.Span})
		elems = append(elems, operands...)
		return core.SyntaxList(stx.Span, elems...), nil
	}
}

type PackageRegistry struct {
	packages map[PackageID]*Package
	resolver PackageResolver
}

// PackageResolver loads a package that is not already registered — the hook the
// runtime sets to fall back to the embedded std tree. It returns (nil, nil) when
// no package by that id exists (so the caller reports "not found"), or a non-nil
// error when the package exists but failed to load.
type PackageResolver func(PackageID) (*Package, error)

func NewPackageRegistry() *PackageRegistry {
	return &PackageRegistry{packages: map[PackageID]*Package{}}
}

// SetResolver installs the fallback loader used by Resolve on a registry miss.
func (r *PackageRegistry) SetResolver(fn PackageResolver) {
	if r != nil {
		r.resolver = fn
	}
}

func (r *PackageRegistry) Register(pkg *Package) {
	if r != nil && pkg != nil {
		r.packages[pkg.ID] = pkg
	}
}

func (r *PackageRegistry) Lookup(id PackageID) (*Package, bool) {
	if r == nil {
		return nil, false
	}
	pkg, ok := r.packages[id]
	return pkg, ok
}

// Resolve finds a package by id. Resolution order is stdlib first, then the
// local registry — the resolver (set by the runtime) covers the embedded std
// tree (and, later, an ISHPATH), and only when it has no such package does a
// locally-registered package answer. The bool reports whether a package was
// found; a load failure surfaces as err.
func (r *PackageRegistry) Resolve(id PackageID) (*Package, bool, error) {
	if r == nil {
		return nil, false, nil
	}
	if r.resolver != nil {
		pkg, err := r.resolver(id)
		if err != nil {
			return nil, false, err
		}
		if pkg != nil {
			return pkg, true, nil
		}
	}
	pkg, ok := r.Lookup(id)
	return pkg, ok, nil
}

func packagePath(stx *core.Syntax) (PackageID, error) {
	path, ok := stx.Node.(core.Word)
	if !ok {
		return "", fmt.Errorf("package path must be a word")
	}
	return PackageID(path), nil
}

func packageProtocolPath(stx *core.Syntax) (PackageID, core.Word, bool, error) {
	path, ok := stx.Node.(core.Word)
	if !ok {
		return "", "", false, fmt.Errorf("package path must be a word")
	}
	s := string(path)
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return PackageID(s[:i]), core.Word(s[i+1:]), true, nil
		}
	}
	return PackageID(path), "", false, nil
}

func defaultPackageAlias(path PackageID) core.Word {
	s := string(path)
	last := 0
	for i := range s {
		if s[i] == '/' {
			last = i + 1
		}
	}
	return core.Word(s[last:])
}

func importPackage(pathStx *core.Syntax, alias core.Word, ctx *Context) error {
	path, err := packagePath(pathStx)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	pkg, ok, err := ctx.Packages.Resolve(path)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	if !ok {
		return fmt.Errorf("import: package not found: %s", path)
	}
	if alias == "" {
		alias = defaultPackageAlias(path)
	}
	ctx.Bindings.Define(alias, ctx.Phase, PackageSpace, ctx.Scopes[ctx.Phase], PackageBinding, pkg)
	return nil
}

func usePackage(pathStx *core.Syntax, ctx *Context) error {
	path, err := packagePath(pathStx)
	if err != nil {
		return fmt.Errorf("use: %w", err)
	}
	pkg, ok, err := ctx.Packages.Resolve(path)
	if err != nil {
		return fmt.Errorf("use: %w", err)
	}
	if !ok {
		return fmt.Errorf("use: package not found: %s", path)
	}
	for _, export := range pkg.Exports {
		ctx.Bindings.Define(export.Name, export.Phase, export.Space, ctx.Scopes[ctx.Phase], export.Kind, export.Value)
	}
	return nil
}

func implementsPackage(pathStx *core.Syntax, ctx *Context) error {
	if name, ok := pathStx.Node.(core.Word); ok {
		if b, r := ctx.Bindings.Resolve(name, ctx.Phase, ctx.Space, pathStx.Scopes[ctx.Phase]); r == ResolveFound && b.Kind == ProtocolBinding {
			p, ok := b.Value.(ProtocolExport)
			if !ok {
				return fmt.Errorf("implements: malformed protocol binding %s", name)
			}
			activateProtocol(p, ctx, ctx.Phase)
			return nil
		}
	}
	path, protocolName, specific, err := packageProtocolPath(pathStx)
	if err != nil {
		return fmt.Errorf("implements: %w", err)
	}
	pkg, ok, err := ctx.Packages.Resolve(path)
	if err != nil {
		return fmt.Errorf("implements: %w", err)
	}
	if !ok {
		return fmt.Errorf("implements: package not found: %s", path)
	}
	if specific {
		protocol, ok := pkg.Protocols[protocolName]
		if !ok {
			return fmt.Errorf("implements: package %s has no protocol %s", path, protocolName)
		}
		activateProtocol(protocol, ctx, ctx.Phase)
		return nil
	}
	for _, protocol := range pkg.Protocols {
		activateProtocol(protocol, ctx, ctx.Phase)
	}
	return nil
}

// ActivateProtocol installs a protocol's operators and reader-form handlers
// into ctx at the given phase and ctx's current scopes — the exported entry the
// runtime uses to make a loaded implementation package active. The runtime
// activates the default std/impl/kernel at BOTH phases (like the dual-phase
// kernel value builtins) so expression syntax — operators, qualified access —
// works in macro bodies (phase 1) exactly as at runtime (phase 0). A user
// `implements` activates at the phase where it appears. The protocol's own
// DefScopes are preserved, so its claim/transform bodies still resolve their
// provider-private helpers by hygiene even though the bindings live at the
// activation scope.
func ActivateProtocol(ctx *Context, protocol ProtocolExport, phase core.Phase) {
	activateProtocol(protocol, ctx, phase)
}

func activateProtocol(protocol ProtocolExport, ctx *Context, phase core.Phase) {
	scopes := ctx.Scopes[phase]
	for _, h := range protocol.Handlers {
		h.Phase = phase
		if h.DefScopes == nil {
			h.DefScopes = ctx.Scopes
		}
		ctx.Bindings.Define(protocolHandlerBindingName(h.Kind, h.Form), phase, ProtocolHandlerSpace, scopes, ProtocolHandlerBinding, h)
	}
	// Group operator entries by (kind, token) so a token's several fixity
	// variants (e.g. infix and prefix `-`) live in one binding; the enforestation
	// selects the variant by position.
	type opKey struct {
		kind  ContextKind
		token core.Word
	}
	opGroups := map[opKey][]OperatorEntry{}
	var opOrder []opKey
	for _, op := range protocol.Operators {
		op.Phase = phase
		if op.DefScopes == nil {
			op.DefScopes = ctx.Scopes
		}
		k := opKey{op.Kind, op.Token}
		if _, seen := opGroups[k]; !seen {
			opOrder = append(opOrder, k)
		}
		opGroups[k] = append(opGroups[k], op)
	}
	for _, k := range opOrder {
		ctx.Bindings.Define(operatorBindingName(k.kind, k.token), phase, OperatorSpace, scopes, OperatorBinding, opGroups[k])
	}
}

func operatorBindingName(kind ContextKind, token core.Word) core.Word {
	return core.Word(fmt.Sprintf("%d:%s", kind, token))
}

func protocolHandlerBindingName(kind ContextKind, form core.Word) core.Word {
	return core.Word(fmt.Sprintf("%d:%s", kind, form))
}
