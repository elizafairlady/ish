package eval

import (
	"testing"

	"ish/internal/value"
)

// =====================================================================
// Bug 1: Anonymous fn callbacks fail through stdlib.Invoke
//
// Two sub-bugs:
//   a) evalFnDef binds anonymous fns to "<anon>" in scope, causing
//      clause accumulation when multiple anon fns are created.
//   b) evalFnDef doesn't set fn.Env (captureEnv) for anonymous fns,
//      so they can't close over enclosing scope variables.
// =====================================================================

func TestBug1_AnonFnCallbackBasic(t *testing.T) {
	// Anonymous fn passed to List.map should work like a lambda
	run(t, `r = List.map([1, 2, 3], fn x do x * 2 end)
echo $r`, "[2, 4, 6]\n")
}

func TestBug1_AnonFnCallbackFilter(t *testing.T) {
	// Anonymous fn passed to List.filter
	run(t, `r = List.filter([1, 2, 3, 4, 5], fn x do x > 3 end)
echo $r`, "[4, 5]\n")
}

func TestBug1_AnonFnMultipleCallbacks(t *testing.T) {
	// Two different anonymous fns in the same scope must not interfere
	// This is the clause accumulation bug: the second fn gets the first fn's body
	run(t, `a = List.map([1, 2, 3], fn x do x + 10 end)
b = List.map([1, 2, 3], fn x do x * 100 end)
echo $a
echo $b`, "[11, 12, 13]\n[100, 200, 300]\n")
}

func TestBug1_AnonFnMultiStatement(t *testing.T) {
	// Multi-statement body in anonymous fn callback
	run(t, `r = List.map([1, 2, 3], fn x do
  doubled = x * 2
  doubled + 1
end)
echo $r`, "[3, 5, 7]\n")
}

func TestBug1_AnonFnClosure(t *testing.T) {
	// Anonymous fn must capture enclosing scope (like lambdas do)
	run(t, `factor = 10
r = List.map([1, 2, 3], fn x do x * factor end)
echo $r`, "[10, 20, 30]\n")
}

func TestBug1_AnonFnClosureNested(t *testing.T) {
	// Anonymous fn inside a user function must capture the function's scope
	run(t, `fn scale list, factor do
  List.map(list, fn x do x * factor end)
end
r = scale([1, 2, 3], 5)
echo $r`, "[5, 10, 15]\n")
}

func TestBug1_AnonFnDoesNotPolluteScope(t *testing.T) {
	// Anonymous fn must not bind "<anon>" or any name in the enclosing scope
	bind(t, `r = List.map([1], fn x do x end)
x = typeof(r)`, "x", value.AtomVal("list"))
}

func TestBug1_AnonFnEach(t *testing.T) {
	// Anonymous fn with side effects via List.each
	run(t, `List.each([1, 2, 3], fn x do echo $x end)`, "1\n2\n3\n")
}

func TestBug1_AnonFnReduce(t *testing.T) {
	// Anonymous fn as reduce callback
	run(t, `r = List.reduce([1, 2, 3, 4], 0, fn acc, x do acc + x end)
echo $r`, "10\n")
}

// =====================================================================
// Bug 2: |> doesn't collect bare lambda/fn args after the callable
//
// `xs |> List.filter \x -> x > 1` should pass the lambda as an arg
// to List.filter, but the parser only collects args in parseExpr,
// not in parseExprLogicOr which is used for the |> right side.
// =====================================================================

func TestBug2_PipeLambdaArg(t *testing.T) {
	// Lambda after pipe callable (no parens)
	run(t, `r = [1, 2, 3, 4, 5] |> List.filter \x -> x > 3
echo $r`, "[4, 5]\n")
}

func TestBug2_PipeLambdaArgMap(t *testing.T) {
	run(t, `r = [1, 2, 3] |> List.map \x -> x * 2
echo $r`, "[2, 4, 6]\n")
}

func TestBug2_PipeLambdaArgChain(t *testing.T) {
	// Chained pipes each collecting their own lambda
	run(t, `r = [1, 2, 3, 4, 5] |> List.filter \x -> x > 2 |> List.map \x -> x * 10
echo $r`, "[30, 40, 50]\n")
}

