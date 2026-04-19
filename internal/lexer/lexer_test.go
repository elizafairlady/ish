package lexer

import (
	"testing"

	"ish/internal/ast"
)

// helper: extract token types from a slice, excluding final TEOF
func tokTypes(tokens []ast.Token) []ast.TokenType {
	var out []ast.TokenType
	for _, t := range tokens {
		if t.Type == ast.TEOF {
			break
		}
		out = append(out, t.Type)
	}
	return out
}

// helper: extract token values from a slice, excluding final TEOF
func tokVals(tokens []ast.Token) []string {
	var out []string
	for _, t := range tokens {
		if t.Type == ast.TEOF {
			break
		}
		out = append(out, t.Val)
	}
	return out
}

func assertTokens(t *testing.T, input string, wantTypes []ast.TokenType, wantVals []string) {
	t.Helper()
	tokens := Lex(input)
	gotTypes := tokTypes(tokens)
	gotVals := tokVals(tokens)

	if len(gotTypes) != len(wantTypes) {
		t.Fatalf("Lex(%q): got %d tokens, want %d\ngotTypes=%v\ngotVals=%v", input, len(gotTypes), len(wantTypes), gotTypes, gotVals)
	}
	for i := range wantTypes {
		if gotTypes[i] != wantTypes[i] {
			t.Errorf("Lex(%q) token[%d] type: got %d, want %d (val=%q)", input, i, gotTypes[i], wantTypes[i], gotVals[i])
		}
		if gotVals[i] != wantVals[i] {
			t.Errorf("Lex(%q) token[%d] val: got %q, want %q", input, i, gotVals[i], wantVals[i])
		}
	}
}

func TestLexSimpleWords(t *testing.T) {
	assertTokens(t, "echo hello",
		[]ast.TokenType{ast.TWord, ast.TWord},
		[]string{"echo", "hello"})
}

func TestLexOperators(t *testing.T) {
	tests := []struct {
		name  string
		input string
		typ   ast.TokenType
		val   string
	}{
		{"pipe", "|", ast.TPipe, "|"},
		{"pipe arrow", "|>", ast.TPipeArrow, "|>"},
		{"or", "||", ast.TOr, "||"},
		{"and", "&&", ast.TAnd, "&&"},
		{"ampersand", "&", ast.TAmpersand, "&"},
		{"semicolon", ";", ast.TSemicolon, ";"},
		{"equals", "=", ast.TEquals, "="},
		{"double equals", "==", ast.TEq, "=="},
		{"not equals", "!=", ast.TNe, "!="},
		{"less equal", "<=", ast.TLe, "<="},
		{"greater equal", ">=", ast.TGe, ">="},
		{"arrow", "->", ast.TArrow, "->"},
		{"left arrow", "<-", ast.TLeftArrow, "<-"},
		{"plus", "+", ast.TPlus, "+"},
		{"minus", "-", ast.TMinus, "-"},
		{"star", "*", ast.TMul, "*"},
		{"bang", "!", ast.TBang, "!"},
		{"dot", ".", ast.TDot, "."},
		{"percent", "%", ast.TPercent, "%"},
		{"comma", ",", ast.TComma, ","},
		{"heredoc", "<<", ast.THeredoc, "<<"},
		{"redir out", "> ", ast.TRedirOut, ">"},
		{"redir append", ">> ", ast.TRedirAppend, ">>"},
		{"redir in", "< ", ast.TRedirIn, "<"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := Lex(tt.input)
			if len(tokens) < 1 {
				t.Fatal("expected at least one token")
			}
			got := tokens[0]
			if got.Type != tt.typ {
				t.Errorf("type: got %d, want %d", got.Type, tt.typ)
			}
			if got.Val != tt.val {
				t.Errorf("val: got %q, want %q", got.Val, tt.val)
			}
		})
	}
}

func TestLexStrings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		val   string
	}{
		{"double quoted", `"hello world"`, "hello world"},
		{"escape quote", `"say \"hi\""`, `say "hi"`},
		{"escape backslash", `"a\\b"`, `a\b`},
		{"escape dollar", `"cost \$5"`, "cost $5"},
		{"single quoted", `'hello world'`, "hello world"},
		{"single no escapes", `'a\nb'`, `a\nb`},
		{"empty double", `""`, ""},
		{"empty single", `''`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := Lex(tt.input)
			if tokens[0].Type != ast.TString {
				t.Errorf("expected TString, got %d", tokens[0].Type)
			}
			if tokens[0].Val != tt.val {
				t.Errorf("val: got %q, want %q", tokens[0].Val, tt.val)
			}
		})
	}
}

func TestLexUnterminatedString(t *testing.T) {
	// Unterminated strings should still produce a token (lexer doesn't error, just stops)
	tokens := Lex(`"hello`)
	if tokens[0].Type != ast.TString {
		t.Errorf("expected TString for unterminated, got %d", tokens[0].Type)
	}
	if tokens[0].Val != "hello" {
		t.Errorf("val: got %q, want %q", tokens[0].Val, "hello")
	}
}

