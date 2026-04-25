package lexer

import (
	"strings"

	"ish/internal/ast"
)

func Lex(src string) []ast.Token {
	l := &lexer{src: src}
	l.run()
	return l.tokens
}

type lexer struct {
	src      string
	pos      int
	tokens   []ast.Token
	segStart int // tracks literal segment start inside interpolated strings
}

func (l *lexer) peek() byte {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *lexer) peekAt(offset int) byte {
	i := l.pos + offset
	if i >= len(l.src) {
		return 0
	}
	return l.src[i]
}

func (l *lexer) advance() byte {
	ch := l.src[l.pos]
	l.pos++
	return ch
}

func (l *lexer) emit(t ast.TokenType, val string, startPos int) {
	l.tokens = append(l.tokens, ast.Token{Type: t, Val: val, Pos: startPos})
}

func unescapeString(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case '$':
				b.WriteByte('$')
			default:
				b.WriteByte('\\')
				b.WriteByte(s[i])
			}
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func (l *lexer) markSpaceAfter() {
	if len(l.tokens) > 0 {
		l.tokens[len(l.tokens)-1].SpaceAfter = true
	}
}

func (l *lexer) run() {
	for l.pos < len(l.src) {
		l.lexOne()
	}
	l.emit(ast.TEOF, "", l.pos)
}

// lexOne lexes a single token at the current position. This is the
// single dispatch function — run() loops it, and interpolation contexts
// call it directly. No duplication.
func (l *lexer) lexOne() {
	l.skipSpaces()
	if l.pos >= len(l.src) {
		return
	}
	ch := l.peek()
	pos := l.pos

	switch {
	case ch == '\n':
		l.advance()
		l.emit(ast.TNewline, "\n", pos)

	case ch == '#':
		if l.isCommentPosition() {
			l.skipComment()
		} else {
			l.advance()
			if l.peek() == '{' {
				l.advance()
				l.emit(ast.THashLBrace, "#{", pos)
			} else {
				l.emit(ast.THash, "#", pos)
			}
		}

	case ch == ';':
		l.advance()
		if l.peek() == ';' {
			l.advance()
			l.emit(ast.TDoubleSemicolon, ";;", pos)
		} else {
			l.emit(ast.TSemicolon, ";", pos)
		}

	case ch == '\'':
		l.lexSingleQuote()

	case ch == '"':
		l.lexDoubleQuote()

	case ch == ':':
		if l.peekAt(1) != 0 && isIdentStart(l.peekAt(1)) && l.isAtomPosition() {
			l.lexAtom()
		} else {
			l.advance()
			l.emit(ast.TColon, ":", pos)
		}

	case ch == '$':
		l.lexDollar()

	case isDigit(ch):
		l.lexNumber()

	case isIdentStart(ch):
		l.lexIdent()

	case ch == '|':
		l.advance()
		if l.peek() == '>' {
			l.advance()
			l.emit(ast.TPipeArrow, "|>", pos)
		} else if l.peek() == '|' {
			l.advance()
			l.emit(ast.TOr, "||", pos)
		} else if l.peek() == '&' {
			l.advance()
			l.emit(ast.TPipeAmp, "|&", pos)
		} else {
			l.emit(ast.TPipe, "|", pos)
		}

	case ch == '&':
		l.advance()
		if l.peek() == '&' {
			l.advance()
			l.emit(ast.TAnd, "&&", pos)
		} else {
			l.emit(ast.TAmpersand, "&", pos)
		}

	case ch == '>':
		l.advance()
		if l.peek() == '>' {
			l.advance()
			l.emit(ast.TAppend, ">>", pos)
		} else if l.peek() == '=' {
			l.advance()
			l.emit(ast.TGtEq, ">=", pos)
		} else {
			l.emit(ast.TGt, ">", pos)
		}

	case ch == '<':
		l.advance()
		if l.peek() == '<' {
			l.advance()
			if l.peek() == '<' {
				l.advance()
				l.emit(ast.THereString, "<<<", pos)
			} else {
				l.lexHeredoc(pos)
			}
		} else if l.peek() == '=' {
			l.advance()
			l.emit(ast.TLtEq, "<=", pos)
		} else {
			l.emit(ast.TLt, "<", pos)
		}

	case ch == '=':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			l.emit(ast.TEqEq, "==", pos)
		} else {
			l.emit(ast.TAssign, "=", pos)
		}

	case ch == '!':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			l.emit(ast.TBangEq, "!=", pos)
		} else {
			l.emit(ast.TBang, "!", pos)
		}

	case ch == '-':
		l.advance()
		if l.peek() == '>' {
			l.advance()
			l.emit(ast.TArrow, "->", pos)
		} else {
			l.emit(ast.TMinus, "-", pos)
		}

	case ch == '+':
		l.advance()
		l.emit(ast.TPlus, "+", pos)
	case ch == '*':
		l.advance()
		l.emit(ast.TStar, "*", pos)
	case ch == '/':
		l.advance()
		l.emit(ast.TSlash, "/", pos)
	case ch == '%':
		l.advance()
		l.emit(ast.TPercent, "%", pos)
	case ch == '.':
		l.advance()
		l.emit(ast.TDot, ".", pos)
	case ch == ',':
		l.advance()
		l.emit(ast.TComma, ",", pos)
	case ch == '(':
		l.advance()
		l.emit(ast.TLParen, "(", pos)
	case ch == ')':
		l.advance()
		l.emit(ast.TRParen, ")", pos)
	case ch == '[':
		l.advance()
		l.emit(ast.TLBracket, "[", pos)
	case ch == ']':
		l.advance()
		l.emit(ast.TRBracket, "]", pos)
	case ch == '{':
		l.advance()
		l.emit(ast.TLBrace, "{", pos)
	case ch == '}':
		l.advance()
		l.emit(ast.TRBrace, "}", pos)
	case ch == '\\':
		l.advance()
		if l.peek() == '\n' {
			l.advance()
		} else {
			l.emit(ast.TBackslash, "\\", pos)
		}
	case ch == '@':
		l.advance()
		l.emit(ast.TAt, "@", pos)
	case ch == '~':
		l.advance()
		l.emit(ast.TTilde, "~", pos)

	case ch == '`':
		l.lexBacktick()

	default:
		l.advance()
	}
}

