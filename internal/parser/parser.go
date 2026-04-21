package parser

import (
	"fmt"
	"strings"

	"ish/internal/ast"
	"ish/internal/lexer"
)

// IsAssignment is no longer used — POSIX assignment detection is done by the
// parser using SpaceAfter adjacency (no whitespace between IDENT and EQUALS).
func IsAssignment(tok ast.Token) bool {
	return false
}

// parsePosixAssign handles FOO=bar (no whitespace around =).
// Also handles FOO= (empty value) and prefix assignments (FOO=bar cmd args).
func (p *Parser) parsePosixAssign() (*ast.Node, error) {
	// Collect one or more consecutive VAR=val assignments.
	var assigns []*ast.Node
	for {
		nameTok := p.advance() // consume IDENT
		eqTok := p.advance()   // consume EQUALS (adjacent to ident — no space)

		// Collect value as compound word if adjacent to =
		var val *ast.Node
		if !eqTok.SpaceAfter && p.cur().Type != ast.TNewline &&
			p.cur().Type != ast.TSemicolon && p.cur().Type != ast.TEOF {
			var err error
			val, err = p.parseCompoundWord()
			if err != nil {
				return nil, err
			}
		}

		node := &ast.Node{Kind: ast.NAssign, Tok: nameTok, Pos: nameTok.Pos}
		if val != nil {
			node.Children = []*ast.Node{val}
		}
		assigns = append(assigns, node)

		// If next is another IDENT with adjacent =, loop to collect it
		if p.cur().Type == ast.TIdent && !p.cur().SpaceAfter && p.peek(1).Type == ast.TEquals {
			continue
		}
		break
	}

	// Check for prefix assignment: FOO=bar [BAR=baz] cmd args
	// A command can start with an identifier, keyword (true/false), path, or dot.
	cur := p.cur()
	if cur.Type == ast.TIdent || cur.Type == ast.TDiv || cur.Type == ast.TDot || cur.Type == ast.TTilde ||
		cur.Type == ast.TTrue || cur.Type == ast.TFalse || cur.Type == ast.TNil ||
		(cur.Type.IsKeyword() && !isBlockEnd(cur.Type)) {
		cmd, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		if cmd != nil && cmd.Kind == ast.NCmd {
			cmd.Assigns = append(assigns, cmd.Assigns...)
			return cmd, nil
		}
	}

	// No command follows — return assignments as standalone statements
	if len(assigns) == 1 {
		return assigns[0], nil
	}
	return &ast.Node{Kind: ast.NBlock, Children: assigns, Pos: assigns[0].Pos}, nil
}

func isBlockEnd(tt ast.TokenType) bool {
	switch tt {
	case ast.TEnd, ast.TDone, ast.TFi, ast.TEsac, ast.TElse, ast.TElif,
		ast.TRescue, ast.TAfter, ast.TThen, ast.TDo:
		return true
	}
	return false
}

// isExprBinOp returns true for tokens that are unambiguously binary expression
// operators at statement level regardless of whitespace context.
// TGt and TLt are excluded (POSIX redirects).
// TMinus and TPlus are excluded here — they need whitespace analysis to
// distinguish binary ops (a - b) from flags (-n, +x). See dispatchIdent.
func isExprBinOp(tt ast.TokenType) bool {
	switch tt {
	case ast.TMul, ast.TDiv, ast.TPercent,
		ast.TEq, ast.TNe, ast.TLe, ast.TGe:
		return true
	}
	return false
}

// isTerminator returns true for tokens that end a statement.
// isTerminator returns true for tokens that unconditionally end a statement.
// Block-end keywords (done, end, fi, etc.) are NOT here — they only terminate
// when the parser is expecting them (checked via isBlockEndToken/terminators stack).
func isTerminator(tt ast.TokenType) bool {
	switch tt {
	case ast.TEOF, ast.TNewline, ast.TSemicolon,
		ast.TPipe, ast.TPipeStderr, ast.TPipeArrow,
		ast.TAnd, ast.TOr, ast.TAmpersand,
		ast.TRParen, ast.TRBrace, ast.TRBracket,
		ast.TComma, ast.TArrow:
		return true
	}
	return false
}

// isShellMetachar returns true for POSIX shell metacharacters that always
// break words regardless of whitespace. | & ; ( ) < > are metacharacters.
// These tokens stop compound word assembly even when adjacent.
func isShellMetachar(tt ast.TokenType) bool {
	switch tt {
	case ast.TPipe, ast.TPipeStderr, ast.TPipeArrow,
		ast.TAnd, ast.TOr, ast.TAmpersand,
		ast.TSemicolon,
		ast.TLParen, ast.TRParen,
		ast.TGt, ast.TLt, ast.TRedirAppend, ast.THeredoc, ast.THereString:
		return true
	}
	return false
}

// isValueStart returns true if the token type can begin a value expression.
func isValueStart(tt ast.TokenType) bool {
	switch tt {
	case ast.TIdent, ast.TInt, ast.TFloat, ast.TString, ast.TStringStart, ast.TAtom,
		ast.TNil, ast.TTrue, ast.TFalse,
		ast.TLParen, ast.TLBracket, ast.TLBrace, ast.TPercent, ast.TPercentLBrace,
		ast.TBackslash,
		ast.TDollar, ast.TDollarLParen, ast.TDollarDLParen, ast.TDollarLBrace,
		ast.TSpecialVar, ast.THashLBrace, ast.TDollarDQuote:
		return true
	}
	// Non-structural keywords are valid values in command argument position
	// (echo in, echo done). But block-structure keywords (do, end, then, fi, etc.)
	// are NOT value starts — they control block structure.
	if tt.IsKeyword() && !isBlockEnd(tt) {
		return true
	}
	return false
}

const maxParseDepth = 1000

type Parser struct {
	lex             *lexer.Lexer
	tokens          []ast.Token
	pos             int
	base            int
	resumePositions []int
	terminators     []ast.TokenType // block-ending token types
	depth           int
	committed       bool  // set when an expression commitment point is reached during tentative parsing
	exprContext     bool  // when true, > and < are comparisons (not redirects). Set by lambda bodies, fn guards, etc.
	savedPositions  []int // tentative parse save points — compact must not drop these
}

func Parse(l *lexer.Lexer) (*ast.Node, error) {
	p := &Parser{lex: l}
	return p.parseProgram()
}

// ParseWithCommands is kept for API compatibility during migration.
// The isCmd callback is ignored — the parser is now purely syntactic.
func ParseWithCommands(l *lexer.Lexer, isCmd func(string) bool) (*ast.Node, error) {
	p := &Parser{lex: l}
	return p.parseProgram()
}