func TestBug2_PipeLambdaArgEach(t *testing.T) {
	run(t, `[1, 2, 3] |> List.each \x -> echo $x`, "1\n2\n3\n")
}

func TestBug2_PipeAnonFnArg(t *testing.T) {
	// Anonymous fn (not just lambda) after pipe callable
	run(t, `r = [1, 2, 3] |> List.map fn x do x * 2 end
echo $r`, "[2, 4, 6]\n")
}

func TestBug2_PipeWithParensStillWorks(t *testing.T) {
	// Paren form must continue to work
	run(t, `r = [1, 2, 3] |> List.map(\x -> x * 2)
echo $r`, "[2, 4, 6]\n")
}

func TestBug2_PipeReduceExtraArgs(t *testing.T) {
	// |> with paren-wrapped extra args (existing functionality)
	run(t, `r = [1, 2, 3] |> List.reduce(0, \acc, x -> acc + x)
echo $r`, "6\n")
}

// =====================================================================
// Bug 3: fn in command argument position parsed as bare word
//
// At statement level, `List.each items, fn x do ... end` treats `fn`
// as a literal string argument instead of an anonymous function.
// parseCmdArg needs to recognize fn and dispatch to parseFnDef(true).
// =====================================================================

func TestBug3_FnInCmdArg(t *testing.T) {
	// fn as second argument at statement level (bare call, no parens)
	run(t, `List.each [1, 2, 3], fn x do echo $x end`, "1\n2\n3\n")
}

func TestBug3_FnInCmdArgMap(t *testing.T) {
	run(t, `r = List.map [1, 2, 3], fn x do x * 2 end
echo $r`, "[2, 4, 6]\n")
}

func TestBug3_FnInCmdArgMultiClause(t *testing.T) {
	// Multi-clause anonymous fn in arg position
	run(t, `r = List.map [1, 2, 3], fn do
  1 -> :one
  2 -> :two
  _ -> :other
end
echo $r`, "[:one, :two, :other]\n")
}

func TestBug3_FnInCmdArgWithParensStillWorks(t *testing.T) {
	// Paren form must continue to work
	run(t, `List.each([1, 2, 3], fn x do echo $x end)`, "1\n2\n3\n")
}

func TestBug3_LambdaInCmdArgStillWorks(t *testing.T) {
	// Lambdas in cmd arg position already work — don't regress
	run(t, `List.each [1, 2, 3], \x -> echo $x`, "1\n2\n3\n")
}

// =====================================================================
// Bug 4: Missing stdlib functions
//
// These test functions that should exist but currently don't.
// Tests are written against the Elixir-expected API.
// =====================================================================

// --- Enum ---

func TestStdlib_EnumGroupBy(t *testing.T) {
	run(t, `r = Enum.group_by([1, 2, 3, 4, 5], \x -> x > 3)
echo $(Map.get(r, ":true"))
echo $(Map.get(r, ":false"))`, "[4, 5]\n[1, 2, 3]\n")
}

func TestStdlib_EnumSortBy(t *testing.T) {
	run(t, `r = Enum.sort_by([{3, "c"}, {1, "a"}, {2, "b"}], \x -> Tuple.at(x, 0))
echo $r`, "[{1, \"a\"}, {2, \"b\"}, {3, \"c\"}]\n")
}

func TestStdlib_EnumFlatMap(t *testing.T) {
	run(t, `r = Enum.flat_map([1, 2, 3], \x -> [x, x * 10])
echo $r`, "[1, 10, 2, 20, 3, 30]\n")
}

func TestStdlib_EnumFrequencies(t *testing.T) {
	bind(t, `r = Enum.frequencies(["a", "b", "a", "c", "a", "b"])`,
		"r", value.MapVal(&value.OrdMap{
			Keys: []string{"a", "b", "c"},
			Vals: map[string]value.Value{
				"a": value.IntVal(3),
				"b": value.IntVal(2),
				"c": value.IntVal(1),
			},
		}))
}

