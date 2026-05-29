package ish

import (
	"strings"
	"testing"
)

// The default `-` operator lowers to the kernel `sub`. A non-numeric left
// operand must produce an ish error, never a Go panic — `sub` type-checks both
// operands like every other arithmetic primitive. (Regression: `sub` previously
// did an unchecked assertion on its first argument.)
func TestSubOperatorNonNumericLeftErrors(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("`:a - 2` panicked the host instead of returning an ish error: %v", r)
		}
	}()
	_, err := NewRuntime().EvalSource("e", ":a - 2")
	if err == nil {
		t.Fatal("expected a language-level error for a non-numeric left operand")
	}
	if !strings.Contains(err.Error(), "non-numeric") {
		t.Fatalf("expected a non-numeric error, got: %v", err)
	}
}
