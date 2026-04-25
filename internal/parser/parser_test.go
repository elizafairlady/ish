package parser

import (
	"testing"

	"ish/internal/ast"
	"ish/internal/lexer"
)

func parse(input string) (*ast.Node, error) {
	tokens := lexer.Lex(input)
	p := New(tokens)
	return p.Parse()
}

func truncName(s string) string {
	if len(s) > 40 {
		return s[:40]
	}
	return s
}

func mustParse(t *testing.T, input string) *ast.Node {
	t.Helper()
	node, err := parse(input)
	if err != nil {
		t.Fatalf("parse(%q): %v", input, err)
	}
	return node
}

func firstChild(t *testing.T, input string) *ast.Node {
	t.Helper()
	block := mustParse(t, input)
	if len(block.Children) == 0 {
		t.Fatalf("parse(%q): empty block", input)
	}
	return block.Children[0]
}

// =====================================================================
// TIER 1: The 7 ambiguous tokens — structural resolution
// =====================================================================

func TestToken_Gt_RedirectInCommand(t *testing.T) {
	cmd := firstChild(t, "echo hello > file")
	if cmd.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", cmd.Kind)
	}
	if len(cmd.Children) != 2 {
		t.Fatalf("expected 2 children (echo, hello), got %d", len(cmd.Children))
	}
	if cmd.Children[0].Tok.Val != "echo" || cmd.Children[1].Tok.Val != "hello" {
		t.Errorf("expected [echo, hello], got [%s, %s]", cmd.Children[0].Tok.Val, cmd.Children[1].Tok.Val)
	}
	if len(cmd.Redirs) != 1 || cmd.Redirs[0].Op != ast.TGt || cmd.Redirs[0].Fd != 1 {
		t.Errorf("expected stdout > redirect, got %d redirs", len(cmd.Redirs))
	}
	if cmd.Redirs[0].Target.Tok.Val != "file" {
		t.Errorf("expected redirect target 'file', got %q", cmd.Redirs[0].Target.Tok.Val)
	}
}

func TestToken_Gt_ComparisonInBinding(t *testing.T) {
	bind := firstChild(t, "x = y > 5")
	if bind.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", bind.Kind)
	}
	rhs := bind.Children[1]
	if rhs.Kind != ast.NBinOp || rhs.Tok.Type != ast.TGt {
		t.Fatalf("expected > comparison, got kind=%v", rhs.Kind)
	}
	if rhs.Children[0].Tok.Val != "y" {
		t.Errorf("expected left operand 'y', got %q", rhs.Children[0].Tok.Val)
	}
	if rhs.Children[1].Tok.Val != "5" {
		t.Errorf("expected right operand '5', got %q", rhs.Children[1].Tok.Val)
	}
}

func TestToken_Gt_ComparisonInParens(t *testing.T) {
	// In binding RHS, parens are expression context
	bind := firstChild(t, "r = (x > 5)")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NBinOp || rhs.Tok.Type != ast.TGt {
		t.Fatalf("expected > comparison in binding parens, got kind=%v", rhs.Kind)
	}
}

func TestToken_Gt_SubshellRedirectInParens(t *testing.T) {
	node := firstChild(t, "(x > 5)")
	if node.Kind != ast.NSubshell {
		t.Fatalf("expected NSubshell, got %v", node.Kind)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child in subshell, got %d", len(node.Children))
	}
	inner := node.Children[0]
	if len(inner.Redirs) != 1 || inner.Redirs[0].Op != ast.TGt {
		t.Fatalf("expected > redirect inside subshell")
	}
}

func TestToken_Lt_RedirectInCommand(t *testing.T) {
	cmd := firstChild(t, "sort < input.txt")
	if len(cmd.Redirs) != 1 || cmd.Redirs[0].Op != ast.TLt || cmd.Redirs[0].Fd != 0 {
		t.Fatalf("expected stdin < redirect")
	}
}

func TestToken_Lt_ComparisonInBinding(t *testing.T) {
	bind := firstChild(t, "x = a < b")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NBinOp || rhs.Tok.Type != ast.TLt {
		t.Fatalf("expected < comparison, got kind=%v tok=%v", rhs.Kind, rhs.Tok.Type)
	}
}

func TestToken_Bracket_ListInExpr(t *testing.T) {
	bind := firstChild(t, "x = [1, 2, 3]")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NList {
		t.Fatalf("expected NList, got %v", rhs.Kind)
	}
	if len(rhs.Children) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(rhs.Children))
	}
	for i, want := range []string{"1", "2", "3"} {
		if rhs.Children[i].Tok.Val != want {
			t.Errorf("element %d: expected %q, got %q", i, want, rhs.Children[i].Tok.Val)
		}
	}
}

func TestToken_Bracket_TestInCommand(t *testing.T) {
	cmd := firstChild(t, "[ -f foo ]")
	if cmd.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", cmd.Kind)
	}
	if len(cmd.Children) != 4 {
		t.Fatalf("expected 4 children ([, -f, foo, ]), got %d", len(cmd.Children))
	}
	if cmd.Children[0].Tok.Val != "[" {
		t.Errorf("expected [ as head, got %q", cmd.Children[0].Tok.Val)
	}
	if cmd.Children[1].Tok.Val != "-f" {
		t.Errorf("expected -f flag, got %q", cmd.Children[1].Tok.Val)
	}
	if cmd.Children[3].Tok.Val != "]" {
		t.Errorf("expected ] as last arg, got %q", cmd.Children[3].Tok.Val)
	}
}

func TestToken_Brace_TupleInExpr(t *testing.T) {
	bind := firstChild(t, "x = {:ok, nil}")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NTuple {
		t.Fatalf("expected NTuple, got %v", rhs.Kind)
	}
	if len(rhs.Children) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(rhs.Children))
	}
	if rhs.Children[0].Kind != ast.NAtom || rhs.Children[0].Tok.Val != "ok" {
		t.Errorf("expected :ok, got kind=%v val=%q", rhs.Children[0].Kind, rhs.Children[0].Tok.Val)
	}
	if rhs.Children[1].Kind != ast.NLit || rhs.Children[1].Tok.Val != "nil" {
		t.Errorf("expected nil, got kind=%v val=%q", rhs.Children[1].Kind, rhs.Children[1].Tok.Val)
	}
}

func TestToken_Brace_GroupInCommand(t *testing.T) {
	node := firstChild(t, "{ echo hello; echo world; }")
	if node.Kind != ast.NBlock {
		t.Fatalf("expected NBlock (group), got %v", node.Kind)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 statements in group, got %d", len(node.Children))
	}
}

func TestToken_Backslash_Lambda(t *testing.T) {
	bind := firstChild(t, `x = \a b -> a + b`)
	rhs := bind.Children[1]
	if rhs.Kind != ast.NLambda {
		t.Fatalf("expected lambda, got %v", rhs.Kind)
	}
	// 2 params + body = 3 children
	if len(rhs.Children) != 3 {
		t.Fatalf("expected 3 children (2 params + body), got %d", len(rhs.Children))
	}
	if rhs.Children[0].Tok.Val != "a" || rhs.Children[1].Tok.Val != "b" {
		t.Errorf("expected params a, b; got %q, %q", rhs.Children[0].Tok.Val, rhs.Children[1].Tok.Val)
	}
	body := rhs.Children[2]
	if body.Kind != ast.NBinOp || body.Tok.Type != ast.TPlus {
		t.Errorf("expected + body, got kind=%v", body.Kind)
	}
}

func TestToken_Minus_FlagInCommand(t *testing.T) {
	cmd := firstChild(t, "ls -la")
	if cmd.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", cmd.Kind)
	}
	if len(cmd.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(cmd.Children))
	}
	if cmd.Children[1].Kind != ast.NFlag || cmd.Children[1].Tok.Val != "-la" {
		t.Errorf("expected flag -la, got kind=%v val=%q", cmd.Children[1].Kind, cmd.Children[1].Tok.Val)
	}
}

func TestToken_Minus_OperatorInExpr(t *testing.T) {
	bind := firstChild(t, "x = 10 - 3")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NBinOp || rhs.Tok.Type != ast.TMinus {
		t.Fatalf("expected - binop, got %v", rhs.Kind)
	}
	if rhs.Children[0].Tok.Val != "10" || rhs.Children[1].Tok.Val != "3" {
		t.Errorf("expected 10 - 3, got %s - %s", rhs.Children[0].Tok.Val, rhs.Children[1].Tok.Val)
	}
}

