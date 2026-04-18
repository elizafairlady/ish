package main

import (
	"fmt"
	"strings"
	"unicode"
)

type Lexer struct {
	src         string
	pos         int
	tokens      []Token
	lastEmitted TokenType // last non-whitespace token emitted, for context-sensitive lexing
	err         string    // first lexer error (e.g., unterminated string)
}

func Lex(src string) []Token {
	l := &Lexer{src: src, lastEmitted: TEOF} // TEOF signals "beginning of input"
	l.lex()
	return l.tokens
}

// LexCheck is like Lex but returns an error for malformed input (e.g., unterminated strings).
func LexCheck(src string) ([]Token, error) {
	l := &Lexer{src: src, lastEmitted: TEOF}
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

		// Comments
		if ch == '#' && !l.isInterpolation() {
			// skip to end of line
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
			continue
		}

		switch {
		case ch == '\n':
			l.emit(TNewline, "\n")
			l.pos++
		case ch == '|':
			if l.peek(1) == '>' {
				l.emit(TPipeArrow, "|>")
				l.pos += 2
			} else if l.peek(1) == '|' {
				l.emit(TOr, "||")
				l.pos += 2
			} else if l.peek(1) == '&' {
				l.emit(TPipe, "|&") // pipe both stdout+stderr
				l.pos += 2
			} else {
				l.emit(TPipe, "|")
				l.pos++
			}
		case ch == '&':
			if l.peek(1) == '&' {
				l.emit(TAnd, "&&")
				l.pos += 2
			} else {
				l.emit(TAmpersand, "&")
				l.pos++
			}
		case ch == ';':
			l.emit(TSemicolon, ";")
			l.pos++
		case ch == '(':
			l.emit(TLParen, "(")
			l.pos++
		case ch == ')':
			l.emit(TRParen, ")")
			l.pos++
		case ch == '{':
			l.emit(TLBrace, "{")
			l.pos++
		case ch == '}':
			l.emit(TRBrace, "}")
			l.pos++
		case ch == '[':
			l.emit(TLBracket, "[")
			l.pos++
		case ch == ']':
			l.emit(TRBracket, "]")
			l.pos++
		case ch == ',':
			l.emit(TComma, ",")
			l.pos++
		case ch == '+':
			next := l.peek(1)
			// +letter is a set flag like +e, +x — lex as a word
			if next >= 'a' && next <= 'z' || next >= 'A' && next <= 'Z' {
				start := l.pos
				l.pos++ // skip +
				for l.pos < len(l.src) {
					c := l.src[l.pos]
					if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
						l.pos++
					} else {
						break
					}
				}
				l.emit(TWord, l.src[start:l.pos])
			} else {
				l.emit(TPlus, "+")
				l.pos++
			}
		case ch == '*':
			l.emit(TMul, "*")
			l.pos++
		case ch == '/':
			// Context-sensitive: `/` is division ONLY after expression atoms (int, ), ])
			// AND when followed by something that could be an operand.
			// After TWord, `/` could be either (e.g., `x / 2` vs `echo /tmp`).
			// Heuristic: treat as division only after TInt, TRParen, TRBracket.
			// After TWord, check if next char looks like a path component.
			isDivision := false
			switch l.lastEmitted {
			case TInt, TRParen, TRBracket:
				isDivision = true
			case TWord:
				// After a word, `/` is division only if next is whitespace or an operand start
				// and the `/` is surrounded by spaces (part of an expression)
				next := l.peek(1)
				isDivision = next == 0 || next == ' ' || next == '\t' || next == '\n'
			}
			if isDivision {
				l.emit(TDiv, "/")
				l.pos++
			} else {
				// Path — lex as word
				l.lexWord()
			}
		case ch == '.':
			l.emit(TDot, ".")
			l.pos++
		case ch == '%':
			next := l.peek(1)
			if next >= '0' && next <= '9' {
				// %N is a job spec — lex as a word
				start := l.pos
				l.pos++ // skip %
				for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
					l.pos++
				}
				l.emit(TWord, l.src[start:l.pos])
			} else {
				l.emit(TPercent, "%")
				l.pos++
			}
		case ch == '!':
			if l.peek(1) == '=' {
				l.emit(TNe, "!=")
				l.pos += 2
			} else {
				l.emit(TBang, "!")
				l.pos++
			}
		case ch == '=':
			if l.peek(1) == '=' {
				l.emit(TEq, "==")
				l.pos += 2
			} else {
				l.emit(TEquals, "=")
				l.pos++
			}
		case ch == '-':
			if l.peek(1) == '>' {
				l.emit(TArrow, "->")
				l.pos += 2
			} else {
				next := l.peek(1)
				// -letter or --letter is a flag: lex as a word
				if next >= 'a' && next <= 'z' || next >= 'A' && next <= 'Z' {
					l.lexFlag()
				} else if next == '-' {
					if l.pos+2 < len(l.src) {
						after := l.src[l.pos+2]
						if after >= 'a' && after <= 'z' || after >= 'A' && after <= 'Z' {
							l.lexFlag()
						} else {
							// -- (bare double dash, end-of-options)
							l.emit(TWord, "--")
							l.pos += 2
						}
					} else {
						// -- at end of input
						l.emit(TWord, "--")
						l.pos += 2
					}
				} else {
					l.emit(TMinus, "-")
					l.pos++
				}
			}
		case ch == '<':
			if l.peek(1) == '-' {
				l.emit(TLeftArrow, "<-")
				l.pos += 2
			} else if l.peek(1) == '<' && l.peek(2) == '<' {
				// <<< here-string
				l.emit(THereString, "<<<")
				l.pos += 3
			} else if l.peek(1) == '<' {
				l.pos += 2 // skip <<
				l.lexHeredoc()
			} else if l.peek(1) == '=' {
				l.emit(TLe, "<=")
				l.pos += 2
			} else {
				l.emit(TRedirIn, "<")
				l.pos++
			}
		case ch == '>':
			if l.peek(1) == '>' {
				l.emit(TRedirAppend, ">>")
				l.pos += 2
			} else if l.peek(1) == '=' {
				l.emit(TGe, ">=")
				l.pos += 2
			} else {
				l.emit(TRedirOut, ">")
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
		default:
			l.lexWord()
		}
	}
	l.emit(TEOF, "")
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

