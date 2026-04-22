package eval_test

// Comprehensive POSIX sh integration tests organized as a tutorial.
// Each section covers a chapter of a typical POSIX shell reference.

import (
	"os"
	"testing"

	"ish/internal/testutil"
)

func posixRun(t *testing.T, tests []struct {
	name   string
	script string
	want   string
}) {
	t.Helper()
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

// =========================================================================
// Chapter 1: Simple Commands and Arguments
// =========================================================================

func TestPOSIX_SimpleCommands(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"single command", "echo hello", "hello\n"},
		{"multiple arguments", "echo one two three", "one two three\n"},
		{"no arguments", "echo", "\n"},
		{"semicolon separator", "echo a; echo b", "a\nb\n"},
		{"newline separator", "echo a\necho b", "a\nb\n"},
		{"empty lines ignored", "\n\necho ok\n\n", "ok\n"},
		{"comments", "# this is a comment\necho ok", "ok\n"},
		{"inline comment", "echo ok # comment", "ok\n"},
		{"colon no-op", ": this does nothing\necho ok", "ok\n"},
		{"true returns 0", "true; echo $?", "0\n"},
		{"false returns 1", "false; echo $?", "1\n"},
	})
}

// =========================================================================
// Chapter 2: Variables
// =========================================================================

func TestPOSIX_Variables(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		// Assignment
		{"simple assignment", "X=hello; echo $X", "hello\n"},
		{"assignment no space around equals", "X=42; echo $X", "42\n"},
		{"empty value", "X=; echo \"[$X]\"", "[]\n"},
		{"multiple assignments", "A=1 B=2 C=3; echo $A $B $C", "1 2 3\n"},
		{"reassignment", "X=old; X=new; echo $X", "new\n"},
		{"unset variable is empty", "echo \"[$NONEXISTENT]\"", "[]\n"},

		// Expansion
		{"braced expansion", "X=hello; echo ${X}", "hello\n"},
		{"expansion in double quotes", "X=world; echo \"hello $X\"", "hello world\n"},
		{"adjacent expansion", "X=foo; echo ${X}bar", "foobar\n"},

		// Prefix assignments
		{"prefix assign scoped to command", "X=temp true; echo \"[$X]\"", "[]\n"},
		// prefix assign visibility to child env is not yet testable in this harness
	})
}

// =========================================================================
// Chapter 3: Quoting
// =========================================================================

func TestPOSIX_Quoting(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		// Single quotes
		{"single quotes literal", `echo 'hello world'`, "hello world\n"},
		{"single quotes no expansion", `echo '$HOME'`, "$HOME\n"},
		{"single quotes preserve specials", `echo '$(cmd) "quotes" $var'`, "$(cmd) \"quotes\" $var\n"},

		// Double quotes
		{"double quotes with spaces", `echo "hello world"`, "hello world\n"},
		{"double quotes expand vars", "X=world; echo \"hello $X\"", "hello world\n"},
		{"double quotes expand cmd sub", `echo "today is $(echo Monday)"`, "today is Monday\n"},
		{"double quotes preserve single quotes", `echo "it's fine"`, "it's fine\n"},
		{"escaped quote in double quotes", `echo "say \"hi\""`, "say \"hi\"\n"},
		{"escaped dollar in double quotes", `echo "price is \$5"`, "price is $5\n"},
		{"escaped backslash in double quotes", `echo "back\\slash"`, "back\\slash\n"},

		// Quote concatenation
		{"adjacent single-double", `echo 'foo'"bar"`, "foobar\n"},
		{"adjacent double-single", `echo "foo"'bar'`, "foobar\n"},
		{"empty strings", `echo "" "" ""`, "  \n"},
		{"empty string preserves arg", `echo "" hello`, " hello\n"},

		// Backslash outside quotes
		{"backslash escapes space", "echo hello\\ world", "hello world\n"},
	})
}

// =========================================================================
// Chapter 4: Parameter Expansion
// =========================================================================

