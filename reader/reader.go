// Package reader parses ISH source text into reader syntax.
//
// The reader preserves ISH surface structure. Whitespace-shaped expressions,
// parenthesized groups, access syntax, explicit expression escape, and infix
// operators are emitted as %-protocol forms for the expander to interpret in
// context. The reader does not decide call vs command, package access vs method
// access, or operator meaning.
package reader

import (
	"fmt"
	"io"
	"strconv"
	"unicode"
	"unicode/utf8"

	"ish/core"
)

// Error is a reader diagnostic bound to a source span.
type Error struct {
	Span    core.Span
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.Span.File, e.Span.Start.Line, e.Span.Start.Col, e.Message)
}

// ReadAll parses every form in source and returns reader syntax forms in order.
func ReadAll(file, source string) ([]*core.Syntax, error) {
	r := newReader(file, source)
	var out []*core.Syntax
	for {
		r.skipLeadingTrivia()
		if r.eof() {
			return out, nil
		}
		f, err := r.readExpr(0, true)
		if err != nil {
			return out, err
		}
		out = append(out, f)
		if err := r.consumeFormEnd(); err != nil {
			return out, err
		}
	}
}

func ReadProgram(file, source string) (*core.Syntax, error) {
	forms, err := ReadAll(file, source)
	if err != nil {
		return nil, err
	}
	span := core.Span{File: file, Start: core.Pos{Line: 1, Col: 1, Byte: 0}, End: core.Pos{Line: 1, Col: 1, Byte: 0}}
	elems := []*core.Syntax{
		{Node: core.Word("%-package-begin"), Span: span},
		{Node: core.Meta("file"), Span: span},
		{Node: core.String(file), Span: span},
	}
	elems = append(elems, forms...)
	return withReaderShape(core.SyntaxList(span, elems...), "package-begin"), nil
}

// ReadOne parses a single form and returns the consumed byte count alongside
// it. Useful for REPL-style multi-line input where the caller wants to know
// how much of the buffer was consumed.
func ReadOne(file, source string) (*core.Syntax, int, error) {
	r := newReader(file, source)
	r.skipLeadingTrivia()
	if r.eof() {
		return nil, r.pos, io.EOF
	}
	f, err := r.readExpr(0, true)
	return f, r.pos, err
}

type reader struct {
	file         string
	src          []byte
	pos          int
	line         int
	col          int
	lastTokenEnd int
	// blockBody is true only while reading a single do-block body form, so the
	// `end`/`else` keywords that close the block terminate the form instead of
	// being slurped into a whitespace application. It is false in the outer
	// expression, where `else` chains do-blocks and may be an ordinary literal.
	blockBody bool
	// lastDoElse is true when the most recently read primary was a do-block
	// closed by `else` (an incomplete if-arm awaiting its else), so a following
	// `else` continues that chain rather than closing the enclosing body. A
	// do-block closed by `end` (a complete arm) clears it, resolving the
	// dangling-else in favor of the nearest still-open `do`.
	lastDoElse bool
}

func newReader(file, source string) *reader {
	return &reader{file: file, src: []byte(source), line: 1, col: 1, lastTokenEnd: -1}
}

func (r *reader) eof() bool { return r.pos >= len(r.src) }

func (r *reader) peekByte() byte {
	if r.eof() {
		return 0
	}
	return r.src[r.pos]
}

func (r *reader) peekByteAt(off int) byte {
	if r.pos+off >= len(r.src) {
		return 0
	}
	return r.src[r.pos+off]
}

func (r *reader) peekRune() (rune, int) {
	if r.eof() {
		return utf8.RuneError, 0
	}
	ru, size := utf8.DecodeRune(r.src[r.pos:])
	return ru, size
}

func (r *reader) advanceByte() byte {
	c := r.src[r.pos]
	r.pos++
	if c == '\n' {
		r.line++
		r.col = 1
	} else {
		r.col++
	}
	return c
}

func (r *reader) advanceRune() rune {
	ru, size := utf8.DecodeRune(r.src[r.pos:])
	r.pos += size
	if ru == '\n' {
		r.line++
		r.col = 1
	} else {
		r.col++
	}
	return ru
}

