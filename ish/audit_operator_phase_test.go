package ish

import (
	"testing"

	"ish/core"
)

// Operators are part of the base language's expression syntax and must work at
// both phases: a macro body (phase 1) writing `1 + 2` enforests exactly as
// runtime code (phase 0) does, because the default std/impl/kernel protocol is
// activated at both phases (like the dual-phase kernel value builtins). This
// regressions the prior bug where operator bindings existed only at PhaseRuntime
// so `1 + 2` in a macro body failed with `unbound identifier: +` even though
// `add 1 2` worked.
func TestOperatorNotationWorksInMacroBody(t *testing.T) {
	// Function application in a macro body (control — always worked).
	control := "defmacro c stx -> datum->syntax stx (add 1 2)\n" +
		"c"
	if v, err := NewRuntime().EvalSource("control", control); err != nil || v != core.Int(3) {
		t.Fatalf("function call in macro body = %#v err=%v, want 3", v, err)
	}
	// The equivalent operator notation in a macro body must compute the same.
	operator := "defmacro m stx -> datum->syntax stx (1 + 2)\n" +
		"m"
	if v, err := NewRuntime().EvalSource("operator", operator); err != nil || v != core.Int(3) {
		t.Fatalf("operator notation in macro body = %#v err=%v, want 3", v, err)
	}
}

func TestOperatorRuntimePhaseBaseline(t *testing.T) {
	v, err := NewRuntime().EvalSource("baseline", "1 + 2")
	if err != nil || v != core.Int(3) {
		t.Fatalf("runtime-phase operator baseline broken: v=%#v err=%v", v, err)
	}
}
