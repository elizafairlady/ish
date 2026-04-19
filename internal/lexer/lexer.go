package lexer

import (
	"fmt"
	"strings"
	"unicode"

	"ish/internal/ast"
)

type Lexer struct {
	src         string
	pos         int
	tokens      []ast.Token
	lastEmitted ast.TokenType
	err         string
}

func Lex(src string) []ast.Token {
	l := &Lexer{src: src, lastEmitted: ast.TEOF}
	l.lex()
	return l.tokens
}

func LexCheck(src string) ([]ast.Token, error) {
	l := &Lexer{src: src, lastEmitted: ast.TEOF}
	l.lex()
	if l.err != "" {
		return l.tokens, fmt.Errorf("%s", l.err)
	}
	return l.tokens, nil
}

func (l *Lexer) lex() {
	for l.pos < len(l.src) {
		l.skipSpaces()
		if l.pos >= len(l.src) {
			break
		}
		ch := l.src[l.pos]

		if ch == '#' && !l.isInterpolation() {
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
			continue
		}

		switch {
		case ch == '\n':
			l.emit(ast.TNewline, "\n")
			l.pos++
		case ch == '|':
			if l.peek(1) == '>' {
				l.emit(ast.TPipeArrow, "|>")
				l.pos += 2
			} else if l.peek(1) == '|' {
				l.emit(ast.TOr, "||")
				l.pos += 2
			} else if l.peek(1) == '&' {
				l.emit(ast.TPipe, "|&")
				l.pos += 2
			} else {
				l.emit(ast.TPipe, "|")
				l.pos++
			}
		case ch == '&':
			if l.peek(1) == '&' {
				l.emit(ast.TAnd, "&&")
				l.pos += 2
			} else {
				l.emit(ast.TAmpersand, "&")
				l.pos++
			}
		case ch == ';':
			l.emit(ast.TSemicolon, ";")
			l.pos++
		case ch == '(':
			l.emit(ast.TLParen, "(")
			l.pos++
		case ch == ')':
			l.emit(ast.TRParen, ")")
			l.pos++
		case ch == '{':
			l.emit(ast.TLBrace, "{")
			l.pos++
		case ch == '}':
			l.emit(ast.TRBrace, "}")
			l.pos++
		case ch == '[':
			l.emit(ast.TLBracket, "[")
			l.pos++
		case ch == ']':
			l.emit(ast.TRBracket, "]")
			l.pos++
		case ch == ',':
			l.emit(ast.TComma, ",")
			l.pos++
		case ch == '+':
			next := l.peek(1)
			if next >= 'a' && next <= 'z' || next >= 'A' && next <= 'Z' {
				start := l.pos
				l.pos++
				for l.pos < len(l.src) {
					c := l.src[l.pos]
					if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
						l.pos++
					} else {
						break
					}
				}
				l.emit(ast.TWord, l.src[start:l.pos])
			} else {
				l.emit(ast.TPlus, "+")
				l.pos++
			}
		case ch == '*':
			l.emit(ast.TMul, "*")
			l.pos++
		case ch == '/':
			isDivision := false
			switch l.lastEmitted {
			case ast.TInt, ast.TRParen, ast.TRBracket:
				isDivision = true
			case ast.TWord:
				next := l.peek(1)
				isDivision = next == 0 || next == ' ' || next == '\t' || next == '\n'
			}
			if isDivision {
				l.emit(ast.TDiv, "/")
				l.pos++
			} else {
				l.lexWord()
			}
		case ch == '.':
			l.emit(ast.TDot, ".")
			l.pos++
		case ch == '%':
			next := l.peek(1)
			if next >= '0' && next <= '9' {
				start := l.pos
				l.pos++
				for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
					l.pos++
				}
				l.emit(ast.TWord, l.src[start:l.pos])
			} else {
				l.emit(ast.TPercent, "%")
				l.pos++
			}
		case ch == '!':
			if l.peek(1) == '=' {
				l.emit(ast.TNe, "!=")
				l.pos += 2
			} else {
				l.emit(ast.TBang, "!")
				l.pos++
			}
		case ch == '=':
			if l.peek(1) == '=' {
				l.emit(ast.TEq, "==")
				l.pos += 2
			} else {
				l.emit(ast.TEquals, "=")
				l.pos++
			}
		case ch == '-':
			if l.peek(1) == '>' {
				l.emit(ast.TArrow, "->")
				l.pos += 2
			} else {
				next := l.peek(1)
				if next >= 'a' && next <= 'z' || next >= 'A' && next <= 'Z' {
					l.lexFlag()
				} else if next == '-' {
					if l.pos+2 < len(l.src) {
						after := l.src[l.pos+2]
						if after >= 'a' && after <= 'z' || after >= 'A' && after <= 'Z' {
							l.lexFlag()
						} else {
							l.emit(ast.TWord, "--")
							l.pos += 2
						}
					} else {
						l.emit(ast.TWord, "--")
						l.pos += 2
					}
				} else {
					l.emit(ast.TMinus, "-")
					l.pos++
				}
			}
		case ch == '<':
			if l.peek(1) == '-' {
				l.emit(ast.TLeftArrow, "<-")
				l.pos += 2
			} else if l.peek(1) == '<' && l.peek(2) == '<' {
				l.emit(ast.THereString, "<<<")
				l.pos += 3
			} else if l.peek(1) == '<' {
				l.pos += 2
				l.lexHeredoc()
			} else if l.peek(1) == '=' {
				l.emit(ast.TLe, "<=")
				l.pos += 2
			} else {
				l.emit(ast.TRedirIn, "<")
				l.pos++
			}
		case ch == '>':
			if l.peek(1) == '>' {
				l.emit(ast.TRedirAppend, ">>")
				l.pos += 2
			} else if l.peek(1) == '=' {
				l.emit(ast.TGe, ">=")
				l.pos += 2
			} else {
				l.emit(ast.TRedirOut, ">")
				l.pos++
			}
		case ch == '$':
			l.lexDollar()
		case ch == '"':
			l.lexDoubleQuote()
		case ch == '\'':
			l.lexSingleQuote()
		case ch == ':':
			l.lexAtom()
		case ch >= '0' && ch <= '9':
			l.lexNumber()
		case ch == '`':
			l.lexBacktick()
		case ch == '\\':
			l.emit(ast.TBackslash, "\\")
			l.pos++
		default:
			l.lexWord()
		}
	}
	l.emit(ast.TEOF, "")
}

