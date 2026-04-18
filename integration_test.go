package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureOutput runs a function and captures what is written to the env's stdout.
// It sets env.stdout to a pipe writer, runs fn, then reads the output.
func captureOutput(env *Env, fn func()) string {
	r, w, _ := os.Pipe()
	env.stdout = w
	fn()
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()
	return buf.String()
}

func testEnv() *Env {
	env := TopEnv()
	env.cmdSub = func(cmd string, e *Env) (string, error) {
		val, err := evalCmdSub(cmd, e)
		if err != nil {
			return "", err
		}
		return val.ToStr(), nil
	}
	return env
}

func TestIntegration(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		// =====================================================================
		// POSIX basics
		// =====================================================================
		{
			name:   "echo simple",
			script: `echo hello`,
			want:   "hello\n",
		},
		{
			name:   "echo multiple words",
			script: `echo hello world`,
			want:   "hello world\n",
		},
		{
			name:   "echo -n no newline",
			script: `echo -n hello`,
			want:   "hello",
		},
		{
			name:   "POSIX var assignment and expansion",
			script: "FOO=bar\necho $FOO",
			want:   "bar\n",
		},
		{
			name:   "double-quoted string expansion",
			script: "NAME=world\necho \"hello $NAME\"",
			want:   "hello world\n",
		},
		{
			name:   "single-quoted string literal",
			script: "echo 'hello world'",
			want:   "hello world\n",
		},
		{
			name:   "exit status true",
			script: "true\necho $?",
			want:   "0\n",
		},
		{
			name:   "exit status false",
			script: "false\necho $?",
			want:   "1\n",
		},
		{
			name:   "echo empty string",
			script: `echo`,
			want:   "\n",
		},

		// =====================================================================
		// Functional pipe operator (|>) tested here; Unix pipes (|) write
		// directly to os.Stdout via evalPipe, so they are not captured by
		// the test harness. The |> operator is tested in a dedicated
		// section below.
		// =====================================================================

		// =====================================================================
		// Control flow: if/then/fi
		// =====================================================================
		{
			name:   "if true then fi",
			script: "if true; then\necho yes\nfi",
			want:   "yes\n",
		},
		{
			name:   "if false then fi",
			script: "if false; then\necho yes\nfi",
			want:   "",
		},
		{
			name:   "if else fi",
			script: "if false; then\necho yes\nelse\necho no\nfi",
			want:   "no\n",
		},

		// =====================================================================
		// Control flow: for/do/done
		// =====================================================================
		{
			name:   "for loop",
			script: "for i in a b c; do\necho $i\ndone",
			want:   "a\nb\nc\n",
		},
		{
			name:   "for loop with break",
			script: "for i in a b c d; do\nif [ $i = c ]; then\nbreak\nfi\necho $i\ndone",
			want:   "a\nb\n",
		},
		{
			name:   "for loop with continue",
			script: "for i in a b c d; do\nif [ $i = b ]; then\ncontinue\nfi\necho $i\ndone",
			want:   "a\nc\nd\n",
		},

		// =====================================================================
		// Control flow: while/do/done
		// =====================================================================
		{
			name: "while loop counting",
			script: "n = 3\nwhile [ $n -gt 0 ]; do\necho $n\nn = (n - 1)\ndone",
			want:   "3\n2\n1\n",
		},

		// =====================================================================
		// Control flow: case/esac
		// =====================================================================
		{
			name: "case matching first pattern",
			script: `X=hello
case $X in
hello)
echo matched
;;
*)
echo default
;;
esac`,
			want: "matched\n",
		},
		{
			name: "case matching wildcard",
			script: `X=other
case $X in
hello)
echo matched
;;
*)
echo default
;;
esac`,
			want: "default\n",
		},

		// =====================================================================
		// Test builtin [ ]
		// =====================================================================
		{
			name:   "test string equality true",
			script: "[ foo = foo ]\necho $?",
			want:   "0\n",
		},
		{
			name:   "test string equality false",
			script: "[ foo = bar ]\necho $?",
			want:   "1\n",
		},
		{
			name:   "test numeric equality",
			script: "[ 1 -eq 1 ]\necho $?",
			want:   "0\n",
		},
		{
			name:   "test numeric inequality",
			script: "[ 1 -eq 2 ]\necho $?",
			want:   "1\n",
		},
		{
			name:   "test -f on existing file",
			script: "[ -f integration_test.go ]\necho $?",
			want:   "0\n",
		},
		{
			name:   "test -f on nonexistent file",
			script: "[ -f noexist_xyz123 ]\necho $?",
			want:   "1\n",
		},

		// =====================================================================
		// Ish bindings: x = expr
		// =====================================================================
		{
			name:   "ish binding integer",
			script: "x = 42\necho $x",
			want:   "42\n",
		},
		{
			name:   "ish binding with arithmetic",
			script: "x = 10 + 5\necho $x",
			want:   "15\n",
		},
		{
			name: "tuple destructuring via match",
			script: `t = {1, 2}
match t do
{a, b} -> echo $a $b
end`,
			want: "1 2\n",
		},
		{
			name: "list destructuring via match",
			script: `l = [10, 20, 30]
match l do
[x, y, z] -> echo $x $y $z
end`,
			want: "10 20 30\n",
		},

		// =====================================================================
		// Data types: atoms, tuples, lists, maps
		// =====================================================================
		{
			name:   "atom literal",
			script: `:hello`,
			want:   "",
		},
		{
			name:   "tuple literal value",
			script: "t = {1, 2, 3}\necho $t",
			want:   "{1, 2, 3}\n",
		},
		{
			name:   "list literal value",
			script: "l = [10, 20, 30]\necho $l",
			want:   "[10, 20, 30]\n",
		},
		{
			name:   "map value",
			script: "m = %{name : \"alice\", age : 30}\necho $m",
			want:   "%{name: \"alice\", age: 30}\n",
		},

		// =====================================================================
		// Functions: fn/do/end
		// =====================================================================
		{
			name:   "simple fn definition and call",
			script: "fn greet name do\necho hello\nend\ngreet world",
			want:   "hello\n",
		},
		{
			name: "multi-clause fib",
			script: `fn fib 0 do
0
end
fn fib 1 do
1
end
fn fib n when n > 1 do
a = fib (n - 1)
b = fib (n - 2)
a + b
end
r = fib 10
echo $r`,
			want: "55\n",
		},
		{
			name: "fn with guard",
			script: `fn abs n when n < 0 do
0 - n
end
fn abs n do
n
end
r = abs (-5)
echo $r`,
			want: "5\n",
		},

		// =====================================================================
		// Match expression: match/do/end
		// =====================================================================
		{
			name: "match on integer",
			script: `x = 2
r = match x do
1 -> :one
2 -> :two
_ -> :other
end
echo $r`,
			want: ":two\n",
		},
		{
			name: "match on atom",
			script: `x = :ok
r = match x do
:ok -> echo success
:err -> echo failure
end`,
			want: "success\n",
		},

		// =====================================================================
		// Pipe operator |>
		// =====================================================================
		{
			name: "pipe operator with user function",
			script: `fn double x do
x * 2
end
fn inc x do
x + 1
end
r = 5 |> double |> inc
echo $r`,
			want: "11\n",
		},

		// =====================================================================
		// Arithmetic: operators and precedence
		// =====================================================================
		{
			name:   "addition",
			script: "r = 3 + 4\necho $r",
			want:   "7\n",
		},
		{
			name:   "subtraction",
			script: "r = 10 - 3\necho $r",
			want:   "7\n",
		},
		{
			name:   "multiplication",
			script: "r = 6 * 7\necho $r",
			want:   "42\n",
		},
		{
			name:   "division",
			script: "r = 20 / 4\necho $r",
			want:   "5\n",
		},
		{
			name:   "precedence mul before add",
			script: "r = 2 + 3 * 4\necho $r",
			want:   "14\n",
		},
		{
			name:   "parenthesized expression",
			script: "r = (2 + 3) * 4\necho $r",
			want:   "20\n",
		},
		{
			name:   "equality true",
			script: "r = 5 == 5\necho $r",
			want:   ":true\n",
		},
		{
			name:   "equality false",
			script: "r = 5 == 6\necho $r",
			want:   ":false\n",
		},
		{
			name:   "inequality true",
			script: "r = 5 != 6\necho $r",
			want:   ":true\n",
		},
		{
			name:   "unary negation",
			script: "r = -42\necho $r",
			want:   "-42\n",
		},
		{
			name:   "boolean not",
			script: "r = (!true)\necho $r",
			want:   ":false\n",
		},

		// =====================================================================
		// And/Or lists
		// =====================================================================
		{
			name:   "and list both true",
			script: "true && echo yes",
			want:   "yes\n",
		},
		{
			name:   "and list first false",
			script: "false && echo yes",
			want:   "",
		},
		{
			name:   "or list first false",
			script: "false || echo fallback",
			want:   "fallback\n",
		},
		{
			name:   "or list first true",
			script: "true || echo fallback",
			want:   "",
		},

		// =====================================================================
		// Variable scoping
		// =====================================================================
		{
			name: "POSIX fn updates parent variable",
			script: "x=1\nf() { x=2; }\nf\necho $x",
			want: "2\n",
		},
		{
			name: "local builtin keeps variable in function scope",
			script: "x=1\nf() { local x=2; echo $x; }\nf\necho $x",
			want: "2\n1\n",
		},
		{
			name: "ish match bind is local to scope",
			script: "x = 10\nfn setx do\nx = 20\nend\nsetx\necho $x",
			want: "10\n",
		},
		{
			name: "set -e stops execution on failure",
			script: "set -e\ntrue\necho before\nfalse\necho after",
			want: "before\n",
		},
		{
			name: "set -e does not trigger in if condition",
			script: "set -e\nif false; then\necho no\nfi\necho survived",
			want: "survived\n",
		},
		{
			name: "set -e does not trigger in && chain",
			script: "set -e\nfalse && echo no\necho survived",
			want: "survived\n",
		},

		// =====================================================================
		// Errors
		// =====================================================================
		{
			name:   "parse error on unterminated if",
			script: "if true; then\necho hello",
			want:   "", // parse error goes to stderr; stdout empty
		},
		{
			name:   "false produces no stdout",
			script: "false",
			want:   "", // false sets exit code 1 but produces no output
		},

		// =====================================================================
		// Parser fixes (Phase 4)
		// =====================================================================
		{
			name:   "echo in as argument (not block end)",
			script: "echo in out",
			want:   "in out\n",
		},
		{
			name:   "echo then as argument",
			script: "echo then now",
			want:   "then now\n",
		},
		{
			name:   "echo do as argument",
			script: "echo do re mi",
			want:   "do re mi\n",
		},
		{
			name:   "echo done as argument",
			script: "echo done deal",
			want:   "done deal\n",
		},
		{
			name:   "echo fi as argument",
			script: "echo fi fo fum",
			want:   "fi fo fum\n",
		},
		{
			name:   "slash as path not division",
			script: "echo /tmp",
			want:   "/tmp\n",
		},
		{
			name:   "slash as division in expression",
			script: "r = 10 / 2\necho $r",
			want:   "5\n",
		},

		// =====================================================================
		// For loop with command substitution (3.4)
		// =====================================================================
		{
			name:   "for loop with command substitution",
			script: "for x in $(echo a b c); do\necho $x\ndone",
			want:   "a\nb\nc\n",
		},

		// =====================================================================
		// Echo flag bundle handling (3.7)
		// =====================================================================
		{
			name:   "echo -neE flag bundle",
			script: `echo -neE "hello\nworld"`,
			want:   "hello\\nworld",
		},
		{
			name:   "echo -nEe flag bundle escapes on",
			script: `echo -nEe "hello\nworld"`,
			want:   "hello\nworld",
		},

		// =====================================================================
		// Heredoc expansion (3.3)
		// =====================================================================
		{
			name:   "herestring with variable expansion",
			script: "X=hello\necho $(cat <<<$X)",
			want:   "hello\n",
		},

		// =====================================================================
		// List head|tail destructuring
		// =====================================================================
		{
			name: "list head|tail destructuring",
			script: `[h | t] = [1, 2, 3]
echo $h
echo $t`,
			want: "1\n[2, 3]\n",
		},
		{
			name: "list head|tail multiple heads",
			script: `[a, b | rest] = [1, 2, 3, 4]
echo $a
echo $b
echo $rest`,
			want: "1\n2\n[3, 4]\n",
		},
		{
			name: "list head|tail empty rest",
			script: `[h | t] = [1]
echo $h
echo $t`,
			want: "1\n[]\n",
		},
		{
			name: "list head|tail in match expression",
			script: `l = [10, 20, 30]
match l do
[x | xs] -> echo $x $xs
end`,
			want: "10 [20, 30]\n",
		},
		{
			name: "list head|tail with wildcard rest",
			script: `[x, y | _] = [1, 2, 3, 4, 5]
echo $x
echo $y`,
			want: "1\n2\n",
		},

		// =====================================================================
		// $@ proper word splitting
		// =====================================================================
		{
			name: "dollar-at expands to separate args",
			script: "f() { echo $1; echo $2; echo $3; }\nset -- \"hello world\" foo bar\nf $@",
			want:   "hello world\nfoo\nbar\n",
		},

		// =====================================================================
		// Heredoc backslash-newline continuation
		// =====================================================================
		{
			name: "heredoc backslash-newline continuation",
			script: "cat <<EOF\nhello \\\nworld\nEOF",
			want:   "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			got := captureOutput(env, func() {
				runSource(tt.script, env)
			})
			if got != tt.want {
				t.Errorf("script:\n%s\ngot:  %q\nwant: %q", tt.script, got, tt.want)
			}
		})
	}
}

