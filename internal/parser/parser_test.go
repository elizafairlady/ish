package parser

import (
	"strings"
	"testing"

	"ish/internal/ast"
	"ish/internal/lexer"
)

// parseStr is a helper that lexes and parses a string in one step.
func parseStr(input string) (*ast.Node, error) {
	return Parse(lexer.New(input))
}

// ---------------------------------------------------------------------------
// Simple commands
// ---------------------------------------------------------------------------

func TestParseSimpleCommand(t *testing.T) {
	node, err := parseStr("echo hello world")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NCmd {
		t.Fatalf("expected NCmd, got %d", node.Kind)
	}
	if len(node.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(node.Children))
	}
	if node.Children[0].Tok.Val != "echo" {
		t.Errorf("child[0] = %q, want %q", node.Children[0].Tok.Val, "echo")
	}
	if node.Children[1].Tok.Val != "hello" {
		t.Errorf("child[1] = %q, want %q", node.Children[1].Tok.Val, "hello")
	}
	if node.Children[2].Tok.Val != "world" {
		t.Errorf("child[2] = %q, want %q", node.Children[2].Tok.Val, "world")
	}
}

func TestParseCommandWithFlags(t *testing.T) {
	node, err := parseStr("ls -la /tmp")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NCmd {
		t.Fatalf("expected NCmd, got %d", node.Kind)
	}
	if len(node.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(node.Children))
	}
	// child[0] = NIdent("ls")
	if node.Children[0].Kind != ast.NIdent || node.Children[0].Tok.Val != "ls" {
		t.Errorf("child[0]: got kind=%d val=%q, want NIdent 'ls'", node.Children[0].Kind, node.Children[0].Tok.Val)
	}
	// child[1] = compound word for "-la" (TMinus + TIdent adjacent)
	arg1 := nodeToString(node.Children[1])
	if arg1 != "-la" {
		t.Errorf("child[1] assembled = %q, want %q", arg1, "-la")
	}
	// child[2] = compound word for "/tmp" (TDiv + TIdent adjacent)
	arg2 := nodeToString(node.Children[2])
	if arg2 != "/tmp" {
		t.Errorf("child[2] assembled = %q, want %q", arg2, "/tmp")
	}
}

func TestParseCommandWithDoubleDash(t *testing.T) {
	node, err := parseStr("cmd --flag")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NCmd {
		t.Fatalf("expected NCmd, got %d", node.Kind)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(node.Children))
	}
	arg := nodeToString(node.Children[1])
	if arg != "--flag" {
		t.Errorf("child[1] assembled = %q, want %q", arg, "--flag")
	}
}

// ---------------------------------------------------------------------------
// Assignments and bindings
// ---------------------------------------------------------------------------

func TestParsePosixAssignment(t *testing.T) {
	node, err := parseStr("X=42")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NAssign {
		t.Fatalf("expected NAssign, got %d", node.Kind)
	}
	// New: Tok.Val = variable name, Children[0] = value node
	if node.Tok.Val != "X" {
		t.Errorf("Tok.Val = %q, want %q", node.Tok.Val, "X")
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child (value), got %d", len(node.Children))
	}
	if node.Children[0].Tok.Val != "42" {
		t.Errorf("value = %q, want %q", node.Children[0].Tok.Val, "42")
	}
}

func TestParseIshBind(t *testing.T) {
	node, err := parseStr("x = 42")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NMatch {
		t.Fatalf("expected NMatch, got %d", node.Kind)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(node.Children))
	}
	if node.Children[0].Kind != ast.NIdent {
		t.Errorf("lhs kind = %d, want NWord (%d)", node.Children[0].Kind, ast.NIdent)
	}
	if node.Children[0].Tok.Val != "x" {
		t.Errorf("lhs val = %q, want %q", node.Children[0].Tok.Val, "x")
	}
	if node.Children[1].Kind != ast.NLit {
		t.Errorf("rhs kind = %d, want NLit (%d)", node.Children[1].Kind, ast.NLit)
	}
	if node.Children[1].Tok.Val != "42" {
		t.Errorf("rhs val = %q, want %q", node.Children[1].Tok.Val, "42")
	}
}

func TestParseTupleBind(t *testing.T) {
	node, err := parseStr("{:ok, :err} = {:ok, :err}")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NMatch {
		t.Fatalf("expected NMatch, got %d", node.Kind)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(node.Children))
	}
	lhs := node.Children[0]
	rhs := node.Children[1]
	if lhs.Kind != ast.NTuple {
		t.Errorf("lhs kind = %d, want NTuple (%d)", lhs.Kind, ast.NTuple)
	}
	if len(lhs.Children) != 2 {
		t.Errorf("lhs children = %d, want 2", len(lhs.Children))
	}
	if rhs.Kind != ast.NTuple {
		t.Errorf("rhs kind = %d, want NTuple (%d)", rhs.Kind, ast.NTuple)
	}
	if len(rhs.Children) != 2 {
		t.Errorf("rhs children = %d, want 2", len(rhs.Children))
	}
}

// ---------------------------------------------------------------------------
// Pipelines
// ---------------------------------------------------------------------------

func TestParsePipeline(t *testing.T) {
	node, err := parseStr("a | b | c")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NPipe {
		t.Fatalf("expected NPipe, got %d", node.Kind)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children at top level, got %d", len(node.Children))
	}
	inner := node.Children[0]
	if inner.Kind != ast.NPipe {
		t.Fatalf("expected inner NPipe, got %d", inner.Kind)
	}
	if inner.Children[0].Kind != ast.NCmd {
		t.Errorf("inner left should be NCmd, got %d", inner.Children[0].Kind)
	}
}

func TestParseFunctionalPipe(t *testing.T) {
	node, err := parseStr("a |> b |> c")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NPipeFn {
		t.Fatalf("expected NPipeFn, got %d", node.Kind)
	}
	inner := node.Children[0]
	if inner.Kind != ast.NPipeFn {
		t.Fatalf("expected inner NPipeFn, got %d", inner.Kind)
	}
}

