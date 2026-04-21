package lexer

import (
	"fmt"
	"strings"
	"unicode"

	"ish/internal/ast"
)

// Keywords maps identifier strings to their keyword token types.
var keywords = map[string]ast.TokenType{
	"if": ast.TIf, "then": ast.TThen, "elif": ast.TElif, "else": ast.TElse, "fi": ast.TFi,
	"for": ast.TFor, "in": ast.TIn, "do": ast.TDo, "done": ast.TDone,
	"while": ast.TWhile, "until": ast.TUntil,
	"case": ast.TCase, "esac": ast.TEsac,
	"fn": ast.TFn, "end": ast.TEnd,
	"defmodule": ast.TDefModule, "use": ast.TUse, "match": ast.TMatch,
	"spawn": ast.TSpawn, "spawn_link": ast.TSpawnLink,
	"send": ast.TSend, "monitor": ast.TMonitor, "await": ast.TAwait,
	"supervise": ast.TSupervise, "receive": ast.TReceive,
	"try": ast.TTry, "rescue": ast.TRescue, "after": ast.TAfter,
	"nil": ast.TNil, "true": ast.TTrue, "false": ast.TFalse,
}

type Lexer struct {
	src         string
	pos         int
	tokens      []ast.Token
	readPos     int
	lastEmitted ast.TokenType
	err         string
	done        bool
}

func New(src string) *Lexer {
	return &Lexer{src: src, lastEmitted: ast.TEOF}
}

// NewFromTokens creates a Lexer pre-loaded with tokens (for cases that need
// token manipulation between lexing and parsing).
func NewFromTokens(tokens []ast.Token) *Lexer {
	return &Lexer{tokens: tokens, done: true, lastEmitted: ast.TEOF}
}

func (l *Lexer) SourcePos() int {
	return l.pos
}

func (l *Lexer) SetPos(pos int) {
	l.pos = pos
	l.done = false
}

func (l *Lexer) SetLastEmitted(t ast.TokenType) {
	l.lastEmitted = t
}

// Truncate discards all buffered tokens from index n onwards and resets
// readPos if it was past n.
func (l *Lexer) Truncate(n int) {
	if n < len(l.tokens) {
		l.tokens = l.tokens[:n]
	}
	if l.readPos > n {
		l.readPos = n
	}
	l.done = false
}

func (l *Lexer) Error() string {
	return l.err
}

func Lex(src string) []ast.Token {
	l := New(src)
	l.lex()
	return l.tokens
}

func LexCheck(src string) ([]ast.Token, error) {
	l := New(src)
	l.lex()
	if l.err != "" {
		return l.tokens, fmt.Errorf("%s", l.err)
	}
	return l.tokens, nil
}

func (l *Lexer) lex() {
	for {
		if l.lexStep() {
			l.emit(ast.TEOF, "")
			return
		}
	}
}

// NextToken returns the next token, lexing on demand.
func (l *Lexer) NextToken() ast.Token {
	for l.readPos >= len(l.tokens) {
		if l.done {
			return ast.Token{Type: ast.TEOF, Pos: l.pos}
		}
		if l.lexStep() {
			l.emit(ast.TEOF, "")
			l.done = true
		}
	}
	tok := l.tokens[l.readPos]
	// Ensure SpaceAfter is set: lex one more step if needed so
	// skipSpaces has a chance to mark this token.
	if l.readPos+1 >= len(l.tokens) && !l.done {
		if l.lexStep() {
			l.emit(ast.TEOF, "")
			l.done = true
		}
		// Re-read the token since skipSpaces may have updated SpaceAfter
		tok = l.tokens[l.readPos]
	}
	l.readPos++
	return tok
}