// TestProcessPingPong tests spawn/send/receive with a timeout to avoid hanging.
func TestProcessPingPong(t *testing.T) {
	env := testEnv()

	script := `pid = spawn fn do
  receive do
    {:ping, sender} -> send sender, :pong
  end
end
send pid, {:ping, self}
receive do
  :pong -> echo "got_pong"
end`

	var got string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		got = captureOutput(env, func() {
			runSource(script, env)
		})
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// completed
	case <-time.After(5 * time.Second):
		t.Fatal("TestProcessPingPong timed out after 5s")
	}

	want := "got_pong\n"
	if got != want {
		t.Errorf("ping-pong got %q, want %q", got, want)
	}
}

// TestSpawnSendReceive tests a simpler spawn/send/receive flow.
func TestSpawnSendReceive(t *testing.T) {
	env := testEnv()

	script := `pid = spawn fn do
  receive do
    {:msg, val, sender} -> send sender, val
  end
end
send pid, {:msg, :hello, self}
receive do
  x -> echo $x
end`

	var got string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		got = captureOutput(env, func() {
			runSource(script, env)
		})
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("TestSpawnSendReceive timed out after 5s")
	}

	want := ":hello\n"
	if got != want {
		t.Errorf("spawn/send/receive got %q, want %q", got, want)
	}
}