// =====================================================================
// TIER 2: Destructuring bindings
// =====================================================================

func TestBind_Simple(t *testing.T) {
	bind := firstChild(t, "x = 42")
	if bind.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", bind.Kind)
	}
	if bind.Children[0].Tok.Val != "x" {
		t.Errorf("expected LHS 'x', got %q", bind.Children[0].Tok.Val)
	}
	if bind.Children[1].Tok.Val != "42" {
		t.Errorf("expected RHS '42', got %q", bind.Children[1].Tok.Val)
	}
}

func TestBind_Expression(t *testing.T) {
	bind := firstChild(t, "x = 1 + 2 * 3")
	rhs := bind.Children[1]
	// + at top (lower precedence), * as right child
	if rhs.Kind != ast.NBinOp || rhs.Tok.Type != ast.TPlus {
		t.Fatalf("expected + at top, got %v %v", rhs.Kind, rhs.Tok.Val)
	}
	right := rhs.Children[1]
	if right.Kind != ast.NBinOp || right.Tok.Type != ast.TStar {
		t.Fatalf("expected * as right child, got %v %v", right.Kind, right.Tok.Val)
	}
}

func TestBind_TupleDestructure(t *testing.T) {
	node := firstChild(t, `{status, value} = {:ok, "hello"}`)
	if node.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", node.Kind)
	}
	lhs := node.Children[0]
	if lhs.Kind != ast.NTuple {
		t.Fatalf("expected NTuple LHS, got %v", lhs.Kind)
	}
	if len(lhs.Children) != 2 {
		t.Fatalf("expected 2 elements in LHS tuple, got %d", len(lhs.Children))
	}
	if lhs.Children[0].Tok.Val != "status" || lhs.Children[1].Tok.Val != "value" {
		t.Errorf("expected {status, value}, got {%s, %s}", lhs.Children[0].Tok.Val, lhs.Children[1].Tok.Val)
	}
	rhs := node.Children[1]
	if rhs.Kind != ast.NTuple || len(rhs.Children) != 2 {
		t.Fatalf("expected NTuple RHS with 2 elems, got %v with %d", rhs.Kind, len(rhs.Children))
	}
}

func TestBind_ListDestructure(t *testing.T) {
	node := firstChild(t, "[a, b, c] = [10, 20, 30]")
	if node.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", node.Kind)
	}
	lhs := node.Children[0]
	if lhs.Kind != ast.NList {
		t.Fatalf("expected NList LHS, got %v", lhs.Kind)
	}
	if len(lhs.Children) != 3 {
		t.Fatalf("expected 3 elements in LHS, got %d", len(lhs.Children))
	}
}

func TestBind_HeadTail(t *testing.T) {
	node := firstChild(t, "[first | rest] = [1, 2, 3, 4]")
	if node.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", node.Kind)
	}
	lhs := node.Children[0]
	if lhs.Kind != ast.NCons {
		t.Fatalf("expected NCons LHS for [h|t], got %v", lhs.Kind)
	}
	if len(lhs.Children) != 2 {
		t.Fatalf("expected 2 children (head, tail) in cons, got %d", len(lhs.Children))
	}
	if lhs.Children[0].Tok.Val != "first" {
		t.Errorf("expected head 'first', got %q", lhs.Children[0].Tok.Val)
	}
	if lhs.Children[1].Tok.Val != "rest" {
		t.Errorf("expected tail 'rest', got %q", lhs.Children[1].Tok.Val)
	}
}

func TestBind_MultiHeadTail(t *testing.T) {
	node := firstChild(t, "[a, b | rest] = [1, 2, 3, 4, 5]")
	if node.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", node.Kind)
	}
	lhs := node.Children[0]
	if lhs.Kind != ast.NCons {
		t.Fatalf("expected NCons LHS, got %v", lhs.Kind)
	}
	// NCons children: heads... then tail as last child
	if len(lhs.Children) != 3 {
		t.Fatalf("expected 3 children (a, b, rest) in multi-head cons, got %d", len(lhs.Children))
	}
}

func TestBind_Wildcard(t *testing.T) {
	node := firstChild(t, `{_, value} = {:error, "msg"}`)
	if node.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", node.Kind)
	}
	lhs := node.Children[0]
	if lhs.Kind != ast.NTuple || len(lhs.Children) != 2 {
		t.Fatalf("expected NTuple LHS with 2 elems, got %v with %d", lhs.Kind, len(lhs.Children))
	}
	// _ should be an identifier (wildcard is semantic, not syntactic)
	if lhs.Children[0].Tok.Val != "_" {
		t.Errorf("expected wildcard _, got %q", lhs.Children[0].Tok.Val)
	}
}

func TestBind_NestedDestructure(t *testing.T) {
	node := firstChild(t, "{a, {b, c}} = {1, {2, 3}}")
	if node.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", node.Kind)
	}
	lhs := node.Children[0]
	if lhs.Kind != ast.NTuple || len(lhs.Children) != 2 {
		t.Fatalf("expected NTuple with 2 elems, got %v with %d", lhs.Kind, len(lhs.Children))
	}
	inner := lhs.Children[1]
	if inner.Kind != ast.NTuple || len(inner.Children) != 2 {
		t.Fatalf("expected nested NTuple with 2 elems, got %v with %d", inner.Kind, len(inner.Children))
	}
}

// =====================================================================
// TIER 3: String interpolation and nested expansions
//
// These test that the lexer segments interpolated strings and the
// parser produces structured expansion nodes.
// =====================================================================

func TestInterp_DoubleQuoteVarExpand(t *testing.T) {
	// "hello $name" — must produce NInterpStr with segments, NOT a flat string
	node := firstChild(t, `echo "hello $name"`)
	if node.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", node.Kind)
	}
	arg := node.Children[1]
	if arg.Kind != ast.NInterpStr {
		t.Fatalf("expected NInterpStr for interpolated string, got %v (val=%q)", arg.Kind, arg.Tok.Val)
	}
}

func TestInterp_HashBraceExpr(t *testing.T) {
	// "result: #{1 + 2}" — must produce NInterpStr with expression child
	node := firstChild(t, `echo "result: #{1 + 2}"`)
	if node.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", node.Kind)
	}
	arg := node.Children[1]
	if arg.Kind != ast.NInterpStr {
		t.Fatalf("expected NInterpStr, got %v", arg.Kind)
	}
}

func TestInterp_NestedCmdSub(t *testing.T) {
	// echo $(echo $(echo deep)) — nested NCmdSub
	node := firstChild(t, "echo $(echo $(echo deep))")
	if node.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", node.Kind)
	}
	outer := node.Children[1]
	if outer.Kind != ast.NCmdSub {
		t.Fatalf("expected NCmdSub, got %v", outer.Kind)
	}
	// The inner $(echo deep) should be inside the outer's pipeline
	if len(outer.Children) == 0 {
		t.Fatal("expected children in outer NCmdSub")
	}
	innerApply := outer.Children[0]
	if innerApply.Kind != ast.NApply {
		t.Fatalf("expected NApply inside outer cmdsub, got %v", innerApply.Kind)
	}
	innerCmdSub := innerApply.Children[1]
	if innerCmdSub.Kind != ast.NCmdSub {
		t.Fatalf("expected nested NCmdSub, got %v", innerCmdSub.Kind)
	}
}

func TestInterp_CmdSubInBinding(t *testing.T) {
	node := firstChild(t, "x = $(echo hello)")
	if node.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", node.Kind)
	}
	rhs := node.Children[1]
	if rhs.Kind != ast.NCmdSub {
		t.Fatalf("expected NCmdSub on RHS, got %v", rhs.Kind)
	}
	if len(rhs.Children) == 0 {
		t.Fatal("expected children in NCmdSub")
	}
	inner := rhs.Children[0]
	if inner.Kind != ast.NApply {
		t.Fatalf("expected NApply inside cmdsub, got %v", inner.Kind)
	}
}