func (l *lexer) lexBacktick() {
	pos := l.pos
	l.advance() // skip `
	l.emit(ast.TDollarLParen, "$(", pos)
	// Lex content until closing backtick
	for l.pos < len(l.src) && l.src[l.pos] != '`' {
		l.lexOne()
	}
	if l.pos < len(l.src) {
		l.advance() // skip closing `
	}
	l.emit(ast.TRParen, ")", l.pos-1)
}

// lexDollar handles all $-prefixed tokens: $var, $(, $((, ${, $?, etc.
func (l *lexer) lexDollar() {
	pos := l.pos
	l.advance() // skip $
	next := l.peek()
	switch {
	case next == '(':
		l.advance()
		if l.peek() == '(' {
			l.advance()
			l.emit(ast.TDollarDLParen, "$((", pos)
			l.lexUntilDoubleClose(')')
		} else {
			l.emit(ast.TDollarLParen, "$(", pos)
			l.lexNestedUntil('(', ')')
		}
	case next == '{':
		l.advance()
		l.emit(ast.TDollarLBrace, "${", pos)
	case next == '?' || next == '$' || next == '!' || next == '@' || next == '*' || next == '#' || (next >= '0' && next <= '9'):
		l.advance()
		l.emit(ast.TSpecialVar, "$"+string(next), pos)
	case isIdentStart(next):
		l.emit(ast.TDollar, "$", pos)
		l.lexIdent()
	default:
		l.emit(ast.TDollar, "$", pos)
	}
}

// lexDollarInString handles $ inside double-quoted strings, where ${
// consumes its content inline as raw text.
func (l *lexer) lexDollarInString() {
	pos := l.pos
	l.advance() // skip $
	next := l.peek()
	switch {
	case next == '(':
		l.advance()
		if l.peek() == '(' {
			l.advance()
			l.emit(ast.TDollarDLParen, "$((", pos)
			l.lexUntilDoubleClose(')')
		} else {
			l.emit(ast.TDollarLParen, "$(", pos)
			l.lexNestedUntil('(', ')')
		}
	case next == '{':
		l.advance()
		l.emit(ast.TDollarLBrace, "${", pos)
		contentStart := l.pos
		depth := 1
		for l.pos < len(l.src) && depth > 0 {
			if l.src[l.pos] == '{' {
				depth++
			} else if l.src[l.pos] == '}' {
				depth--
				if depth == 0 {
					break
				}
			}
			l.pos++
		}
		if contentStart < l.pos {
			l.emit(ast.TString, l.src[contentStart:l.pos], contentStart)
		}
		if l.pos < len(l.src) {
			l.emit(ast.TRBrace, "}", l.pos)
			l.pos++
		}
	case next == '?' || next == '$' || next == '!' || next == '@' || next == '*' || next == '#' || (next >= '0' && next <= '9'):
		l.advance()
		l.emit(ast.TSpecialVar, "$"+string(next), pos)
	case isIdentStart(next):
		l.emit(ast.TDollar, "$", pos)
		l.lexIdent()
	default:
		l.emit(ast.TDollar, "$", pos)
	}
}

