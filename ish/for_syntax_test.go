package ish

import (
	"testing"

	"ish/core"
)

// for-syntax (Racket's begin-for-syntax) raises the phase by one and evaluates
// its body there, so the definitions are available to transformer bodies, which
// run one phase up. This is what lets a macro call an ordinary helper function.
func TestForSyntax_HelperVisibleToMacro(t *testing.T) {
	src := "for-syntax do\n" +
		"  defn triple n do n * 3 end\n" +
		"end\n" +
		"defmacro tri stx -> syntax-parse stx do\n" +
		"  (_ x) -> datum->syntax stx (triple (syntax->datum x))\n" +
		"end\n" +
		"tri 14"
	v, err := NewRuntime().EvalSource("fs", src)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != core.Int(42) {
		t.Fatalf("tri 14 = %#v, want 42", v)
	}
}

// A for-syntax definition lives at phase 1; it must not leak into ordinary
// runtime (phase 0) code.
func TestForSyntax_NotVisibleAtRuntime(t *testing.T) {
	src := "for-syntax do\n  defn only-fs x do x end\nend\nonly-fs 5"
	if _, err := NewRuntime().EvalSource("fs", src); err == nil {
		t.Fatal("expected for-syntax binding to be invisible at runtime")
	}
}

// Mutually-recursive for-syntax helpers (a consecutive defn group) work, and
// remain usable from a later macro.
func TestForSyntax_RecursiveHelpers(t *testing.T) {
	src := "for-syntax do\n" +
		"  defn even-fs n do case (n == 0) do :true -> :true ; _ -> odd-fs (n - 1) end end\n" +
		"  defn odd-fs n do case (n == 0) do :true -> :false ; _ -> even-fs (n - 1) end end\n" +
		"end\n" +
		"defmacro is-even stx -> syntax-parse stx do\n" +
		"  (_ x) -> datum->syntax stx (even-fs (syntax->datum x))\n" +
		"end\n" +
		"is-even 10"
	v, err := NewRuntime().EvalSource("fs", src)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != core.Atom("true") {
		t.Fatalf("is-even 10 = %#v, want :true", v)
	}
}
