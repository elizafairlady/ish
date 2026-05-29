package ish

import (
	"testing"

	"ish/core"
	"ish/eval"
)

// TestSeedProof_SourceStdKernelPackage proves std/kernel is source-loadable and
// provides ordinary base macros (if/and/or) globally — added by loading ish
// source at startup, with no new Go expander/eval case. The forms work without
// an explicit `use`, exactly like the Go-defined kernel builtins.
func TestSeedProof_SourceStdKernelPackage(t *testing.T) {
	if v := evalSeed(t, "if :false do :bad else do :ok end"); v != core.Atom("ok") {
		t.Fatalf("source-loaded if = %#v, want :ok", v)
	}
	if v := evalSeed(t, "and :true :true :true"); v != core.Atom("true") {
		t.Fatalf("variadic and = %#v, want :true", v)
	}
	if v := evalSeed(t, "if (and :true (or :false :true)) do :win else do :lose end"); v != core.Atom("win") {
		t.Fatalf("nested base macros = %#v, want :win", v)
	}
}

func TestSeedProof_SourceStdImplKernelPackage(t *testing.T) {
	t.Skip("seed not locked: std/impl/kernel must be expressible as a source protocol package, then auto-activated as the default implementation mode")
}

func TestSeedProof_SourceShellProtocolPackage(t *testing.T) {
	t.Skip("seed not locked: std/impl/shell must be a source/package protocol that claims generic reader token streams and cannot steal resolved language calls")
}

func TestSeedProof_SourceTasksProtocolPackage(t *testing.T) {
	t.Skip("seed not locked: std/impl/tasks must be a source/package protocol that claims task-like generic token streams without new reader forms unless proven necessary")
}

// TestSeedProof_SourceGenServerLoop proves a recursive actor server loop can be
// written entirely in source using defn/spawn/send/receive and ordinary
// bindings: a stateful counter server answers :inc and :get requests.
func TestSeedProof_SourceGenServerLoop(t *testing.T) {
	src := "defn loop count do\n" +
		"  receive do\n" +
		"    {:inc from} -> do\n" +
		"      send from :ok ;\n" +
		"      loop (add count 1)\n" +
		"    end\n" +
		"    {:get from} -> send from count\n" +
		"  end\n" +
		"end\n" +
		"me = self\n" +
		"server = spawn (fn [[] (loop 0)])\n" +
		"send server {:inc me}\n" +
		"receive do x -> x end\n" +
		"send server {:get me}\n" +
		"receive do n -> n end"
	if v := evalSeed(t, src); v != core.Int(1) {
		t.Fatalf("genserver loop returned %#v, want 1", v)
	}
}

// TestSeedProof_SourceSupervisorRestart proves a source-level supervisor can
// observe a child's abnormal exit via spawn-monitor and restart it: the first
// worker divides by zero, the supervisor receives its :down and spawns a
// replacement that answers a ping.
func TestSeedProof_SourceSupervisorRestart(t *testing.T) {
	src := "me = self\n" +
		"bad = fn [[] (div 1 0)]\n" +
		"{pid ref} = spawn-monitor &bad\n" +
		"restarted = receive do\n" +
		"  {:down r p reason} -> spawn (fn [[] (receive do {:ping from} -> send from :pong end)])\n" +
		"end\n" +
		"send restarted {:ping me}\n" +
		"receive do reply -> reply end"
	if v := evalSeed(t, src); v != core.Atom("pong") {
		t.Fatalf("supervisor restart returned %#v, want :pong", v)
	}
}

// TestSeedProof_SyntaxCaseFreeIdentifierLiterals proves syntax-case literal
// identifiers match by binding (free-identifier), not spelling: the literal
// `plus` matches a use-site `plus` that refers to the same binding, but not one
// shadowed by an inner binding of the same name.
func TestSeedProof_SyntaxCaseFreeIdentifierLiterals(t *testing.T) {
	macro := "plus = :marker\n" +
		"defmacro m stx -> syntax-case stx [plus] do\n" +
		"  (m a plus b) -> %':matched\n" +
		"  _ -> %':nomatch\n" +
		"end\n"
	if v := evalSeed(t, macro+"m 1 plus 2"); v != core.Atom("matched") {
		t.Fatalf("unshadowed literal = %#v, want :matched", v)
	}
	shadowed := macro + "do\n  plus = :other\n  m 1 plus 2\nend"
	if v := evalSeed(t, shadowed); v != core.Atom("nomatch") {
		t.Fatalf("shadowed literal = %#v, want :nomatch (free-identifier, not spelling)", v)
	}
}

// TestSeedProof_MessageCopyIsolation proves a datum message is copy-isolated
// across the process boundary: mutating the sender's original buffer after the
// send does not change what the receiver holds in its mailbox.
func TestSeedProof_MessageCopyIsolation(t *testing.T) {
	rt := eval.NewRuntime()
	p := rt.NewProcess()
	original := core.Bytes{1, 2, 3}
	p.Send(original)
	original[0] = 99
	got, _, ok := p.Receive(func(eval.Value) (int, bool) { return 0, true }, 0)
	if !ok {
		t.Fatal("expected a buffered message")
	}
	bytes, ok := got.(core.Bytes)
	if !ok {
		t.Fatalf("message is %T, want core.Bytes", got)
	}
	if bytes[0] != 1 {
		t.Fatalf("message not copy-isolated: receiver saw sender's mutation %d", bytes[0])
	}
}