// lexStep runs one iteration of the lexer loop. Returns true if EOF is reached.
// No mode checks — every character produces the same token regardless of context.
func (l *Lexer) lexStep() bool {
	l.skipSpaces()
	if l.pos >= len(l.src) {
		return true
	}
	ch := l.src[l.pos]

	// Comments: # starts a comment ONLY at word boundary (after whitespace,
	// newline, semicolon, or at start of input). Mid-word # is a literal.
	// #{  is always string interpolation, never a comment.
	if ch == '#' {
		if l.isInterpolation() {
			// #{ — handled below as THashLBrace
		} else if l.isCommentPosition() {
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
			return false
		} else {
			// Mid-word #: literal character
			l.emit(ast.THash, "#")
			l.pos++
			return false
		}
	}

	switch {
	case ch == '\n':
		l.emit(ast.TNewline, "\n")
		l.pos++

	// Pipes
	case ch == '|':
		if l.peek(1) == '>' {
			l.emit(ast.TPipeArrow, "|>")
			l.pos += 2
		} else if l.peek(1) == '|' {
			l.emit(ast.TOr, "||")
			l.pos += 2
		} else if l.peek(1) == '&' {
			l.emit(ast.TPipeStderr, "|&")
			l.pos += 2
		} else {
			l.emit(ast.TPipe, "|")
			l.pos++
		}

	// Ampersand
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

	// Delimiters — unconditional, no hints
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

	// Operators — always the same token, no mode checks
	case ch == '.':
		l.emit(ast.TDot, ".")
		l.pos++
	case ch == '/':
		l.emit(ast.TDiv, "/")
		l.pos++
	case ch == '+':
		l.emit(ast.TPlus, "+")
		l.pos++
	case ch == '*':
		l.emit(ast.TMul, "*")
		l.pos++
	case ch == '%':
		if l.peek(1) == '{' {
			l.emit(ast.TPercentLBrace, "%{")
			l.pos += 2
		} else {
			l.emit(ast.TPercent, "%")
			l.pos++
		}
	case ch == '~':
		l.emit(ast.TTilde, "~")
		l.pos++
	case ch == '@':
		l.emit(ast.TAt, "@")
		l.pos++

	case ch == '-':
		if l.peek(1) == '>' {
			l.emit(ast.TArrow, "->")
			l.pos += 2
		} else {
			l.emit(ast.TMinus, "-")
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
			l.emit(ast.TLt, "<")
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
			l.emit(ast.TGt, ">")
			l.pos++
		}

	// Expansion delimiters
	case ch == '$':
		l.lexDollar()

	// String interpolation: #{
	case ch == '#' && l.isInterpolation():
		l.emit(ast.THashLBrace, "#{")
		l.pos += 2

	// Strings
	case ch == '"':
		l.lexDoubleQuote()
	case ch == '\'':
		l.lexSingleQuote()

	// Atoms: :name
	case ch == ':':
		l.lexAtom()

	// Numbers
	case ch >= '0' && ch <= '9':
		l.lexNumber()

	// Backtick (legacy command substitution)
	case ch == '`':
		l.lexBacktick()

	// Backslash
	case ch == '\\':
		if l.peek(1) == '\n' {
			l.pos += 2 // line continuation
		} else {
			l.emit(ast.TBackslash, "\\")
			l.pos++
		}

	// Identifiers (and keywords)
	default:
		if isIdentStart(ch) {
			l.lexIdent()
		} else if ch > 127 {
			// Unicode identifier
			l.lexIdent()
		} else {
			// Unknown character — emit as single-char identifier
			l.emit(ast.TIdent, string(ch))
			l.pos++
		}
	}
	return false
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

// isCommentPosition returns true if # at the current position should start
// a comment. In POSIX sh, # is only a comment character at word boundary:
// after whitespace, newline, semicolon, or at start of input.
func (l *Lexer) isCommentPosition() bool {
	// # is a comment at word boundary: after whitespace or at start of input.
	// Since we don't emit TWhitespace, check if the previous token has SpaceAfter.
	if len(l.tokens) == 0 {
		return true // start of input
	}
	prev := l.tokens[len(l.tokens)-1]
	if prev.SpaceAfter {
		return true
	}
	switch prev.Type {
	case ast.TNewline, ast.TSemicolon,
		ast.TPipe, ast.TPipeStderr, ast.TPipeArrow, ast.TAnd, ast.TOr,
		ast.TAmpersand, ast.TLParen, ast.TLBrace:
		return true
	}
	return false
}

func (l *Lexer) emit(t ast.TokenType, val string) {
	l.tokens = append(l.tokens, ast.Token{Type: t, Val: val, Pos: l.pos})
	l.lastEmitted = t
}

func (l *Lexer) skipSpaces() {
	if l.pos < len(l.src) && (l.src[l.pos] == ' ' || l.src[l.pos] == '\t' || l.src[l.pos] == '\r') {
		if len(l.tokens) > 0 {
			l.tokens[len(l.tokens)-1].SpaceAfter = true
		}
		for l.pos < len(l.src) && (l.src[l.pos] == ' ' || l.src[l.pos] == '\t' || l.src[l.pos] == '\r') {
			l.pos++
		}
	}
}

// isIdentStart returns true if ch can start an identifier.
func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

// isIdentChar returns true if ch can continue an identifier.
func isIdentChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') || ch == '_' || ch > 127
}

