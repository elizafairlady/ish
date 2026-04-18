package main

import (
	"fmt"
	"strings"
)

// isAssignment returns true if a TWord token looks like a POSIX assignment (VAR=value).
// The part before = must be a valid variable name (letters, digits, underscore, not starting with digit).
func isAssignment(tok Token) bool {
	if tok.Type != TWord {
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

// pushTerminators adds block-terminating words and returns the old set for restoration.
func (p *Parser) pushTerminators(terms ...string) []string {
	old := p.terminators
	p.terminators = append(append([]string{}, old...), terms...)
	return old
}

// restoreTerminators restores a previously saved terminator set.
func (p *Parser) restoreTerminators(old []string) {
	p.terminators = old
}

func isExprOperator(tt TokenType) bool {
	switch tt {
	case TPlus, TMul, TDiv, TEq, TNe, TLe, TGe, TDot:
		return true
	}
	return false
}

const maxParseDepth = 1000

type Parser struct {
	tokens      []Token
	pos         int
	terminators []string // context-sensitive block-end words
	depth       int      // recursion depth for expression parsing
}

func Parse(tokens []Token) (*Node, error) {
	p := &Parser{tokens: tokens}
	return p.parseProgram()
}

func (p *Parser) cur() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TEOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) peek() Token {
	if p.pos+1 >= len(p.tokens) {
		return Token{Type: TEOF}
	}
	return p.tokens[p.pos+1]
}

func (p *Parser) advance() Token {
	t := p.cur()
	p.pos++
	return t
}

func (p *Parser) expect(tt TokenType) (Token, error) {
	t := p.cur()
	if t.Type != tt {
		if t.Type == TEOF {
			return t, fmt.Errorf("unexpected end of input (expected closing delimiter)")
		}
		return t, fmt.Errorf("expected %d, got %d (%q) at pos %d", tt, t.Type, t.Val, t.Pos)
	}
	p.pos++
	return t, nil
}

func (p *Parser) match(tt TokenType) bool {
	if p.cur().Type == tt {
		p.pos++
		return true
	}
	return false
}

func (p *Parser) skipNewlines() {
	for p.cur().Type == TNewline {
		p.pos++
	}
}

func (p *Parser) parseProgram() (*Node, error) {
	var stmts []*Node
	p.skipNewlines()
	for p.cur().Type != TEOF {
		stmt, err := p.parseList()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
		// consume statement separators
		for p.cur().Type == TNewline || p.cur().Type == TSemicolon {
			p.pos++
		}
	}
	return blockNode(stmts), nil
}

// parseList: pipeline ((&& | ||) pipeline)*
func (p *Parser) parseList() (*Node, error) {
	left, err := p.parsePipeline()
	if err != nil || left == nil {
		return left, err
	}

	for {
		if p.cur().Type == TAnd {
			p.advance()
			p.skipNewlines()
			right, err := p.parsePipeline()
			if err != nil {
				return nil, err
			}
			left = &Node{Kind: NAndList, Children: []*Node{left, right}}
		} else if p.cur().Type == TOr {
			p.advance()
			p.skipNewlines()
			right, err := p.parsePipeline()
			if err != nil {
				return nil, err
			}
			left = &Node{Kind: NOrList, Children: []*Node{left, right}}
		} else if p.cur().Type == TAmpersand {
			p.advance()
			left = &Node{Kind: NBg, Children: []*Node{left}}
		} else {
			break
		}
	}
	return left, nil
}

// parsePipeline: command (| command)* or expr (|> expr)*
func (p *Parser) parsePipeline() (*Node, error) {
	left, err := p.parseCommand()
	if err != nil || left == nil {
		return left, err
	}

	for {
		if p.cur().Type == TPipe {
			tok := p.advance()
			p.skipNewlines()
			right, err := p.parseCommand()
			if err != nil {
				return nil, err
			}
			left = &Node{Kind: NPipe, Tok: tok, Children: []*Node{left, right}}
		} else if p.cur().Type == TPipeArrow {
			p.advance()
			p.skipNewlines()
			right, err := p.parseCommand()
			if err != nil {
				return nil, err
			}
			left = &Node{Kind: NPipeFn, Children: []*Node{left, right}}
		} else {
			break
		}
	}
	return left, nil
}

func (p *Parser) parseCommand() (*Node, error) {
	p.skipNewlines()

	left, err := p.parseCommandInner()
	if err != nil || left == nil {
		return left, err
	}

	// If followed by an arithmetic operator, parse as binary expression.
	// This handles: fib (n-1) + fib (n-2)
	for p.precedence(p.cur().Type) > 0 {
		op := p.advance()
		right, err := p.parseCommandInner()
		if err != nil {
			return nil, err
		}
		left = &Node{Kind: NBinOp, Tok: op, Children: []*Node{left, right}}
	}

	return left, nil
}

func (p *Parser) parseCommandInner() (*Node, error) {
	cur := p.cur()

	switch cur.Type {
	case TEOF:
		return nil, nil

	case TLParen:
		return p.parseSubshell()

	case TAtom:
		return p.parseExpression()

	case TLBrace:
		// Could be a tuple {a, b} or a group { cmd; }
		if p.looksLikeTupleExpr() {
			return p.parseExpression()
		}
		return p.parseGroup()

	case TLBracket:
		if p.looksLikeListExpr() {
			return p.parseExpression()
		}
		return p.parseSimpleCommand()

	case TPercent:
		if p.peek().Type == TLBrace {
			return p.parseExpression()
		}

	case TInt:
		return p.parseExpression()

	case TString:
		return p.parseExpression()

	case TWord:
		return p.parseWordCommand()

	case TMinus:
		return p.parseExpression()
	}

	return p.parseSimpleCommand()
}

func (p *Parser) parseWordCommand() (*Node, error) {
	cur := p.cur()
	if isAssignment(cur) {
		return p.parsePosixAssign()
	}
	switch cur.Val {
	// POSIX compound commands
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
	// ish keywords
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
	// ish match/bind: word = expr
	if p.peek().Type == TEquals {
		return p.parseIshBind()
	}
	// Expression: word followed by an operator
	if isExprOperator(p.peek().Type) {
		return p.parseExpression()
	}
	// POSIX function definition: name() { body; }
	if p.peek().Type == TLParen && p.pos+2 < len(p.tokens) && p.tokens[p.pos+2].Type == TRParen {
		return p.parsePosixFnDef()
	}
	return p.parseSimpleCommand()
}

// isCommandEnd returns true if the current token ends a simple command.
func (p *Parser) isCommandEnd(cur Token) bool {
	switch cur.Type {
	case TEOF, TNewline, TSemicolon, TPipe, TPipeArrow, TAnd, TOr,
		TRParen, TRBrace, TArrow, TPlus, TMul, TDiv, TEq, TNe, TLe, TGe:
		return true
	case TAmpersand:
		// &> and &>> are redirections, not background
		return p.peek().Type != TRedirOut && p.peek().Type != TRedirAppend
	}
	return false
}

// collectAssigns consumes leading POSIX assignments (FOO=bar BAR=baz ...).
func (p *Parser) collectAssigns() []*Node {
	var assigns []*Node
	for p.cur().Type == TWord && isAssignment(p.cur()) {
		tok := p.advance()
		assigns = append(assigns, &Node{Kind: NAssign, Tok: tok, Pos: tok.Pos})
	}
	return assigns
}

// tryParseRedir tries to parse a redirection at the current position.
// Returns the redirections, true if tokens were consumed, and any error.
// Returns nil, false, nil if the current token isn't a redirect.
func (p *Parser) tryParseRedir() ([]Redir, bool, error) {
	cur := p.cur()

	// &> or &>> — redirect both stdout and stderr
	if cur.Type == TAmpersand && (p.peek().Type == TRedirOut || p.peek().Type == TRedirAppend) {
		p.advance() // skip &
		r, err := p.parseRedir()
		if err != nil {
			return nil, true, err
		}
		r.Fd = 1
		return []Redir{r, {Op: r.Op, Fd: 2, Target: r.Target, Quoted: r.Quoted}}, true, nil
	}

	// Standard redirections: > >> < << <<<
	if cur.Type == TRedirOut || cur.Type == TRedirAppend || cur.Type == TRedirIn || cur.Type == THeredoc || cur.Type == THereString {
		r, err := p.parseRedir()
		if err != nil {
			return nil, true, err
		}
		return []Redir{r}, true, nil
	}

	// fd-prefixed redirect: 2> 2>> 0<
	if cur.Type == TInt {
		next := p.peek()
		if next.Type == TRedirOut || next.Type == TRedirAppend || next.Type == TRedirIn {
			fd := 0
			fmt.Sscanf(cur.Val, "%d", &fd)
			p.advance() // consume the fd number
			r, err := p.parseRedir()
			if err != nil {
				return nil, true, err
			}
			r.Fd = fd
			return []Redir{r}, true, nil
		}
	}

	return nil, false, nil
}

// parseArg parses a single argument in command context.
// isFirst is true when this is the first (command-name) position.
func (p *Parser) parseArg(isFirst bool) (*Node, error) {
	cur := p.cur()
	switch cur.Type {
	case TLParen:
		p.advance() // skip (
		expr, err := p.parsePipelineOrExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TRParen); err != nil {
			return nil, err
		}
		return expr, nil
	case TWord:
		if cur.Val == "fn" {
			return p.parseIshFn()
		}
		p.advance()
		return wordNode(cur), nil
	case TString:
		p.advance()
		return litNode(cur), nil
	case TInt:
		p.advance()
		return litNode(cur), nil
	case TAtom:
		p.advance()
		return litNode(cur), nil
	case TMinus:
		p.advance()
		if p.cur().Type == TInt {
			merged := Token{Type: TWord, Val: "-" + p.cur().Val, Pos: cur.Pos}
			p.advance()
			return wordNode(merged), nil
		}
		return wordNode(Token{Type: TWord, Val: "-", Pos: cur.Pos}), nil
	case TLBrace:
		return p.parseTupleExpr()
	case TLBracket:
		if isFirst {
			// [ at command position — treat as the [ test builtin name
			p.advance()
			return wordNode(Token{Type: TWord, Val: "[", Pos: cur.Pos}), nil
		}
		return p.parseListExpr()
	case TPercent:
		if p.peek().Type == TLBrace {
			return p.parseMapExpr()
		}
		p.advance()
		return wordNode(Token{Type: TWord, Val: cur.Val, Pos: cur.Pos}), nil
	case TComma:
		p.advance()
		return nil, nil // skip — caller continues
	default:
		p.advance()
		return wordNode(Token{Type: TWord, Val: cur.Val, Pos: cur.Pos}), nil
	}
}

func (p *Parser) parseSimpleCommand() (*Node, error) {
	startPos := p.cur().Pos
	assigns := p.collectAssigns()
	var children []*Node
	var redirs []Redir

	for {
		cur := p.cur()
		if p.isCommandEnd(cur) {
			break
		}
		if cur.Type == TWord && p.isBlockEnd(cur.Val) {
			break
		}
		if rs, ok, err := p.tryParseRedir(); ok {
			if err != nil {
				return nil, err
			}
			redirs = append(redirs, rs...)
			continue
		}
		arg, err := p.parseArg(len(children) == 0)
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

	// If only assignments and no command, return as a block of assignments
	if len(children) == 0 && len(assigns) > 0 && len(redirs) == 0 {
		if len(assigns) == 1 {
			return assigns[0], nil
		}
		return &Node{Kind: NBlock, Children: assigns}, nil
	}

	node := &Node{Kind: NCmd, Children: children, Pos: startPos}
	node.Assigns = assigns
	node.Redirs = redirs
	return node, nil
}

func (p *Parser) parseRedir() (Redir, error) {
	r := Redir{Op: p.cur().Type}
	switch r.Op {
	case TRedirOut, TRedirAppend:
		r.Fd = 1
	case TRedirIn, THeredoc, THereString:
		r.Fd = 0
	}
	p.advance()

	// Handle >&fd (fd duplication): >&2, 2>&1
	if p.cur().Type == TAmpersand {
		p.advance()
		if p.cur().Type == TInt || p.cur().Type == TWord {
			r.Target = "&" + p.cur().Val // mark as fd dup with & prefix
			p.advance()
			return r, nil
		}
		return r, fmt.Errorf("expected fd number after >& at pos %d", p.cur().Pos)
	}

	if p.cur().Type == TWord || p.cur().Type == TString || p.cur().Type == TInt {
		r.Target = p.cur().Val
		r.Quoted = p.cur().Quoted
		p.advance()
	} else {
		return r, fmt.Errorf("expected filename after redirection at pos %d", p.cur().Pos)
	}
	return r, nil
}

func (p *Parser) parsePosixAssign() (*Node, error) {
	tok := p.advance()
	return &Node{Kind: NAssign, Tok: tok, Pos: tok.Pos}, nil
}

// parseIshBind: pattern = expr
func (p *Parser) parseIshBind() (*Node, error) {
	nameTok := p.advance() // consume the identifier
	p.advance()            // consume =

	// Parse the right-hand side as an expression (with pipeline support)
	rhs, err := p.parsePipelineOrExpr()
	if err != nil {
		return nil, err
	}

	lhs := wordNode(nameTok)
	return &Node{Kind: NMatch, Children: []*Node{lhs, rhs}, Pos: nameTok.Pos}, nil
}

// parsePipelineOrExpr parses the RHS of an = binding.
// Tries expression parsing first, but falls back to command parsing
// for things like $(cmd) or command invocations.
func (p *Parser) parsePipelineOrExpr() (*Node, error) {
	left, err := p.parseCommandOrExpr()
	if err != nil {
		return left, err
	}
	for {
		if p.cur().Type == TPipeArrow {
			p.advance()
			p.skipNewlines()
			right, err := p.parseCommandOrExpr()
			if err != nil {
				return nil, err
			}
			left = &Node{Kind: NPipeFn, Children: []*Node{left, right}}
		} else if p.cur().Type == TPipe {
			p.advance()
			p.skipNewlines()
			right, err := p.parseCommandOrExpr()
			if err != nil {
				return nil, err
			}
			left = &Node{Kind: NPipe, Children: []*Node{left, right}}
		} else {
			break
		}
	}
	return left, nil
}

// parseCommandOrExpr: if it starts with an expression token, parse as expression.
// If it starts with a word, check context to decide.
func (p *Parser) parseCommandOrExpr() (*Node, error) {
	cur := p.cur()
	switch cur.Type {
	case TInt, TAtom, TLParen, TLBracket, TBang, TMinus:
		return p.parseExpr(0)
	case TString:
		return p.parseExpr(0)
	case TLBrace:
		return p.parseTupleExpr()
	case TPercent:
		if p.peek().Type == TLBrace {
			return p.parseMapExpr()
		}
	case TWord:
		// Check if followed by expression operators -> expression
		// Include TMinus here since in expression context a - b is subtraction
		if isExprOperator(p.peek().Type) || p.peek().Type == TMinus {
			return p.parseExpr(0)
		}
		// Check for ish keywords that produce values
		if cur.Val == "fn" || cur.Val == "match" || cur.Val == "spawn" ||
			cur.Val == "spawn_link" || cur.Val == "monitor" || cur.Val == "await" ||
			cur.Val == "supervise" || cur.Val == "receive" || cur.Val == "try" {
			return p.parseCommand()
		}
		// Check if it looks like a command invocation (word followed by more words/args)
		// vs a bare variable reference
		next := p.peek().Type
		if next == TNewline || next == TEOF || next == TSemicolon ||
			next == TPipe || next == TPipeArrow || next == TAnd || next == TOr ||
			next == TRParen || next == TRBrace || next == TComma || next == TArrow {
			// Bare word — treat as variable reference
			return p.parseExpr(0)
		}
		// Word followed by args — treat as command/function call
		return p.parseSimpleCommand()
	}
	return p.parseCommand()
}

func (p *Parser) parseSubshell() (*Node, error) {
	p.advance() // skip (
	stmts, err := p.parseStmtList(TRParen)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TRParen); err != nil {
		return nil, err
	}
	return &Node{Kind: NSubshell, Children: []*Node{blockNode(stmts)}}, nil
}

func (p *Parser) parseGroup() (*Node, error) {
	p.advance() // skip {
	stmts, err := p.parseStmtList(TRBrace)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TRBrace); err != nil {
		return nil, err
	}
	return &Node{Kind: NGroup, Children: []*Node{blockNode(stmts)}}, nil
}

