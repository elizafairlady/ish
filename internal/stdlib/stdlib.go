package stdlib

import (
	"embed"

	"ish/internal/value"
)

//go:embed prelude/*.ish
var preludeFS embed.FS

type Registrar interface {
	SetModule(name string, mod *value.OrdMap)
	Set(name string, v value.Value) error
}

// Invoke calls a function value (native or user-defined).
// Set by eval package to bridge the import cycle.
var Invoke func(fn *value.FnDef, args []value.Value) (value.Value, error)

// RunSource evaluates ish source in the given env.
// Set by eval package to bridge the import cycle.
var RunSource func(src string, env interface{})

func invoke(fn *value.FnDef, args []value.Value) (value.Value, error) {
	if fn.Native != nil {
		return fn.Native(args)
	}
	if Invoke != nil {
		return Invoke(fn, args)
	}
	return value.Nil, nil
}

func Register(env Registrar) {
	registerMod(env, "List", listModule())
	registerMod(env, "String", stringModule())
	registerMod(env, "Map", mapModule())
	registerMod(env, "Tuple", tupleModule())
	registerMod(env, "JSON", jsonModule())
	registerMod(env, "Enum", enumModule())
	registerMod(env, "Math", mathModule())
	registerMod(env, "Regex", regexModule())
	registerMod(env, "Path", pathModule())
	registerMod(env, "IO", ioModule())
	registerMod(env, "CSV", csvModule())

	for name, f := range kernelNatives() {
		env.Set(name, value.FnVal(nativeFn(name, f)))
	}
}

// LoadPrelude runs all embedded .ish files in the env.
func LoadPrelude(env interface{}) {
	entries, err := preludeFS.ReadDir("prelude")
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := preludeFS.ReadFile("prelude/" + e.Name())
		if err != nil {
			continue
		}
		if RunSource != nil {
			RunSource(string(data), env)
		}
	}
}

func registerMod(env Registrar, name string, mod *value.OrdMap) {
	env.SetModule(name, mod)
	env.Set(name, value.MapVal(mod))
}

func nativeFn(name string, f func([]value.Value) (value.Value, error)) *value.FnDef {
	return &value.FnDef{Name: name, Native: f}
}

func makeModule(fns map[string]func([]value.Value) (value.Value, error)) *value.OrdMap {
	m := &value.OrdMap{Vals: make(map[string]value.Value)}
	for name, f := range fns {
		m.Keys = append(m.Keys, name)
		m.Vals[name] = value.FnVal(nativeFn(name, f))
	}
	return m
}

func arg(args []value.Value, i int) value.Value {
	if i < len(args) {
		return args[i]
	}
	return value.Nil
}