func (p *Parser) compact() {
	if p.pos-p.base < 256 {
		return
	}
	// Don't compact past any tentative parse save point
	keep := p.pos - 1
	for _, saved := range p.savedPositions {
		if saved < keep {
			keep = saved
		}
	}
	if keep <= p.base {
		return
	}
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

// rawAt returns the token at absolute position n without skipping whitespace.
func (p *Parser) rawAt(n int) ast.Token {
	p.fillTo(n)
	idx := n - p.base
	if idx >= len(p.tokens) {
		return ast.Token{Type: ast.TEOF}
	}
	return p.tokens[idx]
}

// cur returns the current non-whitespace token, skipping any SpaceAfter.
// cur returns the token at the current position.
// No SpaceAfter in the stream. Whitespace info is on tok.SpaceAfter.
func (p *Parser) cur() ast.Token {
	return p.rawAt(p.pos)
}

// peek returns the token n positions ahead. Non-destructive.
func (p *Parser) peek(n int) ast.Token {
	return p.rawAt(p.pos + n)
}

// advance consumes the current token and returns it.
func (p *Parser) advance() ast.Token {
	t := p.rawAt(p.pos)
	p.pos++
	return t
}

// until returns true if the current token matches any of the given types.
func (p *Parser) until(types ...ast.TokenType) bool {
	tt := p.cur().Type
	for _, t := range types {
		if tt == t {
			return true
		}
	}
	return false
}

// tryParseExpr attempts to parse the current statement as an expression.
// If the parse hits a commitment point (infix operator, comma, parens, pipe),
// the expression interpretation is confirmed and the result is returned.
// If no commitment point is reached (just ident followed by ident), the parse
// is rolled back and the caller should try command invocation instead.
func (p *Parser) tryParseExpr() (*ast.Node, bool) {
	saved := p.pos
	savedCommitted := p.committed
	p.committed = false
	p.savedPositions = append(p.savedPositions, saved)

	node, err := p.parseExpression()
	if err != nil {
		p.savedPositions = p.savedPositions[:len(p.savedPositions)-1]
		p.pos = saved
		p.committed = savedCommitted
		return nil, false
	}

	if p.committed {
		p.savedPositions = p.savedPositions[:len(p.savedPositions)-1]
		return node, true
	}

	// No commitment point — rollback
	p.savedPositions = p.savedPositions[:len(p.savedPositions)-1]
	p.pos = saved
	p.committed = savedCommitted
	return nil, false
}

func (p *Parser) expect(tt ast.TokenType) (ast.Token, error) {
	t := p.cur()
	if t.Type != tt {
		if t.Type == ast.TEOF {
			return t, fmt.Errorf("unexpected end of input (expected %s)", tt.String())
		}
		return t, fmt.Errorf("expected %s, got %q at pos %d", tt.String(), t.Val, t.Pos)
	}
	return p.advance(), nil
}

func (p *Parser) match(tt ast.TokenType) bool {
	if p.cur().Type == tt { // cur() skips SpaceAfter
		p.advance()
		return true
	}
	return false
}

func (p *Parser) skipNewlines() {
	for p.cur().Type == ast.TNewline {
		p.advance()
	}
}

func (p *Parser) skipSeparators() {
	for {
		tt := p.cur().Type
		if tt == ast.TSemicolon || tt == ast.TNewline {
			p.advance()
		} else {
			break
		}
	}
}

func markTail(stmts []*ast.Node) {
	if len(stmts) > 0 {
		stmts[len(stmts)-1].Tail = true
	}
}

// --- Terminator management ---

func (p *Parser) pushTerminators(terms ...ast.TokenType) []ast.TokenType {
	old := p.terminators
	p.terminators = append(append([]ast.TokenType{}, old...), terms...)
	return old
}

func (p *Parser) restoreTerminators(old []ast.TokenType) {
	p.terminators = old
}

func (p *Parser) isBlockEndToken() bool {
	tt := p.cur().Type
	for _, t := range p.terminators {
		if tt == t {
			return true
		}
	}
	return false
}

// --- Block parsing helpers ---

func (p *Parser) ishBlock(body func() error) error {
	if _, err := p.expect(ast.TDo); err != nil {
		return err
	}
	p.skipNewlines()
	old := p.pushTerminators(ast.TEnd)
	// Inside ish blocks (do...end), > and < are comparison operators.
	savedExpr := p.exprContext
	p.exprContext = true
	err := body()
	p.exprContext = savedExpr
	p.restoreTerminators(old)
	if err != nil {
		return err
	}
	if _, err := p.expect(ast.TEnd); err != nil {
		return err
	}
	return nil
}

func (p *Parser) ishBlockWithSection(keyword ast.TokenType, parseMain func() error, parseCont func() error) error {
	return p.ishBlock(func() error {
		old := p.pushTerminators(keyword)
		err := parseMain()
		p.restoreTerminators(old)
		if err != nil {
			return err
		}
		if p.match(keyword) {
			p.skipNewlines()
			return parseCont()
		}
		return nil
	})
}

// --- Program structure ---

func (p *Parser) parseBlock() ([]*ast.Node, error) {
	var stmts []*ast.Node
	for !p.until(ast.TEOF) {
		if p.isBlockEndToken() {
			break
		}
		posBefore := p.pos
		stmt, err := p.parseList()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			stmts = append(stmts, stmt)
		} else if p.pos == posBefore {
			return nil, fmt.Errorf("unexpected token %q at position %d", p.cur().Val, p.cur().Pos)
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
		switch p.cur().Type {
		case ast.TAnd:
			p.advance()
			p.skipNewlines()
			right, err := p.parsePipeline()
			if err != nil {
				return nil, err
			}
			left = &ast.Node{Kind: ast.NAndList, Children: []*ast.Node{left, right}}
		case ast.TOr:
			p.advance()
			p.skipNewlines()
			right, err := p.parsePipeline()
			if err != nil {
				return nil, err
			}
			left = &ast.Node{Kind: ast.NOrList, Children: []*ast.Node{left, right}}
		case ast.TAmpersand:
			p.advance()
			left = &ast.Node{Kind: ast.NBg, Children: []*ast.Node{left}}
		default:
			return left, nil
		}
	}
}

func (p *Parser) parsePipeline() (*ast.Node, error) {
	left, err := p.parseStmtWithOps()
	if err != nil || left == nil {
		return left, err
	}

	for {
		switch p.cur().Type {
		case ast.TPipe, ast.TPipeStderr:
			tok := p.advance()
			p.skipNewlines()
			right, err := p.parseStmtWithOps()
			if err != nil {
				return nil, err
			}
			if right == nil {
				return nil, fmt.Errorf("expected command after '%s'", tok.Val)
			}
			left = &ast.Node{Kind: ast.NPipe, Tok: tok, Children: []*ast.Node{left, right}}
		case ast.TPipeArrow:
			p.advance()
			p.skipNewlines()
			right, err := p.parseStmtWithOps()
			if err != nil {
				return nil, err
			}
			if right == nil {
				return nil, fmt.Errorf("expected expression after '|>'")
			}
			left = &ast.Node{Kind: ast.NPipeFn, Children: []*ast.Node{left, right}}
		default:
			return left, nil
		}
	}
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

// --- Statement dispatch ---

func (p *Parser) parseStmt() (*ast.Node, error) {
	cur := p.cur()

	switch cur.Type {
	case ast.TEOF:
		return nil, nil

	// POSIX : (colon) — no-op command
	case ast.TColon:
		tok := p.advance()
		return &ast.Node{Kind: ast.NCmd, Children: []*ast.Node{
			ast.IdentNode(ast.Token{Type: ast.TIdent, Val: ":", Pos: tok.Pos}),
		}, Pos: tok.Pos}, nil

	// Keywords dispatch directly by token type — no string inspection
	case ast.TIf:
		return p.parseIf()
	case ast.TFor:
		return p.parseFor()
	case ast.TWhile:
		return p.parseWhile()
	case ast.TUntil:
		return p.parseUntil()
	case ast.TCase:
		return p.parseCase()
	case ast.TFn:
		if p.exprContext {
			return p.parseIshFnAnon() // In expression context, fn is always anonymous
		}
		return p.parseIshFn()
	case ast.TDefModule:
		return p.parseDefModule()
	case ast.TUse:
		return p.parseUse()
	case ast.TMatch:
		return p.parseIshMatchExpr()
	case ast.TSpawn:
		return p.parseIshSpawn()
	case ast.TSpawnLink:
		return p.parseIshSpawnLink()
	case ast.TSend:
		return p.parseIshSend()
	case ast.TMonitor:
		return p.parseIshMonitor()
	case ast.TAwait:
		return p.parseIshAwait()
	case ast.TSupervise:
		return p.parseIshSupervise()
	case ast.TReceive:
		return p.parseIshReceive()
	case ast.TTry:
		return p.parseIshTry()

	// true/false/nil at statement start: POSIX commands (set exit code)
	// unless followed by an operator (then expression)
	case ast.TTrue, ast.TFalse, ast.TNil:
		next := p.peek(1)
		if isExprBinOp(next.Type) || next.Type == ast.TDot || next.Type == ast.TEquals {
			return p.parseExpression()
		}
		// Standalone or followed by args → command
		tok := p.advance()
		cmdName := ast.IdentNode(tok) // preserve original token type (TTrue/TFalse/TNil)
		if isTerminator(p.cur().Type) || p.isBlockEndToken() {
			return &ast.Node{Kind: ast.NCmd, Children: []*ast.Node{cmdName}, Pos: tok.Pos}, nil
		}
		return p.parseInvocationWithName(cmdName)

	// Other literals → always expression
	case ast.TInt, ast.TFloat, ast.TString, ast.TStringStart, ast.TAtom:
		return p.parseExpression()

	// Unary operators
	case ast.TBang, ast.TMinus:
		return p.parseExpression()

	// Lambda
	case ast.TBackslash:
		return p.parseLambda()

	// Map literal %{...}
	case ast.TPercentLBrace:
		return p.parseExpression()

	case ast.TPercent:
		return nil, fmt.Errorf("unexpected '%%' at pos %d", cur.Pos)

	// Expansion tokens → expression
	case ast.TDollar, ast.TDollarLParen, ast.TDollarDLParen,
		ast.TDollarLBrace, ast.TSpecialVar, ast.THashLBrace, ast.TDollarDQuote:
		return p.parseExpression()

	// Brackets — disambiguate by lookahead
	case ast.TLParen:
		if p.exprContext {
			return p.parseExpression() // In expression context, ( starts grouped expr
		}
		return p.parseParenStart()
	case ast.TLBrace:
		return p.parseBraceStart()
	case ast.TLBracket:
		return p.parseBracketStart()

	// Capture: &name
	case ast.TAmpersand:
		if p.peek(1).Type == ast.TIdent { // adjacent: &name capture
			p.advance()
			tok := p.advance()
			return &ast.Node{Kind: ast.NCapture, Tok: tok}, nil
		}

	// Identifier at statement start — the key decision point
	case ast.TIdent:
		return p.dispatchIdent()

	// TDot at statement start — source command or path
	case ast.TDot:
		return p.parseDotStart()

	// TDiv at statement start — absolute path command
	case ast.TDiv:
		return p.parsePathInvocation()

	// TTilde at statement start — home-relative path command
	case ast.TTilde:
		return p.parseTildeInvocation()
	}

	// Expression is the fallthrough
	if isValueStart(cur.Type) {
		return p.parseExpression()
	}

	return nil, nil
}

// dispatchIdent handles a bare TIdent at statement start.
// Uses SYNTAX and SpaceAfter to determine: POSIX assign, binding, expression, or invocation.
func (p *Parser) dispatchIdent() (*ast.Node, error) {
	ident := p.cur()       // the identifier (cur skips WS)
	next := p.peek(1)      // next non-WS token after ident

	// Adjacency: does the ident have NO space after it?
	adjacent := !ident.SpaceAfter

	if adjacent {
		// POSIX assignment: FOO=bar — TEquals adjacent
		if next.Type == ast.TEquals {
			return p.parsePosixAssign()
		}
		// Adjacent dot: a.b → module-qualified name.
		// Try expression first (handles NCall + binary ops like Fib.calc (n-1) + Fib.calc (n-2)).
		// If committed (operator found), keep it. Otherwise fall back to NCmd for commas.
		if next.Type == ast.TDot {
			if node, ok := p.tryParseExpr(); ok {
				return node, nil
			}
			// Tentative parse didn't commit — build dot chain for NCmd
			tok := p.advance()
			nameNode := ast.IdentNode(tok)
			for p.cur().Type == ast.TDot {
				p.advance()
				field := p.cur()
				if field.Type == ast.TIdent || field.Type.IsKeyword() {
					field = p.advance()
					field.Type = ast.TIdent
					nameNode = &ast.Node{Kind: ast.NAccess, Tok: field, Children: []*ast.Node{nameNode}}
				} else {
					break
				}
			}
			// Adjacent parens: Module.func(a, b) → NCall
			if p.cur().Type == ast.TLParen && !p.rawAt(p.pos-1).SpaceAfter {
				call := &ast.Node{Kind: ast.NCall, Children: []*ast.Node{nameNode}}
				p.advance()
				for !p.until(ast.TRParen, ast.TEOF) {
					arg, err := p.parseExpr(0)
					if err != nil {
						return nil, err
					}
					call.Children = append(call.Children, arg)
					if p.cur().Type == ast.TComma {
						p.advance()
					}
				}
				if _, err := p.expect(ast.TRParen); err != nil {
					return nil, err
				}
				return call, nil
			}
			if isTerminator(p.cur().Type) || p.isBlockEndToken() {
				return nameNode, nil
			}
			return p.parseInvocationWithName(nameNode)
		}
		// Adjacent paren: name() — POSIX fn def, or name(args) — function call
		if next.Type == ast.TLParen {
			if p.peek(2).Type == ast.TRParen {
				return p.parsePosixFnDef()
			}
			// Adjacent paren call: func(a, b) → expression
			return p.parseExpression()
		}
		// Other adjacent token → compound word / invocation
		return p.parseInvocation()
	}

	// There IS whitespace after the ident. next is the token after the space.

	// ish binding: x = expr
	if next.Type == ast.TEquals {
		return p.parseIshBind()
	}

	// POSIX fn def with space: name ()
	if next.Type == ast.TLParen && p.peek(2).Type == ast.TRParen {
		return p.parsePosixFnDef()
	}

	// Operators with SpaceAfter: spaced = binary op (expression).
	// Adjacent = command arg (flag, path, glob).
	if isExprBinOp(next.Type) || next.Type == ast.TPlus || next.Type == ast.TMinus {
		if next.SpaceAfter {
			return p.parseExpression()
		}
		return p.parseInvocation() // -n, /tmp, *glob, +x
	}

	// Redirect tokens → command (redirect context).
	// But when in expression context (lambda body, fn guard), bare > and <
	// are comparison operators — skip the redirect check and let expression
	// parsing handle them.
	if next.Type == ast.TRedirAppend || next.Type == ast.THeredoc || next.Type == ast.THereString {
		return p.parseInvocation()
	}
	if (next.Type == ast.TGt || next.Type == ast.TLt) && !p.exprContext {
		return p.parseInvocation()
	}

	// Dot after space → path arg (.hidden, .., ./) → command
	if next.Type == ast.TDot {
		return p.parseInvocation()
	}

	// Terminator or block-end → standalone (zero-arg command or value)
	if isTerminator(next.Type) || p.isBlockEndToken() {
		tok := p.advance()
		if p.exprContext {
			return ast.IdentNode(tok), nil
		}
		return &ast.Node{Kind: ast.NCmd, Children: []*ast.Node{ast.IdentNode(tok)}, Pos: tok.Pos}, nil
	}

	// Ambiguous: identifier followed by a value. Could be a function call
	// (countdown n - 1) or a command (echo hello world).
	// Try expression first. If we hit a commitment point (operator, comma,
	// parens, pipe), keep it. Otherwise rollback and parse as command.
	if node, ok := p.tryParseExpr(); ok {
		return node, nil
	}
	return p.parseInvocation()
}

// --- Bracket disambiguation (parser-level, no ExprHint) ---

func (p *Parser) parseParenStart() (*ast.Node, error) {
	// ( at statement start → subshell
	return p.parseSubshell()
}

func (p *Parser) parseBraceStart() (*ast.Node, error) {
	if p.looksLikeTuple() {
		return p.parseExpression() // handles trailing = for pattern match
	}
	return p.parseGroup()
}

func (p *Parser) parseBracketStart() (*ast.Node, error) {
	if p.looksLikeList() {
		return p.parseExpression() // handles trailing = for pattern match
	}
	// In exprContext, [ is a list unless it looks like a POSIX test command.
	// Test commands have patterns like [ -n x ], [ $x = y ], etc.
	// A single value inside [ ] with no commas/pipes is ambiguous —
	// in exprContext default to list.
	if p.exprContext && !p.looksLikeTest() {
		return p.parseExpression()
	}
	// Test builtin: [ -n x ]
	// Parse as command with [ as name. Collect args until ] (which is the last arg).
	pos := p.cur().Pos
	p.advance() // consume [
	nameNode := ast.IdentNode(ast.Token{Type: ast.TIdent, Val: "[", Pos: pos})
	cmd := &ast.Node{Kind: ast.NCmd, Children: []*ast.Node{nameNode}, Pos: pos}
	for {
		cur := p.cur()
		if cur.Type == ast.TEOF || cur.Type == ast.TNewline || cur.Type == ast.TSemicolon {
			break
		}
		if cur.Type == ast.TRBracket {
			p.advance() // consume ] as the closing marker
			break
		}
		arg, err := p.parseCompoundWord()
		if err != nil {
			return nil, err
		}
		if arg != nil {
			cmd.Children = append(cmd.Children, arg)
		}
	}
	return cmd, nil
}

// looksLikeTuple peeks inside { } to check for commas or leading atoms.
func (p *Parser) looksLikeTuple() bool {
	depth := 0
	first := true
	for i := p.pos; ; i++ {
		p.fillTo(i)
		idx := i - p.base
		if idx >= len(p.tokens) {
			return false
		}
		t := p.tokens[idx]
		switch t.Type {
		case ast.TLBrace:
			depth++
			first = depth == 1
		case ast.TRBrace:
			depth--
			if depth == 0 {
				return i == p.pos+1 // empty {} is tuple
			}
		case ast.TComma:
			if depth == 1 {
				return true
			}
		case ast.TAtom:
			if depth == 1 && first {
				return true
			}
			first = false
		case ast.TNewline:
			return false
		case ast.TEOF:
			return false
		default:
			first = false
		}
	}
}

// looksLikeTest peeks inside [ ] for POSIX test patterns (flags like -n, operators like =).
func (p *Parser) looksLikeTest() bool {
	// Scan tokens between [ and ] looking for test-like patterns.
	for i := p.pos + 1; ; i++ {
		p.fillTo(i)
		idx := i - p.base
		if idx >= len(p.tokens) {
			return false
		}
		t := p.tokens[idx]
		if t.Type == ast.TRBracket || t.Type == ast.TEOF || t.Type == ast.TNewline {
			return false
		}
		// Flag like -n, -f, -d at start → test command
		if t.Type == ast.TMinus && i == p.pos+1 {
			return true
		}
		// = or != between values → test command
		if t.Type == ast.TEquals || t.Type == ast.TNe || t.Type == ast.TBang {
			return true
		}
		// -gt, -lt, -eq, -ne, -ge, -le, -nt, -ot, -ef, -a, -o → test operators
		// These appear as TMinus followed by TIdent (adjacent, no space).
		if t.Type == ast.TMinus {
			p.fillTo(i + 1)
			idx2 := i + 1 - p.base
			if idx2 < len(p.tokens) {
				next := p.tokens[idx2]
				if next.Type == ast.TIdent {
					switch next.Val {
					case "gt", "lt", "eq", "ne", "ge", "le", "nt", "ot", "ef", "a", "o",
						"n", "z", "f", "d", "e", "s", "r", "w", "x", "L", "p", "S", "t":
						return true
					}
				}
			}
		}
	}
}

// looksLikeList peeks inside [ ] to check for commas or |.
func (p *Parser) looksLikeList() bool {
	depth := 0
	for i := p.pos; ; i++ {
		p.fillTo(i)
		idx := i - p.base
		if idx >= len(p.tokens) {
			return false
		}
		t := p.tokens[idx]
		switch t.Type {
		case ast.TLBracket:
			depth++
		case ast.TRBracket:
			depth--
			if depth == 0 {
				return i == p.pos+1 // empty [] is list
			}
		case ast.TComma:
			if depth == 1 {
				return true
			}
		case ast.TPipe:
			if depth == 1 {
				return true // [h | t] cons
			}
		case ast.TNewline:
			return false
		case ast.TEOF:
			return false
		}
	}
}

// --- Invocation parsing (commands / function calls) ---

// parseDotStart handles TDot at statement start: source command or path.
func (p *Parser) parseDotStart() (*ast.Node, error) {
	pos := p.cur().Pos
	// Assemble the callee as a compound word starting with "."
	nameNode, err := p.parseCompoundWord()
	if err != nil {
		return nil, err
	}
	if nameNode == nil {
		nameNode = &ast.Node{Kind: ast.NPath, Tok: ast.Token{Type: ast.TIdent, Val: ".", Pos: pos}}
	}

	if isTerminator(p.cur().Type) {
		return &ast.Node{Kind: ast.NCmd, Children: []*ast.Node{nameNode}, Pos: pos}, nil
	}

	return p.parseInvocationWithName(nameNode)
}

// parsePathInvocation handles TDiv at statement start (absolute path command).
func (p *Parser) parsePathInvocation() (*ast.Node, error) {
	pos := p.cur().Pos
	nameNode, err := p.parseCompoundWord()
	if err != nil {
		return nil, err
	}
	return p.parseInvocationWithName(&ast.Node{Kind: ast.NPath, Tok: ast.Token{Type: ast.TIdent, Val: nodeToString(nameNode), Pos: pos}})
}

// parseTildeInvocation handles TTilde at statement start (~/bin/script).
func (p *Parser) parseTildeInvocation() (*ast.Node, error) {
	pos := p.cur().Pos
	nameNode, err := p.parseCompoundWord()
	if err != nil {
		return nil, err
	}
	return p.parseInvocationWithName(&ast.Node{Kind: ast.NPath, Tok: ast.Token{Type: ast.TIdent, Val: nodeToString(nameNode), Pos: pos}})
}

// parseInvocation parses a command invocation starting with TIdent at current position.
func (p *Parser) parseInvocation() (*ast.Node, error) {
	nameTok := p.advance()
	nameNode := ast.IdentNode(nameTok)
	return p.parseInvocationWithName(nameNode)
}

// parseInvocationFrom creates a command from a synthesized name token (for [ builtin).
func (p *Parser) parseInvocationFrom(nameTok ast.Token) (*ast.Node, error) {
	p.advance() // consume the bracket
	nameNode := ast.IdentNode(nameTok)
	return p.parseInvocationWithName(nameNode)
}

// parseInvocationWithName parses command arguments after the command name is known.
// Each argument is a compound word: adjacent tokens with no SpaceAfter between them.
func (p *Parser) parseInvocationWithName(nameNode *ast.Node) (*ast.Node, error) {
	cmd := &ast.Node{Kind: ast.NCmd, Children: []*ast.Node{nameNode}, Pos: nameNode.Pos}

	for {
		cur := p.cur()
		// Commas separate args in NCmd (e.g. `add 3, 4`, `Map.put m, k, v`)
		if cur.Type == ast.TComma {
			p.advance()
			continue
		}
		if isTerminator(cur.Type) {
			break
		}
		if p.isBlockEndToken() {
			break
		}

		// Parenthesized expression in command args: always evaluated as expression
		if cur.Type == ast.TLParen {
			p.advance()
			expr, err := p.parsePipeline()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(ast.TRParen); err != nil {
				return nil, err
			}
			cmd.Children = append(cmd.Children, expr)
			continue
		}

		// Check for redirections BEFORE compound word parsing.
		// Adjacency matters: 2>file is fd redirect, 2 > file is "2" then redirect.
		if rs, ok, err := p.tryParseRedir(); ok {
			if err != nil {
				return nil, err
			}
			cmd.Redirs = append(cmd.Redirs, rs...)
			continue
		}

		// Expression-valued args: lambda, data structures, dotted access, keywords
		if cur.Type == ast.TBackslash || cur.Type == ast.TLBracket ||
			cur.Type == ast.TLBrace || cur.Type == ast.TPercentLBrace ||
			cur.Type == ast.TNil || cur.Type == ast.TTrue || cur.Type == ast.TFalse ||
			(cur.Type == ast.TFn && p.exprContext) ||
			(cur.Type == ast.TIdent && !cur.SpaceAfter && p.peek(1).Type == ast.TDot) {
			expr, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			cmd.Children = append(cmd.Children, expr)
			continue
		}

		// Parse one compound word (one argument)
		arg, err := p.parseCompoundWord()
		if err != nil {
			return nil, err
		}
		if arg != nil {
			cmd.Children = append(cmd.Children, arg)
		}
	}

	return cmd, nil
}

// --- Compound word assembly ---

// parseCompoundWord collects adjacent tokens (no SpaceAfter between them)
// into a single argument node. This is the parser equivalent of the old lexWord:
// instead of one opaque string, it produces a structured AST node.
//
// If the compound word has one part, returns that part directly.
// If it has multiple parts, returns NArg with children.
func (p *Parser) parseCompoundWord() (*ast.Node, error) {
	var parts []*ast.Node

	// Use cur() to find the first token (skips whitespace like all parser functions)
	first := p.cur()
	if first.Type == ast.TEOF || isTerminator(first.Type) || isShellMetachar(first.Type) {
		return nil, nil
	}

	// Parse parts. After each part, check if the token just before p.pos
	// had SpaceAfter — if so, the compound word ends.
	for {
		cur := p.cur()
		if cur.Type == ast.TEOF || isTerminator(cur.Type) || isShellMetachar(cur.Type) {
			break
		}

		posBefore := p.pos
		part, err := p.parseWordPart()
		if err != nil {
			return nil, err
		}
		if part != nil {
			parts = append(parts, part)
		}

		// Check if the last consumed token had space after it → word boundary
		if p.pos > posBefore {
			lastConsumed := p.rawAt(p.pos - 1)
			if lastConsumed.SpaceAfter {
				break
			}
		}
	}

	if len(parts) == 0 {
		return nil, nil
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	// Multiple adjacent parts → compound word
	return &ast.Node{Kind: ast.NArg, Children: parts, Pos: parts[0].Pos}, nil
}

// parseWordPart parses a single part of a compound word at the raw token level.
// Does NOT skip whitespace — the caller (parseCompoundWord) handles boundaries.
func (p *Parser) parseWordPart() (*ast.Node, error) {
	raw := p.rawAt(p.pos)

	switch raw.Type {
	case ast.TIdent:
		p.pos++
		return ast.IdentNode(raw), nil

	case ast.TDollar:
		// $var — variable reference
		p.pos++ // consume $
		next := p.rawAt(p.pos)
		if next.Type == ast.TIdent {
			p.pos++ // consume var name
			node := &ast.Node{Kind: ast.NVarRef, Tok: next, Pos: raw.Pos}
			// Check for $var.field (adjacent dot access)
			for p.rawAt(p.pos).Type == ast.TDot {
				p.pos++ // consume .
				field := p.rawAt(p.pos)
				if field.Type == ast.TIdent || field.Type.IsKeyword() {
					field.Type = ast.TIdent // normalize keyword to ident for field name
					p.pos++ // consume field
					node = &ast.Node{Kind: ast.NAccess, Tok: field, Children: []*ast.Node{node}}
				} else {
					// Dot without field — put it back and stop
					p.pos--
					break
				}
			}
			return node, nil
		}
		// Bare $ — literal
		return ast.LitNode(ast.Token{Type: ast.TString, Val: "$", Pos: raw.Pos}), nil

	case ast.TSpecialVar:
		p.pos++
		return &ast.Node{Kind: ast.NVarRef, Tok: raw, Pos: raw.Pos}, nil

	case ast.TDollarLParen:
		return p.parseCmdSub()

	case ast.TDollarDLParen:
		return p.parseArithSub()

	case ast.TDollarLBrace:
		return p.parseParamExpand()

	case ast.THashLBrace:
		return p.parseInterpolation()

	case ast.TStringStart:
		return p.parseInterpString()

	case ast.TString:
		p.pos++
		return ast.LitNode(raw), nil

	case ast.TDollarDQuote:
		return p.parseDollarString()

	case ast.TInt, ast.TFloat:
		p.pos++
		return ast.LitNode(raw), nil

	case ast.TAtom:
		p.pos++
		return ast.LitNode(raw), nil

	case ast.TBackslash:
		// Escape: \ followed by next token = literal escaped character
		p.pos++ // consume backslash
		next := p.rawAt(p.pos)
		if next.Type != ast.TEOF && next.Type != ast.TNewline {
			p.pos++
			// The escaped character — even if it's whitespace
			return ast.LitNode(ast.Token{Type: ast.TString, Val: next.Val, Pos: next.Pos}), nil
		}
		return ast.LitNode(ast.Token{Type: ast.TString, Val: "\\", Pos: raw.Pos}), nil

	// Operator tokens that are literal characters in compound words
	case ast.TDot:
		p.pos++
		return ast.LitNode(ast.Token{Type: ast.TString, Val: ".", Pos: raw.Pos}), nil
	case ast.TDiv:
		p.pos++
		return ast.LitNode(ast.Token{Type: ast.TString, Val: "/", Pos: raw.Pos}), nil
	case ast.TMinus:
		p.pos++
		return ast.LitNode(ast.Token{Type: ast.TString, Val: "-", Pos: raw.Pos}), nil
	case ast.TPlus:
		p.pos++
		return ast.LitNode(ast.Token{Type: ast.TString, Val: "+", Pos: raw.Pos}), nil
	case ast.TTilde:
		p.pos++
		return ast.LitNode(ast.Token{Type: ast.TString, Val: "~", Pos: raw.Pos}), nil
	case ast.TAt:
		p.pos++
		return ast.LitNode(ast.Token{Type: ast.TString, Val: "@", Pos: raw.Pos}), nil
	case ast.TColon:
		p.pos++
		return ast.LitNode(ast.Token{Type: ast.TString, Val: ":", Pos: raw.Pos}), nil
	case ast.THash:
		p.pos++
		return ast.LitNode(ast.Token{Type: ast.TString, Val: "#", Pos: raw.Pos}), nil
	case ast.TEquals:
		p.pos++
		return ast.LitNode(ast.Token{Type: ast.TString, Val: "=", Pos: raw.Pos}), nil
	case ast.TMul:
		p.pos++
		return ast.LitNode(ast.Token{Type: ast.TString, Val: "*", Pos: raw.Pos}), nil
	case ast.TPercent:
		p.pos++
		return ast.LitNode(ast.Token{Type: ast.TString, Val: "%", Pos: raw.Pos}), nil
	// TGt, TLt, TAmpersand, TPipe, TLParen, TRParen, TSemicolon are
	// POSIX shell metacharacters — they break compound words and are
	// NOT valid word parts. They're caught by the break condition above.

	default:
		// Keyword tokens in compound word position are literal strings
		if raw.Type.IsKeyword() {
			p.pos++
			return ast.LitNode(ast.Token{Type: ast.TString, Val: raw.Val, Pos: raw.Pos}), nil
		}
		// Unknown — consume as literal
		p.pos++
		return ast.LitNode(ast.Token{Type: ast.TString, Val: raw.Val, Pos: raw.Pos}), nil
	}
}

// nodeToString extracts a string representation from a simple node (for path assembly).
func nodeToString(n *ast.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case ast.NLit, ast.NIdent, ast.NPath, ast.NFlag:
		if n.Tok.Type == ast.TAtom {
			return ":" + n.Tok.Val
		}
		return n.Tok.Val
	case ast.NArg:
		var b strings.Builder
		for _, c := range n.Children {
			b.WriteString(nodeToString(c))
		}
		return b.String()
	default:
		return n.Tok.Val
	}
}

// --- Redirection parsing ---

func (p *Parser) tryParseRedir() ([]ast.Redir, bool, error) {
	cur := p.cur()

	// &> file — redirect both stdout and stderr
	if cur.Type == ast.TAmpersand && (p.peek(1).Type == ast.TGt || p.peek(1).Type == ast.TRedirAppend) {
		p.advance()
		r, err := p.parseRedir()
		if err != nil {
			return nil, true, err
		}
		r.Fd = 1
		return []ast.Redir{r, {Op: r.Op, Fd: 2, TargetNode: r.TargetNode, Quoted: r.Quoted}}, true, nil
	}

	// > >> < << <<<
	if cur.Type == ast.TGt || cur.Type == ast.TRedirAppend ||
		cur.Type == ast.TLt || cur.Type == ast.THeredoc || cur.Type == ast.THereString {
		r, err := p.parseRedir()
		if err != nil {
			return nil, true, err
		}
		return []ast.Redir{r}, true, nil
	}

	// fd> fd>> fd< — ONLY when TInt is ADJACENT to the redirect token (no whitespace).
	// "2>file" is fd redirect. "2 > file" is argument "2" followed by redirect.
	if cur.Type == ast.TInt {
		// fd redirect: TInt ADJACENT to redirect token (2>file, not 2 > file)
		rawAfter := p.peek(1)
		if rawAfter.Type == ast.TGt || rawAfter.Type == ast.TRedirAppend || rawAfter.Type == ast.TLt {
			fd := 0
			fmt.Sscanf(cur.Val, "%d", &fd)
			p.advance() // consume TInt
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

func (p *Parser) parseRedir() (ast.Redir, error) {
	r := ast.Redir{Op: p.cur().Type}
	switch r.Op {
	case ast.TGt, ast.TRedirAppend:
		r.Fd = 1
	case ast.TLt, ast.THeredoc, ast.THereString:
		r.Fd = 0
	}
	p.advance()

	if p.cur().Type == ast.TAmpersand {
		p.advance()
		if p.cur().Type == ast.TInt || p.cur().Type == ast.TIdent {
			tok := p.advance()
			r.TargetNode = ast.LitNode(ast.Token{Type: ast.TString, Val: "&" + tok.Val, Pos: tok.Pos})
			return r, nil
		}
		return r, fmt.Errorf("expected fd number after >& at pos %d", p.cur().Pos)
	}

	// Target is a compound word (e.g., /dev/null, $file, ${dir}/out)
	target, err := p.parseCompoundWord()
	if err != nil {
		return r, err
	}
	if target == nil {
		return r, fmt.Errorf("expected filename after redirection at pos %d", p.cur().Pos)
	}
	r.TargetNode = target
	return r, nil
}

// --- Expansion parsing ---

func (p *Parser) parseVarRef() (*ast.Node, error) {
	p.advance() // consume TDollar
	if p.cur().Type == ast.TIdent {
		nameTok := p.advance()
		node := &ast.Node{Kind: ast.NVarRef, Tok: nameTok, Pos: nameTok.Pos}
		// Handle $var.field access
		for p.cur().Type == ast.TDot && p.peek(1).Type == ast.TIdent {
			p.advance() // consume .
			field := p.advance()
			node = &ast.Node{Kind: ast.NAccess, Tok: field, Children: []*ast.Node{node}}
		}
		return node, nil
	}
	// Bare $ — return as-is
	return ast.LitNode(ast.Token{Type: ast.TString, Val: "$"}), nil
}

func (p *Parser) parseCmdSub() (*ast.Node, error) {
	pos := p.cur().Pos
	p.advance() // consume $(
	// Save commitment state — operators inside $() don't affect outer context
	savedCommitted := p.committed
	// Parse interior as a full program until )
	stmts, err := p.parseStmtList(ast.TRParen)
	if err != nil {
		return nil, err
	}
	p.committed = savedCommitted
	if _, err := p.expect(ast.TRParen); err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NCmdSub, Children: []*ast.Node{ast.BlockNode(stmts)}, Pos: pos}, nil
}

func (p *Parser) parseArithSub() (*ast.Node, error) {
	pos := p.cur().Pos
	p.advance() // consume $((
	savedCommitted := p.committed
	expr, err := p.parseExpr(0)
	p.committed = savedCommitted
	if err != nil {
		return nil, err
	}
	// Expect ))
	if _, err := p.expect(ast.TRParen); err != nil {
		return nil, err
	}
	if _, err := p.expect(ast.TRParen); err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NArithSub, Children: []*ast.Node{expr}, Pos: pos}, nil
}

func (p *Parser) parseParamExpand() (*ast.Node, error) {
	pos := p.cur().Pos
	p.advance() // consume ${
	// For now, collect tokens until }
	var parts []*ast.Node
	for !p.until(ast.TRBrace, ast.TEOF) {
		tok := p.advance()
		parts = append(parts, &ast.Node{Kind: ast.NLit, Tok: tok})
	}
	if _, err := p.expect(ast.TRBrace); err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NParamExpand, Children: parts, Pos: pos}, nil
}

func (p *Parser) parseInterpolation() (*ast.Node, error) {
	pos := p.cur().Pos
	p.advance() // consume #{
	savedCommitted := p.committed
	expr, err := p.parseExpr(0)
	p.committed = savedCommitted
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(ast.TRBrace); err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NInterpolation, Children: []*ast.Node{expr}, Pos: pos}, nil
}

func (p *Parser) parseInterpString() (*ast.Node, error) {
	pos := p.cur().Pos
	p.advance() // consume TStringStart
	savedCommitted := p.committed
	var segments []*ast.Node
	for !p.until(ast.TStringEnd, ast.TEOF) {
		switch p.cur().Type {
		case ast.TString:
			tok := p.advance()
			segments = append(segments, ast.LitNode(tok))
		case ast.TDollar:
			node, err := p.parseVarRef()
			if err != nil {
				return nil, err
			}
			segments = append(segments, node)
		case ast.TSpecialVar:
			tok := p.advance()
			segments = append(segments, &ast.Node{Kind: ast.NVarRef, Tok: tok, Pos: tok.Pos})
		case ast.TDollarLParen:
			node, err := p.parseCmdSub()
			if err != nil {
				return nil, err
			}
			segments = append(segments, node)
		case ast.TDollarDLParen:
			node, err := p.parseArithSub()
			if err != nil {
				return nil, err
			}
			segments = append(segments, node)
		case ast.TDollarLBrace:
			node, err := p.parseParamExpand()
			if err != nil {
				return nil, err
			}
			segments = append(segments, node)
		case ast.THashLBrace:
			node, err := p.parseInterpolation()
			if err != nil {
				return nil, err
			}
			segments = append(segments, node)
		default:
			// Skip unexpected tokens inside string
			p.advance()
		}
	}
	p.committed = savedCommitted
	if _, err := p.expect(ast.TStringEnd); err != nil {
		return nil, err
	}
	// Optimize: if only one literal segment, just return it as TString
	if len(segments) == 1 && segments[0].Kind == ast.NLit {
		return segments[0], nil
	}
	return &ast.Node{Kind: ast.NInterpString, Children: segments, Pos: pos}, nil
}

func (p *Parser) parseDollarString() (*ast.Node, error) {
	// $"..." uses TDollarDQuote as opener, same segments as regular interpolated string
	pos := p.cur().Pos
	p.advance() // consume TDollarDQuote
	savedCommitted := p.committed
	var segments []*ast.Node
	for !p.until(ast.TStringEnd, ast.TEOF) {
		switch p.cur().Type {
		case ast.TString:
			tok := p.advance()
			segments = append(segments, ast.LitNode(tok))
		case ast.TDollar:
			node, err := p.parseVarRef()
			if err != nil {
				return nil, err
			}
			segments = append(segments, node)
		case ast.TSpecialVar:
			tok := p.advance()
			segments = append(segments, &ast.Node{Kind: ast.NVarRef, Tok: tok, Pos: tok.Pos})
		case ast.TDollarLParen:
			node, err := p.parseCmdSub()
			if err != nil {
				return nil, err
			}
			segments = append(segments, node)
		case ast.TDollarLBrace:
			node, err := p.parseParamExpand()
			if err != nil {
				return nil, err
			}
			segments = append(segments, node)
		case ast.THashLBrace:
			node, err := p.parseInterpolation()
			if err != nil {
				return nil, err
			}
			segments = append(segments, node)
		default:
			p.advance()
		}
	}
	p.committed = savedCommitted
	if _, err := p.expect(ast.TStringEnd); err != nil {
		return nil, err
	}
	if len(segments) == 1 && segments[0].Kind == ast.NLit {
		return segments[0], nil
	}
	return &ast.Node{Kind: ast.NInterpString, Children: segments, Pos: pos}, nil
}

// --- Binding ---

func (p *Parser) parseIshBind() (*ast.Node, error) {
	nameTok := p.advance() // consume identifier
	p.advance()            // consume =
	p.committed = true     // binding RHS is definitively expression context
	savedExpr := p.exprContext
	p.exprContext = true
	rhs, err := p.parsePipeline()
	p.exprContext = savedExpr
	if err != nil {
		return nil, err
	}
	lhs := ast.IdentNode(nameTok)
	return &ast.Node{Kind: ast.NMatch, Children: []*ast.Node{lhs, rhs}, Pos: nameTok.Pos}, nil
}

// parseExprPipeline parses an expression that may contain |> pipe arrows.
// Unlike parsePipeline (which goes through parseStmt), this stays in expression
// context — fn is anonymous, keywords are values, etc.
func (p *Parser) parseExprPipeline() (*ast.Node, error) {
	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	for p.cur().Type == ast.TPipeArrow {
		p.advance()
		p.committed = true
		right, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		expr = &ast.Node{Kind: ast.NPipeFn, Children: []*ast.Node{expr, right}}
	}
	return expr, nil
}

// --- Subshell / Group ---

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
	for !p.until(terminator, ast.TEOF) {
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

// --- If ---

func (p *Parser) parseIf() (*ast.Node, error) {
	p.advance() // consume TIf

	// Lookahead: scan to determine if this is `if ... then` (POSIX) or `if ... do` (ish).
	// If ish-style, parse condition in expression context so > and < are comparisons.
	isIshStyle := false
	for i := p.pos; ; i++ {
		t := p.rawAt(i)
		if t.Type == ast.TEOF {
			break
		}
		if t.Type == ast.TDo {
			isIshStyle = true
			break
		}
		if t.Type == ast.TThen {
			break
		}
	}

	// Parse condition
	old := p.pushTerminators(ast.TThen, ast.TDo)
	savedExpr := p.exprContext
	if isIshStyle {
		p.exprContext = true
	}
	cond, err := p.parseList()
	p.exprContext = savedExpr
	p.restoreTerminators(old)
	if err != nil {
		return nil, err
	}

	p.skipSeparators()

	if p.cur().Type == ast.TThen {
		return p.parsePosixIf(cond)
	} else if p.cur().Type == ast.TDo {
		return p.parseIshIf(cond)
	}
	return nil, fmt.Errorf("expected 'then' or 'do' after if condition at pos %d", p.cur().Pos)
}

func (p *Parser) parsePosixIf(cond *ast.Node) (*ast.Node, error) {
	node := &ast.Node{Kind: ast.NIf}
	p.advance() // consume TThen
	p.skipNewlines()

	old := p.pushTerminators(ast.TElif, ast.TElse, ast.TFi)
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

	for p.match(ast.TElif) {
		old = p.pushTerminators(ast.TThen, ast.TDo)
		elifCond, err := p.parseList()
		p.restoreTerminators(old)
		if err != nil {
			return nil, err
		}
		p.skipSeparators()
		if p.cur().Type == ast.TThen {
			p.advance()
		}
		p.skipNewlines()
		old = p.pushTerminators(ast.TElif, ast.TElse, ast.TFi)
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

	if p.cur().Type == ast.TElse {
		p.advance()
		p.skipNewlines()
		old = p.pushTerminators(ast.TFi)
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

	if _, err := p.expect(ast.TFi); err != nil {
		return nil, err
	}
	return node, nil
}

func (p *Parser) parseIshIf(cond *ast.Node) (*ast.Node, error) {
	node := &ast.Node{Kind: ast.NIshIf}

	// Convert bare keyword values to literal nodes
	if cond.Kind == ast.NCmd && len(cond.Children) == 1 {
		child := cond.Children[0]
		if child.Kind == ast.NIdent {
			switch child.Tok.Type {
			case ast.TNil, ast.TTrue, ast.TFalse:
				cond = ast.LitNode(child.Tok)
			}
		}
	}

	if err := p.ishBlockWithSection(ast.TElse, func() error {
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

// --- For ---

func (p *Parser) parseFor() (*ast.Node, error) {
	p.advance() // consume TFor
	node := &ast.Node{Kind: ast.NFor}

	varTok, err := p.expect(ast.TIdent)
	if err != nil {
		return nil, fmt.Errorf("expected variable name after 'for' at pos %d", p.cur().Pos)
	}

	p.skipSeparators()
	if _, err := p.expect(ast.TIn); err != nil {
		return nil, fmt.Errorf("expected 'in' after 'for %s' at pos %d", varTok.Val, p.cur().Pos)
	}

	old := p.pushTerminators(ast.TDo)
	var words []*ast.Node
	for !p.until(ast.TEOF) {
		if p.cur().Type == ast.TNewline || p.cur().Type == ast.TSemicolon {
			break
		}
		if p.cur().Type == ast.TDo {
			break
		}
		// Parse each word as a compound word
		arg, err := p.parseCompoundWord()
		if err != nil {
			return nil, err
		}
		if arg != nil {
			words = append(words, arg)
		}
	}
	p.restoreTerminators(old)

	p.skipSeparators()
	if _, err := p.expect(ast.TDo); err != nil {
		return nil, fmt.Errorf("expected 'do' in for loop at pos %d", p.cur().Pos)
	}

	p.skipNewlines()
	old = p.pushTerminators(ast.TDone, ast.TEnd)
	bodyStmts, err := p.parseBlock()
	p.restoreTerminators(old)
	if err != nil {
		return nil, err
	}
	if p.cur().Type == ast.TDone || p.cur().Type == ast.TEnd {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'done' or 'end' at pos %d", p.cur().Pos)
	}

	markTail(bodyStmts)
	varNode := ast.IdentNode(varTok)
	node.Children = append([]*ast.Node{varNode}, words...)
	node.Clauses = []ast.Clause{{Body: ast.BlockNode(bodyStmts)}}
	return node, nil
}

// --- While / Until ---

func (p *Parser) parseWhile() (*ast.Node, error) {
	return p.parseWhileUntil(ast.NWhile)
}

func (p *Parser) parseUntil() (*ast.Node, error) {
	return p.parseWhileUntil(ast.NUntil)
}

func (p *Parser) parseWhileUntil(kind ast.NodeKind) (*ast.Node, error) {
	p.advance()

	old := p.pushTerminators(ast.TDo)
	cond, err := p.parseList()
	p.restoreTerminators(old)
	if err != nil {
		return nil, err
	}
	p.skipSeparators()
	if _, err := p.expect(ast.TDo); err != nil {
		return nil, err
	}

	p.skipNewlines()
	old = p.pushTerminators(ast.TDone, ast.TEnd)
	bodyStmts, err := p.parseBlock()
	p.restoreTerminators(old)
	if err != nil {
		return nil, err
	}
	if p.cur().Type == ast.TDone || p.cur().Type == ast.TEnd {
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

// --- Case ---

func (p *Parser) parseCase() (*ast.Node, error) {
	p.advance() // consume TCase
	node := &ast.Node{Kind: ast.NCase}

	// case WORD in — the word is a compound word
	caseWord, err := p.parseCompoundWord()
	if err != nil {
		return nil, err
	}
	if caseWord == nil {
		return nil, fmt.Errorf("expected word after 'case' at pos %d", p.cur().Pos)
	}
	node.Children = []*ast.Node{caseWord}

	p.skipSeparators()
	if _, err := p.expect(ast.TIn); err != nil {
		return nil, fmt.Errorf("expected 'in' in case at pos %d", p.cur().Pos)
	}
	p.skipNewlines()

	for !p.until(ast.TEOF) {
		if p.cur().Type == ast.TEsac {
			p.advance()
			break
		}
		if p.cur().Type == ast.TLParen {
			p.advance()
		}
		// Collect pattern alternatives separated by |
		var patterns []string
		for !p.until(ast.TRParen, ast.TEOF) {
			pw, err := p.parseCompoundWord()
			if err != nil {
				return nil, err
			}
			if pw != nil {
				patterns = append(patterns, nodeToString(pw))
			}
			if p.cur().Type == ast.TPipe {
				p.advance()
			}
		}
		patVal := strings.Join(patterns, "|")
		pat := &ast.Node{Kind: ast.NLit, Tok: ast.Token{Type: ast.TString, Val: patVal}}
		if p.cur().Type == ast.TRParen {
			p.advance()
		}
		p.skipNewlines()

		old := p.pushTerminators(ast.TEsac)
		var body []*ast.Node
		for !p.until(ast.TEOF) {
			if p.cur().Type == ast.TEsac {
				break
			}
			if p.cur().Type == ast.TSemicolon && p.peek(1).Type == ast.TSemicolon {
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

// --- POSIX function definition ---

func (p *Parser) parsePosixFnDef() (*ast.Node, error) {
	nameTok := p.advance() // name
	p.advance()            // (
	p.advance()            // )
	p.skipNewlines()

	if p.cur().Type != ast.TLBrace {
		return nil, fmt.Errorf("expected '{' in function definition at pos %d", p.cur().Pos)
	}
	p.advance()
	p.skipNewlines()

	var bodyStmts []*ast.Node
	for !p.until(ast.TEOF) {
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

// --- ish function ---

func (p *Parser) parseIshFn() (*ast.Node, error) {
	return p.parseIshFnWith(false)
}

// parseIshFnAnon parses fn in value/expression position — always anonymous.
func (p *Parser) parseIshFnAnon() (*ast.Node, error) {
	p.advance() // consume TFn

	nameTok := ast.Token{Type: ast.TIdent, Val: "<anon>"}

	var params []*ast.Node
	for !p.until(ast.TEOF) {
		if p.cur().Type == ast.TDo || p.cur().Val == "when" {
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
	if p.cur().Type == ast.TIdent && p.cur().Val == "when" {
		p.advance()
		p.committed = true
		var err error
		guard, err = p.parseExpr(0)
		if err != nil {
			return nil, err
		}
	}

	var fnNode *ast.Node
	if err := p.ishBlock(func() error {
		if len(params) == 0 && guard == nil && p.looksLikeClauseStart() {
			clauses, err := p.parseClauses(func() (*ast.Node, error) {
				var clauseParams []*ast.Node
				for !p.until(ast.TEOF) {
					if p.cur().Type == ast.TArrow || (p.cur().Type == ast.TIdent && p.cur().Val == "when") {
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

func (p *Parser) parseIshFnWith(requireName bool) (*ast.Node, error) {
	p.advance() // consume TFn

	var nameTok ast.Token
	if !requireName && (p.cur().Type == ast.TDo || p.cur().Type == ast.TBackslash) {
		// fn do ... end — anonymous, no params
		nameTok = ast.Token{Type: ast.TIdent, Val: "<anon>"}
	} else if !requireName && p.cur().Type == ast.TIdent && (p.peek(1).Type == ast.TComma || p.cur().Val == "when") {
		// fn a, b do — anonymous with params (comma after first ident means it's a param)
		nameTok = ast.Token{Type: ast.TIdent, Val: "<anon>"}
	} else if p.cur().Type == ast.TIdent {
		nameTok = p.advance()
	} else if requireName {
		return nil, fmt.Errorf("expected function name after 'fn' at pos %d", p.cur().Pos)
	} else {
		nameTok = ast.Token{Type: ast.TIdent, Val: "<anon>"}
	}

	var params []*ast.Node
	for !p.until(ast.TEOF) {
		if p.cur().Type == ast.TDo || p.cur().Val == "when" {
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
	if p.cur().Type == ast.TIdent && p.cur().Val == "when" {
		p.advance()
		p.committed = true // guard expression is definitively expression context
		var err error
		guard, err = p.parseExpr(0)
		if err != nil {
			return nil, err
		}
	}

	var fnNode *ast.Node
	if err := p.ishBlock(func() error {
		if len(params) == 0 && guard == nil && p.looksLikeClauseStart() {
			clauses, err := p.parseClauses(func() (*ast.Node, error) {
				var clauseParams []*ast.Node
				for !p.until(ast.TEOF) {
					if p.cur().Type == ast.TArrow || (p.cur().Type == ast.TIdent && p.cur().Val == "when") {
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

// --- Module ---

func (p *Parser) parseDefModule() (*ast.Node, error) {
	pos := p.cur().Pos
	p.advance() // consume TDefModule
	if p.cur().Type != ast.TIdent {
		return nil, fmt.Errorf("defmodule: expected module name")
	}
	name := p.advance()
	if _, err := p.expect(ast.TDo); err != nil {
		return nil, fmt.Errorf("defmodule: expected 'do'")
	}
	p.skipNewlines()

	old := p.pushTerminators(ast.TEnd)
	defer p.restoreTerminators(old)

	var children []*ast.Node
	for !p.until(ast.TEnd, ast.TEOF) {
		p.skipNewlines()
		if p.cur().Type == ast.TEnd {
			break
		}
		var child *ast.Node
		var err error
		if p.cur().Type == ast.TFn || (p.cur().Type == ast.TIdent && p.cur().Val == "def") {
			child, err = p.parseIshFnWith(true)
		} else if p.cur().Type == ast.TUse {
			child, err = p.parseUse()
		} else {
			child, err = p.parseStmt()
		}
		if err != nil {
			return nil, err
		}
		if child != nil {
			children = append(children, child)
		}
		p.skipSeparators()
	}
	if _, err := p.expect(ast.TEnd); err != nil {
		return nil, fmt.Errorf("defmodule: expected 'end'")
	}
	return &ast.Node{Kind: ast.NDefModule, Pos: pos, Tok: name, Children: children}, nil
}

func (p *Parser) parseUse() (*ast.Node, error) {
	pos := p.cur().Pos
	p.advance() // consume TUse
	if p.cur().Type != ast.TIdent {
		return nil, fmt.Errorf("use: expected module name")
	}
	name := p.advance()
	return &ast.Node{Kind: ast.NUse, Pos: pos, Tok: name}, nil
}

// --- Clauses ---

func (p *Parser) parseClauseBody() (*ast.Node, error) {
	old := p.pushTerminators(ast.TEnd)
	defer p.restoreTerminators(old)
	var stmts []*ast.Node
	for !p.until(ast.TEOF) {
		if p.cur().Type == ast.TEnd || p.cur().Type == ast.TAfter {
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
		if t.Type == ast.TAfter || t.Type == ast.TEnd || t.Type == ast.TRescue {
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
	for !p.until(ast.TEOF) {
		if p.isBlockEndToken() {
			break
		}
		pat, err := parsePattern()
		if err != nil {
			return nil, err
		}
		var guard *ast.Node
		if p.cur().Type == ast.TIdent && p.cur().Val == "when" {
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

// --- ish extensions ---

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
		for !p.until(ast.TEOF) {
			if p.isBlockEndToken() {
				break
			}
			if p.cur().Type == ast.TIdent && p.cur().Val == "worker" {
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
	if err := p.ishBlockWithSection(ast.TAfter, func() error {
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
	if err := p.ishBlockWithSection(ast.TRescue, func() error {
		bodyStmts, err := p.parseBlock()
		if err != nil {
			return err
		}
		// Don't markTail in try body — tail calls would bypass error catching
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

// --- Patterns ---

func (p *Parser) parsePattern() (*ast.Node, error) {
	cur := p.cur()
	switch cur.Type {
	case ast.TAtom:
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TInt, ast.TFloat:
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TString, ast.TStringStart:
		if cur.Type == ast.TStringStart {
			return p.parseInterpString()
		}
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TNil, ast.TTrue, ast.TFalse:
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TIdent:
		if cur.Val == "_" {
			p.advance()
			return ast.IdentNode(ast.Token{Type: ast.TIdent, Val: "_"}), nil
		}
		p.advance()
		return ast.IdentNode(cur), nil
	case ast.TLBrace:
		return p.parseTupleExpr()
	case ast.TLBracket:
		return p.parseListExpr()
	case ast.TPercentLBrace:
		return p.parseMapPattern()
	case ast.TPercent:
		return nil, fmt.Errorf("unexpected token in pattern: %%")
	default:
		return nil, fmt.Errorf("unexpected token in pattern: %q at pos %d", cur.Val, cur.Pos)
	}
}

func (p *Parser) parseMapPattern() (*ast.Node, error) {
	p.advance() // skip %{
	node := &ast.Node{Kind: ast.NMap}
	p.skipNewlines()
	for !p.until(ast.TRBrace, ast.TEOF) {
		key := p.advance()
		keyName := key.Val
		if p.cur().Type == ast.TColon {
			p.advance()
		}
		key.Val = keyName
		val, err := p.parsePattern()
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

// --- Lambda ---

func (p *Parser) parseLambda() (*ast.Node, error) {
	p.advance() // skip backslash

	var params []*ast.Node
	for !p.until(ast.TEOF, ast.TArrow) {
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
	p.advance()

	var body *ast.Node
	multiLine := p.cur().Type == ast.TNewline
	if multiLine {
		old := p.pushTerminators(ast.TEnd)
		savedExpr := p.exprContext
		p.exprContext = true
		p.skipNewlines()
		stmts, err := p.parseBlock()
		p.exprContext = savedExpr
		p.restoreTerminators(old)
		if err != nil {
			return nil, err
		}
		body = ast.BlockNode(stmts)
		markTail(stmts)
		p.match(ast.TEnd)
	} else {
		// Single-line lambda: parse as statement but don't consume |> or ,
		// that belong to the outer expression. E.g. in
		// `list |> List.filter \x -> x > 1 |> length`
		// the lambda body is just `x > 1`, not `x > 1 |> length`.
		// Set exprContext so > and < are treated as comparison, not redirect.
		savedExpr := p.exprContext
		p.exprContext = true
		var err error
		body, err = p.parseStmtWithOps()
		p.exprContext = savedExpr
		if err != nil {
			return nil, err
		}
		if body != nil {
			body.Tail = true
		}
	}

	return &ast.Node{
		Kind:     ast.NLambda,
		Children: params,
		Clauses:  []ast.Clause{{Body: body}},
	}, nil
}

// --- Expression parsing ---

func (p *Parser) parseExpression() (*ast.Node, error) {
	expr, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	// |> is handled by parsePipeline at the statement level, not here.
	// This prevents lambda bodies and nested expressions from consuming
	// pipe arrows that belong to the outer pipeline.

	if p.cur().Type == ast.TEquals {
		p.advance()
		rhs, err := p.parseExpression()
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
		// Only commit on spaced operators — adjacent operators (a-z, 3+4)
		// could be compound word fragments in command context.
		prevTok := p.rawAt(p.pos - 1)
		op := p.advance()
		if prevTok.SpaceAfter {
			p.committed = true
		}
		right, err := p.parseExpr(prec)
		if err != nil {
			return nil, err
		}
		left = &ast.Node{Kind: ast.NBinOp, Tok: op, Children: []*ast.Node{left, right}}

		// Comparison chaining: desugar a < b < c into (a < b) && (b < c)
		if isComparisonOp(op.Type) {
			for isComparisonOp(p.cur().Type) {
				nextOp := p.advance()
				nextRight, err := p.parseExpr(p.precedence(nextOp.Type))
				if err != nil {
					return nil, err
				}
				cmp := &ast.Node{Kind: ast.NBinOp, Tok: nextOp, Children: []*ast.Node{right, nextRight}}
				left = &ast.Node{Kind: ast.NAndList, Children: []*ast.Node{left, cmp}}
				right = nextRight
			}
		}
	}

	// Dot access chain — dots must be adjacent (no whitespace). a.b not a . b
	// After a dot, keywords are treated as field names (Regex.match, List.map, etc.)
	for p.cur().Type == ast.TDot {
		p.advance()
		cur := p.cur()
		if cur.Type == ast.TIdent || cur.Type.IsKeyword() {
			field := p.advance()
			// Normalize keyword token to TIdent for the field name
			field.Type = ast.TIdent
			left = &ast.Node{Kind: ast.NAccess, Tok: field, Children: []*ast.Node{left}}
		} else {
			return nil, fmt.Errorf("expected field name after '.' at pos %d", p.cur().Pos)
		}
	}

	// Function application: identifier or access chain followed by a value → NCall
	// Application binds tighter than all binary operators.
	if (left.Kind == ast.NIdent || left.Kind == ast.NAccess) && isValueStart(p.cur().Type) {
		call := &ast.Node{Kind: ast.NCall, Children: []*ast.Node{left}}
		// Adjacent parens: func(a, b) — multi-arg call. Required inside data structures.
		if p.cur().Type == ast.TLParen && !p.rawAt(p.pos-1).SpaceAfter {
			p.advance()
			p.committed = true
			for !p.until(ast.TRParen, ast.TEOF) {
				arg, err := p.parseExpr(0)
				if err != nil {
					return nil, err
				}
				call.Children = append(call.Children, arg)
				if p.cur().Type == ast.TComma {
					p.advance()
				}
			}
			if _, err := p.expect(ast.TRParen); err != nil {
				return nil, err
			}
			left = call
		} else {
			// Juxtaposition: func value — single value arg.
			// func (expr) with spaced parens = grouped expression as single arg.
			arg, err := p.parseValue(false)
			if err != nil {
				return nil, err
			}
			switch arg.Kind {
			case ast.NLambda:
				p.committed = true
			}
			call.Children = append(call.Children, arg)
			left = call
		}

		// After function call, check for more binary operators:
		// fib(n-1) + fib(n-2) — the + connects two call results
		for {
			tt := p.cur().Type
			if tt == ast.TGt || tt == ast.TLt {
				if !p.committed && !p.exprContext {
					break // could be redirect in command context
				}
			}
			// TMinus/TPlus adjacent to next token are flags (-m, +x), not operators
			if (tt == ast.TMinus || tt == ast.TPlus) && !p.cur().SpaceAfter && !p.committed && !p.exprContext {
				break
			}
			prec := p.precedence(tt)
			if prec <= minPrec {
				break
			}
			prevTok2 := p.rawAt(p.pos - 1)
			op := p.advance()
			if prevTok2.SpaceAfter {
				p.committed = true
			}
			right, err := p.parseExpr(prec)
			if err != nil {
				return nil, err
			}
			left = &ast.Node{Kind: ast.NBinOp, Tok: op, Children: []*ast.Node{left, right}}
		}
	}

	return left, nil
}

func (p *Parser) parseValue(cmdArg bool) (*ast.Node, error) {
	cur := p.cur()

	switch cur.Type {
	case ast.TInt, ast.TFloat:
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TString:
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TStringStart:
		return p.parseInterpString()
	case ast.TDollarDQuote:
		return p.parseDollarString()
	case ast.TAtom:
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TNil, ast.TTrue, ast.TFalse:
		p.advance()
		return ast.LitNode(cur), nil
	case ast.TIdent:
		if cur.Type == ast.TFn {
			return p.parseIshFnAnon()
		}
		p.advance()
		return ast.IdentNode(cur), nil
	case ast.TFn:
		return p.parseIshFnAnon()
	case ast.TMatch:
		return p.parseIshMatchExpr()
	case ast.TIf:
		return p.parseIf()
	case ast.TTry:
		return p.parseIshTry()
	case ast.TReceive:
		return p.parseIshReceive()
	case ast.TSpawn:
		return p.parseIshSpawn()
	case ast.TSpawnLink:
		return p.parseIshSpawnLink()
	case ast.TAwait:
		return p.parseIshAwait()
	case ast.TMonitor:
		return p.parseIshMonitor()
	case ast.TSupervise:
		return p.parseIshSupervise()
	case ast.TSend:
		return p.parseIshSend()
	case ast.TDollar:
		return p.parseVarRef()
	case ast.TSpecialVar:
		tok := p.advance()
		return &ast.Node{Kind: ast.NVarRef, Tok: tok, Pos: tok.Pos}, nil
	case ast.TDollarLParen:
		return p.parseCmdSub()
	case ast.TDollarDLParen:
		return p.parseArithSub()
	case ast.TDollarLBrace:
		return p.parseParamExpand()
	case ast.THashLBrace:
		return p.parseInterpolation()
	case ast.TLParen:
		p.advance()
		savedCommitted := p.committed
		expr, err := p.parsePipeline()
		p.committed = savedCommitted
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(ast.TRParen); err != nil {
			return nil, err
		}
		return expr, nil
	case ast.TLBrace:
		return p.parseTupleExpr()
	case ast.TLBracket:
		return p.parseListExpr()
	case ast.TPercentLBrace:
		return p.parseMapExpr()
	case ast.TPercent:
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
	case ast.TAmpersand:
		// &name — capture function reference
		p.advance()
		tok := p.advance()
		return &ast.Node{Kind: ast.NCapture, Tok: tok}, nil
	case ast.TMinus:
		p.advance()
		operand, err := p.parseValue(false)
		if err != nil {
			return nil, err
		}
		return &ast.Node{Kind: ast.NUnary, Tok: cur, Children: []*ast.Node{operand}}, nil
	default:
		if cmdArg {
			p.advance()
			return ast.IdentNode(ast.Token{Type: ast.TIdent, Val: cur.Val, Pos: cur.Pos}), nil
		}
		return nil, fmt.Errorf("unexpected token: %q at pos %d", cur.Val, cur.Pos)
	}
}

func isComparisonOp(tt ast.TokenType) bool {
	switch tt {
	case ast.TEq, ast.TNe, ast.TLe, ast.TGe, ast.TGt, ast.TLt:
		return true
	}
	return false
}

func (p *Parser) precedence(tt ast.TokenType) int {
	switch tt {
	case ast.TEq, ast.TNe:
		return 1
	case ast.TLe, ast.TGe:
		return 2
	case ast.TGt, ast.TLt:
		// Only treat > and < as comparison operators when in expression context
		// (lambda bodies, fn guards, committed expressions). Otherwise they're
		// ambiguous with POSIX redirects.
		if p.committed || p.exprContext {
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

// --- Tuple / List / Map expressions ---

func (p *Parser) parseTupleExpr() (*ast.Node, error) {
	p.advance()
	savedCommitted := p.committed
	var elems []*ast.Node
	p.skipNewlines()
	for !p.until(ast.TRBrace, ast.TEOF) {
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
	p.committed = savedCommitted
	if _, err := p.expect(ast.TRBrace); err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NTuple, Children: elems}, nil
}

func (p *Parser) parseListExpr() (*ast.Node, error) {
	p.advance()
	savedCommitted := p.committed
	var elems []*ast.Node
	var rest *ast.Node
	p.skipNewlines()
	for !p.until(ast.TRBracket, ast.TEOF) {
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
	p.committed = savedCommitted
	if _, err := p.expect(ast.TRBracket); err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NList, Children: elems, Rest: rest}, nil
}

func (p *Parser) parseMapExpr() (*ast.Node, error) {
	p.advance() // %{
	savedCommitted := p.committed
	node := &ast.Node{Kind: ast.NMap}
	p.skipNewlines()
	for !p.until(ast.TRBrace, ast.TEOF) {
		// Key: either IDENT COLON or ATOM (which already has no colon prefix)
		key := p.advance()
		keyName := key.Val
		// Expect TColon after key identifier
		if p.cur().Type == ast.TColon {
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
	p.committed = savedCommitted
	if _, err := p.expect(ast.TRBrace); err != nil {
		return nil, err
	}
	return node, nil
}