func TestInterp_ParamExpand_Default(t *testing.T) {
	// echo ${X:-default} — must produce NParamExpand with var, operator, default
	node := firstChild(t, "echo ${X:-default}")
	if node.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", node.Kind)
	}
	pe := node.Children[1]
	if pe.Kind != ast.NParamExpand {
		t.Fatalf("expected NParamExpand, got %v", pe.Kind)
	}
	// TODO: when param expansion is structured, check var="X", op=":-", default="default"
	// For now, verify the raw content at least contains the right text
	if pe.Tok.Val == "" {
		t.Error("expected non-empty param expand content")
	}
}

func TestInterp_ParamExpand_Length(t *testing.T) {
	node := firstChild(t, "echo ${#X}")
	if node.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", node.Kind)
	}
	pe := node.Children[1]
	if pe.Kind != ast.NParamExpand {
		t.Fatalf("expected NParamExpand, got %v", pe.Kind)
	}
}

func TestInterp_ArithExpand(t *testing.T) {
	node := firstChild(t, "echo $((2 + 3))")
	if node.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", node.Kind)
	}
	arith := node.Children[1]
	// Should have an expression child representing 2 + 3
	if len(arith.Children) == 0 {
		t.Fatal("expected arithmetic expansion to have expression child")
	}
}

func TestInterp_ExprInCmdSubInStringInBinding(t *testing.T) {
	// x = "prefix $(echo ${name:-world}) suffix"
	// Must parse without error; string must be NInterpStr
	node := firstChild(t, `x = "prefix $(echo ${name:-world}) suffix"`)
	if node.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", node.Kind)
	}
	// TODO: when string interpolation is implemented, verify NInterpStr with segments
}

// =====================================================================
// TIER 4: POSIX control flow
// =====================================================================

func TestPOSIX_IfThenFi(t *testing.T) {
	node := firstChild(t, "if true; then echo yes; fi")
	if node.Kind != ast.NIf {
		t.Fatalf("expected NIf, got %v", node.Kind)
	}
	if len(node.Clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(node.Clauses))
	}
	body := node.Clauses[0].Body
	if body.Kind != ast.NBlock || len(body.Children) != 1 {
		t.Fatalf("expected body block with 1 stmt, got %v with %d", body.Kind, len(body.Children))
	}
}

func TestPOSIX_IfElseFi(t *testing.T) {
	node := firstChild(t, "if false; then\necho yes\nelse\necho no\nfi")
	if node.Kind != ast.NIf {
		t.Fatalf("expected NIf, got %v", node.Kind)
	}
	if len(node.Clauses) != 2 {
		t.Fatalf("expected 2 clauses (then + else), got %d", len(node.Clauses))
	}
	// First clause has condition, second (else) has nil pattern
	if node.Clauses[0].Pattern == nil {
		t.Error("first clause should have a condition")
	}
	if node.Clauses[1].Pattern != nil {
		t.Error("else clause should have nil pattern")
	}
}

func TestPOSIX_IfElifFi(t *testing.T) {
	node := firstChild(t, "if false; then\necho a\nelif true; then\necho b\nfi")
	if node.Kind != ast.NIf {
		t.Fatalf("expected NIf, got %v", node.Kind)
	}
	if len(node.Clauses) < 2 {
		t.Fatalf("expected at least 2 clauses (if + elif), got %d", len(node.Clauses))
	}
}

func TestPOSIX_ForDone(t *testing.T) {
	node := firstChild(t, "for i in a b c; do\necho $i\ndone")
	if node.Kind != ast.NFor {
		t.Fatalf("expected NFor, got %v", node.Kind)
	}
	if node.Tok.Val != "i" {
		t.Errorf("expected loop var 'i', got %q", node.Tok.Val)
	}
	// Children: [var_ident, word_list, body]
	if len(node.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(node.Children))
	}
	wordList := node.Children[1]
	if wordList.Kind != ast.NList || len(wordList.Children) != 3 {
		t.Fatalf("expected word list with 3 items, got %v with %d", wordList.Kind, len(wordList.Children))
	}
}

func TestPOSIX_WhileDone(t *testing.T) {
	node := firstChild(t, "while true; do\necho loop\ndone")
	if node.Kind != ast.NWhile {
		t.Fatalf("expected NWhile, got %v", node.Kind)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children (cond, body), got %d", len(node.Children))
	}
}

func TestPOSIX_CaseEsac(t *testing.T) {
	node := firstChild(t, "case $X in\nhello)\necho matched\n;;\n*)\necho default\n;;\nesac")
	if node.Kind != ast.NCase {
		t.Fatalf("expected NCase, got %v", node.Kind)
	}
	if len(node.Clauses) != 2 {
		t.Fatalf("expected 2 clauses, got %d", len(node.Clauses))
	}
	if node.Clauses[0].Pattern.Tok.Val != "hello" {
		t.Errorf("expected first pattern 'hello', got %q", node.Clauses[0].Pattern.Tok.Val)
	}
	if node.Clauses[1].Pattern.Tok.Val != "*" {
		t.Errorf("expected second pattern '*', got %q", node.Clauses[1].Pattern.Tok.Val)
	}
}

func TestPOSIX_CasePipeAlternation(t *testing.T) {
	node := firstChild(t, "case $X in\na|b)\necho matched\n;;\nesac")
	if node.Kind != ast.NCase {
		t.Fatalf("expected NCase, got %v", node.Kind)
	}
	if len(node.Clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(node.Clauses))
	}
	// Pattern should contain the alternation
	if node.Clauses[0].Pattern.Tok.Val != "a|b" {
		t.Errorf("expected pattern 'a|b', got %q", node.Clauses[0].Pattern.Tok.Val)
	}
}

func TestPOSIX_NestedIf(t *testing.T) {
	node := firstChild(t, "if true; then\nif false; then\necho inner\nelse\necho outer\nfi\nfi")
	if node.Kind != ast.NIf {
		t.Fatalf("expected NIf, got %v", node.Kind)
	}
	// The body of the first clause should contain another NIf
	innerBody := node.Clauses[0].Body
	if innerBody.Kind != ast.NBlock || len(innerBody.Children) == 0 {
		t.Fatalf("expected body with children, got %v", innerBody.Kind)
	}
	innerIf := innerBody.Children[0]
	if innerIf.Kind != ast.NIf {
		t.Fatalf("expected nested NIf, got %v", innerIf.Kind)
	}
}

func TestPOSIX_NestedFor(t *testing.T) {
	node := firstChild(t, "for i in a b; do\nfor j in 1 2; do\necho $i$j\ndone\ndone")
	if node.Kind != ast.NFor {
		t.Fatalf("expected NFor, got %v", node.Kind)
	}
	body := node.Children[2]
	if body.Kind != ast.NBlock || len(body.Children) == 0 {
		t.Fatalf("expected body block with children")
	}
	innerFor := body.Children[0]
	if innerFor.Kind != ast.NFor {
		t.Fatalf("expected nested NFor, got %v", innerFor.Kind)
	}
}

// =====================================================================
// TIER 5: ish control flow
// =====================================================================

func TestIsh_IfDoEnd(t *testing.T) {
	node := firstChild(t, "if (x > 5) do\necho big\nend")
	if node.Kind != ast.NIf {
		t.Fatalf("expected NIf, got %v", node.Kind)
	}
	cond := node.Clauses[0].Pattern
	if cond.Kind != ast.NBinOp || cond.Tok.Type != ast.TGt {
		t.Errorf("expected > comparison in condition, got kind=%v tok=%v", cond.Kind, cond.Tok.Type)
	}
}

func TestIsh_IfExprCondition(t *testing.T) {
	node := firstChild(t, "if x == 5 do\necho yes\nend")
	if node.Kind != ast.NIf {
		t.Fatalf("expected NIf, got %v", node.Kind)
	}
	cond := node.Clauses[0].Pattern
	if cond.Kind != ast.NBinOp || cond.Tok.Type != ast.TEqEq {
		t.Errorf("expected == comparison, got kind=%v tok=%v", cond.Kind, cond.Tok.Type)
	}
}

func TestIsh_IfDoElseEnd(t *testing.T) {
	node := firstChild(t, "if false do\necho yes\nelse\necho no\nend")
	if node.Kind != ast.NIf {
		t.Fatalf("expected NIf, got %v", node.Kind)
	}
	if len(node.Clauses) != 2 {
		t.Fatalf("expected 2 clauses, got %d", len(node.Clauses))
	}
}

