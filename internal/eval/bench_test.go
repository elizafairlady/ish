package eval

import (
	"testing"

	"ish/internal/core"
	"ish/internal/lexer"
	"ish/internal/parser"
	"ish/internal/process"
	"ish/internal/stdlib"
)

// baseEnv is the prelude-loaded env, created once and shared.
// Each benchmark iteration gets a child env so mutations don't leak.
var baseEnv *core.Env

func init() {
	baseEnv = core.TopEnv()
	baseEnv.Ctx.Proc = process.NewProcess()
	stdlib.Register(baseEnv)
	baseEnv.Ctx.CmdSub = RunCmdSub
	baseEnv.Ctx.CallFn = CallFn
	stdlib.LoadPrelude(baseEnv, func(src string, e *core.Env) {
		RunSource(src, e) //nolint: errcheck
	})
}

func benchEnv() *core.Env {
	return core.NewEnv(baseEnv)
}

func benchParse(b *testing.B, src string) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l := lexer.New(src)
		_, err := parser.Parse(l)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchEval(b *testing.B, src string) {
	// Parse once
	l := lexer.New(src)
	node, err := parser.Parse(l)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := benchEnv()
		Eval(node, env)
	}
}

func benchFull(b *testing.B, src string) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		env := benchEnv()
		RunSource(src, env)
	}
}

// --- Parse benchmarks ---

func BenchmarkParse_SimpleCommand(b *testing.B) {
	benchParse(b, `echo hello world`)
}

func BenchmarkParse_Pipeline(b *testing.B) {
	benchParse(b, `echo hello | cat | sort | head -n 5`)
}

func BenchmarkParse_IshBinding(b *testing.B) {
	benchParse(b, `x = 2 + 3 * 4`)
}

func BenchmarkParse_FnDef(b *testing.B) {
	benchParse(b, "fn fib n when n > 1 do\nfib (n - 1) + fib (n - 2)\nend")
}

func BenchmarkParse_NestedControlFlow(b *testing.B) {
	benchParse(b, "if true; then\nfor i in a b c; do\necho $i\ndone\nfi")
}

func BenchmarkParse_DataStructures(b *testing.B) {
	benchParse(b, `m = %{name: "alice", scores: [1, 2, 3], meta: {:ok, 42}}`)
}

func BenchmarkParse_MixedPipeline(b *testing.B) {
	benchParse(b, `r = printf "a\nb\nc\n" | sort |> List.reverse |> IO.lines`)
}

// --- Eval benchmarks (shared env, measures marginal cost) ---

func benchEvalShared(b *testing.B, src string) {
	env := benchEnv()
	l := lexer.New(src)
	node, err := parser.Parse(l)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child := core.NewEnv(env)
		Eval(node, child)
	}
}

func BenchmarkEval_Arithmetic(b *testing.B) {
	benchEvalShared(b, `x = 2 + 3 * 4`)
}

func BenchmarkEval_StringConcat(b *testing.B) {
	benchEvalShared(b, `x = "hello" + " " + "world"`)
}

func BenchmarkEval_PatternMatch(b *testing.B) {
	benchEvalShared(b, `{a, b, c} = {1, 2, 3}`)
}

func BenchmarkEval_ListOps(b *testing.B) {
	benchEvalShared(b, `x = [1, 2, 3, 4, 5]`)
}

func BenchmarkEval_MapAccess(b *testing.B) {
	benchEvalShared(b, `m = %{x: 10, y: 20}`)
}

func BenchmarkEval_FnCallSimple(b *testing.B) {
	env := benchEnv()
	RunSource("fn double x do x * 2 end", env)
	l := lexer.New("r = double 21")
	node, _ := parser.ParseWithEnv(l, makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child := core.NewEnv(env)
		Eval(node, child)
	}
}

func BenchmarkEval_ModuleQualified(b *testing.B) {
	env := benchEnv()
	l := lexer.New(`r = List.map [1, 2, 3], \x -> x * 2`)
	node, _ := parser.ParseWithEnv(l, makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child := core.NewEnv(env)
		Eval(node, child)
	}
}