func TestLexAtoms(t *testing.T) {
	t.Run("normal atom", func(t *testing.T) {
		tokens := Lex(":ok")
		if tokens[0].Type != ast.TAtom {
			t.Errorf("expected TAtom, got %d", tokens[0].Type)
		}
		if tokens[0].Val != "ok" {
			t.Errorf("val: got %q, want %q", tokens[0].Val, "ok")
		}
	})

	t.Run("bare colon is POSIX builtin", func(t *testing.T) {
		tokens := Lex(": ")
		if tokens[0].Type != ast.TWord {
			t.Errorf("expected TWord for bare colon, got %d", tokens[0].Type)
		}
		if tokens[0].Val != ":" {
			t.Errorf("val: got %q, want %q", tokens[0].Val, ":")
		}
	})
}

func TestLexNumbers(t *testing.T) {
	t.Run("integer", func(t *testing.T) {
		tokens := Lex("42")
		if tokens[0].Type != ast.TInt {
			t.Errorf("expected TInt, got %d", tokens[0].Type)
		}
		if tokens[0].Val != "42" {
			t.Errorf("val: got %q, want %q", tokens[0].Val, "42")
		}
	})

	t.Run("number followed by letters becomes TWord", func(t *testing.T) {
		tokens := Lex("3rd")
		if tokens[0].Type != ast.TWord {
			t.Errorf("expected TWord for '3rd', got %d", tokens[0].Type)
		}
		if tokens[0].Val != "3rd" {
			t.Errorf("val: got %q, want %q", tokens[0].Val, "3rd")
		}
	})
}

func TestLexDollarForms(t *testing.T) {
	tests := []struct {
		name  string
		input string
		val   string
	}{
		{"simple var", "$var", "$var"},
		{"braced var", "${var}", "${var}"},
		{"exit status", "$?", "$?"},
		{"shell pid", "$$", "$$"},
		{"bg pid", "$!", "$!"},
		{"all args @", "$@", "$@"},
		{"all args *", "$*", "$*"},
		{"arg count", "$#", "$#"},
		{"positional", "$1", "$1"},
		{"command sub", "$(echo hi)", "$(echo hi)"},
		{"arith sub", "$((1+2))", "$((1+2))"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := Lex(tt.input)
			if tokens[0].Type != ast.TWord {
				t.Errorf("expected TWord, got %d", tokens[0].Type)
			}
			if tokens[0].Val != tt.val {
				t.Errorf("val: got %q, want %q", tokens[0].Val, tt.val)
			}
		})
	}
}

func TestLexBareDollar(t *testing.T) {
	// bare $ at end of input
	tokens := Lex("$")
	if tokens[0].Type != ast.TWord {
		t.Errorf("expected TWord, got %d", tokens[0].Type)
	}
	if tokens[0].Val != "$" {
		t.Errorf("val: got %q, want %q", tokens[0].Val, "$")
	}
}

func TestLexPosixAssign(t *testing.T) {
	t.Run("with value", func(t *testing.T) {
		// Assignments are now lexed as TWord -- parser detects them
		tokens := Lex("VAR=value")
		if tokens[0].Type != ast.TWord {
			t.Errorf("expected TWord, got %d", tokens[0].Type)
		}
		if tokens[0].Val != "VAR=value" {
			t.Errorf("val: got %q, want %q", tokens[0].Val, "VAR=value")
		}
	})

	t.Run("empty value", func(t *testing.T) {
		tokens := Lex("VAR=")
		if tokens[0].Type != ast.TWord {
			t.Errorf("expected TWord, got %d", tokens[0].Type)
		}
		if tokens[0].Val != "VAR=" {
			t.Errorf("val: got %q, want %q", tokens[0].Val, "VAR=")
		}
	})

	t.Run("= in argument position is part of word", func(t *testing.T) {
		tokens := Lex("echo foo=bar")
		// echo, foo=bar, EOF
		if len(tokens) < 3 {
			t.Fatalf("expected at least 3 tokens, got %d", len(tokens))
		}
		if tokens[1].Type != ast.TWord || tokens[1].Val != "foo=bar" {
			t.Errorf("expected TWord 'foo=bar', got type=%d val=%q", tokens[1].Type, tokens[1].Val)
		}
	})
}

func TestLexPathVsDivision(t *testing.T) {
	t.Run("path is word", func(t *testing.T) {
		tokens := Lex("/tmp/foo")
		if tokens[0].Type != ast.TWord {
			t.Errorf("expected TWord for path, got %d", tokens[0].Type)
		}
		if tokens[0].Val != "/tmp/foo" {
			t.Errorf("val: got %q, want %q", tokens[0].Val, "/tmp/foo")
		}
	})

	t.Run("division is TDiv", func(t *testing.T) {
		assertTokens(t, "a / b",
			[]ast.TokenType{ast.TWord, ast.TDiv, ast.TWord},
			[]string{"a", "/", "b"})
	})
}

