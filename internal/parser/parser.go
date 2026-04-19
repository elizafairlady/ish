package parser

import (
	"fmt"
	"strings"

	"ish/internal/ast"
	"ish/internal/lexer"
)

func IsAssignment(tok ast.Token) bool {
	if tok.Type != ast.TWord {
		return false
	}
	idx := strings.IndexByte(tok.Val, '=')
	if idx <= 0 {
		return false
	}
	name := tok.Val[:idx]
	for i, ch := range name {
		if i == 0 && ch >= '0' && ch <= '9' {
			return false
		}
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return false
		}
	}
	return true
}

func (p *Parser) isBlockEnd(word string) bool {
	for _, t := range p.terminators {
		if word == t {
			return true
		}
	}
	return false
}

func (p *Parser) pushTerminators(terms ...string) []string {
	old := p.terminators
	p.terminators = append(append([]string{}, old...), terms...)
	return old
}

func (p *Parser) restoreTerminators(old []string) {
	p.terminators = old
}

func isExprOperator(tt ast.TokenType) bool {
	switch tt {
	case ast.TPlus, ast.TMinus, ast.TMul, ast.TDiv, ast.TPercent, ast.TEq, ast.TNe, ast.TLe, ast.TGe, ast.TDot:
		return true
	}
	return false
}

const maxParseDepth = 1000

type ParseMode int

const (
	ModeCommand ParseMode = iota // POSIX: -n is flag, > is redirect, { is group, [ is test
	ModeExpr                     // ish: -n is negation, > is comparison, { is tuple, [ is list
)

type Parser struct {
	lex             *lexer.Lexer
	tokens          []ast.Token
	pos             int
	base            int
	resumePositions []int
	terminators     []string
	mode            ParseMode
	depth           int
}

func (p *Parser) syncLexerMode() {
	lm := lexer.LexerShell
	if p.mode == ModeExpr {
		lm = lexer.LexerExpr
	}
	if p.lex.Mode() == lm {
		return
	}
	p.lex.SetMode(lm)
	idx := p.pos - p.base
	if idx < len(p.tokens) {
		p.lex.SetPos(p.resumePositions[idx])
		if p.pos > 0 && p.pos-1 >= p.base {
			p.lex.SetLastEmitted(p.tokens[p.pos-1-p.base].Type)
		} else {
			p.lex.SetLastEmitted(ast.TEOF)
		}
		p.tokens = p.tokens[:idx]
		p.resumePositions = p.resumePositions[:idx]
	}
}

func (p *Parser) withMode(m ParseMode) ParseMode {
	old := p.mode
	p.mode = m
	p.syncLexerMode()
	return old
}

func (p *Parser) restoreMode(old ParseMode) {
	p.mode = old
	p.syncLexerMode()
}

func Parse(l *lexer.Lexer) (*ast.Node, error) {
	p := &Parser{lex: l}
	return p.parseProgram()
}

func (p *Parser) compact() {
	if p.pos-p.base < 256 {
		return
	}
	keep := p.pos - 1
	drop := keep - p.base
	p.tokens = append([]ast.Token{}, p.tokens[drop:]...)
	p.resumePositions = append([]int{}, p.resumePositions[drop:]...)
	p.base = keep
}

func (p *Parser) fillTo(n int) {
	p.compact()
	for n-p.base >= len(p.tokens) {
		resumePos := p.lex.SourcePos()
		tok := p.lex.NextToken()
		p.tokens = append(p.tokens, tok)
		p.resumePositions = append(p.resumePositions, resumePos)
		if tok.Type == ast.TEOF {
			break
		}
	}
}

func (p *Parser) cur() ast.Token {
	p.fillTo(p.pos)
	idx := p.pos - p.base
	if idx >= len(p.tokens) {
		return ast.Token{Type: ast.TEOF}
	}
	return p.tokens[idx]
}

func (p *Parser) peek() ast.Token {
	p.fillTo(p.pos + 1)
	idx := p.pos + 1 - p.base
	if idx >= len(p.tokens) {
		return ast.Token{Type: ast.TEOF}
	}
	return p.tokens[idx]
}


func (p *Parser) advance() ast.Token {
	t := p.cur()
	p.pos++
	return t
}

func (p *Parser) expect(tt ast.TokenType) (ast.Token, error) {
	t := p.cur()
	if t.Type != tt {
		if t.Type == ast.TEOF {
			return t, fmt.Errorf("unexpected end of input (expected closing delimiter)")
		}
		return t, fmt.Errorf("expected %d, got %d (%q) at pos %d", tt, t.Type, t.Val, t.Pos)
	}
	p.pos++
	return t, nil
}

func (p *Parser) match(tt ast.TokenType) bool {
	if p.cur().Type == tt {
		p.pos++
		return true
	}
	return false
}

func (p *Parser) isWord(w string) bool {
	return p.cur().Type == ast.TWord && p.cur().Val == w
}

func (p *Parser) matchWord(w string) bool {
	if p.isWord(w) {
		p.pos++
		return true
	}
	return false
}

func (p *Parser) expectWord(w string) error {
	if p.cur().Type != ast.TWord || p.cur().Val != w {
		return fmt.Errorf("expected %q at pos %d, got %q", w, p.cur().Pos, p.cur().Val)
	}
	p.pos++
	return nil
}

func (p *Parser) skipNewlines() {
	for p.cur().Type == ast.TNewline {
		p.pos++
	}
}

func (p *Parser) skipSeparators() {
	for p.cur().Type == ast.TSemicolon || p.cur().Type == ast.TNewline {
		p.pos++
	}
}

func (p *Parser) isExprContext() bool {
	return p.mode == ModeExpr || p.cur().ExprHint
}

func markTail(stmts []*ast.Node) {
	if len(stmts) > 0 {
		stmts[len(stmts)-1].Tail = true
	}
}