// ---------------------------------------------------------------------------
// And/Or lists, background
// ---------------------------------------------------------------------------

func TestParseAndOr(t *testing.T) {
	node, err := parseStr("a && b || c")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NOrList {
		t.Fatalf("expected NOrList, got %d", node.Kind)
	}
	inner := node.Children[0]
	if inner.Kind != ast.NAndList {
		t.Fatalf("expected inner NAndList, got %d", inner.Kind)
	}
}

func TestParseBackground(t *testing.T) {
	node, err := parseStr("cmd &")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NBg {
		t.Fatalf("expected NBg, got %d", node.Kind)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(node.Children))
	}
	if node.Children[0].Kind != ast.NCmd {
		t.Errorf("child should be NCmd, got %d", node.Children[0].Kind)
	}
}

// ---------------------------------------------------------------------------
// Subshell
// ---------------------------------------------------------------------------

func TestParseSubshell(t *testing.T) {
	node, err := parseStr("(echo hi)")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NSubshell {
		t.Fatalf("expected NSubshell, got %d", node.Kind)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(node.Children))
	}
	body := node.Children[0]
	if body.Kind != ast.NCmd {
		t.Errorf("body should be NCmd, got %d", body.Kind)
	}
}

// ---------------------------------------------------------------------------
// POSIX if/then/fi
// ---------------------------------------------------------------------------

func TestParsePosixIf(t *testing.T) {
	node, err := parseStr("if true; then echo yes; fi")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NIf {
		t.Fatalf("expected NIf, got %d", node.Kind)
	}
	if len(node.Clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(node.Clauses))
	}
	if node.Clauses[0].Pattern == nil {
		t.Error("expected condition (Pattern) to be non-nil")
	}
	if node.Clauses[0].Body == nil {
		t.Error("expected body to be non-nil")
	}
}

func TestParsePosixIfElse(t *testing.T) {
	node, err := parseStr("if true; then echo yes; else echo no; fi")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NIf {
		t.Fatalf("expected NIf, got %d", node.Kind)
	}
	if len(node.Clauses) != 2 {
		t.Fatalf("expected 2 clauses, got %d", len(node.Clauses))
	}
	if node.Clauses[0].Pattern == nil {
		t.Error("clause[0] should have a condition")
	}
	if node.Clauses[1].Pattern != nil {
		t.Error("clause[1] (else) should have no condition")
	}
}

// ---------------------------------------------------------------------------
// ish if/do/end
// ---------------------------------------------------------------------------

func TestParseIshIf(t *testing.T) {
	node, err := parseStr("if true do\necho yes\nend")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NIshIf {
		t.Fatalf("expected NIshIf, got %d", node.Kind)
	}
	if len(node.Clauses) < 1 {
		t.Fatalf("expected at least 1 clause, got %d", len(node.Clauses))
	}
	if node.Clauses[0].Pattern == nil {
		t.Error("expected condition to be non-nil")
	}
	if node.Clauses[0].Body == nil {
		t.Error("expected body to be non-nil")
	}
}

// ---------------------------------------------------------------------------
// For loop
// ---------------------------------------------------------------------------

func TestParseForLoop(t *testing.T) {
	node, err := parseStr("for x in a b c; do echo $x; done")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NFor {
		t.Fatalf("expected NFor, got %d", node.Kind)
	}
	if len(node.Children) < 1 {
		t.Fatalf("expected at least 1 child (var), got %d", len(node.Children))
	}
	if node.Children[0].Tok.Val != "x" {
		t.Errorf("loop var = %q, want %q", node.Children[0].Tok.Val, "x")
	}
	wordCount := len(node.Children) - 1
	if wordCount != 3 {
		t.Errorf("expected 3 words, got %d", wordCount)
	}
	if len(node.Clauses) != 1 {
		t.Fatalf("expected 1 clause (body), got %d", len(node.Clauses))
	}
	if node.Clauses[0].Body == nil {
		t.Error("expected body to be non-nil")
	}
}

// ---------------------------------------------------------------------------
// While loop
// ---------------------------------------------------------------------------

func TestParseWhileLoop(t *testing.T) {
	node, err := parseStr("while true; do echo loop; done")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NWhile {
		t.Fatalf("expected NWhile, got %d", node.Kind)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child (condition), got %d", len(node.Children))
	}
	if len(node.Clauses) != 1 {
		t.Fatalf("expected 1 clause (body), got %d", len(node.Clauses))
	}
}

// ---------------------------------------------------------------------------
// Case
// ---------------------------------------------------------------------------

func TestParseCase(t *testing.T) {
	node, err := parseStr("case x in\na) echo a;;\nesac")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NCase {
		t.Fatalf("expected NCase, got %d", node.Kind)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child (subject), got %d", len(node.Children))
	}
	if node.Children[0].Tok.Val != "x" {
		t.Errorf("subject = %q, want %q", node.Children[0].Tok.Val, "x")
	}
	if len(node.Clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(node.Clauses))
	}
	if node.Clauses[0].Pattern.Tok.Val != "a" {
		t.Errorf("pattern = %q, want %q", node.Clauses[0].Pattern.Tok.Val, "a")
	}
}

// ---------------------------------------------------------------------------
// POSIX function definition
// ---------------------------------------------------------------------------

func TestParsePosixFnDef(t *testing.T) {
	node, err := parseStr("greet() { echo hi; }")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NFnDef {
		t.Fatalf("expected NFnDef, got %d", node.Kind)
	}
	if node.Tok.Val != "greet" {
		t.Errorf("fn name = %q, want %q", node.Tok.Val, "greet")
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child (body), got %d", len(node.Children))
	}
}

// ---------------------------------------------------------------------------
// ish fn definitions
// ---------------------------------------------------------------------------

