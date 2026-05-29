package eval

import (
	"reflect"
	"testing"

	"ish/core"
	"ish/expand"
	"ish/reader"
)

func setup() (*expand.Context, *expand.Diagnostics, *Env) {
	tbl := expand.NewBindingTable()
	expand.InstallKernel(tbl)
	InstallRuntimeKernel(tbl)
	diag := &expand.Diagnostics{}
	ctx := expand.NewContext("test", tbl, diag)
	return ctx, diag, NewEnv()
}

func word(s string) *core.Syntax { return &core.Syntax{Node: core.Word(s)} }

func expandAndEval(t *testing.T, src *core.Syntax) Value {
	t.Helper()
	ctx, diag, env := setup()
	out, err := expand.Expand(src, ctx)
	if err != nil {
		t.Fatalf("expand failed: %v (diag: %+v)", err, diag.Items)
	}
	v, err := EvalExpr(out, env)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	return v
}

func readExpandEvalProgram(t *testing.T, source string, configure func(*expand.Context)) Value {
	t.Helper()
	program, err := reader.ReadProgram("test", source)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	ctx, diag, env := setup()
	if configure != nil {
		configure(ctx)
	}
	expanded, err := expand.Expand(program, ctx)
	if err != nil {
		t.Fatalf("expand failed: %v (diag: %+v)", err, diag.Items)
	}
	value, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	return value
}

func TestEvalLiteral(t *testing.T) {
	v := expandAndEval(t, &core.Syntax{Node: core.Int(42)})
	if v != core.Int(42) {
		t.Fatalf("literal eval mismatch: %v", v)
	}
}

func TestEvalDoSequentialAssignment(t *testing.T) {
	assign := core.SyntaxList(core.Span{}, word("%-expr"), word("x"), word("="), &core.Syntax{Node: core.Int(7)})
	value := expandAndEval(t, core.SyntaxList(core.Span{}, word("do"), assign, word("x")))
	if value != core.Int(7) {
		t.Fatalf("do assignment result = %#v, want 7", value)
	}
}

func TestReadExpandEvalDoSequentialAssignment(t *testing.T) {
	forms, err := reader.ReadAll("test", "do\n  x=1\n  x\nend")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if len(forms) != 1 {
		t.Fatalf("forms = %d, want 1", len(forms))
	}
	value := expandAndEval(t, forms[0])
	if value != core.Int(1) {
		t.Fatalf("read/expand/eval assignment result = %#v, want 1", value)
	}
}

func TestReadExpandEvalProgramSequentialAssignment(t *testing.T) {
	value := readExpandEvalProgram(t, "x=1\nx", nil)
	if value != core.Int(1) {
		t.Fatalf("program assignment result = %#v, want 1", value)
	}
}

func TestReadExpandEvalSequentialRebindingShadows(t *testing.T) {
	value := readExpandEvalProgram(t, "x=1\nx=2\nx", nil)
	if value != core.Int(2) {
		t.Fatalf("sequential rebinding result = %#v, want 2", value)
	}
}

func TestReadExpandEvalCompileTimeNameCanBeShadowed(t *testing.T) {
	value := readExpandEvalProgram(t, "import = fn x -> x\nimport 7", nil)
	if value != core.Int(7) {
		t.Fatalf("shadowed import result = %#v, want 7", value)
	}
}

func TestReadExpandEvalEqualsIsMatchBindingSyntaxNotBinding(t *testing.T) {
	value := readExpandEvalProgram(t, "= = fn x -> :bad\nx = 1\nx", nil)
	if value != core.Int(1) {
		t.Fatalf("match-binding result = %#v, want 1", value)
	}
}

func TestReadExpandEvalProgramMacroDefinition(t *testing.T) {
	program, err := reader.ReadProgram("test", "defmacro ok stx -> %':ok\nok")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	ctx, diag, env := setup()
	ctx.Macros = &MacroRunner{Runtime: NewRuntime()}
	expanded, err := expand.Expand(program, ctx)
	if err != nil {
		t.Fatalf("expand failed: %v (diag: %+v)", err, diag.Items)
	}
	value, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	if value != core.Atom("ok") {
		t.Fatalf("program macro result = %#v, want :ok", value)
	}
}