func (r *reader) mark() core.Pos { return core.Pos{Line: r.line, Col: r.col, Byte: r.pos} }

func (r *reader) span(start core.Pos) core.Span {
	return core.Span{File: r.file, Start: start, End: r.mark()}
}

func (r *reader) errorf(start core.Pos, format string, args ...any) error {
	return &Error{Span: r.span(start), Message: fmt.Sprintf(format, args...)}
}

func (r *reader) skipLeadingTrivia() {
	for !r.eof() {
		c := r.peekByte()
		if c < 0x80 {
			switch c {
			case ' ', '\t', '\n', '\r', ';':
				r.advanceByte()
				continue
			case '#':
				r.skipLineComment()
				continue
			}
			return
		}
		ru, _ := r.peekRune()
		if unicode.IsSpace(ru) {
			r.advanceRune()
			continue
		}
		return
	}
}

func (r *reader) skipLineComment() {
	for !r.eof() && r.peekByte() != '\n' {
		r.advanceByte()
	}
}

func (r *reader) skipExprSpace(terminators bool) bool {
	skipped := false
	for !r.eof() {
		c := r.peekByte()
		if c < 0x80 {
			switch c {
			case ' ', '\t', '\r':
				r.advanceByte()
				skipped = true
				continue
			case '\n', ';':
				if terminators {
					return skipped
				}
				r.advanceByte()
				skipped = true
				continue
			case '#':
				if terminators {
					return skipped
				}
				r.skipLineComment()
				skipped = true
				continue
			}
			return skipped
		}
		ru, _ := r.peekRune()
		if unicode.IsSpace(ru) {
			if terminators && ru == '\n' {
				return skipped
			}
			r.advanceRune()
			skipped = true
			continue
		}
		return skipped
	}
	return skipped
}

func (r *reader) consumeFormEnd() error {
	r.skipExprSpace(true)
	if r.eof() {
		return nil
	}
	switch r.peekByte() {
	case '\n', ';':
		r.advanceByte()
		return nil
	case '#':
		r.skipLineComment()
		return nil
	case ')', ']', '}':
		start := r.mark()
		return r.errorf(start, "unexpected closing delimiter %q", r.peekByte())
	default:
		return nil
	}
}

func (r *reader) atStop(close byte, terminators bool) bool {
	if r.eof() {
		return true
	}
	if close != 0 && r.peekByte() == close {
		return true
	}
	if terminators {
		switch r.peekByte() {
		case '\n', ';', '#':
			return true
		}
	}
	return false
}

func (r *reader) readExpr(close byte, terminators bool) (*core.Syntax, error) {
	r.skipExprSpace(terminators)
	start := r.mark()
	if r.atStop(close, terminators) {
		return nil, r.errorf(start, "expected expression")
	}
	first, err := r.readPrimary(close, terminators)
	if err != nil {
		return nil, err
	}
	parts := []*core.Syntax{first}
	for {
		r.skipExprSpace(terminators)
		if r.atStop(close, terminators) {
			break
		}
		// Within a do-block body, `end` closes the block. `else` closes it too,
		// EXCEPT when it follows a do-block part: then the `else` belongs to that
		// inner block's do/else chain (an `if … do … else … end` written inline in
		// a body), so read it as a word and keep going. The inner `do … end`
		// consumes its own matching `end`, leaving the enclosing block's `end`
		// intact — i.e. `else` is resolved at do/end depth, not greedily.
		if r.blockBody && r.atKeyword("end") {
			break
		}
		if r.blockBody && r.atKeyword("else") && !r.lastDoElse {
			break
		}
		arg, err := r.readPrimary(close, terminators)
		if err != nil {
			return nil, err
		}
		parts = append(parts, arg)
	}
	if len(parts) == 1 {
		return first, nil
	}
	return r.protocol(start, "%-expr", parts...), nil
}