// ishBlockWithSection parses a do...end block where the body has two phases
// separated by a keyword: main body parsed until keyword, then optional
// continuation if keyword is present. Used by if/else, try/rescue, receive/after.
func (p *Parser) ishBlockWithSection(keyword string, parseMain func() error, parseCont func() error) error {
	return p.ishBlock(func() error {
		old := p.pushTerminators(keyword)
		err := parseMain()
		p.restoreTerminators(old)
		if err != nil {
			return err
		}
		if p.matchWord(keyword) {
			p.skipNewlines()
			return parseCont()
		}
		return nil
	})
}

func (p *Parser) withExprBlock(terminators []string, body func() error) error {
	defer p.restoreMode(p.withMode(ModeExpr))
	defer p.restoreTerminators(p.pushTerminators(terminators...))
	return body()
}

func (p *Parser) ishBlock(body func() error) error {
	if err := p.expectWord("do"); err != nil {
		return err
	}
	p.skipNewlines()
	if err := p.withExprBlock([]string{"end"}, body); err != nil {
		return err
	}
	if p.matchWord("end") {
		return nil
	}
	return fmt.Errorf("expected 'end' at pos %d", p.cur().Pos)
}

func (p *Parser) parseBlock() ([]*ast.Node, error) {
	var stmts []*ast.Node
	for p.cur().Type != ast.TEOF {
		if p.cur().Type == ast.TWord && p.isBlockEnd(p.cur().Val) {
			break
		}
		stmt, err := p.parseList()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
		p.skipSeparators()
	}
	return stmts, nil
}

func (p *Parser) parseProgram() (*ast.Node, error) {
	p.skipNewlines()
	stmts, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return ast.BlockNode(stmts), nil
}

func (p *Parser) parseList() (*ast.Node, error) {
	left, err := p.parsePipeline()
	if err != nil || left == nil {
		return left, err
	}

	for {
		if p.cur().Type == ast.TAnd {
			p.advance()
			p.skipNewlines()
			right, err := p.parsePipeline()
			if err != nil {
				return nil, err
			}
			left = &ast.Node{Kind: ast.NAndList, Children: []*ast.Node{left, right}}
		} else if p.cur().Type == ast.TOr {
			p.advance()
			p.skipNewlines()
			right, err := p.parsePipeline()
			if err != nil {
				return nil, err
			}
			left = &ast.Node{Kind: ast.NOrList, Children: []*ast.Node{left, right}}
		} else if p.cur().Type == ast.TAmpersand {
			p.advance()
			left = &ast.Node{Kind: ast.NBg, Children: []*ast.Node{left}}
		} else {
			break
		}
	}
	return left, nil
}

func (p *Parser) parsePipeline() (*ast.Node, error) {
	left, err := p.parseStmtWithOps()
	if err != nil || left == nil {
		return left, err
	}

	for {
		if p.cur().Type == ast.TPipe {
			tok := p.advance()
			p.skipNewlines()
			right, err := p.parseStmtWithOps()
			if err != nil {
				return nil, err
			}
			left = &ast.Node{Kind: ast.NPipe, Tok: tok, Children: []*ast.Node{left, right}}
		} else if p.cur().Type == ast.TPipeArrow {
			p.advance()
			p.skipNewlines()
			right, err := p.parseStmtWithOps()
			if err != nil {
				return nil, err
			}
			left = &ast.Node{Kind: ast.NPipeFn, Children: []*ast.Node{left, right}}
		} else {
			break
		}
	}
	return left, nil
}

func (p *Parser) parseStmtWithOps() (*ast.Node, error) {
	p.skipNewlines()

	left, err := p.parseStmt()
	if err != nil || left == nil {
		return left, err
	}

	for p.precedence(p.cur().Type) > 0 {
		op := p.advance()
		right, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		left = &ast.Node{Kind: ast.NBinOp, Tok: op, Children: []*ast.Node{left, right}}
	}

	return left, nil
}

// parsePrimary dispatches mode-dependent bracketed constructs:
// ( ) → subshell or expression, { } → group or tuple, [ ] → test or list.
// For { and [, the streaming lexer detects expression syntax (commas, atoms,
// pipes) during lexing and sets ExprHint on the token.
func (p *Parser) parsePrimary() (*ast.Node, error) {
	isExpr := p.isExprContext()
	switch p.cur().Type {
	case ast.TLParen:
		if isExpr {
			return p.parseExpression()
		}
		return p.parseSubshell()
	case ast.TLBrace:
		if isExpr {
			return p.parseExpression()
		}
		return p.parseGroup()
	case ast.TLBracket:
		if isExpr {
			return p.parseExpression()
		}
		return p.parseCmdLine()
	}
	return nil, nil
}

func (p *Parser) parseStmt() (*ast.Node, error) {
	cur := p.cur()

	switch cur.Type {
	case ast.TEOF:
		return nil, nil
	case ast.TLParen, ast.TLBrace, ast.TLBracket:
		return p.parsePrimary()
	case ast.TBang:
		return p.parseExpression()
	case ast.TAtom:
		return p.parseExpression()
	case ast.TPercent:
		if p.peek().Type == ast.TLBrace {
			return p.parseExpression()
		}
	case ast.TBackslash:
		return p.parseLambda()
	case ast.TInt, ast.TFloat:
		return p.parseExpression()
	case ast.TString:
		return p.parseExpression()
	case ast.TWord:
		return p.dispatchKeyword()
	case ast.TMinus:
		return p.parseExpression()
	}

	return p.parseCmdLine()
}