func TestReadExpandEvalDefprotocolImplementsOperator(t *testing.T) {
	value := readExpandEvalProgram(t, "add = fn x y -> 5\ndefprotocol math do\n  operator + %:precedence 60 %:assoc left -> add\nend\nimplements math\n1 + 2", nil)
	if value != core.Int(5) {
		t.Fatalf("defprotocol operator result = %#v, want 5", value)
	}
}

func TestReadExpandEvalFnArrowAssignment(t *testing.T) {
	value := readExpandEvalProgram(t, "id = fn x -> x\nid 7", nil)
	if value != core.Int(7) {
		t.Fatalf("read/expand/eval fn result = %#v, want 7", value)
	}
}

func TestReadExpandEvalFnDoPatternClauses(t *testing.T) {
	value := readExpandEvalProgram(t, "handler = fn do\n  {:ok v} -> v\n  _ -> :error\nend\nhandler {:ok 5}", nil)
	if value != core.Int(5) {
		t.Fatalf("fn do pattern result = %#v, want 5", value)
	}
}

func TestReadExpandEvalDefnBlockDefinition(t *testing.T) {
	value := readExpandEvalProgram(t, "defn id x do\n  x\nend\nid 7", nil)
	if value != core.Int(7) {
		t.Fatalf("defn result = %#v, want 7", value)
	}
}

func TestReadExpandEvalRecursiveDefn(t *testing.T) {
	value := readExpandEvalProgram(t, "defn fact n do\n  case n do\n    0 -> 1\n    _ -> mul n (fact (sub n 1))\n  end\nend\nfact 5", nil)
	if value != core.Int(120) {
		t.Fatalf("recursive defn result = %#v, want 120", value)
	}
}

func TestReadExpandEvalAnonymousFnDoBinding(t *testing.T) {
	value := readExpandEvalProgram(t, "id = fn x do\n  x\nend\nid 7", nil)
	if value != core.Int(7) {
		t.Fatalf("anonymous fn do binding result = %#v, want 7", value)
	}
}

func TestReadExpandEvalPinnedPatternAssignment(t *testing.T) {
	value := readExpandEvalProgram(t, "pid=9\n{:ok ^pid} = {:ok 9}\n:ok", nil)
	if value != core.Atom("ok") {
		t.Fatalf("pinned assignment result = %#v, want :ok", value)
	}
}

func TestReadExpandEvalSurfaceReceiveAfter(t *testing.T) {
	program, err := reader.ReadProgram("test", "receive do\n  :ping -> :matched\nafter 1 -> :timeout\nend")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	ctx, diag, env := setup()
	rt := NewRuntime()
	env.Runtime = rt
	env.Process = rt.NewProcess()
	expanded, err := expand.Expand(program, ctx)
	if err != nil {
		t.Fatalf("expand failed: %v (diag: %+v)", err, diag.Items)
	}
	value, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	if value != core.Atom("timeout") {
		t.Fatalf("receive after result = %#v, want :timeout", value)
	}
}

func TestReadExpandEvalSurfaceReceiveAfterDo(t *testing.T) {
	program, err := reader.ReadProgram("test", "receive do\n  :ping -> :matched\nafter 1 do\n  :timeout\nend\nend")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	ctx, diag, env := setup()
	rt := NewRuntime()
	env.Runtime = rt
	env.Process = rt.NewProcess()
	expanded, err := expand.Expand(program, ctx)
	if err != nil {
		t.Fatalf("expand failed: %v (diag: %+v)", err, diag.Items)
	}
	value, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	if value != core.Atom("timeout") {
		t.Fatalf("receive after do result = %#v, want :timeout", value)
	}
}

func TestReadExpandEvalSurfaceSendSelfReceive(t *testing.T) {
	program, err := reader.ReadProgram("test", "do\n  me = self\n  send me :ping\n  receive do\n    :ping -> :matched\n  after 10 -> :timeout\n  end\nend")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	ctx, diag, env := setup()
	rt := NewRuntime()
	env.Runtime = rt
	env.Process = rt.NewProcess()
	expanded, err := expand.Expand(program, ctx)
	if err != nil {
		t.Fatalf("expand failed: %v (diag: %+v)", err, diag.Items)
	}
	value, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	if value != core.Atom("matched") {
		t.Fatalf("receive result = %#v, want :matched", value)
	}
}