func (r *reader) readPrimary(close byte, terminators bool) (*core.Syntax, error) {
	r.skipExprSpace(terminators)
	if r.eof() {
		return nil, &Error{Span: r.span(r.mark()), Message: "unexpected end of input"}
	}
	// Only a do-block sets lastDoElse; every other primary clears it so a stale
	// value from an earlier part can't misroute a following `else`.
	r.lastDoElse = false
	start := r.mark()
	c := r.peekByte()
	switch c {
	case '(':
		return r.readGroup(start)
	case '[':
		return r.readDelim(start, '[', ']', func(elems []*core.Syntax) any { return core.SyntaxVector(elems) })
	case '{':
		return r.readDelim(start, '{', '}', func(elems []*core.Syntax) any { return core.SyntaxTuple(elems) })
	case ')', ']', '}':
		return nil, r.errorf(start, "unexpected closing delimiter %q", c)
	case '"':
		return r.readString(start)
	case ':':
		if r.canStartAtom(start) && (isWordContByte(r.peekByteAt(1)) || isOperatorContByte(r.peekByteAt(1))) {
			return r.readAtom(start)
		}
		return r.readOperator(start)
	case '\'':
		r.advanceByte()
		return r.readPrefixed(start, "quote")
	case '`':
		r.advanceByte()
		return r.readPrefixed(start, "quasiquote")
	case ',':
		r.advanceByte()
		if r.peekByte() == '@' {
			r.advanceByte()
			return r.readPrefixed(start, "unquote-splicing")
		}
		return r.readPrefixed(start, "unquote")
	case '%':
		return r.readPercent(start)
	case '&':
		if r.peekByteAt(1) == '&' {
			return r.readOperator(start)
		}
		r.advanceByte()
		inner, err := r.readPrimary(close, terminators)
		if err != nil {
			return nil, err
		}
		return r.protocol(start, "%-expression", inner), nil
	case '^':
		r.advanceByte()
		return r.readPrefixed(start, "pin")
	}
	if c == 'b' && r.peekByteAt(1) == '"' {
		r.advanceByte()
		return r.readBytes(start)
	}
	if isDigit(c) || ((c == '-' || c == '+') && isDigit(r.peekByteAt(1))) {
		return r.readNumber(start)
	}
	if c == '.' {
		if r.peekByteAt(1) == '.' && r.peekByteAt(2) == '.' {
			r.advanceByte()
			r.advanceByte()
			r.advanceByte()
			return r.token(start, core.Word("..."), "operator"), nil
		}
		r.advanceByte()
		return r.token(start, core.Word("."), "operator"), nil
	}
	if c == '!' && r.peekByteAt(1) == '=' {
		return r.readOperator(start)
	}
	if isOperatorContByte(c) {
		if c == '*' && r.peekByteAt(1) == '.' {
			return r.readRawWord(start)
		}
		return r.readOperator(start)
	}
	w, err := r.readWord(start)
	if err != nil {
		return nil, err
	}
	if word, ok := w.Node.(core.Word); ok && word == "do" {
		return r.readDoBlock(start, w)
	}
	return w, nil
}

func (r *reader) readDoBlock(start core.Pos, head *core.Syntax) (*core.Syntax, error) {
	var forms []*core.Syntax
	for {
		r.skipLeadingTrivia()
		if r.eof() {
			return nil, r.errorf(start, "unterminated do block")
		}
		if r.atKeyword("else") {
			r.lastDoElse = true
			return r.protocol(start, "%-expr", head, r.protocol(start, "%-body", forms...)), nil
		}
		if r.consumeKeyword("end") {
			r.lastDoElse = false
			return r.protocol(start, "%-expr", head, r.protocol(start, "%-body", forms...)), nil
		}
		prev := r.blockBody
		r.blockBody = true
		form, err := r.readExpr(0, true)
		r.blockBody = prev
		if err != nil {
			return nil, err
		}
		forms = append(forms, form)
		if err := r.consumeFormEnd(); err != nil {
			return nil, err
		}
	}
}

func (r *reader) consumeKeyword(word string) bool {
	if !r.atKeyword(word) {
		return false
	}
	for range word {
		r.advanceByte()
	}
	return true
}