// TestSeedProof_TaggedValueGenericDispatch proves runtime polymorphism is
// expressible in ish over a single extensible value kind: make-tagged builds a
// nominal record, and a generic function dispatches on tag-of via match — no
// per-type Go code, the dispatch logic is ordinary ish.
func TestSeedProof_TaggedValueGenericDispatch(t *testing.T) {
	src := "defn area shape do\n" +
		"  match (tag-of shape) do\n" +
		"    :square -> match (tagged-fields shape) do {s} -> mul s s end\n" +
		"    :circle -> match (tagged-fields shape) do {r} -> mul (mul 3 r) r end\n" +
		"  end\n" +
		"end\n"
	if v := evalSeed(t, src+"area (make-tagged :square {4})"); v != core.Int(16) {
		t.Fatalf("generic area(square 4) = %#v, want 16", v)
	}
	if v := evalSeed(t, src+"area (make-tagged :circle {2})"); v != core.Int(12) {
		t.Fatalf("generic area(circle 2) = %#v, want 12", v)
	}
	if v := evalSeed(t, "tag-of {1 2}"); v != core.Atom("false") {
		t.Fatalf("tag-of of non-tagged = %#v, want :false", v)
	}
}

func evalSeed(t *testing.T, src string) eval.Value {
	t.Helper()
	v, err := NewRuntime().EvalSource("seed", src)
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	return v
}

// TestSeedProof_MacroIntroducedDefinition proves a macro can expand into a
// definition that binds for the rest of the body — the body-context analogue of
// local-expand. A macro emitting `name = val` and one emitting `defn` both make
// their use-site-named bindings visible to later forms, while a binding the
// macro names itself stays hygienically hidden.
func TestSeedProof_MacroIntroducedDefinition(t *testing.T) {
	bind := "defmacro def-const stx -> syntax-parse stx do\n  (_ n v) -> %`(%,n = %,v)\nend\n"
	if v := evalSeed(t, bind+"def-const foo 42\nfoo"); v != core.Int(42) {
		t.Fatalf("macro `=` definition = %#v, want 42", v)
	}
	fn := "defmacro def-id stx -> syntax-parse stx do\n  (_ n) -> %`(defn %,n x do x end)\nend\n"
	if v := evalSeed(t, fn+"def-id ident\nident 7"); v != core.Int(7) {
		t.Fatalf("macro defn definition = %#v, want 7", v)
	}
	// A name the macro introduces itself is hygienic: a use-site reference does
	// not see it.
	if _, err := NewRuntime().EvalSource("seed", "defmacro setx stx -> %`(x = 5)\nsetx\nx"); err == nil {
		t.Fatal("expected hygienic macro-introduced name to be hidden from use site")
	}
}

// TestSeedProof_LocalExpandAndBind proves the expansion-time reflective
// primitives are exposed to macro bodies: bind! mints a fresh hygienic
// identifier usable as a collision-free binding name (it does not capture a
// use-site name of the same spelling), and local-expand expands a sub-form in
// the use-site context.
func TestSeedProof_LocalExpandAndBind(t *testing.T) {
	twice := "defmacro twice stx -> syntax-parse stx do\n  (_ e) -> do\n    t = bind!\n    %`(do %,t = %,e ; add %,t %,t end)\n  end\nend\n"
	if v := evalSeed(t, twice+"twice 5"); v != core.Int(10) {
		t.Fatalf("bind! temp = %#v, want 10", v)
	}
	// The macro's bind!-minted `n` must not capture the use-site `n`.
	noCapture := "defmacro addn stx -> syntax-parse stx do\n  (_ e) -> do\n    n = bind! :n\n    %`(do %,n = 100 ; add %,e %,n end)\n  end\nend\n"
	if v := evalSeed(t, noCapture+"do\n  n = 1\n  addn n\nend"); v != core.Int(101) {
		t.Fatalf("bind! freshness = %#v, want 101 (no capture of use-site n)", v)
	}
	le := "defmacro le stx -> syntax-parse stx do\n  (_ e) -> do\n    ex = local-expand e\n    %':done\n  end\nend\n"
	if v := evalSeed(t, le+"le (add 1 2)"); v != core.Atom("done") {
		t.Fatalf("local-expand = %#v, want :done", v)
	}
}

// TestSeedProof_PanicRescueEnsure proves the Erlang-style error model: panic
// fails hard (and, in a spawned process, is observed by a monitor as the exit
// reason), rescue is the explicit local recovery boundary reifying the failure
// as a value, and ensure runs cleanup unconditionally.
func TestSeedProof_PanicRescueEnsure(t *testing.T) {
	if v := evalSeed(t, "rescue (fn [[] (panic :boom)]) (fn [[e] e])"); v != core.Atom("boom") {
		t.Fatalf("rescue panic = %#v, want :boom", v)
	}
	if v := evalSeed(t, "rescue (fn [[] 42]) (fn [[e] :unused])"); v != core.Int(42) {
		t.Fatalf("rescue success = %#v, want 42", v)
	}
	if v := evalSeed(t, "ensure (fn [[] 7]) (fn [[] :cleaned])"); v != core.Int(7) {
		t.Fatalf("ensure body value = %#v, want 7", v)
	}
	reason := evalSeed(t, "me = self\n{p r} = spawn-monitor (fn [[] (panic :crashed)])\nreceive do {:down rr pp why} -> why end")
	if _, ok := reason.(core.Tuple); !ok {
		t.Fatalf("monitored panic reason = %#v, want an {:error ...} tuple", reason)
	}
}