func TestReadExpandEvalSurfaceCaseDo(t *testing.T) {
	forms, err := reader.ReadAll("test", "case {:ok 5} do\n  {:ok v} -> v\n  _ -> :error\nend")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	value := expandAndEval(t, forms[0])
	if value != core.Int(5) {
		t.Fatalf("case result = %#v, want 5", value)
	}
}

func TestReadExpandEvalSurfaceCaseGuard(t *testing.T) {
	value := readExpandEvalProgram(t, "case 5 do\n  x when :false -> :bad\n  x when :true -> x\nend", nil)
	if value != core.Int(5) {
		t.Fatalf("guarded case result = %#v, want 5", value)
	}
}

func TestReadExpandEvalCollectionLiterals(t *testing.T) {
	cases := map[string]core.Datum{
		`[1 2]`:   core.Vector{core.Int(1), core.Int(2)},
		`{:ok 1}`: core.Tuple{core.Atom("ok"), core.Int(1)},
		`%{:a 1}`: core.Dict{{Key: core.Atom("a"), Value: core.Int(1)}},
	}
	for src, want := range cases {
		forms, err := reader.ReadAll("test", src)
		if err != nil {
			t.Fatalf("read %s failed: %v", src, err)
		}
		got := expandAndEval(t, forms[0])
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s = %#v, want %#v", src, got, want)
		}
	}
}

func TestEvalQuotedDatum(t *testing.T) {
	src := core.SyntaxList(core.Span{}, word("quote"), word("foo"))
	v := expandAndEval(t, src)
	if v != core.Word("foo") {
		t.Fatalf("quote eval mismatch: %v", v)
	}
}

func TestReadExpandEvalQuasiquoteUnquote(t *testing.T) {
	value := readExpandEvalProgram(t, "x=1\n`(a ,x)", nil)
	want := core.Pair{Head: core.Word("a"), Tail: core.Pair{Head: core.Int(1), Tail: core.Nil{}}}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("quasiquote result = %#v, want %#v", value, want)
	}
}

func TestReadExpandEvalQuasiquoteUnquoteSplicing(t *testing.T) {
	value := readExpandEvalProgram(t, "xs = '(1 2)\n`(a ,@xs b)", nil)
	want := core.Pair{Head: core.Word("a"), Tail: core.Pair{Head: core.Int(1), Tail: core.Pair{Head: core.Int(2), Tail: core.Pair{Head: core.Word("b"), Tail: core.Nil{}}}}}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("quasiquote splicing result = %#v, want %#v", value, want)
	}
}

func TestEvalRejectsRawPairApplication(t *testing.T) {
	_, _, env := setup()
	raw := core.SyntaxList(core.Span{}, word("quote"), word("x"))
	_, err := EvalExpr(raw, env)
	if err == nil {
		t.Fatal("EvalExpr(raw SyntaxPair) error = nil, want unsupported node")
	}
}

func TestEvalMalformedResolvedErrors(t *testing.T) {
	_, _, env := setup()
	_, err := EvalExpr(&core.Syntax{Node: core.Resolved{Name: "x"}}, env)
	if err == nil {
		t.Fatal("EvalExpr(malformed Resolved) error = nil")
	}
}

// fnClause helps build (fn [[params...] body]) by wrapping a params vector
// and body in the clause-vector shape the expander requires.
func fnClause(params []*core.Syntax, body *core.Syntax) *core.Syntax {
	return &core.Syntax{Node: core.SyntaxVector{
		&core.Syntax{Node: core.SyntaxVector(params)},
		body,
	}}
}

// ((fn [[x] x]) 5) -> 5
func TestEvalIdentityFunction(t *testing.T) {
	fn := core.SyntaxList(core.Span{}, word("fn"), fnClause([]*core.Syntax{word("x")}, word("x")))
	call := core.SyntaxList(core.Span{}, fn, &core.Syntax{Node: core.Int(5)})
	if v := expandAndEval(t, call); v != core.Int(5) {
		t.Fatalf("identity call mismatch: %v", v)
	}
}