func TestParseIshFn(t *testing.T) {
	node, err := parseStr("fn add x y do\nx + y\nend")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NIshFn {
		t.Fatalf("expected NIshFn, got %d", node.Kind)
	}
	if node.Tok.Val != "add" {
		t.Errorf("fn name = %q, want %q", node.Tok.Val, "add")
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 params, got %d", len(node.Children))
	}
	if node.Children[0].Tok.Val != "x" {
		t.Errorf("param[0] = %q, want %q", node.Children[0].Tok.Val, "x")
	}
	if node.Children[1].Tok.Val != "y" {
		t.Errorf("param[1] = %q, want %q", node.Children[1].Tok.Val, "y")
	}
	if len(node.Clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(node.Clauses))
	}
	if node.Clauses[0].Body == nil {
		t.Error("expected body to be non-nil")
	}
}

func TestParseIshFnAnonymous(t *testing.T) {
	node, err := parseStr("fn do\necho hi\nend")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NIshFn {
		t.Fatalf("expected NIshFn, got %d", node.Kind)
	}
	if node.Tok.Val != "<anon>" {
		t.Errorf("fn name = %q, want %q", node.Tok.Val, "<anon>")
	}
}

func TestParseIshFnAnonWithParams(t *testing.T) {
	t.Run("bound to variable", func(t *testing.T) {
		node, err := parseStr("f = fn a, b do\na + b\nend")
		if err != nil {
			t.Fatal(err)
		}
		// Top-level is NMatch (binding)
		if node.Kind != ast.NMatch {
			t.Fatalf("expected NMatch, got %d", node.Kind)
		}
		fn := node.Children[1]
		if fn.Kind != ast.NIshFn {
			t.Fatalf("expected NIshFn on RHS, got %d", fn.Kind)
		}
		if fn.Tok.Val != "<anon>" {
			t.Errorf("fn name = %q, want %q", fn.Tok.Val, "<anon>")
		}
		if len(fn.Children) != 2 {
			t.Fatalf("expected 2 params, got %d", len(fn.Children))
		}
		if fn.Children[0].Tok.Val != "a" {
			t.Errorf("param[0] = %q, want %q", fn.Children[0].Tok.Val, "a")
		}
	})

	t.Run("no params multi-clause", func(t *testing.T) {
		node, err := parseStr("f = fn do\n0 -> :zero\n_ -> :other\nend")
		if err != nil {
			t.Fatal(err)
		}
		fn := node.Children[1]
		if fn.Kind != ast.NIshFn {
			t.Fatalf("expected NIshFn, got %d", fn.Kind)
		}
		if len(fn.Clauses) != 2 {
			t.Fatalf("expected 2 clauses, got %d", len(fn.Clauses))
		}
	})
}

func TestParseLambda(t *testing.T) {
	t.Run("single param", func(t *testing.T) {
		node, err := parseStr(`\x -> x`)
		if err != nil {
			t.Fatal(err)
		}
		if node.Kind != ast.NLambda {
			t.Fatalf("expected NLambda, got %d", node.Kind)
		}
		if len(node.Children) != 1 {
			t.Fatalf("expected 1 param, got %d", len(node.Children))
		}
	})

	t.Run("multi param", func(t *testing.T) {
		node, err := parseStr(`\a, b -> a`)
		if err != nil {
			t.Fatal(err)
		}
		if node.Kind != ast.NLambda {
			t.Fatalf("expected NLambda, got %d", node.Kind)
		}
		if len(node.Children) != 2 {
			t.Fatalf("expected 2 params, got %d", len(node.Children))
		}
	})

	t.Run("zero param", func(t *testing.T) {
		node, err := parseStr(`\ -> 42`)
		if err != nil {
			t.Fatal(err)
		}
		if node.Kind != ast.NLambda {
			t.Fatalf("expected NLambda, got %d", node.Kind)
		}
		if len(node.Children) != 0 {
			t.Fatalf("expected 0 params, got %d", len(node.Children))
		}
	})
}

// ---------------------------------------------------------------------------
// Match expression
// ---------------------------------------------------------------------------

func TestParseMatchExpr(t *testing.T) {
	node, err := parseStr("match x do\n:ok -> echo yes\nend")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NIshMatch {
		t.Fatalf("expected NIshMatch, got %d", node.Kind)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child (subject), got %d", len(node.Children))
	}
	if len(node.Clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(node.Clauses))
	}
	if node.Clauses[0].Pattern == nil {
		t.Error("expected clause pattern to be non-nil")
	}
	if node.Clauses[0].Body == nil {
		t.Error("expected clause body to be non-nil")
	}
}

// ---------------------------------------------------------------------------
// spawn / send / receive
// ---------------------------------------------------------------------------

func TestParseSpawn(t *testing.T) {
	node, err := parseStr("spawn echo hi")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NIshSpawn {
		t.Fatalf("expected NIshSpawn, got %d", node.Kind)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(node.Children))
	}
}

func TestParseSend(t *testing.T) {
	node, err := parseStr("send pid, :hello")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NIshSend {
		t.Fatalf("expected NIshSend, got %d", node.Kind)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children (target, msg), got %d", len(node.Children))
	}
}

func TestParseReceive(t *testing.T) {
	node, err := parseStr("receive do\n:msg -> echo got\nend")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NIshReceive {
		t.Fatalf("expected NIshReceive, got %d", node.Kind)
	}
	if len(node.Clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(node.Clauses))
	}
}

// ---------------------------------------------------------------------------
// Redirections
// ---------------------------------------------------------------------------

func TestParseRedirection(t *testing.T) {
	node, err := parseStr("echo hi > file")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NCmd {
		t.Fatalf("expected NCmd, got %d", node.Kind)
	}
	if len(node.Redirs) != 1 {
		t.Fatalf("expected 1 redir, got %d", len(node.Redirs))
	}
	if node.Redirs[0].Op != ast.TGt {
		t.Errorf("redir op = %d, want TGt (%d)", node.Redirs[0].Op, ast.TGt)
	}
	redirTarget := ""
	if node.Redirs[0].TargetNode != nil {
		redirTarget = node.Redirs[0].TargetNode.Tok.Val
	}
	if redirTarget != "file" {
		t.Errorf("redir target = %q, want %q", redirTarget, "file")
	}
	if node.Redirs[0].Fd != 1 {
		t.Errorf("redir fd = %d, want 1", node.Redirs[0].Fd)
	}
}