func (p *Parser) parseStmtList(terminator TokenType) ([]*Node, error) {
	var stmts []*Node
	p.skipNewlines()
	for p.cur().Type != terminator && p.cur().Type != TEOF {
		stmt, err := p.parseList()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
		for p.cur().Type == TNewline || p.cur().Type == TSemicolon {
			p.pos++
		}
	}
	return stmts, nil
}

// POSIX: if cond; then body; [elif cond; then body;] [else body;] fi
func (p *Parser) parseIf() (*Node, error) {
	p.advance() // skip "if"
	node := &Node{Kind: NIf}

	// Condition parsing: "then" and "do" are terminators
	old := p.pushTerminators("then", "do")
	cond, err := p.parseList()
	p.restoreTerminators(old)
	if err != nil {
		return nil, err
	}

	// skip optional separators
	for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
		p.pos++
	}

	if p.cur().Type == TWord && p.cur().Val == "then" {
		// POSIX if
		return p.parsePosixIf(cond, node)
	} else if p.cur().Type == TWord && p.cur().Val == "do" {
		// ish if
		return p.parseIshIf(cond, node)
	}
	return nil, fmt.Errorf("expected 'then' or 'do' after if condition at pos %d", p.cur().Pos)
}

func (p *Parser) parsePosixIf(cond *Node, node *Node) (*Node, error) {
	p.advance() // skip "then"
	p.skipNewlines()

	// Parse then body — terminators: elif, else, fi
	old := p.pushTerminators("elif", "else", "fi")
	var bodyStmts []*Node
	for p.cur().Type != TEOF {
		if p.cur().Type == TWord && (p.cur().Val == "elif" || p.cur().Val == "else" || p.cur().Val == "fi") {
			break
		}
		stmt, err := p.parseList()
		if err != nil {
			p.restoreTerminators(old)
			return nil, err
		}
		if stmt != nil {
			bodyStmts = append(bodyStmts, stmt)
		}
		for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
			p.pos++
		}
	}
	p.restoreTerminators(old)

	node.Clauses = append(node.Clauses, Clause{
		Pattern: cond,
		Body:    blockNode(bodyStmts),
	})

	// Handle elif
	for p.cur().Type == TWord && p.cur().Val == "elif" {
		p.advance()
		old = p.pushTerminators("then", "do")
		elifCond, err := p.parseList()
		p.restoreTerminators(old)
		if err != nil {
			return nil, err
		}
		for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
			p.pos++
		}
		if p.cur().Type == TWord && p.cur().Val == "then" {
			p.advance()
		}
		p.skipNewlines()
		old = p.pushTerminators("elif", "else", "fi")
		var elifBody []*Node
		for p.cur().Type != TEOF {
			if p.cur().Type == TWord && (p.cur().Val == "elif" || p.cur().Val == "else" || p.cur().Val == "fi") {
				break
			}
			stmt, err := p.parseList()
			if err != nil {
				p.restoreTerminators(old)
				return nil, err
			}
			if stmt != nil {
				elifBody = append(elifBody, stmt)
			}
			for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
				p.pos++
			}
		}
		p.restoreTerminators(old)
		node.Clauses = append(node.Clauses, Clause{
			Pattern: elifCond,
			Body:    blockNode(elifBody),
		})
	}

	// Handle else
	if p.cur().Type == TWord && p.cur().Val == "else" {
		p.advance()
		p.skipNewlines()
		old = p.pushTerminators("fi")
		var elseBody []*Node
		for p.cur().Type != TEOF {
			if p.cur().Type == TWord && p.cur().Val == "fi" {
				break
			}
			stmt, err := p.parseList()
			if err != nil {
				p.restoreTerminators(old)
				return nil, err
			}
			if stmt != nil {
				elseBody = append(elseBody, stmt)
			}
			for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
				p.pos++
			}
		}
		p.restoreTerminators(old)
		node.Clauses = append(node.Clauses, Clause{
			Body: blockNode(elseBody),
		})
	}

	if p.cur().Type == TWord && p.cur().Val == "fi" {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'fi' at pos %d", p.cur().Pos)
	}
	return node, nil
}