func TestPOSIX_ParameterExpansion(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		// Default values ${var:-word}
		{"default unset", `echo ${UNSET:-default}`, "default\n"},
		{"default set", `X=real; echo ${X:-default}`, "real\n"},
		{"default empty with colon", `X=; echo ${X:-default}`, "default\n"},

		// Assign default ${var:=word}
		{"assign default unset", `echo ${NEWVAR:=assigned}; echo $NEWVAR`, "assigned\nassigned\n"},
		{"assign default set", `X=existing; echo ${X:=other}`, "existing\n"},

		// Alternate value ${var:+word}
		{"alternate unset", `echo "[${UNSET:+alt}]"`, "[]\n"},
		{"alternate set", `X=yes; echo "[${X:+alt}]"`, "[alt]\n"},
		{"alternate empty with colon", `X=; echo "[${X:+alt}]"`, "[]\n"},

		// String length ${#var}
		{"string length", `X=hello; echo ${#X}`, "5\n"},
		{"string length empty", `X=; echo ${#X}`, "0\n"},

		// Suffix removal ${var%pattern} ${var%%pattern}
		{"suffix shortest", `X=file.tar.gz; echo ${X%.*}`, "file.tar\n"},
		{"suffix longest", `X=file.tar.gz; echo ${X%%.*}`, "file\n"},

		// Prefix removal ${var#pattern} ${var##pattern}
		{"prefix shortest", `X=/usr/local/bin; echo ${X#*/}`, "usr/local/bin\n"},
		{"prefix longest", `X=/usr/local/bin; echo ${X##*/}`, "bin\n"},
	})
}

// =========================================================================
// Chapter 5: Command Substitution
// =========================================================================

func TestPOSIX_CommandSubstitution(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"dollar-paren basic", "echo $(echo hello)", "hello\n"},
		{"backtick basic", "echo `echo hello`", "hello\n"},
		{"nested dollar-paren", "echo $(echo $(echo deep))", "deep\n"},
		{"in double quotes", `X=$(echo "hello world"); echo "$X"`, "hello world\n"},
		{"trailing newlines stripped", `X=$(echo hello; echo); echo "[$X]"`, "[hello]\n"},
		{"in variable assignment", "X=$(echo value); echo $X", "value\n"},
		{"in arithmetic context", "X=5; echo $((X + $(echo 3)))", "8\n"},
		{"empty result", `X=$(true); echo "[$X]"`, "[]\n"},
		{"multiline result", `X=$(echo a; echo b); echo "$X"`, "a\nb\n"},
		{"in for loop", "for x in $(echo a b c); do echo $x; done", "a\nb\nc\n"},
	})
}

// =========================================================================
// Chapter 6: Arithmetic Expansion
// =========================================================================

func TestPOSIX_Arithmetic(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"addition", "echo $((2 + 3))", "5\n"},
		{"subtraction", "echo $((10 - 3))", "7\n"},
		{"multiplication", "echo $((4 * 5))", "20\n"},
		{"division", "echo $((20 / 4))", "5\n"},
		{"modulo", "echo $((17 % 5))", "2\n"},
		{"precedence mul before add", "echo $((2 + 3 * 4))", "14\n"},
		{"parentheses", "echo $(((2 + 3) * 4))", "20\n"},
		{"with variable", "X=10; echo $((X + 5))", "15\n"},
		{"negative result", "echo $((3 - 10))", "-7\n"},
		{"nested arithmetic", "echo $((2 * (3 + 4)))", "14\n"},
		{"zero division guard", "echo $(( 0 / 1 ))", "0\n"},
	})
}

// =========================================================================
// Chapter 7: Redirections
// =========================================================================

func TestPOSIX_Redirections(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"stdout to /dev/null", "echo hidden > /dev/null", ""},
		{"stderr to /dev/null", "echo visible 2>/dev/null", "visible\n"},
		{"append to /dev/null", "echo ok >> /dev/null", ""},
		{"heredoc basic", "cat <<EOF\nhello world\nEOF", "hello world"},
		{"heredoc with expansion", "X=yes; cat <<EOF\n$X\nEOF", "yes"},
		{"heredoc quoted no expansion", "X=no; cat <<'EOF'\n$X\nEOF", "$X"},
		{"heredoc strip tabs", "cat <<-EOF\n\thello\n\tworld\nEOF", "hello\nworld"},
		{"herestring", "cat <<<hello", "hello\n"},
		{"herestring with var", "X=hi; cat <<<$X", "hi\n"},
	})
}

// =========================================================================
// Chapter 8: Pipelines
// =========================================================================