// ((fn [[x y] (cons x y)]) 'a 'b) -> (a . b)
func TestEvalCallsKernelPrimitive(t *testing.T) {
	body := core.SyntaxList(core.Span{}, word("cons"), word("x"), word("y"))
	fn := core.SyntaxList(core.Span{}, word("fn"), fnClause([]*core.Syntax{word("x"), word("y")}, body))
	arg1 := core.SyntaxList(core.Span{}, word("quote"), word("a"))
	arg2 := core.SyntaxList(core.Span{}, word("quote"), word("b"))
	call := core.SyntaxList(core.Span{}, fn, arg1, arg2)
	v := expandAndEval(t, call)
	expected := core.Pair{Head: core.Word("a"), Tail: core.Word("b")}
	if !reflect.DeepEqual(v, expected) {
		t.Fatalf("cons call mismatch: got %#v want %#v", v, expected)
	}
}

// Lexical capture: inner closure sees outer parameter.
func TestEvalLexicalCapture(t *testing.T) {
	inner := core.SyntaxList(core.Span{}, word("fn"), fnClause([]*core.Syntax{word("y")}, word("x")))
	innerCall := core.SyntaxList(core.Span{}, inner,
		core.SyntaxList(core.Span{}, word("quote"), word("ignored")))
	outer := core.SyntaxList(core.Span{}, word("fn"), fnClause([]*core.Syntax{word("x")}, innerCall))
	call := core.SyntaxList(core.Span{}, outer,
		core.SyntaxList(core.Span{}, word("quote"), word("captured")))
	if v := expandAndEval(t, call); v != core.Word("captured") {
		t.Fatalf("lexical capture failed: %v", v)
	}
}

// do returns the value of the last form.
func TestEvalDoReturnsLast(t *testing.T) {
	src := core.SyntaxList(core.Span{},
		word("do"),
		core.SyntaxList(core.Span{}, word("quote"), word("a")),
		core.SyntaxList(core.Span{}, word("quote"), word("b")),
		core.SyntaxList(core.Span{}, word("quote"), word("c")),
	)
	v := expandAndEval(t, src)
	if v != core.Word("c") {
		t.Fatalf("do did not yield last: %v", v)
	}
}

// Wildcard pattern matches without binding.
func TestEvalBindWildcard(t *testing.T) {
	src := bindCall(
		core.SyntaxList(core.Span{}, word("quote"), word("ignored")),
		word("_"),
		core.SyntaxList(core.Span{}, word("quote"), word("done")),
	)
	if v := expandAndEval(t, src); v != core.Word("done") {
		t.Fatalf("wildcard bind: %v", v)
	}
}

// Tuple destructure.
func TestEvalBindTupleDestructure(t *testing.T) {
	pattern := &core.Syntax{Node: core.SyntaxTuple{word("a"), word("b")}}
	value := core.SyntaxList(core.Span{}, word("quote"),
		&core.Syntax{Node: core.SyntaxTuple{
			&core.Syntax{Node: core.Int(1)},
			&core.Syntax{Node: core.Int(2)},
		}})
	body := core.SyntaxList(core.Span{}, word("cons"), word("a"), word("b"))
	src := bindCall(value, pattern, body)
	v := expandAndEval(t, src)
	expected := core.Pair{Head: core.Int(1), Tail: core.Int(2)}
	if !reflect.DeepEqual(v, expected) {
		t.Fatalf("tuple destructure: got %#v want %#v", v, expected)
	}
}

// Pattern match failure surfaces as an evaluation error.
func TestEvalBindTuplePatternMismatch(t *testing.T) {
	ctx, _, env := setup()
	pattern := &core.Syntax{Node: core.SyntaxTuple{word("a"), word("b")}}
	value := core.SyntaxList(core.Span{}, word("quote"), word("not-a-tuple"))
	src := bindCall(value, pattern, word("a"))
	expanded, err := expand.Expand(src, ctx)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if _, err := EvalExpr(expanded, env); err == nil {
		t.Fatal("expected pattern mismatch error")
	}
}

// fn with a destructuring parameter pattern.
func TestEvalFnTupleParameter(t *testing.T) {
	tuplePat := &core.Syntax{Node: core.SyntaxTuple{word("a"), word("b")}}
	body := core.SyntaxList(core.Span{}, word("cons"), word("b"), word("a"))
	fn := core.SyntaxList(core.Span{}, word("fn"), fnClause([]*core.Syntax{tuplePat}, body))
	arg := core.SyntaxList(core.Span{}, word("quote"),
		&core.Syntax{Node: core.SyntaxTuple{
			&core.Syntax{Node: core.Atom("first")},
			&core.Syntax{Node: core.Atom("second")},
		}})
	call := core.SyntaxList(core.Span{}, fn, arg)
	v := expandAndEval(t, call)
	expected := core.Pair{Head: core.Atom("second"), Tail: core.Atom("first")}
	if !reflect.DeepEqual(v, expected) {
		t.Fatalf("fn tuple destructure: got %#v want %#v", v, expected)
	}
}