func (l *Lexer) emit(t TokenType, val string) {
	l.tokens = append(l.tokens, Token{Type: t, Val: val, Pos: l.pos})
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
			// = never breaks a word. FOO=bar, echo key=val are single tokens.
			// The ish match = (with spaces) is handled by the main lex loop.
		}
		// Consume quoted strings and escapes first, so depth tracking
		// doesn't see ) or } inside quotes.
		if ch == '"' {
			l.pos++
			for l.pos < len(l.src) && l.src[l.pos] != '"' {
				if l.src[l.pos] == '\\' {
					l.pos++
				}
				l.pos++
			}
			if l.pos < len(l.src) {
				l.pos++ // skip closing "
			}
			continue
		}
		if ch == '\'' {
			l.pos++
			for l.pos < len(l.src) && l.src[l.pos] != '\'' {
				l.pos++
			}
			if l.pos < len(l.src) {
				l.pos++ // skip closing '
			}
			continue
		}
		if ch == '\\' {
			l.pos++ // skip escaped char
			if l.pos < len(l.src) {
				l.pos++
			}
			continue
		}
		// Track $() and ${} so we don't break mid-expansion
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
		l.emit(TWord, l.src[start:l.pos])
	}
}

func (l *Lexer) lexNumber() {
	start := l.pos
	for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
		l.pos++
	}
	// If followed by a word char, it's actually a word (e.g., "3rd")
	if l.pos < len(l.src) && isWordChar(l.src[l.pos]) {
		for l.pos < len(l.src) && isWordChar(l.src[l.pos]) {
			l.pos++
		}
		l.emit(TWord, l.src[start:l.pos])
		return
	}
	l.emit(TInt, l.src[start:l.pos])
}

func (l *Lexer) lexAtom() {
	l.pos++ // skip ':'
	start := l.pos
	for l.pos < len(l.src) && (isWordChar(l.src[l.pos]) || l.src[l.pos] == '_') {
		l.pos++
	}
	if l.pos > start {
		l.emit(TAtom, l.src[start:l.pos])
	} else {
		// bare colon — emit as word (POSIX : builtin)
		l.emit(TWord, ":")
	}
}

func (l *Lexer) lexDoubleQuote() {
	l.pos++ // skip opening "
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
		// $var expansion inside strings — leave as-is, expand at eval time
		buf.WriteByte(l.src[l.pos])
		l.pos++
	}
	if l.pos < len(l.src) {
		l.pos++ // skip closing "
	} else if l.err == "" {
		l.err = "unterminated double-quoted string"
	}
	l.emit(TString, buf.String())
}

func (l *Lexer) lexSingleQuote() {
	l.pos++ // skip opening '
	start := l.pos
	for l.pos < len(l.src) && l.src[l.pos] != '\'' {
		l.pos++
	}
	l.tokens = append(l.tokens, Token{Type: TString, Val: l.src[start:l.pos], Pos: start, Quoted: true})
	if l.pos < len(l.src) {
		l.pos++ // skip closing '
	} else if l.err == "" {
		l.err = "unterminated single-quoted string"
	}
}

