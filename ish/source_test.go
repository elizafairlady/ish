package ish

import (
	"testing"

	"ish/core"
)

func TestRuntimeEvalSourceProgram(t *testing.T) {
	r := NewRuntime()
	v, err := r.EvalSource("test", "x=1\nx")
	if err != nil {
		t.Fatalf("EvalSource failed: %v", err)
	}
	if v != core.Int(1) {
		t.Fatalf("EvalSource = %#v, want 1", v)
	}
}

func TestRuntimeEvalSourceCanDefineMacro(t *testing.T) {
	r := NewRuntime()
	v, err := r.EvalSource("test", "defmacro ok stx -> %':ok\nok")
	if err != nil {
		t.Fatalf("EvalSource failed: %v", err)
	}
	if v != core.Atom("ok") {
		t.Fatalf("EvalSource macro = %#v, want :ok", v)
	}
}

func TestRuntimeEvalSourceDefaultKernelOperatorsActive(t *testing.T) {
	r := NewRuntime()
	v, err := r.EvalSource("test", "1 + 2 * 3")
	if err != nil {
		t.Fatalf("EvalSource failed: %v", err)
	}
	if v != core.Int(7) {
		t.Fatalf("default operators = %#v, want 7", v)
	}
}

func TestRuntimeEvalSourceDoesNotHaveShellByDefault(t *testing.T) {
	r := NewRuntime()
	if _, err := r.EvalSource("test", "printf hello"); err == nil {
		t.Fatal("printf hello unexpectedly worked without shell implementation")
	}
}

func TestRuntimeEvalSourceRejectsUnclaimedTrailingDot(t *testing.T) {
	r := NewRuntime()
	if _, err := r.EvalSource("test", "foo."); err == nil {
		t.Fatal("foo. unexpectedly expanded without a claiming protocol")
	}
}

func TestRuntimeEvalSourceDoesNotHaveTasksByDefault(t *testing.T) {
	r := NewRuntime()
	if _, err := r.EvalSource("test", "build: [main.o]"); err == nil {
		t.Fatal("task syntax unexpectedly worked without task implementation")
	}
}

func TestLoadPackageSourceAndUseIt(t *testing.T) {
	r := NewRuntime()
	if _, err := r.LoadPackageSource("std/id", "std/id/id.ish", "export id\ndefn id x do\n  x\nend"); err != nil {
		t.Fatalf("LoadPackageSource failed: %v", err)
	}
	v, err := r.EvalSource("test", "do\n  import std/id\n  id.id 7\nend")
	if err != nil {
		t.Fatalf("EvalSource failed: %v", err)
	}
	if v != core.Int(7) {
		t.Fatalf("package call = %#v, want 7", v)
	}
}

func TestLoadPackageSourceCanImportPackage(t *testing.T) {
	r := NewRuntime()
	if _, err := r.LoadPackageSource("std/id", "std/id/id.ish", "export id\ndefn id x do\n  x\nend"); err != nil {
		t.Fatalf("LoadPackageSource id failed: %v", err)
	}
	if _, err := r.LoadPackageSource("std/wrap", "std/wrap/wrap.ish", "export wrap\nimport std/id\ndefn wrap x do\n  id.id x\nend"); err != nil {
		t.Fatalf("LoadPackageSource wrap failed: %v", err)
	}
	v, err := r.EvalSource("test", "do\n  import std/wrap\n  wrap.wrap 9\nend")
	if err != nil {
		t.Fatalf("EvalSource failed: %v", err)
	}
	if v != core.Int(9) {
		t.Fatalf("dependent package call = %#v, want 9", v)
	}
}

func TestLoadPackageSourceExportsMacro(t *testing.T) {
	r := NewRuntime()
	if _, err := r.LoadPackageSource("std/macros", "std/macros/macros.ish", "export ok\ndefmacro ok stx -> %':ok"); err != nil {
		t.Fatalf("LoadPackageSource macros failed: %v", err)
	}
	v, err := r.EvalSource("test", "do\n  use std/macros\n  ok\nend")
	if err != nil {
		t.Fatalf("EvalSource failed: %v", err)
	}
	if v != core.Atom("ok") {
		t.Fatalf("source macro package result = %#v, want :ok", v)
	}
}

