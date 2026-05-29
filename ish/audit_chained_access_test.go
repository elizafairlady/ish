package ish

import (
	"strings"
	"testing"

	"ish/eval"
	"ish/expand"
)

// Chained qualified access `a.b.c` must not mis-split: `dotted-parts` separates
// the chain continuation from member b's argument list, so the literal `.` never
// leaks as an argument. There is no value-access protocol yet, so the default
// access protocol reports a clear "chained access not supported" error rather
// than the old misleading `unbound identifier: .`.
func TestChainedAccessReportsClearError(t *testing.T) {
	r := NewRuntime()
	p := expand.NewPackage("std/m")
	p.ExportValue("f", eval.Native("f", func(args []eval.Value, env *eval.Env) (eval.Value, error) {
		return nil, nil
	}))
	expand.RegisterPackage(r.Context, p)

	_, err := r.EvalSource("c", "import std/m\nm.f.c")
	if err == nil {
		t.Fatal("expected an error for chained access m.f.c")
	}
	if strings.Contains(err.Error(), "unbound identifier: .") {
		t.Fatalf("the `.` token leaked as an argument again: %v", err)
	}
	if !strings.Contains(err.Error(), "chained access") {
		t.Fatalf("expected a clear chained-access error, got: %v", err)
	}
}
