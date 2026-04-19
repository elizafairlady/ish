package eval_test

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExamples(t *testing.T) {
	// Build the binary
	buildCmd := exec.Command("go", "build", "-o", "ish", "./cmd/ish/")
	buildCmd.Dir = filepath.Join("..", "..")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build ish: %v\n%s", err, out)
	}

	ishBin := filepath.Join("..", "..", "ish")
	examplesDir := filepath.Join("..", "..", "examples")

	scripts, err := filepath.Glob(filepath.Join(examplesDir, "*.ish"))
	if err != nil {
		t.Fatal(err)
	}

	for _, script := range scripts {
		name := filepath.Base(script)
		t.Run(name, func(t *testing.T) {
			expectFail := scriptExpectsFail(script)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, ishBin, script)
			output, err := cmd.CombinedOutput()

			if ctx.Err() == context.DeadlineExceeded {
				if expectFail {
					t.Skipf("expected failure (hangs)")
				}
				t.Fatalf("timed out after 10s")
			}
			if err != nil {
				if expectFail {
					t.Skipf("expected failure: %v", err)
				}
				t.Fatalf("exit error: %v\noutput:\n%s", err, output)
			}
			if expectFail {
				t.Logf("XPASS: was expected to fail but passed")
			}
		})
	}
}

func scriptExpectsFail(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for i := 0; i < 5 && scanner.Scan(); i++ {
		if strings.Contains(scanner.Text(), "STATUS: FAILS") {
			return true
		}
	}
	return false
}