func TestIsh_FnDef(t *testing.T) {
	node := firstChild(t, "fn add a b do\na + b\nend")
	if node.Kind != ast.NFnDef {
		t.Fatalf("expected NFnDef, got %v", node.Kind)
	}
	if node.Tok.Val != "add" {
		t.Errorf("expected fn name 'add', got %q", node.Tok.Val)
	}
	// Children: name_ident, param_a, param_b, body
	if len(node.Children) < 4 {
		t.Fatalf("expected at least 4 children (name, 2 params, body), got %d", len(node.Children))
	}
}

func TestIsh_FnMultiClause(t *testing.T) {
	block := mustParse(t, "fn fib 0 do\n0\nend\nfn fib 1 do\n1\nend\nfn fib n do\nn\nend")
	if len(block.Children) != 3 {
		t.Fatalf("expected 3 fn definitions, got %d", len(block.Children))
	}
	for i, child := range block.Children {
		if child.Kind != ast.NFnDef {
			t.Errorf("child %d: expected NFnDef, got %v", i, child.Kind)
		}
		if child.Tok.Val != "fib" {
			t.Errorf("child %d: expected fn name 'fib', got %q", i, child.Tok.Val)
		}
	}
}

func TestIsh_FnWithGuard(t *testing.T) {
	node := firstChild(t, "fn abs n when n < 0 do\n0 - n\nend")
	if node.Kind != ast.NFnDef {
		t.Fatalf("expected NFnDef, got %v", node.Kind)
	}
	if node.Tok.Val != "abs" {
		t.Errorf("expected fn name 'abs', got %q", node.Tok.Val)
	}
}

func TestIsh_FnMultiClauseBlock(t *testing.T) {
	node := firstChild(t, "fn classify do\n0 -> :zero\n1 -> :one\n_ -> :other\nend")
	if node.Kind != ast.NFnDef {
		t.Fatalf("expected NFnDef, got %v", node.Kind)
	}
	if len(node.Clauses) != 3 {
		t.Fatalf("expected 3 clauses, got %d", len(node.Clauses))
	}
}

func TestIsh_AnonFn(t *testing.T) {
	node := firstChild(t, "f = fn a b do\na + b\nend")
	if node.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", node.Kind)
	}
	rhs := node.Children[1]
	if rhs.Kind != ast.NFnDef {
		t.Fatalf("expected NFnDef on RHS, got %v", rhs.Kind)
	}
}

func TestIsh_Match(t *testing.T) {
	node := firstChild(t, "r = match x do\n1 -> :one\n2 -> :two\n_ -> :other\nend")
	if node.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", node.Kind)
	}
	m := node.Children[1]
	if m.Kind != ast.NMatch {
		t.Fatalf("expected NMatch, got %v", m.Kind)
	}
	if len(m.Clauses) != 3 {
		t.Fatalf("expected 3 match clauses, got %d", len(m.Clauses))
	}
}

func TestIsh_MatchTuplePatterns(t *testing.T) {
	node := firstChild(t, "match result do\n{:ok, val} -> val\n{:error, msg} -> msg\nend")
	if node.Kind != ast.NMatch {
		t.Fatalf("expected NMatch, got %v", node.Kind)
	}
	if len(node.Clauses) != 2 {
		t.Fatalf("expected 2 clauses, got %d", len(node.Clauses))
	}
	p1 := node.Clauses[0].Pattern
	if p1.Kind != ast.NTuple {
		t.Errorf("expected NTuple pattern, got %v", p1.Kind)
	}
}

func TestIsh_ZeroArityLambda(t *testing.T) {
	node := firstChild(t, `f = \ -> 42`)
	if node.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", node.Kind)
	}
	rhs := node.Children[1]
	if rhs.Kind != ast.NLambda {
		t.Fatalf("expected NLambda, got %v", rhs.Kind)
	}
	// Zero params + body = 1 child
	if len(rhs.Children) != 1 {
		t.Fatalf("expected 1 child (body only), got %d", len(rhs.Children))
	}
}

// =====================================================================
// TIER 6: POSIX function definitions
// =====================================================================

func TestPOSIX_FnDef(t *testing.T) {
	node := firstChild(t, "greet() { echo hello; }")
	if node.Kind != ast.NFnDef {
		t.Fatalf("expected NFnDef, got %v", node.Kind)
	}
	if node.Tok.Val != "greet" {
		t.Errorf("expected fn name 'greet', got %q", node.Tok.Val)
	}
}

func TestPOSIX_FnDefMultiline(t *testing.T) {
	node := firstChild(t, "greet()\n{ echo hello; }")
	if node.Kind != ast.NFnDef {
		t.Fatalf("expected NFnDef, got %v", node.Kind)
	}
}

func TestPOSIX_FnWithArgs(t *testing.T) {
	block := mustParse(t, "greet() { echo hello $1; }\ngreet world")
	if len(block.Children) != 2 {
		t.Fatalf("expected 2 statements (def + call), got %d", len(block.Children))
	}
	if block.Children[0].Kind != ast.NFnDef {
		t.Errorf("first statement should be NFnDef, got %v", block.Children[0].Kind)
	}
}

// =====================================================================
// TIER 7: POSIX assignments
// =====================================================================

func TestPOSIX_SimpleAssign(t *testing.T) {
	block := mustParse(t, "X=hello; echo $X")
	if len(block.Children) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(block.Children))
	}
	assign := block.Children[0]
	if assign.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", assign.Kind)
	}
	if assign.Tok.Val != "X" {
		t.Errorf("expected var 'X', got %q", assign.Tok.Val)
	}
}

func TestPOSIX_AssignEmpty(t *testing.T) {
	block := mustParse(t, "X=; echo $X")
	assign := block.Children[0]
	if assign.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", assign.Kind)
	}
	rhs := assign.Children[1]
	if rhs.Tok.Val != "" {
		t.Errorf("expected empty string RHS, got %q", rhs.Tok.Val)
	}
}

func TestPOSIX_AssignCmdSub(t *testing.T) {
	block := mustParse(t, "X=$(echo hi); echo $X")
	assign := block.Children[0]
	if assign.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", assign.Kind)
	}
	rhs := assign.Children[1]
	if rhs.Kind != ast.NCmdSub {
		t.Fatalf("expected NCmdSub RHS, got %v", rhs.Kind)
	}
}

func TestPOSIX_MultipleAssign(t *testing.T) {
	block := mustParse(t, "A=1; B=2; C=3; echo $A $B $C")
	if len(block.Children) != 4 {
		t.Fatalf("expected 4 statements, got %d", len(block.Children))
	}
}

// =====================================================================
// TIER 8: Pipelines, and-or lists, background
// =====================================================================

func TestPipeline_Simple(t *testing.T) {
	node := firstChild(t, "echo hello | cat")
	if node.Kind != ast.NPipe {
		t.Fatalf("expected NPipe, got %v", node.Kind)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(node.Children))
	}
}

func TestPipeline_ThreeStage(t *testing.T) {
	node := firstChild(t, "echo abc | cat | cat")
	if node.Kind != ast.NPipe {
		t.Fatalf("expected NPipe, got %v", node.Kind)
	}
	// Left-associative: (echo|cat)|cat
	if node.Children[0].Kind != ast.NPipe {
		t.Fatalf("expected nested NPipe on left, got %v", node.Children[0].Kind)
	}
}

func TestPipeline_Arrow(t *testing.T) {
	node := firstChild(t, "data |> double |> inc")
	if node.Kind != ast.NPipeFn {
		t.Fatalf("expected NPipeFn, got %v", node.Kind)
	}
}

func TestPipeline_ArrowLambda(t *testing.T) {
	bind := firstChild(t, `r = 42 |> \x -> x + 1`)
	rhs := bind.Children[1]
	if rhs.Kind != ast.NPipeFn {
		t.Fatalf("expected NPipeFn, got %v", rhs.Kind)
	}
}

func TestAndList(t *testing.T) {
	node := firstChild(t, "true && echo yes")
	if node.Kind != ast.NAndList {
		t.Fatalf("expected NAndList, got %v", node.Kind)
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(node.Children))
	}
}