// bindClause builds the (bind value [pattern body]) shape.
func bindCall(value *core.Syntax, pattern *core.Syntax, body *core.Syntax) *core.Syntax {
	clause := &core.Syntax{Node: core.SyntaxVector{pattern, body}}
	return core.SyntaxList(core.Span{}, word("bind"), value, clause)
}

// bind: single-clause match introduces a lexical binding visible in body.
func TestEvalBindSimple(t *testing.T) {
	src := bindCall(
		core.SyntaxList(core.Span{}, word("quote"), word("a")),
		word("x"),
		word("x"),
	)
	if v := expandAndEval(t, src); v != core.Word("a") {
		t.Fatalf("bind body did not see binding: %v", v)
	}
}

// Nested binds compose; inner sees outer.
func TestEvalBindNested(t *testing.T) {
	innerBind := bindCall(
		core.SyntaxList(core.Span{}, word("quote"), word("b")),
		word("y"),
		core.SyntaxList(core.Span{}, word("cons"), word("x"), word("y")),
	)
	src := bindCall(
		core.SyntaxList(core.Span{}, word("quote"), word("a")),
		word("x"),
		innerBind,
	)
	v := expandAndEval(t, src)
	expected := core.Pair{Head: core.Word("a"), Tail: core.Word("b")}
	if !reflect.DeepEqual(v, expected) {
		t.Fatalf("nested bind mismatch: got %#v want %#v", v, expected)
	}
}

// Hygiene through bind: a transformer-introduced reference to `helper`
// does NOT capture a bind-introduced `helper` of the same name at the use site.
func TestBindDoesNotCaptureTransformerReference(t *testing.T) {
	ctx, _, env := setup()
	ctx.Bindings.Define("helper", core.PhaseRuntime, expand.DefaultSpace, core.ScopeSet{}, expand.ValueBinding, core.Atom("global"))

	refHelper := expand.Transformer(func(stx *core.Syntax, _ *expand.Context) (*core.Syntax, error) {
		return word("helper"), nil
	})
	ctx.Bindings.Define("ref-helper", core.PhaseRuntime, expand.DefaultSpace, core.ScopeSet{}, expand.TransformerBinding, &expand.SyntaxTransformer{Fn: refHelper})

	src := bindCall(
		core.SyntaxList(core.Span{}, word("quote"), word("shadow")),
		word("helper"),
		core.SyntaxList(core.Span{}, word("ref-helper")),
	)
	expanded, err := expand.Expand(src, ctx)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	v, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != core.Atom("global") {
		t.Fatalf("bind captured macro reference; expected global, got %v", v)
	}
}

// `syntax` returns a Syntax value whose Node is the literal source syntax.
func TestEvalSyntaxFormReturnsSyntaxValue(t *testing.T) {
	src := core.SyntaxList(core.Span{}, word("syntax"), word("foo"))
	v := expandAndEval(t, src)
	s, ok := v.(*core.Syntax)
	if !ok {
		t.Fatalf("syntax form did not yield *core.Syntax, got %T", v)
	}
	if s.Node != core.Word("foo") {
		t.Fatalf("syntax wrapper does not preserve inner Word: %#v", s.Node)
	}
}

// Multi-clause fn: dispatch by pattern.
func TestEvalFnMultiClauseDispatch(t *testing.T) {
	clauseA := &core.Syntax{Node: core.SyntaxVector{
		&core.Syntax{Node: core.SyntaxVector{&core.Syntax{Node: core.Int(1)}}},
		core.SyntaxList(core.Span{}, word("quote"), word("one")),
	}}
	clauseB := &core.Syntax{Node: core.SyntaxVector{
		&core.Syntax{Node: core.SyntaxVector{&core.Syntax{Node: core.Int(2)}}},
		core.SyntaxList(core.Span{}, word("quote"), word("two")),
	}}
	fn := core.SyntaxList(core.Span{}, word("fn"), clauseA, clauseB)
	one := core.SyntaxList(core.Span{}, fn, &core.Syntax{Node: core.Int(1)})
	two := core.SyntaxList(core.Span{}, fn, &core.Syntax{Node: core.Int(2)})
	if v := expandAndEval(t, one); v != core.Word("one") {
		t.Errorf("clause A: %v", v)
	}
	if v := expandAndEval(t, two); v != core.Word("two") {
		t.Errorf("clause B: %v", v)
	}
}