func (p *Parser) parseIshIf(cond *Node, node *Node) (*Node, error) {
	p.advance() // skip "do"
	p.skipNewlines()

	old := p.pushTerminators("else", "end")
	var bodyStmts []*Node
	for p.cur().Type != TEOF {
		if p.cur().Type == TWord && (p.cur().Val == "else" || p.cur().Val == "end") {
			break
		}
		stmt, err := p.parseList()
		if err != nil {
			p.restoreTerminators(old)
			return nil, err
		}
		if stmt != nil {
			bodyStmts = append(bodyStmts, stmt)
		}
		for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
			p.pos++
		}
	}
	p.restoreTerminators(old)

	node.Clauses = append(node.Clauses, Clause{
		Pattern: cond,
		Body:    blockNode(bodyStmts),
	})

	if p.cur().Type == TWord && p.cur().Val == "else" {
		p.advance()
		p.skipNewlines()
		old = p.pushTerminators("end")
		var elseBody []*Node
		for p.cur().Type != TEOF {
			if p.cur().Type == TWord && p.cur().Val == "end" {
				break
			}
			stmt, err := p.parseList()
			if err != nil {
				p.restoreTerminators(old)
				return nil, err
			}
			if stmt != nil {
				elseBody = append(elseBody, stmt)
			}
			for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
				p.pos++
			}
		}
		p.restoreTerminators(old)
		node.Clauses = append(node.Clauses, Clause{
			Body: blockNode(elseBody),
		})
	}

	if p.cur().Type == TWord && p.cur().Val == "end" {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'end' at pos %d", p.cur().Pos)
	}
	return node, nil
}