func TestPOSIX_Pipelines(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"simple pipe", "echo hello | cat", "hello\n"},
		{"three-stage pipe", "echo abc | cat | cat", "abc\n"},
		{"pipe with grep", "printf \"apple\\nbanana\\ncherry\\n\" | grep an", "banana\n"},
		{"pipe with sort", "printf \"c\\na\\nb\\n\" | sort", "a\nb\nc\n"},
		{"pipe with wc -l", "printf \"a\\nb\\nc\\n\" | wc -l", "3\n"},
		{"pipe with head", "printf \"a\\nb\\nc\\n\" | head -n 2", "a\nb\n"},
		{"pipe with tail", "printf \"a\\nb\\nc\\n\" | tail -n 1", "c\n"},
		{"pipe exit status from last", "false | true; echo $?", "0\n"},
		{"subshell in pipe", "(echo sub) | cat", "sub\n"},
	})
}

// TestPOSIX_PipeWithDottedFilename tests that commands with uppercase-dotted
// filename arguments (README.md, Makefile.bak) work correctly when piped.
// Regression: evalWithIO didn't handle NAccess args with the same
// try-eval-then-stringify fallback that evalCmd uses, so "cat README.md | grep foo"
// would silently produce no output because Eval(NAccess("README","md")) fails
// and the arg is lost.
func TestPOSIX_PipeWithDottedFilename(t *testing.T) {
	// Create a temp dir and cd into it so bare "README.md" resolves
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/README.md", []byte("line1 hello\nline2 world\nline3 hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"uppercase dotted file piped to grep", "cat README.md | grep world", "line2 world\nline3 hello world\n"},
		{"uppercase dotted file piped to cat", "cat README.md | cat", "line1 hello\nline2 world\nline3 hello world\n"},
		{"uppercase dotted file piped to wc -l", "cat README.md | wc -l", "3\n"},
	})
}

// =========================================================================
// Chapter 9: Lists (&&, ||, ;, &)
// =========================================================================

func TestPOSIX_Lists(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		// AND lists
		{"and both true", "true && echo yes", "yes\n"},
		{"and first false", "false && echo no", ""},
		{"and chain", "true && true && echo ok", "ok\n"},
		{"and chain fails early", "true && false && echo no", ""},

		// OR lists
		{"or first true", "true || echo no", ""},
		{"or first false", "false || echo yes", "yes\n"},
		{"or chain", "false || false || echo ok", "ok\n"},
		{"or chain short-circuits", "true || echo no || echo also_no", ""},

		// Mixed
		{"and-or combo", "false || true && echo yes", "yes\n"},
		{"or-and combo", "true || false && echo yes", "yes\n"},
		{"false and then or", "false && echo no || echo fallback", "fallback\n"},
	})
}

// =========================================================================
// Chapter 10: if / elif / else / fi
// =========================================================================

func TestPOSIX_If(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"if true", "if true; then echo yes; fi", "yes\n"},
		{"if false", "if false; then echo yes; fi", ""},
		{"if else", "if false; then echo yes; else echo no; fi", "no\n"},
		{"elif", "if false; then echo a; elif true; then echo b; fi", "b\n"},
		{"elif chain", "if false; then echo a; elif false; then echo b; elif true; then echo c; fi", "c\n"},
		{"elif with else", "if false; then echo a; elif false; then echo b; else echo c; fi", "c\n"},
		{"nested if", "if true; then\nif false; then echo inner; else echo outer; fi\nfi", "outer\n"},
		{"if with test", "if [ 5 -gt 3 ]; then echo yes; fi", "yes\n"},
		{"if with negation", "if ! false; then echo yes; fi", "yes\n"},
		{"if with pipeline", "if echo hidden | grep -q hidden; then echo found; fi", "found\n"},
		{"if with and list", "if true && true; then echo yes; fi", "yes\n"},
		{"if with or list", "if false || true; then echo yes; fi", "yes\n"},
	})
}

// =========================================================================
// Chapter 11: for / in / do / done
// =========================================================================

func TestPOSIX_For(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"basic for", "for i in a b c; do echo $i; done", "a\nb\nc\n"},
		{"for with numbers", "for n in 1 2 3; do echo $n; done", "1\n2\n3\n"},
		{"for with break", "for i in a b c d; do\nif [ $i = c ]; then break; fi\necho $i\ndone", "a\nb\n"},
		{"for with continue", "for i in a b c d; do\nif [ $i = b ]; then continue; fi\necho $i\ndone", "a\nc\nd\n"},
		{"nested for", "for i in a b; do\nfor j in 1 2; do\necho $i$j\ndone\ndone", "a1\na2\nb1\nb2\n"},
		{"for with command sub", "for x in $(echo a b c); do echo $x; done", "a\nb\nc\n"},
		{"for with glob-like words", "for f in foo bar baz; do echo $f; done", "foo\nbar\nbaz\n"},
		{"for empty list", "for i in; do echo $i; done", ""},
	})
}