// Dict pattern: open-style match — required keys must be present with
// matching values; extra keys are permitted.
func TestEvalBindDictPattern(t *testing.T) {
	// Pattern: %{:a x} (literal key :a, var x for value)
	pattern := &core.Syntax{Node: core.SyntaxDict{
		core.SyntaxDictEntry{
			Key:   &core.Syntax{Node: core.Atom("a")},
			Value: word("x"),
		},
	}}
	value := core.SyntaxList(core.Span{}, word("quote"),
		&core.Syntax{Node: core.SyntaxDict{
			core.SyntaxDictEntry{
				Key:   &core.Syntax{Node: core.Atom("a")},
				Value: &core.Syntax{Node: core.Int(1)},
			},
			core.SyntaxDictEntry{
				Key:   &core.Syntax{Node: core.Atom("b")},
				Value: &core.Syntax{Node: core.Int(2)},
			},
		}})
	src := bindCall(value, pattern, word("x"))
	if v := expandAndEval(t, src); v != core.Int(1) {
		t.Fatalf("dict destructure: %v", v)
	}
}

// Dict pattern: missing required key fails the match.
func TestEvalBindDictMissingKeyFails(t *testing.T) {
	ctx, _, env := setup()
	pattern := &core.Syntax{Node: core.SyntaxDict{
		core.SyntaxDictEntry{
			Key:   &core.Syntax{Node: core.Atom("a")},
			Value: word("x"),
		},
	}}
	value := core.SyntaxList(core.Span{}, word("quote"),
		&core.Syntax{Node: core.SyntaxDict{
			core.SyntaxDictEntry{
				Key:   &core.Syntax{Node: core.Atom("b")},
				Value: &core.Syntax{Node: core.Int(2)},
			},
		}})
	src := bindCall(value, pattern, word("x"))
	expanded, err := expand.Expand(src, ctx)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if _, err := EvalExpr(expanded, env); err == nil {
		t.Fatal("expected pattern mismatch for missing key")
	}
}

// Guard: an `fn` clause with `when` runs only when the guard is truthy.
// Multi-clause dispatch falls through to the next clause on guard failure.
func TestEvalFnGuardDispatch(t *testing.T) {
	// (fn [[x] when (lt? x 10) (quote small)] [[x] (quote large)])
	smallGuard := core.SyntaxList(core.Span{}, word("lt?"), word("x"), &core.Syntax{Node: core.Int(10)})
	smallBody := core.SyntaxList(core.Span{}, word("quote"), word("small"))
	smallClause := &core.Syntax{Node: core.SyntaxVector{
		&core.Syntax{Node: core.SyntaxVector{word("x")}},
		word("when"),
		smallGuard,
		smallBody,
	}}
	largeClause := &core.Syntax{Node: core.SyntaxVector{
		&core.Syntax{Node: core.SyntaxVector{word("x")}},
		core.SyntaxList(core.Span{}, word("quote"), word("large")),
	}}
	fn := core.SyntaxList(core.Span{}, word("fn"), smallClause, largeClause)
	if v := expandAndEval(t, core.SyntaxList(core.Span{}, fn, &core.Syntax{Node: core.Int(3)})); v != core.Word("small") {
		t.Fatalf("guard truthy: %v", v)
	}
	if v := expandAndEval(t, core.SyntaxList(core.Span{}, fn, &core.Syntax{Node: core.Int(99)})); v != core.Word("large") {
		t.Fatalf("guard falsy fall-through: %v", v)
	}
}

// Guard on bind: failure raises an error (no fall-through with single clause).
func TestEvalBindGuardFailure(t *testing.T) {
	ctx, _, env := setup()
	guard := core.SyntaxList(core.Span{}, word("gt?"), word("x"), &core.Syntax{Node: core.Int(0)})
	clause := &core.Syntax{Node: core.SyntaxVector{
		word("x"),
		word("when"),
		guard,
		word("x"),
	}}
	src := core.SyntaxList(core.Span{}, word("bind"),
		&core.Syntax{Node: core.Int(-5)},
		clause,
	)
	expanded, err := expand.Expand(src, ctx)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if _, err := EvalExpr(expanded, env); err == nil {
		t.Fatal("expected guard failure to surface as eval error")
	}
}