// ---------------------------------------------------------------------------
// [ test builtin
// ---------------------------------------------------------------------------

func TestParseTestBuiltin(t *testing.T) {
	node, err := parseStr("[ -f file ]")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NCmd {
		t.Fatalf("expected NCmd (not NList), got %d", node.Kind)
	}
	if len(node.Children) < 1 {
		t.Fatal("expected at least 1 child")
	}
	if node.Children[0].Tok.Val != "[" {
		t.Errorf("child[0] = %q, want %q", node.Children[0].Tok.Val, "[")
	}
}

// ---------------------------------------------------------------------------
// List literal
// ---------------------------------------------------------------------------

func TestParseListLiteral(t *testing.T) {
	node, err := parseStr("l = [1, 2, 3]")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NMatch {
		t.Fatalf("expected NMatch, got %d", node.Kind)
	}
	rhs := node.Children[1]
	if rhs.Kind != ast.NList {
		t.Fatalf("expected NList, got %d", rhs.Kind)
	}
	if len(rhs.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(rhs.Children))
	}
}

// ---------------------------------------------------------------------------
// Tuple literal
// ---------------------------------------------------------------------------

func TestParseTupleLiteral(t *testing.T) {
	node, err := parseStr("{:ok, 42}")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NTuple {
		t.Fatalf("expected NTuple, got %d", node.Kind)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(node.Children))
	}
	if node.Children[0].Kind != ast.NLit {
		t.Errorf("child[0] kind = %d, want NLit (%d)", node.Children[0].Kind, ast.NLit)
	}
	if node.Children[0].Tok.Val != "ok" {
		t.Errorf("child[0] val = %q, want %q", node.Children[0].Tok.Val, "ok")
	}
}

// ---------------------------------------------------------------------------
// Map literal
// ---------------------------------------------------------------------------

func TestParseMapLiteral(t *testing.T) {
	node, err := parseStr("%{x: 1}")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NMap {
		t.Fatalf("expected NMap, got %d", node.Kind)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children (key + val), got %d", len(node.Children))
	}
}

// ---------------------------------------------------------------------------
// Expression precedence
// ---------------------------------------------------------------------------

func TestParseExprPrecedence(t *testing.T) {
	node, err := parseStr("1 + 2 * 3")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NBinOp {
		t.Fatalf("expected NBinOp, got %d", node.Kind)
	}
	if node.Tok.Val != "+" {
		t.Errorf("top op = %q, want %q", node.Tok.Val, "+")
	}
	right := node.Children[1]
	if right.Kind != ast.NBinOp {
		t.Fatalf("right should be NBinOp, got %d", right.Kind)
	}
	if right.Tok.Val != "*" {
		t.Errorf("right op = %q, want %q", right.Tok.Val, "*")
	}
}

// ---------------------------------------------------------------------------
// Arithmetic in command context
// ---------------------------------------------------------------------------

func TestParseArithmeticInCommand(t *testing.T) {
	// fib (n - 1) + fib (n - 2) in expression context (binding RHS)
	// fib followed by ( should be a function call (NCall) in expression context
	node, err := parseStr("r = fib (n - 1) + fib (n - 2)")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NMatch {
		t.Fatalf("expected NMatch (binding), got %d", node.Kind)
	}
	rhs := node.Children[1]
	if rhs.Kind != ast.NBinOp {
		t.Fatalf("expected NBinOp on RHS, got %d", rhs.Kind)
	}
	if rhs.Tok.Val != "+" {
		t.Errorf("op = %q, want %q", rhs.Tok.Val, "+")
	}
	if rhs.Children[0].Kind != ast.NCall {
		t.Errorf("left should be NCall (fib call), got %d", rhs.Children[0].Kind)
	}
	if rhs.Children[1].Kind != ast.NCall {
		t.Errorf("right should be NCall (fib call), got %d", rhs.Children[1].Kind)
	}
}

// ---------------------------------------------------------------------------
// Symbol table: NCall vs NCmd dispatch
// ---------------------------------------------------------------------------

func TestSymbolTable_FnDeclProducesNCall(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		// which child index to check (in NBlock), and expected kind
		child  int
		expect ast.NodeKind
	}{
		{"fn then call", "fn foo do 42 end\nfoo", 1, ast.NCall},
		{"fn then call with arg", "fn add a b do a + b end\nadd 1 2", 1, ast.NCall},
		{"lambda bind then call", "f = \\x -> x * 2\nf 10", 1, ast.NCall},
		{"zero-arity lambda standalone", "f = \\ -> 42\nf", 1, ast.NCall},
		{"posix fn stays NCmd", "f() { echo hello; }\nf", 1, ast.NCmd},
		{"unknown stays NCmd", "foo bar", 0, ast.NCmd},
		{"builtin stays NCmd", "echo hello", 0, ast.NCmd},
		{"known module produces NCall", "String.upcase \"hello\"", 0, ast.NCall},
		{"Enum.map with comma args", "Enum.map [1, 2], \\x -> x * 2", 0, ast.NCall},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node, err := parseStr(tc.src)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			var target *ast.Node
			if node.Kind == ast.NBlock {
				if tc.child >= len(node.Children) {
					t.Fatalf("NBlock has %d children, want index %d", len(node.Children), tc.child)
				}
				target = node.Children[tc.child]
			} else {
				target = node
			}
			if target.Kind != tc.expect {
				t.Errorf("got node kind %d, want %d", target.Kind, tc.expect)
			}
		})
	}
}

func TestSymbolTable_DeclareTracking(t *testing.T) {
	// Verify the parser's symbol table is populated during parsing
	p := newParser(lexer.New("fn greet name do echo $name end\ngreet world"))
	_, err := p.parseProgram()
	if err != nil {
		t.Fatal(err)
	}
	sym, ok := p.symbols["greet"]
	if !ok {
		t.Fatal("greet not in symbol table after fn declaration")
	}
	if sym.Kind != SymFn {
		t.Errorf("greet sym kind = %d, want SymFn (%d)", sym.Kind, SymFn)
	}
}

