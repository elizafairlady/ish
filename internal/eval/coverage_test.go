package eval

import (
	"os"
	"testing"

	"ish/internal/value"
)

// =====================================================================
// Parameter expansion: pattern operations
// =====================================================================

func TestParamExpand_RemoveSuffixShort(t *testing.T) {
	run(t, `F="file.tar.gz"; echo ${F%.gz}`, "file.tar\n")
}

func TestParamExpand_RemoveSuffixLong(t *testing.T) {
	run(t, `F="file.tar.gz"; echo ${F%%.*}`, "file\n")
}

func TestParamExpand_RemovePrefixShort(t *testing.T) {
	run(t, `P="/usr/local/bin"; echo ${P#*/}`, "usr/local/bin\n")
}

func TestParamExpand_RemovePrefixLong(t *testing.T) {
	run(t, `P="/usr/local/bin"; echo ${P##*/}`, "bin\n")
}

func TestParamExpand_ReplaceFirst(t *testing.T) {
	run(t, `S="hello world hello"; echo ${S/hello/goodbye}`, "goodbye world hello\n")
}

func TestParamExpand_ReplaceAll(t *testing.T) {
	run(t, `S="hello world hello"; echo ${S//hello/goodbye}`, "goodbye world goodbye\n")
}

// =====================================================================
// Arithmetic expansion
// =====================================================================

func TestArithExpand_Basic(t *testing.T) {
	run(t, `echo $((2 + 3))`, "5\n")
}

func TestArithExpand_Variables(t *testing.T) {
	run(t, `x=10; echo $((x * 2))`, "20\n")
}

func TestArithExpand_Nested(t *testing.T) {
	run(t, `echo $(( (3 + 4) * 2 ))`, "14\n")
}

func TestArithExpand_Modulo(t *testing.T) {
	run(t, `echo $((17 % 5))`, "2\n")
}

// =====================================================================
// Missing builtins
// =====================================================================

func TestBuiltin_Colon(t *testing.T) {
	// : is a noop that returns success
	run(t, `: && echo yes`, "yes\n")
}

func TestBuiltin_Printf(t *testing.T) {
	run(t, `printf "%s is %d\n" "age" 30`, "age is 30\n")
}

func TestBuiltin_PrintfNoNewline(t *testing.T) {
	run(t, `printf "%s" hello; echo "!"`, "hello!\n")
}

func TestBuiltin_Kill(t *testing.T) {
	run(t, `sleep 100 &; kill $!; wait; echo done`, "done\n")
}

// =====================================================================
// Control flow gaps
// =====================================================================

func TestUntilLoop(t *testing.T) {
	run(t, `x=0; until test $x = 3; do x=$((x + 1)); done; echo $x`, "3\n")
}

func TestBreakInFor(t *testing.T) {
	run(t, `for i in 1 2 3 4 5; do
  if test $i = 3; then break; fi
  echo $i
done`, "1\n2\n")
}

func TestContinueInFor(t *testing.T) {
	run(t, `for i in 1 2 3 4 5; do
  if test $i = 3; then continue; fi
  echo $i
done`, "1\n2\n4\n5\n")
}

func TestBreakInWhile(t *testing.T) {
	run(t, `x=0; while true; do x=$((x + 1)); if test $x = 3; then break; fi; done; echo $x`, "3\n")
}

// =====================================================================
// Heredoc
// =====================================================================

func TestHeredoc_Basic(t *testing.T) {
	run(t, "cat <<EOF\nhello world\nEOF", "hello world\n")
}

func TestHeredoc_VarExpand(t *testing.T) {
	run(t, "X=world; cat <<EOF\nhello $X\nEOF", "hello world\n")
}

func TestHerestring(t *testing.T) {
	run(t, `cat <<< "hello here"`, "hello here")
}

// =====================================================================
// Tilde expansion
// =====================================================================

func TestTildeExpansion(t *testing.T) {
	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("HOME not set")
	}
	env := NewEnv()
	Run(`x = ~`, env)
	v, ok := env.Get("x")
	if !ok || v.ToStr() != home {
		t.Errorf("~ should expand to %s, got %s", home, v.ToStr())
	}
}

// =====================================================================
// Glob expansion in commands
// =====================================================================

func TestGlobExpansion(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/a.txt", []byte("a\n"), 0644)
	os.WriteFile(dir+"/b.txt", []byte("b\n"), 0644)
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	os.Chdir(dir)
	run(t, `echo *.txt`, "a.txt b.txt\n")
}

// =====================================================================
// Nested command substitution
// =====================================================================

func TestCmdSub_Nested(t *testing.T) {
	run(t, `echo $(echo $(echo deep))`, "deep\n")
}

func TestCmdSub_InArg(t *testing.T) {
	run(t, `X=$(echo hello); echo $X`, "hello\n")
}

// =====================================================================
// Special variables
// =====================================================================

func TestSpecialVar_QuestionMark(t *testing.T) {
	run(t, `true; echo $?`, "0\n")
	run(t, `false; echo $?`, "1\n")
}