// match: dispatch a value across clauses, first matching clause wins.
func TestEvalMatchDispatch(t *testing.T) {
	one := &core.Syntax{Node: core.SyntaxVector{
		&core.Syntax{Node: core.Int(1)},
		core.SyntaxList(core.Span{}, word("quote"), word("one")),
	}}
	two := &core.Syntax{Node: core.SyntaxVector{
		&core.Syntax{Node: core.Int(2)},
		core.SyntaxList(core.Span{}, word("quote"), word("two")),
	}}
	other := &core.Syntax{Node: core.SyntaxVector{
		word("_"),
		core.SyntaxList(core.Span{}, word("quote"), word("other")),
	}}
	src := func(n int64) *core.Syntax {
		return core.SyntaxList(core.Span{}, word("match"),
			&core.Syntax{Node: core.Int(n)}, one, two, other)
	}
	if v := expandAndEval(t, src(1)); v != core.Word("one") {
		t.Errorf("match 1: %v", v)
	}
	if v := expandAndEval(t, src(2)); v != core.Word("two") {
		t.Errorf("match 2: %v", v)
	}
	if v := expandAndEval(t, src(99)); v != core.Word("other") {
		t.Errorf("match wildcard: %v", v)
	}
}

// match with no matching clause errors at runtime.
func TestEvalMatchNoClauseFails(t *testing.T) {
	ctx, _, env := setup()
	one := &core.Syntax{Node: core.SyntaxVector{
		&core.Syntax{Node: core.Int(1)},
		core.SyntaxList(core.Span{}, word("quote"), word("one")),
	}}
	src := core.SyntaxList(core.Span{}, word("match"),
		&core.Syntax{Node: core.Int(99)}, one)
	expanded, err := expand.Expand(src, ctx)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if _, err := EvalExpr(expanded, env); err == nil {
		t.Fatal("expected no-clause-matched error")
	}
}

// case is sugar for match — same dispatch behavior.
func TestEvalCaseRewritesToMatch(t *testing.T) {
	one := &core.Syntax{Node: core.SyntaxVector{
		&core.Syntax{Node: core.Int(1)},
		core.SyntaxList(core.Span{}, word("quote"), word("one")),
	}}
	src := core.SyntaxList(core.Span{}, word("case"),
		&core.Syntax{Node: core.Int(1)}, one)
	if v := expandAndEval(t, src); v != core.Word("one") {
		t.Fatalf("case: %v", v)
	}
}