func (p *Parser) dispatchKeyword() (*ast.Node, error) {
	cur := p.cur()

	if IsAssignment(cur) {
		return p.parsePosixAssign()
	}
	// Allow keywords as variable names when followed by =
	if p.peek().Type == ast.TEquals {
		return p.parseIshBind()
	}
	switch cur.Val {
	case "if":
		return p.parseIf()
	case "for":
		return p.parseFor()
	case "while":
		return p.parseWhile()
	case "until":
		return p.parseUntil()
	case "case":
		return p.parseCase()
	case "fn":
		return p.parseIshFn()
	case "match":
		return p.parseIshMatchExpr()
	case "spawn":
		return p.parseIshSpawn()
	case "spawn_link":
		return p.parseIshSpawnLink()
	case "send":
		return p.parseIshSend()
	case "monitor":
		return p.parseIshMonitor()
	case "await":
		return p.parseIshAwait()
	case "supervise":
		return p.parseIshSupervise()
	case "receive":
		return p.parseIshReceive()
	case "try":
		return p.parseIshTry()
	}
	if isExprOperator(p.peek().Type) && p.mode == ModeExpr {
		// Don't treat % as modulo when followed by { (that's a map literal: %{...})
		if p.peek().Type == ast.TPercent {
			p.fillTo(p.pos + 2)
			idx := p.pos + 2 - p.base
			if idx < len(p.tokens) && p.tokens[idx].Type == ast.TLBrace {
				return p.parseCmdLine()
			}
		}
		return p.parseExpression()
	}
	if (p.peek().Type == ast.TRedirIn || p.peek().Type == ast.TRedirOut) && p.mode == ModeExpr {
		return p.parseExpression()
	}
	p.fillTo(p.pos + 2)
	if p.peek().Type == ast.TLParen && p.pos+2-p.base < len(p.tokens) && p.tokens[p.pos+2-p.base].Type == ast.TRParen {
		return p.parsePosixFnDef()
	}
	if p.mode == ModeExpr {
		next := p.peek().Type
		if next == ast.TNewline || next == ast.TEOF || next == ast.TSemicolon ||
			next == ast.TPipe || next == ast.TPipeArrow || next == ast.TAnd || next == ast.TOr ||
			next == ast.TRParen || next == ast.TRBrace || next == ast.TComma || next == ast.TArrow {
			p.advance()
			return ast.WordNode(cur), nil
		}
	}
	return p.parseCmdLine()
}

func (p *Parser) isStmtEnd(cur ast.Token) bool {
	switch cur.Type {
	case ast.TEOF, ast.TNewline, ast.TSemicolon, ast.TPipe, ast.TPipeArrow, ast.TAnd, ast.TOr,
		ast.TRParen, ast.TRBrace, ast.TArrow, ast.TPlus, ast.TMul, ast.TDiv, ast.TEq, ast.TNe, ast.TLe, ast.TGe:
		return true
	case ast.TAmpersand:
		return p.peek().Type != ast.TRedirOut && p.peek().Type != ast.TRedirAppend
	}
	return false
}

func (p *Parser) collectAssigns() []*ast.Node {
	var assigns []*ast.Node
	for p.cur().Type == ast.TWord && IsAssignment(p.cur()) {
		tok := p.advance()
		assigns = append(assigns, &ast.Node{Kind: ast.NAssign, Tok: tok, Pos: tok.Pos})
	}
	return assigns
}

func (p *Parser) tryParseRedir() ([]ast.Redir, bool, error) {
	cur := p.cur()

	if cur.Type == ast.TAmpersand && (p.peek().Type == ast.TRedirOut || p.peek().Type == ast.TRedirAppend) {
		p.advance()
		r, err := p.parseRedir()
		if err != nil {
			return nil, true, err
		}
		r.Fd = 1
		return []ast.Redir{r, {Op: r.Op, Fd: 2, Target: r.Target, Quoted: r.Quoted}}, true, nil
	}

	if cur.Type == ast.TRedirOut || cur.Type == ast.TRedirAppend || cur.Type == ast.TRedirIn || cur.Type == ast.THeredoc || cur.Type == ast.THereString {
		r, err := p.parseRedir()
		if err != nil {
			return nil, true, err
		}
		return []ast.Redir{r}, true, nil
	}

	if cur.Type == ast.TInt {
		next := p.peek()
		if next.Type == ast.TRedirOut || next.Type == ast.TRedirAppend || next.Type == ast.TRedirIn {
			fd := 0
			fmt.Sscanf(cur.Val, "%d", &fd)
			p.advance()
			r, err := p.parseRedir()
			if err != nil {
				return nil, true, err
			}
			r.Fd = fd
			return []ast.Redir{r}, true, nil
		}
	}

	return nil, false, nil
}

