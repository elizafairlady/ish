package eval

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"ish/internal/ast"
	"ish/internal/builtin"
	"ish/internal/core"
	"ish/internal/debug"
	"ish/internal/lexer"
	"ish/internal/parser"
	"ish/internal/process"
	"ish/internal/stdlib"
)

func testEnv() *core.Env {
	env := core.TopEnv()
	env.Proc = process.NewProcess()
	stdlib.Register(env)
	builtin.Init(builtin.EvalContext{RunSource: RunSource})
	env.CmdSub = RunCmdSub
	env.CallFn = CallFn
	return env
}

func captureOutput(env *core.Env, fn func()) string {
	r, w, _ := os.Pipe()
	env.Stdout_ = w
	fn()
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()
	return buf.String()
}

func runSource(src string, env *core.Env) core.Value {
	return RunSource(src, env)
}

// ---------------------------------------------------------------------------
// evalBinOp
// ---------------------------------------------------------------------------

func TestEvalBinOp(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		want    core.Value
		wantErr bool
	}{
		{"int add", "3 + 4", core.IntVal(7), false},
		{"int sub", "10 - 3", core.IntVal(7), false},
		{"int mul", "6 * 7", core.IntVal(42), false},
		{"int div", "20 / 4", core.IntVal(5), false},
		{"int eq true", "5 == 5", core.True, false},
		{"int eq false", "5 == 6", core.False, false},
		{"int ne true", "5 != 6", core.True, false},
		{"int ne false", "5 != 5", core.False, false},
		{"int lt true", "3 < 5", core.True, false},
		{"int lt false", "5 < 3", core.False, false},
		{"int gt true", "5 > 3", core.True, false},
		{"int gt false", "3 > 5", core.False, false},
		{"int le true", "3 <= 3", core.True, false},
		{"int ge true", "5 >= 5", core.True, false},
		{"string concat", `"hello" + " world"`, core.StringVal("hello world"), false},
		{"int plus string", `42 + " things"`, core.StringVal("42 things"), false},
		{"division by zero", "1 / 0", core.Nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			script := "result = " + tt.script
			node, err := parser.Parse(lexer.New(script))
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			_, err = Eval(node, env)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, ok := env.Get("result")
			if !ok {
				t.Fatal("result not set")
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %s, want %s", got.Inspect(), tt.want.Inspect())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// evalUnary
// ---------------------------------------------------------------------------

func TestEvalUnary(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   core.Value
	}{
		{"negate int", "-42", core.IntVal(-42)},
		{"negate positive", "-10", core.IntVal(-10)},
		{"not true", "(!true)", core.False},
		{"not false", "(!false)", core.True},
		{"double negate", "-(-5)", core.IntVal(5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			script := "result = " + tt.script
			node, err := parser.Parse(lexer.New(script))
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			_, err = Eval(node, env)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, ok := env.Get("result")
			if !ok {
				t.Fatal("result not set")
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %s, want %s", got.Inspect(), tt.want.Inspect())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PatternBind
// ---------------------------------------------------------------------------

func TestPatternBind(t *testing.T) {
	t.Run("variable binding", func(t *testing.T) {
		env := testEnv()
		pat := ast.WordNode(ast.Token{Type: ast.TWord, Val: "x"})
		err := PatternBind(pat, core.IntVal(42), env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		v, ok := env.Get("x")
		if !ok || !v.Equal(core.IntVal(42)) {
			t.Errorf("expected x=42, got %v", v)
		}
	})

	t.Run("wildcard _", func(t *testing.T) {
		env := testEnv()
		pat := ast.WordNode(ast.Token{Type: ast.TWord, Val: "_"})
		err := PatternBind(pat, core.IntVal(99), env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("literal match success", func(t *testing.T) {
		env := testEnv()
		pat := ast.LitNode(ast.Token{Type: ast.TInt, Val: "42"})
		err := PatternBind(pat, core.IntVal(42), env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("literal match failure", func(t *testing.T) {
		env := testEnv()
		pat := ast.LitNode(ast.Token{Type: ast.TInt, Val: "42"})
		err := PatternBind(pat, core.IntVal(99), env)
		if err == nil {
			t.Error("expected match error for mismatched literal")
		}
	})

	t.Run("tuple destructuring", func(t *testing.T) {
		env := testEnv()
		pat := &ast.Node{Kind: ast.NTuple, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "a"}),
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "b"}),
		}}
		val := core.TupleVal(core.IntVal(1), core.IntVal(2))
		err := PatternBind(pat, val, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		a, _ := env.Get("a")
		b, _ := env.Get("b")
		if !a.Equal(core.IntVal(1)) || !b.Equal(core.IntVal(2)) {
			t.Errorf("expected a=1, b=2, got a=%s, b=%s", a.Inspect(), b.Inspect())
		}
	})

	t.Run("tuple mismatch - wrong size", func(t *testing.T) {
		env := testEnv()
		pat := &ast.Node{Kind: ast.NTuple, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "a"}),
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "b"}),
		}}
		val := core.TupleVal(core.IntVal(1))
		err := PatternBind(pat, val, env)
		if err == nil {
			t.Error("expected error for tuple size mismatch")
		}
	})

	t.Run("list destructuring", func(t *testing.T) {
		env := testEnv()
		pat := &ast.Node{Kind: ast.NList, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "x"}),
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "y"}),
		}}
		val := core.ListVal(core.StringVal("hello"), core.StringVal("world"))
		err := PatternBind(pat, val, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		x, _ := env.Get("x")
		y, _ := env.Get("y")
		if x.ToStr() != "hello" || y.ToStr() != "world" {
			t.Errorf("expected x=hello, y=world, got x=%s, y=%s", x.Inspect(), y.Inspect())
		}
	})

	t.Run("list mismatch - not a list", func(t *testing.T) {
		env := testEnv()
		pat := &ast.Node{Kind: ast.NList, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "x"}),
		}}
		err := PatternBind(pat, core.IntVal(42), env)
		if err == nil {
			t.Error("expected error binding non-list to list pattern")
		}
	})

	t.Run("nested tuple", func(t *testing.T) {
		env := testEnv()
		pat := &ast.Node{Kind: ast.NTuple, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "a"}),
			{Kind: ast.NTuple, Children: []*ast.Node{
				ast.WordNode(ast.Token{Type: ast.TWord, Val: "b"}),
				ast.WordNode(ast.Token{Type: ast.TWord, Val: "c"}),
			}},
		}}
		val := core.TupleVal(core.IntVal(1), core.TupleVal(core.IntVal(2), core.IntVal(3)))
		err := PatternBind(pat, val, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		a, _ := env.Get("a")
		b, _ := env.Get("b")
		c, _ := env.Get("c")
		if !a.Equal(core.IntVal(1)) || !b.Equal(core.IntVal(2)) || !c.Equal(core.IntVal(3)) {
			t.Errorf("got a=%s, b=%s, c=%s", a.Inspect(), b.Inspect(), c.Inspect())
		}
	})

	t.Run("list head|tail", func(t *testing.T) {
		env := testEnv()
		pat := &ast.Node{Kind: ast.NList, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "h"}),
		}, Rest: ast.WordNode(ast.Token{Type: ast.TWord, Val: "t"})}
		val := core.ListVal(core.IntVal(1), core.IntVal(2), core.IntVal(3))
		err := PatternBind(pat, val, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h, _ := env.Get("h")
		tVal, _ := env.Get("t")
		if !h.Equal(core.IntVal(1)) {
			t.Errorf("expected h=1, got h=%s", h.Inspect())
		}
		wantTail := core.ListVal(core.IntVal(2), core.IntVal(3))
		if !tVal.Equal(wantTail) {
			t.Errorf("expected t=[2, 3], got t=%s", tVal.Inspect())
		}
	})

	t.Run("list head|tail multiple heads", func(t *testing.T) {
		env := testEnv()
		pat := &ast.Node{Kind: ast.NList, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "a"}),
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "b"}),
		}, Rest: ast.WordNode(ast.Token{Type: ast.TWord, Val: "rest"})}
		val := core.ListVal(core.IntVal(1), core.IntVal(2), core.IntVal(3), core.IntVal(4))
		err := PatternBind(pat, val, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		a, _ := env.Get("a")
		b, _ := env.Get("b")
		rest, _ := env.Get("rest")
		if !a.Equal(core.IntVal(1)) || !b.Equal(core.IntVal(2)) {
			t.Errorf("expected a=1, b=2, got a=%s, b=%s", a.Inspect(), b.Inspect())
		}
		wantRest := core.ListVal(core.IntVal(3), core.IntVal(4))
		if !rest.Equal(wantRest) {
			t.Errorf("expected rest=[3, 4], got rest=%s", rest.Inspect())
		}
	})

	t.Run("list head|tail empty rest", func(t *testing.T) {
		env := testEnv()
		pat := &ast.Node{Kind: ast.NList, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "h"}),
		}, Rest: ast.WordNode(ast.Token{Type: ast.TWord, Val: "t"})}
		val := core.ListVal(core.IntVal(1))
		err := PatternBind(pat, val, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h, _ := env.Get("h")
		tVal, _ := env.Get("t")
		if !h.Equal(core.IntVal(1)) {
			t.Errorf("expected h=1, got h=%s", h.Inspect())
		}
		wantTail := core.ListVal()
		if !tVal.Equal(wantTail) {
			t.Errorf("expected t=[], got t=%s", tVal.Inspect())
		}
	})

	t.Run("list head|tail mismatch", func(t *testing.T) {
		env := testEnv()
		pat := &ast.Node{Kind: ast.NList, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "h"}),
		}, Rest: ast.WordNode(ast.Token{Type: ast.TWord, Val: "t"})}
		val := core.ListVal()
		err := PatternBind(pat, val, env)
		if err == nil {
			t.Error("expected error for head|tail match on empty list")
		}
	})
}

