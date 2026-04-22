package eval

import (
	"testing"

	"ish/internal/core"
	"ish/internal/lexer"
	"ish/internal/parser"
	"ish/internal/process"
	"ish/internal/stdlib"
)

func benchEnv() *core.Env {
	env := core.TopEnv()
	env.Proc = process.NewProcess()
	stdlib.Register(env)
	env.Ctx.CmdSub = RunCmdSub
	env.Ctx.CallFn = CallFn
	stdlib.LoadPrelude(env, func(src string, e *core.Env) {
		RunSource(src, e) //nolint: errcheck
	})
	return env
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
	benchParse(b, `r = printf "a\nb\nc\n" | sort |> IO.lines |> List.reverse |> IO.unlines`)
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
	node, _ := parser.Parse(l)
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
	node, _ := parser.Parse(l)
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
	node, _ := parser.Parse(l)
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
	node, _ := parser.Parse(l)
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
	node, _ := parser.Parse(l)
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
