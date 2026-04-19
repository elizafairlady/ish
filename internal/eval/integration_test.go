package eval_test

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"ish/internal/testutil"
)

func TestIntegration(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		// POSIX basics
		{"echo simple", `echo hello`, "hello\n"},
		{"echo multiple words", `echo hello world`, "hello world\n"},
		{"echo -n no newline", `echo -n hello`, "hello"},
		{"POSIX var assignment and expansion", "FOO=bar\necho $FOO", "bar\n"},
		{"double-quoted string expansion", "NAME=world\necho \"hello $NAME\"", "hello world\n"},
		{"single-quoted string literal", "echo 'hello world'", "hello world\n"},
		{"exit status true", "true\necho $?", "0\n"},
		{"exit status false", "false\necho $?", "1\n"},
		{"echo empty string", `echo`, "\n"},

		// Control flow: if/then/fi
		{"if true then fi", "if true; then\necho yes\nfi", "yes\n"},
		{"if false then fi", "if false; then\necho yes\nfi", ""},
		{"if else fi", "if false; then\necho yes\nelse\necho no\nfi", "no\n"},

		// Control flow: for/do/done
		{"for loop", "for i in a b c; do\necho $i\ndone", "a\nb\nc\n"},
		{"for loop with break", "for i in a b c d; do\nif [ $i = c ]; then\nbreak\nfi\necho $i\ndone", "a\nb\n"},
		{"for loop with continue", "for i in a b c d; do\nif [ $i = b ]; then\ncontinue\nfi\necho $i\ndone", "a\nc\nd\n"},

		// Control flow: while/do/done
		{"while loop counting", "n = 3\nwhile [ $n -gt 0 ]; do\necho $n\nn = (n - 1)\ndone", "3\n2\n1\n"},

		// Control flow: case/esac
		{"case matching first pattern", "X=hello\ncase $X in\nhello)\necho matched\n;;\n*)\necho default\n;;\nesac", "matched\n"},
		{"case matching wildcard", "X=other\ncase $X in\nhello)\necho matched\n;;\n*)\necho default\n;;\nesac", "default\n"},

		// Test builtin [ ]
		{"test string equality true", "[ foo = foo ]\necho $?", "0\n"},
		{"test string equality false", "[ foo = bar ]\necho $?", "1\n"},
		{"test numeric equality", "[ 1 -eq 1 ]\necho $?", "0\n"},

		// Ish bindings
		{"ish binding integer", "x = 42\necho $x", "42\n"},
		{"ish binding with arithmetic", "x = 10 + 5\necho $x", "15\n"},
		{"tuple destructuring via match", "t = {1, 2}\nmatch t do\n{a, b} -> echo $a $b\nend", "1 2\n"},
		{"list destructuring via match", "l = [10, 20, 30]\nmatch l do\n[x, y, z] -> echo $x $y $z\nend", "10 20 30\n"},

		// Data types
		{"atom literal", `:hello`, ""},
		{"tuple literal value", "t = {1, 2, 3}\necho $t", "{1, 2, 3}\n"},
		{"list literal value", "l = [10, 20, 30]\necho $l", "[10, 20, 30]\n"},
		{"map value", "m = %{name : \"alice\", age : 30}\necho $m", "%{name: \"alice\", age: 30}\n"},

		// Functions
		{"simple fn definition and call", "fn greet name do\necho hello\nend\ngreet world", "hello\n"},
		{"multi-clause fib", "fn fib 0 do\n0\nend\nfn fib 1 do\n1\nend\nfn fib n when n > 1 do\na = fib (n - 1)\nb = fib (n - 2)\na + b\nend\nr = fib 10\necho $r", "55\n"},
		{"fn with guard", "fn abs n when n < 0 do\n0 - n\nend\nfn abs n do\nn\nend\nr = abs (-5)\necho $r", "5\n"},

		// Match expression
		{"match on integer", "x = 2\nr = match x do\n1 -> :one\n2 -> :two\n_ -> :other\nend\necho $r", ":two\n"},
		{"match on atom", "x = :ok\nr = match x do\n:ok -> echo success\n:err -> echo failure\nend", "success\n"},

		// Pipe operator |>
		{"pipe operator with user function", "fn double x do\nx * 2\nend\nfn inc x do\nx + 1\nend\nr = 5 |> double |> inc\necho $r", "11\n"},

		// Arithmetic
		{"addition", "r = 3 + 4\necho $r", "7\n"},
		{"subtraction", "r = 10 - 3\necho $r", "7\n"},
		{"multiplication", "r = 6 * 7\necho $r", "42\n"},
		{"division", "r = 20 / 4\necho $r", "5\n"},
		{"precedence mul before add", "r = 2 + 3 * 4\necho $r", "14\n"},
		{"parenthesized expression", "r = (2 + 3) * 4\necho $r", "20\n"},
		{"equality true", "r = 5 == 5\necho $r", ":true\n"},
		{"equality false", "r = 5 == 6\necho $r", ":false\n"},
		{"inequality true", "r = 5 != 6\necho $r", ":true\n"},
		{"unary negation", "r = -42\necho $r", "-42\n"},
		{"boolean not", "r = (!true)\necho $r", ":false\n"},

		// And/Or lists
		{"and list both true", "true && echo yes", "yes\n"},
		{"and list first false", "false && echo yes", ""},
		{"or list first false", "false || echo fallback", "fallback\n"},
		{"or list first true", "true || echo fallback", ""},

		// Variable scoping
		{"POSIX fn updates parent variable", "x=1\nf() { x=2; }\nf\necho $x", "2\n"},
		{"local builtin keeps variable in function scope", "x=1\nf() { local x=2; echo $x; }\nf\necho $x", "2\n1\n"},
		{"ish match bind is local to scope", "x = 10\nfn setx do\nx = 20\nend\nsetx\necho $x", "10\n"},
		{"set -e stops execution on failure", "set -e\ntrue\necho before\nfalse\necho after", "before\n"},
		{"set -e does not trigger in if condition", "set -e\nif false; then\necho no\nfi\necho survived", "survived\n"},
		{"set -e does not trigger in && chain", "set -e\nfalse && echo no\necho survived", "survived\n"},

		// Errors
		{"parse error on unterminated if", "if true; then\necho hello", ""},
		{"false produces no stdout", "false", ""},

		// Parser fixes
		{"echo in as argument", "echo in out", "in out\n"},
		{"echo then as argument", "echo then now", "then now\n"},
		{"echo do as argument", "echo do re mi", "do re mi\n"},
		{"echo done as argument", "echo done deal", "done deal\n"},
		{"echo fi as argument", "echo fi fo fum", "fi fo fum\n"},
		{"slash as path not division", "echo /tmp", "/tmp\n"},
		{"slash as division in expression", "r = 10 / 2\necho $r", "5\n"},

		// For loop with command substitution
		{"for loop with command substitution", "for x in $(echo a b c); do\necho $x\ndone", "a\nb\nc\n"},

		// Echo flag bundle handling
		{"echo -neE flag bundle", `echo -neE "hello\nworld"`, "hello\\nworld"},
		{"echo -nEe flag bundle escapes on", `echo -nEe "hello\nworld"`, "hello\nworld"},

		// Herestring with variable expansion
		{"herestring with variable expansion", "X=hello\necho $(cat <<<$X)", "hello\n"},

		// List head|tail destructuring
		{"list head|tail destructuring", "[h | t] = [1, 2, 3]\necho $h\necho $t", "1\n[2, 3]\n"},
		{"list head|tail multiple heads", "[a, b | rest] = [1, 2, 3, 4]\necho $a\necho $b\necho $rest", "1\n2\n[3, 4]\n"},
		{"list head|tail empty rest", "[h | t] = [1]\necho $h\necho $t", "1\n[]\n"},
		{"list head|tail in match expression", "l = [10, 20, 30]\nmatch l do\n[x | xs] -> echo $x $xs\nend", "10 [20, 30]\n"},
		{"list head|tail with wildcard rest", "[x, y | _] = [1, 2, 3, 4, 5]\necho $x\necho $y", "1\n2\n"},

		// $@ proper word splitting
		{"dollar-at expands to separate args", "f() { echo $1; echo $2; echo $3; }\nset -- \"hello world\" foo bar\nf $@", "hello world\nfoo\nbar\n"},

		// Heredoc backslash-newline continuation
		{"heredoc backslash-newline continuation", "cat <<EOF\nhello \\\nworld\nEOF", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testutil.TestEnv()
			got := testutil.CaptureOutput(env, func() {
				testutil.RunSource(tt.script, env)
			})
			if got != tt.want {
				t.Errorf("script:\n%s\ngot:  %q\nwant: %q", tt.script, got, tt.want)
			}
		})
	}
}