// lexNestedUntil lexes tokens inside a nested delimiter pair (e.g., $(...))
// using lexOne for each token, tracking depth.
func (l *lexer) lexNestedUntil(open, close byte) {
	depth := 1
	for l.pos < len(l.src) && depth > 0 {
		ch := l.src[l.pos]
		if ch == open {
			depth++
		} else if ch == close {
			depth--
			if depth == 0 {
				break
			}
		}
		l.lexOne()
	}
	if l.pos < len(l.src) && l.src[l.pos] == close {
		l.emit(ast.TRParen, string(close), l.pos)
		l.pos++
	}
}

// lexUntilDoubleClose lexes tokens until a double-close (e.g., )) for $((...)))
func (l *lexer) lexUntilDoubleClose(close byte) {
	for l.pos < len(l.src) {
		// Skip spaces first (same as lexOne does) so we check the actual next char
		l.skipSpaces()
		if l.pos >= len(l.src) {
			return
		}
		if l.src[l.pos] == close && l.peekAt(1) == close {
			l.emit(ast.TDoubleRParen, "))", l.pos)
			l.pos += 2
			return
		}
		l.lexOne()
	}
}

func (l *lexer) skipSpaces() {
	start := l.pos
	for l.pos < len(l.src) && (l.src[l.pos] == ' ' || l.src[l.pos] == '\t') {
		l.pos++
	}
	if l.pos > start {
		l.markSpaceAfter()
	}
}

func (l *lexer) isCommentPosition() bool {
	if len(l.tokens) == 0 {
		return true
	}
	last := l.tokens[len(l.tokens)-1]
	if last.SpaceAfter {
		return true
	}
	switch last.Type {
	case ast.TNewline, ast.TSemicolon, ast.TDoubleSemicolon,
		ast.TPipe, ast.TPipeArrow, ast.TAnd, ast.TOr, ast.TAmpersand,
		ast.TLParen, ast.TLBrace:
		return true
	}
	return false
}

func (l *lexer) isAtomPosition() bool {
	if len(l.tokens) == 0 {
		return true
	}
	last := l.tokens[len(l.tokens)-1]
	if last.SpaceAfter {
		return true
	}
	switch last.Type {
	case ast.TNewline, ast.TSemicolon, ast.TDoubleSemicolon,
		ast.TPipe, ast.TPipeArrow, ast.TAnd, ast.TOr, ast.TAmpersand,
		ast.TLParen, ast.TLBrace, ast.TLBracket,
		ast.TComma, ast.TAssign, ast.TArrow,
		ast.TStringStart, ast.THashLBrace:
		return true
	}
	return false
}

func (l *lexer) skipComment() {
	l.pos++
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
}

func (l *lexer) lexSingleQuote() {
	pos := l.pos
	l.advance()
	start := l.pos
	for l.pos < len(l.src) && l.src[l.pos] != '\'' {
		l.pos++
	}
	val := l.src[start:l.pos]
	if l.pos < len(l.src) {
		l.advance()
	}
	l.emit(ast.TString, val, pos)
}

func (l *lexer) lexDoubleQuote() {
	pos := l.pos
	l.advance() // skip opening "

	hasInterp := false
	for scan := l.pos; scan < len(l.src) && l.src[scan] != '"'; scan++ {
		if l.src[scan] == '\\' {
			scan++
			continue
		}
		if l.src[scan] == '$' || (l.src[scan] == '#' && scan+1 < len(l.src) && l.src[scan+1] == '{') {
			hasInterp = true
			break
		}
	}

	if !hasInterp {
		start := l.pos
		for l.pos < len(l.src) && l.src[l.pos] != '"' {
			if l.src[l.pos] == '\\' && l.pos+1 < len(l.src) {
				l.pos++
			}
			l.pos++
		}
		val := unescapeString(l.src[start:l.pos])
		if l.pos < len(l.src) {
			l.advance()
		}
		l.emit(ast.TString, val, pos)
		return
	}

	l.emit(ast.TStringStart, "\"", pos)
	l.segStart = l.pos

	for l.pos < len(l.src) && l.src[l.pos] != '"' {
		ch := l.src[l.pos]

		if ch == '\\' && l.pos+1 < len(l.src) {
			l.pos += 2
			continue
		}

		if ch == '#' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{' {
			l.flushSeg()
			interpPos := l.pos
			l.pos += 2
			l.emit(ast.THashLBrace, "#{", interpPos)
			depth := 1
			for l.pos < len(l.src) && depth > 0 {
				if l.src[l.pos] == '{' {
					depth++
				} else if l.src[l.pos] == '}' {
					depth--
					if depth == 0 {
						break
					}
				}
				l.lexOne()
			}
			if l.pos < len(l.src) && l.src[l.pos] == '}' {
				l.emit(ast.TRBrace, "}", l.pos)
				l.pos++
			}
			l.segStart = l.pos
			continue
		}

		if ch == '$' {
			l.flushSeg()
			l.lexDollarInString()
			l.segStart = l.pos
			continue
		}

		l.pos++
	}

	l.flushSeg()
	endPos := l.pos
	if l.pos < len(l.src) {
		l.pos++
	}
	l.emit(ast.TStringEnd, "\"", endPos)
}

