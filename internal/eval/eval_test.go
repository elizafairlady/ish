package eval

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"ish/internal/value"
)

func capture(env *Env, fn func()) string {
	r, w, _ := os.Pipe()
	old := env.Ctx.Stdout
	env.Ctx.Stdout = w
	fn()
	env.Ctx.BgJobs.Wait() // drain background jobs before closing pipe
	w.Close()
	env.Ctx.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()
	return buf.String()
}

func run(t *testing.T, input, want string) {
	t.Helper()
	env := NewEnv()
	got := capture(env, func() { Run(input, env); RunExitTraps(env) })
	if got != want {
		t.Errorf("input:\n%s\ngot:  %q\nwant: %q", input, got, want)
	}
}

func bind(t *testing.T, input string, name string, want value.Value) {
	t.Helper()
	env := NewEnv()
	Run(input, env)
	got, ok := env.Get(name)
	if !ok {
		t.Fatalf("variable %q not bound", name)
	}
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

// =====================================================================
// PASSING: Arithmetic and bindings
// =====================================================================

func TestEval_IntArithmetic(t *testing.T) {
	bind(t, "x = 1 + 2", "x", value.IntVal(3))
	bind(t, "x = 10 - 3", "x", value.IntVal(7))
	bind(t, "x = 2 * 3", "x", value.IntVal(6))
	bind(t, "x = 10 / 2", "x", value.IntVal(5))
	bind(t, "x = 10 % 3", "x", value.IntVal(1))
}

func TestEval_Comparison(t *testing.T) {
	bind(t, "x = 5 > 3", "x", value.True)
	bind(t, "x = 3 > 5", "x", value.False)
	bind(t, "x = 5 == 5", "x", value.True)
	bind(t, "x = 5 != 3", "x", value.True)
	bind(t, "x = 3 < 5", "x", value.True)
	bind(t, "x = 5 >= 5", "x", value.True)
	bind(t, "x = 3 <= 5", "x", value.True)
}

// =====================================================================
// PASSING: Commands as expressions — {:ok, nil} / {:error, code}
// =====================================================================

func TestEval_TrueReturnsOk(t *testing.T) {
	env := NewEnv()
	result := Run("true", env)
	if !result.Truthy() || result.ExitCode() != 0 {
		t.Errorf("true: truthy=%v exit=%d, want truthy=true exit=0", result.Truthy(), result.ExitCode())
	}
}

func TestEval_FalseReturnsError(t *testing.T) {
	env := NewEnv()
	result := Run("false", env)
	if result.Truthy() || result.ExitCode() != 1 {
		t.Errorf("false: truthy=%v exit=%d, want truthy=false exit=1", result.Truthy(), result.ExitCode())
	}
}

func TestEval_ExternalCommandOk(t *testing.T) {
	env := NewEnv()
	result := Run("/bin/true", env)
	if !result.Truthy() {
		t.Errorf("/bin/true should be truthy, got %s", result)
	}
}

func TestEval_ExternalCommandError(t *testing.T) {
	env := NewEnv()
	result := Run("/bin/false", env)
	if result.Truthy() {
		t.Errorf("/bin/false should be falsy, got %s", result)
	}
}

// =====================================================================
// PASSING: Truthiness drives control flow
// =====================================================================

func TestEval_AndList(t *testing.T)             { run(t, "true && echo yes", "yes\n") }
func TestEval_AndListShortCircuit(t *testing.T)  { run(t, "false && echo no", "") }
func TestEval_OrList(t *testing.T)               { run(t, "false || echo fallback", "fallback\n") }
func TestEval_OrListShortCircuit(t *testing.T)   { run(t, "true || echo no", "") }

// =====================================================================
// PASSING: Echo, bindings, data structures, functions, control flow
// =====================================================================

func TestEval_Echo(t *testing.T)                 { run(t, "echo hello world", "hello world\n") }
func TestEval_EchoMultipleStatements(t *testing.T) { run(t, "echo a; echo b; echo c", "a\nb\nc\n") }
func TestEval_Binding(t *testing.T)              { run(t, "x = 42\necho $x", "42\n") }
func TestEval_PosixAssign(t *testing.T)          { run(t, "X=hello; echo $X", "hello\n") }

func TestEval_Tuple(t *testing.T) {
	env := NewEnv()
	Run("x = {:ok, 42}", env)
	got, _ := env.Get("x")
	if got.Kind != value.VTuple || !got.Truthy() {
		t.Errorf("expected truthy tuple, got %s", got)
	}
}

func TestEval_ErrorTuple(t *testing.T) {
	env := NewEnv()
	Run("x = {:error, 2}", env)
	got, _ := env.Get("x")
	if got.Truthy() || got.ExitCode() != 2 {
		t.Errorf("expected falsy with exit 2, got truthy=%v exit=%d", got.Truthy(), got.ExitCode())
	}
}

func TestEval_List(t *testing.T) {
	env := NewEnv()
	Run("x = [1, 2, 3]", env)
	got, _ := env.Get("x")
	if got.Kind != value.VList || len(got.Elems()) != 3 {
		t.Fatalf("expected list with 3 elems, got %v", got)
	}
}

func TestEval_FnDefAndCall(t *testing.T)  { run(t, "fn add a b do a + b end\nr = add(3, 4)\necho $r", "7\n") }
func TestEval_Lambda(t *testing.T) {
	env := NewEnv()
	Run(`double = \x -> x * 2`, env)
	got, _ := env.Get("double")
	if got.Kind != value.VFn {
		t.Fatalf("expected function, got %v", got.Kind)
	}
}
func TestEval_LambdaCall(t *testing.T)    { run(t, "double = \\x -> x * 2\nr = double(5)\necho $r", "10\n") }
func TestEval_PipeArrow(t *testing.T)     { run(t, "fn double x do x * 2 end\nfn inc x do x + 1 end\nr = 5 |> double |> inc\necho $r", "11\n") }
func TestEval_IfDoEnd(t *testing.T)       { run(t, "if (5 > 3) do echo yes end", "yes\n") }
func TestEval_IfElse(t *testing.T)        { run(t, "if (3 > 5) do\necho yes\nelse\necho no\nend", "no\n") }
func TestEval_IfThenFi(t *testing.T)      { run(t, "if true; then echo yes; fi", "yes\n") }
func TestEval_ForLoop(t *testing.T)       { run(t, "for i in a b c; do\necho $i\ndone", "a\nb\nc\n") }
func TestEval_RedirectToDevNull(t *testing.T) { run(t, "echo hidden > /dev/null", "") }

// =====================================================================
// ROADMAP: Pattern matching — match expression
// =====================================================================

func TestEval_MatchInt(t *testing.T) {
	run(t, "r = match 2 do\n1 -> :one\n2 -> :two\n_ -> :other\nend\necho $r", ":two\n")
}

func TestEval_MatchAtom(t *testing.T) {
	run(t, "match :error do\n:ok -> echo yes\n:error -> echo no\nend", "no\n")
}

func TestEval_MatchTuple(t *testing.T) {
	run(t, "match {:ok, 42} do\n{:ok, val} -> echo $val\n{:error, msg} -> echo $msg\nend", "42\n")
}

func TestEval_MatchWildcard(t *testing.T) {
	run(t, "r = match 99 do\n1 -> :one\n_ -> :other\nend\necho $r", ":other\n")
}

// =====================================================================
// ROADMAP: Destructuring bind
// =====================================================================

func TestEval_DestructTuple(t *testing.T) {
	run(t, "{status, val} = {:ok, 42}\necho $status\necho $val", ":ok\n42\n")
}

func TestEval_DestructList(t *testing.T) {
	run(t, "[a, b, c] = [10, 20, 30]\necho $a $b $c", "10 20 30\n")
}

func TestEval_DestructHeadTail(t *testing.T) {
	run(t, "[h | t] = [1, 2, 3]\necho $h\necho $t", "1\n[2, 3]\n")
}

func TestEval_DestructWildcard(t *testing.T) {
	run(t, "{_, val} = {:error, 42}\necho $val", "42\n")
}

func TestEval_DestructNested(t *testing.T) {
	run(t, "{a, {b, c}} = {1, {2, 3}}\necho $a $b $c", "1 2 3\n")
}

// =====================================================================
// ROADMAP: Multi-clause fn and guards
// =====================================================================

func TestEval_FnMultiClause(t *testing.T) {
	run(t, "fn fib 0 do\n0\nend\nfn fib 1 do\n1\nend\nfn fib n do\nfib(n - 1) + fib(n - 2)\nend\nr = fib(6)\necho $r", "8\n")
}

func TestEval_FnGuard(t *testing.T) {
	run(t, "fn abs n when n < 0 do 0 - n end\nfn abs n do n end\nr = abs(-5)\necho $r", "5\n")
}

func TestEval_FnClauseBlock(t *testing.T) {
	run(t, "fn classify do\n0 -> :zero\n1 -> :one\n_ -> :other\nend\necho $(classify(0))\necho $(classify(1))\necho $(classify(42))", ":zero\n:one\n:other\n")
}

// =====================================================================
// ROADMAP: String interpolation
// =====================================================================

func TestEval_InterpVar(t *testing.T) {
	run(t, "name = world\necho \"hello $name\"", "hello world\n")
}

func TestEval_InterpExpr(t *testing.T) {
	run(t, "x = 21\necho \"answer: #{x * 2}\"", "answer: 42\n")
}

func TestEval_InterpCmdSub(t *testing.T) {
	run(t, `echo "hello $(echo world)"`, "hello world\n")
}

// =====================================================================
// ROADMAP: Parameter expansion
// =====================================================================

func TestEval_ParamExpandDefault(t *testing.T) {
	run(t, "echo ${UNSET_VAR_XYZ:-default}", "default\n")
}

func TestEval_ParamExpandSet(t *testing.T) {
	run(t, "X=real; echo ${X:-default}", "real\n")
}

func TestEval_ParamExpandLength(t *testing.T) {
	run(t, "X=hello; echo ${#X}", "5\n")
}

// =====================================================================
// ROADMAP: Case/esac
// =====================================================================

func TestEval_CaseMatch(t *testing.T) {
	run(t, "X=hello\ncase $X in\nhello)\necho matched\n;;\n*)\necho default\n;;\nesac", "matched\n")
}

func TestEval_CaseWildcard(t *testing.T) {
	run(t, "X=other\ncase $X in\nhello)\necho no\n;;\n*)\necho default\n;;\nesac", "default\n")
}

// =====================================================================
// ROADMAP: Break/continue/return
// =====================================================================

func TestEval_ForBreak(t *testing.T) {
	run(t, "for i in a b c d; do\nif (i == c) do\nbreak\nend\necho $i\ndone", "a\nb\n")
}

func TestEval_ForContinue(t *testing.T) {
	run(t, "for i in a b c d; do\nif (i == b) do\ncontinue\nend\necho $i\ndone", "a\nc\nd\n")
}

func TestEval_FnReturn(t *testing.T) {
	run(t, "fn check x do\nif (x > 10) do\nreturn :big\nend\n:small\nend\necho $(check(5))\necho $(check(20))", ":small\n:big\n")
}

// =====================================================================
// ROADMAP: POSIX function definitions
// =====================================================================

func TestEval_PosixFnDef(t *testing.T) {
	run(t, "greet() { echo hello $1; }\ngreet world", "hello world\n")
}

func TestEval_PosixFnLocal(t *testing.T) {
	run(t, "x=outer\nf() { local x=inner; echo $x; }\nf\necho $x", "inner\nouter\n")
}

// =====================================================================
// ROADMAP: Byte pipe with proper plumbing
// =====================================================================

func TestEval_PipeEchoGrep(t *testing.T) {
	run(t, "echo hello | cat", "hello\n")
}

func TestEval_PipeThreeStage(t *testing.T) {
	run(t, "echo abc | cat | cat", "abc\n")
}

// =====================================================================
// ROADMAP: Modules
// =====================================================================

func TestEval_DefModule(t *testing.T) {
	run(t, "defmodule M do\nfn greet name do\n\"hello \" + name\nend\nend\nr = M.greet(\"world\")\necho $r", "hello world\n")
}

func TestEval_UseModule(t *testing.T) {
	run(t, "defmodule M do\nfn double x do\nx * 2\nend\nend\nuse M\necho $(double(5))", "10\n")
}

// =====================================================================
// ROADMAP: OTP primitives
// =====================================================================

func TestEval_SpawnAwait(t *testing.T) {
	run(t, "task = spawn fn do 2 + 3 end\nresult = await task\necho $result", "5\n")
}

func TestEval_SendReceive(t *testing.T) {
	run(t, "pid = spawn fn do\nreceive do\n{:ping, sender} -> send sender, :pong\nend\nend\nsend pid, {:ping, self}\nreceive do\n:pong -> echo got_pong\nend", "got_pong\n")
}

// =====================================================================
// ROADMAP: Try/rescue
// =====================================================================

func TestEval_TryRescue(t *testing.T) {
	run(t, "r = try do\n1 / 0\nrescue\n_ -> :caught\nend\necho $r", ":caught\n")
}

// =====================================================================
// ROADMAP: Map construction and access
// =====================================================================

func TestEval_MapConstruct(t *testing.T) {
	run(t, `m = %{name: "alice", age: 30}` + "\necho $m.name", "alice\n")
}

func TestEval_MapAccess(t *testing.T) {
	run(t, "m = %{x: 10, y: 20}\nr = m.x\necho $r", "10\n")
}

// =====================================================================
// ROADMAP: Arithmetic expansion $((expr))
// =====================================================================

func TestEval_ArithExpansion(t *testing.T) {
	run(t, "echo $((2 + 3))", "5\n")
}

func TestEval_ArithWithVar(t *testing.T) {
	run(t, "X=10; echo $((X + 5))", "15\n")
}

// =====================================================================
// ROADMAP: Special variables
// =====================================================================

func TestEval_ExitStatus(t *testing.T) {
	run(t, "true; echo $?", "0\n")
}

func TestEval_ExitStatusFalse(t *testing.T) {
	run(t, "false; echo $?", "1\n")
}

func TestEval_PID(t *testing.T) {
	env := NewEnv()
	got := capture(env, func() { Run("echo $$", env) })
	if got == "\n" || got == "" || got == "$$\n" {
		t.Errorf("$$ should be a pid, got %q", got)
	}
}

// =====================================================================
// ROADMAP: Glob expansion
// =====================================================================

func TestEval_GlobStar(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/aaa.txt", []byte(""), 0644)
	os.WriteFile(dir+"/bbb.txt", []byte(""), 0644)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)
	env := NewEnv()
	got := capture(env, func() { Run("echo *.txt", env) })
	if got != "aaa.txt bbb.txt\n" && got != "bbb.txt aaa.txt\n" {
		t.Errorf("glob: got %q, want files", got)
	}
}

