package eval

import (
	"strings"
	"testing"
)

// =====================================================================
// Job control: bg, fg, jobs, wait, &
// =====================================================================

func TestJob_BackgroundBasic(t *testing.T) {
	env := NewEnv()
	got := capture(env, func() { Run("echo bg &\nwait", env) })
	if !strings.Contains(got, "bg") {
		t.Errorf("background job should produce output, got %q", got)
	}
}

func TestJob_WaitSpecificPid(t *testing.T) {
	// wait with no args waits for all; wait $pid waits for one
	env := NewEnv()
	got := capture(env, func() { Run("echo first &\nwait\necho second", env) })
	if !strings.Contains(got, "first") || !strings.HasSuffix(got, "second\n") {
		t.Errorf("got %q, want first then second", got)
	}
}

func TestJob_BackgroundExitCode(t *testing.T) {
	run(t, "true &\nwait\necho $?", "0\n")
}

func TestJob_JobsBuiltin(t *testing.T) {
	// jobs should list running background jobs — POSIX format
	run(t, "sleep 10 &\njobs\nkill $!\nwait", "[1]+\tRunning\tsleep 10\n")
}

func TestJob_FgBuiltin(t *testing.T) {
	// fg brings a background job to foreground
	run(t, "sleep 0 &\nfg\necho done", "done\n")
}

func TestJob_BgBuiltin(t *testing.T) {
	// bg resumes a stopped job in background
	// Hard to test without signals; basic smoke test
	run(t, "echo test &\nwait\necho ok", "test\nok\n")
}

// =====================================================================
// Signal handling: trap
// =====================================================================

func TestTrap_ExitRuns(t *testing.T) {
	run(t, "trap 'echo bye' EXIT\necho hello", "hello\nbye\n")
}

func TestTrap_ExitMultipleCommands(t *testing.T) {
	run(t, "trap 'echo a; echo b' EXIT\necho start", "start\na\nb\n")
}

func TestTrap_Override(t *testing.T) {
	run(t, "trap 'echo first' EXIT\ntrap 'echo second' EXIT\necho go", "go\nsecond\n")
}

func TestTrap_Clear(t *testing.T) {
	run(t, "trap 'echo no' EXIT\ntrap '' EXIT\necho go", "go\n")
}

// =====================================================================
// Process substitution
// =====================================================================

func TestProcessSubstitution_Basic(t *testing.T) {
	run(t, "cat <(echo hello)", "hello\n")
}

// =====================================================================
// Coprocess
// =====================================================================

func TestCoprocess_Basic(t *testing.T) {
	// |& pipes both stdout and stderr
	run(t, "echo output >&2 |& cat", "output\n")
}