func (r *reader) canStartAtom(start core.Pos) bool {
	if start.Byte == 0 {
		return true
	}
	prev := r.src[start.Byte-1]
	if prev <= ' ' {
		return true
	}
	switch prev {
	case '(', '[', '{', ';', '\n', '\r', '\t', '\'', '`', ',':
		return true
	}
	return false
}

func (r *reader) atKeyword(word string) bool {
	if r.pos+len(word) > len(r.src) || string(r.src[r.pos:r.pos+len(word)]) != word {
		return false
	}
	if r.pos+len(word) < len(r.src) {
		c := r.src[r.pos+len(word)]
		if c >= 0x80 || isWordContByte(c) || isOperatorContByte(c) || c == '.' {
			return false
		}
	}
	return true
}

func (r *reader) readGroup(start core.Pos) (*core.Syntax, error) {
	prev := r.blockBody
	r.blockBody = false
	defer func() { r.blockBody = prev; r.lastDoElse = false }()
	r.advanceByte()
	r.skipExprSpace(false)
	if r.eof() {
		return nil, r.errorf(start, "unterminated group")
	}
	if r.peekByte() == ')' {
		r.advanceByte()
		return withReaderShape(r.protocol(start, "%-group"), "()"), nil
	}
	inner, err := r.readExpr(')', false)
	if err != nil {
		return nil, err
	}
	r.skipExprSpace(false)
	if r.eof() || r.peekByte() != ')' {
		return nil, r.errorf(start, "unterminated group")
	}
	r.advanceByte()
	return withReaderShape(r.protocol(start, "%-group", inner), "()"), nil
}

func (r *reader) readPercent(start core.Pos) (*core.Syntax, error) {
	r.advanceByte()
	switch r.peekByte() {
	case ':':
		return r.readMeta(start)
	case '{':
		return r.readDictBody(start)
	case '\'':
		r.advanceByte()
		return r.readPrefixed(start, "syntax")
	case '`':
		r.advanceByte()
		return r.readPrefixed(start, "quasisyntax")
	case ',':
		r.advanceByte()
		if r.peekByte() == '@' {
			r.advanceByte()
			return r.readPrefixed(start, "unsyntax-splicing")
		}
		return r.readPrefixed(start, "unsyntax")
	case '~':
		// %~name reads as the single combinator word ~name, used as the head
		// of syntax-parse pattern combinators like (%~or a b). The reader keeps
		// it whole so the expander can recognize it without tokenization games.
		r.advanceByte()
		buf := r.scanWord()
		if len(buf) == 0 {
			return nil, r.errorf(start, "empty %%~ combinator name")
		}
		return r.token(start, core.Word("~"+string(buf)), "word"), nil
	}
	r.pos = start.Byte
	r.line = start.Line
	r.col = start.Col
	return r.readOperator(start)
}

func (r *reader) readMeta(start core.Pos) (*core.Syntax, error) {
	r.advanceByte()
	buf := r.scanWord()
	if len(buf) == 0 {
		return nil, r.errorf(start, "empty metadata tag after %%:")
	}
	return &core.Syntax{Node: core.Meta(buf), Span: r.span(start)}, nil
}

func (r *reader) readPrefixed(start core.Pos, head string) (*core.Syntax, error) {
	form, err := r.readPrimary(0, true)
	if err != nil {
		return nil, err
	}
	return r.form(start, head, form), nil
}

func (r *reader) readDelim(start core.Pos, open, close byte, wrap func([]*core.Syntax) any) (*core.Syntax, error) {
	prev := r.blockBody
	r.blockBody = false
	defer func() { r.blockBody = prev; r.lastDoElse = false }()
	r.advanceByte()
	var elems []*core.Syntax
	for {
		r.skipExprSpace(false)
		if r.eof() {
			return nil, r.errorf(start, "unterminated %c", open)
		}
		if r.peekByte() == close {
			r.advanceByte()
			break
		}
		f, err := r.readPrimary(close, false)
		if err != nil {
			return nil, err
		}
		elems = append(elems, f)
	}
	return withReaderShape(&core.Syntax{Node: wrap(elems), Span: r.span(start)}, string([]byte{open, close})), nil
}