func (l *Lexer) peek(offset int) byte {
	p := l.pos + offset
	if p >= len(l.src) {
		return 0
	}
	return l.src[p]
}

func (l *Lexer) isInterpolation() bool {
	return l.pos+1 < len(l.src) && l.src[l.pos] == '#' && l.src[l.pos+1] == '{'
}

func (l *Lexer) emit(t ast.TokenType, val string) {
	l.tokens = append(l.tokens, ast.Token{Type: t, Val: val, Pos: l.pos})
	l.lastEmitted = t
}

func (l *Lexer) skipSpaces() {
	for l.pos < len(l.src) && (l.src[l.pos] == ' ' || l.src[l.pos] == '\t' || l.src[l.pos] == '\r') {
		l.pos++
	}
}

func (l *Lexer) lexWord() {
	start := l.pos
	braceDepth := 0
	parenDepth := 0
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if braceDepth == 0 && parenDepth == 0 {
			if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
				break
			}
			if ch == '|' || ch == '&' || ch == ';' || ch == '(' || ch == ')' ||
				ch == '{' || ch == '}' || ch == '[' || ch == ']' ||
				ch == '<' || ch == '>' || ch == ',' || ch == '#' {
				break
			}
		}
		if ch == '"' {
			l.pos++
			for l.pos < len(l.src) && l.src[l.pos] != '"' {
				if l.src[l.pos] == '\\' {
					l.pos++
				}
				l.pos++
			}
			if l.pos < len(l.src) {
				l.pos++
			}
			continue
		}
		if ch == '\'' {
			l.pos++
			for l.pos < len(l.src) && l.src[l.pos] != '\'' {
				l.pos++
			}
			if l.pos < len(l.src) {
				l.pos++
			}
			continue
		}
		if ch == '\\' {
			l.pos++
			if l.pos < len(l.src) {
				l.pos++
			}
			continue
		}
		if ch == '$' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '(' {
			parenDepth++
			l.pos += 2
			continue
		}
		if ch == '$' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{' {
			braceDepth++
			l.pos += 2
			continue
		}
		if ch == '(' && parenDepth > 0 {
			parenDepth++
		}
		if ch == ')' && parenDepth > 0 {
			parenDepth--
			l.pos++
			continue
		}
		if ch == '{' && braceDepth > 0 {
			braceDepth++
		}
		if ch == '}' && braceDepth > 0 {
			braceDepth--
			l.pos++
			continue
		}
		l.pos++
	}
	if l.pos > start {
		l.emit(ast.TWord, l.src[start:l.pos])
	}
}