// IsAlphaNum returns true if r is a letter, digit, or underscore. Used externally.
func IsAlphaNum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// lexIdent scans an identifier and emits either a keyword token or TIdent.
func (l *Lexer) lexIdent() {
	start := l.pos
	for l.pos < len(l.src) && isIdentChar(l.src[l.pos]) {
		l.pos++
	}
	word := l.src[start:l.pos]
	if kwTok, ok := keywords[word]; ok {
		l.emit(kwTok, word)
	} else {
		l.emit(ast.TIdent, word)
	}
}

// lexNumber scans integer or float literals.
func (l *Lexer) lexNumber() {
	start := l.pos
	for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
		l.pos++
	}
	// Float: digits followed by '.' followed by digit
	if l.pos < len(l.src) && l.src[l.pos] == '.' && l.pos+1 < len(l.src) && l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '9' {
		l.pos++ // consume '.'
		for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
			l.pos++
		}
		l.emit(ast.TFloat, l.src[start:l.pos])
		return
	}
	// Number followed by letter/underscore → treat as identifier (e.g. "3rd")
	if l.pos < len(l.src) && isIdentStart(l.src[l.pos]) {
		for l.pos < len(l.src) && isIdentChar(l.src[l.pos]) {
			l.pos++
		}
		l.emit(ast.TIdent, l.src[start:l.pos])
		return
	}
	l.emit(ast.TInt, l.src[start:l.pos])
}

// lexAtom scans :name atoms. If ':' is not followed by an identifier, emits TColon.
func (l *Lexer) lexAtom() {
	l.pos++ // skip ':'
	start := l.pos
	for l.pos < len(l.src) && isIdentChar(l.src[l.pos]) {
		l.pos++
	}
	if l.pos > start {
		if l.src[start] >= '0' && l.src[start] <= '9' {
			if l.err == "" {
				l.err = "atom name cannot start with a digit"
			}
		}
		l.emit(ast.TAtom, l.src[start:l.pos])
	} else {
		l.emit(ast.TColon, ":")
	}
}

