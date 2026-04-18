package main

import (
	"os"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// evalBinOp
// ---------------------------------------------------------------------------

func TestEvalBinOp(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		want    Value
		wantErr bool
	}{
		// int + int
		{"int add", "3 + 4", IntVal(7), false},
		{"int sub", "10 - 3", IntVal(7), false},
		{"int mul", "6 * 7", IntVal(42), false},
		{"int div", "20 / 4", IntVal(5), false},

		// comparisons int
		{"int eq true", "5 == 5", True, false},
		{"int eq false", "5 == 6", False, false},
		{"int ne true", "5 != 6", True, false},
		{"int ne false", "5 != 5", False, false},
		{"int lt true", "3 < 5", True, false},
		{"int lt false", "5 < 3", False, false},
		{"int gt true", "5 > 3", True, false},
		{"int gt false", "3 > 5", False, false},
		{"int le true", "3 <= 3", True, false},
		{"int ge true", "5 >= 5", True, false},

		// string + string
		{"string concat", `"hello" + " world"`, StringVal("hello world"), false},
		// int + string
		{"int plus string", `42 + " things"`, StringVal("42 things"), false},

		// division by zero
		{"division by zero", "1 / 0", Nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			// Parse as an expression on the RHS of a binding: result = <expr>
			script := "result = " + tt.script
			tokens := Lex(script)
			node, err := Parse(tokens)
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
		want   Value
	}{
		{"negate int", "-42", IntVal(-42)},
		{"negate positive", "-10", IntVal(-10)},
		{"not true", "(!true)", False},
		{"not false", "(!false)", True},
		{"double negate", "-(-5)", IntVal(5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			script := "result = " + tt.script
			tokens := Lex(script)
			node, err := Parse(tokens)
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
// patternBind
// ---------------------------------------------------------------------------

func TestPatternBind(t *testing.T) {
	t.Run("variable binding", func(t *testing.T) {
		env := testEnv()
		pat := wordNode(Token{Type: TWord, Val: "x"})
		err := patternBind(pat, IntVal(42), env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		v, ok := env.Get("x")
		if !ok || !v.Equal(IntVal(42)) {
			t.Errorf("expected x=42, got %v", v)
		}
	})

	t.Run("wildcard _", func(t *testing.T) {
		env := testEnv()
		pat := wordNode(Token{Type: TWord, Val: "_"})
		err := patternBind(pat, IntVal(99), env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// _ should not bind
		if _, ok := env.Get("_"); ok {
			// Some shells set _ but our patternBind should skip it
		}
	})

	t.Run("literal match success", func(t *testing.T) {
		env := testEnv()
		pat := litNode(Token{Type: TInt, Val: "42"})
		err := patternBind(pat, IntVal(42), env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("literal match failure", func(t *testing.T) {
		env := testEnv()
		pat := litNode(Token{Type: TInt, Val: "42"})
		err := patternBind(pat, IntVal(99), env)
		if err == nil {
			t.Error("expected match error for mismatched literal")
		}
	})

	t.Run("tuple destructuring", func(t *testing.T) {
		env := testEnv()
		pat := &Node{Kind: NTuple, Children: []*Node{
			wordNode(Token{Type: TWord, Val: "a"}),
			wordNode(Token{Type: TWord, Val: "b"}),
		}}
		val := TupleVal(IntVal(1), IntVal(2))
		err := patternBind(pat, val, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		a, _ := env.Get("a")
		b, _ := env.Get("b")
		if !a.Equal(IntVal(1)) || !b.Equal(IntVal(2)) {
			t.Errorf("expected a=1, b=2, got a=%s, b=%s", a.Inspect(), b.Inspect())
		}
	})

	t.Run("tuple mismatch - wrong size", func(t *testing.T) {
		env := testEnv()
		pat := &Node{Kind: NTuple, Children: []*Node{
			wordNode(Token{Type: TWord, Val: "a"}),
			wordNode(Token{Type: TWord, Val: "b"}),
		}}
		val := TupleVal(IntVal(1)) // only 1 element
		err := patternBind(pat, val, env)
		if err == nil {
			t.Error("expected error for tuple size mismatch")
		}
	})

	t.Run("list destructuring", func(t *testing.T) {
		env := testEnv()
		pat := &Node{Kind: NList, Children: []*Node{
			wordNode(Token{Type: TWord, Val: "x"}),
			wordNode(Token{Type: TWord, Val: "y"}),
		}}
		val := ListVal(StringVal("hello"), StringVal("world"))
		err := patternBind(pat, val, env)
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
		pat := &Node{Kind: NList, Children: []*Node{
			wordNode(Token{Type: TWord, Val: "x"}),
		}}
		err := patternBind(pat, IntVal(42), env)
		if err == nil {
			t.Error("expected error binding non-list to list pattern")
		}
	})

	t.Run("nested tuple", func(t *testing.T) {
		env := testEnv()
		pat := &Node{Kind: NTuple, Children: []*Node{
			wordNode(Token{Type: TWord, Val: "a"}),
			{Kind: NTuple, Children: []*Node{
				wordNode(Token{Type: TWord, Val: "b"}),
				wordNode(Token{Type: TWord, Val: "c"}),
			}},
		}}
		val := TupleVal(IntVal(1), TupleVal(IntVal(2), IntVal(3)))
		err := patternBind(pat, val, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		a, _ := env.Get("a")
		b, _ := env.Get("b")
		c, _ := env.Get("c")
		if !a.Equal(IntVal(1)) || !b.Equal(IntVal(2)) || !c.Equal(IntVal(3)) {
			t.Errorf("got a=%s, b=%s, c=%s", a.Inspect(), b.Inspect(), c.Inspect())
		}
	})

	t.Run("list head|tail", func(t *testing.T) {
		// [h | t] = [1, 2, 3] -> h=1, t=[2,3]
		env := testEnv()
		pat := &Node{Kind: NList, Children: []*Node{
			wordNode(Token{Type: TWord, Val: "h"}),
		}, Rest: wordNode(Token{Type: TWord, Val: "t"})}
		val := ListVal(IntVal(1), IntVal(2), IntVal(3))
		err := patternBind(pat, val, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h, _ := env.Get("h")
		tVal, _ := env.Get("t")
		if !h.Equal(IntVal(1)) {
			t.Errorf("expected h=1, got h=%s", h.Inspect())
		}
		wantTail := ListVal(IntVal(2), IntVal(3))
		if !tVal.Equal(wantTail) {
			t.Errorf("expected t=[2, 3], got t=%s", tVal.Inspect())
		}
	})

	t.Run("list head|tail multiple heads", func(t *testing.T) {
		// [a, b | rest] = [1, 2, 3, 4] -> a=1, b=2, rest=[3,4]
		env := testEnv()
		pat := &Node{Kind: NList, Children: []*Node{
			wordNode(Token{Type: TWord, Val: "a"}),
			wordNode(Token{Type: TWord, Val: "b"}),
		}, Rest: wordNode(Token{Type: TWord, Val: "rest"})}
		val := ListVal(IntVal(1), IntVal(2), IntVal(3), IntVal(4))
		err := patternBind(pat, val, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		a, _ := env.Get("a")
		b, _ := env.Get("b")
		rest, _ := env.Get("rest")
		if !a.Equal(IntVal(1)) || !b.Equal(IntVal(2)) {
			t.Errorf("expected a=1, b=2, got a=%s, b=%s", a.Inspect(), b.Inspect())
		}
		wantRest := ListVal(IntVal(3), IntVal(4))
		if !rest.Equal(wantRest) {
			t.Errorf("expected rest=[3, 4], got rest=%s", rest.Inspect())
		}
	})

	t.Run("list head|tail empty rest", func(t *testing.T) {
		// [h | t] = [1] -> h=1, t=[]
		env := testEnv()
		pat := &Node{Kind: NList, Children: []*Node{
			wordNode(Token{Type: TWord, Val: "h"}),
		}, Rest: wordNode(Token{Type: TWord, Val: "t"})}
		val := ListVal(IntVal(1))
		err := patternBind(pat, val, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h, _ := env.Get("h")
		tVal, _ := env.Get("t")
		if !h.Equal(IntVal(1)) {
			t.Errorf("expected h=1, got h=%s", h.Inspect())
		}
		wantTail := ListVal()
		if !tVal.Equal(wantTail) {
			t.Errorf("expected t=[], got t=%s", tVal.Inspect())
		}
	})

	t.Run("list head|tail mismatch", func(t *testing.T) {
		// [h | t] = [] -> error
		env := testEnv()
		pat := &Node{Kind: NList, Children: []*Node{
			wordNode(Token{Type: TWord, Val: "h"}),
		}, Rest: wordNode(Token{Type: TWord, Val: "t"})}
		val := ListVal()
		err := patternBind(pat, val, env)
		if err == nil {
			t.Error("expected error for head|tail match on empty list")
		}
	})
}

// ---------------------------------------------------------------------------
// patternMatches
// ---------------------------------------------------------------------------

func TestPatternMatches(t *testing.T) {
	tests := []struct {
		name string
		pat  *Node
		val  Value
		want bool
	}{
		{
			"variable always matches",
			wordNode(Token{Type: TWord, Val: "x"}),
			IntVal(42),
			true,
		},
		{
			"literal match",
			litNode(Token{Type: TInt, Val: "42"}),
			IntVal(42),
			true,
		},
		{
			"literal mismatch",
			litNode(Token{Type: TInt, Val: "42"}),
			IntVal(99),
			false,
		},
		{
			"atom match",
			litNode(Token{Type: TAtom, Val: "ok"}),
			AtomVal("ok"),
			true,
		},
		{
			"atom mismatch",
			litNode(Token{Type: TAtom, Val: "ok"}),
			AtomVal("err"),
			false,
		},
		{
			"tuple match",
			&Node{Kind: NTuple, Children: []*Node{
				litNode(Token{Type: TInt, Val: "1"}),
				wordNode(Token{Type: TWord, Val: "x"}),
			}},
			TupleVal(IntVal(1), IntVal(2)),
			true,
		},
		{
			"tuple size mismatch",
			&Node{Kind: NTuple, Children: []*Node{
				wordNode(Token{Type: TWord, Val: "a"}),
			}},
			TupleVal(IntVal(1), IntVal(2)),
			false,
		},
		{
			"list match",
			&Node{Kind: NList, Children: []*Node{
				wordNode(Token{Type: TWord, Val: "x"}),
			}},
			ListVal(IntVal(1)),
			true,
		},
		{
			"list mismatch - not list",
			&Node{Kind: NList, Children: []*Node{
				wordNode(Token{Type: TWord, Val: "x"}),
			}},
			IntVal(1),
			false,
		},
		{
			"list head|tail matches",
			&Node{Kind: NList, Children: []*Node{
				wordNode(Token{Type: TWord, Val: "h"}),
			}, Rest: wordNode(Token{Type: TWord, Val: "t"})},
			ListVal(IntVal(1), IntVal(2), IntVal(3)),
			true,
		},
		{
			"list head|tail too few elements",
			&Node{Kind: NList, Children: []*Node{
				wordNode(Token{Type: TWord, Val: "h"}),
			}, Rest: wordNode(Token{Type: TWord, Val: "t"})},
			ListVal(),
			false,
		},
		{
			"list head|tail exact match",
			&Node{Kind: NList, Children: []*Node{
				wordNode(Token{Type: TWord, Val: "h"}),
			}, Rest: wordNode(Token{Type: TWord, Val: "t"})},
			ListVal(IntVal(1)),
			true,
		},
		{
			"list head|tail not a list",
			&Node{Kind: NList, Children: []*Node{
				wordNode(Token{Type: TWord, Val: "h"}),
			}, Rest: wordNode(Token{Type: TWord, Val: "t"})},
			IntVal(42),
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			got := patternMatches(tt.pat, tt.val, env)
			if got != tt.want {
				t.Errorf("patternMatches = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// callFn
// ---------------------------------------------------------------------------

func TestCallFn(t *testing.T) {
	t.Run("POSIX-style fn uses positional args", func(t *testing.T) {
		env := testEnv()
		// Define a POSIX fn that echoes $1
		body := &Node{Kind: NCmd, Children: []*Node{
			wordNode(Token{Type: TWord, Val: "echo"}),
			wordNode(Token{Type: TWord, Val: "$1"}),
		}}
		fn := &FnValue{Name: "greet", Clauses: []FnClause{{Body: body}}}

		got := captureOutput(env, func() {
			_, err := callFn(fn, []Value{StringVal("world")}, env)
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
		// fn double x do x * 2 end
		body := &Node{Kind: NBinOp, Tok: Token{Type: TMul, Val: "*"},
			Children: []*Node{
				wordNode(Token{Type: TWord, Val: "x"}),
				litNode(Token{Type: TInt, Val: "2"}),
			},
		}
		fn := &FnValue{Name: "double", Clauses: []FnClause{{
			Params: []Node{*wordNode(Token{Type: TWord, Val: "x"})},
			Body:   body,
		}}}
		val, err := callFn(fn, []Value{IntVal(5)}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !val.Equal(IntVal(10)) {
			t.Errorf("got %s, want 10", val.Inspect())
		}
	})

	t.Run("multi-clause fn", func(t *testing.T) {
		env := testEnv()
		// fn fact 0 do 1 end
		// fn fact n do n * fact(n-1) end
		clause0 := FnClause{
			Params: []Node{*litNode(Token{Type: TInt, Val: "0"})},
			Body:   litNode(Token{Type: TInt, Val: "1"}),
		}
		clause1 := FnClause{
			Params: []Node{*litNode(Token{Type: TInt, Val: "1"})},
			Body:   litNode(Token{Type: TInt, Val: "1"}),
		}
		fn := &FnValue{Name: "base", Clauses: []FnClause{clause0, clause1}}

		// Call with 0 -> should return 1
		val, err := callFn(fn, []Value{IntVal(0)}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !val.Equal(IntVal(1)) {
			t.Errorf("base(0) = %s, want 1", val.Inspect())
		}

		// Call with 1 -> should return 1
		val, err = callFn(fn, []Value{IntVal(1)}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !val.Equal(IntVal(1)) {
			t.Errorf("base(1) = %s, want 1", val.Inspect())
		}
	})

	t.Run("guard", func(t *testing.T) {
		env := testEnv()
		// fn positive n when n > 0 do :yes end
		guard := &Node{Kind: NBinOp, Tok: Token{Type: TRedirOut, Val: ">"},
			Children: []*Node{
				wordNode(Token{Type: TWord, Val: "n"}),
				litNode(Token{Type: TInt, Val: "0"}),
			},
		}
		body := litNode(Token{Type: TAtom, Val: "yes"})
		fn := &FnValue{Name: "positive", Clauses: []FnClause{{
			Params: []Node{*wordNode(Token{Type: TWord, Val: "n"})},
			Guard:  guard,
			Body:   body,
		}}}

		val, err := callFn(fn, []Value{IntVal(5)}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !val.Equal(AtomVal("yes")) {
			t.Errorf("got %s, want :yes", val.Inspect())
		}

		// Negative should fail guard
		_, err = callFn(fn, []Value{IntVal(-1)}, env)
		if err == nil {
			t.Error("expected no matching clause error for negative value")
		}
	})

	t.Run("arity mismatch", func(t *testing.T) {
		env := testEnv()
		fn := &FnValue{Name: "unary", Clauses: []FnClause{{
			Params: []Node{*wordNode(Token{Type: TWord, Val: "x"})},
			Body:   litNode(Token{Type: TInt, Val: "1"}),
		}}}

		_, err := callFn(fn, []Value{IntVal(1), IntVal(2)}, env)
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
		{
			"POSIX if true",
			"if true; then\necho yes\nfi",
			"yes\n",
		},
		{
			"POSIX if false with else",
			"if false; then\necho yes\nelse\necho no\nfi",
			"no\n",
		},
		{
			"ish if true",
			"if true do\necho ok\nend",
			"ok\n",
		},
		{
			"ish if false with else",
			"if false do\necho yes\nelse\necho no\nend",
			"no\n",
		},
		{
			"ish if with expression condition",
			"x = 5\nif x == 5 do\necho match\nend",
			"match\n",
		},
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
	// Unix pipes write to os.Stdout via os.Pipe, so we test via runSource
	// which processes through evalPipe. We use /bin/echo | cat pattern.
	env := testEnv()
	script := "echo hello | cat"

	// evalPipe writes to real os.File pipes, not to env.stdout.
	// We need to capture os.Stdout to test this.
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	env.stdout = w

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
// evalPipeFn (value |> fn)
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
	// Use /bin/echo which writes to cmd.Stdout
	// We redirect via env.stdout by using captureOutput around runSource
	// But evalExternalCmd checks for *os.File, so we test via pipe capture
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	env.stdout = w

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

func TestEvalCmdSubInString(t *testing.T) {
	env := testEnv()
	// Set up cmdSub for string expansion like the real shell does
	env.cmdSub = func(cmd string, e *Env) (string, error) {
		val, err := evalCmdSub(cmd, e)
		if err != nil {
			return "", err
		}
		return val.ToStr(), nil
	}
	got := captureOutput(env, func() {
		runSource(`x = $(echo world)
echo $x`, env)
	})
	if got != "world\n" {
		t.Errorf("got %q, want %q", got, "world\n")
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
	// A quoted argument with glob chars should NOT be expanded
	args := []string{"*.go", "*.go"}
	quoted := []bool{true, false}
	result := expandGlobsSelective(args, quoted)

	// First arg is quoted — must remain literal "*.go"
	if result[0] != "*.go" {
		t.Errorf("quoted arg was expanded: got %q, want %q", result[0], "*.go")
	}
	// Second arg is unquoted — expansion depends on cwd, but function should be called
	// (we just verify it doesn't crash and returns at least one result)
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
// evalSubshell / evalGroup
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
	script := "t = {1, 2, 3}"
	runSource(script, env)
	v, ok := env.Get("t")
	if !ok {
		t.Fatal("t not set")
	}
	if v.Kind != VTuple || len(v.Elems) != 3 {
		t.Errorf("expected 3-tuple, got %s", v.Inspect())
	}
}

func TestEvalList(t *testing.T) {
	env := testEnv()
	script := "l = [10, 20]"
	runSource(script, env)
	v, ok := env.Get("l")
	if !ok {
		t.Fatal("l not set")
	}
	if v.Kind != VList || len(v.Elems) != 2 {
		t.Errorf("expected 2-list, got %s", v.Inspect())
	}
}

func TestEvalMap(t *testing.T) {
	env := testEnv()
	script := `m = %{name: "alice", age: 30}`
	runSource(script, env)
	v, ok := env.Get("m")
	if !ok {
		t.Fatal("m not set")
	}
	if v.Kind != VMap {
		t.Errorf("expected map, got %s", v.Inspect())
	}
}

// ---------------------------------------------------------------------------
// evalAccess (map field access)
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
		script := `fn add a b do
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
	if !boolVal(true).Equal(True) {
		t.Error("boolVal(true) should be True")
	}
	if !boolVal(false).Equal(False) {
		t.Error("boolVal(false) should be False")
	}
}

// ---------------------------------------------------------------------------
// syncExit
// ---------------------------------------------------------------------------

func TestSyncExit(t *testing.T) {
	t.Run("truthy sets 0", func(t *testing.T) {
		env := testEnv()
		env.setExit(1)
		syncExit(IntVal(42), env)
		if env.lastExit != 0 {
			t.Errorf("expected 0, got %d", env.lastExit)
		}
	})

	t.Run("falsy sets 1", func(t *testing.T) {
		env := testEnv()
		env.setExit(0)
		syncExit(IntVal(0), env)
		if env.lastExit != 1 {
			t.Errorf("expected 1, got %d", env.lastExit)
		}
	})

	t.Run("nil does not change exit", func(t *testing.T) {
		env := testEnv()
		env.setExit(42)
		syncExit(Nil, env)
		if env.lastExit != 42 {
			t.Errorf("expected 42, got %d", env.lastExit)
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
// evalIshTry (try/rescue)
// ---------------------------------------------------------------------------

func TestEvalIshTry(t *testing.T) {
	t.Run("try success", func(t *testing.T) {
		env := testEnv()
		script := `result = try do
  42
end`
		got := captureOutput(env, func() {
			runSource(script, env)
		})
		_ = got
		v, ok := env.Get("result")
		if !ok {
			t.Fatal("result not set")
		}
		if !v.Equal(IntVal(42)) {
			t.Errorf("try success: got %s, want 42", v.Inspect())
		}
	})

	t.Run("try with rescue catches error", func(t *testing.T) {
		env := testEnv()
		script := `result = try do
  hd []
rescue
  {:error, msg} -> msg
end`
		runSource(script, env)
		v, ok := env.Get("result")
		if !ok {
			t.Fatal("result not set")
		}
		if v.Kind != VString || v.Str == "" {
			t.Errorf("try/rescue: expected error message string, got %s", v.Inspect())
		}
	})

	t.Run("try without rescue propagates error", func(t *testing.T) {
		env := testEnv()
		script := `result = try do
  hd []
end`
		runSource(script, env)
		// The error should propagate and result should not be set
		_, ok := env.Get("result")
		if ok {
			t.Error("expected result to not be set when no rescue matches")
		}
	})

	t.Run("try does not catch break", func(t *testing.T) {
		env := testEnv()
		// break inside try should propagate out
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
		// alias where expansion starts with same name should not recurse
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
		// Using 'command echo' should bypass the alias and run builtin echo directly
		got := captureOutput(env, func() {
			runSource("command echo direct", env)
		})
		if got != "direct\n" {
			t.Errorf("command bypass alias: got %q, want %q", got, "direct\n")
		}
	})
}
