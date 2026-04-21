package eval_test

import (
	"sync"
	"testing"
	"time"

	"ish/internal/testutil"
)

// ==========================================================================
// Adversarial POSIX sh tests
//
// These test the boundaries of POSIX shell behavior that commonly break
// when a shell adds expression-oriented extensions. Every test here should
// also pass in dash/bash (modulo output format differences).
// ==========================================================================

func TestAdversarialPOSIX(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		// ---------------------------------------------------------------
		// Whitespace-sensitive assignment vs binding disambiguation
		// ---------------------------------------------------------------
		{"POSIX assign then echo", "X=hello; echo $X", "hello\n"},
		{"POSIX assign empty value", "X=; echo \"[$X]\"", "[]\n"},
		{"POSIX assign with command substitution", "X=$(echo hi); echo $X", "hi\n"},
		{"multiple POSIX assigns on one line", "A=1 B=2 C=3; echo $A $B $C", "1 2 3\n"},
		{"prefix assignment does not persist", "X=temp true; echo \"[$X]\"", "[]\n"},

		// ---------------------------------------------------------------
		// Quoting edge cases
		// ---------------------------------------------------------------
		{"single quotes preserve everything", `echo '$HOME "hello" $(cmd)'`, "$HOME \"hello\" $(cmd)\n"},
		{"double quotes expand variables", "X=world; echo \"hello $X\"", "hello world\n"},
		{"empty string argument", `echo "" hello`, " hello\n"},
		{"adjacent string concatenation", "echo 'foo''bar'", "foobar\n"},
		{"mixed quotes concatenation", `echo "foo"'bar'`, "foobar\n"},
		{"escaped double quote inside double quotes", `echo "hello \"world\""`, "hello \"world\"\n"},

		// ---------------------------------------------------------------
		// Parameter expansion
		// ---------------------------------------------------------------
		{"braced variable expansion", "X=hello; echo ${X}", "hello\n"},
		{"default value expansion unset", `echo ${UNSET_VAR:-default}`, "default\n"},
		{"default value expansion set", "X=real; echo ${X:-default}", "real\n"},

		// ---------------------------------------------------------------
		// Command substitution edge cases
		// ---------------------------------------------------------------
		{"nested command substitution", "echo $(echo $(echo deep))", "deep\n"},
		{"command sub in double quotes", `X=$(echo "hello world"); echo "$X"`, "hello world\n"},
		{"command sub trailing newlines stripped", "X=$(echo hello; echo); echo \"[$X]\"", "[hello]\n"},
		{"backtick command substitution", "X=`echo backtick`; echo $X", "backtick\n"},

		// ---------------------------------------------------------------
		// Redirections and file descriptors
		// ---------------------------------------------------------------
		{"redirect to /dev/null", "echo hidden > /dev/null", ""},
		{"stderr redirect does not affect stdout", "echo visible 2>/dev/null", "visible\n"},
		{"redirect append vs overwrite syntax", "echo ok >> /dev/null", ""},

		// ---------------------------------------------------------------
		// Control flow — semicolons, newlines, and nesting
		// ---------------------------------------------------------------
		{"semicolon-separated commands", "echo a; echo b; echo c", "a\nb\nc\n"},
		{"if with pipeline condition", "if echo hidden | grep -q hidden; then echo found; fi", "found\n"},
		{"if with negation", "if ! false; then echo yes; fi", "yes\n"},
		{"nested for loops", "for i in a b; do\nfor j in 1 2; do\necho $i$j\ndone\ndone", "a1\na2\nb1\nb2\n"},
		{"case with pipe pattern", "X=b\ncase $X in\na|b)\necho matched\n;;\nesac", "matched\n"},
		{"case with multiple patterns", "X=c\ncase $X in\na) echo A;;\nb) echo B;;\n*) echo other;;\nesac", "other\n"},
		{"while with compound condition", "n=3\nwhile [ $n -gt 0 ] && [ $n -lt 10 ]; do\necho $n\nn=$((n - 1))\ndone", "3\n2\n1\n"},

		// ---------------------------------------------------------------
		// Functions — POSIX style
		// ---------------------------------------------------------------
		{"POSIX function with args", "greet() { echo hello $1; }\ngreet world", "hello world\n"},
		{"POSIX function local scope", "x=outer\nf() { local x=inner; echo $x; }\nf\necho $x", "inner\nouter\n"},
		{"POSIX recursive function", "fact() {\nif [ $1 -le 1 ]; then\necho 1\nreturn\nfi\nlocal n=$1\nlocal prev=$(fact $((n - 1)))\necho $((n * prev))\n}\nfact 5", "120\n"},

		// ---------------------------------------------------------------
		// Special parameters
		// ---------------------------------------------------------------
		{"dollar-question after true", "true; echo $?", "0\n"},
		{"dollar-question after false", "false; echo $?", "1\n"},
		{"positional parameters via set", "set -- alpha beta gamma\necho $1 $2 $3", "alpha beta gamma\n"},
		{"dollar-hash parameter count", "set -- a b c d\necho $#", "4\n"},

		// ---------------------------------------------------------------
		// Keywords as arguments (the classic shell trap)
		// ---------------------------------------------------------------
		{"echo if as argument", "echo if then else fi", "if then else fi\n"},
		{"echo for as argument", "echo for in do done", "for in do done\n"},
		{"echo while as argument", "echo while do done", "while do done\n"},
		{"echo case as argument", "echo case in esac", "case in esac\n"},
		{"echo fn as argument", "echo fn end match", "fn end match\n"},
		{"echo keywords with other args", "echo before if after", "before if after\n"},

		// ---------------------------------------------------------------
		// Arithmetic expansion
		// ---------------------------------------------------------------
		{"arithmetic expansion basic", "echo $((2 + 3))", "5\n"},
		{"arithmetic expansion with variable", "X=10; echo $((X + 5))", "15\n"},
		{"arithmetic expansion nested ops", "echo $((2 * 3 + 4))", "10\n"},

		// ---------------------------------------------------------------
		// Heredoc edge cases
		// ---------------------------------------------------------------
		{"heredoc basic", "cat <<EOF\nhello world\nEOF", "hello world"},
		{"heredoc with variable expansion", "X=expanded\ncat <<EOF\n$X\nEOF", "expanded"},
		{"heredoc quoted delimiter no expansion", "X=hidden\ncat <<'EOF'\n$X\nEOF", "$X"},

		// ---------------------------------------------------------------
		// Pipeline edge cases
		// ---------------------------------------------------------------
		{"pipeline preserves ordering", "echo hello | cat", "hello\n"},
		{"long pipeline chain", "echo abc | cat | cat | cat", "abc\n"},
		{"pipeline with subshell", "(echo sub) | cat", "sub\n"},
		{"pipeline exit status from last", "false | true; echo $?", "0\n"},

		// ---------------------------------------------------------------
		// Background and job control
		// ---------------------------------------------------------------
		{"background job completes", "echo bg &\nwait", "bg\n"},

		// ---------------------------------------------------------------
		// Glob/special characters in strings
		// ---------------------------------------------------------------
		{"literal asterisk in quotes", `echo "*"`, "*\n"},
		{"literal question mark in quotes", `echo "?"`, "?\n"},
		{"hash is not comment mid-word", "echo foo#bar", "foo#bar\n"},

		// ---------------------------------------------------------------
		// Subshell isolation
		// ---------------------------------------------------------------
		{"subshell does not leak variables", "X=before\n(X=after)\necho $X", "before\n"},
		{"subshell does not leak functions", "(f() { echo inner; })\ntype f 2>/dev/null; echo $?", "1\n"},

		// ---------------------------------------------------------------
		// Edge case: empty and whitespace-only inputs
		// ---------------------------------------------------------------
		{"empty script produces no output", "", ""},
		{"whitespace-only script", "   \n\n   ", ""},
		{"comment-only script", "# just a comment\n# another one", ""},

		// ---------------------------------------------------------------
		// Multiple statements with varied separators
		// ---------------------------------------------------------------
		{"newline as separator", "echo a\necho b", "a\nb\n"},
		{"semicolon as separator", "echo a; echo b", "a\nb\n"},
		{"mixed separators", "echo a; echo b\necho c", "a\nb\nc\n"},
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

// ==========================================================================
// Adversarial ish expression tests
//
// These probe the boundary between command-mode and expression-mode,
// particularly the whitespace-driven disambiguation rules.
// ==========================================================================

func TestAdversarialIshExpressions(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		// ---------------------------------------------------------------
		// Assignment vs binding: whitespace is the disambiguator
		// ---------------------------------------------------------------
		{"ish binding vs POSIX assign", "x = 42\nX=42\necho $x $X", "42 42\n"},
		{"ish binding with expression", "x = 2 + 3\necho $x", "5\n"},
		{"ish binding does not leak from fn", "x = 1\nfn f do\nx = 2\nend\nf\necho $x", "1\n"},
		{"POSIX assign leaks from fn", "X=1\nf() { X=2; }\nf\necho $X", "2\n"},

		// ---------------------------------------------------------------
		// Operator spacing: the a-b vs a - b distinction
		// ---------------------------------------------------------------
		{"hyphenated word as command arg", "echo foo-bar", "foo-bar\n"},
		{"spaced minus is subtraction", "r = 10 - 3\necho $r", "7\n"},
		{"flag-like in command context", "echo -n hello", "hello"},
		{"double-hyphen flag", "echo --verbose", "--verbose\n"},

		// ---------------------------------------------------------------
		// Slash: path vs division
		// ---------------------------------------------------------------
		{"slash as path argument", "echo /usr/bin", "/usr/bin\n"},
		{"slash as division in binding", "r = 10 / 2\necho $r", "5\n"},
		{"path starting with dot-slash", "echo ./relative", "./relative\n"},
		{"path with multiple slashes", "echo /a/b/c/d", "/a/b/c/d\n"},

		// ---------------------------------------------------------------
		// Dot: field access vs file path
		// ---------------------------------------------------------------
		{"map field access", "m = %{x: 42}\necho $m.x", "42\n"},
		{"chained field access", "m = %{a: %{b: 99}}\necho $m.a.b", "99\n"},
		{"dotfile as command argument", "echo .gitignore", ".gitignore\n"},
		{"cd dotdot as command", "echo ..", "..\n"},

		// ---------------------------------------------------------------
		// Parentheses: subshell vs grouping vs function call
		// ---------------------------------------------------------------
		{"parens in expression context group", "r = (2 + 3) * 4\necho $r", "20\n"},
		{"subshell at statement start", "(echo subshell)", "subshell\n"},
		{"fn call with parens no space", "fn add a, b do\na + b\nend\nr = add(1, 2)\necho $r", "3\n"},
		{"fn call with parens and space is subshell-like", "r = (2 + 3)\necho $r", "5\n"},

		// ---------------------------------------------------------------
		// Pipe vs value pipe
		// ---------------------------------------------------------------
		{"unix pipe between commands", "echo hello | cat", "hello\n"},
		{"value pipe between functions", "fn double x do\nx * 2\nend\nr = 5 |> double\necho $r", "10\n"},
		{"value pipe chain", "fn inc x do\nx + 1\nend\nfn double x do\nx * 2\nend\nr = 3 |> inc |> double\necho $r", "8\n"},
		{"unix pipe does not pass values", "echo 42 | cat", "42\n"},

		// ---------------------------------------------------------------
		// Data structure literals
		// ---------------------------------------------------------------
		{"empty list", "l = []\necho $l", "[]\n"},
		{"nested lists", "l = [[1, 2], [3, 4]]\necho $l", "[[1, 2], [3, 4]]\n"},
		{"empty map", "m = %{}\necho $m", "%{}\n"},
		{"map with string keys", `m = %{name: "alice"}` + "\necho $m.name", "alice\n"},
		{"tuple with mixed types", `t = {:ok, 42, "hello"}` + "\necho $t", `{:ok, 42, "hello"}` + "\n"},
		{"single-element tuple", "t = {:ok}\necho $t", "{:ok}\n"},
		{"list of atoms", "l = [:a, :b, :c]\necho $l", "[:a, :b, :c]\n"},

		// ---------------------------------------------------------------
		// Pattern matching edge cases
		// ---------------------------------------------------------------
		{"wildcard discards value", "_ = 42\necho ok", "ok\n"},
		{"nested tuple destructure", "{a, {b, c}} = {1, {2, 3}}\necho $a $b $c", "1 2 3\n"},
		{"head|tail empty list rest", "[h | t] = [99]\necho $h $t", "99 []\n"},
		{"match with guard", "fn classify n when n > 0 do\n:positive\nend\nfn classify n when n < 0 do\n:negative\nend\nfn classify 0 do\n:zero\nend\necho $(classify 5)\necho $(classify (-3))\necho $(classify 0)", ":positive\n:negative\n:zero\n"},
		{"match expression all clauses", "x = 42\nr = match x do\n1 -> :one\n42 -> :answer\n_ -> :other\nend\necho $r", ":answer\n"},
		{"match on atom", "x = :error\nmatch x do\n:ok -> echo yes\n:error -> echo no\n_ -> echo unknown\nend", "no\n"},

		// ---------------------------------------------------------------
		// Lambda and higher-order functions
		// ---------------------------------------------------------------
		{"lambda stored in variable", "f = \\x -> x * 2\nr = f 10\necho $r", "20\n"},
		{"lambda with multiple params", "f = \\a, b -> a + b\nr = f 3, 4\necho $r", "7\n"},
		{"lambda in pipe", "r = 10 |> \\x -> x + 5\necho $r", "15\n"},
		{"zero-arity lambda", "f = \\ -> 42\necho $(f)", "42\n"},

		// ---------------------------------------------------------------
		// Multi-clause function dispatch
		// ---------------------------------------------------------------
		{"multi-clause pattern match", "fn describe :ok do\necho success\nend\nfn describe :error do\necho failure\nend\ndescribe :ok\ndescribe :error", "success\nfailure\n"},
		{"multi-clause with integers", "fn fib 0 do\n0\nend\nfn fib 1 do\n1\nend\nfn fib n when n > 1 do\nfib (n - 1) + fib (n - 2)\nend\nr = fib 8\necho $r", "21\n"},

		// ---------------------------------------------------------------
		// Boolean and comparison
		// ---------------------------------------------------------------
		{"boolean not in parens", "r = (!false)\necho $r", ":true\n"},
		{"equality returns atom", "r = 5 == 5\necho $r", ":true\n"},
		{"inequality returns atom", "r = 5 != 5\necho $r", ":false\n"},
		{"less than", "r = 3 < 5\necho $r", ":true\n"},
		{"greater than", "r = 5 > 3\necho $r", ":true\n"},
		{"chained comparison true", "r = 1 < 2 < 3\necho $r", ":true\n"},
		{"chained comparison false", "r = 3 < 2 < 1\necho $r", ":false\n"},

		// ---------------------------------------------------------------
		// String operations
		// ---------------------------------------------------------------
		{"string concatenation with +", `r = "hello" + " " + "world"` + "\necho $r", "hello world\n"},
		{"string interpolation", `name = "world"` + "\necho \"hello #{name}\"", "hello world\n"},
		{"string interpolation with expression", "x = 21\necho \"answer: #{x * 2}\"", "answer: 42\n"},
		{"dollar-string with escape", `echo $"tab\there"`, "tab\there\n"},

		// ---------------------------------------------------------------
		// Module system
		// ---------------------------------------------------------------
		{"module function call", "defmodule M do\nfn greet name do\n\"hello \" + name\nend\nend\nr = M.greet \"world\"\necho $r", "hello world\n"},
		{"module constant-like zero-arity", "defmodule C do\nfn pi do 3 end\nend\necho $(C.pi)", "3\n"},

		// ---------------------------------------------------------------
		// Stdlib integration
		// ---------------------------------------------------------------
		{"hd and tl", "echo $(hd [1, 2, 3]) $(tl [1, 2, 3])", "1 [2, 3]\n"},
		{"length of list", "echo $(length [1, 2, 3])", "3\n"},
		{"length of string", `echo $(length "hello")`, "5\n"},
		{"List.map", "use List\nr = List.map [1, 2, 3], \\x -> x * 2\necho $r", "[2, 4, 6]\n"},
		{"List.filter", "use List\nr = List.filter [1, 2, 3, 4], \\x -> x > 2\necho $r", "[3, 4]\n"},
		{"List.reduce", "use List\nr = List.reduce [1, 2, 3, 4], 0, \\acc, x -> acc + x\necho $r", "10\n"},

		// ---------------------------------------------------------------
		// Mixed POSIX + ish in same script
		// ---------------------------------------------------------------
		{"POSIX if with ish binding", "x = 42\nif [ $x -eq 42 ]; then\necho yes\nfi", "yes\n"},
		{"ish fn called from POSIX for", "fn double x do\nx * 2\nend\nfor i in 1 2 3; do\nr = double $i\necho $r\ndone", "2\n4\n6\n"},
		{"POSIX var in ish expression", "X=10\nr = $X + 5\necho $r", "15\n"},
		{"command sub in ish binding", "x = $(echo 42)\necho $x", "42\n"},
		{"ish data in POSIX case", "x = :hello\ncase $x in\n:hello)\necho matched\n;;\nesac", "matched\n"},

		// ---------------------------------------------------------------
		// if-do-end (ish) vs if-then-fi (POSIX) disambiguation
		// ---------------------------------------------------------------
		{"ish if with expression condition", "x = 5\nif x > 3 do\necho big\nend", "big\n"},
		{"ish if with else", "x = 1\nif x > 3 do\necho big\nelse\necho small\nend", "small\n"},
		{"posix if with test", "if [ 5 -gt 3 ]; then\necho yes\nfi", "yes\n"},

		// ---------------------------------------------------------------
		// Expression as top-level statement
		// ---------------------------------------------------------------
		{"bare integer at top level", "42", ""},
		{"bare atom at top level", ":hello", ""},
		{"bare string at top level", `"hello"`, ""},
		{"bare list at top level", "[1, 2, 3]", ""},
		{"bare tuple at top level", "{:ok, 42}", ""},

		// ---------------------------------------------------------------
		// Zero-arity function auto-call
		// ---------------------------------------------------------------
		{"zero-arity fn auto-call in expr", "fn greeting do\n\"hello\"\nend\nx = greeting\necho $x", "hello\n"},
		{"zero-arity fn capture prevents call", "fn greeting do\n\"hello\"\nend\nf = &greeting\necho $f", "#Function<greeting/1>\n"},

		// ---------------------------------------------------------------
		// try/rescue
		// ---------------------------------------------------------------
		{"try-rescue catches error", "r = try do\n1 / 0\nrescue\ne -> :caught\nend\necho $r", ":caught\n"},
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

// ==========================================================================
// Should-error tests: inputs that MUST produce errors, not silent misbehavior
// ==========================================================================

func TestAdversarialShouldError(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		// Unterminated blocks
		{"unterminated if-then", "if true; then echo hi"},
		{"unterminated for", "for x in a b; do echo $x"},
		{"unterminated while", "while true; do echo loop"},
		{"unterminated ish if", "if true do\necho hi"},
		{"unterminated fn", "fn foo do\necho hi"},
		{"unterminated match", "match x do\n:ok -> echo yes"},
		{"unterminated receive", "receive do\n:msg -> echo got"},
		{"unterminated try", "try do\necho hi"},

		// Mismatched terminators
		{"fi without if", "fi"},
		{"done without for/while", "done"},
		{"end without block", "end"},
		{"esac without case", "esac"},

		// Invalid syntax
		{"redirect without target", "echo hi >"},
		{"double redirect", "echo hi > >"},
		{"pipe to nothing at EOF", "echo hi |"},
		{"value pipe to nothing at EOF", "42 |>"},
		{"double pipe arrow", "x = [1,2,3] |> |> length"},
		{"standalone arrow", "->"},
		{"standalone pipe", "|"},
		{"standalone pipe arrow", "|>"},
		{"standalone or", "||"},
		{"standalone and", "&&"},
		{"standalone rparen", ")"},
		{"standalone rbrace", "}"},
		{"atom starting with digit", ":123abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done := make(chan struct{})
			go func() {
				defer close(done)
				env := testutil.TestEnv()
				testutil.CaptureOutput(env, func() {
					testutil.RunSource(tt.script, env)
				})
			}()

			select {
			case <-done:
				// Terminated — success (didn't hang)
			case <-time.After(3 * time.Second):
				t.Fatalf("script hung (did not terminate within 3s):\n%s", tt.script)
			}
		})
	}
}