func (l *Lexer) lexNumber() {
	start := l.pos
	for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
		l.pos++
	}
	if l.pos < len(l.src) && isWordChar(l.src[l.pos]) {
		for l.pos < len(l.src) && isWordChar(l.src[l.pos]) {
			l.pos++
		}
		l.emit(ast.TWord, l.src[start:l.pos])
		return
	}
	l.emit(ast.TInt, l.src[start:l.pos])
}

func (l *Lexer) lexAtom() {
	l.pos++
	start := l.pos
	for l.pos < len(l.src) && (isWordChar(l.src[l.pos]) || l.src[l.pos] == '_') {
		l.pos++
	}
	if l.pos > start {
		l.emit(ast.TAtom, l.src[start:l.pos])
	} else {
		l.emit(ast.TWord, ":")
	}
}

func (l *Lexer) lexDoubleQuote() {
	l.pos++
	var buf strings.Builder
	for l.pos < len(l.src) && l.src[l.pos] != '"' {
		if l.src[l.pos] == '\\' && l.pos+1 < len(l.src) {
			next := l.src[l.pos+1]
			switch next {
			case '"', '\\', '$', '`', '\n':
				buf.WriteByte(next)
				l.pos += 2
			default:
				buf.WriteByte('\\')
				buf.WriteByte(next)
				l.pos += 2
			}
			continue
		}
		buf.WriteByte(l.src[l.pos])
		l.pos++
	}
	if l.pos < len(l.src) {
		l.pos++
	} else if l.err == "" {
		l.err = "unterminated double-quoted string"
	}
	l.emit(ast.TString, buf.String())
}

func (l *Lexer) lexSingleQuote() {
	l.pos++
	start := l.pos
	for l.pos < len(l.src) && l.src[l.pos] != '\'' {
		l.pos++
	}
	l.tokens = append(l.tokens, ast.Token{Type: ast.TString, Val: l.src[start:l.pos], Pos: start, Quoted: true})
	if l.pos < len(l.src) {
		l.pos++
	} else if l.err == "" {
		l.err = "unterminated single-quoted string"
	}
}

func (l *Lexer) lexHeredoc() {
	stripTabs := false
	if l.pos < len(l.src) && l.src[l.pos] == '-' {
		stripTabs = true
		l.pos++
	}

	l.skipSpaces()

	quoted := false
	var delim string
	if l.pos < len(l.src) && (l.src[l.pos] == '\'' || l.src[l.pos] == '"') {
		quoted = true
		q := l.src[l.pos]
		l.pos++
		start := l.pos
		for l.pos < len(l.src) && l.src[l.pos] != q {
			l.pos++
		}
		delim = l.src[start:l.pos]
		if l.pos < len(l.src) {
			l.pos++
		}
	} else {
		start := l.pos
		for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != ' ' && l.src[l.pos] != '\t' {
			l.pos++
		}
		delim = l.src[start:l.pos]
	}

	if delim == "" {
		l.emit(ast.THeredoc, "<<")
		return
	}

	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
	if l.pos < len(l.src) {
		l.pos++
	}

	var body strings.Builder
	for l.pos < len(l.src) {
		lineStart := l.pos
		for l.pos < len(l.src) && l.src[l.pos] != '\n' {
			l.pos++
		}
		line := l.src[lineStart:l.pos]
		if l.pos < len(l.src) {
			l.pos++
		}

		trimmed := line
		if stripTabs {
			trimmed = strings.TrimLeft(line, "\t")
		}
		if strings.TrimRight(trimmed, " \t\r") == delim {
			break
		}

		if stripTabs {
			line = strings.TrimLeft(line, "\t")
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}

	content := body.String()
	if len(content) > 0 && content[len(content)-1] == '\n' {
		content = content[:len(content)-1]
	}

	if !quoted {
		content = strings.ReplaceAll(content, "\\\n", "")
	}

	l.emit(ast.THeredoc, "<<")
	if quoted {
		l.tokens = append(l.tokens, ast.Token{Type: ast.TString, Val: content, Pos: l.pos, Quoted: true})
	} else {
		l.tokens = append(l.tokens, ast.Token{Type: ast.TString, Val: content, Pos: l.pos})
	}
}

func (l *Lexer) lexBacktick() {
	l.pos++
	var buf strings.Builder
	for l.pos < len(l.src) && l.src[l.pos] != '`' {
		if l.src[l.pos] == '\\' && l.pos+1 < len(l.src) {
			next := l.src[l.pos+1]
			if next == '$' || next == '`' || next == '\\' || next == '"' {
				buf.WriteByte(next)
				l.pos += 2
				continue
			}
		}
		buf.WriteByte(l.src[l.pos])
		l.pos++
	}
	content := buf.String()
	if l.pos < len(l.src) {
		l.pos++
	}
	l.emit(ast.TWord, "$("+content+")")
}

func (l *Lexer) lexFlag() {
	start := l.pos
	l.pos++
	if l.pos < len(l.src) && l.src[l.pos] == '-' {
		l.pos++
	}
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' {
			l.pos++
		} else {
			break
		}
	}
	if l.pos < len(l.src) && l.src[l.pos] == '=' {
		l.pos++
		for l.pos < len(l.src) {
			ch := l.src[l.pos]
			if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' ||
				ch == '|' || ch == '&' || ch == ';' || ch == ')' || ch == '}' || ch == ']' {
				break
			}
			if ch == '"' {
				l.pos++
				for l.pos < len(l.src) && l.src[l.pos] != '"' {
					if l.src[l.pos] == '\\' {
						l.pos++
					}
					l.pos++
				}
				if l.pos < len(l.src) {
					l.pos++
				}
				continue
			}
			if ch == '\'' {
				l.pos++
				for l.pos < len(l.src) && l.src[l.pos] != '\'' {
					l.pos++
				}
				if l.pos < len(l.src) {
					l.pos++
				}
				continue
			}
			l.pos++
		}
	}
	l.emit(ast.TWord, l.src[start:l.pos])
}

