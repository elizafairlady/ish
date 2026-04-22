package stdlib

import (
	"fmt"

	"ish/internal/core"
)

// kindModule maps enumerable value kinds to their canonical module name.
// This is the single point of type→module dispatch for the Enumerable abstraction.
var kindModule = map[core.ValueKind]string{
	core.VList:  "List",
	core.VMap:   "Map",
	core.VTuple: "Tuple",
}

// enumReduce dispatches to the appropriate module's reduce based on value type.
func enumReduce(args []core.Value, scope core.Scope) (core.Value, error) {
	if len(args) != 3 {
		return core.Nil, fmt.Errorf("reduce: expected 3 arguments, got %d", len(args))
	}
	coll, fn := args[0], args[2]
	if fn.Kind != core.VFn || fn.GetFn() == nil {
		return core.Nil, fmt.Errorf("reduce: third argument must be a function, got %s", fn.Inspect())
	}

	modName, ok := kindModule[coll.Kind]
	if !ok {
		return core.Nil, fmt.Errorf("reduce: %s is not enumerable", coll.Inspect())
	}
	mod, ok := scope.GetModule(modName)
	if !ok {
		return core.Nil, fmt.Errorf("reduce: module %s not found", modName)
	}
	if fn, ok := mod.Fns["reduce"]; ok && fn.Native != nil {
		return fn.Native(args, scope)
	}
	return core.Nil, fmt.Errorf("reduce: %s does not implement reduce", modName)
}