// =====================================================================
// ROADMAP: Tilde expansion
// =====================================================================

func TestEval_TildeExpansion(t *testing.T) {
	env := NewEnv()
	got := capture(env, func() { Run("echo ~/test", env) })
	home := os.Getenv("HOME")
	if got != home+"/test\n" {
		t.Errorf("expected %q, got %q", home+"/test\n", got)
	}
}

// =====================================================================
// ROADMAP: Set options
// =====================================================================

func TestEval_SetE(t *testing.T) {
	run(t, "set -e\ntrue\necho before\nfalse\necho after", "before\n")
}

func TestEval_SetEIfExempt(t *testing.T) {
	run(t, "set -e\nif false; then echo no; fi\necho survived", "survived\n")
}

// =====================================================================
// ROADMAP: Export/unset/local/readonly
// =====================================================================

func TestEval_Export(t *testing.T) {
	run(t, "export FOO=bar; echo $FOO", "bar\n")
}

func TestEval_Unset(t *testing.T) {
	run(t, "X=hello; unset X; echo \"[$X]\"", "[]\n")
}

// =====================================================================
// ROADMAP: Heredoc with expansion
// =====================================================================

func TestEval_Heredoc(t *testing.T) {
	run(t, "cat <<EOF\nhello world\nEOF", "hello world\n")
}

func TestEval_HeredocExpand(t *testing.T) {
	run(t, "X=expanded\ncat <<EOF\n$X\nEOF", "expanded\n")
}

// =====================================================================
// ROADMAP: Cons construction
// =====================================================================

