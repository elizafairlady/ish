package lexer

import (
	"testing"

	"ish/internal/ast"
)

func types(tokens []ast.Token) []ast.TokenType {
	tt := make([]ast.TokenType, len(tokens))
	for i, t := range tokens {
		tt[i] = t.Type
	}
	return tt
}

func vals(tokens []ast.Token) []string {
	vv := make([]string, len(tokens))
	for i, t := range tokens {
		vv[i] = t.Val
	}
	return vv
}

func TestLexBasicTokens(t *testing.T) {
	tokens := Lex("hello 42 3.14 :ok")
	want := []ast.TokenType{ast.TIdent, ast.TInt, ast.TFloat, ast.TAtom, ast.TEOF}
	got := types(tokens)
	if len(got) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d: got %d, want %d", i, got[i], want[i])
		}
	}
	if tokens[0].Val != "hello" {
		t.Errorf("token 0 val: got %q, want %q", tokens[0].Val, "hello")
	}
	if tokens[3].Val != "ok" {
		t.Errorf("atom val: got %q, want %q", tokens[3].Val, "ok")
	}
}

func TestLexOperators(t *testing.T) {
	tokens := Lex("+ - * / == != >= <= && || |> | > < = ->")
	want := []ast.TokenType{
		ast.TPlus, ast.TMinus, ast.TStar, ast.TSlash,
		ast.TEqEq, ast.TBangEq, ast.TGtEq, ast.TLtEq,
		ast.TAnd, ast.TOr, ast.TPipeArrow, ast.TPipe,
		ast.TGt, ast.TLt, ast.TAssign, ast.TArrow, ast.TEOF,
	}
	got := types(tokens)
	if len(got) != len(want) {
		t.Fatalf("got %d tokens, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d: got %d, want %d (val=%q)", i, got[i], want[i], tokens[i].Val)
		}
	}
}

func TestLexAmbiguousTokensSameOutput(t *testing.T) {
	// The lexer emits the SAME tokens regardless of context.
	// Parser decides whether > is redirect or comparison.
	tokens := Lex("echo hello > file")
	if tokens[2].Type != ast.TGt {
		t.Errorf("> should lex as TGt, got %d", tokens[2].Type)
	}

	tokens2 := Lex("x > 5")
	if tokens2[1].Type != ast.TGt {
		t.Errorf("> should lex as TGt here too, got %d", tokens2[1].Type)
	}
}

func TestLexMultiCharOperators(t *testing.T) {
	tests := []struct {
		input string
		want  ast.TokenType
	}{
		{">>", ast.TAppend},
		{"|>", ast.TPipeArrow},
		{"&&", ast.TAnd},
		{"||", ast.TOr},
		{"==", ast.TEqEq},
		{"!=", ast.TBangEq},
		{">=", ast.TGtEq},
		{"<=", ast.TLtEq},
		{"->", ast.TArrow},
	}
	for _, tt := range tests {
		tokens := Lex(tt.input)
		if tokens[0].Type != tt.want {
			t.Errorf("Lex(%q)[0].Type = %d, want %d", tt.input, tokens[0].Type, tt.want)
		}
	}
}

func TestLexKeywords(t *testing.T) {
	tokens := Lex("fn if else end do match nil true false")
	// All keywords lex as TIdent — the parser matches by value
	for i := 0; i < len(tokens)-1; i++ { // skip EOF
		if tokens[i].Type != ast.TIdent {
			t.Errorf("token %d (%q): expected TIdent, got %d", i, tokens[i].Val, tokens[i].Type)
		}
	}
	wantVals := []string{"fn", "if", "else", "end", "do", "match", "nil", "true", "false"}
	for i, want := range wantVals {
		if tokens[i].Val != want {
			t.Errorf("token %d: expected val %q, got %q", i, want, tokens[i].Val)
		}
	}
}

func TestLexAtom(t *testing.T) {
	tokens := Lex(":ok :error :hello_world")
	if tokens[0].Type != ast.TAtom || tokens[0].Val != "ok" {
		t.Errorf("got %v %q, want TAtom 'ok'", tokens[0].Type, tokens[0].Val)
	}
	if tokens[1].Type != ast.TAtom || tokens[1].Val != "error" {
		t.Errorf("got %v %q, want TAtom 'error'", tokens[1].Type, tokens[1].Val)
	}
	if tokens[2].Type != ast.TAtom || tokens[2].Val != "hello_world" {
		t.Errorf("got %v %q, want TAtom 'hello_world'", tokens[2].Type, tokens[2].Val)
	}
}

func TestLexSpaceAfter(t *testing.T) {
	tokens := Lex("echo hello")
	if !tokens[0].SpaceAfter {
		t.Error("'echo' should have SpaceAfter=true")
	}
	if tokens[1].SpaceAfter {
		t.Error("'hello' at end should have SpaceAfter=false")
	}
}

func TestLexSingleQuotedString(t *testing.T) {
	tokens := Lex("'hello world'")
	if tokens[0].Type != ast.TString || tokens[0].Val != "hello world" {
		t.Errorf("got %v %q", tokens[0].Type, tokens[0].Val)
	}
}

func TestLexComment(t *testing.T) {
	tokens := Lex("echo hello # this is a comment")
	got := types(tokens)
	want := []ast.TokenType{ast.TIdent, ast.TIdent, ast.TEOF}
	if len(got) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(got), len(want), vals(tokens))
	}
}

func TestLexDollarVar(t *testing.T) {
	tokens := Lex("$HOME")
	if tokens[0].Type != ast.TDollar {
		t.Errorf("expected TDollar, got %d", tokens[0].Type)
	}
	if tokens[1].Type != ast.TIdent || tokens[1].Val != "HOME" {
		t.Errorf("expected TIdent 'HOME', got %d %q", tokens[1].Type, tokens[1].Val)
	}
}

func TestLexDelimiters(t *testing.T) {
	tokens := Lex("( ) [ ] { } ,")
	want := []ast.TokenType{
		ast.TLParen, ast.TRParen, ast.TLBracket, ast.TRBracket,
		ast.TLBrace, ast.TRBrace, ast.TComma, ast.TEOF,
	}
	got := types(tokens)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d: got %d, want %d", i, got[i], want[i])
		}
	}
}

func TestLexBackslash(t *testing.T) {
	tokens := Lex(`\x`)
	if tokens[0].Type != ast.TBackslash {
		t.Errorf("expected TBackslash, got %d", tokens[0].Type)
	}
}

func TestLexLineContinuation(t *testing.T) {
	tokens := Lex("hello \\\nworld")
	// backslash-newline is consumed; hello and world are separate tokens
	got := types(tokens)
	want := []ast.TokenType{ast.TIdent, ast.TIdent, ast.TEOF}
	if len(got) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(got), len(want), vals(tokens))
	}
}
