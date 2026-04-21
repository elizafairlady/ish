package eval_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"ish/internal/testutil"
)

func TestTutorialExamples(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		// === Section 1: Your First Commands ===
		{"s1 echo", "echo hello world", "hello world\n"},
		{"s1 pipe", `echo "hello world" | tr a-z A-Z`, "HELLO WORLD\n"},
		{"s1 and-or", "true && echo \"kept going\"\nfalse || echo \"swerved\"", "kept going\nswerved\n"},

		// === Section 2: Variables ===
		{"s2 posix assign", "NAME=world\necho $NAME", "world\n"},
		{"s2 double quote expand", "NAME=world\nname = \"world\"\necho \"hello $NAME\"\necho \"hello #{name}\"", "hello world\nhello world\n"},
		{"s2 single quote literal", "NAME=world\necho '$NAME'", "$NAME\n"},
		{"s2 string length", "NAME=hello\necho ${#NAME}", "5\n"},
		{"s2 prefix remove", "NAME=world\necho ${NAME#wo}", "rld\n"},
		{"s2 suffix remove", "NAME=world\necho ${NAME%rld}", "wo\n"},
		{"s2 replace", "NAME=world\necho ${NAME/world/fox}", "fox\n"},
		{"s2 default value", "echo ${UNSET_VAR_XYZ:-default}", "default\n"},

		// === Section 3: Data Types ===
		{"s3 atom", "status = :ok\necho $status", ":ok\n"},
		{"s3 tuple", "result = {:ok, \"the data\"}\necho $result", "{:ok, \"the data\"}\n"},
		{"s3 list", "nums = [1, 2, 3]\necho $nums", "[1, 2, 3]\n"},
		{"s3 map", "config = %{host: \"localhost\", port: 8080}\necho $config", "%{host: \"localhost\", port: 8080}\n"},
		{"s3 map access", "config = %{host: \"localhost\", port: 8080}\nr = config.host\necho $r", "localhost\n"},

		// === Section 4: Arithmetic ===
		{"s4 posix arith", "echo $(( 3 + 4 ))", "7\n"},
		{"s4 ish arith", "r = 3 + 4\necho $r", "7\n"},
		{"s4 precedence", "r = (2 + 3) * 4\necho $r", "20\n"},
		{"s4 comparison", "r = 5 == 5\necho $r", ":true\n"},
		{"s4 less than", "r = 3 < 5\necho $r", ":true\n"},
		{"s4 string concat", "r = \"hello\" + \" \" + \"world\"\necho $r", "hello world\n"},

		// === Section 5: Pattern Matching ===
		{"s5 tuple destructure", "{status, value} = {:ok, \"hello\"}\necho $status\necho $value", ":ok\nhello\n"},
		{"s5 list destructure", "[a, b, c] = [10, 20, 30]\necho $a $b $c", "10 20 30\n"},
		{"s5 head tail", "[first | rest] = [1, 2, 3, 4]\necho $first\necho $rest", "1\n[2, 3, 4]\n"},
		{"s5 multi head tail", "[a, b | rest] = [1, 2, 3, 4, 5]\necho $a\necho $b\necho $rest", "1\n2\n[3, 4, 5]\n"},
		{"s5 wildcard", "{_, value} = {:error, \"the message\"}\necho $value", "the message\n"},
		{"s5 match expr", "x = 2\nr = match x do\n1 -> :one\n2 -> :two\n_ -> :other\nend\necho $r", ":two\n"},
		{"s5 match tuple", "result = {:error, \"disk full\"}\nmatch result do\n{:ok, val} -> echo \"got: $val\"\n{:error, msg} -> echo \"failed: $msg\"\nend", "failed: disk full\n"},

		// === Section 6: Functions ===
		{"s6 greet", "fn greet name do\necho \"Hello, $name!\"\nend\ngreet world", "Hello, world!\n"},
		{"s6 add", "fn add a, b do\na + b\nend\nr = add(3, 4)\necho $r", "7\n"},
		{"s6 fib", "fn fib 0 do 0 end\nfn fib 1 do 1 end\nfn fib n when n > 1 do\nfib (n - 1) + fib (n - 2)\nend\nr = fib 10\necho $r", "55\n"},
		{"s6 abs", "fn abs n when n < 0 do 0 - n end\nfn abs n do n end\nr = abs (-5)\necho $r", "5\n"},
		{"s6 multi-clause block", "fn classify do\n0 -> :zero\n1 -> :one\n_ -> :other\nend\necho $(classify 0)", ":zero\n"},
		{"s6 anon dispatch", "f = fn do\n0 -> :zero\nn when n > 0 -> :positive\n_ -> :negative\nend\necho $(f 0)", ":zero\n"},
		{"s6 lambda single", "doubled = \\x -> x * 2\necho $(doubled 5)", "10\n"},
		{"s6 lambda multi param", "sum = \\a, b -> a + b\necho $(sum(3, 4))", "7\n"},
		{"s6 lambda zero param", "greet = \\ -> echo \"hello\"\ngreet", "hello\n"},
		{"s6 posix fn", "greet() { echo \"hi $1\"; }\ngreet world", "hi world\n"},
		{"s6 anon with params", "f = fn a, b do\na + b\nend\nr = f(3, 4)\necho $r", "7\n"},
		{"s6 anon single param", "doubled = fn x do x * 2 end\necho $(doubled 5)", "10\n"},

		// === Section 7: Two Kinds of Pipes ===
		{"s7 pipe arrow", "fn double x do x * 2 end\nfn inc x do x + 1 end\nr = 5 |> double |> inc\necho $r", "11\n"},

		// === Section 8: Control Flow ===
		{"s8 if then fi", "if true; then\necho \"yes\"\nfi", "yes\n"},
		{"s8 if else fi", "if false; then\necho \"yes\"\nelse\necho \"no\"\nfi", "no\n"},
		{"s8 for", "for i in a b c; do\necho $i\ndone", "a\nb\nc\n"},
		{"s8 case", "X=hello\ncase $X in\nhello)\necho \"matched\"\n;;\n*)\necho \"default\"\n;;\nesac", "matched\n"},
		{"s8 ish if do end", "if true do\necho \"yes\"\nend", "yes\n"},
		{"s8 ish if expression", "x = 5\nif x == 5 do\necho \"five\"\nend", "five\n"},

		// === Section 9: Error Handling ===
		{"s9 try rescue", "r = try do\n1 / 0\nrescue\n_ -> :caught\nend\necho $r", ":caught\n"},
		{"s9 try match error", "r = try do\n{:ok, val} = {:error, \"bad things\"}\nrescue\n{:error, msg} -> echo \"handled: $msg\"\nend", "handled: match error: expected :ok, got :error\n"},

		// === Section 11: When Things Crash ===
		{"s11 supervisor", "sup = supervise :one_for_one do\nworker :greeter fn do\necho \"worker started\"\nend\nend\nawait sup", "worker started\n"},

		// === Section 12: The Toolbox ===
		{"s12 map lambda", "r = [1, 2, 3] |> List.map \\x -> x * 2\necho $r", "[2, 4, 6]\n"},
		{"s12 filter lambda", "r = [1, 2, 3, 4, 5] |> List.filter \\x -> x >= 4\necho $r", "[4, 5]\n"},
		{"s12 reduce lambda", "r = [1, 2, 3, 4] |> List.reduce(0, \\acc, x -> acc + x)\necho $r", "10\n"},
		{"s12 range filter length", "r = List.range(1, 11) |> List.filter \\x -> x >= 6 |> length\necho $r", "5\n"},

		// === Script safety ===
		{"pipefail catches left failure", "set -o pipefail\nfalse | true\necho $?", "1\n"},
		{"no pipefail ignores left", "false | true\necho $?", "0\n"},
		{"set -e stops on failure", "set -e\ntrue\necho ok", "ok\n"},
		{"set -e if exempt", "set -e\nif false; then echo y; fi\necho ok", "ok\n"},
		{"set -e or exempt", "set -e\nfalse || echo swerved\necho ok", "swerved\nok\n"},
		{"set -e bang exempt", "set -e\n! false\necho ok", "ok\n"},
		{"trap ERR fires", "trap 'echo trapped' ERR\nfalse\necho after", "trapped\nafter\n"},
		{"trap ERR not on success", "trap 'echo trapped' ERR\ntrue\necho after", "after\n"},

		// === $() in strings ===
		{"cmdsub in string", "echo \"hello $(echo world)\"", "hello world\n"},
		{"cmdsub date", "echo \"year: $(date +%Y)\"", "year: 2026\n"},
		{"arith no spaces", "echo $((3+4))", "7\n"},
		{"arith in string", "echo \"r=$((2+2))\"", "r=4\n"},
		{"nested cmdsub", "echo \"nested: $(echo $(echo deep))\"", "nested: deep\n"},

		// === Float math ===
		{"float literal", "r = 3.14\necho $r", "3.14\n"},
		{"float division", "r = 5.0 / 2\necho $r", "2.5\n"},
		{"int division stays int", "r = 5 / 2\necho $r", "2\n"},
		{"float add", "r = 1 + 0.5\necho $r", "1.5\n"},
		{"float mul", "r = 3.14 * 2\necho $r", "6.28\n"},
		{"float int equality", "r = 5 == 5.0\necho $r", ":true\n"},
		{"float comparison", "r = 3.14 > 3\necho $r", ":true\n"},
		{"float negation", "r = -3.14\necho $r", "-3.14\n"},
		{"float string concat", "r = \"pi is \" + 3.14\necho $r", "pi is 3.14\n"},

		// === Pipe auto-coercion ===
		{"pipe value to cmd", "[1, 2, 3] | cat", "1\n2\n3\n"},
		{"pipe scalar to cmd", "42 | cat", "42\n"},
		{"pipe tuple to cmd", "{:ok, \"hi\"} | cat", "{:ok, \"hi\"}\n"},
		{"pipe value chain to cmd", "List.range(1, 4) |> List.filter \\x -> x > 1 | cat", "2\n3\n"},
		{"pipefn cmd to map", "r = printf \"a\\nb\\nc\\n\" |> List.map \\f -> String.upcase f\necho $r", "[\"A\", \"B\", \"C\"]\n"},
		{"pipefn cmd to length", "r = printf \"a\\nb\\nc\\n\" |> length\necho $r", "3\n"},
		{"pipefn explicit from_json", "r = echo \"{\\\"x\\\":1}\" |> JSON.parse\necho $r", "%{x: 1}\n"},

		// === Serialization (bridge) ===
		{"s12 JSON.parse map", "r = JSON.parse \"{\\\"name\\\":\\\"fox\\\"}\";\necho $r", "%{name: \"fox\"}\n"},
		{"s12 JSON.parse list", "r = JSON.parse \"[1,2,3]\"\necho $r", "[1, 2, 3]\n"},
		{"s12 JSON.encode", "r = JSON.encode [1, 2, 3]\necho $r", "[1,2,3]\n"},
		{"s12 IO.lines", "r = IO.lines \"hello\\nworld\"\necho $r", "[\"hello\\\\nworld\"]\n"},
		{"s12 IO.unlines", "r = IO.unlines [\"hello\", \"world\"]\necho $r", "hello world\n"},
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

func TestTutorialPingPong(t *testing.T) {
	env := testutil.TestEnv()

	script := `pid = spawn fn do
  receive do
    {:ping, sender} -> send sender, :pong
  end
end
send pid, {:ping, self}
receive do
  :pong -> echo "got pong"
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
		t.Fatal("timed out")
	}

	if got != "got pong\n" {
		t.Errorf("got %q, want %q", got, "got pong\n")
	}
}

func TestTutorialSelectiveReceive(t *testing.T) {
	env := testutil.TestEnv()

	script := `me = self
pid = spawn fn do
  send me, :b
  send me, :a
end
receive do
  :a -> echo "got a first"
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
		t.Fatal("timed out")
	}

	if got != "got a first\n" {
		t.Errorf("got %q, want %q", got, "got a first\n")
	}
}

