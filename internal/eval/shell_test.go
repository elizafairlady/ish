package eval

import (
	"os"
	"testing"
)

// =====================================================================
// Sourcing: source, .
// =====================================================================

func TestSource_FnDefinition(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/lib.ish", []byte("fn helper do \"sourced\" end"), 0644)
	run(t, "source "+dir+"/lib.ish\necho $(helper)", "sourced\n")
}

func TestSource_VariableInherits(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/vars.sh", []byte("Y=$X"), 0644)
	run(t, "X=hello; source "+dir+"/vars.sh; echo $Y", "hello\n")
}

func TestDot_SameAsSource(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/lib.sh", []byte("Z=dotted"), 0644)
	run(t, ". "+dir+"/lib.sh; echo $Z", "dotted\n")
}

// =====================================================================
// Aliases
// =====================================================================

func TestAlias_SimpleExpansion(t *testing.T) {
	run(t, "alias ll='echo listing'\nll", "listing\n")
}

func TestAlias_WithOriginalArgs(t *testing.T) {
	run(t, "alias say='echo'\nsay hello world", "hello world\n")
}

func TestAlias_Unalias(t *testing.T) {
	run(t, "alias greet='echo hi'\nunalias greet\ngreet 2>/dev/null; echo $?", "127\n")
}

func TestAlias_ChainedNotExpanded(t *testing.T) {
	// Alias expansion should not recurse
	run(t, "alias ls='echo fake-ls'\nls -la", "fake-ls -la\n")
}

// =====================================================================
// Readonly
// =====================================================================

func TestReadonly_PreventSet(t *testing.T) {
	run(t, "X=10; readonly X; X=20 2>/dev/null; echo $X", "10\n")
}

func TestReadonly_PreventUnset(t *testing.T) {
	run(t, "X=10; readonly X; unset X 2>/dev/null; echo $X", "10\n")
}

func TestReadonly_AllowRead(t *testing.T) {
	run(t, "X=42; readonly X; echo $X", "42\n")
}

// =====================================================================
// POSIX special builtins
// =====================================================================

func TestBuiltin_Printf_String(t *testing.T) {
	run(t, `printf "hello %s\n" world`, "hello world\n")
}

func TestBuiltin_Printf_Int(t *testing.T) {
	run(t, `printf "%d items\n" 42`, "42 items\n")
}

func TestBuiltin_Printf_NoNewline(t *testing.T) {
	run(t, `printf "no newline"`, "no newline")
}

func TestBuiltin_Exec(t *testing.T) {
	run(t, "exec echo replaced; echo should_not_appear", "replaced\n")
}

func TestBuiltin_Pwd(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	dir := t.TempDir()
	run(t, "cd "+dir+"; pwd", dir+"\n")
}

func TestBuiltin_Getopts(t *testing.T) {
	run(t, `set -- -a -b arg
while getopts "ab" opt; do
  echo $opt
done`, "a\nb\n")
}

func TestBuiltin_Umask(t *testing.T) {
	run(t, "umask 022; umask", "0022\n")
}

// =====================================================================
// Word splitting and quoting
// =====================================================================

func TestWordSplit_UnquotedVar(t *testing.T) {
	run(t, `X="a b c"; for w in $X; do echo $w; done`, "a\nb\nc\n")
}

func TestWordSplit_QuotedVar(t *testing.T) {
	run(t, `X="a b c"; for w in "$X"; do echo $w; done`, "a b c\n")
}

func TestWordSplit_QuotedCmdSub(t *testing.T) {
	run(t, `for w in "$(echo a b c)"; do echo $w; done`, "a b c\n")
}

func TestWordSplit_EmptyVar(t *testing.T) {
	run(t, `X=""; for w in $X; do echo word; done; echo done`, "done\n")
}

func TestQuoting_SingleQuoteLiteral(t *testing.T) {
	run(t, "echo 'hello $world'", "hello $world\n")
}

func TestQuoting_DoubleQuoteExpands(t *testing.T) {
	run(t, `X=world; echo "hello $X"`, "hello world\n")
}

func TestQuoting_EscapedDollar(t *testing.T) {
	run(t, `echo "\$HOME"`, "$HOME\n")
}

// =====================================================================
// Redirects
// =====================================================================

func TestRedirect_StderrToFile(t *testing.T) {
	dir := t.TempDir()
	// POSIX order: 2>file sets stderr, then >&2 copies stdout to stderr (the file)
	run(t, "echo err 2>"+dir+"/err.txt >&2; cat "+dir+"/err.txt", "err\n")
}

func TestRedirect_InputFromFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/in.txt", []byte("from file\n"), 0644)
	run(t, "cat <"+dir+"/in.txt", "from file\n")
}

func TestRedirect_AppendFile(t *testing.T) {
	dir := t.TempDir()
	run(t, "echo line1 >"+dir+"/out.txt; echo line2 >>"+dir+"/out.txt; cat "+dir+"/out.txt", "line1\nline2\n")
}

// =====================================================================
// Subshell isolation
// =====================================================================

func TestSubshell_VarIsolation(t *testing.T) {
	run(t, "X=outer; (X=inner); echo $X", "outer\n")
}

func TestSubshell_CwdIsolated(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	run(t, "D=$(pwd); (cd /tmp); test $(pwd) = $D && echo same", "same\n")
}

func TestSubshell_ExitDoesNotKillParent(t *testing.T) {
	run(t, "(exit 1); echo survived", "survived\n")
}