func TestLexComments(t *testing.T) {
	tokens := Lex("echo hi # this is a comment")
	types := tokTypes(tokens)
	// Should only have echo and hi, comment is stripped
	if len(types) != 2 {
		t.Errorf("expected 2 tokens, got %d: %v", len(types), tokVals(tokens))
	}
}

func TestLexNewlines(t *testing.T) {
	assertTokens(t, "a\nb",
		[]ast.TokenType{ast.TWord, ast.TNewline, ast.TWord},
		[]string{"a", "\n", "b"})
}

func TestLexEmptyInput(t *testing.T) {
	tokens := Lex("")
	if len(tokens) != 1 || tokens[0].Type != ast.TEOF {
		t.Errorf("expected only TEOF for empty input, got %v", tokens)
	}
}

func TestLexWhitespaceOnly(t *testing.T) {
	tokens := Lex("   \t  ")
	if len(tokens) != 1 || tokens[0].Type != ast.TEOF {
		t.Errorf("expected only TEOF for whitespace-only input, got %v", tokens)
	}
}

func TestLexBrackets(t *testing.T) {
	assertTokens(t, "( ) [ ] { }",
		[]ast.TokenType{ast.TLParen, ast.TRParen, ast.TLBracket, ast.TRBracket, ast.TLBrace, ast.TRBrace},
		[]string{"(", ")", "[", "]", "{", "}"})
}

func TestLexRedirections(t *testing.T) {
	assertTokens(t, "echo hi > out.txt",
		[]ast.TokenType{ast.TWord, ast.TWord, ast.TRedirOut, ast.TWord},
		[]string{"echo", "hi", ">", "out.txt"})
}

func TestLexComplexLine(t *testing.T) {
	assertTokens(t, "cat file.txt | grep foo && echo done",
		[]ast.TokenType{ast.TWord, ast.TWord, ast.TPipe, ast.TWord, ast.TWord, ast.TAnd, ast.TWord, ast.TWord},
		[]string{"cat", "file.txt", "|", "grep", "foo", "&&", "echo", "done"})
}

func TestLexBacktickEscapes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"escape dollar", "`echo \\$HOME`", "$(echo $HOME)"},
		{"escape backslash", "`echo \\\\`", "$(echo \\)"},
		{"escape double quote", "`echo \\\"hi\\\"`", `$(echo "hi")`},
		{"no escape other", "`echo \\n`", "$(echo \\n)"}, // \n stays as \n
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := Lex(tt.input)
			if tokens[0].Type != ast.TWord {
				t.Errorf("expected TWord, got %d", tokens[0].Type)
			}
			if tokens[0].Val != tt.want {
				t.Errorf("val: got %q, want %q", tokens[0].Val, tt.want)
			}
		})
	}
}

func TestLexHeredocBackslashNewline(t *testing.T) {
	// Unquoted heredoc: backslash-newline should be removed (line continuation)
	input := "cat <<EOF\nhello \\\nworld\nEOF\n"
	tokens := Lex(input)
	// Expect: TWord("cat"), THeredoc("<<"), TString("hello world"), TEOF
	if len(tokens) < 3 {
		t.Fatalf("expected at least 3 tokens, got %d", len(tokens))
	}
	if tokens[1].Type != ast.THeredoc {
		t.Fatalf("expected THeredoc, got %d", tokens[1].Type)
	}
	if tokens[2].Type != ast.TString {
		t.Fatalf("expected TString, got %d", tokens[2].Type)
	}
	if tokens[2].Val != "hello world" {
		t.Errorf("heredoc content: got %q, want %q", tokens[2].Val, "hello world")
	}
}

func TestLexCheckUnterminatedString(t *testing.T) {
	_, err := LexCheck(`echo "hello`)
	if err == nil {
		t.Fatal("expected error for unterminated double-quoted string")
	}

	_, err = LexCheck(`echo 'hello`)
	if err == nil {
		t.Fatal("expected error for unterminated single-quoted string")
	}

	// Terminated strings should be fine
	_, err = LexCheck(`echo "hello"`)
	if err != nil {
		t.Fatalf("unexpected error for terminated string: %v", err)
	}
}

func TestLexHeredocEmptyDelimiter(t *testing.T) {
	// "cat <<" with no delimiter should not panic
	tokens := Lex("cat <<")
	// Should get TWord("cat") and THeredoc("<<") without crashing
	found := false
	for _, tok := range tokens {
		if tok.Type == ast.THeredoc {
			found = true
		}
	}
	if !found {
		t.Error("expected THeredoc token for bare <<")
	}

	// "cat <<\n" with newline but no delimiter
	tokens = Lex("cat <<\n")
	found = false
	for _, tok := range tokens {
		if tok.Type == ast.THeredoc {
			found = true
		}
	}
	if !found {
		t.Error("expected THeredoc token for << followed by newline")
	}
}