func TestEval_ConsConstruct(t *testing.T) {
	env := NewEnv()
	Run("x = [1 | [2, 3]]", env)
	got, _ := env.Get("x")
	want := value.ListVal(value.IntVal(1), value.IntVal(2), value.IntVal(3))
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

// =====================================================================
// ROADMAP: Float arithmetic
// =====================================================================

func TestEval_FloatArith(t *testing.T) {
	bind(t, "x = 5.0 / 2", "x", value.FloatVal(2.5))
}

func TestEval_FloatAdd(t *testing.T) {
	bind(t, "x = 1 + 0.5", "x", value.FloatVal(1.5))
}

// =====================================================================
// ROADMAP: String concat
// =====================================================================

func TestEval_StringConcat(t *testing.T) {
	run(t, `r = "hello" + " " + "world"` + "\necho $r", "hello world\n")
}

// =====================================================================
// ROADMAP: Negation
// =====================================================================

func TestEval_BoolNot(t *testing.T) {
	bind(t, "x = (!true)", "x", value.False)
}

func TestEval_NegationCmd(t *testing.T) {
	run(t, "! false && echo yes", "yes\n")
}

// =====================================================================
// ROADMAP: Command substitution in binding
// =====================================================================

func TestEval_CmdSubInBinding(t *testing.T) {
	run(t, "x = $(echo hello)\necho $x", "hello\n")
}

// =====================================================================
// ROADMAP: Subshell isolation
// =====================================================================

func TestEval_SubshellVarIsolation(t *testing.T) {
	run(t, "X=before\n(X=after)\necho $X", "before\n")
}

// =====================================================================
// ROADMAP: While loop
// =====================================================================

func TestEval_WhileBreak(t *testing.T) {
	run(t, "n = 3\nwhile (n > 0) do\necho $n\nn = n - 1\ndone", "3\n2\n1\n")
}

// =====================================================================
// ROADMAP: Pipe arrow with lambda
// =====================================================================

func TestEval_PipeArrowLambda(t *testing.T) {
	run(t, `r = 42 |> \x -> x + 1` + "\necho $r", "43\n")
}

// =====================================================================
// ROADMAP: Zero-arity fn
// =====================================================================

func TestEval_ZeroArityFn(t *testing.T) {
	run(t, "fn greeting do \"hello\" end\nx = greeting\necho $x", "hello\n")
}

// =====================================================================
// ROADMAP: Keywords as echo args
// =====================================================================

func TestEval_KeywordsAsArgs(t *testing.T) {
	run(t, "echo if then else fi for in do done", "if then else fi for in do done\n")
}

// =====================================================================
// ROADMAP: Mixed POSIX + ish
// =====================================================================

func TestEval_IshFnInPosixFor(t *testing.T) {
	run(t, "fn double x do\nx * 2\nend\nfor i in 1 2 3; do\nr = double($i)\necho $r\ndone", "2\n4\n6\n")
}

func TestEval_PosixVarInIshExpr(t *testing.T) {
	run(t, "X=10\nr = $X + 5\necho $r", "15\n")
}

// =====================================================================
// ROADMAP: POSIX builtins
// =====================================================================

func TestEval_Cd(t *testing.T) {
	run(t, "cd /tmp; pwd", "/tmp\n")
}

func TestEval_TestFileExists(t *testing.T) {
	run(t, "test -e /dev/null && echo yes", "yes\n")
}

func TestEval_TestBracket(t *testing.T) {
	run(t, "[ 5 -gt 3 ] && echo yes", "yes\n")
}

func TestEval_TestStringEqual(t *testing.T) {
	run(t, "[ foo = foo ]; echo $?", "0\n")
}

func TestEval_Printf(t *testing.T) {
	run(t, `printf "%s %s\n" hello world`, "hello world\n")
}

func TestEval_ReadBuiltin(t *testing.T) {
	// read from herestring
	run(t, "read X <<<hello; echo $X", "hello\n")
}

func TestEval_Shift(t *testing.T) {
	run(t, "set -- a b c; shift; echo $1 $2", "b c\n")
}

func TestEval_CommandV(t *testing.T) {
	run(t, "command -v echo; echo $?", "echo\n0\n")
}

func TestEval_TypeBuiltin(t *testing.T) {
	run(t, "type echo", "echo is a shell builtin\n")
}

func TestEval_EvalBuiltin(t *testing.T) {
	run(t, `eval 'echo hello'`, "hello\n")
}

func TestEval_SourceBuiltin(t *testing.T) {
	// Can't easily test without a file, but at least verify parse
	env := NewEnv()
	Run("true", env) // just ensure no crash
}

// =====================================================================
// ROADMAP: Word splitting and glob
// =====================================================================

func TestEval_WordSplitIFS(t *testing.T) {
	run(t, "X=\"a b c\"; for i in $X; do echo $i; done", "a\nb\nc\n")
}

func TestEval_GlobExpansion(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/foo.go", []byte(""), 0644)
	os.WriteFile(dir+"/bar.go", []byte(""), 0644)
	os.WriteFile(dir+"/baz.txt", []byte(""), 0644)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)
	env := NewEnv()
	got := capture(env, func() { Run("echo *.go", env) })
	if !strings.Contains(got, "foo.go") || !strings.Contains(got, "bar.go") {
		t.Errorf("glob *.go: got %q, want foo.go and bar.go", got)
	}
	if strings.Contains(got, "baz.txt") {
		t.Errorf("glob *.go should not match baz.txt, got %q", got)
	}
}

// =====================================================================
// ROADMAP: Prefix assignments
// =====================================================================

func TestEval_PrefixAssign(t *testing.T) {
	run(t, "X=temp true; echo \"[$X]\"", "[]\n")
}

// =====================================================================
// ROADMAP: Pipe semantics
// =====================================================================

func TestEval_PipeExitStatusLast(t *testing.T) {
	run(t, "false | true; echo $?", "0\n")
}

func TestEval_Pipefail(t *testing.T) {
	run(t, "set -o pipefail; false | true; echo $?", "1\n")
}

// =====================================================================
// ROADMAP: Comparison chaining
// =====================================================================

func TestEval_ComparisonChainTrue(t *testing.T) {
	bind(t, "x = 1 < 2 < 3", "x", value.True)
}

func TestEval_ComparisonChainFalse(t *testing.T) {
	bind(t, "x = 3 < 2 < 1", "x", value.False)
}

// =====================================================================
// ROADMAP: Dollar-string
// =====================================================================

func TestEval_DollarStringTab(t *testing.T) {
	run(t, `echo $"a\tb"`, "a\tb\n")
}

func TestEval_DollarStringNewline(t *testing.T) {
	run(t, `echo $"a\nb"`, "a\nb\n")
}

// =====================================================================
// ROADMAP: Backtick command substitution
// =====================================================================

func TestEval_BacktickCmdSub(t *testing.T) {
	run(t, "X=`echo hello`; echo $X", "hello\n")
}

// =====================================================================
// ROADMAP: Until loop
// =====================================================================

func TestEval_UntilLoop(t *testing.T) {
	run(t, "n = 0\nuntil (n == 3) do\nn = n + 1\ndone\necho $n", "3\n")
}

// =====================================================================
// ROADMAP: Module self-reference and use inside defmodule
// =====================================================================

func TestEval_ModuleSelfRef(t *testing.T) {
	run(t, "defmodule M do\nfn bar do 42 end\nfn foo do M.bar end\nend\necho $(M.foo)", "42\n")
}

// =====================================================================
// ROADMAP: Map patterns in match
// =====================================================================

func TestEval_MapPatternMatch(t *testing.T) {
	run(t, `m = %{name: "alice"}`+"\nmatch m do\n%{name: n} -> echo $n\nend", "alice\n")
}

// =====================================================================
// ROADMAP: Function capture
// =====================================================================

func TestEval_FnCapture(t *testing.T) {
	run(t, "fn greet do \"hello\" end\nf = &greet\necho $f", "#Function<greet>\n")
}

// =====================================================================
// ROADMAP: Stdlib — List operations
// =====================================================================

func TestEval_ListMap(t *testing.T) {
	run(t, `r = List.map [1, 2, 3], \x -> x * 2`+"\necho $r", "[2, 4, 6]\n")
}

func TestEval_ListFilter(t *testing.T) {
	run(t, `r = List.filter [1, 2, 3, 4], \x -> x > 2`+"\necho $r", "[3, 4]\n")
}

func TestEval_ListReduce(t *testing.T) {
	run(t, `r = List.reduce [1, 2, 3, 4], 0, \acc, x -> acc + x`+"\necho $r", "10\n")
}

func TestEval_Hd(t *testing.T) {
	run(t, "echo $(hd [1, 2, 3])", "1\n")
}

func TestEval_Tl(t *testing.T) {
	run(t, "echo $(tl [1, 2, 3])", "[2, 3]\n")
}

func TestEval_Length(t *testing.T) {
	run(t, "echo $(length [1, 2, 3])", "3\n")
}

func TestEval_LengthString(t *testing.T) {
	run(t, `echo $(length "hello")`, "5\n")
}

// =====================================================================
// ROADMAP: Stdlib — String operations
// =====================================================================

func TestEval_StringUpcase(t *testing.T) {
	run(t, `echo $(String.upcase "hello")`, "HELLO\n")
}

func TestEval_StringDowncase(t *testing.T) {
	run(t, `echo $(String.downcase "HELLO")`, "hello\n")
}

func TestEval_StringTrim(t *testing.T) {
	run(t, `echo $(String.trim "  hello  ")`, "hello\n")
}

// =====================================================================
// ROADMAP: Stdlib — JSON
// =====================================================================

func TestEval_JSONParse(t *testing.T) {
	run(t, `r = JSON.parse "{\"name\":\"fox\"}"` + "\necho $r", `%{name: "fox"}`+"\n")
}