// =========================================================================
// Chapter 12: while / until
// =========================================================================

func TestPOSIX_WhileUntil(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"while basic", "n=3; while [ $n -gt 0 ]; do echo $n; n=$((n - 1)); done", "3\n2\n1\n"},
		{"while with break", "n=5; while true; do\nif [ $n -eq 2 ]; then break; fi\nn=$((n - 1))\ndone\necho $n", "2\n"},
		{"while false never runs", "while false; do echo no; done; echo ok", "ok\n"},
		{"until basic", "n=0; until [ $n -eq 3 ]; do n=$((n + 1)); done; echo $n", "3\n"},
		{"until true never runs", "until true; do echo no; done; echo ok", "ok\n"},
	})
}

// =========================================================================
// Chapter 13: case / esac
// =========================================================================

func TestPOSIX_Case(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"match first", "X=hello; case $X in hello) echo matched;; esac", "matched\n"},
		{"match second", "X=world; case $X in hello) echo no;; world) echo yes;; esac", "yes\n"},
		{"wildcard default", "X=other; case $X in hello) echo no;; *) echo default;; esac", "default\n"},
		{"pipe alternation", "X=b; case $X in a|b) echo matched;; esac", "matched\n"},
		{"no match", "X=z; case $X in a) echo no;; b) echo no;; esac", ""},
		{"empty subject", "X=; case $X in '') echo empty;; *) echo other;; esac", "empty\n"},
		{"numeric match", "X=42; case $X in 42) echo found;; esac", "found\n"},
	})
}

// =========================================================================
// Chapter 14: Functions
// =========================================================================

func TestPOSIX_Functions(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"define and call", "greet() { echo hello; }\ngreet", "hello\n"},
		{"with arguments", "greet() { echo hello $1; }\ngreet world", "hello world\n"},
		{"multiple args", "f() { echo $1 $2 $3; }\nf a b c", "a b c\n"},
		{"return value", "f() { return 42; }\nf; echo $?", "42\n"},
		{"return 0", "f() { return 0; }\nf; echo $?", "0\n"},
		{"local variables", "x=outer; f() { local x=inner; echo $x; }; f; echo $x", "inner\nouter\n"},
		{"globals leak", "x=1; f() { x=2; }; f; echo $x", "2\n"},
		{"recursive", "countdown() {\nif [ $1 -le 0 ]; then echo done; return; fi\necho $1\ncountdown $(($1 - 1))\n}\ncountdown 3", "3\n2\n1\ndone\n"},
		{"multiline body", "f() {\necho line1\necho line2\n}\nf", "line1\nline2\n"},
		{"redefinition replaces", "f() { echo old; }\nf() { echo new; }\nf", "new\n"},
	})
}

// =========================================================================
// Chapter 15: Special Parameters
// =========================================================================

func TestPOSIX_SpecialParams(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"exit status true", "true; echo $?", "0\n"},
		{"exit status false", "false; echo $?", "1\n"},
		{"positional params", "set -- a b c; echo $1 $2 $3", "a b c\n"},
		{"param count", "set -- a b c d; echo $#", "4\n"},
		{"dollar-at preserves boundaries", "f() { echo $1; echo $2; }\nset -- \"hello world\" foo\nf $@", "hello world\nfoo\n"},
		{"shift removes first", "set -- a b c; shift; echo $1 $2", "b c\n"},
		{"shift n", "set -- a b c d; shift 2; echo $1 $2", "c d\n"},
		// $$ tested separately in TestPOSIX_SpecialParams_PID
	})
}

func TestPOSIX_SpecialParams_PID(t *testing.T) {
	// $$ should return a nonzero numeric string
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("echo $$", env)
	})
	if got == "\n" || got == "0\n" || got == "" {
		t.Errorf("$$ should be nonzero pid, got %q", got)
	}
}

// =========================================================================
// Chapter 16: Test Expressions [ ]
// =========================================================================

