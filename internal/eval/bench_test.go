package eval

import (
	"testing"

	"ish/internal/lexer"
	"ish/internal/parser"
)

var benchBaseEnv *Env

func init() {
	benchBaseEnv = NewEnv()
}

func benchChildEnv() *Env {
	return newChildEnv(benchBaseEnv)
}

func benchParse(b *testing.B, src string) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tokens := lexer.Lex(src)
		p := parser.New(tokens)
		_, err := p.Parse()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchEvalShared(b *testing.B, src string) {
	tokens := lexer.Lex(src)
	p := parser.New(tokens)
	node, err := p.Parse()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child := benchChildEnv()
		eval(node, child, false)
	}
}

func benchFull(b *testing.B, src string) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		env := benchChildEnv()
		Run(src, env)
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
	benchParse(b, "fn fib n when n > 1 do\nfib(n - 1) + fib(n - 2)\nend")
}

func BenchmarkParse_NestedControlFlow(b *testing.B) {
	benchParse(b, "if true; then\nfor i in a b c; do\necho $i\ndone\nfi")
}

func BenchmarkParse_DataStructures(b *testing.B) {
	benchParse(b, `m = %{name: "alice", scores: [1, 2, 3], meta: {:ok, 42}}`)
}

// --- Eval benchmarks ---

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
	env := benchChildEnv()
	Run("fn double x do x * 2 end", env)
	tokens := lexer.Lex("r = double(21)")
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child := newChildEnv(env)
		eval(node, child, false)
	}
}

func BenchmarkEval_ModuleQualified(b *testing.B) {
	env := benchChildEnv()
	tokens := lexer.Lex(`r = List.map([1, 2, 3], \x -> x * 2)`)
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child := newChildEnv(env)
		eval(node, child, false)
	}
}

func BenchmarkEval_PipeArrow3(b *testing.B) {
	env := benchChildEnv()
	Run("fn inc x do x + 1 end\nfn double x do x * 2 end", env)
	tokens := lexer.Lex("r = 5 |> inc |> double |> inc")
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child := newChildEnv(env)
		eval(node, child, false)
	}
}

func BenchmarkEval_Fib15(b *testing.B) {
	env := benchChildEnv()
	Run("fn fib 0 do 0 end\nfn fib 1 do 1 end\nfn fib n when n > 1 do fib(n - 1) + fib(n - 2) end", env)
	tokens := lexer.Lex("r = fib(15)")
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child := newChildEnv(env)
		eval(node, child, false)
	}
}

func BenchmarkEval_Fib25(b *testing.B) {
	env := benchChildEnv()
	Run("fn fib 0 do 0 end\nfn fib 1 do 1 end\nfn fib n when n > 1 do fib(n - 1) + fib(n - 2) end", env)
	tokens := lexer.Lex("r = fib(25)")
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child := newChildEnv(env)
		eval(node, child, false)
	}
}

func BenchmarkStartup(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		newChildEnv(benchBaseEnv)
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
	benchFull(b, `fn fib 0 do 0 end
fn fib 1 do 1 end
fn fib n when n > 1 do fib(n - 1) + fib(n - 2) end
r = fib(15)`)
}

func BenchmarkFull_ListMap(b *testing.B) {
	benchFull(b, `r = List.map([1, 2, 3, 4, 5], \x -> x * 2)`)
}

func BenchmarkFull_ListReduce(b *testing.B) {
	benchFull(b, `r = List.reduce([1, 2, 3, 4, 5], 0, \acc, x -> acc + x)`)
}

func BenchmarkFull_ForLoop(b *testing.B) {
	benchFull(b, "s = 0\nfor i in 1 2 3 4 5 6 7 8 9 10; do\ns = s + $i\ndone")
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
r = M.double(21)`)
}

func BenchmarkFull_PipeArrowChain(b *testing.B) {
	benchFull(b, `fn inc x do x + 1 end
fn double x do x * 2 end
r = 5 |> inc |> double |> inc`)
}

func BenchmarkFull_HeadTailRecursion(b *testing.B) {
	benchFull(b, `fn sum [] do 0 end
fn sum [h | t] do h + sum(t) end
r = sum([1, 2, 3, 4, 5, 6, 7, 8, 9, 10])`)
}

// --- Allocation stress ---

func BenchmarkAlloc_Arity0(b *testing.B) {
	env := benchChildEnv()
	Run("fn ping do :pong end", env)
	tokens := lexer.Lex("ping")
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval(node, env, false)
	}
}

func BenchmarkAlloc_Arity1(b *testing.B) {
	env := benchChildEnv()
	Run("fn double x do x * 2 end", env)
	tokens := lexer.Lex("double(21)")
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval(node, env, false)
	}
}

func BenchmarkAlloc_Arity2(b *testing.B) {
	env := benchChildEnv()
	Run("fn add a, b do a + b end", env)
	tokens := lexer.Lex("add(3, 4)")
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval(node, env, false)
	}
}

func BenchmarkAlloc_Arity3(b *testing.B) {
	env := benchChildEnv()
	Run("fn clamp x, lo, hi do\nif x < lo do lo else if x > hi do hi else x end end\nend", env)
	tokens := lexer.Lex("clamp(5, 0, 10)")
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval(node, env, false)
	}
}

func BenchmarkAlloc_Arity5(b *testing.B) {
	env := benchChildEnv()
	Run("fn sum5 a, b, c, d, e do a + b + c + d + e end", env)
	tokens := lexer.Lex("sum5(1, 2, 3, 4, 5)")
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval(node, env, false)
	}
}

func BenchmarkAlloc_Recurse100(b *testing.B) {
	env := benchChildEnv()
	Run("fn countdown n do\nif n == 0 do :done else countdown(n - 1) end\nend", env)
	tokens := lexer.Lex("countdown(100)")
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval(node, env, false)
	}
}

func BenchmarkAlloc_Closure(b *testing.B) {
	env := benchChildEnv()
	Run("fn make_adder n do \\x -> x + n end\nadd5 = make_adder(5)", env)
	tokens := lexer.Lex("add5(10)")
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval(node, env, false)
	}
}

func BenchmarkAlloc_ModuleNCall(b *testing.B) {
	env := benchChildEnv()
	Run("xs = [1, 2, 3, 4, 5]", env)
	tokens := lexer.Lex(`List.map(xs, \x -> x * 2)`)
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval(node, env, false)
	}
}

func BenchmarkAlloc_Fib15(b *testing.B) {
	env := benchChildEnv()
	Run("fn fib 0 do 0 end\nfn fib 1 do 1 end\nfn fib n when n > 1 do fib(n - 1) + fib(n - 2) end", env)
	tokens := lexer.Lex("fib(15)")
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval(node, env, false)
	}
}

func BenchmarkAlloc_MapLambda10(b *testing.B) {
	env := benchChildEnv()
	Run("xs = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]", env)
	tokens := lexer.Lex(`List.map(xs, \x -> x * 2)`)
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval(node, env, false)
	}
}

func BenchmarkAlloc_PipeChain5(b *testing.B) {
	env := benchChildEnv()
	Run("fn inc x do x + 1 end\nfn dbl x do x * 2 end", env)
	tokens := lexer.Lex("1 |> inc |> dbl |> inc |> dbl |> inc")
	p := parser.New(tokens)
	node, _ := p.Parse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eval(node, env, false)
	}
}