func TestOrList(t *testing.T) {
	node := firstChild(t, "false || echo fallback")
	if node.Kind != ast.NOrList {
		t.Fatalf("expected NOrList, got %v", node.Kind)
	}
}

func TestAndOrMixed(t *testing.T) {
	node := firstChild(t, "false && echo no || echo fallback")
	// Should be: (false && echo no) || echo fallback
	if node.Kind != ast.NOrList {
		t.Fatalf("expected NOrList at top, got %v", node.Kind)
	}
	if node.Children[0].Kind != ast.NAndList {
		t.Fatalf("expected NAndList as left child, got %v", node.Children[0].Kind)
	}
}

func TestBackground(t *testing.T) {
	node := firstChild(t, "echo bg &")
	if node.Kind != ast.NBg {
		t.Fatalf("expected NBg, got %v", node.Kind)
	}
}

// =====================================================================
// TIER 9: Keywords as command arguments
// =====================================================================

func TestKeywordsAsArgs(t *testing.T) {
	tests := []struct {
		input    string
		argCount int
	}{
		{"echo if then else fi", 5},
		{"echo for in do done", 5},
		{"echo while do done", 4},
		{"echo case in esac", 4},
		{"echo fn end match", 4},
		{"echo true false nil", 4},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			node := firstChild(t, tt.input)
			if node.Kind != ast.NApply {
				t.Fatalf("expected NApply, got %v", node.Kind)
			}
			if len(node.Children) != tt.argCount {
				t.Errorf("expected %d children, got %d", tt.argCount, len(node.Children))
			}
		})
	}
}

// =====================================================================
// TIER 10: Filenames, paths, dotted names
// =====================================================================

func TestFilenames(t *testing.T) {
	tests := []struct {
		input string
		arg   string
	}{
		{"echo file.txt", "file.txt"},
		{"echo archive.tar.gz", "archive.tar.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			node := firstChild(t, tt.input)
			if node.Kind != ast.NApply || len(node.Children) != 2 {
				t.Fatalf("expected NApply with 2 children, got %v with %d", node.Kind, len(node.Children))
			}
			arg := node.Children[1]
			if arg.Tok.Val != tt.arg {
				t.Errorf("expected arg %q, got %q", tt.arg, arg.Tok.Val)
			}
		})
	}
}

func TestFilenames_IPAddress(t *testing.T) {
	node := firstChild(t, "echo 192.168.1.120")
	if node.Kind != ast.NApply || len(node.Children) != 2 {
		t.Fatalf("expected NApply with 2 children, got %v with %d", node.Kind, len(node.Children))
	}
	arg := node.Children[1]
	if arg.Kind != ast.NIPv4 {
		t.Fatalf("expected NIPv4, got %v", arg.Kind)
	}
	if arg.Tok.Val != "192.168.1.120" {
		t.Errorf("expected '192.168.1.120', got %q", arg.Tok.Val)
	}
}

func TestFilenames_Dotfile(t *testing.T) {
	node := firstChild(t, "echo .gitignore")
	if node.Kind != ast.NApply || len(node.Children) != 2 {
		t.Fatalf("expected NApply with 2 children, got %v with %d", node.Kind, len(node.Children))
	}
	arg := node.Children[1]
	if arg.Tok.Val != ".gitignore" {
		t.Errorf("expected '.gitignore', got %q", arg.Tok.Val)
	}
}

func TestFilenames_IPv6(t *testing.T) {
	tests := []struct {
		input string
		addr  string
	}{
		{"echo ::1", "::1"},
		{"echo fe80::1", "fe80::1"},
		{"echo 2001:db8::1", "2001:db8::1"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			node := firstChild(t, tt.input)
			if node.Kind != ast.NApply || len(node.Children) != 2 {
				t.Fatalf("expected NApply with 2 children, got %v with %d", node.Kind, len(node.Children))
			}
			arg := node.Children[1]
			if arg.Kind != ast.NIPv6 {
				t.Fatalf("expected NIPv6, got %v", arg.Kind)
			}
			if arg.Tok.Val != tt.addr {
				t.Errorf("expected %q, got %q", tt.addr, arg.Tok.Val)
			}
		})
	}
}

func TestFilenames_AbsolutePath(t *testing.T) {
	node := firstChild(t, "echo /usr/local/bin")
	if node.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", node.Kind)
	}
}

// =====================================================================
// TIER 11: Redirections
// =====================================================================

func TestRedirect_Stdout(t *testing.T) {
	cmd := firstChild(t, "echo hello > /dev/null")
	if len(cmd.Redirs) != 1 || cmd.Redirs[0].Op != ast.TGt || cmd.Redirs[0].Fd != 1 {
		t.Fatalf("expected stdout redirect")
	}
}

func TestRedirect_Stdin(t *testing.T) {
	cmd := firstChild(t, "sort < input.txt")
	if len(cmd.Redirs) != 1 || cmd.Redirs[0].Op != ast.TLt || cmd.Redirs[0].Fd != 0 {
		t.Fatalf("expected stdin redirect")
	}
}

func TestRedirect_Append(t *testing.T) {
	cmd := firstChild(t, "echo hello >> log")
	if len(cmd.Redirs) != 1 || cmd.Redirs[0].Op != ast.TAppend {
		t.Fatalf("expected append redirect")
	}
}

func TestRedirect_StderrToDevNull(t *testing.T) {
	cmd := firstChild(t, "echo visible 2>/dev/null")
	if len(cmd.Redirs) != 1 || cmd.Redirs[0].Fd != 2 {
		t.Fatalf("expected fd 2 redirect, got %d redirs", len(cmd.Redirs))
	}
}

// =====================================================================
// TIER 12: Subshell and negation
// =====================================================================

func TestSubshell(t *testing.T) {
	node := firstChild(t, "(echo hello)")
	if node.Kind != ast.NSubshell {
		t.Fatalf("expected NSubshell, got %v", node.Kind)
	}
}

func TestSubshellVarIsolation(t *testing.T) {
	block := mustParse(t, "X=before\n(X=after)\necho $X")
	if len(block.Children) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(block.Children))
	}
}

func TestNegation(t *testing.T) {
	node := firstChild(t, "! false")
	if node.Kind != ast.NUnary || node.Tok.Type != ast.TBang {
		t.Fatalf("expected ! unary, got %v", node.Kind)
	}
}

func TestNegationInIf(t *testing.T) {
	node := firstChild(t, "if ! false; then echo yes; fi")
	if node.Kind != ast.NIf {
		t.Fatalf("expected NIf, got %v", node.Kind)
	}
	cond := node.Clauses[0].Pattern
	if cond.Kind != ast.NUnary || cond.Tok.Type != ast.TBang {
		t.Fatalf("expected ! in condition, got %v", cond.Kind)
	}
}

// =====================================================================
// TIER 13: Heredocs and herestrings
// =====================================================================

func TestHeredoc_Basic(t *testing.T) {
	node := firstChild(t, "cat <<EOF\nhello world\nEOF")
	if node.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", node.Kind)
	}
	// TODO: verify heredoc content is "hello world\n" when properly implemented
}

func TestHeredoc_QuotedDelimiter(t *testing.T) {
	// Quoted delimiter means no expansion
	node := firstChild(t, "cat <<'EOF'\n$X should not expand\nEOF")
	if node.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", node.Kind)
	}
	// TODO: verify quoted flag is set on heredoc node
}

func TestHerestring(t *testing.T) {
	node := firstChild(t, "cat <<<hello")
	if node.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", node.Kind)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child (cat), got %d", len(node.Children))
	}
	if len(node.Redirs) != 1 {
		t.Fatalf("expected 1 redirect (herestring), got %d", len(node.Redirs))
	}
}

func TestHerestring_Var(t *testing.T) {
	block := mustParse(t, "X=hi; cat <<<$X")
	if len(block.Children) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(block.Children))
	}
}

// =====================================================================
// TIER 14: Data structures
// =====================================================================

func TestList_Empty(t *testing.T) {
	bind := firstChild(t, "x = []")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NList {
		t.Fatalf("expected NList, got %v", rhs.Kind)
	}
	if len(rhs.Children) != 0 {
		t.Errorf("expected empty list, got %d elements", len(rhs.Children))
	}
}