func BenchmarkEval_PipeArrow3(b *testing.B) {
	env := benchEnv()
	RunSource("fn inc x do x + 1 end\nfn double x do x * 2 end", env)
	l := lexer.New("r = 5 |> inc |> double |> inc")
	node, _ := parser.ParseWithEnv(l, makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child := core.NewEnv(env)
		Eval(node, child)
	}
}

func BenchmarkEval_Fib15(b *testing.B) {
	env := benchEnv()
	RunSource("fn fib 0 do 0 end\nfn fib 1 do 1 end\nfn fib n when n > 1 do fib (n - 1) + fib (n - 2) end", env)
	l := lexer.New("r = fib 15")
	node, _ := parser.ParseWithEnv(l, makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child := core.NewEnv(env)
		Eval(node, child)
	}
}

func BenchmarkEval_Fib25(b *testing.B) {
	env := benchEnv()
	RunSource("fn fib 0 do 0 end\nfn fib 1 do 1 end\nfn fib n when n > 1 do fib (n - 1) + fib (n - 2) end", env)
	l := lexer.New("r = fib 25")
	node, _ := parser.ParseWithEnv(l, makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child := core.NewEnv(env)
		Eval(node, child)
	}
}

func BenchmarkStartup(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchEnv()
	}
}

// --- Full (parse+eval) benchmarks ---

func BenchmarkFull_Echo(b *testing.B) {
	benchFull(b, `echo hello`)
}

func BenchmarkFull_Assignment(b *testing.B) {
	benchFull(b, "X=42\nY=hello")
}

func BenchmarkFull_FibIterative(b *testing.B) {
	benchFull(b, `fn fib 0 do
0
end
fn fib 1 do
1
end
fn fib n when n > 1 do
fib (n - 1) + fib (n - 2)
end
r = fib 15`)
}

func BenchmarkFull_ListMap(b *testing.B) {
	benchFull(b, `r = List.map [1, 2, 3, 4, 5], \x -> x * 2`)
}

func BenchmarkFull_ListReduce(b *testing.B) {
	benchFull(b, `r = List.reduce [1, 2, 3, 4, 5], 0, \acc, x -> acc + x`)
}

func BenchmarkFull_ForLoop(b *testing.B) {
	benchFull(b, "s = 0\nfor i in 1 2 3 4 5 6 7 8 9 10; do\ns = (s + $i)\ndone")
}

func BenchmarkFull_MatchExpr(b *testing.B) {
	benchFull(b, `x = 42
r = match x do
1 -> :one
2 -> :two
42 -> :answer
_ -> :other
end`)
}

func BenchmarkFull_ModuleCall(b *testing.B) {
	benchFull(b, `defmodule M do
fn double x do x * 2 end
end
r = M.double 21`)
}

func BenchmarkFull_PipeArrowChain(b *testing.B) {
	benchFull(b, `fn inc x do x + 1 end
fn double x do x * 2 end
r = 5 |> inc |> double |> inc`)
}

func BenchmarkFull_HeadTailRecursion(b *testing.B) {
	benchFull(b, `fn sum [] do 0 end
fn sum [h | t] do h + sum t end
r = sum [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]`)
}