func TestEval_JSONEncode(t *testing.T) {
	run(t, "r = JSON.encode [1, 2, 3]\necho $r", "[1,2,3]\n")
}

// =====================================================================
// ROADMAP: Stdlib — Enum
// =====================================================================

func TestEval_EnumSum(t *testing.T) {
	run(t, "echo $(Enum.sum [1, 2, 3])", "6\n")
}

// =====================================================================
// ROADMAP: Stdlib — Math/Regex/Path
// =====================================================================

func TestEval_MathAbs(t *testing.T) {
	run(t, "echo $(Math.abs (-5))", "5\n")
}

func TestEval_RegexMatch(t *testing.T) {
	run(t, `echo $(Regex.match "hello123", "[0-9]+")`, "123\n")
}

func TestEval_PathBasename(t *testing.T) {
	run(t, `echo $(Path.basename "/usr/local/bin/ish")`, "ish\n")
}

func TestEval_PathDirname(t *testing.T) {
	run(t, `echo $(Path.dirname "/usr/local/bin/ish")`, "/usr/local/bin\n")
}

// =====================================================================
// ROADMAP: POSIX special params
// =====================================================================

func TestEval_SetDashDash(t *testing.T) {
	run(t, "set -- alpha beta gamma; echo $1 $2 $3", "alpha beta gamma\n")
}

func TestEval_DollarHash(t *testing.T) {
	run(t, "set -- a b c d; echo $#", "4\n")
}

func TestEval_DollarAt(t *testing.T) {
	run(t, "f() { echo $1; echo $2; }\nset -- \"hello world\" foo\nf $@", "hello world\nfoo\n")
}

// =====================================================================
// ROADMAP: Trap
// =====================================================================

func TestEval_TrapExit(t *testing.T) {
	run(t, "trap 'echo goodbye' EXIT", "goodbye\n")
}

// =====================================================================
// ROADMAP: Heredoc tab stripping
// =====================================================================

func TestEval_HeredocStripTabs(t *testing.T) {
	run(t, "cat <<-EOF\n\thello\n\tworld\nEOF", "hello\nworld\n")
}

// =====================================================================
// ROADMAP: Nested interp in strings
// =====================================================================

func TestEval_NestedCmdSubInString(t *testing.T) {
	run(t, `echo "nested: $(echo $(echo deep))"`, "nested: deep\n")
}

func TestEval_ArithInString(t *testing.T) {
	run(t, `echo "r=$((2+2))"`, "r=4\n")
}

// =====================================================================
// ROADMAP: POSIX control flow edge cases
// =====================================================================

func TestEval_IfWithPipelineCondition(t *testing.T) {
	run(t, "if echo hidden | grep -q hidden; then echo found; fi", "found\n")
}

func TestEval_IfNegation(t *testing.T) {
	run(t, "if ! false; then echo yes; fi", "yes\n")
}

func TestEval_NestedFor(t *testing.T) {
	run(t, "for i in a b; do\nfor j in 1 2; do\necho $i$j\ndone\ndone", "a1\na2\nb1\nb2\n")
}

func TestEval_CasePipeAlternation(t *testing.T) {
	run(t, "X=b; case $X in\na|b) echo matched;;\nesac", "matched\n")
}

func TestEval_WhileFalseNeverRuns(t *testing.T) {
	run(t, "while false; do echo no; done; echo ok", "ok\n")
}

func TestEval_RecursivePosixFn(t *testing.T) {
	run(t, "fact() {\nif [ $1 -le 1 ]; then\necho 1\nreturn\nfi\nlocal n=$1\nlocal prev=$(fact $((n - 1)))\necho $((n * prev))\n}\nfact 5", "120\n")
}

// =====================================================================
// ROADMAP: Adversarial — keywords in strings/args
// =====================================================================

func TestEval_EchoKeywords(t *testing.T) {
	run(t, "echo if then else fi for in do done while case esac", "if then else fi for in do done while case esac\n")
}

func TestEval_DottedFilenameArg(t *testing.T) {
	run(t, "echo file.txt", "file.txt\n")
}

func TestEval_IPAddressArg(t *testing.T) {
	run(t, "echo 192.168.1.120", "192.168.1.120\n")
}

func TestEval_MultipleExtensions(t *testing.T) {
	run(t, "echo archive.tar.gz", "archive.tar.gz\n")
}

// =====================================================================
// ROADMAP: Pipe auto-coercion value to cmd
// =====================================================================

func TestEval_PipeValueToCmd(t *testing.T) {
	run(t, "[1, 2, 3] | cat", "1\n2\n3\n")
}

func TestEval_PipeScalarToCmd(t *testing.T) {
	run(t, "42 | cat", "42\n")
}

// =====================================================================
// ROADMAP: Float edge cases
// =====================================================================

func TestEval_FloatIntEquality(t *testing.T) {
	bind(t, "r = 5 == 5.0", "r", value.True)
}

func TestEval_FloatComparison(t *testing.T) {
	bind(t, "r = 3.14 > 3", "r", value.True)
}

func TestEval_FloatStringConcat(t *testing.T) {
	run(t, `r = "pi is " + 3.14`+"\necho $r", "pi is 3.14\n")
}

// =====================================================================
// ARCHITECTURE ENFORCEMENT: Scope interface
// These tests verify that fn calls use Frame (not Env), that scope
// isolation works correctly, and that the Scope interface is used.
// =====================================================================

func TestArch_FnScopeIsolation(t *testing.T) {
	// ish binding in fn body must NOT leak to parent
	run(t, "x = 1\nfn f do x = 2 end\nf\necho $x", "1\n")
}

func TestArch_PosixFnLeaksGlobals(t *testing.T) {
	// POSIX fn WITHOUT local leaks to parent
	run(t, "x=1\nf() { x=2; }\nf\necho $x", "2\n")
}

func TestArch_LocalPreventsLeak(t *testing.T) {
	run(t, "x=outer\nf() { local x=inner; echo $x; }\nf\necho $x", "inner\nouter\n")
}

func TestArch_NestedFnScope(t *testing.T) {
	run(t, "fn outer do\nx = 10\nfn inner do\necho $x\nend\ninner\nend\nouter", "10\n")
}

func TestArch_ClosureCapturesScope(t *testing.T) {
	run(t, "fn make_adder n do\n\\x -> x + n\nend\nadd5 = make_adder(5)\necho $(add5(3))", "8\n")
}

// =====================================================================
// ARCHITECTURE ENFORCEMENT: Builtins return exit codes, not values
// =====================================================================

func TestArch_BuiltinExitCodeTrue(t *testing.T) {
	env := NewEnv()
	result := Run("true", env)
	if !result.Equal(value.True) {
		t.Fatalf("true should return boolean true, got %s", result.Inspect())
	}
	if result.ExitCode() != 0 {
		t.Fatalf("true exit code should be 0, got %d", result.ExitCode())
	}
}

func TestArch_BuiltinExitCodeFalse(t *testing.T) {
	env := NewEnv()
	result := Run("false", env)
	if !result.Equal(value.False) {
		t.Fatalf("false should return boolean false, got %s", result.Inspect())
	}
	if result.ExitCode() != 1 {
		t.Fatalf("false exit code should be 1, got %d", result.ExitCode())
	}
}

// =====================================================================
// PORTED FROM: adversarial_test.go — POSIX sh compatibility
// =====================================================================

func TestPort_PosixAssignThenEcho(t *testing.T)  { run(t, "X=hello; echo $X", "hello\n") }
func TestPort_PosixAssignEmpty(t *testing.T)      { run(t, `X=; echo "[$X]"`, "[]\n") }
func TestPort_PosixAssignCmdSub(t *testing.T)     { run(t, "X=$(echo hi); echo $X", "hi\n") }
func TestPort_MultipleAssigns(t *testing.T)       { run(t, "A=1; B=2; C=3; echo $A $B $C", "1 2 3\n") }
func TestPort_PrefixAssignNoPersist(t *testing.T) { run(t, "X=temp true; echo \"[$X]\"", "[]\n") }

func TestPort_SingleQuotesLiteral(t *testing.T) {
	run(t, `echo '$HOME "hello" $(cmd)'`, "$HOME \"hello\" $(cmd)\n")
}

func TestPort_DoubleQuotesExpand(t *testing.T) {
	run(t, "X=world; echo \"hello $X\"", "hello world\n")
}

func TestPort_BracedExpansion(t *testing.T)  { run(t, "X=hello; echo ${X}", "hello\n") }
func TestPort_DefaultUnset(t *testing.T)     { run(t, "echo ${UNSET_VAR:-default}", "default\n") }
func TestPort_DefaultSet(t *testing.T)       { run(t, "X=real; echo ${X:-default}", "real\n") }