func (l *Lexer) lexDollar() {
	if l.peek(1) == '(' {
		if l.peek(2) == '(' {
			l.pos += 3
			start := l.pos
			depth := 1
			for l.pos < len(l.src) && depth > 0 {
				ch := l.src[l.pos]
				if ch == '"' {
					l.pos++
					for l.pos < len(l.src) && l.src[l.pos] != '"' {
						if l.src[l.pos] == '\\' {
							l.pos++
						}
						l.pos++
					}
					if l.pos < len(l.src) {
						l.pos++
					}
					continue
				}
				if ch == '\'' {
					l.pos++
					for l.pos < len(l.src) && l.src[l.pos] != '\'' {
						l.pos++
					}
					if l.pos < len(l.src) {
						l.pos++
					}
					continue
				}
				if ch == '(' && l.peek(1) == '(' {
					depth++
					l.pos += 2
				} else if ch == ')' && l.peek(1) == ')' {
					depth--
					if depth == 0 {
						break
					}
					l.pos += 2
				} else {
					l.pos++
				}
			}
			l.emit(ast.TWord, "$(("+l.src[start:l.pos]+"))")
			if l.pos < len(l.src) {
				l.pos += 2
			}
		} else {
			l.pos += 2
			start := l.pos
			depth := 1
			for l.pos < len(l.src) && depth > 0 {
				if l.src[l.pos] == '(' {
					depth++
				} else if l.src[l.pos] == ')' {
					depth--
				}
				if depth > 0 {
					l.pos++
				}
			}
			l.emit(ast.TWord, "$("+l.src[start:l.pos]+")")
			if l.pos < len(l.src) {
				l.pos++
			}
		}
	} else if l.peek(1) == '{' {
		l.pos += 2
		start := l.pos
		for l.pos < len(l.src) && l.src[l.pos] != '}' {
			l.pos++
		}
		l.emit(ast.TWord, "${"+l.src[start:l.pos]+"}")
		if l.pos < len(l.src) {
			l.pos++
		}
	} else {
		l.pos++
		start := l.pos
		if l.pos < len(l.src) && (l.src[l.pos] == '?' || l.src[l.pos] == '$' ||
			l.src[l.pos] == '!' || l.src[l.pos] == '@' || l.src[l.pos] == '*' ||
			l.src[l.pos] == '#' || (l.src[l.pos] >= '0' && l.src[l.pos] <= '9')) {
			l.pos++
			l.emit(ast.TWord, "$"+l.src[start:l.pos])
		} else {
			for l.pos < len(l.src) && (isWordChar(l.src[l.pos]) || l.src[l.pos] == '_') {
				l.pos++
			}
			if l.pos > start {
				l.emit(ast.TWord, "$"+l.src[start:l.pos])
			} else {
				l.emit(ast.TWord, "$")
			}
		}
	}
}

func isWordChar(ch byte) bool {
	return ch == '_' || ch == '-' || ch == '/' || ch == '.' || ch == '~' ||
		ch == '@' || ch == ':' || ch == '+' ||
		(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') ||
		ch > 127
}

func IsAlphaNum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}