func (l *Lexer) lexHeredoc() {
	// Skip optional - (for <<- which strips leading tabs)
	stripTabs := false
	if l.pos < len(l.src) && l.src[l.pos] == '-' {
		stripTabs = true
		l.pos++
	}

	l.skipSpaces()

	// Read delimiter word (may be quoted)
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
			l.pos++ // skip closing quote
		}
	} else {
		start := l.pos
		for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != ' ' && l.src[l.pos] != '\t' {
			l.pos++
		}
		delim = l.src[start:l.pos]
	}

	if delim == "" {
		l.emit(THeredoc, "<<")
		return
	}

	// Skip to next newline (rest of the command line after <<DELIM)
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
	if l.pos < len(l.src) {
		l.pos++ // skip newline
	}

	// Collect heredoc body until we find a line that is exactly the delimiter
	var body strings.Builder
	for l.pos < len(l.src) {
		// Find end of current line
		lineStart := l.pos
		for l.pos < len(l.src) && l.src[l.pos] != '\n' {
			l.pos++
		}
		line := l.src[lineStart:l.pos]
		if l.pos < len(l.src) {
			l.pos++ // skip newline
		}

		// Check if this line is the delimiter
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
		content = content[:len(content)-1] // remove trailing newline
	}

	// For unquoted heredocs, process backslash-newline continuation
	if !quoted {
		content = strings.ReplaceAll(content, "\\\n", "")
	}

	// Emit as a heredoc redirect: THeredoc token followed by string content
	l.emit(THeredoc, "<<")
	if quoted {
		// Quoted delimiter: no expansion
		l.tokens = append(l.tokens, Token{Type: TString, Val: content, Pos: l.pos, Quoted: true})
	} else {
		// Unquoted delimiter: subject to expansion
		l.tokens = append(l.tokens, Token{Type: TString, Val: content, Pos: l.pos})
	}
}

// lexBacktick handles `cmd` backtick substitution — emitted as $(cmd) token.
// Inside backticks, \ followed by $, `, \, or " strips the backslash (POSIX).
func (l *Lexer) lexBacktick() {
	l.pos++ // skip opening `
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
		l.pos++ // skip closing `
	}
	// Emit as $(content) — same as command substitution
	l.emit(TWord, "$("+content+")")
}

// lexFlag handles -flag, --flag, -d=, --color=auto, etc.
func (l *Lexer) lexFlag() {
	start := l.pos
	l.pos++ // skip first -
	if l.pos < len(l.src) && l.src[l.pos] == '-' {
		l.pos++ // skip second - for --flag
	}
	// Consume flag name (alphanumeric, -, _)
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' {
			l.pos++
		} else {
			break
		}
	}
	// Consume =value if present
	if l.pos < len(l.src) && l.src[l.pos] == '=' {
		l.pos++ // consume =
		// Consume the value — stop at whitespace or shell metacharacters
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
	l.emit(TWord, l.src[start:l.pos])
}

func (l *Lexer) lexDollar() {
	if l.peek(1) == '(' {
		if l.peek(2) == '(' {
			// $(( arithmetic ))
			l.pos += 3
			start := l.pos
			depth := 1
			for l.pos < len(l.src) && depth > 0 {
				ch := l.src[l.pos]
				// Skip quoted strings so parens inside don't affect depth
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
			l.emit(TWord, "$(("+l.src[start:l.pos]+"))")
			if l.pos < len(l.src) {
				l.pos += 2 // skip ))
			}
		} else {
			// $( command substitution )
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
			l.emit(TWord, "$("+l.src[start:l.pos]+")")
			if l.pos < len(l.src) {
				l.pos++ // skip )
			}
		}
	} else if l.peek(1) == '{' {
		// ${var}
		l.pos += 2
		start := l.pos
		for l.pos < len(l.src) && l.src[l.pos] != '}' {
			l.pos++
		}
		l.emit(TWord, "${"+l.src[start:l.pos]+"}")
		if l.pos < len(l.src) {
			l.pos++ // skip }
		}
	} else {
		// $var or $? etc.
		l.pos++
		start := l.pos
		if l.pos < len(l.src) && (l.src[l.pos] == '?' || l.src[l.pos] == '$' ||
			l.src[l.pos] == '!' || l.src[l.pos] == '@' || l.src[l.pos] == '*' ||
			l.src[l.pos] == '#' || (l.src[l.pos] >= '0' && l.src[l.pos] <= '9')) {
			l.pos++
			l.emit(TWord, "$"+l.src[start:l.pos])
		} else {
			for l.pos < len(l.src) && (isWordChar(l.src[l.pos]) || l.src[l.pos] == '_') {
				l.pos++
			}
			if l.pos > start {
				l.emit(TWord, "$"+l.src[start:l.pos])
			} else {
				l.emit(TWord, "$")
			}
		}
	}
}

func isWordChar(ch byte) bool {
	return ch == '_' || ch == '-' || ch == '/' || ch == '.' || ch == '~' ||
		ch == '@' || ch == ':' || ch == '+' ||
		(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') ||
		ch > 127 // utf-8 continuation
}

func isAlphaNum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}