// lexDollar handles all $-prefixed constructs by emitting expansion delimiter tokens.
// The interior is lexed normally by subsequent lexStep calls.
// The parser matches opening/closing delimiters and builds structured nodes.
func (l *Lexer) lexDollar() {
	if l.peek(1) == '"' {
		l.lexDollarDoubleQuote()
		return
	}
	if l.peek(1) == '(' {
		if l.peek(2) == '(' {
			// $(( — arithmetic expansion
			l.emit(ast.TDollarDLParen, "$((")
			l.pos += 3
		} else {
			// $( — command substitution
			l.emit(ast.TDollarLParen, "$(")
			l.pos += 2
		}
		return
	}
	if l.peek(1) == '{' {
		// ${ — parameter expansion
		l.emit(ast.TDollarLBrace, "${")
		l.pos += 2
		return
	}

	// $? $$ $! $@ $* $# $0-$9 — special variables
	l.pos++ // skip '$'
	if l.pos < len(l.src) {
		ch := l.src[l.pos]
		if ch == '?' || ch == '$' || ch == '!' || ch == '@' || ch == '*' ||
			ch == '#' || (ch >= '0' && ch <= '9') {
			l.emit(ast.TSpecialVar, "$"+string(ch))
			l.pos++
			return
		}
	}

	// $identifier — variable reference
	start := l.pos
	for l.pos < len(l.src) && isIdentChar(l.src[l.pos]) {
		l.pos++
	}
	if l.pos > start {
		// Emit TDollar marker, then the variable name as TIdent
		varName := l.src[start:l.pos]
		l.emit(ast.TDollar, "$")
		l.emit(ast.TIdent, varName)
	} else {
		// Bare $ with no identifier
		l.emit(ast.TDollar, "$")
	}
}

// lexDoubleQuote handles "..." strings with embedded interpolation.
// Emits TStringStart, then alternating TString segments and expansion tokens,
// then TStringEnd. The parser builds NInterpString from the sequence.
//
// Example: "hello $name, $(cmd)" emits:
//   TStringStart, TString("hello "), TDollar, TIdent("name"), TString(", "), TDollarLParen, ..., TRParen, TStringEnd
func (l *Lexer) lexDoubleQuote() {
	l.emit(ast.TStringStart, "\"")
	l.pos++ // skip opening "

	var buf strings.Builder
	flushLiteral := func() {
		if buf.Len() > 0 {
			l.emit(ast.TString, buf.String())
			buf.Reset()
		}
	}

	for l.pos < len(l.src) && l.src[l.pos] != '"' {
		ch := l.src[l.pos]

		// Escape sequences
		if ch == '\\' && l.pos+1 < len(l.src) {
			next := l.src[l.pos+1]
			switch next {
			case '"', '\\', '$', '`', '\n':
				buf.WriteByte(next)
			default:
				buf.WriteByte('\\')
				buf.WriteByte(next)
			}
			l.pos += 2
			continue
		}

		// $( — command substitution inside string
		if ch == '$' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '(' {
			flushLiteral()
			if l.pos+2 < len(l.src) && l.src[l.pos+2] == '(' {
				// $(( — arithmetic inside string
				l.emit(ast.TDollarDLParen, "$((")
				l.pos += 3
				l.lexUntilDoubleClose()
			} else {
				l.emit(ast.TDollarLParen, "$(")
				l.pos += 2
				l.lexUntilClose(ast.TRParen)
			}
			continue
		}

		// ${ — parameter expansion inside string
		if ch == '$' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{' {
			flushLiteral()
			l.emit(ast.TDollarLBrace, "${")
			l.pos += 2
			l.lexUntilClose(ast.TRBrace)
			continue
		}

		// $var — variable reference inside string
		if ch == '$' && l.pos+1 < len(l.src) {
			next := l.src[l.pos+1]
			// Special variables: $?, $$, $!, $@, $*, $#, $0-$9
			if next == '?' || next == '$' || next == '!' || next == '@' || next == '*' ||
				next == '#' || (next >= '0' && next <= '9') {
				flushLiteral()
				l.emit(ast.TSpecialVar, "$"+string(next))
				l.pos += 2
				continue
			}
			// Named variable: $identifier
			if isIdentStart(next) {
				flushLiteral()
				l.pos++ // skip $
				start := l.pos
				for l.pos < len(l.src) && isIdentChar(l.src[l.pos]) {
					l.pos++
				}
				l.emit(ast.TDollar, "$")
				l.emit(ast.TIdent, l.src[start:l.pos])
				continue
			}
			// Bare $ not followed by expansion — literal
			buf.WriteByte('$')
			l.pos++
			continue
		}

		// #{ — ish string interpolation inside string
		if ch == '#' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{' {
			flushLiteral()
			l.emit(ast.THashLBrace, "#{")
			l.pos += 2
			l.lexUntilClose(ast.TRBrace)
			continue
		}

		// Backtick inside string — command substitution (legacy)
		if ch == '`' {
			flushLiteral()
			l.pos++
			var btBuf strings.Builder
			for l.pos < len(l.src) && l.src[l.pos] != '`' {
				if l.src[l.pos] == '\\' && l.pos+1 < len(l.src) {
					next := l.src[l.pos+1]
					if next == '$' || next == '`' || next == '\\' || next == '"' {
						btBuf.WriteByte(next)
						l.pos += 2
						continue
					}
				}
				btBuf.WriteByte(l.src[l.pos])
				l.pos++
			}
			if l.pos < len(l.src) {
				l.pos++ // skip closing `
			}
			l.emit(ast.TDollarLParen, "$(")
			l.emit(ast.TString, btBuf.String())
			l.emit(ast.TRParen, ")")
			continue
		}

		// Regular character
		buf.WriteByte(ch)
		l.pos++
	}

	flushLiteral()

	if l.pos < len(l.src) {
		l.pos++ // skip closing "
	} else if l.err == "" {
		l.err = "unterminated double-quoted string"
	}
	l.emit(ast.TStringEnd, "\"")
}