func (p *Parser) parseValue(cmdArg bool) (*ast.Node, error) {
	cur := p.cur()
	isExpr := p.isExprContext()

	switch cur.Type {
	case ast.TInt, ast.TFloat:
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TString:
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TAtom:
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TWord:
		if cur.Val == "fn" {
			if cmdArg {
				defer p.restoreMode(p.withMode(ModeExpr))
			}
			return p.parseIshFn()
		}
		p.advance()
		return ast.WordNode(cur), nil
	case ast.TLParen:
		p.advance()
		expr, err := p.parsePipeline()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(ast.TRParen); err != nil {
			return nil, err
		}
		return expr, nil
	case ast.TLBrace:
		if isExpr || !cmdArg {
			return p.parseTupleExpr()
		}
		p.advance()
		return ast.WordNode(ast.Token{Type: ast.TWord, Val: "{", Pos: cur.Pos}), nil
	case ast.TLBracket:
		if isExpr || !cmdArg {
			return p.parseListExpr()
		}
		p.advance()
		return ast.WordNode(ast.Token{Type: ast.TWord, Val: "[", Pos: cur.Pos}), nil
	case ast.TPercent:
		if p.peek().Type == ast.TLBrace {
			return p.parseMapExpr()
		}
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TBackslash:
		return p.parseLambda()
	case ast.TBang:
		p.advance()
		operand, err := p.parseValue(false)
		if err != nil {
			return nil, err
		}
		return &ast.Node{Kind: ast.NUnary, Tok: cur, Children: []*ast.Node{operand}}, nil
	case ast.TMinus:
		p.advance()
		if cmdArg && p.cur().Type == ast.TInt {
			merged := ast.Token{Type: ast.TWord, Val: "-" + p.cur().Val, Pos: cur.Pos}
			p.advance()
			return ast.WordNode(merged), nil
		}
		operand, err := p.parseValue(false)
		if err != nil {
			return nil, err
		}
		return &ast.Node{Kind: ast.NUnary, Tok: cur, Children: []*ast.Node{operand}}, nil
	case ast.TComma:
		if cmdArg {
			p.advance()
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected comma at pos %d", cur.Pos)
	default:
		if cmdArg {
			p.advance()
			return ast.WordNode(ast.Token{Type: ast.TWord, Val: cur.Val, Pos: cur.Pos}), nil
		}
		return nil, fmt.Errorf("unexpected token: %q at pos %d", cur.Val, cur.Pos)
	}
}

func (p *Parser) parseCmdLine() (*ast.Node, error) {
	startPos := p.cur().Pos
	assigns := p.collectAssigns()
	var children []*ast.Node
	var redirs []ast.Redir

	for {
		cur := p.cur()
		if p.isStmtEnd(cur) {
			break
		}
		if cur.Type == ast.TWord && p.isBlockEnd(cur.Val) {
			break
		}
		if rs, ok, err := p.tryParseRedir(); ok {
			if err != nil {
				return nil, err
			}
			redirs = append(redirs, rs...)
			continue
		}
		// First [ in a command line is the test builtin, not a list literal
		if cur.Type == ast.TLBracket && len(children) == 0 && !cur.ExprHint && p.mode != ModeExpr {
			p.advance()
			children = append(children, ast.WordNode(ast.Token{Type: ast.TWord, Val: "[", Pos: cur.Pos}))
			continue
		}
		arg, err := p.parseValue(true)
		if err != nil {
			return nil, err
		}
		if arg != nil {
			children = append(children, arg)
		}
	}

	if len(children) == 0 && len(redirs) == 0 && len(assigns) == 0 {
		return nil, nil
	}

	if len(children) == 0 && len(assigns) > 0 && len(redirs) == 0 {
		if len(assigns) == 1 {
			return assigns[0], nil
		}
		return &ast.Node{Kind: ast.NBlock, Children: assigns}, nil
	}

	node := &ast.Node{Kind: ast.NCmd, Children: children, Pos: startPos}
	node.Assigns = assigns
	node.Redirs = redirs
	return node, nil
}

func (p *Parser) parseRedir() (ast.Redir, error) {
	r := ast.Redir{Op: p.cur().Type}
	switch r.Op {
	case ast.TRedirOut, ast.TRedirAppend:
		r.Fd = 1
	case ast.TRedirIn, ast.THeredoc, ast.THereString:
		r.Fd = 0
	}
	p.advance()

	if p.cur().Type == ast.TAmpersand {
		p.advance()
		if p.cur().Type == ast.TInt || p.cur().Type == ast.TWord {
			r.Target = "&" + p.cur().Val
			p.advance()
			return r, nil
		}
		return r, fmt.Errorf("expected fd number after >& at pos %d", p.cur().Pos)
	}

	if p.cur().Type == ast.TWord || p.cur().Type == ast.TString || p.cur().Type == ast.TInt {
		r.Target = p.cur().Val
		r.Quoted = p.cur().Quoted
		p.advance()
	} else {
		return r, fmt.Errorf("expected filename after redirection at pos %d", p.cur().Pos)
	}
	return r, nil
}

func (p *Parser) parsePosixAssign() (*ast.Node, error) {
	tok := p.advance()
	return &ast.Node{Kind: ast.NAssign, Tok: tok, Pos: tok.Pos}, nil
}

func (p *Parser) parseIshBind() (*ast.Node, error) {
	nameTok := p.advance()
	p.advance()

	defer p.restoreMode(p.withMode(ModeExpr))
	rhs, err := p.parsePipeline()
	if err != nil {
		return nil, err
	}

	lhs := ast.WordNode(nameTok)
	return &ast.Node{Kind: ast.NMatch, Children: []*ast.Node{lhs, rhs}, Pos: nameTok.Pos}, nil
}


func (p *Parser) parseSubshell() (*ast.Node, error) {
	p.advance()
	stmts, err := p.parseStmtList(ast.TRParen)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(ast.TRParen); err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NSubshell, Children: []*ast.Node{ast.BlockNode(stmts)}}, nil
}

func (p *Parser) parseGroup() (*ast.Node, error) {
	p.advance()
	stmts, err := p.parseStmtList(ast.TRBrace)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(ast.TRBrace); err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NGroup, Children: []*ast.Node{ast.BlockNode(stmts)}}, nil
}

func (p *Parser) parseStmtList(terminator ast.TokenType) ([]*ast.Node, error) {
	var stmts []*ast.Node
	p.skipNewlines()
	for p.cur().Type != terminator && p.cur().Type != ast.TEOF {
		stmt, err := p.parseList()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
		for p.cur().Type == ast.TNewline || p.cur().Type == ast.TSemicolon {
			p.pos++
		}
	}
	return stmts, nil
}

func (p *Parser) parseIf() (*ast.Node, error) {
	p.advance()
	node := &ast.Node{Kind: ast.NIf}

	condMode := p.withMode(ModeCommand)
	old := p.pushTerminators("then", "do")
	cond, err := p.parseList()
	p.restoreTerminators(old)
	p.restoreMode(condMode)
	if err != nil {
		return nil, err
	}

	p.skipSeparators()

	if p.isWord("then") {
		return p.parsePosixIf(cond, node)
	} else if p.isWord("do") {
		return p.parseIshIf(cond, node)
	}
	return nil, fmt.Errorf("expected 'then' or 'do' after if condition at pos %d", p.cur().Pos)
}