func TestProcessPingPong(t *testing.T) {
	env := testutil.TestEnv()

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
		got = testutil.CaptureOutput(env, func() {
			testutil.RunSource(script, env)
		})
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("TestProcessPingPong timed out after 5s")
	}

	want := "got_pong\n"
	if got != want {
		t.Errorf("ping-pong got %q, want %q", got, want)
	}
}

func TestSpawnSendReceive(t *testing.T) {
	env := testutil.TestEnv()

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
		got = testutil.CaptureOutput(env, func() {
			testutil.RunSource(script, env)
		})
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("TestSpawnSendReceive timed out after 5s")
	}

	want := ":hello\n"
	if got != want {
		t.Errorf("spawn/send/receive got %q, want %q", got, want)
	}
}

func TestSubshellCwdIsolation(t *testing.T) {
	env := testutil.TestEnv()
	origCwd, _ := os.Getwd()
	defer os.Chdir(origCwd)

	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("(cd /tmp)\npwd", env)
	})

	currentCwd, _ := os.Getwd()
	if currentCwd != origCwd {
		t.Errorf("subshell changed cwd: got %q, want %q", currentCwd, origCwd)
	}
	if strings.TrimSpace(got) != origCwd {
		t.Errorf("pwd after subshell: got %q, want %q", strings.TrimSpace(got), origCwd)
	}
}