// ---------------------------------------------------------------------------
// PatternMatches
// ---------------------------------------------------------------------------

func TestPatternMatches(t *testing.T) {
	tests := []struct {
		name string
		pat  *ast.Node
		val  core.Value
		want bool
	}{
		{"variable always matches", ast.WordNode(ast.Token{Type: ast.TWord, Val: "x"}), core.IntVal(42), true},
		{"literal match", ast.LitNode(ast.Token{Type: ast.TInt, Val: "42"}), core.IntVal(42), true},
		{"literal mismatch", ast.LitNode(ast.Token{Type: ast.TInt, Val: "42"}), core.IntVal(99), false},
		{"atom match", ast.LitNode(ast.Token{Type: ast.TAtom, Val: "ok"}), core.AtomVal("ok"), true},
		{"atom mismatch", ast.LitNode(ast.Token{Type: ast.TAtom, Val: "ok"}), core.AtomVal("err"), false},
		{"tuple match", &ast.Node{Kind: ast.NTuple, Children: []*ast.Node{
			ast.LitNode(ast.Token{Type: ast.TInt, Val: "1"}),
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "x"}),
		}}, core.TupleVal(core.IntVal(1), core.IntVal(2)), true},
		{"tuple size mismatch", &ast.Node{Kind: ast.NTuple, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "a"}),
		}}, core.TupleVal(core.IntVal(1), core.IntVal(2)), false},
		{"list match", &ast.Node{Kind: ast.NList, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "x"}),
		}}, core.ListVal(core.IntVal(1)), true},
		{"list mismatch - not list", &ast.Node{Kind: ast.NList, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "x"}),
		}}, core.IntVal(1), false},
		{"list head|tail matches", &ast.Node{Kind: ast.NList, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "h"}),
		}, Rest: ast.WordNode(ast.Token{Type: ast.TWord, Val: "t"})}, core.ListVal(core.IntVal(1), core.IntVal(2), core.IntVal(3)), true},
		{"list head|tail too few", &ast.Node{Kind: ast.NList, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "h"}),
		}, Rest: ast.WordNode(ast.Token{Type: ast.TWord, Val: "t"})}, core.ListVal(), false},
		{"list head|tail exact", &ast.Node{Kind: ast.NList, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "h"}),
		}, Rest: ast.WordNode(ast.Token{Type: ast.TWord, Val: "t"})}, core.ListVal(core.IntVal(1)), true},
		{"list head|tail not a list", &ast.Node{Kind: ast.NList, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "h"}),
		}, Rest: ast.WordNode(ast.Token{Type: ast.TWord, Val: "t"})}, core.IntVal(42), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			got := PatternMatches(tt.pat, tt.val, env)
			if got != tt.want {
				t.Errorf("PatternMatches = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CallFn
// ---------------------------------------------------------------------------

func TestCallFn(t *testing.T) {
	t.Run("POSIX-style fn uses positional args", func(t *testing.T) {
		env := testEnv()
		body := &ast.Node{Kind: ast.NCmd, Children: []*ast.Node{
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "echo"}),
			ast.WordNode(ast.Token{Type: ast.TWord, Val: "$1"}),
		}}
		fn := &core.FnValue{Name: "greet", Clauses: []core.FnClause{{Body: body}}}

		got := captureOutput(env, func() {
			_, err := CallFn(fn, []core.Value{core.StringVal("world")}, env)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
		if got != "world\n" {
			t.Errorf("got %q, want %q", got, "world\n")
		}
	})

	t.Run("ish fn with pattern matching", func(t *testing.T) {
		env := testEnv()
		body := &ast.Node{Kind: ast.NBinOp, Tok: ast.Token{Type: ast.TMul, Val: "*"},
			Children: []*ast.Node{
				ast.WordNode(ast.Token{Type: ast.TWord, Val: "x"}),
				ast.LitNode(ast.Token{Type: ast.TInt, Val: "2"}),
			},
		}
		fn := &core.FnValue{Name: "double", Clauses: []core.FnClause{{
			Params: []ast.Node{*ast.WordNode(ast.Token{Type: ast.TWord, Val: "x"})},
			Body:   body,
		}}}
		val, err := CallFn(fn, []core.Value{core.IntVal(5)}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !val.Equal(core.IntVal(10)) {
			t.Errorf("got %s, want 10", val.Inspect())
		}
	})

	t.Run("multi-clause fn", func(t *testing.T) {
		env := testEnv()
		clause0 := core.FnClause{
			Params: []ast.Node{*ast.LitNode(ast.Token{Type: ast.TInt, Val: "0"})},
			Body:   ast.LitNode(ast.Token{Type: ast.TInt, Val: "1"}),
		}
		clause1 := core.FnClause{
			Params: []ast.Node{*ast.LitNode(ast.Token{Type: ast.TInt, Val: "1"})},
			Body:   ast.LitNode(ast.Token{Type: ast.TInt, Val: "1"}),
		}
		fn := &core.FnValue{Name: "base", Clauses: []core.FnClause{clause0, clause1}}

		val, err := CallFn(fn, []core.Value{core.IntVal(0)}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !val.Equal(core.IntVal(1)) {
			t.Errorf("base(0) = %s, want 1", val.Inspect())
		}

		val, err = CallFn(fn, []core.Value{core.IntVal(1)}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !val.Equal(core.IntVal(1)) {
			t.Errorf("base(1) = %s, want 1", val.Inspect())
		}
	})

	t.Run("guard", func(t *testing.T) {
		env := testEnv()
		guard := &ast.Node{Kind: ast.NBinOp, Tok: ast.Token{Type: ast.TRedirOut, Val: ">"},
			Children: []*ast.Node{
				ast.WordNode(ast.Token{Type: ast.TWord, Val: "n"}),
				ast.LitNode(ast.Token{Type: ast.TInt, Val: "0"}),
			},
		}
		body := ast.LitNode(ast.Token{Type: ast.TAtom, Val: "yes"})
		fn := &core.FnValue{Name: "positive", Clauses: []core.FnClause{{
			Params: []ast.Node{*ast.WordNode(ast.Token{Type: ast.TWord, Val: "n"})},
			Guard:  guard,
			Body:   body,
		}}}

		val, err := CallFn(fn, []core.Value{core.IntVal(5)}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !val.Equal(core.AtomVal("yes")) {
			t.Errorf("got %s, want :yes", val.Inspect())
		}

		_, err = CallFn(fn, []core.Value{core.IntVal(-1)}, env)
		if err == nil {
			t.Error("expected no matching clause error for negative value")
		}
	})

	t.Run("arity mismatch", func(t *testing.T) {
		env := testEnv()
		fn := &core.FnValue{Name: "unary", Clauses: []core.FnClause{{
			Params: []ast.Node{*ast.WordNode(ast.Token{Type: ast.TWord, Val: "x"})},
			Body:   ast.LitNode(ast.Token{Type: ast.TInt, Val: "1"}),
		}}}

		_, err := CallFn(fn, []core.Value{core.IntVal(1), core.IntVal(2)}, env)
		if err == nil {
			t.Error("expected error for arity mismatch")
		}
	})
}

// ---------------------------------------------------------------------------
// evalIf
// ---------------------------------------------------------------------------

func TestEvalIf(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{"POSIX if true", "if true; then\necho yes\nfi", "yes\n"},
		{"POSIX if false with else", "if false; then\necho yes\nelse\necho no\nfi", "no\n"},
		{"ish if true", "if true do\necho ok\nend", "ok\n"},
		{"ish if false with else", "if false do\necho yes\nelse\necho no\nend", "no\n"},
		{"ish if with expression condition", "x = 5\nif x == 5 do\necho match\nend", "match\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			got := captureOutput(env, func() {
				runSource(tt.script, env)
			})
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// evalFor
// ---------------------------------------------------------------------------

func TestEvalFor(t *testing.T) {
	t.Run("basic iteration", func(t *testing.T) {
		env := testEnv()
		script := "for i in x y z; do\necho $i\ndone"
		got := captureOutput(env, func() {
			runSource(script, env)
		})
		if got != "x\ny\nz\n" {
			t.Errorf("got %q, want %q", got, "x\ny\nz\n")
		}
	})

	t.Run("break", func(t *testing.T) {
		env := testEnv()
		script := "for i in a b c; do\nif [ $i = b ]; then\nbreak\nfi\necho $i\ndone"
		got := captureOutput(env, func() {
			runSource(script, env)
		})
		if got != "a\n" {
			t.Errorf("got %q, want %q", got, "a\n")
		}
	})

	t.Run("continue", func(t *testing.T) {
		env := testEnv()
		script := "for i in a b c; do\nif [ $i = b ]; then\ncontinue\nfi\necho $i\ndone"
		got := captureOutput(env, func() {
			runSource(script, env)
		})
		if got != "a\nc\n" {
			t.Errorf("got %q, want %q", got, "a\nc\n")
		}
	})
}

// ---------------------------------------------------------------------------
// evalCase
// ---------------------------------------------------------------------------

func TestEvalCase(t *testing.T) {
	t.Run("matching pattern", func(t *testing.T) {
		env := testEnv()
		script := "X=hello\ncase $X in\nhello)\necho matched\n;;\n*)\necho default\n;;\nesac"
		got := captureOutput(env, func() {
			runSource(script, env)
		})
		if got != "matched\n" {
			t.Errorf("got %q, want %q", got, "matched\n")
		}
	})

	t.Run("wildcard fallthrough", func(t *testing.T) {
		env := testEnv()
		script := "X=other\ncase $X in\nhello)\necho matched\n;;\n*)\necho default\n;;\nesac"
		got := captureOutput(env, func() {
			runSource(script, env)
		})
		if got != "default\n" {
			t.Errorf("got %q, want %q", got, "default\n")
		}
	})

	t.Run("no match produces no output", func(t *testing.T) {
		env := testEnv()
		script := "X=other\ncase $X in\nhello)\necho matched\n;;\nesac"
		got := captureOutput(env, func() {
			runSource(script, env)
		})
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

// ---------------------------------------------------------------------------
// evalPipe (Unix pipe)
// ---------------------------------------------------------------------------

func TestEvalPipe(t *testing.T) {
	env := testEnv()
	script := "echo hello | cat"

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	env.Stdout_ = w

	runSource(script, env)

	w.Close()
	os.Stdout = origStdout

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	r.Close()
	got := string(buf[:n])

	if strings.TrimSpace(got) != "hello" {
		t.Errorf("pipe got %q, want %q", got, "hello\n")
	}
}

// ---------------------------------------------------------------------------
// evalPipeFn
// ---------------------------------------------------------------------------

func TestEvalPipeFn(t *testing.T) {
	env := testEnv()
	script := `fn double x do
x * 2
end
fn inc x do
x + 1
end
r = 5 |> double |> inc
echo $r`
	got := captureOutput(env, func() {
		runSource(script, env)
	})
	if got != "11\n" {
		t.Errorf("pipe fn got %q, want %q", got, "11\n")
	}
}

// ---------------------------------------------------------------------------
// evalExternalCmd
// ---------------------------------------------------------------------------

func TestEvalExternalCmd(t *testing.T) {
	env := testEnv()
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	env.Stdout_ = w

	runSource("/bin/echo external_test", env)

	w.Close()
	os.Stdout = origStdout

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	r.Close()
	got := string(buf[:n])

	if strings.TrimSpace(got) != "external_test" {
		t.Errorf("external cmd got %q, want %q", strings.TrimSpace(got), "external_test")
	}
}

// ---------------------------------------------------------------------------
// evalCmdSub
// ---------------------------------------------------------------------------

func TestEvalCmdSub(t *testing.T) {
	env := testEnv()
	got := captureOutput(env, func() {
		runSource("x = $(echo hello)\necho $x", env)
	})
	if got != "hello\n" {
		t.Errorf("command sub got %q, want %q", got, "hello\n")
	}
}

// ---------------------------------------------------------------------------
// evalPosixAssign
// ---------------------------------------------------------------------------

func TestEvalPosixAssign(t *testing.T) {
	tests := []struct {
		name   string
		script string
		varN   string
		want   string
	}{
		{"simple assign", "X=42", "X", "42"},
		{"assign with quotes", `Y="hello world"`, "Y", "hello world"},
		{"assign with expansion", "A=base\nB=$A/sub", "B", "base/sub"},
		{"empty value", "Z=", "Z", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			runSource(tt.script, env)
			v, ok := env.Get(tt.varN)
			if !ok {
				t.Fatalf("variable %s not set", tt.varN)
			}
			if v.ToStr() != tt.want {
				t.Errorf("%s = %q, want %q", tt.varN, v.ToStr(), tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// stripAssignQuotes
// ---------------------------------------------------------------------------

func TestStripAssignQuotes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"double quoted", `"hello"`, "hello"},
		{"single quoted", `'hello'`, "hello"},
		{"mixed double+single", `"hello"'world'`, "helloworld"},
		{"unquoted", `plain`, "plain"},
		{"double with escape", `"a\"b"`, `a"b`},
		{"single literal backslash", `'a\b'`, `a\b`},
		{"mixed unquoted+quoted", `pre"mid"post`, "premidpost"},
		{"empty double", `""`, ""},
		{"empty single", `''`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripAssignQuotes(tt.input)
			if got != tt.want {
				t.Errorf("stripAssignQuotes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// expandGlobsSelective
// ---------------------------------------------------------------------------

func TestExpandGlobsSelective(t *testing.T) {
	args := []string{"*.go", "*.go"}
	quoted := []bool{true, false}
	result := expandGlobsSelective(args, quoted)

	if result[0] != "*.go" {
		t.Errorf("quoted arg was expanded: got %q, want %q", result[0], "*.go")
	}
	if len(result) < 2 {
		t.Errorf("expected at least 2 results, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// evalIshMatch expression
// ---------------------------------------------------------------------------

func TestEvalIshMatch(t *testing.T) {
	t.Run("match on integer", func(t *testing.T) {
		env := testEnv()
		script := `x = 2
r = match x do
1 -> :one
2 -> :two
_ -> :other
end
echo $r`
		got := captureOutput(env, func() {
			runSource(script, env)
		})
		if got != ":two\n" {
			t.Errorf("got %q, want %q", got, ":two\n")
		}
	})

	t.Run("match wildcard", func(t *testing.T) {
		env := testEnv()
		script := `x = 99
r = match x do
1 -> :one
_ -> :wildcard
end
echo $r`
		got := captureOutput(env, func() {
			runSource(script, env)
		})
		if got != ":wildcard\n" {
			t.Errorf("got %q, want %q", got, ":wildcard\n")
		}
	})

	t.Run("match on atom", func(t *testing.T) {
		env := testEnv()
		script := `x = :ok
r = match x do
:ok -> :success
:err -> :failure
end
echo $r`
		got := captureOutput(env, func() {
			runSource(script, env)
		})
		if got != ":success\n" {
			t.Errorf("got %q, want %q", got, ":success\n")
		}
	})
}

// ---------------------------------------------------------------------------
// evalAndList / evalOrList
// ---------------------------------------------------------------------------

func TestEvalAndOrList(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{"and both true", "true && echo yes", "yes\n"},
		{"and first false", "false && echo yes", ""},
		{"or first false", "false || echo fallback", "fallback\n"},
		{"or first true", "true || echo fallback", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			got := captureOutput(env, func() {
				runSource(tt.script, env)
			})
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// evalSubshell
// ---------------------------------------------------------------------------

func TestEvalSubshell(t *testing.T) {
	env := testEnv()
	got := captureOutput(env, func() {
		runSource("(echo subshell_test)", env)
	})
	if got != "subshell_test\n" {
		t.Errorf("got %q, want %q", got, "subshell_test\n")
	}
}

// ---------------------------------------------------------------------------
// evalTuple / evalList / evalMap
// ---------------------------------------------------------------------------

func TestEvalTuple(t *testing.T) {
	env := testEnv()
	runSource("t = {1, 2, 3}", env)
	v, ok := env.Get("t")
	if !ok {
		t.Fatal("t not set")
	}
	if v.Kind != core.VTuple || len(v.Elems) != 3 {
		t.Errorf("expected 3-tuple, got %s", v.Inspect())
	}
}

func TestEvalList(t *testing.T) {
	env := testEnv()
	runSource("l = [10, 20]", env)
	v, ok := env.Get("l")
	if !ok {
		t.Fatal("l not set")
	}
	if v.Kind != core.VList || len(v.Elems) != 2 {
		t.Errorf("expected 2-list, got %s", v.Inspect())
	}
}

func TestEvalMap(t *testing.T) {
	env := testEnv()
	runSource(`m = %{name: "alice", age: 30}`, env)
	v, ok := env.Get("m")
	if !ok {
		t.Fatal("m not set")
	}
	if v.Kind != core.VMap {
		t.Errorf("expected map, got %s", v.Inspect())
	}
}

// ---------------------------------------------------------------------------
// evalAccess
// ---------------------------------------------------------------------------

func TestEvalAccess(t *testing.T) {
	env := testEnv()
	script := `m = %{x: 10, y: 20}
r = m.x
echo $r`
	got := captureOutput(env, func() {
		runSource(script, env)
	})
	if got != "10\n" {
		t.Errorf("got %q, want %q", got, "10\n")
	}
}

// ---------------------------------------------------------------------------
// evalWhileUntil
// ---------------------------------------------------------------------------

func TestEvalWhile(t *testing.T) {
	env := testEnv()
	script := `n = 3
while [ $n -gt 0 ]; do
echo $n
n = (n - 1)
done`
	got := captureOutput(env, func() {
		runSource(script, env)
	})
	if got != "3\n2\n1\n" {
		t.Errorf("got %q, want %q", got, "3\n2\n1\n")
	}
}

// ---------------------------------------------------------------------------
// evalPosixFnDef
// ---------------------------------------------------------------------------

func TestEvalPosixFnDef(t *testing.T) {
	env := testEnv()
	script := "greet() { echo hi; }\ngreet"
	got := captureOutput(env, func() {
		runSource(script, env)
	})
	if got != "hi\n" {
		t.Errorf("got %q, want %q", got, "hi\n")
	}
}

// ---------------------------------------------------------------------------
// evalIshFn
// ---------------------------------------------------------------------------

func TestEvalIshFn(t *testing.T) {
	t.Run("simple fn", func(t *testing.T) {
		env := testEnv()
		script := `fn add a, b do
a + b
end
r = add 3, 4
echo $r`
		got := captureOutput(env, func() {
			runSource(script, env)
		})
		if got != "7\n" {
			t.Errorf("got %q, want %q", got, "7\n")
		}
	})

	t.Run("multi-clause with guard", func(t *testing.T) {
		env := testEnv()
		script := `fn abs n when n < 0 do
0 - n
end
fn abs n do
n
end
r = abs (-3)
echo $r`
		got := captureOutput(env, func() {
			runSource(script, env)
		})
		if got != "3\n" {
			t.Errorf("got %q, want %q", got, "3\n")
		}
	})
}

// ---------------------------------------------------------------------------
// boolVal helper
// ---------------------------------------------------------------------------

func TestBoolVal(t *testing.T) {
	if !core.BoolVal(true).Equal(core.True) {
		t.Error("BoolVal(true) should be True")
	}
	if !core.BoolVal(false).Equal(core.False) {
		t.Error("BoolVal(false) should be False")
	}
}

// ---------------------------------------------------------------------------
// syncExit
// ---------------------------------------------------------------------------

func TestSyncExit(t *testing.T) {
	t.Run("truthy sets 0", func(t *testing.T) {
		env := testEnv()
		env.SetExit(1)
		syncExit(core.IntVal(42), env)
		if env.ExitCode() != 0 {
			t.Errorf("expected 0, got %d", env.ExitCode())
		}
	})

	t.Run("falsy sets 1", func(t *testing.T) {
		env := testEnv()
		env.SetExit(0)
		syncExit(core.IntVal(0), env)
		if env.ExitCode() != 1 {
			t.Errorf("expected 1, got %d", env.ExitCode())
		}
	})

	t.Run("nil does not change exit", func(t *testing.T) {
		env := testEnv()
		env.SetExit(42)
		syncExit(core.Nil, env)
		if env.ExitCode() != 42 {
			t.Errorf("expected 42, got %d", env.ExitCode())
		}
	})
}

// ---------------------------------------------------------------------------
// matchPattern (used in evalCase)
// ---------------------------------------------------------------------------

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern string
		s       string
		want    bool
	}{
		{"*", "anything", true},
		{"hello", "hello", true},
		{"hello", "world", false},
		{"h*", "hello", true},
		{"h*", "world", false},
		{"*.go", "main.go", true},
		{"*.go", "main.rs", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.s, func(t *testing.T) {
			got := matchPattern(tt.pattern, tt.s)
			if got != tt.want {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.s, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// evalIshTry
// ---------------------------------------------------------------------------

func TestEvalIshTry(t *testing.T) {
	t.Run("try success", func(t *testing.T) {
		env := testEnv()
		script := `result = try do
  42
end`
		captureOutput(env, func() {
			runSource(script, env)
		})
		v, ok := env.Get("result")
		if !ok {
			t.Fatal("result not set")
		}
		if !v.Equal(core.IntVal(42)) {
			t.Errorf("try success: got %s, want 42", v.Inspect())
		}
	})

	t.Run("try with rescue catches error", func(t *testing.T) {
		env := testEnv()
		script := `result = try do
  List.hd []
rescue
  {:error, msg} -> msg
end`
		runSource(script, env)
		v, ok := env.Get("result")
		if !ok {
			t.Fatal("result not set")
		}
		if v.Kind != core.VString || v.Str == "" {
			t.Errorf("try/rescue: expected error message string, got %s", v.Inspect())
		}
	})

	t.Run("try without rescue propagates error", func(t *testing.T) {
		env := testEnv()
		script := `result = try do
  List.hd []
end`
		runSource(script, env)
		_, ok := env.Get("result")
		if ok {
			t.Error("expected result to not be set when no rescue matches")
		}
	})

	t.Run("try does not catch break", func(t *testing.T) {
		env := testEnv()
		script := `for i in a b c; do
try do
  if [ $i = b ]; then
    break
  fi
  echo $i
end
done`
		got := captureOutput(env, func() {
			runSource(script, env)
		})
		if got != "a\n" {
			t.Errorf("try should not catch break: got %q, want %q", got, "a\n")
		}
	})
}

// ---------------------------------------------------------------------------
// alias expansion in evalCmd
// ---------------------------------------------------------------------------

func TestAliasExpansion(t *testing.T) {
	t.Run("basic alias expansion", func(t *testing.T) {
		env := testEnv()
		env.SetAlias("greet", "echo hello")
		got := captureOutput(env, func() {
			runSource("greet", env)
		})
		if got != "hello\n" {
			t.Errorf("alias greet: got %q, want %q", got, "hello\n")
		}
	})

	t.Run("alias with args", func(t *testing.T) {
		env := testEnv()
		env.SetAlias("ll", "echo listing")
		got := captureOutput(env, func() {
			runSource("ll foo bar", env)
		})
		if got != "listing foo bar\n" {
			t.Errorf("alias ll with args: got %q, want %q", got, "listing foo bar\n")
		}
	})

	t.Run("alias avoids infinite recursion", func(t *testing.T) {
		env := testEnv()
		env.SetAlias("echo", "echo")
		got := captureOutput(env, func() {
			runSource("echo safe", env)
		})
		if got != "safe\n" {
			t.Errorf("self-referencing alias: got %q, want %q", got, "safe\n")
		}
	})

	t.Run("command bypasses alias", func(t *testing.T) {
		env := testEnv()
		env.SetAlias("echo", "echo aliased")
		got := captureOutput(env, func() {
			runSource("command echo direct", env)
		})
		if got != "direct\n" {
			t.Errorf("command bypass alias: got %q, want %q", got, "direct\n")
		}
	})
}

func TestTailCallOptimization(t *testing.T) {
	t.Run("self-recursion does not overflow", func(t *testing.T) {
		env := testEnv()
		src := `
fn countdown n do
  if n == 0 do
    :done
  else
    countdown (n - 1)
  end
end
result = countdown 100000
`
		node, err := parser.Parse(lexer.New(src))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Eval(node, env)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := env.Get("result")
		if got.Kind != core.VAtom || got.Str != "done" {
			t.Errorf("got %s, want :done", got.Inspect())
		}
	})

	t.Run("mutual recursion", func(t *testing.T) {
		env := testEnv()
		src := `
fn is_even n do
  if n == 0 do
    :true
  else
    is_odd (n - 1)
  end
end
fn is_odd n do
  if n == 0 do
    :false
  else
    is_even (n - 1)
  end
end
result = is_even 100000
`
		node, err := parser.Parse(lexer.New(src))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Eval(node, env)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := env.Get("result")
		if got.Kind != core.VAtom || got.Str != "true" {
			t.Errorf("got %s, want :true", got.Inspect())
		}
	})
}

func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()
	os.Stderr = old
	return buf.String()
}

func TestSetXTracesExternalCommands(t *testing.T) {
	env := testEnv()
	stderr := captureStderr(func() {
		RunSource(`set -x; echo hello`, env)
	})
	if !strings.Contains(stderr, "+ echo hello") {
		t.Errorf("set -x should trace external commands, got: %q", stderr)
	}
	// set -x should NOT produce [file:line:col] trace lines
	if strings.Contains(stderr, "[") {
		t.Errorf("set -x should not produce ish-level traces, got: %q", stderr)
	}
}

func TestSetXUpperTracesEverything(t *testing.T) {
	env := testEnv()
	stderr := captureStderr(func() {
		RunSource(`set -X; echo hello`, env)
	})
	// Should include ish-level position-tagged trace
	if !strings.Contains(stderr, "echo hello") {
		t.Errorf("set -X should trace ish nodes, got: %q", stderr)
	}
	// Should NOT produce POSIX xtrace output (no implied -x)
	lines := strings.Split(stderr, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "+ ") && !strings.Contains(line, "[") {
			t.Errorf("set -X should not produce POSIX xtrace output; use set -xX for both; got line: %q", line)
		}
	}
}

func TestSetXUpperShowsFunctionDispatch(t *testing.T) {
	env := testEnv()
	stderr := captureStderr(func() {
		RunSource("fn greet name do\n  echo \"hello $name\"\nend\nset -X\ngreet world", env)
	})
	// Should show the ish function call as a position-tagged trace
	if !strings.Contains(stderr, "greet world") {
		t.Errorf("set -X should show function dispatch, got: %q", stderr)
	}
	// Should NOT produce POSIX xtrace lines (no implied -x)
	lines := strings.Split(stderr, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "+ ") && !strings.Contains(line, "[") {
			t.Errorf("set -X alone should not produce POSIX xtrace; use set -xX for both; got line: %q", line)
		}
	}
}

func TestSetXLowerAndUpperProducesBoth(t *testing.T) {
	env := testEnv()
	stderr := captureStderr(func() {
		RunSource(`set -xX; echo hello`, env)
	})
	// Should have ish-level position-tagged trace
	if !strings.Contains(stderr, "[") {
		t.Errorf("set -xX should produce ish-level traces, got: %q", stderr)
	}
	// Should also have POSIX xtrace output
	hasPosix := false
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "+ ") && !strings.Contains(line, "[") {
			hasPosix = true
			break
		}
	}
	if !hasPosix {
		t.Errorf("set -xX should produce POSIX xtrace output, got: %q", stderr)
	}
}

func TestSetXDoesNotImplyX(t *testing.T) {
	// set -x alone should not produce position-tagged traces
	env := testEnv()
	stderr := captureStderr(func() {
		RunSource("fn greet name do\n  echo \"hello $name\"\nend\nset -x\ngreet world", env)
	})
	// Should show the expanded command
	if !strings.Contains(stderr, "+ echo hello world") {
		t.Errorf("set -x should trace echo, got: %q", stderr)
	}
	// Should NOT show ish-level function dispatch
	if strings.Contains(stderr, "greet world") && strings.Contains(stderr, "[") {
		t.Errorf("set -x should not produce ish-level traces, got: %q", stderr)
	}
}

func TestDebuggerStackTrace(t *testing.T) {
	env := testEnv()
	env.Debugger = debug.New()
	stderr := captureStderr(func() {
		RunSource("fn add a b do\n  a + b\nend\nadd 1 :bad", env)
	})
	if !strings.Contains(stderr, "add/2") {
		t.Errorf("stack trace should show add/2, got: %q", stderr)
	}
}

func TestCommandVsExprInFnBody(t *testing.T) {
	env := testEnv()

	// head -1 should work as a command inside fn body
	out := captureOutput(env, func() {
		RunSource("fn first_line f do\n  head -1 \"$f\"\nend\nfirst_line ../../examples/closures.ish", env)
	})
	if !strings.Contains(out, "Closures") {
		t.Errorf("head -1 inside fn body should work as command, got: %q", out)
	}

	// x - 5 should work as expression inside fn body
	env2 := testEnv()
	out2 := captureOutput(env2, func() {
		RunSource("fn sub x do\n  x - 5\nend\necho $(sub 10)", env2)
	})
	if !strings.Contains(out2, "5") {
		t.Errorf("x - 5 inside fn body should work as expression, got: %q", out2)
	}
}