func (p *Parser) parsePosixIf(cond *ast.Node, node *ast.Node) (*ast.Node, error) {
	p.advance()
	p.skipNewlines()

	oldMode := p.withMode(ModeCommand)
	defer p.restoreMode(oldMode)
	old := p.pushTerminators("elif", "else", "fi")
	bodyStmts, err := p.parseBlock()
	p.restoreTerminators(old)
	if err != nil {
		return nil, err
	}

	markTail(bodyStmts)
	node.Clauses = append(node.Clauses, ast.Clause{
		Pattern: cond,
		Body:    ast.BlockNode(bodyStmts),
	})

	for p.isWord("elif") {
		p.advance()
		old = p.pushTerminators("then", "do")
		elifCond, err := p.parseList()
		p.restoreTerminators(old)
		if err != nil {
			return nil, err
		}
		p.skipSeparators()
		if p.isWord("then") {
			p.advance()
		}
		p.skipNewlines()
		old = p.pushTerminators("elif", "else", "fi")
		elifBody, err := p.parseBlock()
		p.restoreTerminators(old)
		if err != nil {
			return nil, err
		}
		markTail(elifBody)
		node.Clauses = append(node.Clauses, ast.Clause{
			Pattern: elifCond,
			Body:    ast.BlockNode(elifBody),
		})
	}

	if p.isWord("else") {
		p.advance()
		p.skipNewlines()
		old = p.pushTerminators("fi")
		elseBody, err := p.parseBlock()
		p.restoreTerminators(old)
		if err != nil {
			return nil, err
		}
		markTail(elseBody)
		node.Clauses = append(node.Clauses, ast.Clause{
			Body: ast.BlockNode(elseBody),
		})
	}

	if p.isWord("fi") {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'fi' at pos %d", p.cur().Pos)
	}
	return node, nil
}

func (p *Parser) parseIshIf(cond *ast.Node, node *ast.Node) (*ast.Node, error) {
	if err := p.ishBlockWithSection("else", func() error {
		bodyStmts, err := p.parseBlock()
		if err != nil {
			return err
		}
		markTail(bodyStmts)
		node.Clauses = append(node.Clauses, ast.Clause{
			Pattern: cond,
			Body:    ast.BlockNode(bodyStmts),
		})
		return nil
	}, func() error {
		elseBody, err := p.parseBlock()
		if err != nil {
			return err
		}
		markTail(elseBody)
		node.Clauses = append(node.Clauses, ast.Clause{
			Body: ast.BlockNode(elseBody),
		})
		return nil
	}); err != nil {
		return nil, err
	}
	return node, nil
}

func (p *Parser) parseFor() (*ast.Node, error) {
	p.advance()
	node := &ast.Node{Kind: ast.NFor}

	varTok, err := p.expect(ast.TWord)
	if err != nil {
		return nil, fmt.Errorf("expected variable name after 'for' at pos %d", p.cur().Pos)
	}

	p.skipSeparators()
	if !p.isWord("in") {
		return nil, fmt.Errorf("expected 'in' after 'for %s' at pos %d", varTok.Val, p.cur().Pos)
	}
	p.advance()

	old := p.pushTerminators("do")
	var words []*ast.Node
	for p.cur().Type != ast.TEOF {
		if p.cur().Type == ast.TNewline || p.cur().Type == ast.TSemicolon {
			break
		}
		if p.isWord("do") {
			break
		}
		if p.cur().Type == ast.TWord {
			words = append(words, ast.WordNode(p.cur()))
		} else {
			words = append(words, ast.LitNode(p.cur()))
		}
		p.advance()
	}
	p.restoreTerminators(old)

	p.skipSeparators()
	if p.isWord("do") {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'do' in for loop at pos %d", p.cur().Pos)
	}

	p.skipNewlines()
	old = p.pushTerminators("done", "end")
	bodyStmts, err := p.parseBlock()
	p.restoreTerminators(old)
	if err != nil {
		return nil, err
	}
	if p.isWord("done") || p.isWord("end") {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'done' or 'end' at pos %d", p.cur().Pos)
	}

	markTail(bodyStmts)
	varNode := &ast.Node{Kind: ast.NWord, Tok: varTok}
	node.Children = append([]*ast.Node{varNode}, words...)
	node.Clauses = []ast.Clause{{Body: ast.BlockNode(bodyStmts)}}
	return node, nil
}

func (p *Parser) parseWhile() (*ast.Node, error) {
	return p.parseWhileUntil(ast.NWhile)
}

func (p *Parser) parseUntil() (*ast.Node, error) {
	return p.parseWhileUntil(ast.NUntil)
}

func (p *Parser) parseWhileUntil(kind ast.NodeKind) (*ast.Node, error) {
	p.advance()

	old := p.pushTerminators("do")
	cond, err := p.parseList()
	p.restoreTerminators(old)
	if err != nil {
		return nil, err
	}
	p.skipSeparators()
	if err := p.expectWord("do"); err != nil {
		return nil, err
	}
	p.advance()

	p.skipNewlines()
	old = p.pushTerminators("done", "end")
	bodyStmts, err := p.parseBlock()
	p.restoreTerminators(old)
	if err != nil {
		return nil, err
	}
	if p.isWord("done") || p.isWord("end") {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'done' or 'end' at pos %d", p.cur().Pos)
	}

	markTail(bodyStmts)
	return &ast.Node{
		Kind:     kind,
		Children: []*ast.Node{cond},
		Clauses:  []ast.Clause{{Body: ast.BlockNode(bodyStmts)}},
	}, nil
}