func TestSpecialVar_DollarBang(t *testing.T) {
	// $! is the PID of the last background job
	env := NewEnv()
	Run("sleep 0 &", env)
	v, ok := env.Get("!")
	if !ok {
		t.Skip("$! not set")
	}
	if v.Kind != value.VInt && v.Kind != value.VString {
		t.Errorf("$! should be a number, got %s", v.Inspect())
	}
}

func TestSpecialVar_DollarDollar(t *testing.T) {
	// $$ is the shell's PID
	env := NewEnv()
	Run("x=$$", env)
	v, ok := env.Get("x")
	if !ok || v.ToStr() == "" || v.ToStr() == "0" {
		t.Errorf("$$ should be shell PID, got %s", v)
	}
}

func TestSpecialVar_DollarAt(t *testing.T) {
	run(t, `set -- a b c; for x in "$@"; do echo $x; done`, "a\nb\nc\n")
}

// =====================================================================
// String interpolation edge cases
// =====================================================================

func TestInterpStr_EscapeNewline(t *testing.T) {
	run(t, `echo "a\nb"`, "a\nb\n")
}

func TestInterpStr_EscapeTab(t *testing.T) {
	run(t, `echo "a\tb"`, "a\tb\n")
}

func TestInterpStr_DollarInDoubleQuote(t *testing.T) {
	run(t, `X=hello; echo "$X world"`, "hello world\n")
}

// =====================================================================
// Pipe edge cases
// =====================================================================

func TestPipe_MultiStage(t *testing.T) {
	run(t, `echo "c\na\nb" | sort | head -1`, "a\n")
}

func TestPipeFn_Identity(t *testing.T) {
	bind(t, `x = 42 |> \n -> n`, "x", value.IntVal(42))
}

func TestPipeFn_ModuleChain(t *testing.T) {
	bind(t, `x = "hello" |> String.upcase |> String.length`, "x", value.IntVal(5))
}

// =====================================================================
// Match expression
// =====================================================================

func TestMatch_WithGuard(t *testing.T) {
	run(t, `x = match 5 do
  n when n > 10 -> :big
  n when n > 3 -> :medium
  _ -> :small
end
echo $x`, ":medium\n")
}

// =====================================================================
// Module features
// =====================================================================

func TestModule_Reopen(t *testing.T) {
	run(t, `defmodule M do
  fn a do 1 end
end
defmodule M do
  fn b do 2 end
end
echo $(M.a) $(M.b)`, "1 2\n")
}

func TestModule_ZeroArityCall(t *testing.T) {
	bind(t, `defmodule M do
  fn value do 42 end
end
x = M.value`, "x", value.IntVal(42))
}

func TestModule_UseImport(t *testing.T) {
	run(t, `defmodule M do
  fn greet name do "hi " + name end
end
use M
echo $(greet("world"))`, "hi world\n")
}

// =====================================================================
// OTP completeness
// =====================================================================

func TestOTP_SelfOutsideProcess(t *testing.T) {
	// self should work at top level (main process)
	env := NewEnv()
	Run("x = self", env)
	v, ok := env.Get("x")
	if !ok || v.Kind != value.VPid {
		t.Errorf("self should return PID, got %s", v.Inspect())
	}
}

func TestOTP_SendReceiveAtTopLevel(t *testing.T) {
	run(t, `send self, :hello
receive do
  :hello -> echo "got it"
end`, "got it\n")
}

func TestOTP_AwaitValue(t *testing.T) {
	bind(t, `pid = spawn fn do 42 end
x = await pid`, "x", value.IntVal(42))
}

// =====================================================================
// Error handling
// =====================================================================

func TestTry_CatchDivisionByZero(t *testing.T) {
	run(t, `r = try do
  1 / 0
rescue
  _ -> :caught
end
echo $r`, ":caught\n")
}

func TestTry_NoError(t *testing.T) {
	bind(t, `x = try do 42 rescue _ -> :err end`, "x", value.IntVal(42))
}

// =====================================================================
// Closures
// =====================================================================

func TestClosure_CapturesScope(t *testing.T) {
	bind(t, `x = 10
f = \n -> n + x
r = f(5)`, "r", value.IntVal(15))
}

func TestClosure_Adder(t *testing.T) {
	// Closure captures outer variable
	bind(t, `fn make_adder base do
  \x -> base + x
end
add5 = make_adder(5)
r = add5(3)`, "r", value.IntVal(8))
}

// =====================================================================
// Tail call optimization
// =====================================================================

func TestTCO_DeepRecursion(t *testing.T) {
	bind(t, `fn loop n do
  if n == 0 do :done else loop(n - 1) end
end
x = loop(100000)`, "x", value.AtomVal("done"))
}

func TestTCO_MutualRecursion(t *testing.T) {
	bind(t, `fn even n do
  if n == 0 do true else odd(n - 1) end
end
fn odd n do
  if n == 0 do false else even(n - 1) end
end
x = even(1000)`, "x", value.True)
}
