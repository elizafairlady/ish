package ish

import (
	"testing"

	"ish/core"
)

// `export <definition>` fuses a definition with its export: `export defn f …`,
// `export x = …`, `export defmacro …`, `export defprotocol …`. It is equivalent
// to a separate `export name` plus the definition.
func TestExportDefinitionPrefix(t *testing.T) {
	r := NewRuntime()
	provider := "export defn inc x do\n" +
		"  add x 1\n" +
		"end\n" +
		"export greeting = :hello"
	if _, err := r.LoadPackageSource("p", "p/p.ish", provider); err != nil {
		t.Fatalf("load provider: %v", err)
	}
	v, err := r.EvalSource("c", "use p\ninc 41")
	if err != nil || v != core.Int(42) {
		t.Fatalf("export defn then use = %#v err=%v, want 42", v, err)
	}
	v, err = r.EvalSource("c2", "use p\ngreeting")
	if err != nil || v != core.Atom("hello") {
		t.Fatalf("export binding then use = %#v err=%v, want :hello", v, err)
	}
}

// A name not exported is not visible to a consumer (export prefix only exports
// the name it precedes).
func TestExportPrefixDoesNotLeakSiblings(t *testing.T) {
	r := NewRuntime()
	provider := "export defn shown do\n  :shown\nend\n" +
		"defn hidden do\n  :hidden\nend"
	if _, err := r.LoadPackageSource("q", "q/q.ish", provider); err != nil {
		t.Fatalf("load provider: %v", err)
	}
	if _, err := r.EvalSource("c", "use q\nhidden"); err == nil {
		t.Fatal("hidden (unexported) should not be visible after use")
	}
}

// `implements protocol do … end` activates an anonymous protocol inline, in the
// current scope — no name, no separate defprotocol+implements.
func TestImplementsInlineAnonymousProtocol(t *testing.T) {
	src := "defn dec x do\n  sub x 1\nend\n" +
		"implements protocol do\n" +
		"  operator ~ %:precedence 80 %:fixity prefix -> dec\n" +
		"end\n" +
		"~ 5"
	if v, err := NewRuntime().EvalSource("m", src); err != nil || v != core.Int(4) {
		t.Fatalf("implements protocol inline = %#v err=%v, want 4", v, err)
	}
}

// `implements defprotocol NAME do … end` both binds NAME and activates it.
func TestImplementsDefprotocolInline(t *testing.T) {
	src := "defn dec x do\n  sub x 1\nend\n" +
		"implements defprotocol decrementer do\n" +
		"  operator ~ %:precedence 80 %:fixity prefix -> dec\n" +
		"end\n" +
		"~ 9"
	if v, err := NewRuntime().EvalSource("m", src); err != nil || v != core.Int(8) {
		t.Fatalf("implements defprotocol inline = %#v err=%v, want 8", v, err)
	}
}
