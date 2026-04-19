package stdlib

import "ish/internal/core"

// Register registers all stdlib native functions into the environment.
func Register(env *core.Env) {
	env.SetNativeFn("hd", stdlibHd)
	env.SetNativeFn("tl", stdlibTl)
	env.SetNativeFn("length", stdlibLength)
	env.SetNativeFn("append", stdlibAppend)
	env.SetNativeFn("concat", stdlibConcat)
	env.SetNativeFn("map", stdlibMap)
	env.SetNativeFn("filter", stdlibFilter)
	env.SetNativeFn("reduce", stdlibReduce)
	env.SetNativeFn("range", stdlibRange)
	env.SetNativeFn("at", stdlibAt)

	env.SetNativeFn("each", stdlibEach)
	env.SetNativeFn("sorted", stdlibSort)
	env.SetNativeFn("reverse", stdlibReverse)
	env.SetNativeFn("any", stdlibAny)
	env.SetNativeFn("all", stdlibAll)
	env.SetNativeFn("first", stdlibFind)
	env.SetNativeFn("enumerate", stdlibEnumerate)

	// Map functions
	env.SetNativeFn("put", stdlibPut)
	env.SetNativeFn("delete", stdlibDelete)
	env.SetNativeFn("merge", stdlibMerge)
	env.SetNativeFn("keys", stdlibKeys)
	env.SetNativeFn("values", stdlibValues)
	env.SetNativeFn("has_key", stdlibHasKey)
	env.SetNativeFn("get", stdlibGet)
	env.SetNativeFn("pairs", stdlibPairs)

	// String functions
	env.SetNativeFn("split", stdlibSplit)
	env.SetNativeFn("join", stdlibJoin)
	env.SetNativeFn("trim", stdlibTrim)
	env.SetNativeFn("upcase", stdlibUpcase)
	env.SetNativeFn("downcase", stdlibDowncase)
	env.SetNativeFn("replace", stdlibReplace)
	env.SetNativeFn("replace_all", stdlibReplaceAll)
	env.SetNativeFn("starts_with", stdlibStartsWith)
	env.SetNativeFn("ends_with", stdlibEndsWith)
	env.SetNativeFn("contains", stdlibContains)
	env.SetNativeFn("substring", stdlibSubstring)
	env.SetNativeFn("index_of", stdlibIndexOf)

	// Serialization functions
	env.SetNativeFn("from_json", stdlibFromJSON)
	env.SetNativeFn("to_json", stdlibToJSON)
	env.SetNativeFn("from_csv", stdlibFromCSV)
	env.SetNativeFn("to_csv", stdlibToCSV)
	env.SetNativeFn("from_tsv", stdlibFromTSV)
	env.SetNativeFn("to_tsv", stdlibToTSV)
	env.SetNativeFn("from_lines", stdlibFromLines)
	env.SetNativeFn("to_lines", stdlibToLines)

	// Utilities
	env.SetNativeFn("delay", stdlibSleep)
}