func (r *reader) readDictBody(start core.Pos) (*core.Syntax, error) {
	prev := r.blockBody
	r.blockBody = false
	defer func() { r.blockBody = prev }()
	r.advanceByte()
	var entries core.SyntaxDict
	for {
		r.skipExprSpace(false)
		if r.eof() {
			return nil, r.errorf(start, "unterminated %%{")
		}
		if r.peekByte() == '}' {
			r.advanceByte()
			break
		}
		k, err := r.readPrimary('}', false)
		if err != nil {
			return nil, err
		}
		r.skipExprSpace(false)
		if r.eof() || r.peekByte() == '}' {
			return nil, r.errorf(start, "dict literal: key without value")
		}
		v, err := r.readPrimary('}', false)
		if err != nil {
			return nil, err
		}
		entries = append(entries, core.SyntaxDictEntry{Key: k, Value: v})
	}
	return withReaderShape(&core.Syntax{Node: entries, Span: r.span(start)}, "%{}"), nil
}

func withReaderShape(stx *core.Syntax, shape string) *core.Syntax {
	stx.Properties = stx.Properties.With(core.PropReaderShape, core.String(shape))
	return stx
}

func (r *reader) readString(start core.Pos) (*core.Syntax, error) {
	r.advanceByte()
	buf, err := r.readStringLikeBody(start, false)
	if err != nil {
		return nil, err
	}
	return r.token(start, core.String(buf), "string"), nil
}

func (r *reader) readBytes(start core.Pos) (*core.Syntax, error) {
	r.advanceByte()
	buf, err := r.readStringLikeBody(start, true)
	if err != nil {
		return nil, err
	}
	return r.token(start, core.Bytes(buf), "bytes"), nil
}

func (r *reader) readStringLikeBody(start core.Pos, allowHex bool) ([]byte, error) {
	buf := []byte{}
	for {
		if r.eof() {
			return nil, r.errorf(start, "unterminated string literal")
		}
		ru, size := r.peekRune()
		if ru == utf8.RuneError && size == 1 {
			return nil, r.errorf(start, "malformed UTF-8")
		}
		if ru == '"' && size == 1 {
			r.advanceByte()
			return buf, nil
		}
		if ru == '\\' && size == 1 {
			r.advanceByte()
			if r.eof() {
				return nil, r.errorf(start, "unterminated escape")
			}
			esc := r.advanceByte()
			switch esc {
			case 'n':
				buf = append(buf, '\n')
			case 't':
				buf = append(buf, '\t')
			case 'r':
				buf = append(buf, '\r')
			case '\\':
				buf = append(buf, '\\')
			case '"':
				buf = append(buf, '"')
			case '0':
				buf = append(buf, 0)
			case 'x':
				if !allowHex {
					return nil, r.errorf(start, "\\x escape only valid in bytes literal")
				}
				if r.pos+1 >= len(r.src) {
					return nil, r.errorf(start, "truncated \\x escape")
				}
				hi, lo := r.advanceByte(), r.advanceByte()
				v, err := strconv.ParseUint(string([]byte{hi, lo}), 16, 8)
				if err != nil {
					return nil, r.errorf(start, "bad \\x escape: %v", err)
				}
				buf = append(buf, byte(v))
			default:
				return nil, r.errorf(start, "unknown escape \\%c", esc)
			}
			continue
		}
		buf = append(buf, r.src[r.pos:r.pos+size]...)
		r.advanceRune()
	}
}

func (r *reader) readAtom(start core.Pos) (*core.Syntax, error) {
	r.advanceByte()
	var buf []byte
	if isOperatorContByte(r.peekByte()) {
		// An operator-character atom: `:+`, `:*`, `:==`, `:<`, ... — symmetric
		// to a word atom, the name is a run of operator characters.
		for !r.eof() && isOperatorContByte(r.peekByte()) {
			buf = append(buf, r.advanceByte())
		}
	} else {
		buf = r.scanWord()
	}
	if len(buf) == 0 {
		return nil, r.errorf(start, "empty atom name after ':'")
	}
	// `:nil` is the canonical surface spelling of the empty/nil value, the same
	// value the empty list `'()` denotes.
	if string(buf) == "nil" {
		return r.token(start, core.Nil{}, "nil"), nil
	}
	return r.token(start, core.Atom(buf), "atom"), nil
}