// TestSubshellCwdIsolation tests that (cd /tmp) doesn't change parent cwd.
func TestSubshellCwdIsolation(t *testing.T) {
	env := testEnv()
	origCwd, _ := os.Getwd()
	defer os.Chdir(origCwd)

	got := captureOutput(env, func() {
		runSource("(cd /tmp)\npwd", env)
	})

	currentCwd, _ := os.Getwd()
	if currentCwd != origCwd {
		t.Errorf("subshell changed cwd: got %q, want %q", currentCwd, origCwd)
	}
	if strings.TrimSpace(got) != origCwd {
		t.Errorf("pwd after subshell: got %q, want %q", strings.TrimSpace(got), origCwd)
	}
}

// TestSubshellVarIsolation tests that variable changes in subshell don't leak.
func TestSubshellVarIsolation(t *testing.T) {
	env := testEnv()
	got := captureOutput(env, func() {
		runSource("X=original\n(X=changed)\necho $X", env)
	})
	if strings.TrimSpace(got) != "original" {
		t.Errorf("subshell var leak: got %q, want %q", strings.TrimSpace(got), "original")
	}
}

// TestTrapExit tests that trap EXIT fires at shell exit.
func TestTrapExit(t *testing.T) {
	env := testEnv()
	got := captureOutput(env, func() {
		runSource("trap 'echo goodbye' EXIT", env)
		RunExitTraps(env)
	})
	if strings.TrimSpace(got) != "goodbye" {
		t.Errorf("trap EXIT: got %q, want %q", strings.TrimSpace(got), "goodbye")
	}
}