func TestLoadPackageSourceDoesNotExportPrivateBindings(t *testing.T) {
	r := NewRuntime()
	if _, err := r.LoadPackageSource("std/private", "std/private/private.ish", "secret=1\nexport public\npublic=2"); err != nil {
		t.Fatalf("LoadPackageSource failed: %v", err)
	}
	if _, err := r.EvalSource("test", "do\n  import std/private\n  private.secret\nend"); err == nil {
		t.Fatal("private binding was exported")
	}
	v, err := r.EvalSource("test", "do\n  import std/private\n  private.public\nend")
	if err != nil {
		t.Fatalf("public binding was not exported: %v", err)
	}
	if v != core.Int(2) {
		t.Fatalf("public binding = %#v, want 2", v)
	}
}

func TestLoadPackageSourceDoesNotReexportUsedBindings(t *testing.T) {
	r := NewRuntime()
	if _, err := r.LoadPackageSource("std/id", "std/id/id.ish", "export id\ndefn id x do\n  x\nend"); err != nil {
		t.Fatalf("LoadPackageSource id failed: %v", err)
	}
	if _, err := r.LoadPackageSource("std/uses-id", "std/uses-id/use.ish", "use std/id"); err != nil {
		t.Fatalf("LoadPackageSource uses-id failed: %v", err)
	}
	if _, err := r.EvalSource("test", "do\n  import std/uses-id\n  uses-id.id 1\nend"); err == nil {
		t.Fatal("used binding was re-exported")
	}
}

func TestLoadPackageSourceExportsProtocol(t *testing.T) {
	r := NewRuntime()
	if _, err := r.LoadPackageSource("std/math", "std/math/math.ish", "export add\ndefn add x y do\n  42\nend"); err != nil {
		t.Fatalf("LoadPackageSource math failed: %v", err)
	}
	impl := "export math\n" +
		"use std/math\n" +
		"defprotocol math do\n" +
		"  operator + %:precedence 60 %:assoc left -> add\n" +
		"end"
	if _, err := r.LoadPackageSource("std/impl/math", "std/impl/math/math.ish", impl); err != nil {
		t.Fatalf("LoadPackageSource impl failed: %v", err)
	}
	// The operator target `add` resolves in the protocol's provider scope (where
	// std/math was use'd), via the operator's def-scopes — the consumer need not
	// see `add` itself, only implement the protocol.
	v, err := r.EvalSource("test", "do\n  implements std/impl/math\n  1 + 2\nend")
	if err != nil {
		t.Fatalf("EvalSource failed: %v", err)
	}
	if v != core.Int(42) {
		t.Fatalf("source protocol operator = %#v, want 42", v)
	}
}

func TestImportUseDoNotActivateSourceProtocol(t *testing.T) {
	r := NewRuntime()
	impl := "export math\n" +
		"defprotocol math do\n" +
		"  operator | %:precedence 60 %:assoc left -> missing\n" +
		"end"
	if _, err := r.LoadPackageSource("std/impl/math", "std/impl/math/math.ish", impl); err != nil {
		t.Fatalf("LoadPackageSource impl failed: %v", err)
	}
	if _, err := r.EvalSource("test", "do\n  import std/impl/math\n  1 | 2\nend"); err == nil {
		t.Fatal("import activated source protocol")
	}
	if _, err := r.EvalSource("test", "do\n  use std/impl/math\n  1 | 2\nend"); err == nil {
		t.Fatal("use activated source protocol")
	}
}

func TestLoadPackageSourceExportsSelfHostedIfMacro(t *testing.T) {
	r := NewRuntime()
	ifSource := "export if\n" +
		"defmacro if stx -> syntax-case stx [] do\n" +
		"  (if c t e) -> %`(case %,c [:true %,t] [_ %,e])\n" +
		"end"
	if _, err := r.LoadPackageSource("std/base", "std/base/if.ish", ifSource); err != nil {
		t.Fatalf("LoadPackageSource std/base failed: %v", err)
	}
	v, err := r.EvalSource("test", "do\n  use std/base\n  if :false :bad :ok\nend")
	if err != nil {
		t.Fatalf("EvalSource failed: %v", err)
	}
	if v != core.Atom("ok") {
		t.Fatalf("source if macro package result = %#v, want :ok", v)
	}
}
