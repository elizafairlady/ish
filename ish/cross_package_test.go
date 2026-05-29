package ish

import (
	"reflect"
	"testing"

	"ish/core"
)

// These tests exercise cross-package hygiene: a macro or protocol authored in
// one package, used/implemented in a separate consumer package, must resolve
// its own provider-private helpers. This works because all packages share one
// binding table (isolated by per-package scopes) and one eval env (keyed by
// globally-unique binding id), so a reference introduced by a provider's
// transformer resolves by scope in the shared table and finds its value in the
// shared env — while staying invisible to consumers unqualified.

func TestCrossPackageMacroReferencesProviderHelper(t *testing.T) {
	r := NewRuntime()
	provider := "export greet\n" +
		"defn helper x do\n  {:helped x}\nend\n" +
		"defmacro greet stx -> syntax-parse stx do\n  (_ x) -> %`(helper %,x)\nend"
	if _, err := r.LoadPackageSource("ext/greeter", "ext/greeter.ish", provider); err != nil {
		t.Fatalf("load provider: %v", err)
	}
	v, err := r.EvalSource("consumer", "use ext/greeter\ngreet 5")
	if err != nil {
		t.Fatalf("consumer errored: %v", err)
	}
	want := core.Tuple{core.Atom("helped"), core.Int(5)}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("cross-package macro result = %#v, want %#v", v, want)
	}
}

func TestCrossPackageNonExportedHelperStaysIsolated(t *testing.T) {
	r := NewRuntime()
	provider := "export greet\n" +
		"defn helper x do\n  {:helped x}\nend\n" +
		"defmacro greet stx -> syntax-parse stx do\n  (_ x) -> %`(helper %,x)\nend"
	if _, err := r.LoadPackageSource("ext/greeter", "ext/greeter.ish", provider); err != nil {
		t.Fatalf("load provider: %v", err)
	}
	// `helper` is not exported, so it must not be reachable unqualified even
	// after `use`; only the shared scopes the macro carries can reach it.
	if _, err := r.EvalSource("consumer", "use ext/greeter\nhelper 9"); err == nil {
		t.Fatal("expected non-exported helper to be unbound in consumer")
	}
}