// TestMultiClauseFnBlock tests fn with multiple clauses in a single block.
func TestMultiClauseFnBlock(t *testing.T) {
	env := testEnv()
	script := `fn classify do
  0 -> echo zero
  1 -> echo one
  _ -> echo other
end
classify 0
classify 1
classify 42`
	got := captureOutput(env, func() {
		runSource(script, env)
	})
	want := "zero\none\nother\n"
	if got != want {
		t.Errorf("multi-clause fn: got %q, want %q", got, want)
	}
}

// TestStdlibIntegration tests stdlib functions in expression context.
func TestStdlibIntegration(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{"hd", "r = hd [1, 2, 3]\necho $r", "1\n"},
		{"tl", "r = tl [1, 2, 3]\necho $r", "[2, 3]\n"},
		{"length list", "r = length [10, 20, 30]\necho $r", "3\n"},
		{"length string", `r = length "hello"` + "\necho $r", "5\n"},
		{"append", "r = append [1, 2], 3\necho $r", "[1, 2, 3]\n"},
		{"concat", "r = concat [1, 2], [3, 4]\necho $r", "[1, 2, 3, 4]\n"},
		{"range", "r = range 1, 4\necho $r", "[1, 2, 3]\n"},
		{"at", "r = at [10, 20, 30], 1\necho $r", "20\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			got := captureOutput(env, func() {
				runSource(tt.script, env)
			})
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestNeedsMore validates that the REPL multiline detection uses the real parser.
func TestNeedsMore(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Complete statements — should NOT need more
		{"echo hello", false},
		{"echo then now", false},
		{"echo done deal", false},
		{"echo fi fo fum", false},
		{"echo in out", false},
		{"echo do re mi", false},
		{"true", false},
		{"FOO=bar", false},
		{"x = 5", false},
		{"for x in a b c; do echo $x; done", false},
		{"if true; then echo hi; fi", false},
		{"if true do echo hi end", false},
		{"fn foo x do x + 1 end", false},
		{"case $x in a) echo yes;; esac", false},

		// Unterminated constructs — SHOULD need more
		{"if true; then", true},
		{"if true do", true},
		{"for x in a b c; do", true},
		{"while true; do", true},
		{"fn foo x do", true},
		{"case $x in", true},
		{"{", true},
		{"(", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := needsMore(tt.input)
			if got != tt.want {
				t.Errorf("needsMore(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestPosixFunctionDef tests POSIX-style function definitions name() { body; }
func TestPosixFunctionDef(t *testing.T) {
	env := testEnv()
	got := captureOutput(env, func() {
		runSource("greet() { echo hi; }\ngreet", env)
	})
	want := "hi\n"
	if got != want {
		t.Errorf("POSIX fn def got %q, want %q", got, want)
	}
}

// TestCommandSubstitution tests $() command substitution.
func TestCommandSubstitution(t *testing.T) {
	env := testEnv()
	got := captureOutput(env, func() {
		runSource("x = $(echo hello)\necho $x", env)
	})
	want := "hello\n"
	if got != want {
		t.Errorf("command substitution got %q, want %q", got, want)
	}
}

// TestSubshell tests (cmd) subshell grouping.
func TestSubshell(t *testing.T) {
	env := testEnv()
	got := captureOutput(env, func() {
		runSource("(echo subshell)", env)
	})
	want := "subshell\n"
	if got != want {
		t.Errorf("subshell got %q, want %q", got, want)
	}
}

// TestStringConcat tests string concatenation via the + operator.
func TestStringConcat(t *testing.T) {
	env := testEnv()
	got := captureOutput(env, func() {
		runSource(`r = "hello" + " " + "world"
echo $r`, env)
	})
	want := "hello world\n"
	if got != want {
		t.Errorf("string concat got %q, want %q", got, want)
	}
}

// TestMapAccess tests map field access via dot syntax.
func TestMapAccess(t *testing.T) {
	env := testEnv()

	script := `m = %{x: 10, y: 20}
r = m.x
echo $r`
	got := captureOutput(env, func() {
		runSource(script, env)
	})
	want := "10\n"
	if got != want {
		t.Errorf("map access got %q, want %q", got, want)
	}
}

// TestTypeBuiltin tests the "type" builtin.
func TestTypeBuiltin(t *testing.T) {
	env := testEnv()
	got := captureOutput(env, func() {
		runSource("type echo", env)
	})
	if !strings.Contains(got, "builtin") {
		t.Errorf("type echo got %q, want something containing 'builtin'", got)
	}
}

// TestWhileBreak tests that break exits a while loop.
func TestWhileBreak(t *testing.T) {
	env := testEnv()
	script := `n = 5
while true; do
if [ $n -eq 2 ]; then
break
fi
n = (n - 1)
done
echo $n`
	got := captureOutput(env, func() {
		runSource(script, env)
	})
	want := "2\n"
	if got != want {
		t.Errorf("while break got %q, want %q", got, want)
	}
}

// TestMultipleAssignments tests sequential POSIX assignments.
func TestMultipleAssignments(t *testing.T) {
	env := testEnv()
	script := "A=1\nB=2\nC=3\necho $A $B $C"
	got := captureOutput(env, func() {
		runSource(script, env)
	})
	want := "1 2 3\n"
	if got != want {
		t.Errorf("multiple assignments got %q, want %q", got, want)
	}
}

// TestNestedIf tests nested if/then/else/fi.
func TestNestedIf(t *testing.T) {
	env := testEnv()
	script := `if true; then
if false; then
echo inner_true
else
echo inner_false
fi
fi`
	got := captureOutput(env, func() {
		runSource(script, env)
	})
	want := "inner_false\n"
	if got != want {
		t.Errorf("nested if got %q, want %q", got, want)
	}
}

// TestIshIfDoEnd tests the ish-style if/do/end syntax.
func TestIshIfDoEnd(t *testing.T) {
	env := testEnv()
	script := `if true do
echo ish_yes
end`
	got := captureOutput(env, func() {
		runSource(script, env)
	})
	want := "ish_yes\n"
	if got != want {
		t.Errorf("ish if/do/end got %q, want %q", got, want)
	}
}

// TestIshIfDoElseEnd tests ish-style if/do/else/end.
func TestIshIfDoElseEnd(t *testing.T) {
	env := testEnv()
	script := `if false do
echo yes
else
echo no
end`
	got := captureOutput(env, func() {
		runSource(script, env)
	})
	want := "no\n"
	if got != want {
		t.Errorf("ish if/do/else/end got %q, want %q", got, want)
	}
}

// TestComparisonOperators tests various comparison operators.
func TestComparisonOperators(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{"less than true", "r = 3 < 5\necho $r", ":true\n"},
		{"less than false", "r = 5 < 3\necho $r", ":false\n"},
		{"greater than true", "r = 5 > 3\necho $r", ":true\n"},
		{"less or equal true", "r = 3 <= 3\necho $r", ":true\n"},
		{"greater or equal true", "r = 5 >= 5\necho $r", ":true\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			got := captureOutput(env, func() {
				runSource(tt.script, env)
			})
			if got != tt.want {
				t.Errorf("script:\n%s\ngot:  %q\nwant: %q", tt.script, got, tt.want)
			}
		})
	}
}

func TestWildcardInPatterns(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{"tuple wildcard", "{_, b} = {1, 2}\necho $b", "2\n"},
		{"list wildcard", "[_, b] = [1, 2]\necho $b", "2\n"},
		{"atom wildcard", "{:ok, _} = {:ok, \"data\"}\necho matched", "matched\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			got := captureOutput(env, func() {
				runSource(tt.script, env)
			})
			if got != tt.want {
				t.Errorf("script:\n%s\ngot:  %q\nwant: %q", tt.script, got, tt.want)
			}
		})
	}
}

func TestPosixFnDefMultiline(t *testing.T) {
	env := testEnv()
	got := captureOutput(env, func() {
		runSource("myfunc()\n{ echo hello; }\nmyfunc", env)
	})
	if got != "hello\n" {
		t.Errorf("multiline fn def: got %q, want %q", got, "hello\n")
	}
}