func BenchmarkEval_FnCallAllocTrace(b *testing.B) {
	env := benchEnv()
	RunSource("fn double x do x * 2 end", env)
	node, _ := parser.ParseWithEnv(lexer.New("double 21"), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}

// ---------------------------------------------------------------------------
// Frame + args allocation stress tests.
// Vary arity, recursion depth, and call patterns to isolate the cost of
// Frame allocation and args slice allocation.
// ---------------------------------------------------------------------------

// Arity 0: zero-arg function (no args slice, still allocates Frame)
func BenchmarkAlloc_Arity0(b *testing.B) {
	env := benchEnv()
	RunSource("fn ping do :pong end", env)
	node, _ := parser.ParseWithEnv(lexer.New("ping"), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}

// Arity 1: single-arg (most common — fib, double, inc)
func BenchmarkAlloc_Arity1(b *testing.B) {
	env := benchEnv()
	RunSource("fn double x do x * 2 end", env)
	node, _ := parser.ParseWithEnv(lexer.New("double 21"), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}

// Arity 2: two-arg (add, map operations)
func BenchmarkAlloc_Arity2(b *testing.B) {
	env := benchEnv()
	RunSource("fn add a, b do a + b end", env)
	node, _ := parser.ParseWithEnv(lexer.New("add 3, 4"), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}

// Arity 3: three-arg (reduce-style)
func BenchmarkAlloc_Arity3(b *testing.B) {
	env := benchEnv()
	RunSource("fn clamp x, lo, hi do\nif x < lo do lo\nelse if x > hi do hi\nelse x\nend\nend\nend", env)
	node, _ := parser.ParseWithEnv(lexer.New("clamp 5, 0, 10"), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}

// Arity 5: five-arg (exceeds flat buffer of 4 — tests spill)
func BenchmarkAlloc_Arity5(b *testing.B) {
	env := benchEnv()
	RunSource("fn sum5 a, b, c, d, e do a + b + c + d + e end", env)
	node, _ := parser.ParseWithEnv(lexer.New("sum5 1, 2, 3, 4, 5"), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}

// Deep recursion arity 1: countdown 100 (linear recursion, tail-call eligible)
func BenchmarkAlloc_Recurse100(b *testing.B) {
	env := benchEnv()
	RunSource("fn countdown n do\nif n == 0 do :done\nelse countdown (n - 1)\nend\nend", env)
	node, _ := parser.ParseWithEnv(lexer.New("countdown 100"), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}

// Deep recursion arity 2: ackermann(2,3) — non-tail-recursive, multi-arg
func BenchmarkAlloc_Ackermann2_3(b *testing.B) {
	env := benchEnv()
	RunSource("fn ack do\n0, n -> n + 1\nm, 0 -> ack(m - 1, 1)\nm, n -> ack(m - 1, ack(m, n - 1))\nend", env)
	node, _ := parser.ParseWithEnv(lexer.New("ack 2, 3"), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}

// Multi-clause dispatch: pattern matching iterates clauses before finding match
func BenchmarkAlloc_MultiClause(b *testing.B) {
	env := benchEnv()
	RunSource(`fn classify do
:zero -> 0
:one -> 1
:two -> 2
:three -> 3
n -> n
end`, env)
	node, _ := parser.ParseWithEnv(lexer.New("classify :three"), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}

// Closure call: captured environment adds parent chain depth
func BenchmarkAlloc_Closure(b *testing.B) {
	env := benchEnv()
	RunSource("fn make_adder n do \\x -> x + n end\nadd5 = make_adder 5", env)
	node, _ := parser.ParseWithEnv(lexer.New("add5 10"), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}

// Module-qualified call via NCall: List.map(list, fn)
func BenchmarkAlloc_ModuleNCall(b *testing.B) {
	env := benchEnv()
	RunSource("xs = [1, 2, 3, 4, 5]", env)
	node, _ := parser.ParseWithEnv(lexer.New(`List.map(xs, \x -> x * 2)`), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}

// Fib15: the canonical recursive benchmark
func BenchmarkAlloc_Fib15(b *testing.B) {
	env := benchEnv()
	RunSource("fn fib do\n0 -> 0\n1 -> 1\nn -> fib(n - 1) + fib(n - 2)\nend", env)
	node, _ := parser.ParseWithEnv(lexer.New("fib 15"), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}

// Higher-order: map over list calls a lambda per element
func BenchmarkAlloc_MapLambda10(b *testing.B) {
	env := benchEnv()
	RunSource("xs = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]", env)
	node, _ := parser.ParseWithEnv(lexer.New(`List.map(xs, \x -> x * 2)`), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}

// Chain of pipe arrows: each |> is a function call
func BenchmarkAlloc_PipeChain5(b *testing.B) {
	env := benchEnv()
	RunSource("fn inc x do x + 1 end\nfn dbl x do x * 2 end", env)
	node, _ := parser.ParseWithEnv(lexer.New("1 |> inc |> dbl |> inc |> dbl |> inc"), makeSymbolLookup(env))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Eval(node, env)
	}
}