func (p *Parser) parseFor() (*Node, error) {
	p.advance() // skip "for"
	node := &Node{Kind: NFor}

	varTok, err := p.expect(TWord)
	if err != nil {
		return nil, fmt.Errorf("expected variable name after 'for' at pos %d", p.cur().Pos)
	}

	// Expect "in"
	for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
		p.pos++
	}
	if p.cur().Type != TWord || p.cur().Val != "in" {
		return nil, fmt.Errorf("expected 'in' after 'for %s' at pos %d", varTok.Val, p.cur().Pos)
	}
	p.advance()

	// Collect words until do/; /newline
	old := p.pushTerminators("do")
	var words []*Node
	for p.cur().Type != TEOF {
		if p.cur().Type == TNewline || p.cur().Type == TSemicolon {
			break
		}
		if p.cur().Type == TWord && p.cur().Val == "do" {
			break
		}
		if p.cur().Type == TWord {
			words = append(words, wordNode(p.cur()))
		} else {
			words = append(words, litNode(p.cur()))
		}
		p.advance()
	}
	p.restoreTerminators(old)

	for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
		p.pos++
	}
	if p.cur().Type == TWord && p.cur().Val == "do" {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'do' in for loop at pos %d", p.cur().Pos)
	}

	// Parse body until "done" (POSIX) or "end" (ish)
	p.skipNewlines()
	old = p.pushTerminators("done", "end")
	var bodyStmts []*Node
	for p.cur().Type != TEOF {
		if p.cur().Type == TWord && (p.cur().Val == "done" || p.cur().Val == "end") {
			break
		}
		stmt, err := p.parseList()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			bodyStmts = append(bodyStmts, stmt)
		}
		for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
			p.pos++
		}
	}
	p.restoreTerminators(old)
	if p.cur().Type == TWord && (p.cur().Val == "done" || p.cur().Val == "end") {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'done' or 'end' at pos %d", p.cur().Pos)
	}

	varNode := &Node{Kind: NWord, Tok: varTok}
	node.Children = append([]*Node{varNode}, words...)
	node.Clauses = []Clause{{Body: blockNode(bodyStmts)}}
	return node, nil
}

func (p *Parser) parseWhile() (*Node, error) {
	return p.parseWhileUntil(NWhile)
}

func (p *Parser) parseUntil() (*Node, error) {
	return p.parseWhileUntil(NUntil)
}

func (p *Parser) parseWhileUntil(kind NodeKind) (*Node, error) {
	p.advance() // skip "while"/"until"

	// Parse condition — "do" is terminator
	old := p.pushTerminators("do")
	cond, err := p.parseList()
	p.restoreTerminators(old)
	if err != nil {
		return nil, err
	}
	for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
		p.pos++
	}
	if p.cur().Type != TWord || p.cur().Val != "do" {
		return nil, fmt.Errorf("expected 'do' at pos %d", p.cur().Pos)
	}
	p.advance()

	// Parse body until "done" (POSIX) or "end" (ish)
	p.skipNewlines()
	old = p.pushTerminators("done", "end")
	var bodyStmts []*Node
	for p.cur().Type != TEOF {
		if p.cur().Type == TWord && (p.cur().Val == "done" || p.cur().Val == "end") {
			break
		}
		stmt, err := p.parseList()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			bodyStmts = append(bodyStmts, stmt)
		}
		for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
			p.pos++
		}
	}
	p.restoreTerminators(old)
	if p.cur().Type == TWord && (p.cur().Val == "done" || p.cur().Val == "end") {
		p.advance()
	} else if p.cur().Type == TEOF {
		return nil, fmt.Errorf("expected 'done' or 'end' at pos %d", p.cur().Pos)
	}

	return &Node{
		Kind:     kind,
		Children: []*Node{cond},
		Clauses:  []Clause{{Body: blockNode(bodyStmts)}},
	}, nil
}