func TestSymbolTable_PosixFnTracking(t *testing.T) {
	p := newParser(lexer.New("f() { echo hello; }\nf arg"))
	_, err := p.parseProgram()
	if err != nil {
		t.Fatal(err)
	}
	sym, ok := p.symbols["f"]
	if !ok {
		t.Fatal("f not in symbol table after POSIX fn def")
	}
	if sym.Kind != SymPOSIXFn {
		t.Errorf("f sym kind = %d, want SymPOSIXFn (%d)", sym.Kind, SymPOSIXFn)
	}
}

func TestSymbolTable_LambdaBindTracking(t *testing.T) {
	p := newParser(lexer.New("f = \\x -> x * 2\nf 10"))
	_, err := p.parseProgram()
	if err != nil {
		t.Fatal(err)
	}
	sym, ok := p.symbols["f"]
	if !ok {
		t.Fatal("f not in symbol table after lambda bind")
	}
	if sym.Kind != SymFn {
		t.Errorf("f sym kind = %d, want SymFn (%d)", sym.Kind, SymFn)
	}
}

func TestSymbolTable_FnDeclStandaloneDispatch(t *testing.T) {
	// Instrument dispatchIdent by observing what it does
	// Parse step-by-step to see when the symbol appears
	p := newParser(lexer.New("fn foo do 42 end\nfoo"))

	// Parse the fn definition first
	p.skipNewlines()
	stmt1, err := p.parseList()
	if err != nil {
		t.Fatalf("parse stmt1: %v", err)
	}
	t.Logf("stmt1 kind=%d tok=%q", stmt1.Kind, stmt1.Tok.Val)

	// Now check symbol table AFTER fn def, BEFORE second stmt
	sym, hasFoo := p.symbols["foo"]
	t.Logf("after fn def: foo in symbols = %v, sym = %+v", hasFoo, sym)

	// Check what the parser sees next
	p.skipSeparators()
	cur := p.cur()
	t.Logf("next token: type=%d val=%q", cur.Type, cur.Val)
	nextSym := p.symbols[cur.Val]
	t.Logf("symbol for %q: %+v", cur.Val, nextSym)

	// Parse the second statement
	stmt2, err := p.parseList()
	if err != nil {
		t.Fatalf("parse stmt2: %v", err)
	}
	node := ast.BlockNode([]*ast.Node{stmt1, stmt2})

	_ = node
	// Check symbol table
	sym, ok := p.symbols["foo"]
	if !ok {
		t.Fatal("foo not in symbol table")
	}
	t.Logf("foo sym kind = %d (SymFn=%d, SymBuiltin=%d)", sym.Kind, SymFn, SymBuiltin)

	// Check AST
	if node.Kind != ast.NBlock {
		t.Fatalf("expected NBlock, got %d", node.Kind)
	}
	t.Logf("children: %d", len(node.Children))
	for i, c := range node.Children {
		t.Logf("  child[%d] kind=%d tok=%q", i, c.Kind, c.Tok.Val)
	}
	if len(node.Children) >= 2 {
		second := node.Children[1]
		t.Logf("second child kind=%d (NCall=%d, NCmd=%d)", second.Kind, ast.NCall, ast.NCmd)
		if second.Kind == ast.NCmd && len(second.Children) > 0 {
			nameChild := second.Children[0]
			t.Logf("  NCmd name child kind=%d tok=%q toktype=%d", nameChild.Kind, nameChild.Tok.Val, nameChild.Tok.Type)
		}
	}
}

// ---------------------------------------------------------------------------
// isBlockEnd
// ---------------------------------------------------------------------------

func TestIsBlockEnd(t *testing.T) {
	t.Run("keyword block-enders are always block-end", func(t *testing.T) {
		for _, tt := range []ast.TokenType{ast.TEnd, ast.TDone, ast.TFi, ast.TEsac} {
			if !isBlockEnd(tt) {
				t.Errorf("isBlockEnd(%v) = false, want true", tt)
			}
		}
		if isBlockEnd(ast.TIdent) {
			t.Error("isBlockEnd(TIdent) should be false")
		}
	})

	t.Run("pushTerminators accumulates", func(t *testing.T) {
		p := &Parser{lex: lexer.New("")}
		old := p.pushTerminators(ast.TDone)
		// TDone is always a block end (keyword), plus it's in terminators
		p.fillTo(0) // ensure token buffer
		old2 := p.pushTerminators(ast.TFi)
		p.restoreTerminators(old2)
		p.restoreTerminators(old)
		_ = old // suppress unused
	})
}

// ---------------------------------------------------------------------------
// isExprOperator
// ---------------------------------------------------------------------------