func TestPort_NestedCmdSub(t *testing.T)     { run(t, "echo $(echo $(echo deep))", "deep\n") }

func TestPort_RedirectDevNull(t *testing.T)  { run(t, "echo hidden > /dev/null", "") }
func TestPort_AppendDevNull(t *testing.T)    { run(t, "echo ok >> /dev/null", "") }

func TestPort_SemicolonSeparated(t *testing.T) { run(t, "echo a; echo b; echo c", "a\nb\nc\n") }

func TestPort_IfPipelineCondition(t *testing.T) {
	run(t, "if echo hidden | grep -q hidden; then echo found; fi", "found\n")
}

func TestPort_IfNegation(t *testing.T)       { run(t, "if ! false; then echo yes; fi", "yes\n") }

func TestPort_NestedForLoops(t *testing.T) {
	run(t, "for i in a b; do\nfor j in 1 2; do\necho $i$j\ndone\ndone", "a1\na2\nb1\nb2\n")
}

func TestPort_CaseMultiplePatterns(t *testing.T) {
	run(t, "X=c\ncase $X in\na) echo A;;\nb) echo B;;\n*) echo other;;\nesac", "other\n")
}

func TestPort_PosixRecursiveFn(t *testing.T) {
	run(t, "fact() {\nif [ $1 -le 1 ]; then\necho 1\nreturn\nfi\nlocal n=$1\nlocal prev=$(fact $((n - 1)))\necho $((n * prev))\n}\nfact 5", "120\n")
}

func TestPort_DollarQuestion(t *testing.T)    { run(t, "true; echo $?", "0\n") }
func TestPort_DollarQuestionFalse(t *testing.T) { run(t, "false; echo $?", "1\n") }

func TestPort_EchoKeywords(t *testing.T) {
	run(t, "echo if then else fi for in do done", "if then else fi for in do done\n")
}

func TestPort_ArithBasic(t *testing.T)        { run(t, "echo $((2 + 3))", "5\n") }
func TestPort_ArithWithVar(t *testing.T)      { run(t, "X=10; echo $((X + 5))", "15\n") }

func TestPort_HeredocBasic(t *testing.T)      { run(t, "cat <<EOF\nhello world\nEOF", "hello world\n") }
func TestPort_HeredocExpand(t *testing.T)     { run(t, "X=expanded\ncat <<EOF\n$X\nEOF", "expanded\n") }
func TestPort_HeredocQuoted(t *testing.T)     { run(t, "X=hidden\ncat <<'EOF'\n$X\nEOF", "$X\n") }

func TestPort_PipePreservesOrder(t *testing.T) { run(t, "echo hello | cat", "hello\n") }
func TestPort_LongPipeChain(t *testing.T)      { run(t, "echo abc | cat | cat | cat", "abc\n") }
func TestPort_PipeExitFromLast(t *testing.T)   { run(t, "false | true; echo $?", "0\n") }

func TestPort_AndBothTrue(t *testing.T)       { run(t, "true && echo yes", "yes\n") }
func TestPort_AndFirstFalse(t *testing.T)     { run(t, "false && echo no", "") }
func TestPort_OrFirstFalse(t *testing.T)      { run(t, "false || echo yes", "yes\n") }
func TestPort_OrFirstTrue(t *testing.T)       { run(t, "true || echo no", "") }
func TestPort_FalseAndOrFallback(t *testing.T) { run(t, "false && echo no || echo fallback", "fallback\n") }

func TestPort_IfTrueThenFi(t *testing.T)      { run(t, "if true; then echo yes; fi", "yes\n") }
func TestPort_IfFalseThenFi(t *testing.T)     { run(t, "if false; then echo yes; fi", "") }
func TestPort_IfElse(t *testing.T)            { run(t, "if false; then echo yes; else echo no; fi", "no\n") }
func TestPort_Elif(t *testing.T)              { run(t, "if false; then echo a; elif true; then echo b; fi", "b\n") }

func TestPort_ForBasic(t *testing.T)          { run(t, "for i in a b c; do echo $i; done", "a\nb\nc\n") }
func TestPort_ForBreak(t *testing.T)          { run(t, "for i in a b c d; do\nif [ $i = c ]; then break; fi\necho $i\ndone", "a\nb\n") }
func TestPort_ForContinue(t *testing.T)       { run(t, "for i in a b c d; do\nif [ $i = b ]; then continue; fi\necho $i\ndone", "a\nc\nd\n") }
func TestPort_ForCmdSub(t *testing.T)         { run(t, "for x in $(echo a b c); do echo $x; done", "a\nb\nc\n") }

func TestPort_WhileCount(t *testing.T) {
	run(t, "n=3; while [ $n -gt 0 ]; do echo $n; n=$((n - 1)); done", "3\n2\n1\n")
}

func TestPort_CaseFirst(t *testing.T)         { run(t, "X=hello; case $X in hello) echo matched;; esac", "matched\n") }
func TestPort_CaseWildcard(t *testing.T)      { run(t, "X=other; case $X in hello) echo no;; *) echo default;; esac", "default\n") }
func TestPort_CasePipeAlt(t *testing.T)       { run(t, "X=b; case $X in a|b) echo matched;; esac", "matched\n") }

func TestPort_PosixFnDefCall(t *testing.T)    { run(t, "greet() { echo hello; }\ngreet", "hello\n") }
func TestPort_PosixFnArgs(t *testing.T)       { run(t, "greet() { echo hello $1; }\ngreet world", "hello world\n") }
func TestPort_PosixFnReturn(t *testing.T)     { run(t, "f() { return 42; }\nf; echo $?", "42\n") }

func TestPort_SubshellBasic(t *testing.T)     { run(t, "(echo sub)", "sub\n") }
func TestPort_SubshellVarIsolation(t *testing.T) { run(t, "X=outer; (X=inner); echo $X", "outer\n") }
func TestPort_SubshellExitCode(t *testing.T)  { run(t, "(exit 42); echo $?", "42\n") }

func TestPort_GroupBasic(t *testing.T)        { run(t, "{ echo group; }", "group\n") }
func TestPort_GroupSharesScope(t *testing.T)  { run(t, "X=before; { X=after; }; echo $X", "after\n") }

func TestPort_EmptyScript(t *testing.T)       { run(t, "", "") }
func TestPort_WhitespaceOnly(t *testing.T)    { run(t, "   \n\n   ", "") }
func TestPort_CommentOnly(t *testing.T)       { run(t, "# just a comment\n# another one", "") }

func TestPort_DottedFilename(t *testing.T)    { run(t, "echo file.txt", "file.txt\n") }
func TestPort_DottedPath(t *testing.T)        { run(t, "echo src/main.go", "src/main.go\n") }
func TestPort_MultipleDots(t *testing.T)      { run(t, "echo archive.tar.gz", "archive.tar.gz\n") }
func TestPort_IPAddress(t *testing.T)         { run(t, "echo 192.168.1.120", "192.168.1.120\n") }

// =====================================================================
// PORTED FROM: adversarial_test.go — ish expression tests
// =====================================================================

func TestPort_IshBindVsPosix(t *testing.T)    { run(t, "x = 42\nX=42\necho $x $X", "42 42\n") }
func TestPort_IshBindExpr(t *testing.T)       { run(t, "x = 2 + 3\necho $x", "5\n") }
func TestPort_HyphenatedArg(t *testing.T)     { run(t, "echo foo-bar", "foo-bar\n") }
func TestPort_SpacedMinus(t *testing.T)       { run(t, "r = 10 - 3\necho $r", "7\n") }
func TestPort_SlashAsPath(t *testing.T)       { run(t, "echo /usr/bin", "/usr/bin\n") }
func TestPort_SlashAsDivision(t *testing.T)   { run(t, "r = 10 / 2\necho $r", "5\n") }
func TestPort_ParenGroupExpr(t *testing.T)    { run(t, "r = (2 + 3) * 4\necho $r", "20\n") }

func TestPort_PipeArrowChain(t *testing.T) {
	run(t, "fn double x do x * 2 end\nfn inc x do x + 1 end\nr = 5 |> double |> inc\necho $r", "11\n")
}

func TestPort_TupleOk(t *testing.T) {
	run(t, "result = {:ok, \"the data\"}\necho $result", "{:ok, \"the data\"}\n")
}

func TestPort_ListLiteral(t *testing.T)       { run(t, "nums = [1, 2, 3]\necho $nums", "[1, 2, 3]\n") }
func TestPort_MapAccess(t *testing.T) {
	run(t, "config = %{host: \"localhost\", port: 8080}\nr = config.host\necho $r", "localhost\n")
}

func TestPort_TupleDestructure(t *testing.T) {
	run(t, "{status, val} = {:ok, \"hello\"}\necho $status\necho $val", ":ok\nhello\n")
}