func (p *Parser) parseCase() (*Node, error) {
	p.advance() // skip "case"
	node := &Node{Kind: NCase}

	// Parse word
	wordTok := p.advance()
	node.Children = []*Node{{Kind: NWord, Tok: wordTok}}

	for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
		p.pos++
	}
	if p.cur().Type != TWord || p.cur().Val != "in" {
		return nil, fmt.Errorf("expected 'in' in case at pos %d", p.cur().Pos)
	}
	p.advance()
	p.skipNewlines()

	// Parse patterns
	for p.cur().Type != TEOF {
		if p.cur().Type == TWord && p.cur().Val == "esac" {
			p.advance()
			break
		}
		// Skip optional leading ( before pattern (POSIX allows it)
		if p.cur().Type == TLParen {
			p.advance()
		}
		// Collect pattern tokens until ) — supports alternation with |
		var patterns []string
		for p.cur().Type != TRParen && p.cur().Type != TEOF {
			patterns = append(patterns, p.cur().Val)
			p.advance()
			if p.cur().Type == TPipe {
				p.advance() // skip |
			}
		}
		patVal := strings.Join(patterns, "|")
		pat := &Node{Kind: NLit, Tok: Token{Type: TWord, Val: patVal}}
		if p.cur().Type == TRParen {
			p.advance()
		}
		p.skipNewlines()

		old := p.pushTerminators("esac")
		var body []*Node
		for p.cur().Type != TEOF {
			if p.cur().Type == TWord && p.cur().Val == "esac" {
				break
			}
			if p.cur().Type == TSemicolon && p.peek().Type == TSemicolon {
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
			for p.cur().Type == TNewline {
				p.pos++
			}
		}
		p.restoreTerminators(old)
		p.skipNewlines()

		node.Clauses = append(node.Clauses, Clause{
			Pattern: pat,
			Body:    blockNode(body),
		})
	}

	if p.cur().Type == TEOF && len(node.Clauses) == 0 {
		return nil, fmt.Errorf("expected 'esac' at pos %d", p.cur().Pos)
	}

	return node, nil
}

// ish extensions

// POSIX function definition: name() { body; }
func (p *Parser) parsePosixFnDef() (*Node, error) {
	nameTok := p.advance() // consume name
	p.advance()            // skip (
	p.advance()            // skip )
	p.skipNewlines()

	// Expect { body; }
	if p.cur().Type != TLBrace {
		return nil, fmt.Errorf("expected '{' in function definition at pos %d", p.cur().Pos)
	}
	p.advance() // skip {
	p.skipNewlines()

	var bodyStmts []*Node
	for p.cur().Type != TEOF {
		if p.cur().Type == TRBrace {
			break
		}
		stmt, err := p.parseList()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			bodyStmts = append(bodyStmts, stmt)
		}
		for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
			p.pos++
		}
	}
	if p.cur().Type == TRBrace {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected '}' in function definition at pos %d", p.cur().Pos)
	}

	return &Node{
		Kind:     NFnDef,
		Tok:      nameTok,
		Children: []*Node{blockNode(bodyStmts)},
	}, nil
}

// fn name pattern... [when guard] do body end
func (p *Parser) parseIshFn() (*Node, error) {
	p.advance() // skip "fn"

	// Check for anonymous fn: fn do ... end  or  fn -> expr
	var nameTok Token
	isAnon := false
	if p.cur().Type == TWord && p.cur().Val == "do" {
		isAnon = true
		nameTok = Token{Type: TWord, Val: "<anon>"}
	} else if p.cur().Type == TArrow {
		// fn -> expr (single-line anonymous)
		p.advance() // skip ->
		body, err := p.parseList()
		if err != nil {
			return nil, err
		}
		return &Node{
			Kind: NIshFn,
			Tok:  Token{Type: TWord, Val: "<anon>"},
			Clauses: []Clause{{
				Body: body,
			}},
		}, nil
	} else {
		var err error
		nameTok, err = p.expect(TWord)
		if err != nil {
			return nil, fmt.Errorf("expected function name after 'fn' at pos %d", p.cur().Pos)
		}
	}

	// Parse parameter patterns until "when" or "do"
	var params []*Node
	if !isAnon {
		for p.cur().Type != TEOF {
			if p.cur().Type == TWord && (p.cur().Val == "when" || p.cur().Val == "do") {
				break
			}
			if p.cur().Type == TComma {
				p.advance() // skip commas between params
				continue
			}
			param, err := p.parsePattern()
			if err != nil {
				return nil, err
			}
			params = append(params, param)
		}
	}

	// Optional guard
	var guard *Node
	if p.cur().Type == TWord && p.cur().Val == "when" {
		p.advance()
		var err error
		guard, err = p.parseExpr(0)
		if err != nil {
			return nil, err
		}
	}

	// Expect "do"
	if p.cur().Type != TWord || p.cur().Val != "do" {
		return nil, fmt.Errorf("expected 'do' in fn definition at pos %d", p.cur().Pos)
	}
	p.advance()
	p.skipNewlines()

	old := p.pushTerminators("end")

	// Check if this is a multi-clause fn:
	//   fn name do
	//     pattern1 -> body1
	//     pattern2 -> body2
	//   end
	// Detected when there are no params AND the first thing looks like a clause (has ->)
	if len(params) == 0 && guard == nil && p.looksLikeClauseStart() {
		var clauses []Clause
		for p.cur().Type != TEOF {
			if p.cur().Type == TWord && p.cur().Val == "end" {
				break
			}
			// Parse clause: pattern [, pattern...] [when guard] -> body
			var clauseParams []*Node
			for p.cur().Type != TEOF {
				if p.cur().Type == TArrow {
					break
				}
				if p.cur().Type == TWord && p.cur().Val == "when" {
					break
				}
				if p.cur().Type == TComma {
					p.advance()
					continue
				}
				param, err := p.parsePattern()
				if err != nil {
					p.restoreTerminators(old)
					return nil, err
				}
				clauseParams = append(clauseParams, param)
			}
			// Optional guard
			var clauseGuard *Node
			if p.cur().Type == TWord && p.cur().Val == "when" {
				p.advance()
				var err error
				clauseGuard, err = p.parseExpr(0)
				if err != nil {
					p.restoreTerminators(old)
					return nil, err
				}
			}
			if p.cur().Type != TArrow {
				p.restoreTerminators(old)
				return nil, fmt.Errorf("expected '->' in fn clause at pos %d", p.cur().Pos)
			}
			p.advance()
			p.skipNewlines()

			body, err := p.parseClauseBody()
			if err != nil {
				p.restoreTerminators(old)
				return nil, err
			}
			for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
				p.pos++
			}

			fnParams := make([]Node, len(clauseParams))
			for i, cp := range clauseParams {
				fnParams[i] = *cp
			}
			clauses = append(clauses, Clause{
				Pattern: blockNode(clauseParams), // store params in Pattern for reference
				Guard:   clauseGuard,
				Body:    body,
			})
			// Also store as FnClause-compatible: we'll convert in eval
			_ = fnParams
		}
		p.restoreTerminators(old)
		if p.cur().Type == TWord && p.cur().Val == "end" {
			p.advance()
		} else {
			return nil, fmt.Errorf("expected 'end' at pos %d", p.cur().Pos)
		}

		// Convert Clause patterns to proper Children-based representation
		node := &Node{Kind: NIshFn, Tok: nameTok, Clauses: clauses}
		return node, nil
	}

	// Single-clause fn with explicit params
	var bodyStmts []*Node
	for p.cur().Type != TEOF {
		if p.cur().Type == TWord && p.cur().Val == "end" {
			break
		}
		stmt, err := p.parseList()
		if err != nil {
			p.restoreTerminators(old)
			return nil, err
		}
		if stmt != nil {
			bodyStmts = append(bodyStmts, stmt)
		}
		for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
			p.pos++
		}
	}
	p.restoreTerminators(old)
	if p.cur().Type == TWord && p.cur().Val == "end" {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'end' at pos %d", p.cur().Pos)
	}

	node := &Node{
		Kind: NIshFn,
		Tok:  nameTok,
		Clauses: []Clause{{
			Body:  blockNode(bodyStmts),
			Guard: guard,
		}},
		Children: params,
	}
	return node, nil
}

