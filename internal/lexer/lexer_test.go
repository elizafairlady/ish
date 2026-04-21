package lexer

import (
	"testing"

	"ish/internal/ast"
)

// assertTokens checks that Lex(input) produces the expected token types and values.
// No TWhitespace tokens in the stream — whitespace info is on tok.SpaceAfter.
func assertTokens(t *testing.T, input string, wantTypes []ast.TokenType, wantVals []string) {
	t.Helper()
	tokens := Lex(input)
	var got []ast.Token
	for _, tok := range tokens {
		if tok.Type == ast.TEOF {
			break
		}
		got = append(got, tok)
	}

	if len(got) != len(wantTypes) {
		var gotTypes []ast.TokenType
		var gotVals []string
		for _, tok := range got {
			gotTypes = append(gotTypes, tok.Type)
			gotVals = append(gotVals, tok.Val)
		}
		t.Fatalf("Lex(%q): got %d tokens, want %d\n  gotTypes=%v\n  gotVals=%v",
			input, len(got), len(wantTypes), gotTypes, gotVals)
	}
	for i := range wantTypes {
		if got[i].Type != wantTypes[i] {
			t.Errorf("Lex(%q) token[%d] type: got %v (%d), want %v (%d) (val=%q)",
				input, i, got[i].Type, got[i].Type, wantTypes[i], wantTypes[i], got[i].Val)
		}
		if got[i].Val != wantVals[i] {
			t.Errorf("Lex(%q) token[%d] val: got %q, want %q", input, i, got[i].Val, wantVals[i])
		}
	}
}

// assertSpaceAfter checks that the nth token (0-indexed) has the expected SpaceAfter value.
func assertSpaceAfter(t *testing.T, input string, index int, want bool) {
	t.Helper()
	tokens := Lex(input)
	var got []ast.Token
	for _, tok := range tokens {
		if tok.Type == ast.TEOF {
			break
		}
		got = append(got, tok)
	}
	if index >= len(got) {
		t.Fatalf("Lex(%q): only %d tokens, can't check index %d", input, len(got), index)
	}
	if got[index].SpaceAfter != want {
		t.Errorf("Lex(%q) token[%d] (%q) SpaceAfter = %v, want %v",
			input, index, got[index].Val, got[index].SpaceAfter, want)
	}
}

const ID = ast.TIdent

func TestLexIdentifiers(t *testing.T) {
	assertTokens(t, "echo hello",
		[]ast.TokenType{ID, ID},
		[]string{"echo", "hello"})
	assertSpaceAfter(t, "echo hello", 0, true)  // space between echo and hello
	assertSpaceAfter(t, "echo hello", 1, false) // nothing after hello
}

func TestLexKeywords(t *testing.T) {
	assertTokens(t, "if then else fi",
		[]ast.TokenType{ast.TIf, ast.TThen, ast.TElse, ast.TFi},
		[]string{"if", "then", "else", "fi"})

	assertTokens(t, "fn do end",
		[]ast.TokenType{ast.TFn, ast.TDo, ast.TEnd},
		[]string{"fn", "do", "end"})

	assertTokens(t, "true false nil",
		[]ast.TokenType{ast.TTrue, ast.TFalse, ast.TNil},
		[]string{"true", "false", "nil"})
}

func TestLexOperators(t *testing.T) {
	tests := []struct {
		name  string
		input string
		typ   ast.TokenType
		val   string
	}{
		{"pipe", "|", ast.TPipe, "|"},
		{"pipe stderr", "|&", ast.TPipeStderr, "|&"},
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
		{"slash", "/", ast.TDiv, "/"},
		{"percent", "%", ast.TPercent, "%"},
		{"tilde", "~", ast.TTilde, "~"},
		{"at", "@", ast.TAt, "@"},
		{"comma", ",", ast.TComma, ","},
		{"gt", ">", ast.TGt, ">"},
		{"lt", "<", ast.TLt, "<"},
		{"redir append", ">>", ast.TRedirAppend, ">>"},
		{"heredoc", "<<", ast.THeredoc, "<<"},
		{"herestring", "<<<", ast.THereString, "<<<"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := Lex(tt.input)
			if tokens[0].Type != tt.typ {
				t.Errorf("type: got %v (%d), want %v (%d)", tokens[0].Type, tokens[0].Type, tt.typ, tt.typ)
			}
			if tokens[0].Val != tt.val {
				t.Errorf("val: got %q, want %q", tokens[0].Val, tt.val)
			}
		})
	}
}

