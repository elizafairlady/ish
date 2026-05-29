package ish

import (
	"testing"

	"ish/core"
	"ish/eval"
	"ish/expand"
)

// These exercise the ish-authored std/impl/kernel default implementation —
// arithmetic/comparison operators and qualified package access — at the runtime
// layer that loads it. They migrated down from eval-layer tests that had relied
// on the deleted Go default implementation.

func TestDefaultOperatorsAndAccessOverImportedPackages(t *testing.T) {
	r := NewRuntime()
	base := expand.NewPackage("std/base")
	base.ExportValue("add", eval.Native("add", func(args []eval.Value, env *eval.Env) (eval.Value, error) {
		return core.Int(int64(args[0].(core.Int)) + int64(args[1].(core.Int))), nil
	}))
	expand.RegisterPackage(r.Context, base)
	math := expand.NewPackage("std/math")
	math.ExportValue("inc", eval.Native("inc", func(args []eval.Value, env *eval.Env) (eval.Value, error) {
		return core.Int(int64(args[0].(core.Int)) + 1), nil
	}))
	expand.RegisterPackage(r.Context, math)
	// `math.inc` is default qualified access (ish); `1 + 2` is a default operator
	// (ish); both resolve through std/impl/kernel without any Go access/operator.
	v, err := r.EvalSource("consumer", "import std/math\nuse std/base\nadd (math.inc 1) (1 + 2)")
	if err != nil {
		t.Fatalf("consumer errored: %v", err)
	}
	if v != core.Int(5) {
		t.Fatalf("import/use + default access/operators = %#v, want 5", v)
	}
}

func TestMacroProducedOperatorExpressionReentersDefaultOperators(t *testing.T) {
	// A macro that yields `(1 + 2)` re-enters the default operator protocol on
	// re-expansion of its output.
	v, err := NewRuntime().EvalSource("m", "defmacro sum stx -> %'(1 + 2)\nsum")
	if err != nil {
		t.Fatalf("eval errored: %v", err)
	}
	if v != core.Int(3) {
		t.Fatalf("macro-produced operator expression = %#v, want 3", v)
	}
}