// lexUntilClose lexes tokens inside an expansion (within a string or at top level),
// using token-level depth tracking. Stops when the matching closer is found at depth 0.
// The closing token is emitted by this function.
func (l *Lexer) lexUntilClose(closerType ast.TokenType) {
	var openerType ast.TokenType
	switch closerType {
	case ast.TRParen:
		openerType = ast.TLParen
	case ast.TRBrace:
		openerType = ast.TLBrace
	}

	depth := 1
	for depth > 0 {
		if l.pos >= len(l.src) {
			return
		}
		before := len(l.tokens)
		if l.lexStep() {
			return // EOF
		}
		// Check tokens emitted by lexStep for depth tracking
		for i := before; i < len(l.tokens); i++ {
			tt := l.tokens[i].Type
			if tt == openerType || (closerType == ast.TRParen && tt == ast.TDollarLParen) || (closerType == ast.TRParen && tt == ast.TDollarDLParen) {
				depth++
			} else if tt == closerType {
				depth--
				if depth == 0 {
					return
				}
			}
		}
	}
}

// lexUntilDoubleClose handles )) for $((...)) inside strings.
// Tracks depth by looking for matching $(( and )) pairs.
func (l *Lexer) lexUntilDoubleClose() {
	depth := 1
	for depth > 0 {
		if l.pos >= len(l.src) {
			return
		}
		// Check for )) at character level before lexStep
		if l.src[l.pos] == ')' && l.pos+1 < len(l.src) && l.src[l.pos+1] == ')' {
			depth--
			if depth == 0 {
				l.emit(ast.TRParen, ")")
				l.emit(ast.TRParen, ")")
				l.pos += 2
				return
			}
		}
		if l.lexStep() {
			return
		}
	}
}

func (l *Lexer) lexSingleQuote() {
	l.pos++
	start := l.pos
	for l.pos < len(l.src) && l.src[l.pos] != '\'' {
		l.pos++
	}
	l.tokens = append(l.tokens, ast.Token{Type: ast.TString, Val: l.src[start:l.pos], Pos: start, Quoted: true})
	l.lastEmitted = ast.TString
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
		l.lastEmitted = ast.TString
	} else {
		// Unquoted heredoc: process content for $variable and $(cmd) expansion,
		// emitting the same token sequence as a double-quoted string.
		l.lexHeredocInterp(content)
	}
}