func TestPOSIX_Test(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		// String tests
		{"string equal", "[ foo = foo ]; echo $?", "0\n"},
		{"string not equal", "[ foo = bar ]; echo $?", "1\n"},
		{"string inequality", "[ foo != bar ]; echo $?", "0\n"},
		{"string non-empty -n", `[ -n "hello" ]; echo $?`, "0\n"},
		{"string empty -n", `[ -n "" ]; echo $?`, "1\n"},
		{"string empty -z", `[ -z "" ]; echo $?`, "0\n"},
		{"string non-empty -z", `[ -z "hello" ]; echo $?`, "1\n"},

		// Numeric tests
		{"num equal", "[ 5 -eq 5 ]; echo $?", "0\n"},
		{"num not equal", "[ 5 -ne 3 ]; echo $?", "0\n"},
		{"num greater", "[ 5 -gt 3 ]; echo $?", "0\n"},
		{"num less", "[ 3 -lt 5 ]; echo $?", "0\n"},
		{"num greater-equal", "[ 5 -ge 5 ]; echo $?", "0\n"},
		{"num less-equal", "[ 3 -le 5 ]; echo $?", "0\n"},

		// File tests (use known paths)
		{"file exists -e", "[ -e /dev/null ]; echo $?", "0\n"},
		{"file not exists", "[ -e /nonexistent_path_xyz ]; echo $?", "1\n"},
		{"is directory -d", "[ -d /tmp ]; echo $?", "0\n"},
		{"is not directory", "[ -d /dev/null ]; echo $?", "1\n"},

		// Negation
		{"not true", "[ ! -e /nonexistent_path_xyz ]; echo $?", "0\n"},
		{"not false", "[ ! -e /dev/null ]; echo $?", "1\n"},

		// Compound (test builtin compat)
		{"and -a", "[ 1 -eq 1 -a 2 -eq 2 ]; echo $?", "0\n"},
		{"and -a false", "[ 1 -eq 1 -a 2 -eq 3 ]; echo $?", "1\n"},
		{"or -o", "[ 1 -eq 2 -o 2 -eq 2 ]; echo $?", "0\n"},
	})
}

// =========================================================================
// Chapter 17: set Options
// =========================================================================

func TestPOSIX_SetOptions(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"set -e exits on failure", "set -e; true; echo before; false; echo after", "before\n"},
		{"set -e spared in if", "set -e; if false; then echo no; fi; echo survived", "survived\n"},
		{"set -e spared in &&", "set -e; false && echo no; echo survived", "survived\n"},
		{"set -e spared in ||", "set -e; false || echo recovered; echo ok", "recovered\nok\n"},
		{"set +e undoes set -e", "set -e; set +e; false; echo survived", "survived\n"},
	})
}

// =========================================================================
// Chapter 18: Subshells and Groups
// =========================================================================

func TestPOSIX_SubshellsGroups(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		// Subshells
		{"subshell basic", "(echo sub)", "sub\n"},
		{"subshell var isolation", "X=outer; (X=inner); echo $X", "outer\n"},
		{"subshell cwd isolation", "(cd /tmp); echo ok", "ok\n"},
		{"subshell exit code", "(exit 42); echo $?", "42\n"},
		{"nested subshells", "((echo deep))", "deep\n"},

		// Groups
		{"group basic", "{ echo group; }", "group\n"},
		{"group shares scope", "X=before; { X=after; }; echo $X", "after\n"},
		{"group multiple commands", "{ echo a; echo b; }", "a\nb\n"},
	})
}

// =========================================================================
// Chapter 19: Builtins
// =========================================================================

func TestPOSIX_Builtins(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		// echo
		{"echo basic", "echo hello", "hello\n"},
		{"echo -n no newline", "echo -n hello", "hello"},
		{"echo multiple", "echo a b c", "a b c\n"},

		// printf
		{"printf basic", `printf "%s\n" hello`, "hello\n"},
		{"printf format", `printf "%d + %d = %d\n" 2 3 5`, "2 + 3 = 5\n"},
		{"printf escape", `printf "a\tb\n"`, "a\tb\n"},

		// export
		{"export and read", "export FOO=bar; echo $FOO", "bar\n"},

		// unset
		{"unset variable", "X=hello; unset X; echo \"[$X]\"", "[]\n"},

		// readonly
		{"readonly prevents change", "readonly X=42; echo $X", "42\n"},

		// eval
		{"eval basic", `eval 'echo hello'`, "hello\n"},
		{"eval with expansion", `CMD="echo world"; eval $CMD`, "world\n"},

		// command -v
		{"command -v builtin", "command -v echo; echo $?", "echo\n0\n"},
		{"command -v not found", "command -v nonexistent_cmd_xyz; echo $?", "1\n"},

		// type
		{"type builtin", "type echo", "echo is a shell builtin\n"},
	})
}

