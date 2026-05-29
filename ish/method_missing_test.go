package ish

import (
	"reflect"
	"testing"

	"ish/core"
)

// This proves the access *lookup itself* is an overloadable protocol, not a
// privileged kernel behavior: a package's qualified access falls through to a
// `missing` handler, where the entire lookup-extension — claim and redirect —
// is authored in ish, in the package, and activated by `implements`.
//
// Crucially, the claim is written over the *generic* expander primitives —
// `dotted-parts`, `space-of`, `resolve` — not a bespoke `package-access-missing?`
// predicate. It claims a dotted access exactly when the member `resolve`s to
// `:unbound` in the base package's space; that is the same `space-of`/`resolve`
// the default access protocol uses to claim *found* members. The two protocols
// partition declined-vs-found accesses with no kernel routing and no
// access-specific surface — the proof the protocol interface is generic.
//
// `missing` resolves back in the provider via cross-package hygiene; the kernel
// routes nothing.
const missingProvider = "export greet\n" +
	"greet = :hello\n" +
	"defn missing name args do\n" +
	"  {:missing name args}\n" +
	"end\n" +
	"defmacro missing-claim? stx do\n" +
	"  unbound-access? = match (dotted-parts stx) do\n" +
	"    :false -> :false\n" +
	"    {base member args chain} -> match (space-of base) do\n" +
	"      :nospace -> :false\n" +
	"      space -> match (resolve member space) do\n" +
	"        :unbound -> :true\n" +
	"        _ -> :false\n" +
	"      end\n" +
	"    end\n" +
	"  end\n" +
	"  datum->syntax stx unbound-access?\n" +
	"end\n" +
	"defmacro missing-redirect stx do\n" +
	"  {base member args chain} = dotted-parts stx\n" +
	"  %`(missing (quote %,member) (vector %,@args))\n" +
	"end\n" +
	"export package-missing\n" +
	"defprotocol package-missing do\n" +
	"  handler expr claims missing-claim? -> missing-redirect\n" +
	"end"

func loadMissingProvider(t *testing.T) *Runtime {
	t.Helper()
	r := NewRuntime()
	if _, err := r.LoadPackageSource("ext/objects", "ext/objects.ish", missingProvider); err != nil {
		t.Fatalf("load provider: %v", err)
	}
	return r
}

func TestQualifiedAccessFallsThroughToMissing(t *testing.T) {
	r := loadMissingProvider(t)
	v, err := r.EvalSource("consumer", "import ext/objects\nimplements ext/objects\nobjects.frobnicate 1 2")
	if err != nil {
		t.Fatalf("consumer errored: %v", err)
	}
	want := core.Tuple{core.Atom("missing"), core.Word("frobnicate"), core.Vector{core.Int(1), core.Int(2)}}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("qualified-access fallthrough = %#v, want %#v", v, want)
	}
}

func TestQualifiedAccessPresentMemberNotIntercepted(t *testing.T) {
	r := loadMissingProvider(t)
	// Even with the missing protocol active, a present member resolves through
	// the default access protocol — the missing lookup declines it.
	v, err := r.EvalSource("consumer", "import ext/objects\nimplements ext/objects\nobjects.greet")
	if err != nil {
		t.Fatalf("consumer errored: %v", err)
	}
	if v != core.Atom("hello") {
		t.Fatalf("present member = %#v, want :hello (must not be intercepted)", v)
	}
}

func TestQualifiedAccessErrorsWithoutMissingProtocol(t *testing.T) {
	r := loadMissingProvider(t)
	// Without `implements`, the default access protocol declines the absent
	// member and nothing else claims it: a plain unbound error, no catch-all.
	if _, err := r.EvalSource("consumer", "import ext/objects\nobjects.frobnicate 1 2"); err == nil {
		t.Fatal("expected error for absent member without a missing protocol")
	}
}

// foundProvider proves the *found* access path is itself an ish-authorable
// protocol over the same generic primitives — the default qualified-access
// behavior holds no privilege the primitives can't reproduce; only its global
// activation is in Go. The claim accepts a dotted access whose member `resolve`s
// to a ref of `binding-kind :value`; the redirect emits that ref with
// `ref->syntax`. Activated in a consumer scope, this protocol dominates the
// global default access (superset scopes) and tags the result so the override is
// observable. It is the worked example for `binding-kind` and `ref->syntax`.
const foundProvider = "export greet\n" +
	"greet = :hello\n" +
	"defmacro found-claim? stx do\n" +
	"  found-value? = match (dotted-parts stx) do\n" +
	"    :false -> :false\n" +
	"    {base member args chain} -> match (space-of base) do\n" +
	"      :nospace -> :false\n" +
	"      space -> match (resolve member space) do\n" +
	"        :unbound -> :false\n" +
	"        :ambiguous -> :false\n" +
	"        ref -> match (binding-kind ref) do\n" +
	"          :value -> :true\n" +
	"          _ -> :false\n" +
	"        end\n" +
	"      end\n" +
	"    end\n" +
	"  end\n" +
	"  datum->syntax stx found-value?\n" +
	"end\n" +
	"defmacro found-redirect stx do\n" +
	"  {base member args chain} = dotted-parts stx\n" +
	"  ref-syntax = ref->syntax (resolve member (space-of base))\n" +
	"  %`{:found %,ref-syntax}\n" +
	"end\n" +
	"export package-found\n" +
	"defprotocol package-found do\n" +
	"  handler expr claims found-claim? -> found-redirect\n" +
	"end"

func TestQualifiedAccessFoundIsAlsoAnIshProtocol(t *testing.T) {
	r := NewRuntime()
	if _, err := r.LoadPackageSource("ext/found", "ext/found.ish", foundProvider); err != nil {
		t.Fatalf("load provider: %v", err)
	}
	// With the found protocol active, `objects.greet` is claimed by the
	// ish-authored protocol (a more specific scope than the global default
	// access) and lowered via ref->syntax into a tagged tuple.
	v, err := r.EvalSource("consumer", "import ext/found\nimplements ext/found\nfound.greet")
	if err != nil {
		t.Fatalf("consumer errored: %v", err)
	}
	want := core.Tuple{core.Atom("found"), core.Atom("hello")}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("found access via ish protocol = %#v, want %#v", v, want)
	}
}

func TestQualifiedAccessFoundDefaultsWhenProtocolInactive(t *testing.T) {
	r := NewRuntime()
	if _, err := r.LoadPackageSource("ext/found", "ext/found.ish", foundProvider); err != nil {
		t.Fatalf("load provider: %v", err)
	}
	// Without `implements`, the global Go default access handles it: the plain
	// value, untagged. This isolates that the tag in the prior test came from the
	// ish protocol, not from anything in the kernel access path.
	v, err := r.EvalSource("consumer", "import ext/found\nfound.greet")
	if err != nil {
		t.Fatalf("consumer errored: %v", err)
	}
	if v != core.Atom("hello") {
		t.Fatalf("default access = %#v, want :hello", v)
	}
}