func (p *Parser) parseCase() (*ast.Node, error) {
	p.advance()
	node := &ast.Node{Kind: ast.NCase}

	wordTok := p.advance()
	node.Children = []*ast.Node{{Kind: ast.NWord, Tok: wordTok}}

	p.skipSeparators()
	if !p.isWord("in") {
		return nil, fmt.Errorf("expected 'in' in case at pos %d", p.cur().Pos)
	}
	p.advance()
	p.skipNewlines()

	for p.cur().Type != ast.TEOF {
		if p.isWord("esac") {
			p.advance()
			break
		}
		if p.cur().Type == ast.TLParen {
			p.advance()
		}
		var patterns []string
		for p.cur().Type != ast.TRParen && p.cur().Type != ast.TEOF {
			patterns = append(patterns, p.cur().Val)
			p.advance()
			if p.cur().Type == ast.TPipe {
				p.advance()
			}
		}
		patVal := strings.Join(patterns, "|")
		pat := &ast.Node{Kind: ast.NLit, Tok: ast.Token{Type: ast.TWord, Val: patVal}}
		if p.cur().Type == ast.TRParen {
			p.advance()
		}
		p.skipNewlines()

		old := p.pushTerminators("esac")
		var body []*ast.Node
		for p.cur().Type != ast.TEOF {
			if p.isWord("esac") {
				break
			}
			if p.cur().Type == ast.TSemicolon && p.peek().Type == ast.TSemicolon {
				p.advance()
				p.advance()
				break
			}
			stmt, err := p.parseList()
			if err != nil {
				return nil, err
			}
			if stmt != nil {
				body = append(body, stmt)
			}
			for p.cur().Type == ast.TNewline {
				p.pos++
			}
		}
		p.restoreTerminators(old)
		p.skipNewlines()

		markTail(body)
		node.Clauses = append(node.Clauses, ast.Clause{
			Pattern: pat,
			Body:    ast.BlockNode(body),
		})
	}

	if p.cur().Type == ast.TEOF && len(node.Clauses) == 0 {
		return nil, fmt.Errorf("expected 'esac' at pos %d", p.cur().Pos)
	}

	return node, nil
}

func (p *Parser) parsePosixFnDef() (*ast.Node, error) {
	nameTok := p.advance()
	p.advance()
	p.advance()
	p.skipNewlines()

	if p.cur().Type != ast.TLBrace {
		return nil, fmt.Errorf("expected '{' in function definition at pos %d", p.cur().Pos)
	}
	p.advance()
	p.skipNewlines()

	var bodyStmts []*ast.Node
	for p.cur().Type != ast.TEOF {
		if p.cur().Type == ast.TRBrace {
			break
		}
		stmt, err := p.parseList()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			bodyStmts = append(bodyStmts, stmt)
		}
		p.skipSeparators()
	}
	if p.cur().Type == ast.TRBrace {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected '}' in function definition at pos %d", p.cur().Pos)
	}

	markTail(bodyStmts)
	return &ast.Node{
		Kind:     ast.NFnDef,
		Tok:      nameTok,
		Children: []*ast.Node{ast.BlockNode(bodyStmts)},
	}, nil
}