func TestPOSIX_Builtins_Cd(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		testutil.RunSource("cd /tmp; pwd", env)
	})
	if got != "/tmp\n" {
		t.Errorf("cd /tmp; pwd got %q, want %q", got, "/tmp\n")
	}
	os.Chdir(orig) // restore
}

// =========================================================================
// Chapter 20: Filenames, Paths, and Dotted Names
// =========================================================================

func TestPOSIX_Filenames(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"simple filename", "echo file.txt", "file.txt\n"},
		{"path with extension", "echo src/main.go", "src/main.go\n"},
		{"multiple extensions", "echo archive.tar.gz", "archive.tar.gz\n"},
		{"dotfile", "echo .gitignore", ".gitignore\n"},
		{"dotdot", "echo ..", "..\n"},
		{"absolute path", "echo /usr/local/bin", "/usr/local/bin\n"},
		{"relative path", "echo ./script.sh", "./script.sh\n"},
		{"path with numbers", "echo log.2024.01.txt", "log.2024.01.txt\n"},
		{"IP address", "echo 192.168.1.120", "192.168.1.120\n"},
		{"user@host", "echo user@host.example.com", "user@host.example.com\n"},
		{"url-like path", "echo https://example.com", "https://example.com\n"},
		{"tilde path", "echo ~/Documents", os.Getenv("HOME") + "/Documents\n"},
	})
}

// =========================================================================
// Chapter 21: Keywords as Arguments
// =========================================================================

func TestPOSIX_KeywordsAsArgs(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		{"echo if", "echo if", "if\n"},
		{"echo then", "echo then", "then\n"},
		{"echo else", "echo else", "else\n"},
		{"echo fi", "echo fi", "fi\n"},
		{"echo for", "echo for", "for\n"},
		{"echo in", "echo in", "in\n"},
		{"echo do", "echo do", "do\n"},
		{"echo done", "echo done", "done\n"},
		{"echo while", "echo while", "while\n"},
		{"echo until", "echo until", "until\n"},
		{"echo case", "echo case", "case\n"},
		{"echo esac", "echo esac", "esac\n"},
		{"echo fn", "echo fn", "fn\n"},
		{"echo end", "echo end", "end\n"},
		{"all together", "echo if then else fi for in do done while case esac", "if then else fi for in do done while case esac\n"},
	})
}

// =========================================================================
// Chapter 22: Real-World One-Liners
// =========================================================================

func TestPOSIX_RealWorld(t *testing.T) {
	posixRun(t, []struct {
		name   string
		script string
		want   string
	}{
		// Common patterns from tutorials
		{"count to 5", "i=1; while [ $i -le 5 ]; do echo $i; i=$((i + 1)); done", "1\n2\n3\n4\n5\n"},
		{"reverse list", "for i in 5 4 3 2 1; do echo $i; done", "5\n4\n3\n2\n1\n"},
		{"string in variable check", `X=hello; if [ "$X" = hello ]; then echo match; fi`, "match\n"},
		{"default config", `DB=${DATABASE:-localhost}; echo $DB`, "localhost\n"},
		{"basename-like", `F=/path/to/file.txt; echo ${F##*/}`, "file.txt\n"},
		{"extension-like", `F=archive.tar.gz; echo ${F%%.*}`, "archive\n"},
		{"conditional assignment", "X=; if [ -z \"$X\" ]; then X=default; fi; echo $X", "default\n"},
		{"pipeline word count", "printf \"hello world\\n\" | wc -w", "2\n"},
		{"sum via for", "sum=0; for n in 1 2 3 4 5; do sum=$((sum + n)); done; echo $sum", "15\n"},
		{"factorial iterative", "n=5; f=1; while [ $n -gt 1 ]; do f=$((f * n)); n=$((n - 1)); done; echo $f", "120\n"},
	})
}