func TestTutorialReceiveTimeout(t *testing.T) {
	env := testutil.TestEnv()

	script := `r = receive do
  msg -> msg
after 100 ->
  :timeout
end
echo $r`

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
		t.Fatal("timed out")
	}

	if got != ":timeout\n" {
		t.Errorf("got %q, want %q", got, ":timeout\n")
	}
}

func TestTutorialAwait(t *testing.T) {
	env := testutil.TestEnv()

	script := `task = spawn fn do 2 + 3 end
result = await task
echo $result`

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
		t.Fatal("timed out")
	}

	if got != "5\n" {
		t.Errorf("got %q, want %q", got, "5\n")
	}
}

func TestTutorialMonitor(t *testing.T) {
	env := testutil.TestEnv()

	script := `pid = spawn fn do
  receive do
    :quit -> :ok
  end
end
ref = monitor pid
send pid, :quit
receive do
  {:DOWN, ref, pid, reason} -> echo "exited: $reason"
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
		t.Fatal("timed out")
	}

	if got != "exited: :normal\n" {
		t.Errorf("got %q, want %q", got, "exited: :normal\n")
	}
}

func TestTutorialWhile(t *testing.T) {
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("n = 3\nwhile [ $n -gt 0 ]; do\necho $n\nn = (n - 1)\ndone", env)
	})
	if got != "3\n2\n1\n" {
		t.Errorf("while loop: got %q, want %q", got, "3\n2\n1\n")
	}
}

func TestTutorialFromJSON(t *testing.T) {
	env := testutil.TestEnv()
	script := `data = JSON.parse "{\"name\":\"fox\",\"age\":3}"
echo $data`
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource(script, env)
	})
	if !strings.Contains(got, "fox") || !strings.Contains(got, "3") {
		t.Errorf("JSON.parse: got %q, expected map with fox and 3", got)
	}
}

func TestTutorialToJSON(t *testing.T) {
	env := testutil.TestEnv()
	script := `r = JSON.encode [1, 2, 3]
echo $r`
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource(script, env)
	})
	if got != "[1,2,3]\n" {
		t.Errorf("JSON.encode: got %q, want %q", got, "[1,2,3]\n")
	}
}
