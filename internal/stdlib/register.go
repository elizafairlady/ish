package stdlib

import "ish/internal/core"

// Register registers all stdlib native functions as modules.
func Register(env *core.Env) {
	env.SetModule("Kernel", &core.Module{
		Name: "Kernel",
		NativeFns: map[string]core.NativeFn{
			"is_integer": kernelIsInteger,
			"is_float":   kernelIsFloat,
			"is_string":  kernelIsString,
			"is_atom":    kernelIsAtom,
			"is_list":    kernelIsList,
			"is_map":     kernelIsMap,
			"is_nil":     kernelIsNil,
			"is_tuple":   kernelIsTuple,
			"is_pid":     kernelIsPid,
			"is_fn":      kernelIsFn,
			"to_string":  kernelToString,
			"to_integer": kernelToInteger,
			"to_float":   kernelToFloat,
			"inspect":    kernelInspect,
			"apply":      kernelApply,
			"hd":         stdlibHd,
			"tl":         stdlibTl,
			"length":     stdlibLength,
			"abs":        kernelAbs,
			"min":        kernelMin,
			"max":        kernelMax,
		},
	})

	env.SetModule("List", &core.Module{
		Name: "List",
		NativeFns: map[string]core.NativeFn{
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
			"chars":       stdlibChars,
			"pad_left":    stdlibPadLeft,
			"pad_right":   stdlibPadRight,
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

	env.SetModule("Math", &core.Module{
		Name: "Math",
		NativeFns: map[string]core.NativeFn{
			"sqrt":  mathSqrt,
			"pow":   mathPow,
			"log":   mathLog,
			"log2":  mathLog2,
			"log10": mathLog10,
			"floor": mathFloor,
			"ceil":  mathCeil,
			"round": mathRound,
		},
	})

	env.SetModule("Regex", &core.Module{
		Name: "Regex",
		NativeFns: map[string]core.NativeFn{
			"match":       regexMatch,
			"scan":        regexScan,
			"replace":     regexReplace,
			"replace_all": regexReplaceAll,
			"split":       regexSplit,
		},
	})

	env.SetModule("Path", &core.Module{
		Name: "Path",
		NativeFns: map[string]core.NativeFn{
			"basename": pathBasename,
			"dirname":  pathDirname,
			"extname":  pathExtname,
			"join":     pathJoin,
			"abs":      pathAbs,
			"exists":   pathExists,
		},
	})

	env.SetModule("Process", &core.Module{
		Name: "Process",
		NativeFns: map[string]core.NativeFn{
			"sleep":      stdlibSleep,
			"send_after": stdlibSendAfter,
		},
	})

	// Auto-import Kernel: copy all Kernel native functions into the root env
	// so they work without qualification.
	if kmod, ok := env.GetModule("Kernel"); ok {
		for name, nfn := range kmod.NativeFns {
			env.SetNativeFn(name, nfn)
		}
	}
}