func TestLexDotIsAlwaysTDot(t *testing.T) {
	assertTokens(t, "a.b",
		[]ast.TokenType{ID, ast.TDot, ID},
		[]string{"a", ".", "b"})

	assertTokens(t, "Module.func",
		[]ast.TokenType{ID, ast.TDot, ID},
		[]string{"Module", ".", "func"})

	assertTokens(t, ".hidden",
		[]ast.TokenType{ast.TDot, ID},
		[]string{".", "hidden"})

	assertTokens(t, "..",
		[]ast.TokenType{ast.TDot, ast.TDot},
		[]string{".", "."})

	// Dot with spaces — SpaceAfter on tokens
	assertTokens(t, "a . b",
		[]ast.TokenType{ID, ast.TDot, ID},
		[]string{"a", ".", "b"})
	assertSpaceAfter(t, "a . b", 0, true)  // space after a
	assertSpaceAfter(t, "a . b", 1, true)  // space after .
}

func TestLexSlashIsAlwaysTDiv(t *testing.T) {
	assertTokens(t, "/tmp/foo",
		[]ast.TokenType{ast.TDiv, ID, ast.TDiv, ID},
		[]string{"/", "tmp", "/", "foo"})

	assertTokens(t, "a / b",
		[]ast.TokenType{ID, ast.TDiv, ID},
		[]string{"a", "/", "b"})
	assertSpaceAfter(t, "a / b", 0, true) // space after a

	assertTokens(t, "./script",
		[]ast.TokenType{ast.TDot, ast.TDiv, ID},
		[]string{".", "/", "script"})
}

func TestLexMinusIsAlwaysTMinus(t *testing.T) {
	assertTokens(t, "-la",
		[]ast.TokenType{ast.TMinus, ID},
		[]string{"-", "la"})
	assertSpaceAfter(t, "-la", 0, false) // no space: flag

	assertTokens(t, "a - b",
		[]ast.TokenType{ID, ast.TMinus, ID},
		[]string{"a", "-", "b"})
	assertSpaceAfter(t, "a - b", 0, true)  // space after a
	assertSpaceAfter(t, "a - b", 1, true)  // space after -

	assertTokens(t, "->",
		[]ast.TokenType{ast.TArrow},
		[]string{"->"})
}

func TestLexStrings(t *testing.T) {
	t.Run("single quoted", func(t *testing.T) {
		tokens := Lex(`'hello world'`)
		if tokens[0].Type != ast.TString || tokens[0].Val != "hello world" || !tokens[0].Quoted {
			t.Errorf("got type=%v val=%q quoted=%v", tokens[0].Type, tokens[0].Val, tokens[0].Quoted)
		}
	})

	t.Run("double quoted no interp", func(t *testing.T) {
		assertTokens(t, `"hello world"`,
			[]ast.TokenType{ast.TStringStart, ast.TString, ast.TStringEnd},
			[]string{"\"", "hello world", "\""})
	})

	t.Run("double quoted with var", func(t *testing.T) {
		assertTokens(t, `"hello $name"`,
			[]ast.TokenType{ast.TStringStart, ast.TString, ast.TDollar, ID, ast.TStringEnd},
			[]string{"\"", "hello ", "$", "name", "\""})
	})

	t.Run("double quoted with cmd sub", func(t *testing.T) {
		assertTokens(t, `"hello $(echo world)"`,
			[]ast.TokenType{ast.TStringStart, ast.TString, ast.TDollarLParen, ID, ID, ast.TRParen, ast.TStringEnd},
			[]string{"\"", "hello ", "$(", "echo", "world", ")", "\""})
	})
}

func TestLexDollarForms(t *testing.T) {
	t.Run("simple var", func(t *testing.T) {
		assertTokens(t, "$var",
			[]ast.TokenType{ast.TDollar, ID},
			[]string{"$", "var"})
	})

	t.Run("braced var", func(t *testing.T) {
		assertTokens(t, "${var}",
			[]ast.TokenType{ast.TDollarLBrace, ID, ast.TRBrace},
			[]string{"${", "var", "}"})
	})

	t.Run("command sub", func(t *testing.T) {
		assertTokens(t, "$(echo hi)",
			[]ast.TokenType{ast.TDollarLParen, ID, ID, ast.TRParen},
			[]string{"$(", "echo", "hi", ")"})
	})

	t.Run("arith sub", func(t *testing.T) {
		assertTokens(t, "$((1+2))",
			[]ast.TokenType{ast.TDollarDLParen, ast.TInt, ast.TPlus, ast.TInt, ast.TRParen, ast.TRParen},
			[]string{"$((", "1", "+", "2", ")", ")"})
	})

	t.Run("special vars", func(t *testing.T) {
		for _, sv := range []string{"$?", "$$", "$!", "$@", "$*", "$#", "$0", "$1"} {
			tokens := Lex(sv)
			if tokens[0].Type != ast.TSpecialVar {
				t.Errorf("Lex(%q): expected TSpecialVar, got %v", sv, tokens[0].Type)
			}
		}
	})

	t.Run("bare dollar", func(t *testing.T) {
		tokens := Lex("$")
		if tokens[0].Type != ast.TDollar {
			t.Errorf("expected TDollar, got %v", tokens[0].Type)
		}
	})
}

