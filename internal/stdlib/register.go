package stdlib

import "ish/internal/core"

// nfn wraps a native Go function as a *FnValue. Allocated once at registration.
func nfn(name string, f core.NativeFn) *core.FnValue {
	return &core.FnValue{Name: name, Native: f}
}

// Register registers all stdlib native functions as modules.
func Register(env *core.Env) {
	env.SetModule("Kernel", &core.Module{
		Name: "Kernel",
		Fns: map[string]*core.FnValue{
			"is_integer": nfn("is_integer", kernelIsInteger),
			"is_float":   nfn("is_float", kernelIsFloat),
			"is_string":  nfn("is_string", kernelIsString),
			"is_atom":    nfn("is_atom", kernelIsAtom),
			"is_list":    nfn("is_list", kernelIsList),
			"is_map":     nfn("is_map", kernelIsMap),
			"is_nil":     nfn("is_nil", kernelIsNil),
			"is_tuple":   nfn("is_tuple", kernelIsTuple),
			"is_pid":     nfn("is_pid", kernelIsPid),
			"is_fn":      nfn("is_fn", kernelIsFn),
			"to_string":  nfn("to_string", kernelToString),
			"to_integer": nfn("to_integer", kernelToInteger),
			"to_float":   nfn("to_float", kernelToFloat),
			"inspect":    nfn("inspect", kernelInspect),
			"apply":      nfn("apply", kernelApply),
			"hd":         nfn("hd", stdlibHd),
			"tl":         nfn("tl", stdlibTl),
			"length":     nfn("length", stdlibLength),
			"abs":        nfn("abs", kernelAbs),
			"min":        nfn("min", kernelMin),
			"max":        nfn("max", kernelMax),
		},
	})

	env.SetModule("List", &core.Module{
		Name: "List",
		Fns: map[string]*core.FnValue{
			"append":     nfn("List.append", stdlibAppend),
			"concat":     nfn("List.concat", stdlibConcat),
			"at":         nfn("List.at", stdlibAt),
			"range":      nfn("List.range", stdlibRange),
			"map":        nfn("List.map", stdlibMap),
			"filter":     nfn("List.filter", stdlibFilter),
			"reduce":     nfn("List.reduce", stdlibReduce),
			"each":       nfn("List.each", stdlibEach),
			"sort":       nfn("List.sort", stdlibSort),
			"reverse":    nfn("List.reverse", stdlibReverse),
			"any":        nfn("List.any", stdlibAny),
			"all":        nfn("List.all", stdlibAll),
			"find":       nfn("List.find", stdlibFind),
			"with_index": nfn("List.with_index", stdlibEnumerate),
		},
	})

	env.SetModule("String", &core.Module{
		Name: "String",
		Fns: map[string]*core.FnValue{
			"split":       nfn("String.split", stdlibSplit),
			"join":        nfn("String.join", stdlibJoin),
			"trim":        nfn("String.trim", stdlibTrim),
			"upcase":      nfn("String.upcase", stdlibUpcase),
			"downcase":    nfn("String.downcase", stdlibDowncase),
			"replace":     nfn("String.replace", stdlibReplace),
			"replace_all": nfn("String.replace_all", stdlibReplaceAll),
			"starts_with": nfn("String.starts_with", stdlibStartsWith),
			"ends_with":   nfn("String.ends_with", stdlibEndsWith),
			"contains":    nfn("String.contains", stdlibContains),
			"slice":       nfn("String.slice", stdlibSubstring),
			"index_of":    nfn("String.index_of", stdlibIndexOf),
			"chars":       nfn("String.chars", stdlibChars),
			"pad_left":    nfn("String.pad_left", stdlibPadLeft),
			"pad_right":   nfn("String.pad_right", stdlibPadRight),
		},
	})

	env.SetModule("Map", &core.Module{
		Name: "Map",
		Fns: map[string]*core.FnValue{
			"put":     nfn("Map.put", stdlibPut),
			"delete":  nfn("Map.delete", stdlibDelete),
			"merge":   nfn("Map.merge", stdlibMerge),
			"keys":    nfn("Map.keys", stdlibKeys),
			"values":  nfn("Map.values", stdlibValues),
			"has_key": nfn("Map.has_key", stdlibHasKey),
			"get":     nfn("Map.get", stdlibGet),
			"pairs":   nfn("Map.pairs", stdlibPairs),
			"reduce":  nfn("Map.reduce", stdlibMapReduce),
		},
	})

	env.SetModule("Enum", &core.Module{
		Name: "Enum",
		Fns: map[string]*core.FnValue{
			"reduce": nfn("Enum.reduce", enumReduce),
		},
	})

	env.SetModule("Tuple", &core.Module{
		Name: "Tuple",
		Fns: map[string]*core.FnValue{
			"reduce":    nfn("Tuple.reduce", tupleReduce),
			"to_list":   nfn("Tuple.to_list", tupleToList),
			"from_list": nfn("Tuple.from_list", tupleFromList),
			"at":        nfn("Tuple.at", tupleAt),
			"size":      nfn("Tuple.size", tupleSize),
		},
	})

	env.SetModule("JSON", &core.Module{
		Name: "JSON",
		Fns: map[string]*core.FnValue{
			"parse":  nfn("JSON.parse", stdlibFromJSON),
			"encode": nfn("JSON.encode", stdlibToJSON),
		},
	})

	env.SetModule("CSV", &core.Module{
		Name: "CSV",
		Fns: map[string]*core.FnValue{
			"parse":      nfn("CSV.parse", stdlibFromCSV),
			"encode":     nfn("CSV.encode", stdlibToCSV),
			"parse_tsv":  nfn("CSV.parse_tsv", stdlibFromTSV),
			"encode_tsv": nfn("CSV.encode_tsv", stdlibToTSV),
		},
	})

	env.SetModule("IO", &core.Module{
		Name: "IO",
		Fns: map[string]*core.FnValue{
			"lines":   nfn("IO.lines", stdlibLines),
			"unlines": nfn("IO.unlines", stdlibUnlines),
		},
	})

	env.SetModule("Math", &core.Module{
		Name: "Math",
		Fns: map[string]*core.FnValue{
			"sqrt":  nfn("Math.sqrt", mathSqrt),
			"pow":   nfn("Math.pow", mathPow),
			"log":   nfn("Math.log", mathLog),
			"log2":  nfn("Math.log2", mathLog2),
			"log10": nfn("Math.log10", mathLog10),
			"floor": nfn("Math.floor", mathFloor),
			"ceil":  nfn("Math.ceil", mathCeil),
			"round": nfn("Math.round", mathRound),
		},
	})

	env.SetModule("Regex", &core.Module{
		Name: "Regex",
		Fns: map[string]*core.FnValue{
			"match":       nfn("Regex.match", regexMatch),
			"scan":        nfn("Regex.scan", regexScan),
			"replace":     nfn("Regex.replace", regexReplace),
			"replace_all": nfn("Regex.replace_all", regexReplaceAll),
			"split":       nfn("Regex.split", regexSplit),
		},
	})

	env.SetModule("Path", &core.Module{
		Name: "Path",
		Fns: map[string]*core.FnValue{
			"basename": nfn("Path.basename", pathBasename),
			"dirname":  nfn("Path.dirname", pathDirname),
			"extname":  nfn("Path.extname", pathExtname),
			"join":     nfn("Path.join", pathJoin),
			"abs":      nfn("Path.abs", pathAbs),
			"exists":   nfn("Path.exists", pathExists),
		},
	})

	env.SetModule("Process", &core.Module{
		Name: "Process",
		Fns: map[string]*core.FnValue{
			"sleep":      nfn("Process.sleep", stdlibSleep),
			"send_after": nfn("Process.send_after", stdlibSendAfter),
		},
	})

	// Auto-import Kernel: copy all Kernel functions into the root env
	// so they work without qualification.
	if kmod, ok := env.GetModule("Kernel"); ok {
		for name, fn := range kmod.Fns {
			if fn.Native != nil {
				env.SetNativeFn(name, fn.Native)
			}
		}
	}
}
