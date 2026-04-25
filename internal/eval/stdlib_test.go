package eval

import (
	"testing"

	"ish/internal/value"
)

// =====================================================================
// Kernel: typeof
// =====================================================================

func TestKernel_TypeofInt(t *testing.T)    { bind(t, "x = typeof(42)", "x", value.AtomVal("integer")) }
func TestKernel_TypeofStr(t *testing.T)    { bind(t, `x = typeof("hi")`, "x", value.AtomVal("string")) }
func TestKernel_TypeofAtom(t *testing.T)   { bind(t, "x = typeof(:ok)", "x", value.AtomVal("atom")) }
func TestKernel_TypeofList(t *testing.T)   { bind(t, "x = typeof([1])", "x", value.AtomVal("list")) }
func TestKernel_TypeofMap(t *testing.T)    { bind(t, "x = typeof(%{a: 1})", "x", value.AtomVal("map")) }
func TestKernel_TypeofNil(t *testing.T)    { bind(t, "x = typeof(nil)", "x", value.AtomVal("nil")) }
func TestKernel_TypeofTuple(t *testing.T)  { bind(t, "x = typeof({:ok, 1})", "x", value.AtomVal("tuple")) }
func TestKernel_TypeofFn(t *testing.T)     { bind(t, "fn f x do x end\nx = typeof(&f)", "x", value.AtomVal("fn")) }

// =====================================================================
// Kernel: conversions
// =====================================================================

func TestKernel_ToStringInt(t *testing.T)   { run(t, "echo $(to_string(42))", "42\n") }
func TestKernel_ToIntegerStr(t *testing.T)  { bind(t, `x = to_integer("99")`, "x", value.IntVal(99)) }
func TestKernel_ToFloatStr(t *testing.T)    { bind(t, `x = to_float("3.14")`, "x", value.FloatVal(3.14)) }
func TestKernel_ToFloatInt(t *testing.T)    { bind(t, "x = to_float(3)", "x", value.FloatVal(3.0)) }

// =====================================================================
// Kernel: apply
// =====================================================================

func TestKernel_Apply(t *testing.T) {
	run(t, "fn add a, b do a + b end\necho $(apply(&add, [3, 4]))", "7\n")
}

// =====================================================================
// String: full coverage
// =====================================================================

func TestString_PadLeft(t *testing.T) {
	run(t, `echo $(String.pad_left("hi", 5, " "))`, "   hi\n")
}

func TestString_PadRight(t *testing.T) {
	run(t, `echo $(String.pad_right("hi", 5, " "))`, "hi   \n")
}

func TestString_IndexOf(t *testing.T) {
	bind(t, `x = String.index_of("hello world", "world")`, "x", value.IntVal(6))
}

func TestString_IndexOfNotFound(t *testing.T) {
	bind(t, `x = String.index_of("hello", "xyz")`, "x", value.IntVal(-1))
}

// =====================================================================
// Map: additional coverage
// =====================================================================

func TestMap_Pairs(t *testing.T) {
	run(t, `echo $(Map.pairs(%{a: 1, b: 2}))`, `[{"a", 1}, {"b", 2}]`+"\n")
}

func TestMap_Reduce(t *testing.T) {
	bind(t, `x = Map.reduce(%{a: 1, b: 2}, 0, \acc, k, v -> acc + v)`, "x", value.IntVal(3))
}

// =====================================================================
// List: additional coverage
// =====================================================================

func TestList_Flatten(t *testing.T) {
	run(t, "echo $(List.flatten([[1, 2], [3, 4]]))", "[1, 2, 3, 4]\n")
}

func TestList_Zip(t *testing.T) {
	run(t, "echo $(List.zip([1, 2], [3, 4]))", "[{1, 3}, {2, 4}]\n")
}

func TestList_Uniq(t *testing.T) {
	run(t, "echo $(List.uniq([1, 2, 2, 3, 1]))", "[1, 2, 3]\n")
}

func TestList_Take(t *testing.T) {
	run(t, "echo $(List.take([1, 2, 3, 4, 5], 3))", "[1, 2, 3]\n")
}

func TestList_Drop(t *testing.T) {
	run(t, "echo $(List.drop([1, 2, 3, 4, 5], 2))", "[3, 4, 5]\n")
}