func TestIsExprBinOp(t *testing.T) {
	// These are unambiguous binary ops at statement level
	ops := []ast.TokenType{ast.TMul, ast.TDiv, ast.TEq, ast.TNe, ast.TLe, ast.TGe}
	for _, op := range ops {
		if !isExprBinOp(op) {
			t.Errorf("isExprBinOp(%v) = false, want true", op)
		}
	}

	// These are NOT unambiguous at statement level:
	// TMinus/TPlus: could be flags. TGt/TLt: could be redirects. TDot: field access (separate check).
	nonOps := []ast.TokenType{ast.TPipe, ast.TAnd, ast.TOr, ast.TSemicolon, ast.TNewline, ast.TEOF,
		ast.TIdent, ast.TInt, ast.TMinus, ast.TPlus, ast.TGt, ast.TLt, ast.TDot}
	for _, op := range nonOps {
		if isExprBinOp(op) {
			t.Errorf("isExprBinOp(%v) = true, want false", op)
		}
	}
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

func TestParseErrorUnterminatedIf(t *testing.T) {
	_, err := parseStr("if true; then echo hello")
	if err == nil {
		t.Error("expected parse error for unterminated if, got nil")
	}
}

func TestParseErrorMissingDo(t *testing.T) {
	_, err := parseStr("if true\necho hi\nend")
	if err == nil {
		t.Error("expected parse error for missing do/then, got nil")
	}
}

func TestParseErrorMissingEnd(t *testing.T) {
	_, err := parseStr("fn add x y do\nx + y")
	if err == nil {
		t.Error("expected parse error for missing end, got nil")
	}
}

func TestParseErrorMissingDone(t *testing.T) {
	_, err := parseStr("for x in a b c; do echo $x")
	if err == nil {
		t.Error("expected parse error for missing done, got nil")
	}
}

func TestParseErrorMissingFi(t *testing.T) {
	_, err := parseStr("if true; then echo hello; else echo world")
	if err == nil {
		t.Error("expected parse error for missing fi, got nil")
	}
}

func TestParseErrorMissingDoInFn(t *testing.T) {
	_, err := parseStr("fn add x y\nx + y\nend")
	if err == nil {
		t.Error("expected parse error for missing do in fn, got nil")
	}
}

func TestParseErrorMissingDoInWhile(t *testing.T) {
	_, err := parseStr("while true echo loop done")
	if err == nil {
		t.Error("expected parse error for missing do in while, got nil")
	}
}

func TestParseErrorMissingDoAfterReceive(t *testing.T) {
	_, err := parseStr("receive\n:msg -> echo got\nend")
	if err == nil {
		t.Error("expected parse error for missing do after receive, got nil")
	}
}

func TestParseErrorMissingDoAfterMatch(t *testing.T) {
	_, err := parseStr("match x\n:ok -> echo yes\nend")
	if err == nil {
		t.Error("expected parse error for missing do after match, got nil")
	}
}

func TestParseErrorRedirMissingTarget(t *testing.T) {
	_, err := parseStr("echo hi >")
	if err == nil {
		t.Error("expected parse error for missing redirection target, got nil")
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestParseEmptyInput(t *testing.T) {
	node, err := parseStr("")
	if err != nil {
		t.Fatal(err)
	}
	if node == nil {
		return // also acceptable
	}
	if node.Kind != ast.NBlock || len(node.Children) != 0 {
		t.Errorf("expected empty NBlock for empty input, got kind %d with %d children", node.Kind, len(node.Children))
	}
}

func TestParseMultipleStatements(t *testing.T) {
	node, err := parseStr("echo a\necho b")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NBlock {
		t.Fatalf("expected NBlock, got %d", node.Kind)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(node.Children))
	}
}

func TestParseSemicolonSeparated(t *testing.T) {
	node, err := parseStr("echo a; echo b")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NBlock {
		t.Fatalf("expected NBlock, got %d", node.Kind)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(node.Children))
	}
}

func TestParseSingleStatement(t *testing.T) {
	node, err := parseStr("echo hello")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NCmd {
		t.Fatalf("expected NCmd (not NBlock), got %d", node.Kind)
	}
}

func TestParseCaseMultipleClauses(t *testing.T) {
	node, err := parseStr("case x in\na) echo a;;\nb) echo b;;\nesac")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NCase {
		t.Fatalf("expected NCase, got %d", node.Kind)
	}
	if len(node.Clauses) != 2 {
		t.Fatalf("expected 2 clauses, got %d", len(node.Clauses))
	}
}

func TestParseIshMatchMultipleClauses(t *testing.T) {
	node, err := parseStr("match x do\n:ok -> echo yes\n:err -> echo no\nend")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NIshMatch {
		t.Fatalf("expected NIshMatch, got %d", node.Kind)
	}
	if len(node.Clauses) != 2 {
		t.Fatalf("expected 2 clauses, got %d", len(node.Clauses))
	}
}

func TestParseRedirAppend(t *testing.T) {
	node, err := parseStr("echo hi >> file")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NCmd {
		t.Fatalf("expected NCmd, got %d", node.Kind)
	}
	if len(node.Redirs) != 1 {
		t.Fatalf("expected 1 redir, got %d", len(node.Redirs))
	}
	if node.Redirs[0].Op != ast.TRedirAppend {
		t.Errorf("redir op = %d, want TRedirAppend (%d)", node.Redirs[0].Op, ast.TRedirAppend)
	}
}

func TestParseRedirInput(t *testing.T) {
	node, err := parseStr("cat < input.txt")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NCmd {
		t.Fatalf("expected NCmd, got %d", node.Kind)
	}
	if len(node.Redirs) != 1 {
		t.Fatalf("expected 1 redir, got %d", len(node.Redirs))
	}
	if node.Redirs[0].Op != ast.TLt {
		t.Errorf("redir op = %d, want TRedirIn (%d)", node.Redirs[0].Op, ast.TLt)
	}
	if node.Redirs[0].Fd != 0 {
		t.Errorf("redir fd = %d, want 0", node.Redirs[0].Fd)
	}
}

func TestParsePosixIfElif(t *testing.T) {
	node, err := parseStr("if false; then echo a; elif true; then echo b; fi")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NIf {
		t.Fatalf("expected NIf, got %d", node.Kind)
	}
	if len(node.Clauses) != 2 {
		t.Fatalf("expected 2 clauses (if + elif), got %d", len(node.Clauses))
	}
	if node.Clauses[0].Pattern == nil {
		t.Error("clause[0] should have a condition")
	}
	if node.Clauses[1].Pattern == nil {
		t.Error("clause[1] (elif) should have a condition")
	}
}

func TestParseIshIfDoElseEnd(t *testing.T) {
	node, err := parseStr("if false do\necho yes\nelse\necho no\nend")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NIshIf {
		t.Fatalf("expected NIshIf, got %d", node.Kind)
	}
	if len(node.Clauses) != 2 {
		t.Fatalf("expected 2 clauses, got %d", len(node.Clauses))
	}
}

func TestParseFnWithGuard(t *testing.T) {
	node, err := parseStr("fn fib n when n > 1 do\nn\nend")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NIshFn {
		t.Fatalf("expected NIshFn, got %d", node.Kind)
	}
	if len(node.Clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(node.Clauses))
	}
	if node.Clauses[0].Guard == nil {
		t.Error("expected guard to be non-nil")
	}
}