// lexHeredocInterp processes an unquoted heredoc body for variable/command
// expansion by temporarily splicing the content into the lexer's source
// and reusing the double-quote interpolation machinery.
func (l *Lexer) lexHeredocInterp(content string) {
	// Save lexer state
	savedSrc := l.src
	savedPos := l.pos

	// Inject content wrapped in double quotes so lexDoubleQuote handles it
	l.src = `"` + content + `"`
	l.pos = 0
	l.lexDoubleQuote()

	// Restore lexer state
	l.src = savedSrc
	l.pos = savedPos
}


// lexBacktick handles `...` legacy command substitution.
// Emits as TDollarLParen + TString(content) + TRParen to represent $(content).
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
	if l.pos < len(l.src) {
		l.pos++
	}
	// Emit as command substitution: $( content )
	// Recursively lex the backtick content so the parser sees real tokens.
	content := buf.String()
	l.emit(ast.TDollarLParen, "$(")

	savedSrc := l.src
	savedPos := l.pos
	l.src = content
	l.pos = 0
	for l.pos < len(l.src) {
		if l.lexStep() {
			break
		}
	}
	l.src = savedSrc
	l.pos = savedPos

	l.emit(ast.TRParen, ")")
}

// lexDollarDoubleQuote handles $"..." strings with C-style escapes and interpolation.
// Uses the same structured token approach as lexDoubleQuote.
func (l *Lexer) lexDollarDoubleQuote() {
	l.emit(ast.TDollarDQuote, "$\"")
	l.pos += 2 // skip $"

	var buf strings.Builder
	flushLiteral := func() {
		if buf.Len() > 0 {
			l.emit(ast.TString, buf.String())
			buf.Reset()
		}
	}

	for l.pos < len(l.src) && l.src[l.pos] != '"' {
		ch := l.src[l.pos]

		if ch == '\\' && l.pos+1 < len(l.src) {
			next := l.src[l.pos+1]
			switch next {
			case 't':
				buf.WriteByte('\t')
			case 'n':
				buf.WriteByte('\n')
			case 'r':
				buf.WriteByte('\r')
			case '0':
				buf.WriteByte(0)
			case '"', '\\', '$', '`':
				buf.WriteByte(next)
			case '\n':
				buf.WriteByte(next)
			default:
				buf.WriteByte('\\')
				buf.WriteByte(next)
			}
			l.pos += 2
			continue
		}

		// $( inside $"..."
		if ch == '$' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '(' {
			flushLiteral()
			l.emit(ast.TDollarLParen, "$(")
			l.pos += 2
			l.lexUntilClose(ast.TRParen)
			continue
		}

		// ${ inside $"..."
		if ch == '$' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{' {
			flushLiteral()
			l.emit(ast.TDollarLBrace, "${")
			l.pos += 2
			l.lexUntilClose(ast.TRBrace)
			continue
		}

		// $var inside $"..."
		if ch == '$' && l.pos+1 < len(l.src) {
			next := l.src[l.pos+1]
			if next == '?' || next == '$' || next == '!' || next == '@' || next == '*' ||
				next == '#' || (next >= '0' && next <= '9') {
				flushLiteral()
				l.emit(ast.TSpecialVar, "$"+string(next))
				l.pos += 2
				continue
			}
			if isIdentStart(next) {
				flushLiteral()
				l.pos++
				start := l.pos
				for l.pos < len(l.src) && isIdentChar(l.src[l.pos]) {
					l.pos++
				}
				l.emit(ast.TDollar, "$")
				l.emit(ast.TIdent, l.src[start:l.pos])
				continue
			}
		}

		// #{ inside $"..."
		if ch == '#' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{' {
			flushLiteral()
			l.emit(ast.THashLBrace, "#{")
			l.pos += 2
			l.lexUntilClose(ast.TRBrace)
			continue
		}

		buf.WriteByte(ch)
		l.pos++
	}

	flushLiteral()

	if l.pos < len(l.src) {
		l.pos++
	} else if l.err == "" {
		l.err = "unterminated dollar double-quoted string"
	}
	l.emit(ast.TStringEnd, "\"")
}
