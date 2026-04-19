package lexer

import (
	"fmt"
	"strings"
	"unicode"

	"ish/internal/ast"
)

type LexerMode int

const (
	LexerShell LexerMode = iota
	LexerExpr
)

type Lexer struct {
	src         string
	pos         int
	tokens      []ast.Token
	readPos     int
	lastEmitted ast.TokenType
	err         string
	mode        LexerMode
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

func (l *Lexer) SetMode(m LexerMode) {
	l.mode = m
}

func (l *Lexer) Mode() LexerMode {
	return l.mode
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
	l.readPos++
	return tok
}

// lexStep runs one iteration of the lexer loop. Returns true if EOF is reached.
func (l *Lexer) lexStep() bool {
	l.skipSpaces()
	if l.pos >= len(l.src) {
		return true
	}
	ch := l.src[l.pos]

	if ch == '#' && !l.isInterpolation() {
		for l.pos < len(l.src) && l.src[l.pos] != '\n' {
			l.pos++
		}
		return false
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
		if l.mode == LexerShell && l.looksLikeExprBrace() {
			l.tokens = append(l.tokens, ast.Token{Type: ast.TLBrace, Val: "{", Pos: l.pos, ExprHint: true})
			l.lastEmitted = ast.TLBrace
		} else {
			l.emit(ast.TLBrace, "{")
		}
		l.pos++
	case ch == '}':
		l.emit(ast.TRBrace, "}")
		l.pos++
	case ch == '[':
		if l.mode == LexerShell && l.looksLikeExprBracket() {
			l.tokens = append(l.tokens, ast.Token{Type: ast.TLBracket, Val: "[", Pos: l.pos, ExprHint: true})
			l.lastEmitted = ast.TLBracket
		} else {
			l.emit(ast.TLBracket, "[")
		}
		l.pos++
	case ch == ']':
		l.emit(ast.TRBracket, "]")
		l.pos++
	case ch == ',':
		l.emit(ast.TComma, ",")
		l.pos++
	case ch == '+':
		next := l.peek(1)
		if next >= 'a' && next <= 'z' || next >= 'A' && next <= 'Z' || next == '%' {
			l.lexWord()
		} else {
			l.emit(ast.TPlus, "+")
			l.pos++
		}
	case ch == '*':
		l.emit(ast.TMul, "*")
		l.pos++
	case ch == '/':
		if l.mode == LexerExpr {
			l.emit(ast.TDiv, "/")
			l.pos++
		} else {
			isDivision := false
			next := l.peek(1)
			isPathStart := (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') ||
				(next >= '0' && next <= '9') || next == '.' || next == '_' || next == '-'
			if !isPathStart {
				switch l.lastEmitted {
				case ast.TInt, ast.TFloat, ast.TRParen, ast.TRBracket:
					isDivision = true
				case ast.TWord:
					isDivision = next == 0 || next == ' ' || next == '\t' || next == '\n'
				}
			}
			if isDivision {
				l.emit(ast.TDiv, "/")
				l.pos++
			} else {
				l.lexWord()
			}
		}
	case ch == '.':
		next := l.peek(1)
		if next == '.' || next == '/' || (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || next == '_' {
			l.lexWord()
		} else {
			l.emit(ast.TDot, ".")
			l.pos++
		}
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
		} else if l.mode == LexerExpr {
			// In expression mode, - is always minus operator
			l.emit(ast.TMinus, "-")
			l.pos++
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
		if l.peek(1) == '\n' {
			// Backslash-newline: line continuation, skip both
			l.pos += 2
		} else {
			l.emit(ast.TBackslash, "\\")
			l.pos++
		}
	default:
		l.lexWord()
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

// looksLikeExprBrace peeks ahead from { to check for commas or leading atoms
// at depth 1, which distinguish a tuple {a, b} from a group command { cmd; }.
func (l *Lexer) looksLikeExprBrace() bool {
	depth := 0
	first := true
	for i := l.pos; i < len(l.src); i++ {
		ch := l.src[i]
		switch ch {
		case '{':
			depth++
			first = depth == 1
		case '}':
			depth--
			if depth == 0 {
				return i == l.pos+1 // empty {} is expression
			}
		case ',':
			if depth == 1 {
				return true
			}
		case ':':
			if depth == 1 && first {
				return true // atom after { = tuple
			}
			first = false
		case '\n':
			return false
		case ' ', '\t', '\r':
			// skip whitespace without clearing first
		default:
			first = false
		}
	}
	return false
}

// looksLikeExprBracket peeks ahead from [ to check for commas or | at depth 1,
// which distinguish a list literal [a, b] from the test builtin [ -n x ].
func (l *Lexer) looksLikeExprBracket() bool {
	depth := 0
	for i := l.pos; i < len(l.src); i++ {
		ch := l.src[i]
		switch ch {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i == l.pos+1 // empty [] is list
			}
		case ',':
			if depth == 1 {
				return true
			}
		case '|':
			if depth == 1 {
				return true
			}
		case '\n':
			return false
		}
	}
	return false
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
				ch == '<' || ch == '>' || ch == ',' {
				break
			}
			if ch == '#' && !(l.pos+1 < len(l.src) && l.src[l.pos+1] == '{') {
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
		if ch == '#' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{' {
			braceDepth++
			l.pos += 2
			continue
		}
		if ch == '`' {
			// Backtick substitution inside a word: skip to closing backtick
			l.pos++
			for l.pos < len(l.src) && l.src[l.pos] != '`' {
				if l.src[l.pos] == '\\' {
					l.pos++
				}
				l.pos++
			}
			if l.pos < len(l.src) {
				l.pos++ // skip closing backtick
			}
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
	// Check for float: digits followed by '.' followed by digit
	if l.pos < len(l.src) && l.src[l.pos] == '.' && l.pos+1 < len(l.src) && l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '9' {
		l.pos++ // consume '.'
		for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
			l.pos++
		}
		l.emit(ast.TFloat, l.src[start:l.pos])
		return
	}
	// If followed by a letter or underscore, treat the whole thing as a word
	// (e.g. "3abc"). Don't extend for operators like +, -, /, % which should
	// be tokenized separately.
	if l.pos < len(l.src) {
		ch := l.src[l.pos]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' {
			for l.pos < len(l.src) && isWordChar(l.src[l.pos]) {
				l.pos++
			}
			l.emit(ast.TWord, l.src[start:l.pos])
			return
		}
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
	} else {
		l.tokens = append(l.tokens, ast.Token{Type: ast.TString, Val: content, Pos: l.pos})
	}
	l.lastEmitted = ast.TString
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
		ch == '@' || ch == ':' || ch == '+' || ch == '%' ||
		(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') ||
		ch > 127
}

func IsAlphaNum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}