func TestLexAtoms(t *testing.T) {
	t.Run("normal atom", func(t *testing.T) {
		tokens := Lex(":ok")
		if tokens[0].Type != ast.TAtom || tokens[0].Val != "ok" {
			t.Errorf("got type=%v val=%q", tokens[0].Type, tokens[0].Val)
		}
	})

	t.Run("bare colon", func(t *testing.T) {
		tokens := Lex(": ")
		if tokens[0].Type != ast.TColon {
			t.Errorf("expected TColon, got %v", tokens[0].Type)
		}
	})
}

func TestLexNumbers(t *testing.T) {
	assertTokens(t, "42", []ast.TokenType{ast.TInt}, []string{"42"})
	assertTokens(t, "3.14", []ast.TokenType{ast.TFloat}, []string{"3.14"})
	assertTokens(t, "3rd", []ast.TokenType{ID}, []string{"3rd"})
}

func TestLexComments(t *testing.T) {
	t.Run("comment after whitespace", func(t *testing.T) {
		assertTokens(t, "echo hi # comment",
			[]ast.TokenType{ID, ID},
			[]string{"echo", "hi"})
	})

	t.Run("comment at line start", func(t *testing.T) {
		tokens := Lex("# full line comment")
		if tokens[0].Type != ast.TEOF {
			t.Errorf("expected TEOF, got %v %q", tokens[0].Type, tokens[0].Val)
		}
	})

	t.Run("hash mid-word is literal", func(t *testing.T) {
		assertTokens(t, "hello#world",
			[]ast.TokenType{ID, ast.THash, ID},
			[]string{"hello", "#", "world"})
	})
}

func TestLexSpaceAfter(t *testing.T) {
	t.Run("space between tokens", func(t *testing.T) {
		assertSpaceAfter(t, "a b", 0, true)
		assertSpaceAfter(t, "a b", 1, false)
	})

	t.Run("tab between tokens", func(t *testing.T) {
		assertSpaceAfter(t, "a\tb", 0, true)
	})

	t.Run("no space between adjacent tokens", func(t *testing.T) {
		assertSpaceAfter(t, "$var.field", 0, false) // $ not spaced
		assertSpaceAfter(t, "$var.field", 1, false) // var not spaced
		assertSpaceAfter(t, "$var.field", 2, false) // . not spaced
	})

	t.Run("POSIX assign vs binding", func(t *testing.T) {
		// FOO=bar — no space after FOO
		assertSpaceAfter(t, "FOO=bar", 0, false)
		// x = 5 — space after x
		assertSpaceAfter(t, "x = 5", 0, true)
	})

	t.Run("flag adjacency", func(t *testing.T) {
		// echo -n — space after echo, no space after -
		assertSpaceAfter(t, "echo -n hello", 0, true)  // echo
		assertSpaceAfter(t, "echo -n hello", 1, false) // -
		assertSpaceAfter(t, "echo -n hello", 2, true)  // n
	})
}

func TestLexNewlines(t *testing.T) {
	assertTokens(t, "a\nb",
		[]ast.TokenType{ID, ast.TNewline, ID},
		[]string{"a", "\n", "b"})
}

func TestLexBrackets(t *testing.T) {
	assertTokens(t, "( ) [ ] { }",
		[]ast.TokenType{ast.TLParen, ast.TRParen, ast.TLBracket, ast.TRBracket, ast.TLBrace, ast.TRBrace},
		[]string{"(", ")", "[", "]", "{", "}"})
}

func TestLexPosixAssign(t *testing.T) {
	assertTokens(t, "FOO=bar",
		[]ast.TokenType{ID, ast.TEquals, ID},
		[]string{"FOO", "=", "bar"})

	assertTokens(t, "FOO=",
		[]ast.TokenType{ID, ast.TEquals},
		[]string{"FOO", "="})

	assertTokens(t, "x = 5",
		[]ast.TokenType{ID, ast.TEquals, ast.TInt},
		[]string{"x", "=", "5"})
}