// flushSeg emits accumulated literal text since segStart.
func (l *lexer) flushSeg() {
	if l.segStart < l.pos {
		l.emit(ast.TString, l.src[l.segStart:l.pos], l.segStart)
	}
}

func (l *lexer) lexHeredoc(startPos int) {
	stripTabs := false
	if l.pos < len(l.src) && l.src[l.pos] == '-' {
		stripTabs = true
		l.pos++
	}
	l.emit(ast.THeredoc, "<<", startPos)

	// Read delimiter word
	l.skipSpaces()
	quoted := false
	delimStart := l.pos
	if l.pos < len(l.src) && (l.src[l.pos] == '\'' || l.src[l.pos] == '"') {
		quoted = true
		q := l.src[l.pos]
		l.pos++
		delimStart = l.pos
		for l.pos < len(l.src) && l.src[l.pos] != q {
			l.pos++
		}
		delimEnd := l.pos
		if l.pos < len(l.src) {
			l.pos++
		}
		l.emit(ast.TIdent, l.src[delimStart:delimEnd], delimStart)
	} else {
		for l.pos < len(l.src) && isIdentChar(l.src[l.pos]) {
			l.pos++
		}
		l.emit(ast.TIdent, l.src[delimStart:l.pos], delimStart)
	}

	delim := l.src[delimStart:l.pos]
	if quoted {
		delim = l.tokens[len(l.tokens)-1].Val
	}

	// Skip to end of current line
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
	if l.pos < len(l.src) {
		l.pos++ // skip newline
	}

	// Collect content lines until delimiter
	contentStart := l.pos
	var contentLines []string
	for l.pos < len(l.src) {
		lineStart := l.pos
		for l.pos < len(l.src) && l.src[l.pos] != '\n' {
			l.pos++
		}
		line := l.src[lineStart:l.pos]
		checkLine := line
		if stripTabs {
			checkLine = strings.TrimLeft(line, "\t")
		}
		if checkLine == delim {
			if l.pos < len(l.src) {
				l.pos++ // skip newline after delimiter
			}
			break
		}
		if stripTabs {
			line = strings.TrimLeft(line, "\t")
		}
		contentLines = append(contentLines, line)
		if l.pos < len(l.src) {
			l.pos++ // skip newline
		}
	}

	content := strings.Join(contentLines, "\n")
	if len(contentLines) > 0 {
		content += "\n"
	}

	if quoted {
		l.emit(ast.TString, content, contentStart)
	} else {
		// Lex content for expansions like double-quoted strings
		sub := &lexer{src: content, pos: 0}
		sub.emit(ast.TStringStart, "<<", contentStart)
		sub.segStart = 0
		for sub.pos < len(sub.src) {
			ch := sub.src[sub.pos]
			if ch == '$' {
				sub.flushSeg()
				sub.lexDollarInString()
				sub.segStart = sub.pos
			} else {
				sub.pos++
			}
		}
		sub.flushSeg()
		sub.emit(ast.TStringEnd, "<<", contentStart+len(content))
		l.tokens = append(l.tokens, sub.tokens...)
	}
}

func (l *lexer) lexAtom() {
	pos := l.pos
	l.advance()
	start := l.pos
	for l.pos < len(l.src) && isIdentChar(l.src[l.pos]) {
		l.pos++
	}
	l.emit(ast.TAtom, l.src[start:l.pos], pos)
}

func (l *lexer) lexNumber() {
	pos := l.pos
	for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
		l.pos++
	}
	intEnd := l.pos
	isFloat := false
	if l.pos < len(l.src) && l.src[l.pos] == '.' && l.peekAt(1) != 0 && isDigit(l.peekAt(1)) {
		l.pos++
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
		if l.pos < len(l.src) && l.src[l.pos] == '.' {
			l.pos = intEnd
		} else {
			isFloat = true
		}
	}
	val := l.src[pos:l.pos]
	if isFloat {
		l.emit(ast.TFloat, val, pos)
	} else {
		l.emit(ast.TInt, val, pos)
	}
}

func (l *lexer) lexIdent() {
	pos := l.pos
	for l.pos < len(l.src) && isIdentChar(l.src[l.pos]) {
		l.pos++
	}
	l.emit(ast.TIdent, l.src[pos:l.pos], pos)
}

func isDigit(ch byte) bool      { return ch >= '0' && ch <= '9' }
func isIdentStart(ch byte) bool { return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' }
func isIdentChar(ch byte) bool  { return isIdentStart(ch) || isDigit(ch) }