// match expr do clauses end
// parseClauseBody parses the body of a match/receive clause.
// A clause body is one or more statements, terminated by:
// - "end" keyword
// - another clause pattern (detected by lookahead for "->")
func (p *Parser) parseClauseBody() (*Node, error) {
	old := p.pushTerminators("end")
	var stmts []*Node
	for p.cur().Type != TEOF {
		// Stop at "end" or "after" (for receive timeout clauses)
		if p.cur().Type == TWord && (p.cur().Val == "end" || p.cur().Val == "after") {
			break
		}
		// Stop if this looks like the start of a new clause.
		// Heuristic: if we see a pattern token and somewhere ahead there's a ->
		// before a newline, this is a new clause, not part of the body.
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
		for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
			p.pos++
		}
	}
	p.restoreTerminators(old)
	return blockNode(stmts), nil
}

// looksLikeClauseStart checks if the current position starts a new clause
// (pattern -> body). Scans ahead for -> on the same line.
func (p *Parser) looksLikeClauseStart() bool {
	depth := 0
	for i := p.pos; i < len(p.tokens); i++ {
		t := p.tokens[i]
		if t.Type == TNewline || t.Type == TEOF {
			return false
		}
		switch t.Type {
		case TLParen, TLBracket, TLBrace:
			depth++
		case TRParen, TRBracket, TRBrace:
			depth--
		case TArrow:
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

// looksLikeListExpr checks if the current [ token starts a list expression
// rather than a [ test builtin. Scans ahead for , | or immediate ] inside brackets.
// looksLikeTupleExpr checks if the current { starts a tuple expression
// rather than a group command. Scans ahead for , inside braces at depth 1.
// Also returns true for empty {} and {atom, ...}.
func (p *Parser) looksLikeTupleExpr() bool {
	depth := 0
	for i := p.pos; i < len(p.tokens); i++ {
		t := p.tokens[i]
		switch t.Type {
		case TLBrace:
			depth++
		case TRBrace:
			depth--
			if depth == 0 {
				// Empty {} or single-element — check if it was just { }
				if i == p.pos+1 {
					return true // {}
				}
				return false // { ... } with no comma — group
			}
		case TComma:
			if depth == 1 {
				return true // found a comma — tuple
			}
		case TAtom:
			if depth == 1 && i == p.pos+1 {
				return true // { :atom ... — tuple
			}
		case TNewline, TEOF:
			return false
		}
	}
	return false
}

func (p *Parser) looksLikeListExpr() bool {
	depth := 0
	for i := p.pos; i < len(p.tokens); i++ {
		t := p.tokens[i]
		switch t.Type {
		case TLBracket:
			depth++
		case TRBracket:
			depth--
			if depth == 0 {
				// Empty list [] or single-element [x]
				// Check if preceded only by the opening [ (empty) or one token
				if i == p.pos+1 {
					return true // []
				}
				return false // [ ... ] with no , or | — could be test builtin
			}
		case TComma:
			if depth == 1 {
				return true
			}
		case TPipe:
			if depth == 1 {
				return true
			}
		case TNewline, TEOF:
			return false
		}
	}
	return false
}

func (p *Parser) parseIshMatchExpr() (*Node, error) {
	p.advance() // skip "match"

	subject, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
		p.pos++
	}
	if p.cur().Type != TWord || p.cur().Val != "do" {
		return nil, fmt.Errorf("expected 'do' after match expression at pos %d", p.cur().Pos)
	}
	p.advance()
	p.skipNewlines()

	old := p.pushTerminators("end")
	var clauses []Clause
	for p.cur().Type != TEOF {
		if p.cur().Type == TWord && p.cur().Val == "end" {
			break
		}

		pat, err := p.parsePattern()
		if err != nil {
			return nil, err
		}

		if p.cur().Type != TArrow {
			p.restoreTerminators(old)
			return nil, fmt.Errorf("expected '->' in match clause at pos %d", p.cur().Pos)
		}
		p.advance()
		p.skipNewlines()

		body, err := p.parseClauseBody()
		if err != nil {
			p.restoreTerminators(old)
			return nil, err
		}
		for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
			p.pos++
		}

		clauses = append(clauses, Clause{Pattern: pat, Body: body})
	}
	p.restoreTerminators(old)
	if p.cur().Type == TWord && p.cur().Val == "end" {
		p.advance()
	}

	return &Node{Kind: NIshMatch, Children: []*Node{subject}, Clauses: clauses}, nil
}

func (p *Parser) parseIshSpawn() (*Node, error) {
	p.advance() // skip "spawn"
	expr, err := p.parseCommand()
	if err != nil {
		return nil, err
	}
	return &Node{Kind: NIshSpawn, Children: []*Node{expr}}, nil
}

func (p *Parser) parseIshSpawnLink() (*Node, error) {
	p.advance() // skip "spawn_link"
	expr, err := p.parseCommand()
	if err != nil {
		return nil, err
	}
	return &Node{Kind: NIshSpawnLink, Children: []*Node{expr}}, nil
}

func (p *Parser) parseIshMonitor() (*Node, error) {
	p.advance() // skip "monitor"
	target, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &Node{Kind: NIshMonitor, Children: []*Node{target}}, nil
}

func (p *Parser) parseIshAwait() (*Node, error) {
	p.advance() // skip "await"
	target, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &Node{Kind: NIshAwait, Children: []*Node{target}}, nil
}

// supervise :strategy do
//   worker :name fn do ... end
//   worker :name fn do ... end
// end
func (p *Parser) parseIshSupervise() (*Node, error) {
	p.advance() // skip "supervise"
	strategy, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
		p.pos++
	}
	if p.cur().Type != TWord || p.cur().Val != "do" {
		return nil, fmt.Errorf("expected 'do' after supervise strategy at pos %d", p.cur().Pos)
	}
	p.advance()
	p.skipNewlines()

	// Parse worker declarations
	old := p.pushTerminators("end")
	var workers []*Node
	for p.cur().Type != TEOF {
		if p.cur().Type == TWord && p.cur().Val == "end" {
			break
		}
		if p.cur().Type == TWord && p.cur().Val == "worker" {
			p.advance() // skip "worker"
			// worker :name fn_expr
			workerName, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			fnExpr, err := p.parseCommand()
			if err != nil {
				return nil, err
			}
			workers = append(workers, &Node{
				Kind:     NCmd, // reuse NCmd to hold name + fn
				Children: []*Node{workerName, fnExpr},
			})
		} else {
			// Allow arbitrary statements inside supervise too
			stmt, err := p.parseList()
			if err != nil {
				return nil, err
			}
			if stmt != nil {
				workers = append(workers, stmt)
			}
		}
		for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
			p.pos++
		}
	}
	p.restoreTerminators(old)
	if p.cur().Type == TWord && p.cur().Val == "end" {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'end' in supervise at pos %d", p.cur().Pos)
	}

	node := &Node{Kind: NIshSupervise, Children: append([]*Node{strategy}, workers...)}
	return node, nil
}

