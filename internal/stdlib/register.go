package stdlib

import "ish/internal/core"

// Register registers all stdlib native functions as modules.
func Register(env *core.Env) {
	env.SetModule("List", &core.Module{
		Name: "List",
		NativeFns: map[string]core.NativeFn{
			"hd":         stdlibHd,
			"tl":         stdlibTl,
			"length":     stdlibLength,
			"append":     stdlibAppend,
			"concat":     stdlibConcat,
			"at":         stdlibAt,
			"range":      stdlibRange,
			"map":        stdlibMap,
			"filter":     stdlibFilter,
			"reduce":     stdlibReduce,
			"each":       stdlibEach,
			"sort":       stdlibSort,
			"reverse":    stdlibReverse,
			"any":        stdlibAny,
			"all":        stdlibAll,
			"find":       stdlibFind,
			"with_index": stdlibEnumerate,
		},
	})

	env.SetModule("String", &core.Module{
		Name: "String",
		NativeFns: map[string]core.NativeFn{
			"length":      stdlibLength,
			"split":       stdlibSplit,
			"join":        stdlibJoin,
			"trim":        stdlibTrim,
			"upcase":      stdlibUpcase,
			"downcase":    stdlibDowncase,
			"replace":     stdlibReplace,
			"replace_all": stdlibReplaceAll,
			"starts_with": stdlibStartsWith,
			"ends_with":   stdlibEndsWith,
			"contains":    stdlibContains,
			"slice":       stdlibSubstring,
			"index_of":    stdlibIndexOf,
		},
	})

	env.SetModule("Map", &core.Module{
		Name: "Map",
		NativeFns: map[string]core.NativeFn{
			"put":     stdlibPut,
			"delete":  stdlibDelete,
			"merge":   stdlibMerge,
			"keys":    stdlibKeys,
			"values":  stdlibValues,
			"has_key": stdlibHasKey,
			"get":     stdlibGet,
			"pairs":   stdlibPairs,
		},
	})

	env.SetModule("JSON", &core.Module{
		Name: "JSON",
		NativeFns: map[string]core.NativeFn{
			"parse":  stdlibFromJSON,
			"encode": stdlibToJSON,
		},
	})

	env.SetModule("CSV", &core.Module{
		Name: "CSV",
		NativeFns: map[string]core.NativeFn{
			"parse":      stdlibFromCSV,
			"encode":     stdlibToCSV,
			"parse_tsv":  stdlibFromTSV,
			"encode_tsv": stdlibToTSV,
		},
	})

	env.SetModule("IO", &core.Module{
		Name: "IO",
		NativeFns: map[string]core.NativeFn{
			"lines":   stdlibFromLines,
			"unlines": stdlibToLines,
		},
	})

	env.SetModule("Process", &core.Module{
		Name: "Process",
		NativeFns: map[string]core.NativeFn{
			"sleep": stdlibSleep,
		},
	})
}