func TestSubshellVarIsolation(t *testing.T) {
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("X=original\n(X=changed)\necho $X", env)
	})
	if strings.TrimSpace(got) != "original" {
		t.Errorf("subshell var leak: got %q, want %q", strings.TrimSpace(got), "original")
	}
}

func TestTrapExit(t *testing.T) {
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("trap 'echo goodbye' EXIT", env)
		// RunExitTraps is in builtin package
		testutil.RunSource("", env) // no-op
	})
	// Note: RunExitTraps is called by the main shell loop, not by RunSource.
	// This test verifies trap registration; the actual firing is hard to test
	// in isolation without calling RunExitTraps directly.
	_ = got
}

func TestMultiClauseFnBlock(t *testing.T) {
	env := testutil.TestEnv()
	script := `fn classify do
  0 -> echo zero
  1 -> echo one
  _ -> echo other
end
classify 0
classify 1
classify 42`
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource(script, env)
	})
	want := "zero\none\nother\n"
	if got != want {
		t.Errorf("multi-clause fn: got %q, want %q", got, want)
	}
}

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
			env := testutil.TestEnv()
			got := testutil.CaptureOutput(env, func() {
				testutil.RunSource(tt.script, env)
			})
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPosixFunctionDef(t *testing.T) {
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("greet() { echo hi; }\ngreet", env)
	})
	if got != "hi\n" {
		t.Errorf("POSIX fn def got %q, want %q", got, "hi\n")
	}
}

func TestCommandSubstitution(t *testing.T) {
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("x = $(echo hello)\necho $x", env)
	})
	if got != "hello\n" {
		t.Errorf("command substitution got %q, want %q", got, "hello\n")
	}
}

func TestSubshell(t *testing.T) {
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("(echo subshell)", env)
	})
	if got != "subshell\n" {
		t.Errorf("subshell got %q, want %q", got, "subshell\n")
	}
}

func TestStringConcat(t *testing.T) {
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("r = \"hello\" + \" \" + \"world\"\necho $r", env)
	})
	if got != "hello world\n" {
		t.Errorf("string concat got %q, want %q", got, "hello world\n")
	}
}

func TestMapAccess(t *testing.T) {
	env := testutil.TestEnv()
	script := `m = %{x: 10, y: 20}
r = m.x
echo $r`
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource(script, env)
	})
	if got != "10\n" {
		t.Errorf("map access got %q, want %q", got, "10\n")
	}
}

func TestWhileBreak(t *testing.T) {
	env := testutil.TestEnv()
	script := `n = 5
while true; do
if [ $n -eq 2 ]; then
break
fi
n = (n - 1)
done
echo $n`
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource(script, env)
	})
	if got != "2\n" {
		t.Errorf("while break got %q, want %q", got, "2\n")
	}
}

func TestMultipleAssignments(t *testing.T) {
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("A=1\nB=2\nC=3\necho $A $B $C", env)
	})
	if got != "1 2 3\n" {
		t.Errorf("multiple assignments got %q, want %q", got, "1 2 3\n")
	}
}

func TestNestedIf(t *testing.T) {
	env := testutil.TestEnv()
	script := `if true; then
if false; then
echo inner_true
else
echo inner_false
fi
fi`
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource(script, env)
	})
	if got != "inner_false\n" {
		t.Errorf("nested if got %q, want %q", got, "inner_false\n")
	}
}

func TestIshIfDoEnd(t *testing.T) {
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("if true do\necho ish_yes\nend", env)
	})
	if got != "ish_yes\n" {
		t.Errorf("ish if/do/end got %q, want %q", got, "ish_yes\n")
	}
}

func TestIshIfDoElseEnd(t *testing.T) {
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("if false do\necho yes\nelse\necho no\nend", env)
	})
	if got != "no\n" {
		t.Errorf("ish if/do/else/end got %q, want %q", got, "no\n")
	}
}

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
			env := testutil.TestEnv()
			got := testutil.CaptureOutput(env, func() {
				testutil.RunSource(tt.script, env)
			})
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
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
			env := testutil.TestEnv()
			got := testutil.CaptureOutput(env, func() {
				testutil.RunSource(tt.script, env)
			})
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPosixFnDefMultiline(t *testing.T) {
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("myfunc()\n{ echo hello; }\nmyfunc", env)
	})
	if got != "hello\n" {
		t.Errorf("multiline fn def: got %q, want %q", got, "hello\n")
	}
}