func (p *Parser) parseIshFn() (*ast.Node, error) {
	p.advance()

	var nameTok ast.Token
	if p.mode == ModeExpr || (p.isWord("do")) {
		// Anonymous: expression context, or fn do...end at statement level
		nameTok = ast.Token{Type: ast.TWord, Val: "<anon>"}
	} else {
		// fn name ... — named function definition at statement level
		var err error
		nameTok, err = p.expect(ast.TWord)
		if err != nil {
			return nil, fmt.Errorf("expected function name after 'fn' at pos %d", p.cur().Pos)
		}
	}

	var params []*ast.Node
	for p.cur().Type != ast.TEOF {
		if p.isWord("when") || p.isWord("do") {
			break
		}
		if p.cur().Type == ast.TComma {
			p.advance()
			continue
		}
		param, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		params = append(params, param)
	}

	var guard *ast.Node
	if p.isWord("when") {
		p.advance()
		guardMode := p.withMode(ModeExpr)
		var err error
		guard, err = p.parseExpr(0)
		p.restoreMode(guardMode)
		if err != nil {
			return nil, err
		}
	}

	var fnNode *ast.Node
	if err := p.ishBlock(func() error {
		if len(params) == 0 && guard == nil && p.looksLikeClauseStart() {
			clauses, err := p.parseClauses(func() (*ast.Node, error) {
				var clauseParams []*ast.Node
				for p.cur().Type != ast.TEOF {
					if p.cur().Type == ast.TArrow || (p.isWord("when")) {
						break
					}
					if p.cur().Type == ast.TComma {
						p.advance()
						continue
					}
					param, err := p.parsePattern()
					if err != nil {
						return nil, err
					}
					clauseParams = append(clauseParams, param)
				}
				return ast.BlockNode(clauseParams), nil
			})
			if err != nil {
				return err
			}
			fnNode = &ast.Node{Kind: ast.NIshFn, Tok: nameTok, Clauses: clauses}
			return nil
		}

		bodyStmts, err := p.parseBlock()
		if err != nil {
			return err
		}
		markTail(bodyStmts)
		fnNode = &ast.Node{
			Kind: ast.NIshFn,
			Tok:  nameTok,
			Clauses: []ast.Clause{{
				Body:  ast.BlockNode(bodyStmts),
				Guard: guard,
			}},
			Children: params,
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return fnNode, nil
}

func (p *Parser) parseClauseBody() (*ast.Node, error) {
	defer p.restoreTerminators(p.pushTerminators("end"))
	var stmts []*ast.Node
	for p.cur().Type != ast.TEOF {
		if p.isWord("end") || p.isWord("after") {
			break
		}
		if p.looksLikeClauseStart() {
			break
		}
		stmt, err := p.parseList()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
		p.skipSeparators()
	}
	markTail(stmts)
	return ast.BlockNode(stmts), nil
}

func (p *Parser) looksLikeClauseStart() bool {
	depth := 0
	inLambda := false
	for i := p.pos; ; i++ {
		p.fillTo(i)
		if i-p.base >= len(p.tokens) {
			return false
		}
		t := p.tokens[i-p.base]
		if t.Type == ast.TNewline || t.Type == ast.TSemicolon || t.Type == ast.TEOF {
			return false
		}
		// Stop at keywords that have their own -> syntax
		if t.Type == ast.TWord && (t.Val == "after" || t.Val == "end" || t.Val == "rescue") {
			return false
		}
		switch t.Type {
		case ast.TBackslash:
			inLambda = true
		case ast.TLParen, ast.TLBracket, ast.TLBrace:
			depth++
		case ast.TRParen, ast.TRBracket, ast.TRBrace:
			depth--
		case ast.TArrow:
			if inLambda {
				// This arrow belongs to a lambda, skip it
				inLambda = false
				continue
			}
			if depth == 0 {
				return true
			}
		}
	}
}

func (p *Parser) parseClauses(parsePattern func() (*ast.Node, error)) ([]ast.Clause, error) {
	var clauses []ast.Clause
	for p.cur().Type != ast.TEOF {
		if p.cur().Type == ast.TWord && p.isBlockEnd(p.cur().Val) {
			break
		}
		pat, err := parsePattern()
		if err != nil {
			return nil, err
		}
		var guard *ast.Node
		if p.isWord("when") {
			p.advance()
			guard, err = p.parseExpr(0)
			if err != nil {
				return nil, err
			}
		}
		if p.cur().Type != ast.TArrow {
			return nil, fmt.Errorf("expected '->' in clause at pos %d", p.cur().Pos)
		}
		p.advance()
		p.skipNewlines()
		body, err := p.parseClauseBody()
		if err != nil {
			return nil, err
		}
		p.skipSeparators()
		clauses = append(clauses, ast.Clause{Pattern: pat, Guard: guard, Body: body})
	}
	return clauses, nil
}

func (p *Parser) parseIshMatchExpr() (*ast.Node, error) {
	p.advance()

	subject, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	p.skipSeparators()
	var clauses []ast.Clause
	if err := p.ishBlock(func() error {
		var err error
		clauses, err = p.parseClauses(p.parsePattern)
		return err
	}); err != nil {
		return nil, err
	}

	return &ast.Node{Kind: ast.NIshMatch, Children: []*ast.Node{subject}, Clauses: clauses}, nil
}

func (p *Parser) parseIshSpawn() (*ast.Node, error) {
	p.advance()
	expr, err := p.parseStmtWithOps()
	if err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NIshSpawn, Children: []*ast.Node{expr}}, nil
}

func (p *Parser) parseIshSpawnLink() (*ast.Node, error) {
	p.advance()
	expr, err := p.parseStmtWithOps()
	if err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NIshSpawnLink, Children: []*ast.Node{expr}}, nil
}

func (p *Parser) parseIshMonitor() (*ast.Node, error) {
	p.advance()
	target, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NIshMonitor, Children: []*ast.Node{target}}, nil
}

func (p *Parser) parseIshAwait() (*ast.Node, error) {
	p.advance()
	target, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NIshAwait, Children: []*ast.Node{target}}, nil
}

func (p *Parser) parseIshSupervise() (*ast.Node, error) {
	p.advance()
	strategy, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	p.skipSeparators()
	var workers []*ast.Node
	if err := p.ishBlock(func() error {
		for p.cur().Type != ast.TEOF {
			if p.cur().Type == ast.TWord && p.isBlockEnd(p.cur().Val) {
				break
			}
			if p.isWord("worker") {
				p.advance()
				workerName, err := p.parseExpr(0)
				if err != nil {
					return err
				}
				fnExpr, err := p.parseStmtWithOps()
				if err != nil {
					return err
				}
				workers = append(workers, &ast.Node{
					Kind:     ast.NCmd,
					Children: []*ast.Node{workerName, fnExpr},
				})
			} else {
				stmt, err := p.parseList()
				if err != nil {
					return err
				}
				if stmt != nil {
					workers = append(workers, stmt)
				}
			}
			p.skipSeparators()
		}
		return nil
	}); err != nil {
		return nil, err
	}

	node := &ast.Node{Kind: ast.NIshSupervise, Children: append([]*ast.Node{strategy}, workers...)}
	return node, nil
}

func (p *Parser) parseIshSend() (*ast.Node, error) {
	p.advance()
	target, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.cur().Type == ast.TComma {
		p.advance()
	}
	msg, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NIshSend, Children: []*ast.Node{target, msg}}, nil
}

func (p *Parser) parseIshReceive() (*ast.Node, error) {
	p.advance()
	p.skipSeparators()

	node := &ast.Node{Kind: ast.NIshReceive}
	if err := p.ishBlockWithSection("after", func() error {
		clauses, err := p.parseClauses(p.parsePattern)
		if err != nil {
			return err
		}
		node.Clauses = clauses
		return nil
	}, func() error {
		timeoutExpr, err := p.parseExpr(0)
		if err != nil {
			return err
		}
		node.Timeout = timeoutExpr
		if p.cur().Type != ast.TArrow {
			return fmt.Errorf("expected '->' after timeout expression at pos %d", p.cur().Pos)
		}
		p.advance()
		p.skipNewlines()
		bodyStmts, err := p.parseBlock()
		if err != nil {
			return err
		}
		markTail(bodyStmts)
		node.TimeoutBody = ast.BlockNode(bodyStmts)
		return nil
	}); err != nil {
		return nil, err
	}

	return node, nil
}