func TestParseUnaryNegation(t *testing.T) {
	node, err := parseStr("-42")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NUnary {
		t.Fatalf("expected NUnary, got %d", node.Kind)
	}
	if node.Tok.Val != "-" {
		t.Errorf("op = %q, want %q", node.Tok.Val, "-")
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(node.Children))
	}
}

func TestParseUnaryBang(t *testing.T) {
	node, err := parseStr("x = (!true)")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NMatch {
		t.Fatalf("expected NMatch, got %d", node.Kind)
	}
	rhs := node.Children[1]
	if rhs.Kind != ast.NUnary {
		t.Fatalf("expected NUnary, got %d", rhs.Kind)
	}
	if rhs.Tok.Val != "!" {
		t.Errorf("op = %q, want %q", rhs.Tok.Val, "!")
	}
}

func TestParseEmptyListLiteral(t *testing.T) {
	node, err := parseStr("[]")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NList {
		t.Fatalf("expected NList, got %d", node.Kind)
	}
	if len(node.Children) != 0 {
		t.Errorf("expected 0 children for empty list, got %d", len(node.Children))
	}
}

func TestParseEmptyTuple(t *testing.T) {
	node, err := parseStr("{:ok}")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NTuple {
		t.Fatalf("expected NTuple, got %d", node.Kind)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(node.Children))
	}
}

func TestParseStringLiteral(t *testing.T) {
	node, err := parseStr(`"hello world"`)
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NLit {
		t.Fatalf("expected NLit, got %d", node.Kind)
	}
	if node.Tok.Type != ast.TString {
		t.Errorf("tok type = %d, want TString (%d)", node.Tok.Type, ast.TString)
	}
	if node.Tok.Val != "hello world" {
		t.Errorf("tok val = %q, want %q", node.Tok.Val, "hello world")
	}
}

func TestParseIntLiteral(t *testing.T) {
	node, err := parseStr("42")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NLit {
		t.Fatalf("expected NLit, got %d", node.Kind)
	}
	if node.Tok.Type != ast.TInt {
		t.Errorf("tok type = %d, want TInt (%d)", node.Tok.Type, ast.TInt)
	}
}

func TestParseAtomLiteral(t *testing.T) {
	node, err := parseStr(":hello")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NLit {
		t.Fatalf("expected NLit, got %d", node.Kind)
	}
	if node.Tok.Type != ast.TAtom {
		t.Errorf("tok type = %d, want TAtom (%d)", node.Tok.Type, ast.TAtom)
	}
	if node.Tok.Val != "hello" {
		t.Errorf("tok val = %q, want %q", node.Tok.Val, "hello")
	}
}

func TestParseMapMultipleEntries(t *testing.T) {
	node, err := parseStr("%{x: 1, y: 2}")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NMap {
		t.Fatalf("expected NMap, got %d", node.Kind)
	}
	if len(node.Children) != 4 {
		t.Fatalf("expected 4 children, got %d", len(node.Children))
	}
}

func TestParseEqualityExpr(t *testing.T) {
	node, err := parseStr("5 == 5")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NBinOp {
		t.Fatalf("expected NBinOp, got %d", node.Kind)
	}
	if node.Tok.Type != ast.TEq {
		t.Errorf("op type = %d, want TEq (%d)", node.Tok.Type, ast.TEq)
	}
}

func TestParseInequalityExpr(t *testing.T) {
	node, err := parseStr("5 != 6")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NBinOp {
		t.Fatalf("expected NBinOp, got %d", node.Kind)
	}
	if node.Tok.Type != ast.TNe {
		t.Errorf("op type = %d, want TNe (%d)", node.Tok.Type, ast.TNe)
	}
}

func TestParseComparisonLe(t *testing.T) {
	node, err := parseStr("3 <= 5")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NBinOp {
		t.Fatalf("expected NBinOp, got %d", node.Kind)
	}
	if node.Tok.Type != ast.TLe {
		t.Errorf("op type = %d, want TLe (%d)", node.Tok.Type, ast.TLe)
	}
}

func TestParseComparisonGe(t *testing.T) {
	node, err := parseStr("5 >= 3")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NBinOp {
		t.Fatalf("expected NBinOp, got %d", node.Kind)
	}
	if node.Tok.Type != ast.TGe {
		t.Errorf("op type = %d, want TGe (%d)", node.Tok.Type, ast.TGe)
	}
}

func TestParseSubtraction(t *testing.T) {
	node, err := parseStr("10 - 3")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NBinOp {
		t.Fatalf("expected NBinOp, got %d", node.Kind)
	}
	if node.Tok.Type != ast.TMinus {
		t.Errorf("op type = %d, want TMinus (%d)", node.Tok.Type, ast.TMinus)
	}
}

func TestParseDivision(t *testing.T) {
	node, err := parseStr("20 / 4")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NBinOp {
		t.Fatalf("expected NBinOp, got %d", node.Kind)
	}
	if node.Tok.Type != ast.TDiv {
		t.Errorf("op type = %d, want TDiv (%d)", node.Tok.Type, ast.TDiv)
	}
}

func TestParseDotAccess(t *testing.T) {
	node, err := parseStr("r = m.x")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NMatch {
		t.Fatalf("expected NMatch, got %d", node.Kind)
	}
	rhs := node.Children[1]
	if rhs.Kind != ast.NAccess {
		t.Fatalf("expected NAccess, got %d", rhs.Kind)
	}
	if rhs.Tok.Val != "x" {
		t.Errorf("field = %q, want %q", rhs.Tok.Val, "x")
	}
}