func (r *reader) readNumber(start core.Pos) (*core.Syntax, error) {
	var buf []byte
	if r.peekByte() == '+' || r.peekByte() == '-' {
		buf = append(buf, r.advanceByte())
	}
	for !r.eof() && isDigit(r.peekByte()) {
		buf = append(buf, r.advanceByte())
	}
	isFloat := false
	if r.peekByte() == '.' && isDigit(r.peekByteAt(1)) {
		isFloat = true
		buf = append(buf, r.advanceByte())
		for !r.eof() && isDigit(r.peekByte()) {
			buf = append(buf, r.advanceByte())
		}
	}
	if c := r.peekByte(); c == 'e' || c == 'E' {
		isFloat = true
		buf = append(buf, r.advanceByte())
		if r.peekByte() == '+' || r.peekByte() == '-' {
			buf = append(buf, r.advanceByte())
		}
		if !isDigit(r.peekByte()) {
			return nil, r.errorf(start, "bad float exponent")
		}
		for !r.eof() && isDigit(r.peekByte()) {
			buf = append(buf, r.advanceByte())
		}
	}
	if isFloat {
		f, err := strconv.ParseFloat(string(buf), 64)
		if err != nil {
			return nil, r.errorf(start, "bad float: %v", err)
		}
		return r.token(start, core.Float(f), "number"), nil
	}
	n, err := strconv.ParseInt(string(buf), 10, 64)
	if err != nil {
		return nil, r.errorf(start, "bad int: %v", err)
	}
	return r.token(start, core.Int(n), "number"), nil
}

func (r *reader) readWord(start core.Pos) (*core.Syntax, error) {
	return r.readWordContinuing(start, nil)
}

func (r *reader) readOperator(start core.Pos) (*core.Syntax, error) {
	buf := []byte{r.advanceByte()}
	if buf[0] == '!' && r.peekByte() == '=' {
		buf = append(buf, r.advanceByte())
		return r.token(start, core.Word(buf), "operator"), nil
	}
	for !r.eof() && isOperatorContByte(r.peekByte()) {
		buf = append(buf, r.advanceByte())
	}
	return r.token(start, core.Word(buf), "operator"), nil
}

func (r *reader) readRawWord(start core.Pos) (*core.Syntax, error) {
	var buf []byte
	for !r.eof() {
		c := r.peekByte()
		if c < 0x80 {
			switch c {
			case ' ', '\t', '\n', '\r', '\v', '\f', ';', '(', ')', '[', ']', '{', '}', '"', '\'', '`', '#', ',':
				return r.token(start, core.Word(buf), "word"), nil
			}
			buf = append(buf, r.advanceByte())
			continue
		}
		ru, size := r.peekRune()
		if ru == utf8.RuneError && size == 1 {
			return r.token(start, core.Word(buf), "word"), nil
		}
		if unicode.IsSpace(ru) || unicode.IsControl(ru) {
			return r.token(start, core.Word(buf), "word"), nil
		}
		buf = append(buf, r.src[r.pos:r.pos+size]...)
		r.advanceRune()
	}
	return r.token(start, core.Word(buf), "word"), nil
}

func (r *reader) readWordContinuing(start core.Pos, initial []byte) (*core.Syntax, error) {
	buf := append([]byte(nil), initial...)
	buf = append(buf, r.scanWord()...)
	if len(buf) == 0 {
		return nil, r.errorf(start, "unexpected character")
	}
	baseSpan := r.span(start)
	return r.tokenFromSpan(baseSpan, core.Word(buf), "word"), nil
}