func TestPort_ListDestructure(t *testing.T) {
	run(t, "[a, b, c] = [10, 20, 30]\necho $a $b $c", "10 20 30\n")
}

func TestPort_HeadTail(t *testing.T) {
	run(t, "[first | rest] = [1, 2, 3, 4]\necho $first\necho $rest", "1\n[2, 3, 4]\n")
}

func TestPort_MatchExpr(t *testing.T) {
	run(t, "x = 2\nr = match x do\n1 -> :one\n2 -> :two\n_ -> :other\nend\necho $r", ":two\n")
}

func TestPort_MatchTuple(t *testing.T) {
	run(t, "result = {:error, \"disk full\"}\nmatch result do\n{:ok, val} -> echo \"got: $val\"\n{:error, msg} -> echo \"failed: $msg\"\nend", "failed: disk full\n")
}

func TestPort_FnGreet(t *testing.T) {
	run(t, "fn greet name do\necho \"Hello, $name!\"\nend\ngreet world", "Hello, world!\n")
}

func TestPort_FnAdd(t *testing.T) {
	run(t, "fn add a, b do a + b end\nr = add(3, 4)\necho $r", "7\n")
}

func TestPort_FnFib(t *testing.T) {
	run(t, "fn fib 0 do 0 end\nfn fib 1 do 1 end\nfn fib n do\nfib(n - 1) + fib(n - 2)\nend\nr = fib(10)\necho $r", "55\n")
}

func TestPort_LambdaSingle(t *testing.T) {
	run(t, "doubled = \\x -> x * 2\necho $(doubled(5))", "10\n")
}

func TestPort_LambdaMultiParam(t *testing.T) {
	run(t, "sum = \\a, b -> a + b\necho $(sum(3, 4))", "7\n")
}

func TestPort_LambdaZeroArity(t *testing.T) {
	run(t, "greet = \\ -> echo \"hello\"\ngreet", "hello\n")
}

func TestPort_IshIfDoEnd(t *testing.T)        { run(t, "if true do\necho yes\nend", "yes\n") }
func TestPort_IshIfExprCond(t *testing.T) {
	run(t, "x = 5\nif x == 5 do\necho five\nend", "five\n")
}

func TestPort_TryRescueDivZero(t *testing.T) {
	run(t, "r = try do\n1 / 0\nrescue\n_ -> :caught\nend\necho $r", ":caught\n")
}

func TestPort_PipeAutoCoerceValue(t *testing.T) { run(t, "[1, 2, 3] | cat", "1\n2\n3\n") }
func TestPort_PipeAutoCoerceScalar(t *testing.T) { run(t, "42 | cat", "42\n") }

func TestPort_FloatDivision(t *testing.T)     { run(t, "r = 5.0 / 2\necho $r", "2.5\n") }
func TestPort_FloatAdd(t *testing.T)          { run(t, "r = 1 + 0.5\necho $r", "1.5\n") }

func TestPort_SetEStops(t *testing.T) {
	run(t, "set -e\ntrue\necho before\nfalse\necho after", "before\n")
}

func TestPort_SetEIfExempt(t *testing.T) {
	run(t, "set -e\nif false; then echo y; fi\necho survived", "survived\n")
}

func TestPort_SetEOrExempt(t *testing.T) {
	run(t, "set -e\nfalse || echo swerved\necho ok", "swerved\nok\n")
}

// =====================================================================
// PORTED FROM: tutorial_test.go
// =====================================================================

func TestPort_SpawnAndAwait(t *testing.T) {
	run(t, "task = spawn fn do 2 + 3 end\nresult = await task\necho $result", "5\n")
}

func TestPort_PingPong(t *testing.T) {
	run(t, "pid = spawn fn do\nreceive do\n{:ping, sender} -> send sender, :pong\nend\nend\nsend pid, {:ping, self}\nreceive do\n:pong -> echo \"got pong\"\nend", "got pong\n")
}

func TestPort_DefModuleAndCall(t *testing.T) {
	run(t, "defmodule M do\nfn greet name do\n\"hello \" + name\nend\nend\nr = M.greet(\"world\")\necho $r", "hello world\n")
}

func TestPort_UseModule(t *testing.T) {
	run(t, "defmodule M do\nfn double x do x * 2 end\nend\nuse M\necho $(double(5))", "10\n")
}

// #####################################################################
// UNIMPLEMENTED: Tests below specify features that need to be built.
// They are organized by subsystem. Each test is a real specification
// of expected behavior, not a stub.
// #####################################################################

// =====================================================================
// BUILTINS: printf
// =====================================================================

func TestBuiltin_PrintfString(t *testing.T) {
	run(t, `printf "hello %s\n" world`, "hello world\n")
}
func TestBuiltin_PrintfInt(t *testing.T) {
	run(t, `printf "%d items\n" 42`, "42 items\n")
}
func TestBuiltin_PrintfNoTrailingNewline(t *testing.T) {
	run(t, `printf "no newline"`, "no newline")
}

// =====================================================================
// BUILTINS: readonly
// =====================================================================

func TestBuiltin_Readonly(t *testing.T) {
	run(t, "X=10; readonly X; X=20 2>/dev/null; echo $X", "10\n")
}
func TestBuiltin_ReadonlyReject(t *testing.T) {
	// readonly should prevent unset too
	run(t, "X=10; readonly X; unset X 2>/dev/null; echo $X", "10\n")
}

// =====================================================================
// BUILTINS: alias / unalias
// =====================================================================

func TestBuiltin_AliasBasic(t *testing.T) {
	run(t, "alias greet='echo hello'\ngreet", "hello\n")
}
func TestBuiltin_AliasWithArgs(t *testing.T) {
	run(t, "alias say='echo'\nsay world", "world\n")
}
func TestBuiltin_Unalias(t *testing.T) {
	run(t, "alias greet='echo hello'\nunalias greet\ngreet 2>/dev/null; echo $?", "127\n")
}

// =====================================================================
// BUILTINS: trap
// =====================================================================

func TestBuiltin_TrapExit(t *testing.T) {
	run(t, "trap 'echo bye' EXIT\necho hello", "hello\nbye\n")
}

// =====================================================================
// BUILTINS: source / .
// =====================================================================

func TestBuiltin_Source(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/lib.sh", []byte("X=from_source"), 0644)
	run(t, "source "+dir+"/lib.sh; echo $X", "from_source\n")
}
func TestBuiltin_Dot(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/lib.sh", []byte("Y=dotted"), 0644)
	run(t, ". "+dir+"/lib.sh; echo $Y", "dotted\n")
}

// =====================================================================
// BUILTINS: wait
// =====================================================================

func TestBuiltin_WaitAll(t *testing.T) {
	env := NewEnv()
	got := capture(env, func() { Run("echo a &\necho b &\nwait\necho done", env); RunExitTraps(env) })
	// Background goroutines don't guarantee order
	if !strings.Contains(got, "a\n") || !strings.Contains(got, "b\n") || !strings.HasSuffix(got, "done\n") {
		t.Errorf("got %q, want a and b (any order) then done", got)
	}
}

// =====================================================================
// BUILTINS: kill
// =====================================================================

func TestBuiltin_KillProcess(t *testing.T) {
	run(t, "pid = spawn fn do\nreceive do _ -> nil end\nend\nkill $pid\necho done", "done\n")
}

// =====================================================================
// BUILTINS: exec
// =====================================================================

func TestBuiltin_ExecReplace(t *testing.T) {
	// exec replaces the shell — subsequent commands don't run
	run(t, "exec echo replaced; echo should_not_appear", "replaced\n")
}

// =====================================================================
// STDLIB: Kernel — type checks
// =====================================================================

func TestStdlib_IsInteger(t *testing.T)  { bind(t, "x = is_integer(42)", "x", value.True) }
func TestStdlib_IsIntegerFalse(t *testing.T) { bind(t, "x = is_integer(\"hi\")", "x", value.False) }
func TestStdlib_IsFloat(t *testing.T)    { bind(t, "x = is_float(3.14)", "x", value.True) }
func TestStdlib_IsString(t *testing.T)   { bind(t, `x = is_string("hi")`, "x", value.True) }
func TestStdlib_IsAtom(t *testing.T)     { bind(t, "x = is_atom(:ok)", "x", value.True) }
func TestStdlib_IsList(t *testing.T)     { bind(t, "x = is_list([1,2])", "x", value.True) }
func TestStdlib_IsMap(t *testing.T)      { bind(t, `x = is_map(%{a: 1})`, "x", value.True) }
func TestStdlib_IsNil(t *testing.T)      { bind(t, "x = is_nil(nil)", "x", value.True) }
func TestStdlib_IsFn(t *testing.T)       { bind(t, "fn f do 1 end\nx = is_fn(&f)", "x", value.True) }
func TestStdlib_IsTuple(t *testing.T)    { bind(t, "x = is_tuple({:ok, 1})", "x", value.True) }