func (p *Parser) parseIshTry() (*ast.Node, error) {
	p.advance()
	p.skipSeparators()

	node := &ast.Node{Kind: ast.NIshTry}
	if err := p.ishBlockWithSection("rescue", func() error {
		bodyStmts, err := p.parseBlock()
		if err != nil {
			return err
		}
		markTail(bodyStmts)
		node.Children = []*ast.Node{ast.BlockNode(bodyStmts)}
		return nil
	}, func() error {
		clauses, err := p.parseClauses(p.parsePattern)
		if err != nil {
			return err
		}
		node.Clauses = clauses
		return nil
	}); err != nil {
		return nil, err
	}
	return node, nil
}

func (p *Parser) parsePattern() (*ast.Node, error) {
	cur := p.cur()
	switch cur.Type {
	case ast.TAtom:
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TInt, ast.TFloat:
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TString:
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TWord:
		if cur.Val == "_" {
			p.advance()
			return &ast.Node{Kind: ast.NWord, Tok: ast.Token{Type: ast.TWord, Val: "_"}}, nil
		}
		p.advance()
		return ast.WordNode(cur), nil
	case ast.TLBrace:
		return p.parseTupleExpr()
	case ast.TLBracket:
		return p.parseListExpr()
	default:
		return nil, fmt.Errorf("unexpected token in pattern: %q at pos %d", cur.Val, cur.Pos)
	}
}

func (p *Parser) parseLambda() (*ast.Node, error) {
	p.advance() // skip backslash

	// Parse params until ->
	var params []*ast.Node
	for p.cur().Type != ast.TEOF && p.cur().Type != ast.TArrow {
		if p.cur().Type == ast.TComma {
			p.advance()
			continue
		}
		param, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		params = append(params, param)
	}

	if p.cur().Type != ast.TArrow {
		return nil, fmt.Errorf("expected '->' in lambda at pos %d", p.cur().Pos)
	}
	p.advance() // skip ->

	var body *ast.Node
	if err := p.withExprBlock([]string{"end"}, func() error {
		var e error
		body, e = p.parseStmtWithOps()
		if body != nil {
			body.Tail = true
		}
		return e
	}); err != nil {
		return nil, err
	}

	return &ast.Node{
		Kind:     ast.NLambda,
		Children: params,
		Clauses:  []ast.Clause{{Body: body}},
	}, nil
}

func (p *Parser) parseExpression() (*ast.Node, error) {
	expr, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	if p.cur().Type == ast.TEquals {
		p.advance()
		defer p.restoreMode(p.withMode(ModeExpr))
		rhs, err := p.parsePipeline()
		if err != nil {
			return nil, err
		}
		return &ast.Node{Kind: ast.NMatch, Children: []*ast.Node{expr, rhs}}, nil
	}

	return expr, nil
}

func (p *Parser) parseExpr(minPrec int) (*ast.Node, error) {
	p.depth++
	if p.depth > maxParseDepth {
		return nil, fmt.Errorf("expression too deeply nested (depth > %d)", maxParseDepth)
	}
	defer func() { p.depth-- }()

	left, err := p.parseValue(false)
	if err != nil {
		return nil, err
	}

	for {
		prec := p.precedence(p.cur().Type)
		if prec <= minPrec {
			break
		}
		op := p.advance()
		right, err := p.parseExpr(prec)
		if err != nil {
			return nil, err
		}
		left = &ast.Node{Kind: ast.NBinOp, Tok: op, Children: []*ast.Node{left, right}}
	}

	for p.cur().Type == ast.TDot {
		p.advance()
		field := p.advance()
		left = &ast.Node{Kind: ast.NAccess, Tok: field, Children: []*ast.Node{left}}
	}

	return left, nil
}

func (p *Parser) precedence(tt ast.TokenType) int {
	switch tt {
	case ast.TEq, ast.TNe:
		return 1
	case ast.TLe, ast.TGe:
		return 2
	case ast.TRedirIn, ast.TRedirOut:
		if p.mode == ModeExpr {
			return 2
		}
		return 0
	case ast.TPlus, ast.TMinus:
		return 3
	case ast.TMul, ast.TDiv, ast.TPercent:
		return 4
	default:
		return 0
	}
}

func (p *Parser) parseTupleExpr() (*ast.Node, error) {
	p.advance()
	var elems []*ast.Node
	p.skipNewlines()
	for p.cur().Type != ast.TRBrace && p.cur().Type != ast.TEOF {
		elem, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		elems = append(elems, elem)
		if p.cur().Type == ast.TComma {
			p.advance()
			p.skipNewlines()
		}
	}
	if _, err := p.expect(ast.TRBrace); err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NTuple, Children: elems}, nil
}

func (p *Parser) parseListExpr() (*ast.Node, error) {
	p.advance()
	var elems []*ast.Node
	var rest *ast.Node
	p.skipNewlines()
	for p.cur().Type != ast.TRBracket && p.cur().Type != ast.TEOF {
		elem, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		elems = append(elems, elem)
		if p.cur().Type == ast.TPipe {
			p.advance()
			p.skipNewlines()
			tail, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			rest = tail
			break
		}
		if p.cur().Type == ast.TComma {
			p.advance()
			p.skipNewlines()
		}
	}
	if _, err := p.expect(ast.TRBracket); err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NList, Children: elems, Rest: rest}, nil
}

func (p *Parser) parseMapExpr() (*ast.Node, error) {
	p.advance()
	p.advance()
	node := &ast.Node{Kind: ast.NMap}
	p.skipNewlines()
	for p.cur().Type != ast.TRBrace && p.cur().Type != ast.TEOF {
		key := p.advance()
		keyName := key.Val
		if strings.HasSuffix(keyName, ":") {
			keyName = keyName[:len(keyName)-1]
		} else if p.isWord(":") {
			p.advance()
		}
		key.Val = keyName
		val, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, &ast.Node{Kind: ast.NLit, Tok: key}, val)
		if p.cur().Type == ast.TComma {
			p.advance()
			p.skipNewlines()
		}
	}
	if _, err := p.expect(ast.TRBrace); err != nil {
		return nil, err
	}
	return node, nil
}