// Dict pattern with duplicate key produces a diagnostic at compile time.
func TestExpandDictPatternDuplicateKey(t *testing.T) {
	ctx, diag, _ := setup()
	pattern := &core.Syntax{Node: core.SyntaxDict{
		core.SyntaxDictEntry{
			Key:   &core.Syntax{Node: core.Atom("k")},
			Value: word("x"),
		},
		core.SyntaxDictEntry{
			Key:   &core.Syntax{Node: core.Atom("k")},
			Value: word("y"),
		},
	}}
	value := core.SyntaxList(core.Span{}, word("quote"),
		&core.Syntax{Node: core.SyntaxDict{}})
	src := bindCall(value, pattern, word("x"))
	if _, err := expand.Expand(src, ctx); err == nil {
		t.Fatal("expected duplicate-key error")
	}
	found := false
	for _, d := range diag.Items {
		if d.Kind == expand.DiagDuplicatePattern {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected DiagDuplicatePattern, got %+v", diag.Items)
	}
}

// Pin pattern: `^x` matches an existing binding's value.
func TestEvalPinPattern(t *testing.T) {
	pin := core.SyntaxList(core.Span{}, word("pin"), word("helper"))
	innerPattern := &core.Syntax{Node: core.SyntaxTuple{pin, word("x")}}
	innerValue := core.SyntaxList(core.Span{}, word("quote"),
		&core.Syntax{Node: core.SyntaxTuple{
			&core.Syntax{Node: core.Atom("h")},
			&core.Syntax{Node: core.Atom("v")},
		}})
	innerBind := bindCall(innerValue, innerPattern, word("x"))
	outer := bindCall(
		core.SyntaxList(core.Span{}, word("quote"), &core.Syntax{Node: core.Atom("h")}),
		word("helper"),
		innerBind,
	)
	if v := expandAndEval(t, outer); v != core.Atom("v") {
		t.Fatalf("pin matched: %v", v)
	}
}

// Pin failure: pinned value doesn't match.
func TestEvalPinPatternMismatch(t *testing.T) {
	ctx, _, env := setup()
	pin := core.SyntaxList(core.Span{}, word("pin"), word("helper"))
	innerPattern := &core.Syntax{Node: core.SyntaxTuple{pin, word("x")}}
	innerValue := core.SyntaxList(core.Span{}, word("quote"),
		&core.Syntax{Node: core.SyntaxTuple{
			&core.Syntax{Node: core.Atom("different")},
			&core.Syntax{Node: core.Atom("v")},
		}})
	innerBind := bindCall(innerValue, innerPattern, word("x"))
	outer := bindCall(
		core.SyntaxList(core.Span{}, word("quote"), &core.Syntax{Node: core.Atom("h")}),
		word("helper"),
		innerBind,
	)
	expanded, err := expand.Expand(outer, ctx)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if _, err := EvalExpr(expanded, env); err == nil {
		t.Fatal("expected pin mismatch error")
	}
}

// Duplicate binding in pattern produces a diagnostic.
func TestExpandDuplicateBindingDiagnoses(t *testing.T) {
	ctx, diag, _ := setup()
	pattern := &core.Syntax{Node: core.SyntaxTuple{word("x"), word("x")}}
	value := core.SyntaxList(core.Span{}, word("quote"),
		&core.Syntax{Node: core.SyntaxTuple{
			&core.Syntax{Node: core.Int(1)}, &core.Syntax{Node: core.Int(2)},
		}})
	src := bindCall(value, pattern, word("x"))
	if _, err := expand.Expand(src, ctx); err == nil {
		t.Fatal("expected expansion failure")
	}
	found := false
	for _, d := range diag.Items {
		if d.Kind == expand.DiagDuplicatePattern {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected DiagDuplicatePattern, got %+v", diag.Items)
	}
}

// Empty do evaluates to Nil.
func TestEvalDoEmpty(t *testing.T) {
	src := core.SyntaxList(core.Span{}, word("do"))
	v := expandAndEval(t, src)
	if v != (core.Nil{}) {
		t.Fatalf("empty do: %v", v)
	}
}

// Arithmetic through expansion + evaluation.
func TestEvalArithmeticPipeline(t *testing.T) {
	src := core.SyntaxList(core.Span{}, word("add"),
		&core.Syntax{Node: core.Int(1)},
		&core.Syntax{Node: core.Int(2)},
		&core.Syntax{Node: core.Int(3)},
	)
	if v := expandAndEval(t, src); v != core.Int(6) {
		t.Fatalf("add 1 2 3: %v", v)
	}
}

// Hygiene + evaluation: a transformer returning a reference to its global
// resolves and evaluates to the global's value, even when the use site has
// a same-named binding bound to a different value.
func TestHygieneEvaluatesToGlobalNotUseSite(t *testing.T) {
	ctx, _, env := setup()
	ctx.Bindings.Define("helper", core.PhaseRuntime, expand.DefaultSpace, core.ScopeSet{}, expand.ValueBinding, core.Atom("global"))
	sUser := core.NewScope()
	ctx.Bindings.Define("helper", core.PhaseRuntime, expand.DefaultSpace, core.ScopeSet{}.Add(sUser), expand.ValueBinding, core.Atom("user"))

	refHelper := expand.Transformer(func(stx *core.Syntax, _ *expand.Context) (*core.Syntax, error) {
		return word("helper"), nil
	})
	ctx.Bindings.Define("ref-helper", core.PhaseRuntime, expand.DefaultSpace, core.ScopeSet{}, expand.TransformerBinding, &expand.SyntaxTransformer{Fn: refHelper})

	stx := core.AddScope(core.SyntaxList(core.Span{}, word("ref-helper")), core.PhaseRuntime, sUser)
	expanded, err := expand.Expand(stx, ctx)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	v, err := EvalExpr(expanded, env)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != core.Atom("global") {
		t.Fatalf("expected global (hygiene preserved through eval), got %v", v)
	}
}