func (p *Parser) parseIshSend() (*Node, error) {
	p.advance() // skip "send"
	target, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.cur().Type == TComma {
		p.advance()
	}
	msg, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &Node{Kind: NIshSend, Children: []*Node{target, msg}}, nil
}

func (p *Parser) parseIshReceive() (*Node, error) {
	p.advance() // skip "receive"
	for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
		p.pos++
	}
	if p.cur().Type != TWord || p.cur().Val != "do" {
		return nil, fmt.Errorf("expected 'do' after receive at pos %d", p.cur().Pos)
	}
	p.advance()
	p.skipNewlines()

	old := p.pushTerminators("end", "after")
	var clauses []Clause
	for p.cur().Type != TEOF {
		if p.cur().Type == TWord && (p.cur().Val == "end" || p.cur().Val == "after") {
			break
		}
		pat, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		if p.cur().Type != TArrow {
			p.restoreTerminators(old)
			return nil, fmt.Errorf("expected '->' in receive clause at pos %d", p.cur().Pos)
		}
		p.advance()
		p.skipNewlines()
		body, err := p.parseClauseBody()
		if err != nil {
			p.restoreTerminators(old)
			return nil, err
		}
		for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
			p.pos++
		}
		clauses = append(clauses, Clause{Pattern: pat, Body: body})
	}
	p.restoreTerminators(old)

	node := &Node{Kind: NIshReceive, Clauses: clauses}

	// Parse optional "after timeout -> body"
	if p.cur().Type == TWord && p.cur().Val == "after" {
		p.advance() // skip "after"
		timeoutExpr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		node.Timeout = timeoutExpr

		if p.cur().Type != TArrow {
			return nil, fmt.Errorf("expected '->' after timeout expression at pos %d", p.cur().Pos)
		}
		p.advance() // skip ->
		p.skipNewlines()

		old2 := p.pushTerminators("end")
		var bodyStmts []*Node
		for p.cur().Type != TEOF {
			if p.cur().Type == TWord && p.cur().Val == "end" {
				break
			}
			stmt, err := p.parseList()
			if err != nil {
				p.restoreTerminators(old2)
				return nil, err
			}
			if stmt != nil {
				bodyStmts = append(bodyStmts, stmt)
			}
			for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
				p.pos++
			}
		}
		p.restoreTerminators(old2)
		node.TimeoutBody = blockNode(bodyStmts)
	}

	if p.cur().Type == TWord && p.cur().Val == "end" {
		p.advance()
	}

	return node, nil
}

// try [do] body [rescue clauses] end
func (p *Parser) parseIshTry() (*Node, error) {
	p.advance() // skip "try"

	// skip optional separators
	for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
		p.pos++
	}
	// skip optional "do"
	if p.cur().Type == TWord && p.cur().Val == "do" {
		p.advance()
	}
	p.skipNewlines()

	// Parse body until "rescue" or "end"
	old := p.pushTerminators("rescue", "end")
	var bodyStmts []*Node
	for p.cur().Type != TEOF {
		if p.cur().Type == TWord && (p.cur().Val == "rescue" || p.cur().Val == "end") {
			break
		}
		stmt, err := p.parseList()
		if err != nil {
			p.restoreTerminators(old)
			return nil, err
		}
		if stmt != nil {
			bodyStmts = append(bodyStmts, stmt)
		}
		for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
			p.pos++
		}
	}
	p.restoreTerminators(old)

	node := &Node{Kind: NIshTry, Children: []*Node{blockNode(bodyStmts)}}

	// Parse rescue clauses (optional)
	if p.cur().Type == TWord && p.cur().Val == "rescue" {
		p.advance()
		p.skipNewlines()
		old = p.pushTerminators("end")
		var clauses []Clause
		for p.cur().Type != TEOF {
			if p.cur().Type == TWord && p.cur().Val == "end" {
				break
			}
			pat, err := p.parsePattern()
			if err != nil {
				p.restoreTerminators(old)
				return nil, err
			}
			if p.cur().Type != TArrow {
				p.restoreTerminators(old)
				return nil, fmt.Errorf("expected '->' in rescue clause at pos %d", p.cur().Pos)
			}
			p.advance()
			p.skipNewlines()
			body, err := p.parseClauseBody()
			if err != nil {
				p.restoreTerminators(old)
				return nil, err
			}
			for p.cur().Type == TSemicolon || p.cur().Type == TNewline {
				p.pos++
			}
			clauses = append(clauses, Clause{Pattern: pat, Body: body})
		}
		p.restoreTerminators(old)
		node.Clauses = clauses
	}

	if p.cur().Type == TWord && p.cur().Val == "end" {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'end' at pos %d", p.cur().Pos)
	}
	return node, nil
}