// ==========================================================================
// Tests that stress concurrency and process model
// ==========================================================================

func TestAdversarialProcesses(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		// Basic spawn/send/receive
		{"spawn returns pid", "pid = spawn fn do :ok end\necho $(is_pid pid)", ":true\n"},

		// Multiple messages
		{"multiple sends and receives", `pid = spawn fn do
  receive do
    {:first, sender} -> send sender, :got_first
  end
  receive do
    {:second, sender} -> send sender, :got_second
  end
end
send pid, {:first, self}
receive do
  :got_first -> echo first
end
send pid, {:second, self}
receive do
  :got_second -> echo second
end`, "first\nsecond\n"},

		// Pattern match in receive
		{"receive pattern match", `pid = spawn fn do
  receive do
    {:add, a, b, sender} -> send sender, a + b
  end
end
send pid, {:add, 3, 4, self}
receive do
  result -> echo $result
end`, "7\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testutil.TestEnv()
			var got string
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				got = testutil.CaptureOutput(env, func() {
					testutil.RunSource(tt.script, env)
				})
			}()
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("timed out after 5s")
			}
			if got != tt.want {
				t.Errorf("script:\n%s\ngot:  %q\nwant: %q", tt.script, got, tt.want)
			}
		})
	}
}

// ==========================================================================
// Adversarial parser-level: inputs that test the boundary between
// command parsing and expression parsing without executing
// ==========================================================================