func TestList_Nested(t *testing.T) {
	bind := firstChild(t, "x = [[1, 2], [3, 4]]")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NList || len(rhs.Children) != 2 {
		t.Fatalf("expected NList with 2 children, got %v with %d", rhs.Kind, len(rhs.Children))
	}
	for i, child := range rhs.Children {
		if child.Kind != ast.NList {
			t.Errorf("child %d: expected NList, got %v", i, child.Kind)
		}
	}
}

func TestList_ConsConstruction(t *testing.T) {
	bind := firstChild(t, "x = [1 | [2, 3]]")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NCons {
		t.Fatalf("expected NCons for [h|t] construction, got %v", rhs.Kind)
	}
	if len(rhs.Children) != 2 {
		t.Fatalf("expected 2 children (head, tail), got %d", len(rhs.Children))
	}
}

func TestTuple_OkNil(t *testing.T) {
	bind := firstChild(t, "x = {:ok, nil}")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NTuple || len(rhs.Children) != 2 {
		t.Fatalf("expected NTuple with 2 elems, got %v with %d", rhs.Kind, len(rhs.Children))
	}
}

func TestTuple_ErrorCode(t *testing.T) {
	bind := firstChild(t, "x = {:error, 42}")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NTuple || len(rhs.Children) != 2 {
		t.Fatalf("expected NTuple with 2 elems, got %v with %d", rhs.Kind, len(rhs.Children))
	}
	if rhs.Children[0].Kind != ast.NAtom || rhs.Children[0].Tok.Val != "error" {
		t.Errorf("expected :error, got %v %q", rhs.Children[0].Kind, rhs.Children[0].Tok.Val)
	}
}

func TestTuple_MixedTypes(t *testing.T) {
	bind := firstChild(t, `x = {:ok, 42, "hello"}`)
	rhs := bind.Children[1]
	if rhs.Kind != ast.NTuple || len(rhs.Children) != 3 {
		t.Fatalf("expected NTuple with 3 elems, got %v with %d", rhs.Kind, len(rhs.Children))
	}
}

func TestMap_Basic(t *testing.T) {
	bind := firstChild(t, `x = %{name: "alice", age: 30}`)
	rhs := bind.Children[1]
	if rhs.Kind != ast.NMap {
		t.Fatalf("expected NMap, got %v", rhs.Kind)
	}
	if len(rhs.Children) != 4 {
		t.Fatalf("expected 4 children (2 key-value pairs interleaved), got %d", len(rhs.Children))
	}
}

func TestMap_Access(t *testing.T) {
	bind := firstChild(t, "x = config.host")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NAccess {
		t.Fatalf("expected NAccess, got %v", rhs.Kind)
	}
	if rhs.Tok.Val != "host" {
		t.Errorf("expected field 'host', got %q", rhs.Tok.Val)
	}
}

// =====================================================================
// TIER 15: Arithmetic and operator precedence
// =====================================================================

func TestArith_Precedence(t *testing.T) {
	bind := firstChild(t, "r = 2 + 3 * 4")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NBinOp || rhs.Tok.Type != ast.TPlus {
		t.Fatalf("expected + at top, got %v %v", rhs.Kind, rhs.Tok.Val)
	}
	right := rhs.Children[1]
	if right.Kind != ast.NBinOp || right.Tok.Type != ast.TStar {
		t.Fatalf("expected * as right child of +, got %v %v", right.Kind, right.Tok.Val)
	}
}

func TestArith_ParenGrouping(t *testing.T) {
	bind := firstChild(t, "r = (2 + 3) * 4")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NBinOp || rhs.Tok.Type != ast.TStar {
		t.Fatalf("expected * at top (parens override precedence), got %v %v", rhs.Kind, rhs.Tok.Val)
	}
}

func TestArith_UnaryNeg(t *testing.T) {
	bind := firstChild(t, "r = -42")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NUnary || rhs.Tok.Type != ast.TMinus {
		t.Fatalf("expected unary -, got %v", rhs.Kind)
	}
}

func TestArith_BooleanNot(t *testing.T) {
	bind := firstChild(t, "r = (!true)")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NUnary || rhs.Tok.Type != ast.TBang {
		t.Fatalf("expected unary !, got %v", rhs.Kind)
	}
}

func TestArith_Float(t *testing.T) {
	bind := firstChild(t, "r = 3.14 * 2")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NBinOp || rhs.Tok.Type != ast.TStar {
		t.Fatalf("expected * binop, got %v", rhs.Kind)
	}
	if rhs.Children[0].Tok.Val != "3.14" {
		t.Errorf("expected left operand 3.14, got %q", rhs.Children[0].Tok.Val)
	}
}

func TestArith_StringConcat(t *testing.T) {
	bind := firstChild(t, `r = "hello" + " " + "world"`)
	rhs := bind.Children[1]
	if rhs.Kind != ast.NBinOp || rhs.Tok.Type != ast.TPlus {
		t.Fatalf("expected + binop, got %v", rhs.Kind)
	}
}

// =====================================================================
// TIER 16: Function calls
// =====================================================================

func TestCall_ParenDelimited(t *testing.T) {
	bind := firstChild(t, "r = add(3, 4)")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NCall {
		t.Fatalf("expected NCall, got %v", rhs.Kind)
	}
	// callee + 2 args = 3 children
	if len(rhs.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(rhs.Children))
	}
	if rhs.Children[0].Tok.Val != "add" {
		t.Errorf("expected callee 'add', got %q", rhs.Children[0].Tok.Val)
	}
}

func TestCall_BareArgs(t *testing.T) {
	bind := firstChild(t, "r = double 5")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NApply {
		t.Fatalf("expected NApply for bare call, got %v", rhs.Kind)
	}
	if len(rhs.Children) != 2 {
		t.Fatalf("expected 2 children (fn, arg), got %d", len(rhs.Children))
	}
}

func TestCall_ModuleQualified(t *testing.T) {
	bind := firstChild(t, "r = List.map [1, 2, 3], f")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", rhs.Kind)
	}
	head := rhs.Children[0]
	if head.Kind != ast.NAccess || head.Tok.Val != "map" {
		t.Errorf("expected NAccess 'map', got kind=%v val=%q", head.Kind, head.Tok.Val)
	}
}

func TestCall_LambdaInPipeArrow(t *testing.T) {
	bind := firstChild(t, `r = [1, 2, 3] |> List.map \x -> x * 2`)
	rhs := bind.Children[1]
	if rhs.Kind != ast.NPipeFn {
		t.Fatalf("expected NPipeFn, got %v", rhs.Kind)
	}
}

func TestCall_FnCapture(t *testing.T) {
	bind := firstChild(t, "f = &greet")
	rhs := bind.Children[1]
	// &greet should be an identifier with & prefix
	if rhs.Kind != ast.NIdent {
		t.Fatalf("expected NIdent for capture, got %v", rhs.Kind)
	}
	if rhs.Tok.Val != "&greet" {
		t.Errorf("expected '&greet', got %q", rhs.Tok.Val)
	}
}

// =====================================================================
// TIER 17: OTP primitives
// =====================================================================

func TestOTP_Spawn(t *testing.T) {
	bind := firstChild(t, "pid = spawn fn do :ok end")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NApply {
		t.Fatalf("expected NApply for spawn, got %v", rhs.Kind)
	}
	if rhs.Children[0].Tok.Val != "spawn" {
		t.Errorf("expected head 'spawn', got %q", rhs.Children[0].Tok.Val)
	}
}

func TestOTP_SpawnAndSend(t *testing.T) {
	node := firstChild(t, "send pid, {:ping, self}")
	if node.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", node.Kind)
	}
	if node.Children[0].Tok.Val != "send" {
		t.Errorf("expected head 'send', got %q", node.Children[0].Tok.Val)
	}
	if len(node.Children) != 3 {
		t.Fatalf("expected 3 children (send, pid, tuple), got %d", len(node.Children))
	}
}

func TestOTP_Receive(t *testing.T) {
	node := firstChild(t, "receive do\n{:ping, sender} -> send sender, :pong\nend")
	if node.Kind != ast.NReceive {
		t.Fatalf("expected NReceive, got %v", node.Kind)
	}
	if len(node.Clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(node.Clauses))
	}
	pat := node.Clauses[0].Pattern
	if pat.Kind != ast.NTuple {
		t.Errorf("expected tuple pattern, got %v", pat.Kind)
	}
}

