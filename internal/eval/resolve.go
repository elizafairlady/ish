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
	KindUserFn                      // user-defined fn
	KindNativeFn                    // native fn (stdlib)
	KindVarFn                       // variable holding a VFn value
	KindBuiltin                     // shell builtin
	KindExternal                    // external command on PATH
)

// ResolvedCmd holds the result of resolving a command name.
type ResolvedCmd struct {
	Kind     ResolvedKind
	Fn       *core.FnValue
	NativeFn core.NativeFn
	Builtin  builtin.BuiltinFunc
	ModName  string
	FnName   string
}

// IsFn returns true if the resolved command is any kind of function (not a builtin or external).
func (r ResolvedCmd) IsFn() bool {
	switch r.Kind {
	case KindModuleFn, KindModuleNativeFn, KindUserFn, KindNativeFn, KindVarFn:
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
						return ResolvedCmd{Kind: KindModuleNativeFn, Fn: fn, NativeFn: fn.Native, ModName: modName, FnName: fnName}
					}
					return ResolvedCmd{Kind: KindModuleFn, Fn: fn, ModName: modName, FnName: fnName}
				}
			}
		}
	}

	// 2. User-defined function
	if scope != nil {
		if fn, ok := scope.GetFn(name); ok {
			return ResolvedCmd{Kind: KindUserFn, Fn: fn}
		}
	}

	// 3. Native function (stdlib)
	if scope != nil {
		if nfn, ok := scope.GetNativeFn(name); ok {
			return ResolvedCmd{Kind: KindNativeFn, NativeFn: nfn}
		}
	}

	// 4. Variable holding a function value
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

// ResolveCmdCached returns a closure that resolves commands with cached PATH lookups.
// Used by MakeIsCommand for parser disambiguation.
func ResolveCmdCached(scope core.Scope) func(string) bool {
	pathCache := make(map[string]bool)
	return func(name string) bool {
		// Module-qualified
		if dotIdx := strings.IndexByte(name, '.'); dotIdx > 0 {
			if scope != nil {
				if _, ok := scope.GetModule(name[:dotIdx]); ok {
					return true
				}
			}
		}
		if scope != nil {
			if _, ok := scope.GetFn(name); ok {
				return true
			}
			if _, ok := scope.GetNativeFn(name); ok {
				return true
			}
		}
		if _, ok := builtin.Builtins[name]; ok {
			return true
		}
		if found, ok := pathCache[name]; ok {
			return found
		}
		_, err := exec.LookPath(name)
		pathCache[name] = err == nil
		return err == nil
	}
}