func TestAdversarialWhitespaceSensitivity(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		// The core ambiguity: spaces around = change meaning entirely
		{"no-space equals is POSIX assign", "X=42; echo $X", "42\n"},
		{"spaced equals is ish binding", "x = 42\necho $x", "42\n"},

		// Minus with and without spaces
		{"no-space minus is flag", "echo -n hello", "hello"},
		{"spaced minus is subtraction", "r = 10 - 3\necho $r", "7\n"},
		{"double-dash flag", "echo -- hello", "-- hello\n"},

		// Plus with and without spaces
		{"spaced plus is addition", "r = 2 + 3\necho $r", "5\n"},

		// Adjacent parens = function call; spaced = subshell/grouping
		{"fn-call no-space parens", "fn add a, b do\na + b\nend\nr = add(1, 2)\necho $r", "3\n"},

		// Dot: adjacent = field access; in path = path component
		{"adjacent dot is field access", "m = %{x: 1}\necho $m.x", "1\n"},
		{"dot in path context", "echo /tmp/.", "/tmp/.\n"},

		// Slash: always path in command context, division in expression
		{"slash as path in command", "echo /etc/hosts", "/etc/hosts\n"},
		{"slash as division in expression", "r = 100 / 10\necho $r", "10\n"},

		// The trickiest: an identifier followed by something that could
		// be either an operator or part of a command argument
		{"ident-then-star is glob arg", "echo '*'", "*\n"},
		{"ident-then-star-spaced is multiplication", "r = 3 * 4\necho $r", "12\n"},
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

// ==========================================================================
// Test that ish-style and POSIX-style control flow can be deeply nested
// and interleaved without confusion
// ==========================================================================

func TestAdversarialNestedControlFlow(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{"posix if inside ish fn", "fn check x do\nif [ $x -gt 0 ]; then\necho positive\nelse\necho non_positive\nfi\nend\ncheck 5\ncheck (-1)", "positive\nnon_positive\n"},

		// ish if inside POSIX for
		{"ish if inside posix for", "for i in 1 2 3; do\nif $i == 2 do\necho found\nend\ndone", "found\n"},

		// Nested ish if
		{"nested ish if", "x = 5\nif x > 0 do\nif x < 10 do\necho in_range\nend\nend", "in_range\n"},

		// Match inside for
		{"match inside for loop", "for i in 1 2 3; do\nr = match $i do\n1 -> :one\n2 -> :two\n_ -> :other\nend\necho $r\ndone", ":one\n:two\n:other\n"},

		// For inside fn
		{"for inside fn", "fn enumerate items do\nfor item in $items do\necho $item\nend\nend\nenumerate [a, b, c]", "a\nb\nc\n"},

		// POSIX case inside ish fn
		{"case inside ish fn", "fn dispatch cmd do\ncase $cmd in\nstart)\necho starting\n;;\nstop)\necho stopping\n;;\nesac\nend\ndispatch start\ndispatch stop", "starting\nstopping\n"},

		// Deeply nested POSIX
		{"triple nested if", "if true; then\nif true; then\nif true; then\necho deep\nfi\nfi\nfi", "deep\n"},

		// While with ish binding inside
		{"while with ish counter", "n = 3\nwhile [ $n -gt 0 ]; do\necho $n\nn = (n - 1)\ndone", "3\n2\n1\n"},
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
