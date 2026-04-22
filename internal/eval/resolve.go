package eval

import (
	"os/exec"
	"strings"

	"ish/internal/builtin"
	"ish/internal/core"
)

// ResolvedKind identifies what a command name resolved to.
type ResolvedKind int

const (
	KindNotFound       ResolvedKind = iota
	KindModuleFn                    // Module.func -> user-defined fn in module
	KindModuleNativeFn              // Module.func -> native fn in module
	KindUserFn                      // user-defined or native fn (both in Env.Fns)
	KindVarFn                       // variable holding a VFn value
	KindBuiltin                     // shell builtin
	KindExternal                    // external command on PATH
)

// ResolvedCmd holds the result of resolving a command name.
type ResolvedCmd struct {
	Kind    ResolvedKind
	Fn      *core.FnValue
	Builtin builtin.BuiltinFunc
	ModName string
	FnName  string
}

// IsFn returns true if the resolved command is any kind of function (not a builtin or external).
func (r ResolvedCmd) IsFn() bool {
	switch r.Kind {
	case KindModuleFn, KindModuleNativeFn, KindUserFn, KindVarFn:
		return true
	}
	return false
}

// IsCmd returns true if the resolved command produces bytes (builtin or external).
func (r ResolvedCmd) IsCmd() bool {
	return r.Kind == KindBuiltin || r.Kind == KindExternal
}

// ResolveCmd performs the canonical lookup chain for a command name.
// Order: module-qualified -> user fn -> native fn -> var fn -> builtin -> external (PATH).
func ResolveCmd(name string, scope core.Scope) ResolvedCmd {
	// 1. Module-qualified (contains '.')
	if dotIdx := strings.IndexByte(name, '.'); dotIdx > 0 {
		modName := name[:dotIdx]
		fnName := name[dotIdx+1:]
		if scope != nil {
			if mod, ok := scope.GetModule(modName); ok {
				if fn, ok := mod.Fns[fnName]; ok {
					if fn.Native != nil {
						return ResolvedCmd{Kind: KindModuleNativeFn, Fn: fn, ModName: modName, FnName: fnName}
					}
					return ResolvedCmd{Kind: KindModuleFn, Fn: fn, ModName: modName, FnName: fnName}
				}
			}
		}
	}

	// 2. User-defined or native function (both stored in Env.Fns)
	if scope != nil {
		if fn, ok := scope.GetFn(name); ok {
			return ResolvedCmd{Kind: KindUserFn, Fn: fn}
		}
	}

	// 3. Variable holding a function value
	if scope != nil {
		if v, ok := scope.Get(name); ok && v.Kind == core.VFn && v.GetFn() != nil {
			return ResolvedCmd{Kind: KindVarFn, Fn: v.GetFn()}
		}
	}

	// 5. Shell builtin
	if b, ok := builtin.Builtins[name]; ok {
		return ResolvedCmd{Kind: KindBuiltin, Builtin: b}
	}

	// 6. External command (PATH lookup)
	if _, err := exec.LookPath(name); err == nil {
		return ResolvedCmd{Kind: KindExternal}
	}

	return ResolvedCmd{Kind: KindNotFound}
}