// =====================================================================
// Enum: additional coverage
// =====================================================================

func TestEnum_Any(t *testing.T) {
	bind(t, `x = Enum.any([1, 2, 3], \n -> n > 2)`, "x", value.True)
}

func TestEnum_All(t *testing.T) {
	bind(t, `x = Enum.all([2, 4, 6], \n -> n > 0)`, "x", value.True)
}

func TestEnum_Find(t *testing.T) {
	bind(t, `x = Enum.find([1, 2, 3], \n -> n > 1)`, "x", value.IntVal(2))
}

func TestEnum_MinMax(t *testing.T) {
	bind(t, "x = Enum.min([3, 1, 2])", "x", value.IntVal(1))
	bind(t, "x = Enum.max([3, 1, 2])", "x", value.IntVal(3))
}

func TestEnum_Sort(t *testing.T) {
	run(t, "echo $(Enum.sort([3, 1, 2]))", "[1, 2, 3]\n")
}

func TestEnum_Reverse(t *testing.T) {
	run(t, "echo $(Enum.reverse([1, 2, 3]))", "[3, 2, 1]\n")
}

func TestEnum_Each(t *testing.T) {
	run(t, `Enum.each [1, 2, 3], \x -> echo $x`, "1\n2\n3\n")
}

func TestEnum_Zip(t *testing.T) {
	run(t, "echo $(Enum.zip([1, 2], [3, 4]))", "[{1, 3}, {2, 4}]\n")
}

// =====================================================================
// Math: additional coverage
// =====================================================================

func TestMath_MinMax(t *testing.T) {
	bind(t, "x = Math.min(3, 7)", "x", value.IntVal(3))
	bind(t, "x = Math.max(3, 7)", "x", value.IntVal(7))
}

func TestMath_Pi(t *testing.T) {
	bind(t, "x = Math.pi", "x", value.FloatVal(3.141592653589793))
}

// =====================================================================
// Regex: additional coverage
// =====================================================================

func TestRegex_MatchNil(t *testing.T) {
	bind(t, `x = Regex.match("hello", "[0-9]+")`, "x", value.Nil)
}

func TestRegex_ReplaceAll(t *testing.T) {
	run(t, `echo $(Regex.replace_all("a1b2c3", "[0-9]", "X"))`, "aXbXcX\n")
}

// =====================================================================
// Path: additional coverage
// =====================================================================

func TestPath_Abs(t *testing.T) {
	// Path.abs should resolve to absolute
	env := NewEnv()
	Run(`x = Path.abs(".")`, env)
	v, ok := env.Get("x")
	if !ok || v.Kind != value.VString || len(v.Str()) < 2 || v.Str()[0] != '/' {
		t.Errorf("Path.abs(\".\") should return absolute path, got %s", v)
	}
}

// =====================================================================
// JSON: edge cases
// =====================================================================

func TestJSON_ParseNull(t *testing.T) {
	bind(t, `x = JSON.parse("null")`, "x", value.Nil)
}

func TestJSON_ParseArray(t *testing.T) {
	run(t, `echo $(JSON.parse("[1,2,3]"))`, "[1, 2, 3]\n")
}

func TestJSON_EncodeTuple(t *testing.T) {
	run(t, "echo $(JSON.encode({:ok, 42}))", `["ok",42]`+"\n")
}

// =====================================================================
// CSV
// =====================================================================

func TestCSV_ParseBasic(t *testing.T) {
	run(t, `rows = CSV.parse("a,b\n1,2\n")`+"\necho $(length(rows))", "2\n")
}

func TestCSV_RoundTrip(t *testing.T) {
	run(t, `data = [["name", "age"], ["fox", "3"]]`+"\ncsv = CSV.encode(data)\nrows = CSV.parse(csv)\necho $(length(rows))", "2\n")
}

// =====================================================================
// IO
// =====================================================================

func TestIO_LinesBasic(t *testing.T) {
	run(t, `echo $(IO.lines("a\nb\nc"))`, `["a", "b", "c"]`+"\n")
}

func TestIO_LinesSingle(t *testing.T) {
	run(t, `echo $(IO.lines("hello"))`, `["hello"]`+"\n")
}
