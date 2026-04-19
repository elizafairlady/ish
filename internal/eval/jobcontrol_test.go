package eval

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestJobControlSIGTSTP(t *testing.T) {
	// Test that we can stop a child with SIGTSTP and detect it via Wait4+WUNTRACED.
	// We can't test terminal ownership (tcsetpgrp) in a test environment because
	// the test process isn't a session leader. Real terminal testing is manual.
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid

	// Verify child is in its own process group
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}
	if pgid != pid {
		t.Fatalf("child pgid=%d, want %d", pgid, pid)
	}

	// Send SIGTSTP to the child's process group
	time.Sleep(50 * time.Millisecond)
	if err := syscall.Kill(-pid, syscall.SIGTSTP); err != nil {
		t.Fatalf("kill -SIGTSTP: %v", err)
	}

	// Wait4 with WUNTRACED should detect the stop
	var ws syscall.WaitStatus
	done := make(chan struct{})
	go func() {
		syscall.Wait4(pid, &ws, syscall.WUNTRACED, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("Wait4 timed out — SIGTSTP did not stop the child")
	}

	if !ws.Stopped() {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("expected Stopped, got exited=%v signaled=%v", ws.Exited(), ws.Signaled())
	}
	t.Logf("child stopped, signal=%d", ws.StopSignal())

	// Resume with SIGCONT
	syscall.Kill(-pid, syscall.SIGCONT)
	time.Sleep(50 * time.Millisecond)

	// Now kill it and verify we can wait for exit
	syscall.Kill(pid, syscall.SIGKILL)
	syscall.Wait4(pid, &ws, 0, nil)
}

func TestJobControlSignalInheritance(t *testing.T) {
	// Verify that signal.Reset before Start() causes children to get SIG_DFL.
	// We do this by starting a child, sending SIGTSTP, and checking it stops.
	// If it inherited SIG_IGN, it would not stop.

	// First, set up signal handling like the shell does
	InitJobSignals()

	cmd := exec.Command("sleep", "30")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Reset signals before start (like evalExternalCmd does)
	signal.Reset(syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)
	if err := cmd.Start(); err != nil {
		renotifyJobSignals()
		t.Fatal(err)
	}
	renotifyJobSignals()

	pid := cmd.Process.Pid
	time.Sleep(50 * time.Millisecond)

	// Send SIGTSTP — if child inherited SIG_DFL, it will stop
	syscall.Kill(-pid, syscall.SIGTSTP)

	var ws syscall.WaitStatus
	done := make(chan struct{})
	go func() {
		syscall.Wait4(pid, &ws, syscall.WUNTRACED, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("child did not stop — signal disposition inherited as SIG_IGN")
	}

	if !ws.Stopped() {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("expected Stopped (SIG_DFL inherited), got exited=%v", ws.Exited())
	}
	t.Log("child stopped — confirms SIG_DFL was inherited, not SIG_IGN")

	syscall.Kill(pid, syscall.SIGKILL)
	syscall.Wait4(pid, &ws, 0, nil)
}