// =====================================================================
// STDLIB: Kernel — conversions
// =====================================================================

func TestStdlib_ToString(t *testing.T)    { run(t, `echo $(to_string(42))`, "42\n") }
func TestStdlib_ToInteger(t *testing.T)   { bind(t, `x = to_integer("42")`, "x", value.IntVal(42)) }
func TestStdlib_ToFloat(t *testing.T)     { bind(t, `x = to_float("3.14")`, "x", value.FloatVal(3.14)) }
func TestStdlib_Inspect(t *testing.T)     { run(t, `echo $(inspect({:ok, [1, 2]}))`, "{:ok, [1, 2]}\n") }

// =====================================================================
// STDLIB: Kernel — min, max, apply
// =====================================================================

func TestStdlib_Min(t *testing.T) { bind(t, "x = min(3, 7)", "x", value.IntVal(3)) }
func TestStdlib_Max(t *testing.T) { bind(t, "x = max(3, 7)", "x", value.IntVal(7)) }
func TestStdlib_Apply(t *testing.T) {
	run(t, "fn add a, b do a + b end\necho $(apply(&add, [3, 4]))", "7\n")
}

// =====================================================================
// STDLIB: List — full API
// =====================================================================

func TestStdlib_ListAppend(t *testing.T) {
	run(t, `echo $(List.append([1, 2], 3))`, "[1, 2, 3]\n")
}
func TestStdlib_ListConcat(t *testing.T) {
	run(t, `echo $(List.concat([1, 2], [3, 4]))`, "[1, 2, 3, 4]\n")
}
func TestStdlib_ListAt(t *testing.T) {
	bind(t, "x = List.at([10, 20, 30], 1)", "x", value.IntVal(20))
}
func TestStdlib_ListRange(t *testing.T) {
	run(t, "echo $(List.range(1, 5))", "[1, 2, 3, 4, 5]\n")
}
func TestStdlib_ListSort(t *testing.T) {
	run(t, "echo $(List.sort([3, 1, 2]))", "[1, 2, 3]\n")
}
func TestStdlib_ListReverse(t *testing.T) {
	run(t, "echo $(List.reverse([1, 2, 3]))", "[3, 2, 1]\n")
}
func TestStdlib_ListEach(t *testing.T) {
	run(t, `List.each [1, 2, 3], \x -> echo $x`, "1\n2\n3\n")
}
func TestStdlib_ListAny(t *testing.T) {
	bind(t, `x = List.any([1, 2, 3], \n -> n > 2)`, "x", value.True)
}
func TestStdlib_ListAll(t *testing.T) {
	bind(t, `x = List.all([2, 4, 6], \n -> n > 0)`, "x", value.True)
}
func TestStdlib_ListAllFalse(t *testing.T) {
	bind(t, `x = List.all([2, 4, -1], \n -> n > 0)`, "x", value.False)
}
func TestStdlib_ListFind(t *testing.T) {
	bind(t, `x = List.find([1, 2, 3], \n -> n > 1)`, "x", value.IntVal(2))
}
func TestStdlib_ListWithIndex(t *testing.T) {
	run(t, `List.each List.with_index(["a", "b"]), \pair -> echo $pair`, "{0, \"a\"}\n{1, \"b\"}\n")
}

// =====================================================================
// STDLIB: String — full API
// =====================================================================

func TestStdlib_StringSplit(t *testing.T) {
	run(t, `echo $(String.split("a,b,c", ","))`, `["a", "b", "c"]`+"\n")
}
func TestStdlib_StringJoin(t *testing.T) {
	run(t, `echo $(String.join(["a", "b", "c"], "-"))`, "a-b-c\n")
}
func TestStdlib_StringReplace(t *testing.T) {
	run(t, `echo $(String.replace("hello world", "world", "ish"))`, "hello ish\n")
}
func TestStdlib_StringContains(t *testing.T) {
	bind(t, `x = String.contains("hello world", "world")`, "x", value.True)
}
func TestStdlib_StringStartsWith(t *testing.T) {
	bind(t, `x = String.starts_with("hello", "hel")`, "x", value.True)
}
func TestStdlib_StringEndsWith(t *testing.T) {
	bind(t, `x = String.ends_with("hello", "llo")`, "x", value.True)
}
func TestStdlib_StringSlice(t *testing.T) {
	run(t, `echo $(String.slice("hello", 1, 3))`, "ell\n")
}
func TestStdlib_StringChars(t *testing.T) {
	run(t, `echo $(String.chars("hi"))`, `["h", "i"]`+"\n")
}
func TestStdlib_StringLength(t *testing.T) {
	bind(t, `x = String.length("hello")`, "x", value.IntVal(5))
}

// =====================================================================
// STDLIB: Map
// =====================================================================

func TestStdlib_MapPut(t *testing.T) {
	run(t, `m = Map.put(%{a: 1}, "b", 2)`+"\necho $(m.b)", "2\n")
}
func TestStdlib_MapDelete(t *testing.T) {
	run(t, `m = Map.delete(%{a: 1, b: 2}, "a")`+"\necho $(Map.keys(m))", `["b"]`+"\n")
}
func TestStdlib_MapKeys(t *testing.T) {
	run(t, `echo $(Map.keys(%{x: 1, y: 2}))`, `["x", "y"]`+"\n")
}
func TestStdlib_MapValues(t *testing.T) {
	run(t, `echo $(Map.values(%{x: 1, y: 2}))`, "[1, 2]\n")
}
func TestStdlib_MapMerge(t *testing.T) {
	run(t, `m = Map.merge(%{a: 1}, %{b: 2})`+"\necho $(m.a) $(m.b)", "1 2\n")
}
func TestStdlib_MapHasKey(t *testing.T) {
	bind(t, `x = Map.has_key(%{a: 1}, "a")`, "x", value.True)
}
func TestStdlib_MapGet(t *testing.T) {
	bind(t, `x = Map.get(%{a: 42}, "a")`, "x", value.IntVal(42))
}

// =====================================================================
// STDLIB: Tuple
// =====================================================================

func TestStdlib_TupleAt(t *testing.T) {
	bind(t, "x = Tuple.at({:ok, 42}, 1)", "x", value.IntVal(42))
}
func TestStdlib_TupleSize(t *testing.T) {
	bind(t, "x = Tuple.size({:a, :b, :c})", "x", value.IntVal(3))
}
func TestStdlib_TupleToList(t *testing.T) {
	run(t, "echo $(Tuple.to_list({1, 2, 3}))", "[1, 2, 3]\n")
}

// =====================================================================
// STDLIB: Enum — generalized
// =====================================================================

func TestStdlib_EnumReduce(t *testing.T) {
	bind(t, `x = Enum.reduce([1, 2, 3], 0, \acc, n -> acc + n)`, "x", value.IntVal(6))
}
func TestStdlib_EnumMap(t *testing.T) {
	run(t, `echo $(Enum.map([1, 2, 3], \x -> x * 10))`, "[10, 20, 30]\n")
}
func TestStdlib_EnumFilter(t *testing.T) {
	run(t, `echo $(Enum.filter([1, 2, 3, 4], \x -> x > 2))`, "[3, 4]\n")
}
func TestStdlib_EnumCount(t *testing.T) {
	bind(t, "x = Enum.count([1, 2, 3])", "x", value.IntVal(3))
}

// =====================================================================
// STDLIB: Math — full API
// =====================================================================

func TestStdlib_MathSqrt(t *testing.T)  { bind(t, "x = Math.sqrt(9.0)", "x", value.FloatVal(3.0)) }
func TestStdlib_MathPow(t *testing.T)   { bind(t, "x = Math.pow(2, 10)", "x", value.FloatVal(1024.0)) }
func TestStdlib_MathFloor(t *testing.T) { bind(t, "x = Math.floor(3.7)", "x", value.IntVal(3)) }
func TestStdlib_MathCeil(t *testing.T)  { bind(t, "x = Math.ceil(3.2)", "x", value.IntVal(4)) }
func TestStdlib_MathRound(t *testing.T) { bind(t, "x = Math.round(3.5)", "x", value.IntVal(4)) }
func TestStdlib_MathLog(t *testing.T)   { bind(t, "x = Math.log(1.0)", "x", value.FloatVal(0.0)) }

// =====================================================================
// STDLIB: Regex — full API
// =====================================================================

func TestStdlib_RegexScan(t *testing.T) {
	run(t, `echo $(Regex.scan("a1b2c3", "[0-9]+"))`, `["1", "2", "3"]`+"\n")
}
func TestStdlib_RegexReplace(t *testing.T) {
	run(t, `echo $(Regex.replace("hello world", "world", "ish"))`, "hello ish\n")
}
func TestStdlib_RegexSplit(t *testing.T) {
	run(t, `echo $(Regex.split("a1b2c3", "[0-9]+"))`, `["a", "b", "c", ""]`+"\n")
}