func TestLexRedirections(t *testing.T) {
	assertTokens(t, "echo hi > out.txt",
		[]ast.TokenType{ID, ID, ast.TGt, ID, ast.TDot, ID},
		[]string{"echo", "hi", ">", "out", ".", "txt"})
}

func TestLexComplexLine(t *testing.T) {
	// "done" is a keyword token
	assertTokens(t, "cat file.txt | grep foo && echo done",
		[]ast.TokenType{
			ID, ID, ast.TDot, ID, // cat file.txt
			ast.TPipe, ID, ID,    // | grep foo
			ast.TAnd, ID, ast.TDone, // && echo done
		},
		[]string{
			"cat", "file", ".", "txt",
			"|", "grep", "foo",
			"&&", "echo", "done",
		})
}

func TestLexCommandWithFlags(t *testing.T) {
	assertTokens(t, "ls -la",
		[]ast.TokenType{ID, ast.TMinus, ID},
		[]string{"ls", "-", "la"})

	assertTokens(t, "echo -n hello",
		[]ast.TokenType{ID, ast.TMinus, ID, ID},
		[]string{"echo", "-", "n", "hello"})
}

func TestLexPathTokenization(t *testing.T) {
	assertTokens(t, "/usr/local/bin",
		[]ast.TokenType{ast.TDiv, ID, ast.TDiv, ID, ast.TDiv, ID},
		[]string{"/", "usr", "/", "local", "/", "bin"})

	assertTokens(t, "~/bin",
		[]ast.TokenType{ast.TTilde, ast.TDiv, ID},
		[]string{"~", "/", "bin"})

	assertTokens(t, "../foo",
		[]ast.TokenType{ast.TDot, ast.TDot, ast.TDiv, ID},
		[]string{".", ".", "/", "foo"})
}

func TestLexAdjacentVsSpaced(t *testing.T) {
	// $var.field — all adjacent
	assertTokens(t, "$var.field",
		[]ast.TokenType{ast.TDollar, ID, ast.TDot, ID},
		[]string{"$", "var", ".", "field"})

	// $ var — space between
	assertTokens(t, "$ var",
		[]ast.TokenType{ast.TDollar, ID},
		[]string{"$", "var"})
	assertSpaceAfter(t, "$ var", 0, true)
}

func TestLexMapLiteral(t *testing.T) {
	assertTokens(t, "%{name: \"alice\"}",
		[]ast.TokenType{
			ast.TPercentLBrace, ID, ast.TColon,
			ast.TStringStart, ast.TString, ast.TStringEnd,
			ast.TRBrace,
		},
		[]string{"%{", "name", ":", "\"", "alice", "\"", "}"})
}

func TestLexTupleAndList(t *testing.T) {
	assertTokens(t, "{1, 2}",
		[]ast.TokenType{ast.TLBrace, ast.TInt, ast.TComma, ast.TInt, ast.TRBrace},
		[]string{"{", "1", ",", "2", "}"})

	assertTokens(t, "[a, b]",
		[]ast.TokenType{ast.TLBracket, ID, ast.TComma, ID, ast.TRBracket},
		[]string{"[", "a", ",", "b", "]"})
}

func TestLexEmptyInput(t *testing.T) {
	tokens := Lex("")
	if len(tokens) != 1 || tokens[0].Type != ast.TEOF {
		t.Errorf("expected only TEOF, got %v", tokens)
	}
}

func TestLexBacktick(t *testing.T) {
	tokens := Lex("`echo hi`")
	if tokens[0].Type != ast.TDollarLParen {
		t.Errorf("expected TDollarLParen, got %v", tokens[0].Type)
	}
}

func TestLexHeredoc(t *testing.T) {
	input := "cat <<EOF\nhello world\nEOF\n"
	tokens := Lex(input)
	if tokens[0].Type != ID || tokens[0].Val != "cat" {
		t.Errorf("token[0]: expected TIdent 'cat', got %v %q", tokens[0].Type, tokens[0].Val)
	}
	foundHeredoc := false
	foundContent := false
	for _, tok := range tokens {
		if tok.Type == ast.THeredoc {
			foundHeredoc = true
		}
		if tok.Type == ast.TString && tok.Val == "hello world" {
			foundContent = true
		}
	}
	if !foundHeredoc {
		t.Error("expected THeredoc token")
	}
	if !foundContent {
		t.Error("expected TString with heredoc content")
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
}

func TestLexDollarString(t *testing.T) {
	tokens := Lex(`$"hello\tworld"`)
	if tokens[0].Type != ast.TDollarDQuote {
		t.Errorf("expected TDollarDQuote, got %v", tokens[0].Type)
	}
}