// parsePattern: atom | int | string | word | tuple | list | _
func (p *Parser) parsePattern() (*Node, error) {
	cur := p.cur()
	switch cur.Type {
	case TAtom:
		p.advance()
		return litNode(cur), nil
	case TInt:
		p.advance()
		return litNode(cur), nil
	case TString:
		p.advance()
		return litNode(cur), nil
	case TWord:
		if cur.Val == "_" {
			p.advance()
			return &Node{Kind: NWord, Tok: Token{Type: TWord, Val: "_"}}, nil
		}
		p.advance()
		return wordNode(cur), nil
	case TLBrace:
		return p.parseTupleExpr()
	case TLBracket:
		return p.parseListExpr()
	default:
		return nil, fmt.Errorf("unexpected token in pattern: %q at pos %d", cur.Val, cur.Pos)
	}
}

// Expression parsing (Pratt parser)

func (p *Parser) parseExpression() (*Node, error) {
	expr, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	// Check for = (ish match/bind) after an expression like {pattern} = expr
	if p.cur().Type == TEquals {
		p.advance()
		rhs, err := p.parsePipelineOrExpr()
		if err != nil {
			return nil, err
		}
		return &Node{Kind: NMatch, Children: []*Node{expr, rhs}}, nil
	}

	return expr, nil
}

func (p *Parser) parseExpr(minPrec int) (*Node, error) {
	p.depth++
	if p.depth > maxParseDepth {
		return nil, fmt.Errorf("expression too deeply nested (depth > %d)", maxParseDepth)
	}
	defer func() { p.depth-- }()

	left, err := p.parseAtom()
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
		left = &Node{Kind: NBinOp, Tok: op, Children: []*Node{left, right}}
	}

	// Map access: expr.field
	for p.cur().Type == TDot {
		p.advance()
		field := p.advance()
		left = &Node{Kind: NAccess, Tok: field, Children: []*Node{left}}
	}

	return left, nil
}

func (p *Parser) precedence(tt TokenType) int {
	switch tt {
	case TEq, TNe:
		return 1
	case TLe, TGe, TRedirIn, TRedirOut:
		return 2
	case TPlus, TMinus:
		return 3
	case TMul, TDiv:
		return 4
	default:
		return 0
	}
}

func (p *Parser) parseAtom() (*Node, error) {
	cur := p.cur()
	switch cur.Type {
	case TInt:
		p.advance()
		return litNode(cur), nil
	case TString:
		p.advance()
		return litNode(cur), nil
	case TAtom:
		p.advance()
		return litNode(cur), nil
	case TWord:
		p.advance()
		return wordNode(cur), nil
	case TLParen:
		p.advance()
		expr, err := p.parsePipelineOrExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TRParen); err != nil {
			return nil, err
		}
		return expr, nil
	case TLBrace:
		return p.parseTupleExpr()
	case TLBracket:
		return p.parseListExpr()
	case TPercent:
		if p.peek().Type == TLBrace {
			return p.parseMapExpr()
		}
		p.advance()
		return litNode(cur), nil
	case TBang:
		p.advance()
		operand, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		return &Node{Kind: NUnary, Tok: cur, Children: []*Node{operand}}, nil
	case TMinus:
		p.advance()
		operand, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		return &Node{Kind: NUnary, Tok: cur, Children: []*Node{operand}}, nil
	default:
		return nil, fmt.Errorf("unexpected token: %q at pos %d", cur.Val, cur.Pos)
	}
}

func (p *Parser) parseTupleExpr() (*Node, error) {
	p.advance() // skip {
	var elems []*Node
	p.skipNewlines()
	for p.cur().Type != TRBrace && p.cur().Type != TEOF {
		elem, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		elems = append(elems, elem)
		if p.cur().Type == TComma {
			p.advance()
			p.skipNewlines()
		}
	}
	if _, err := p.expect(TRBrace); err != nil {
		return nil, err
	}
	return &Node{Kind: NTuple, Children: elems}, nil
}

func (p *Parser) parseListExpr() (*Node, error) {
	p.advance() // skip [
	var elems []*Node
	var rest *Node
	p.skipNewlines()
	for p.cur().Type != TRBracket && p.cur().Type != TEOF {
		elem, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		elems = append(elems, elem)
		// Check for | (head|tail separator) before comma or ]
		if p.cur().Type == TPipe {
			p.advance() // skip |
			p.skipNewlines()
			tail, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			rest = tail
			break
		}
		if p.cur().Type == TComma {
			p.advance()
			p.skipNewlines()
		}
	}
	if _, err := p.expect(TRBracket); err != nil {
		return nil, err
	}
	return &Node{Kind: NList, Children: elems, Rest: rest}, nil
}

func (p *Parser) parseMapExpr() (*Node, error) {
	p.advance() // skip %
	p.advance() // skip {
	node := &Node{Kind: NMap}
	p.skipNewlines()
	for p.cur().Type != TRBrace && p.cur().Type != TEOF {
		key := p.advance() // key
		// Handle key: value syntax — key might include trailing ':'
		keyName := key.Val
		if strings.HasSuffix(keyName, ":") {
			keyName = keyName[:len(keyName)-1]
		} else if p.cur().Type == TWord && p.cur().Val == ":" {
			p.advance() // skip standalone :
		}
		key.Val = keyName
		val, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, &Node{Kind: NLit, Tok: key}, val)
		if p.cur().Type == TComma {
			p.advance()
			p.skipNewlines()
		}
	}
	if _, err := p.expect(TRBrace); err != nil {
		return nil, err
	}
	return node, nil
}