func TestParseIshBindWithExpr(t *testing.T) {
	node, err := parseStr("x = 10 + 5")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NMatch {
		t.Fatalf("expected NMatch, got %d", node.Kind)
	}
	rhs := node.Children[1]
	if rhs.Kind != ast.NBinOp {
		t.Fatalf("rhs should be NBinOp, got %d", rhs.Kind)
	}
	if rhs.Tok.Val != "+" {
		t.Errorf("rhs op = %q, want %q", rhs.Tok.Val, "+")
	}
}

func TestParseUntilLoop(t *testing.T) {
	node, err := parseStr("until false; do echo loop; done")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NUntil {
		t.Fatalf("expected NUntil, got %d", node.Kind)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child (condition), got %d", len(node.Children))
	}
	if len(node.Clauses) != 1 {
		t.Fatalf("expected 1 clause (body), got %d", len(node.Clauses))
	}
}

func TestParseGroupCommand(t *testing.T) {
	node, err := parseStr("{ echo hi; }")
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != ast.NGroup {
		t.Fatalf("expected NGroup, got %d", node.Kind)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(node.Children))
	}
}

func TestParseDotPaths(t *testing.T) {
	t.Run("cd dotdot is command", func(t *testing.T) {
		node, err := parseStr("cd ..")
		if err != nil {
			t.Fatal(err)
		}
		if node.Kind != ast.NCmd {
			t.Fatalf("expected NCmd, got %d", node.Kind)
		}
		if len(node.Children) != 2 {
			t.Fatalf("expected 2 children [cd, ..], got %d", len(node.Children))
		}
		arg := nodeToString(node.Children[1])
		if arg != ".." {
			t.Errorf("arg = %q, want %q", arg, "..")
		}
	})

	t.Run("dot-slash script is command", func(t *testing.T) {
		node, err := parseStr("./script")
		if err != nil {
			t.Fatal(err)
		}
		if node.Kind != ast.NCmd {
			t.Fatalf("expected NCmd, got %d", node.Kind)
		}
		cmd := nodeToString(node.Children[0])
		if cmd != "./script" {
			t.Errorf("cmd = %q, want %q", cmd, "./script")
		}
	})

	t.Run("ls dotfile is command", func(t *testing.T) {
		node, err := parseStr("ls .hidden")
		if err != nil {
			t.Fatal(err)
		}
		if node.Kind != ast.NCmd {
			t.Fatalf("expected NCmd, got %d", node.Kind)
		}
		if len(node.Children) != 2 {
			t.Fatalf("expected 2 children, got %d", len(node.Children))
		}
		arg := nodeToString(node.Children[1])
		if arg != ".hidden" {
			t.Errorf("arg = %q, want %q", arg, ".hidden")
		}
	})

	t.Run("cd ../foo is command", func(t *testing.T) {
		node, err := parseStr("cd ../foo")
		if err != nil {
			t.Fatal(err)
		}
		if node.Kind != ast.NCmd {
			t.Fatalf("expected NCmd, got %d", node.Kind)
		}
		arg := nodeToString(node.Children[1])
		if arg != "../foo" {
			t.Errorf("arg = %q, want %q", arg, "../foo")
		}
	})
}

func TestParseExprDepthLimit(t *testing.T) {
	t.Skip("TODO: deeply nested parens in binding RHS go through subshell path, depth limit needs rework")
	deep := "x = " + strings.Repeat("(", 1001) + "1" + strings.Repeat(")", 1001)
	_, err := parseStr(deep)
	if err == nil {
		t.Fatal("expected error for deeply nested expression")
	}
	if !strings.Contains(err.Error(), "too deeply nested") {
		t.Errorf("expected 'too deeply nested' error, got: %s", err)
	}
}

func TestTailPositionMarking(t *testing.T) {
	t.Run("fn body last stmt", func(t *testing.T) {
		node, err := parseStr("fn foo do\necho a\necho b\nend")
		if err != nil {
			t.Fatal(err)
		}
		body := node.Clauses[0].Body
		if body.Kind != ast.NBlock {
			t.Fatalf("expected NBlock, got %d", body.Kind)
		}
		if body.Children[0].Tail {
			t.Error("first stmt should not be in tail position")
		}
		if !body.Children[1].Tail {
			t.Error("last stmt should be in tail position")
		}
	})

	t.Run("fn single stmt body", func(t *testing.T) {
		node, err := parseStr("fn foo do\necho a\nend")
		if err != nil {
			t.Fatal(err)
		}
		body := node.Clauses[0].Body
		if !body.Tail {
			t.Error("single-stmt fn body should be in tail position")
		}
	})

	t.Run("if/else both branches", func(t *testing.T) {
		node, err := parseStr("fn foo do\nif true do\necho a\nelse\necho b\nend\nend")
		if err != nil {
			t.Fatal(err)
		}
		body := node.Clauses[0].Body
		if !body.Tail {
			t.Error("if in tail position should be marked")
		}
		if body.Kind != ast.NIshIf {
			t.Fatalf("expected NIshIf, got %d", body.Kind)
		}
		thenBody := body.Clauses[0].Body
		if !thenBody.Tail {
			t.Error("then branch body should be in tail position")
		}
		elseBody := body.Clauses[1].Body
		if !elseBody.Tail {
			t.Error("else branch body should be in tail position")
		}
	})

	t.Run("clause bodies in match", func(t *testing.T) {
		node, err := parseStr("fn foo do\nmatch x do\n:a -> echo a\n:b -> echo b\nend\nend")
		if err != nil {
			t.Fatal(err)
		}
		body := node.Clauses[0].Body
		if !body.Tail {
			t.Error("match in tail position should be marked")
		}
	})

	t.Run("lambda body", func(t *testing.T) {
		node, err := parseStr("f = \\x -> x + 1")
		if err != nil {
			t.Fatal(err)
		}
		lambda := node.Children[1]
		if lambda.Kind != ast.NLambda {
			t.Fatalf("expected NLambda, got %d", lambda.Kind)
		}
		lambdaBody := lambda.Clauses[0].Body
		if !lambdaBody.Tail {
			t.Error("lambda body should be in tail position")
		}
	})
}