func (r *reader) scanWord() []byte {
	var buf []byte
	for !r.eof() {
		c := r.peekByte()
		if c < 0x80 {
			if c == '!' && r.peekByteAt(1) == '=' {
				return buf
			}
			if c == '>' && len(buf) > 0 && buf[len(buf)-1] == '-' {
				buf = append(buf, r.advanceByte())
				continue
			}
			if c == '=' && r.peekByteAt(1) == '?' && len(buf) > 0 {
				buf = append(buf, r.advanceByte())
				buf = append(buf, r.advanceByte())
				continue
			}
			if !isWordContByte(c) {
				return buf
			}
			buf = append(buf, r.advanceByte())
			continue
		}
		ru, size := r.peekRune()
		if ru == utf8.RuneError && size == 1 {
			return buf
		}
		if !isWordContRune(ru) {
			return buf
		}
		buf = append(buf, r.src[r.pos:r.pos+size]...)
		r.advanceRune()
	}
	return buf
}

func (r *reader) protocol(start core.Pos, head string, elems ...*core.Syntax) *core.Syntax {
	return r.protocolFromSpan(r.span(start), head, elems...)
}

func (r *reader) token(start core.Pos, node any, kind string) *core.Syntax {
	return r.tokenFromSpan(r.span(start), node, kind)
}

func (r *reader) tokenFromSpan(span core.Span, node any, kind string) *core.Syntax {
	raw := ""
	if span.Start.Byte >= 0 && span.End.Byte >= span.Start.Byte && span.End.Byte <= len(r.src) {
		raw = string(r.src[span.Start.Byte:span.End.Byte])
	}
	leadingSpace := core.Atom("false")
	adjacent := core.Atom("false")
	if r.lastTokenEnd >= 0 {
		if span.Start.Byte > r.lastTokenEnd {
			leadingSpace = core.Atom("true")
		} else if span.Start.Byte == r.lastTokenEnd {
			adjacent = core.Atom("true")
		}
	}
	r.lastTokenEnd = span.End.Byte
	return &core.Syntax{Node: node, Span: span, Properties: core.Properties{}.
		With(core.PropTokenKind, core.String(kind)).
		With(core.PropTokenRaw, core.String(raw)).
		With(core.PropTokenLeadingSpace, leadingSpace).
		With(core.PropTokenAdjacentPrev, adjacent)}
}

func (r *reader) protocolFromSpan(span core.Span, head string, elems ...*core.Syntax) *core.Syntax {
	headStx := &core.Syntax{Node: core.Word(head), Span: span}
	all := append([]*core.Syntax{headStx}, elems...)
	return core.SyntaxList(span, all...)
}

func (r *reader) form(start core.Pos, head string, elems ...*core.Syntax) *core.Syntax {
	sp := r.span(start)
	headStx := &core.Syntax{Node: core.Word(head), Span: sp}
	all := append([]*core.Syntax{headStx}, elems...)
	return core.SyntaxList(sp, all...)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isOperatorContByte(c byte) bool {
	if c >= 0x80 || c <= ' ' {
		return false
	}
	switch c {
	case '(', ')', '[', ']', '{', '}', '"', '\'', '`', '#', ',', '.', ';':
		return false
	case '_', '-', '?', '!', '/':
		// These are accepted inside ordinary identifiers/package paths today.
		// `!=` is handled explicitly in readPrimary/readOperator.
		return false
	}
	return isASCIIPunctuation(c)
}

func isASCIIPunctuation(c byte) bool {
	return (c >= '!' && c <= '/') || (c >= ':' && c <= '@') || (c >= '[' && c <= '`') || (c >= '{' && c <= '~')
}

func isWordContByte(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\v', '\f', ';',
		'(', ')', '[', ']', '{', '}',
		'"', '\'', '`', '#', ',', '.',
		'+', '*', '=', '<', '>', '&', '|', '^', ':', '%', '$':
		return false
	}
	return c > ' ' && c < 0x7F
}

func isWordContRune(ru rune) bool {
	if ru < 0x80 {
		return isWordContByte(byte(ru))
	}
	return !unicode.IsSpace(ru) && !unicode.IsControl(ru)
}