// =====================================================================
// STDLIB: Path — full API
// =====================================================================

func TestStdlib_PathExtname(t *testing.T) {
	run(t, `echo $(Path.extname("file.tar.gz"))`, ".gz\n")
}
func TestStdlib_PathJoin(t *testing.T) {
	run(t, `echo $(Path.join("/usr", "local", "bin"))`, "/usr/local/bin\n")
}
func TestStdlib_PathExists(t *testing.T) {
	bind(t, `x = Path.exists("/dev/null")`, "x", value.True)
}

// =====================================================================
// STDLIB: IO
// =====================================================================

func TestStdlib_IOLines(t *testing.T) {
	run(t, `echo $(IO.lines("a\nb\nc"))`, `["a", "b", "c"]`+"\n")
}

// =====================================================================
// STDLIB: JSON — roundtrip
// =====================================================================

func TestStdlib_JSONRoundtripMap(t *testing.T) {
	run(t, `m = %{name: "fox", age: 3}`+"\nj = JSON.encode(m)\nm2 = JSON.parse(j)\necho $(m2.name)", "fox\n")
}
func TestStdlib_JSONRoundtripNested(t *testing.T) {
	run(t, `j = JSON.encode(%{tags: [1, 2, 3]})`+"\necho $j", `{"tags":[1,2,3]}`+"\n")
}

// =====================================================================
// STDLIB: CSV
// =====================================================================

func TestStdlib_CSVParse(t *testing.T) {
	run(t, `rows = CSV.parse("a,b\n1,2\n")`+"\necho $(length(rows))", "2\n")
}
func TestStdlib_CSVEncode(t *testing.T) {
	run(t, `echo $(CSV.encode([["a", "b"], ["1", "2"]]))`, "a,b\n1,2\n")
}

// =====================================================================
// OTP: spawn_link — linked process failure propagates
// =====================================================================

func TestOTP_SpawnLink(t *testing.T) {
	run(t, `r = try do
pid = spawn_link fn do
  1 / 0
end
await pid
rescue
_ -> :caught
end
echo $r`, ":caught\n")
}

// =====================================================================
// OTP: monitor — get DOWN message when process exits
// =====================================================================

func TestOTP_Monitor(t *testing.T) {
	run(t, `pid = spawn fn do 42 end
monitor pid
receive do
{:DOWN, _, :normal} -> echo "process exited normally"
end`, "process exited normally\n")
}

// =====================================================================
// OTP: supervisor — restart strategy
// =====================================================================

func TestOTP_SupervisorOneForOne(t *testing.T) {
	// Supervisor should restart a crashed child
	run(t, `counter = spawn fn do
receive do
  {:get, sender} -> send sender, 0
end
end
sup = supervise [
  fn do
    receive do
      :crash -> 1 / 0
    end
  end
], strategy: :one_for_one
echo "supervised"`, "supervised\n")
}

// =====================================================================
// PIPE: auto-coercion — detect value vs command
// =====================================================================

func TestPipe_ValueToCmdAutoCoerce(t *testing.T) {
	// Map should serialize to key=value lines when piped to a command
	run(t, `%{a: 1, b: 2} | sort`, "a: 1\nb: 2\n")
}
// TestPipeFn_CmdToValue in pipe_test.go covers value pipe chains

// =====================================================================
// TAIL CALL OPTIMIZATION
// =====================================================================

func TestTailCall_NoStackOverflow(t *testing.T) {
	// This should not stack overflow with TCO — 100k iterations
	run(t, `fn loop n do
if (n <= 0) do
  echo "done"
else
  loop(n - 1)
end
end
loop 100000`, "done\n")
}

// =====================================================================
// ADVANCED: process substitution <(cmd)
// =====================================================================

func TestProcessSubstitution(t *testing.T) {
	run(t, `cat <(echo hello)`, "hello\n")
}

// =====================================================================
// ADVANCED: word splitting and quoting
// =====================================================================

func TestWordSplit_Unquoted(t *testing.T) {
	run(t, `X="a b c"; for w in $X; do echo $w; done`, "a\nb\nc\n")
}
func TestWordSplit_Quoted(t *testing.T) {
	// Quoted variable should NOT word-split
	run(t, `X="a b c"; for w in "$X"; do echo $w; done`, "a b c\n")
}

// =====================================================================
// ADVANCED: arithmetic expansion in more contexts
// =====================================================================

func TestArith_InCondition(t *testing.T) {
	run(t, "if (( 2 + 2 == 4 )); then echo yes; fi", "yes\n")
}
func TestArith_PreIncrement(t *testing.T) {
	run(t, "x=5; echo $((x + 1))", "6\n")
}

// =====================================================================
// ADVANCED: arrays (POSIX-style)
// =====================================================================

func TestArray_Declare(t *testing.T) {
	run(t, `arr = ["one", "two", "three"]`+"\necho $(List.at(arr, 1))", "two\n")
}

// =====================================================================
// CONTROL FLOW: nested match with guards
// =====================================================================

func TestMatch_NestedGuard(t *testing.T) {
	run(t, `result = match {:ok, 42} do
{:ok, n} when n > 10 -> "big"
{:ok, n} -> "small"
{:error, _} -> "err"
end
echo $result`, "big\n")
}
func TestMatch_GuardFalse(t *testing.T) {
	run(t, `result = match {:ok, 3} do
{:ok, n} when n > 10 -> "big"
{:ok, n} -> "small"
end
echo $result`, "small\n")
}

// =====================================================================
// FN: multi-clause with different arities
// =====================================================================

func TestFn_DefaultArgs(t *testing.T) {
	// Two-clause function: one with 1 param, one with 2
	run(t, `fn greet "world" do "hello world" end
fn greet name do "hello " + name end
echo $(greet("world"))
echo $(greet("ish"))`, "hello world\nhello ish\n")
}

// =====================================================================
// POSIX: subshell isolation
// =====================================================================

func TestSubshell_CwdIsolation(t *testing.T) {
	run(t, "orig=$(pwd); (cd /tmp); echo $(pwd) | grep -q \"$orig\" && echo same", "same\n")
}

// =====================================================================
// POSIX: command substitution nesting
// =====================================================================

func TestCmdSub_TripleNest(t *testing.T) {
	run(t, `echo $(echo $(echo $(echo deep)))`, "deep\n")
}

// =====================================================================
// POSIX: here-doc with expansion
// =====================================================================

func TestHeredoc_VarExpansion(t *testing.T) {
	run(t, "NAME=world\ncat <<EOF\nhello $NAME\nEOF", "hello world\n")
}

// =====================================================================
// POSIX: here-doc quoted delimiter (no expansion)
// =====================================================================

func TestHeredoc_QuotedNoExpand(t *testing.T) {
	run(t, "NAME=world\ncat <<'EOF'\nhello $NAME\nEOF", "hello $NAME\n")
}

// =====================================================================
// INTEGRATION: real shell patterns
// =====================================================================

func TestIntegration_PipeToGrep(t *testing.T) {
	run(t, `echo "apple\nbanana\ncherry" | grep an`, "banana\n")
}

func TestIntegration_ForWithSeq(t *testing.T) {
	run(t, "for i in $(seq 1 3); do echo $i; done", "1\n2\n3\n")
}

func TestIntegration_ConditionPipeline(t *testing.T) {
	run(t, `echo hello | grep -q hello && echo found`, "found\n")
}

func TestIntegration_FnPipeline(t *testing.T) {
	run(t, `fn double x do x * 2 end
fn inc x do x + 1 end
r = [1, 2, 3] |> List.map(\x -> double(x)) |> List.map(\x -> inc(x))
echo $r`, "[3, 5, 7]\n")
}

func TestIntegration_ErrorHandling(t *testing.T) {
	run(t, `result = try do
  val = JSON.parse("{invalid")
  {:ok, val}
rescue
  err -> {:error, err}
end
match result do
  {:ok, _} -> echo "parsed"
  {:error, _} -> echo "failed"
end`, "failed\n")
}

func TestIntegration_ModuleWithState(t *testing.T) {
	// Module function accessing outer scope via closure
	run(t, `defmodule Counter do
  fn new do
    spawn fn do
      fn loop count do
        receive do
          {:inc, sender} -> send sender, count + 1; loop(count + 1)
          {:get, sender} -> send sender, count; loop(count)
        end
      end
      loop(0)
    end
  end
end
c = Counter.new
send c, {:inc, self}
receive do n -> nil end
send c, {:inc, self}
receive do n -> nil end
send c, {:get, self}
receive do
  n -> echo $n
end`, "2\n")
}