func TestStdlib_EnumReject(t *testing.T) {
	run(t, `r = Enum.reject([1, 2, 3, 4, 5], \x -> x > 3)
echo $r`, "[1, 2, 3]\n")
}

func TestStdlib_EnumChunkEvery(t *testing.T) {
	run(t, `r = Enum.chunk_every([1, 2, 3, 4, 5], 2)
echo $r`, "[[1, 2], [3, 4], [5]]\n")
}

func TestStdlib_EnumJoin(t *testing.T) {
	run(t, `r = Enum.join(["a", "b", "c"], "-")
echo $r`, "a-b-c\n")
}

func TestStdlib_EnumMinBy(t *testing.T) {
	run(t, `r = Enum.min_by([{3, "c"}, {1, "a"}, {2, "b"}], \x -> Tuple.at(x, 0))
echo $r`, "{1, \"a\"}\n")
}

func TestStdlib_EnumMaxBy(t *testing.T) {
	run(t, `r = Enum.max_by([{3, "c"}, {1, "a"}, {2, "b"}], \x -> Tuple.at(x, 0))
echo $r`, "{3, \"c\"}\n")
}

func TestStdlib_EnumTakeWhile(t *testing.T) {
	run(t, `r = Enum.take_while([1, 2, 3, 4, 5], \x -> x < 4)
echo $r`, "[1, 2, 3]\n")
}

func TestStdlib_EnumDropWhile(t *testing.T) {
	run(t, `r = Enum.drop_while([1, 2, 3, 4, 5], \x -> x < 4)
echo $r`, "[4, 5]\n")
}

func TestStdlib_EnumDedup(t *testing.T) {
	run(t, `r = Enum.dedup([1, 1, 2, 2, 2, 3, 1, 1])
echo $r`, "[1, 2, 3, 1]\n")
}

func TestStdlib_EnumUniqBy(t *testing.T) {
	run(t, `r = Enum.uniq_by([{1, "a"}, {2, "a"}, {1, "b"}], \x -> Tuple.at(x, 0))
echo $r`, "[{1, \"a\"}, {2, \"a\"}]\n")
}

// --- Map ---

func TestStdlib_MapNew(t *testing.T) {
	run(t, `r = Map.new([{"a", 1}, {"b", 2}])
echo $(Map.get(r, "a"))`, "1\n")
}

func TestStdlib_MapUpdate(t *testing.T) {
	run(t, `r = Map.update(%{x: 1}, "x", \v -> v + 10)
echo $(Map.get(r, "x"))`, "11\n")
}

func TestStdlib_MapTake(t *testing.T) {
	run(t, `r = Map.take(%{a: 1, b: 2, c: 3}, ["a", "c"])
echo $r`, "%{a: 1, c: 3}\n")
}

func TestStdlib_MapDrop(t *testing.T) {
	run(t, `r = Map.drop(%{a: 1, b: 2, c: 3}, ["b"])
echo $r`, "%{a: 1, c: 3}\n")
}

// --- String ---

func TestStdlib_StringCapitalize(t *testing.T) {
	run(t, `echo $(String.capitalize("hello world"))`, "Hello world\n")
}

func TestStdlib_StringTrimLeading(t *testing.T) {
	run(t, `echo $(String.trim_leading("  hello  "))`, "hello  \n")
}

func TestStdlib_StringTrimTrailing(t *testing.T) {
	run(t, `echo $(String.trim_trailing("  hello  "))`, "  hello\n")
}

func TestStdlib_StringReverse(t *testing.T) {
	run(t, `echo $(String.reverse("hello"))`, "olleh\n")
}

func TestStdlib_StringLengthUnicode(t *testing.T) {
	// String.length must count runes, not bytes
	bind(t, `x = String.length("café")`, "x", value.IntVal(4))
}

// --- List ---

func TestStdlib_ListLast(t *testing.T) {
	run(t, `echo $(List.last([1, 2, 3]))`, "3\n")
}

func TestStdlib_ListFirst(t *testing.T) {
	run(t, `echo $(List.first([1, 2, 3]))`, "1\n")
}