func TestOTP_ReceiveTimeout(t *testing.T) {
	node := firstChild(t, "receive do\nmsg -> msg\nafter 100 ->\n:timeout\nend")
	if node.Kind != ast.NReceive {
		t.Fatalf("expected NReceive, got %v", node.Kind)
	}
	if len(node.Clauses) != 1 {
		t.Fatalf("expected 1 match clause, got %d", len(node.Clauses))
	}
	if len(node.Children) != 2 {
		t.Fatalf("expected 2 children (timeout, after body), got %d", len(node.Children))
	}
}

func TestOTP_Await(t *testing.T) {
	bind := firstChild(t, "result = await task")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NApply {
		t.Fatalf("expected NApply for await, got %v", rhs.Kind)
	}
}

func TestOTP_Monitor(t *testing.T) {
	bind := firstChild(t, "ref = monitor pid")
	rhs := bind.Children[1]
	if rhs.Kind != ast.NApply {
		t.Fatalf("expected NApply for monitor, got %v", rhs.Kind)
	}
}

func TestOTP_Supervise(t *testing.T) {
	bind := firstChild(t, "sup = supervise :one_for_one do\nworker :greeter fn do\necho started\nend\nend")
	if bind.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", bind.Kind)
	}
}

// =====================================================================
// TIER 18: try/rescue
// =====================================================================

func TestTryRescue(t *testing.T) {
	bind := firstChild(t, "r = try do\n1 / 0\nrescue\n_ -> :caught\nend")
	if bind.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", bind.Kind)
	}
	rhs := bind.Children[1]
	if rhs.Kind != ast.NTry {
		t.Fatalf("expected NTry, got %v", rhs.Kind)
	}
	if len(rhs.Clauses) != 1 {
		t.Fatalf("expected 1 rescue clause, got %d", len(rhs.Clauses))
	}
}

func TestTryRescuePattern(t *testing.T) {
	bind := firstChild(t, "r = try do\n{:ok, val} = {:error, \"bad\"}\nrescue\n{:error, msg} -> msg\nend")
	if bind.Kind != ast.NBind {
		t.Fatalf("expected NBind, got %v", bind.Kind)
	}
}

// =====================================================================
// TIER 19: Modules
// =====================================================================

func TestDefmodule(t *testing.T) {
	node := firstChild(t, "defmodule M do\nfn greet name do\necho hello\nend\nend")
	if node.Kind != ast.NDefModule {
		t.Fatalf("expected NDefModule, got %v", node.Kind)
	}
	if node.Tok.Val != "M" {
		t.Errorf("expected module name 'M', got %q", node.Tok.Val)
	}
}

func TestUse(t *testing.T) {
	node := firstChild(t, "use List")
	if node.Kind != ast.NUseImport {
		t.Fatalf("expected NUseImport, got %v", node.Kind)
	}
	if node.Tok.Val != "use" {
		t.Errorf("expected tok 'use', got %q", node.Tok.Val)
	}
	if node.Children[0].Tok.Val != "List" {
		t.Errorf("expected module 'List', got %q", node.Children[0].Tok.Val)
	}
}

func TestImport(t *testing.T) {
	node := firstChild(t, "import List")
	if node.Kind != ast.NUseImport {
		t.Fatalf("expected NUseImport, got %v", node.Kind)
	}
	if node.Tok.Val != "import" {
		t.Errorf("expected tok 'import', got %q", node.Tok.Val)
	}
}

// =====================================================================
// TIER 20: Special parameters
// =====================================================================

func TestSpecialParam_ExitStatus(t *testing.T) {
	block := mustParse(t, "true; echo $?")
	echo := block.Children[1]
	if echo.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", echo.Kind)
	}
	varRef := echo.Children[1]
	if varRef.Kind != ast.NVarRef || varRef.Tok.Val != "$?" {
		t.Errorf("expected $? var ref, got kind=%v val=%q", varRef.Kind, varRef.Tok.Val)
	}
}

func TestSpecialParam_PID(t *testing.T) {
	node := firstChild(t, "echo $$")
	varRef := node.Children[1]
	if varRef.Kind != ast.NVarRef || varRef.Tok.Val != "$$" {
		t.Errorf("expected $$ var ref, got kind=%v val=%q", varRef.Kind, varRef.Tok.Val)
	}
}

func TestSpecialParam_Args(t *testing.T) {
	node := firstChild(t, "echo $1 $2 $3")
	if node.Kind != ast.NApply {
		t.Fatalf("expected NApply, got %v", node.Kind)
	}
	if len(node.Children) != 4 {
		t.Fatalf("expected 4 children (echo + 3 vars), got %d", len(node.Children))
	}
	for i := 1; i <= 3; i++ {
		ref := node.Children[i]
		if ref.Kind != ast.NVarRef {
			t.Errorf("arg %d: expected NVarRef, got %v", i, ref.Kind)
		}
	}
}

func TestSpecialParam_ArgCount(t *testing.T) {
	node := firstChild(t, "echo $#")
	varRef := node.Children[1]
	if varRef.Kind != ast.NVarRef || varRef.Tok.Val != "$#" {
		t.Errorf("expected $# var ref, got kind=%v val=%q", varRef.Kind, varRef.Tok.Val)
	}
}

func TestSpecialParam_AllArgs(t *testing.T) {
	node := firstChild(t, "echo $@")
	varRef := node.Children[1]
	if varRef.Kind != ast.NVarRef || varRef.Tok.Val != "$@" {
		t.Errorf("expected $@ var ref, got kind=%v val=%q", varRef.Kind, varRef.Tok.Val)
	}
}

// =====================================================================
// TIER 21: Error cases
// =====================================================================

func TestError_UntermIf(t *testing.T) {
	_, err := parse("if true; then echo hi")
	if err == nil {
		t.Error("expected error for unterminated if")
	}
}

func TestError_UntermFor(t *testing.T) {
	_, err := parse("for x in a b; do echo $x")
	if err == nil {
		t.Error("expected error for unterminated for")
	}
}

func TestError_UntermFn(t *testing.T) {
	_, err := parse("fn foo do echo hi")
	if err == nil {
		t.Error("expected error for unterminated fn")
	}
}

func TestError_UntermMatch(t *testing.T) {
	_, err := parse("match x do\n:ok -> echo yes")
	if err == nil {
		t.Error("expected error for unterminated match")
	}
}

func TestError_RedirectNoTarget(t *testing.T) {
	_, err := parse("echo hi >")
	if err == nil {
		t.Error("expected error for redirect without target")
	}
}

func TestError_PipeNoRHS(t *testing.T) {
	_, err := parse("echo hi |")
	if err == nil {
		t.Error("expected error for pipe without RHS")
	}
}

func TestError_StandaloneArrow(t *testing.T) {
	_, err := parse("->")
	if err == nil {
		t.Error("expected error for standalone arrow")
	}
}

// =====================================================================
// TIER 22: Expression extension at statement level
// =====================================================================

func TestExprExtend_Addition(t *testing.T) {
	node := firstChild(t, "a + b")
	if node.Kind != ast.NBinOp || node.Tok.Type != ast.TPlus {
		t.Fatalf("expected + binop, got kind=%v tok=%v", node.Kind, node.Tok.Type)
	}
}

func TestExprExtend_Comparison(t *testing.T) {
	node := firstChild(t, "x == 5")
	if node.Kind != ast.NBinOp || node.Tok.Type != ast.TEqEq {
		t.Fatalf("expected == binop, got kind=%v tok=%v", node.Kind, node.Tok.Type)
	}
}

func TestExprExtend_NotRedirect(t *testing.T) {
	// x > 5 at statement level — > is redirect, NOT comparison
	cmd := firstChild(t, "x > 5")
	if len(cmd.Redirs) == 0 || cmd.Redirs[0].Op != ast.TGt {
		t.Fatalf("expected > as redirect at statement level")
	}
}

// =====================================================================
// TIER 23: Mixed POSIX + ish
// =====================================================================

func TestMixed_POSIXIfWithIshBinding(t *testing.T) {
	block := mustParse(t, "x = 42\nif [ $x -eq 42 ]; then\necho yes\nfi")
	if len(block.Children) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(block.Children))
	}
	if block.Children[0].Kind != ast.NBind {
		t.Errorf("first should be binding, got %v", block.Children[0].Kind)
	}
	if block.Children[1].Kind != ast.NIf {
		t.Errorf("second should be if, got %v", block.Children[1].Kind)
	}
}

func TestMixed_IshFnCalledFromPOSIXFor(t *testing.T) {
	block := mustParse(t, "fn double x do\nx * 2\nend\nfor i in 1 2 3; do\nr = double $i\necho $r\ndone")
	if len(block.Children) != 2 {
		t.Fatalf("expected 2 statements (fn + for), got %d", len(block.Children))
	}
}

func TestMixed_POSIXVarInIshExpr(t *testing.T) {
	block := mustParse(t, "X=10\nr = $X + 5\necho $r")
	if len(block.Children) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(block.Children))
	}
}

func TestMixed_CmdSubInIshBinding(t *testing.T) {
	block := mustParse(t, "x = $(echo 42)\necho $x")
	bind := block.Children[0]
	if bind.Children[1].Kind != ast.NCmdSub {
		t.Fatalf("expected NCmdSub on RHS, got %v", bind.Children[1].Kind)
	}
}

func TestMixed_WhileWithIshBinding(t *testing.T) {
	block := mustParse(t, "n = 3\nwhile [ $n -gt 0 ]; do\necho $n\nn = (n - 1)\ndone")
	if len(block.Children) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(block.Children))
	}
}

// =====================================================================
// TIER 24: Realistic POSIX commands
// =====================================================================

func TestRealistic_Curl(t *testing.T) {
	inputs := []string{
		"curl https://example.com",
		"curl -s -o /dev/null -w '%{http_code}' https://example.com",
		"curl -X POST -H 'Content-Type: application/json' -d '{\"key\":\"val\"}' https://api.example.com",
		"curl -fsSL https://get.docker.com | sh",
		"curl --retry 3 --retry-delay 5 https://example.com",
	}
	for _, input := range inputs {
		t.Run(truncName(input), func(t *testing.T) {
			_, err := parse(input)
			if err != nil {
				t.Errorf("parse error: %v", err)
			}
		})
	}
}

func TestRealistic_Awk(t *testing.T) {
	inputs := []string{
		"echo hello | awk '{print $1}'",
		"awk -F: '{print $1}' /etc/passwd",
		"awk 'NR==1{print}' file.txt",
		"ps aux | awk '{print $2, $11}'",
	}
	for _, input := range inputs {
		t.Run(truncName(input), func(t *testing.T) {
			_, err := parse(input)
			if err != nil {
				t.Errorf("parse error: %v", err)
			}
		})
	}
}

func TestRealistic_Sed(t *testing.T) {
	inputs := []string{
		"sed 's/foo/bar/g' file.txt",
		"sed -i 's/old/new/' file.txt",
		"echo hello | sed 's/hello/world/'",
		"sed -n '1,10p' file.txt",
	}
	for _, input := range inputs {
		t.Run(truncName(input), func(t *testing.T) {
			_, err := parse(input)
			if err != nil {
				t.Errorf("parse error: %v", err)
			}
		})
	}
}

func TestRealistic_Git(t *testing.T) {
	inputs := []string{
		"git status",
		"git commit -m 'initial commit'",
		"git log --oneline --graph",
		"git push origin main",
		"git diff HEAD~1",
		"git stash pop",
		"git remote add origin https://github.com/user/repo.git",
		"git checkout -b feature/new-thing",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, err := parse(input)
			if err != nil {
				t.Errorf("parse error: %v", err)
			}
		})
	}
}

func TestRealistic_Find(t *testing.T) {
	inputs := []string{
		"find . -name '*.go'",
		"find /tmp -type f -mtime +7",
		"find . -name '*.log' -delete",
		"find /usr -type f -executable",
	}
	for _, input := range inputs {
		t.Run(truncName(input), func(t *testing.T) {
			_, err := parse(input)
			if err != nil {
				t.Errorf("parse error: %v", err)
			}
		})
	}
}

func TestRealistic_Grep(t *testing.T) {
	inputs := []string{
		"grep -r 'TODO' src/",
		"grep -n 'func main' *.go",
		"grep -v '^#' config.conf",
		"grep -E 'foo|bar' file.txt",
		"grep -c 'error' /var/log/syslog",
		"ps aux | grep nginx | grep -v grep",
	}
	for _, input := range inputs {
		t.Run(truncName(input), func(t *testing.T) {
			_, err := parse(input)
			if err != nil {
				t.Errorf("parse error: %v", err)
			}
		})
	}
}

func TestRealistic_Docker(t *testing.T) {
	inputs := []string{
		"docker run -d -p 8080:80 nginx",
		"docker ps -a",
		"docker exec -it container_name sh",
		"docker build -t myapp:latest .",
		"docker compose up -d",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, err := parse(input)
			if err != nil {
				t.Errorf("parse error: %v", err)
			}
		})
	}
}

func TestRealistic_SshScp(t *testing.T) {
	inputs := []string{
		"ssh user@host.example.com",
		"ssh -p 2222 user@host 'ls -la'",
		"scp file.txt user@host:/tmp/",
		"scp -r local/ user@host:remote/",
	}
	for _, input := range inputs {
		t.Run(truncName(input), func(t *testing.T) {
			_, err := parse(input)
			if err != nil {
				t.Errorf("parse error: %v", err)
			}
		})
	}
}

func TestRealistic_Tar(t *testing.T) {
	inputs := []string{
		"tar czf archive.tar.gz src/",
		"tar xzf archive.tar.gz",
		"tar tf archive.tar.gz",
		"tar czf - src/ | ssh host 'tar xzf -'",
	}
	for _, input := range inputs {
		t.Run(truncName(input), func(t *testing.T) {
			_, err := parse(input)
			if err != nil {
				t.Errorf("parse error: %v", err)
			}
		})
	}
}

func TestRealistic_MakeGoBuild(t *testing.T) {
	inputs := []string{
		"make -j4",
		"make clean && make build",
		"go build -o myapp ./cmd/myapp",
		"go test ./... -v -count=1",
		"go run main.go",
		"cargo build --release",
		"npm install && npm run build",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, err := parse(input)
			if err != nil {
				t.Errorf("parse error: %v", err)
			}
		})
	}
}

func TestRealistic_ComplexPipelines(t *testing.T) {
	inputs := []string{
		"ps aux | sort -k3 -rn | head -10",
		"cat /etc/passwd | cut -d: -f1 | sort",
		"find . -name '*.go' | xargs wc -l | sort -n | tail -10",
		"ls -la | awk '{print $9}' | grep -v '^$'",
		"du -sh * | sort -h | tail -5",
		"netstat -tlnp 2>/dev/null | grep :80",
	}
	for _, input := range inputs {
		t.Run(truncName(input), func(t *testing.T) {
			_, err := parse(input)
			if err != nil {
				t.Errorf("parse error: %v", err)
			}
		})
	}
}

func TestRealistic_ConditionalExecution(t *testing.T) {
	inputs := []string{
		"test -f /etc/hosts && echo exists",
		"[ -d /tmp ] && echo yes || echo no",
		"command -v git > /dev/null && echo 'git found'",
		"which python3 > /dev/null 2>&1 || echo 'no python3'",
		"mkdir -p /tmp/test && cd /tmp/test && touch file",
	}
	for _, input := range inputs {
		t.Run(truncName(input), func(t *testing.T) {
			_, err := parse(input)
			if err != nil {
				t.Errorf("parse error: %v", err)
			}
		})
	}
}

func TestRealistic_Assignments(t *testing.T) {
	inputs := []string{
		"CC=gcc make",
		"DEBIAN_FRONTEND=noninteractive apt-get install -y curl",
		"PATH=$PATH:/usr/local/go/bin",
		"GOPATH=$HOME/go go build",
	}
	for _, input := range inputs {
		t.Run(truncName(input), func(t *testing.T) {
			_, err := parse(input)
			if err != nil {
				t.Errorf("parse error: %v", err)
			}
		})
	}
}
