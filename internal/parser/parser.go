package parser

import (
	"fmt"

	"ish/internal/ast"
)

type Parser struct {
	tokens []ast.Token
	pos    int
	stops  []string
}

func New(tokens []ast.Token) *Parser {
	return &Parser{tokens: tokens}
}

func (p *Parser) cur() ast.Token {
	if p.pos >= len(p.tokens) {
		return ast.Token{Type: ast.TEOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) prev() ast.Token {
	if p.pos == 0 {
		return ast.Token{Type: ast.TEOF}
	}
	return p.tokens[p.pos-1]
}

func (p *Parser) peek(n int) ast.Token {
	i := p.pos + n
	if i >= len(p.tokens) {
		return ast.Token{Type: ast.TEOF}
	}
	return p.tokens[i]
}

func (p *Parser) advance() ast.Token {
	t := p.cur()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return t
}

func (p *Parser) expect(tt ast.TokenType) (ast.Token, error) {
	t := p.cur()
	if t.Type != tt {
		return t, fmt.Errorf("expected token %d, got %d (%q) at pos %d", tt, t.Type, t.Val, t.Pos)
	}
	p.pos++
	return t, nil
}

func (p *Parser) expectWord(val string) (ast.Token, error) {
	t := p.cur()
	if t.Type != ast.TIdent || t.Val != val {
		return t, fmt.Errorf("expected '%s', got %q at pos %d", val, t.Val, t.Pos)
	}
	p.pos++
	return t, nil
}

func (p *Parser) is(val string) bool {
	return p.cur().Type == ast.TIdent && p.cur().Val == val
}

func (p *Parser) skipNewlines() {
	for p.cur().Type == ast.TNewline || p.cur().Type == ast.TSemicolon {
		p.advance()
	}
}

// Parse parses the full program.
func (p *Parser) Parse() (*ast.Node, error) {
	return p.parseBlockUntilEOF()
}

func (p *Parser) parseBlockUntilEOF() (*ast.Node, error) {
	block := &ast.Node{Kind: ast.NBlock}
	p.skipNewlines()
	for p.cur().Type != ast.TEOF {
		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			block.Children = append(block.Children, stmt)
		}
		p.skipNewlines()
	}
	return block, nil
}

func (p *Parser) parseBlockUntil(newStops ...string) (*ast.Node, error) {
	saved := p.stops
	p.stops = append(p.stops, newStops...)
	block := &ast.Node{Kind: ast.NBlock}
	p.skipNewlines()
	for p.cur().Type != ast.TEOF {
		for _, s := range newStops {
			if p.is(s) {
				p.stops = saved
				return block, nil
			}
		}
		if p.cur().Type == ast.TRParen {
			p.stops = saved
			return block, nil
		}
		stmt, err := p.parseStatement()
		if err != nil {
			p.stops = saved
			return nil, err
		}
		if stmt != nil {
			block.Children = append(block.Children, stmt)
		}
		p.skipNewlines()
	}
	p.stops = saved
	return block, nil
}

// ============================================================
// STATEMENT CONTEXT
// ============================================================

func (p *Parser) parseStatement() (*ast.Node, error) {
	// Destructuring bind: {pattern} = expr or [pattern] = expr
	if p.cur().Type == ast.TLBrace || p.cur().Type == ast.TLBracket {
		if bind, ok := p.tryDestructBind(); ok {
			return bind, nil
		}
	}

	// Simple binding: IDENT = expr (spaced =)
	if p.cur().Type == ast.TIdent && p.cur().SpaceAfter && p.peek(1).Type == ast.TAssign {
		return p.parseBinding()
	}

	// POSIX assignment: FOO=bar (no space)
	if p.cur().Type == ast.TIdent && !p.cur().SpaceAfter && p.peek(1).Type == ast.TAssign {
		assign, err := p.parsePosixAssign()
		if err != nil {
			return nil, err
		}
		// Prefix assign: X=val cmd (command follows on same line, no separator)
		if p.cur().Type != ast.TSemicolon && p.cur().Type != ast.TNewline &&
			p.cur().Type != ast.TEOF && p.cur().Type != ast.TPipe &&
			p.cur().Type != ast.TAnd && p.cur().Type != ast.TOr &&
			p.cur().Type != ast.TRParen && p.cur().Type != ast.TRBrace {
			cmd, err := p.parsePipeline()
			if err != nil {
				return nil, err
			}
			return &ast.Node{Kind: ast.NBlock, Tok: ast.Token{Val: "prefix"}, Children: []*ast.Node{assign, cmd}}, nil
		}
		return assign, nil
	}

	// POSIX function definition: name() { body }
	if p.cur().Type == ast.TIdent && !p.cur().SpaceAfter && p.peek(1).Type == ast.TLParen && p.peek(2).Type == ast.TRParen {
		return p.parsePosixFnDef()
	}

	// Keyword-driven constructs at statement head
	if p.cur().Type == ast.TIdent {
		switch p.cur().Val {
		case "fn":
			return p.parseFnDef(false)
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
		case "match":
			return p.parseMatch()
		case "defmodule":
			return p.parseDefModule()
		case "use", "import":
			return p.parseUseImport()
		case "try":
			return p.parseTry()
		case "receive":
			return p.parseReceive()
		case "spawn", "spawn_link", "send", "await", "monitor":
			// OTP keywords: parse args in expression context
			head := p.advance()
			args := []*ast.Node{{Kind: ast.NIdent, Tok: head}}
			for p.cur().Type != ast.TSemicolon && p.cur().Type != ast.TNewline && p.cur().Type != ast.TEOF {
				if p.cur().Type == ast.TComma {
					p.advance()
					continue
				}
				if !isExprArgStart(p.cur().Type) {
					break
				}
				arg, err := p.parseExprPipe()
				if err != nil {
					break
				}
				args = append(args, arg)
			}
			return &ast.Node{Kind: ast.NApply, Children: args}, nil
		}
	}

	if p.cur().Type == ast.TLBrace {
		if p.looksLikeTuple() {
			return p.parseExpr()
		}
		return p.parseGroup()
	}

	if p.cur().Type == ast.TLParen {
		p.advance()
		block, err := p.parseBlockUntil()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(ast.TRParen); err != nil {
			return nil, fmt.Errorf("expected )")
		}
		return &ast.Node{Kind: ast.NSubshell, Children: block.Children}, nil
	}

	result, err := p.parsePipeline()
	if err != nil {
		return nil, err
	}

	if isSingleValue(result) && isExprExtendOp(p.cur().Type) {
		return p.extendAsExpr(result)
	}

	if p.cur().Type == ast.TAmpersand {
		p.advance()
		return &ast.Node{Kind: ast.NBg, Children: []*ast.Node{result}}, nil
	}

	return result, nil
}

func (p *Parser) tryDestructBind() (*ast.Node, bool) {
	open := p.cur().Type
	close := ast.TRBrace
	if open == ast.TLBracket {
		close = ast.TRBracket
	}
	depth := 0
	for i := p.pos; i < len(p.tokens); i++ {
		if p.tokens[i].Type == open {
			depth++
		} else if p.tokens[i].Type == close {
			depth--
			if depth == 0 {
				if i+1 < len(p.tokens) && p.tokens[i+1].Type == ast.TAssign {
					node, err := p.parseDestructBind()
					if err != nil {
						return nil, false
					}
					return node, true
				}
				return nil, false
			}
		} else if p.tokens[i].Type == ast.TEOF {
			break
		}
	}
	return nil, false
}

func (p *Parser) parseDestructBind() (*ast.Node, error) {
	lhs, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(ast.TAssign); err != nil {
		return nil, fmt.Errorf("expected = in destructuring bind")
	}
	rhs, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NBind, Children: []*ast.Node{lhs, rhs}}, nil
}

func (p *Parser) parseBinding() (*ast.Node, error) {
	name := p.advance()
	p.advance() // skip =
	rhs, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ast.Node{
		Kind:     ast.NBind,
		Tok:      name,
		Children: []*ast.Node{{Kind: ast.NIdent, Tok: name}, rhs},
	}, nil
}

func (p *Parser) parsePosixAssign() (*ast.Node, error) {
	name := p.advance()
	p.advance() // skip =
	if p.cur().Type == ast.TSemicolon || p.cur().Type == ast.TNewline || p.cur().Type == ast.TEOF {
		return &ast.Node{
			Kind: ast.NBind, Tok: name,
			Children: []*ast.Node{
				{Kind: ast.NIdent, Tok: name},
				{Kind: ast.NLit, Tok: ast.Token{Type: ast.TString, Val: ""}},
			},
		}, nil
	}
	val, err := p.parseCmdPrimary()
	if err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NBind, Tok: name, Children: []*ast.Node{{Kind: ast.NIdent, Tok: name}, val}}, nil
}

func (p *Parser) parsePosixFnDef() (*ast.Node, error) {
	name := p.advance()
	p.advance() // (
	p.advance() // )
	p.skipNewlines()
	if p.cur().Type != ast.TLBrace {
		return nil, fmt.Errorf("expected { in function definition")
	}
	body, err := p.parseGroup()
	if err != nil {
		return nil, err
	}
	return &ast.Node{Kind: ast.NFnDef, Tok: name, Children: []*ast.Node{{Kind: ast.NIdent, Tok: name}, body}}, nil
}

// ============================================================
// Command pipeline
// ============================================================

func (p *Parser) parsePipeline() (*ast.Node, error) {
	left, err := p.parseLogicCmd()
	if err != nil {
		return nil, err
	}
	for p.cur().Type == ast.TPipe || p.cur().Type == ast.TPipeArrow || p.cur().Type == ast.TPipeAmp {
		op := p.advance()
		p.skipNewlines()
		kind := ast.NPipe
		if op.Type == ast.TPipeArrow {
			kind = ast.NPipeFn
		} else if op.Type == ast.TPipeAmp {
			kind = ast.NPipeAmp
		}
		var right *ast.Node
		var err error
		if kind == ast.NPipeFn {
			// |> right side: single expression operand (not a full chain)
			right, err = p.parseExprLogicOr()
		} else {
			right, err = p.parseLogicCmd()
		}
		if err != nil {
			return nil, err
		}
		left = &ast.Node{Kind: kind, Tok: op, Children: []*ast.Node{left, right}}
	}
	return left, nil
}

func (p *Parser) parseLogicCmd() (*ast.Node, error) {
	left, err := p.parseCommandForm()
	if err != nil {
		return nil, err
	}
	for p.cur().Type == ast.TAnd || p.cur().Type == ast.TOr {
		op := p.advance()
		p.skipNewlines()
		right, err := p.parseCommandForm()
		if err != nil {
			return nil, err
		}
		kind := ast.NAndList
		if op.Type == ast.TOr {
			kind = ast.NOrList
		}
		left = &ast.Node{Kind: kind, Tok: op, Children: []*ast.Node{left, right}}
	}
	return left, nil
}

func (p *Parser) parseCommandForm() (*ast.Node, error) {
	if p.cur().Type == ast.TBang {
		op := p.advance()
		child, err := p.parseCommandForm()
		if err != nil {
			return nil, err
		}
		return &ast.Node{Kind: ast.NUnary, Tok: op, Children: []*ast.Node{child}}, nil
	}

	apply, err := p.parseCmdApply()
	if err != nil {
		return nil, err
	}

	// Redirects: > < >>
	for {
		fd := -1
		if apply.Kind == ast.NApply && len(apply.Children) > 0 {
			last := apply.Children[len(apply.Children)-1]
			if last.Kind == ast.NLit && last.Tok.Type == ast.TInt && !p.prev().SpaceAfter {
				if p.cur().Type == ast.TGt || p.cur().Type == ast.TLt || p.cur().Type == ast.TAppend {
					n := 0
					fmt.Sscanf(last.Tok.Val, "%d", &n)
					fd = n
					apply.Children = apply.Children[:len(apply.Children)-1]
				}
			}
		}
		if p.cur().Type == ast.THeredoc {
			heredoc, err := p.parseHeredoc()
			if err != nil {
				return nil, err
			}
			apply.Redirs = append(apply.Redirs, ast.Redirect{
				Op:     ast.THeredoc,
				Fd:     0,
				Target: heredoc,
			})
			continue
		}
		if p.cur().Type == ast.THereString {
			p.advance() // skip <<<
			word, err := p.parseCmdPrimary()
			if err != nil {
				return nil, fmt.Errorf("expected herestring word: %w", err)
			}
			apply.Redirs = append(apply.Redirs, ast.Redirect{
				Op:     ast.THeredoc, // reuse heredoc op for eval
				Fd:     0,
				Target: word,
			})
			continue
		}
		if p.cur().Type != ast.TGt && p.cur().Type != ast.TLt && p.cur().Type != ast.TAppend {
			break
		}
		op := p.advance()
		if fd == -1 {
			fd = 1
			if op.Type == ast.TLt {
				fd = 0
			}
		}
		if p.cur().Type == ast.TAmpersand {
			p.advance()
			target, err := p.parseCmdPrimary()
			if err != nil {
				return nil, fmt.Errorf("expected fd after >&: %w", err)
			}
			apply.Redirs = append(apply.Redirs, ast.Redirect{Op: op.Type, Fd: fd, Target: target, FdDup: true})
			continue
		}
		target, err := p.parseCmdPrimary()
		if err != nil {
			return nil, fmt.Errorf("expected redirect target: %w", err)
		}
		apply.Redirs = append(apply.Redirs, ast.Redirect{Op: op.Type, Fd: fd, Target: target})
	}
	return apply, nil
}

func (p *Parser) isStopped() bool {
	if p.cur().Type != ast.TIdent {
		return false
	}
	for _, s := range p.stops {
		if p.cur().Val == s {
			return true
		}
	}
	return false
}

func (p *Parser) parseCmdApply() (*ast.Node, error) {
	head, err := p.parseCmdPrimary()
	if err != nil {
		return nil, err
	}
	// Test command [..] already returns a complete NApply
	if head.Kind == ast.NApply {
		return head, nil
	}
	head, err = p.maybeWordJoin(head)
	if err != nil {
		return nil, err
	}
	args := []*ast.Node{head}
	for !p.isStopped() && (isCmdArgStart(p.cur(), p.prev()) || p.cur().Type == ast.TComma) {
		if p.cur().Type == ast.TComma {
			p.advance()
			continue
		}
		arg, err := p.parseCmdArg()
		if err != nil {
			break
		}
		args = append(args, arg)
	}
	return &ast.Node{Kind: ast.NApply, Children: args}, nil
}

func isCmdArgStart(t ast.Token, prev ast.Token) bool {
	switch t.Type {
	case ast.TIdent:
		return true
	case ast.TInt, ast.TFloat, ast.TString, ast.TStringStart, ast.TAtom,
		ast.TDollar, ast.TDollarLParen, ast.TDollarLBrace, ast.TDollarDLParen,
		ast.TSpecialVar,
		ast.TLParen, ast.TBackslash,
		ast.TTilde, ast.TLBracket, ast.TRBracket,
		ast.TLBrace:
		return true
	case ast.TStar:
		return !t.SpaceAfter
	case ast.TBang:
		return true
	case ast.TAssign:
		return true // = is a valid command arg (e.g., test x = y)
	case ast.TMinus, ast.TPlus, ast.TPercent, ast.TSlash,
		ast.TDot, ast.THash, ast.TAt, ast.TColon:
		return !t.SpaceAfter
	}
	return false
}

func isCmdWordContinue(tt ast.TokenType) bool {
	switch tt {
	case ast.TDollar, ast.TDollarLParen, ast.TDollarLBrace, ast.TDollarDLParen,
		ast.TSpecialVar, ast.TIdent, ast.TInt, ast.TString, ast.TStringStart:
		return true
	}
	return false
}

func (p *Parser) maybeWordJoin(first *ast.Node) (*ast.Node, error) {
	if !p.prev().SpaceAfter && isCmdWordContinue(p.cur().Type) {
		parts := []*ast.Node{first}
		for !p.prev().SpaceAfter && isCmdWordContinue(p.cur().Type) {
			part, err := p.parseCmdPrimary()
			if err != nil {
				break
			}
			parts = append(parts, part)
		}
		return &ast.Node{Kind: ast.NInterpStr, Children: parts}, nil
	}
	return first, nil
}

func isCmdCompoundContinue(tt ast.TokenType) bool {
	switch tt {
	case ast.TIdent, ast.TInt, ast.TFloat, ast.TDot, ast.TSlash,
		ast.TMinus, ast.TPlus, ast.TPercent, ast.THash, ast.TAt,
		ast.TColon, ast.TStar, ast.TAssign, ast.TBang:
		return true
	}
	return false
}

// parseCmdArg parses a command argument (non-head position).
// { in arg position is a tuple, not a group.
func (p *Parser) parseCmdArg() (*ast.Node, error) {
	if p.cur().Type == ast.TLBrace {
		return p.parseTuple()
	}
	// ( in arg position → expression context
	if p.cur().Type == ast.TLParen {
		p.advance()
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(ast.TRParen); err != nil {
			return nil, fmt.Errorf("expected ) after expression")
		}
		return expr, nil
	}
	node, err := p.parseCmdPrimary()
	if err != nil {
		return nil, err
	}
	return p.maybeWordJoin(node)
}

func (p *Parser) parseCmdPrimary() (*ast.Node, error) {
	t := p.cur()
	switch {
	case t.Type == ast.TIdent:
		p.advance()
		if !t.SpaceAfter && p.cur().Type == ast.TLParen {
			return p.parseCallArgs(t)
		}
		if !t.SpaceAfter && isCmdCompoundContinue(p.cur().Type) {
			node, err := p.parseCmdCompoundWord(t)
			if err != nil {
				return nil, err
			}
			// Compound word followed by ( → paren call (e.g., List.append(...))
			if !p.prev().SpaceAfter && p.cur().Type == ast.TLParen {
				return p.parseCallArgsWithCallee(node)
			}
			return p.classifyCompoundWord(node), nil
		}
		return &ast.Node{Kind: ast.NIdent, Tok: t}, nil

	case t.Type == ast.TInt:
		p.advance()
		if !t.SpaceAfter && (p.cur().Type == ast.TDot || p.cur().Type == ast.TColon) {
			node, err := p.parseCmdCompoundWord(t)
			if err != nil {
				return nil, err
			}
			return p.classifyCompoundWord(node), nil
		}
		return &ast.Node{Kind: ast.NLit, Tok: t}, nil

	case t.Type == ast.TFloat:
		p.advance()
		return &ast.Node{Kind: ast.NLit, Tok: t}, nil

	case t.Type == ast.TString:
		p.advance()
		return &ast.Node{Kind: ast.NLit, Tok: t}, nil

	case t.Type == ast.TStringStart:
		return p.parseInterpString()

	case t.Type == ast.TAtom:
		p.advance()
		return &ast.Node{Kind: ast.NAtom, Tok: t}, nil

	case t.Type == ast.TDollar:
		p.advance()
		// $"string" — dollar string (C-style escapes already handled by lexer)
		if p.cur().Type == ast.TString || p.cur().Type == ast.TStringStart {
			return p.parseCmdPrimary()
		}
		if p.cur().Type == ast.TIdent {
			name := p.advance()
			node := &ast.Node{Kind: ast.NVarRef, Tok: name}
			for !p.prev().SpaceAfter && p.cur().Type == ast.TDot {
				p.advance()
				field, err := p.expect(ast.TIdent)
				if err != nil {
					return nil, fmt.Errorf("expected field name after .")
				}
				node = &ast.Node{Kind: ast.NAccess, Tok: field, Children: []*ast.Node{node}}
			}
			return node, nil
		}
		return nil, fmt.Errorf("expected variable name after $")

	case t.Type == ast.TSpecialVar:
		p.advance()
		return &ast.Node{Kind: ast.NVarRef, Tok: t}, nil

	case t.Type == ast.TDollarLParen:
		return p.parseCmdSub()

	case t.Type == ast.TDollarLBrace:
		return p.parseParamExpand()

	case t.Type == ast.TDollarDLParen:
		return p.parseArithSub()

	case t.Type == ast.TLParen:
		p.advance()
		block, err := p.parseBlockUntil()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(ast.TRParen); err != nil {
			return nil, fmt.Errorf("expected )")
		}
		if len(block.Children) == 1 {
			return block.Children[0], nil
		}
		return block, nil

	case t.Type == ast.TLBracket:
		if !t.SpaceAfter {
			// Adjacent [1,2,3] — list expression
			return p.parseList()
		}
		return p.parseTestCommand()

	case t.Type == ast.TRBracket:
		p.advance()
		return &ast.Node{Kind: ast.NLit, Tok: ast.Token{Type: ast.TString, Val: "]", Pos: t.Pos}}, nil

	case t.Type == ast.TLBrace:
		return p.parseGroup()

	case t.Type == ast.TMinus:
		return p.parseCmdFlag()

	case t.Type == ast.TBackslash:
		return p.parseLambda()

	case t.Type == ast.TTilde || t.Type == ast.TSlash:
		return p.parsePath()

	case t.Type == ast.THeredoc:
		return p.parseHeredoc()

	case t.Type == ast.TPercent && p.peek(1).Type == ast.TLBrace:
		return p.parseMapLiteral()

	case t.Type == ast.TDot || t.Type == ast.THash || t.Type == ast.TAt || t.Type == ast.TAssign ||
		t.Type == ast.TStar || t.Type == ast.TColon ||
		t.Type == ast.TPlus || t.Type == ast.TPercent || t.Type == ast.TBang:
		p.advance()
		if !t.SpaceAfter && isCmdCompoundContinue(p.cur().Type) {
			node, err := p.parseCmdCompoundWord(t)
			if err != nil {
				return nil, err
			}
			return p.classifyCompoundWord(node), nil
		}
		return &ast.Node{Kind: ast.NLit, Tok: t}, nil

	default:
		return nil, fmt.Errorf("unexpected token %d (%q) at pos %d in command context", t.Type, t.Val, t.Pos)
	}
}

func (p *Parser) classifyCompoundWord(node *ast.Node) *ast.Node {
	val := node.Tok.Val
	hasColon := false
	hasDot := false
	hasDoubleColon := false
	for i := 0; i < len(val); i++ {
		if val[i] == ':' {
			hasColon = true
			if i+1 < len(val) && val[i+1] == ':' {
				hasDoubleColon = true
			}
		} else if val[i] == '.' {
			hasDot = true
		}
	}
	if hasDoubleColon || (hasColon && !hasDot) {
		node.Kind = ast.NIPv6
	} else if hasDot && !hasColon {
		// Check if all segments are digits (IPv4) vs mixed (filename)
		isIP := true
		for _, ch := range val {
			if ch != '.' && (ch < '0' || ch > '9') {
				isIP = false
				break
			}
		}
		if isIP {
			node.Kind = ast.NIPv4
		}
	}
	return node
}

func (p *Parser) parseCmdCompoundWord(start ast.Token) (*ast.Node, error) {
	val := start.Val
	for !p.prev().SpaceAfter && isCmdCompoundContinue(p.cur().Type) {
		val += p.advance().Val
	}
	return &ast.Node{Kind: ast.NLit, Tok: ast.Token{Type: ast.TString, Val: val, Pos: start.Pos}}, nil
}

func (p *Parser) parseTestCommand() (*ast.Node, error) {
	open := p.advance() // skip [
	head := &ast.Node{Kind: ast.NIdent, Tok: ast.Token{Type: ast.TIdent, Val: "[", Pos: open.Pos}}
	args := []*ast.Node{head}
	for p.cur().Type != ast.TRBracket && p.cur().Type != ast.TEOF &&
		p.cur().Type != ast.TNewline && p.cur().Type != ast.TSemicolon {
		t := p.cur()
		switch t.Type {
		case ast.TDollar, ast.TSpecialVar, ast.TDollarLParen, ast.TDollarLBrace, ast.TDollarDLParen:
			arg, err := p.parseCmdPrimary()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
		case ast.TString, ast.TInt, ast.TFloat:
			p.advance()
			args = append(args, &ast.Node{Kind: ast.NLit, Tok: t})
		case ast.TStringStart:
			arg, err := p.parseInterpString()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
		case ast.TMinus:
			p.advance()
			// Join -flag (e.g., -gt, -eq, -le, -z, -n, -f, -d, -e)
			if !t.SpaceAfter && p.cur().Type == ast.TIdent {
				name := p.advance()
				args = append(args, &ast.Node{Kind: ast.NIdent, Tok: ast.Token{Type: ast.TIdent, Val: "-" + name.Val, Pos: t.Pos}})
			} else {
				args = append(args, &ast.Node{Kind: ast.NIdent, Tok: ast.Token{Type: ast.TIdent, Val: t.Val, Pos: t.Pos}})
			}
		default:
			p.advance()
			args = append(args, &ast.Node{Kind: ast.NIdent, Tok: ast.Token{Type: ast.TIdent, Val: t.Val, Pos: t.Pos}})
		}
	}
	if p.cur().Type == ast.TRBracket {
		close := p.advance()
		args = append(args, &ast.Node{Kind: ast.NLit, Tok: ast.Token{Type: ast.TString, Val: "]", Pos: close.Pos}})
	}
	return &ast.Node{Kind: ast.NApply, Children: args}, nil
}

func (p *Parser) parseCmdFlag() (*ast.Node, error) {
	t := p.advance()
	pos := t.Pos
	if p.cur().Type == ast.TMinus && !p.prev().SpaceAfter {
		p.advance()
		if p.cur().Type == ast.TIdent && !p.prev().SpaceAfter {
			name := p.advance()
			return &ast.Node{Kind: ast.NFlag, Tok: ast.Token{Type: ast.TIdent, Val: "--" + name.Val, Pos: pos}}, nil
		}
		return &ast.Node{Kind: ast.NFlag, Tok: ast.Token{Type: ast.TIdent, Val: "--", Pos: pos}}, nil
	}
	if p.cur().Type == ast.TIdent && !p.prev().SpaceAfter {
		name := p.advance()
		return &ast.Node{Kind: ast.NFlag, Tok: ast.Token{Type: ast.TIdent, Val: "-" + name.Val, Pos: pos}}, nil
	}
	return &ast.Node{Kind: ast.NLit, Tok: t}, nil
}

func (p *Parser) parsePath() (*ast.Node, error) {
	pos := p.cur().Pos
	path := ""
	for p.cur().Type == ast.TSlash || p.cur().Type == ast.TIdent || p.cur().Type == ast.TDot || p.cur().Type == ast.TTilde {
		path += p.advance().Val
		for !p.prev().SpaceAfter && (p.cur().Type == ast.TSlash || p.cur().Type == ast.TIdent || p.cur().Type == ast.TDot || p.cur().Type == ast.TInt || p.cur().Type == ast.TStar || p.cur().Type == ast.TMinus) {
			path += p.advance().Val
		}
	}
	return &ast.Node{Kind: ast.NPath, Tok: ast.Token{Type: ast.TString, Val: path, Pos: pos}}, nil
}

func (p *Parser) parseCmdSub() (*ast.Node, error) {
	p.advance() // skip $(
	block, err := p.parseBlockUntil()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(ast.TRParen); err != nil {
		return nil, fmt.Errorf("expected ) to close $(")
	}
	return &ast.Node{Kind: ast.NCmdSub, Children: block.Children}, nil
}

func (p *Parser) parseParamExpand() (*ast.Node, error) {
	tok := p.advance() // skip ${
	var parts []ast.Token
	depth := 1
	for depth > 0 && p.cur().Type != ast.TEOF {
		if p.cur().Type == ast.TLBrace {
			depth++
		} else if p.cur().Type == ast.TRBrace {
			depth--
			if depth == 0 {
				break
			}
		}
		parts = append(parts, p.advance())
	}
	if _, err := p.expect(ast.TRBrace); err != nil {
		return nil, fmt.Errorf("expected } to close ${")
	}
	val := ""
	for _, pt := range parts {
		val += pt.Val
	}
	return &ast.Node{Kind: ast.NParamExpand, Tok: ast.Token{Type: ast.TString, Val: val, Pos: tok.Pos}}, nil
}

func (p *Parser) parseArithSub() (*ast.Node, error) {
	p.advance() // skip $((
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(ast.TDoubleRParen); err != nil {
		return nil, fmt.Errorf("expected )) to close $((")
	}
	return &ast.Node{Kind: ast.NLit, Children: []*ast.Node{expr}}, nil
}

func (p *Parser) parseHeredoc() (*ast.Node, error) {
	p.advance() // skip <<
	p.advance() // skip delimiter ident (lexer already consumed the content)

	// The lexer emits either:
	// - TString (quoted delimiter, no expansion)
	// - TStringStart ... TStringEnd (unquoted, with expansion)
	if p.cur().Type == ast.TString {
		content := p.advance()
		return &ast.Node{Kind: ast.NLit, Tok: content}, nil
	}
	if p.cur().Type == ast.TStringStart {
		return p.parseInterpString()
	}
	return &ast.Node{Kind: ast.NLit, Tok: ast.Token{Type: ast.TString, Val: ""}}, nil
}

func (p *Parser) parseGroup() (*ast.Node, error) {
	p.advance() // skip {
	block := &ast.Node{Kind: ast.NBlock}
	p.skipNewlines()
	for p.cur().Type != ast.TRBrace && p.cur().Type != ast.TEOF {
		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			block.Children = append(block.Children, stmt)
		}
		p.skipNewlines()
	}
	if _, err := p.expect(ast.TRBrace); err != nil {
		return nil, fmt.Errorf("expected }")
	}
	return block, nil
}

// ============================================================
// EXPRESSION CONTEXT
// ============================================================

// parseExpr: full expression with juxtaposition + commas
func (p *Parser) parseExpr() (*ast.Node, error) {
	head, err := p.parseExprPipe()
	if err != nil {
		return nil, err
	}
	if isCallable(head) {
		var args []*ast.Node
		args = append(args, head)
		for !p.isStopped() && (isExprArgStart(p.cur().Type) || p.cur().Type == ast.TComma) {
			if p.cur().Type == ast.TComma {
				p.advance()
				continue
			}
			arg, err := p.parseExprPipe()
			if err != nil {
				break
			}
			args = append(args, arg)
		}
		if len(args) > 1 {
			return &ast.Node{Kind: ast.NApply, Children: args}, nil
		}
	}
	return head, nil
}

func (p *Parser) parseExprPipe() (*ast.Node, error) {
	left, err := p.parseExprLogicOr()
	if err != nil {
		return nil, err
	}
	for p.cur().Type == ast.TPipeArrow {
		op := p.advance()
		right, err := p.parseExprLogicOr()
		if err != nil {
			return nil, err
		}
		left = &ast.Node{Kind: ast.NPipeFn, Tok: op, Children: []*ast.Node{left, right}}
	}
	return left, nil
}

func (p *Parser) parseExprLogicOr() (*ast.Node, error) {
	left, err := p.parseExprLogicAnd()
	if err != nil {
		return nil, err
	}
	for p.cur().Type == ast.TOr {
		op := p.advance()
		right, err := p.parseExprLogicAnd()
		if err != nil {
			return nil, err
		}
		left = &ast.Node{Kind: ast.NOrList, Tok: op, Children: []*ast.Node{left, right}}
	}
	return left, nil
}

func (p *Parser) parseExprLogicAnd() (*ast.Node, error) {
	left, err := p.parseExprCompare()
	if err != nil {
		return nil, err
	}
	for p.cur().Type == ast.TAnd {
		op := p.advance()
		right, err := p.parseExprCompare()
		if err != nil {
			return nil, err
		}
		left = &ast.Node{Kind: ast.NAndList, Tok: op, Children: []*ast.Node{left, right}}
	}
	return left, nil
}

func (p *Parser) parseExprCompare() (*ast.Node, error) {
	left, err := p.parseExprAdd()
	if err != nil {
		return nil, err
	}
	if !isCompareOp(p.cur().Type) {
		return left, nil
	}
	// First comparison
	op := p.advance()
	right, err := p.parseExprAdd()
	if err != nil {
		return nil, err
	}
	cmp := &ast.Node{Kind: ast.NBinOp, Tok: op, Children: []*ast.Node{left, right}}
	// Chain: a < b < c → (a < b) && (b < c)
	for isCompareOp(p.cur().Type) {
		op2 := p.advance()
		right2, err := p.parseExprAdd()
		if err != nil {
			return nil, err
		}
		cmp2 := &ast.Node{Kind: ast.NBinOp, Tok: op2, Children: []*ast.Node{right, right2}}
		cmp = &ast.Node{Kind: ast.NAndList, Tok: op2, Children: []*ast.Node{cmp, cmp2}}
		right = right2
	}
	return cmp, nil
}

func isCompareOp(tt ast.TokenType) bool {
	return tt == ast.TGt || tt == ast.TLt || tt == ast.TGtEq || tt == ast.TLtEq ||
		tt == ast.TEqEq || tt == ast.TBangEq
}

func (p *Parser) parseExprAdd() (*ast.Node, error) {
	left, err := p.parseExprMul()
	if err != nil {
		return nil, err
	}
	for p.cur().Type == ast.TPlus || p.cur().Type == ast.TMinus {
		op := p.advance()
		right, err := p.parseExprMul()
		if err != nil {
			return nil, err
		}
		left = &ast.Node{Kind: ast.NBinOp, Tok: op, Children: []*ast.Node{left, right}}
	}
	return left, nil
}

func (p *Parser) parseExprMul() (*ast.Node, error) {
	left, err := p.parseExprUnary()
	if err != nil {
		return nil, err
	}
	for p.cur().Type == ast.TStar || p.cur().Type == ast.TSlash || p.cur().Type == ast.TPercent {
		op := p.advance()
		right, err := p.parseExprUnary()
		if err != nil {
			return nil, err
		}
		left = &ast.Node{Kind: ast.NBinOp, Tok: op, Children: []*ast.Node{left, right}}
	}
	return left, nil
}

func (p *Parser) parseExprUnary() (*ast.Node, error) {
	if p.cur().Type == ast.TBang || p.cur().Type == ast.TMinus {
		op := p.advance()
		operand, err := p.parseExprUnary()
		if err != nil {
			return nil, err
		}
		return &ast.Node{Kind: ast.NUnary, Tok: op, Children: []*ast.Node{operand}}, nil
	}
	return p.parseExprPostfix()
}

func (p *Parser) parseExprPostfix() (*ast.Node, error) {
	node, err := p.parseExprPrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.cur().Type == ast.TDot:
			p.advance()
			field, err := p.expect(ast.TIdent)
			if err != nil {
				return nil, fmt.Errorf("expected field name after .")
			}
			node = &ast.Node{Kind: ast.NAccess, Tok: field, Children: []*ast.Node{node}}
		case p.cur().Type == ast.TLParen && !p.prev().SpaceAfter:
			node, err = p.parseCallArgsWithCallee(node)
			if err != nil {
				return nil, err
			}
		default:
			return node, nil
		}
	}
}

func (p *Parser) parseExprPrimary() (*ast.Node, error) {
	t := p.cur()
	switch t.Type {
	case ast.TIdent:
		switch t.Val {
		case "nil", "true", "false":
			p.advance()
			return &ast.Node{Kind: ast.NLit, Tok: t}, nil
		case "fn":
			return p.parseFnDef(true)
		case "match":
			return p.parseMatch()
		case "if":
			return p.parseIf()
		case "try":
			return p.parseTry()
		case "receive":
			return p.parseReceive()
		case "spawn", "send":
			p.advance()
			return &ast.Node{Kind: ast.NIdent, Tok: t}, nil
		default:
			p.advance()
			if !t.SpaceAfter && p.cur().Type == ast.TLParen {
				return p.parseCallArgs(t)
			}
			return &ast.Node{Kind: ast.NIdent, Tok: t}, nil
		}

	case ast.TInt, ast.TFloat, ast.TString:
		p.advance()
		return &ast.Node{Kind: ast.NLit, Tok: t}, nil

	case ast.TStringStart:
		return p.parseInterpString()

	case ast.TAtom:
		p.advance()
		return &ast.Node{Kind: ast.NAtom, Tok: t}, nil

	case ast.TDollar:
		p.advance()
		if p.cur().Type == ast.TIdent {
			name := p.advance()
			return &ast.Node{Kind: ast.NVarRef, Tok: name}, nil
		}
		return nil, fmt.Errorf("expected variable name after $")

	case ast.TSpecialVar:
		p.advance()
		return &ast.Node{Kind: ast.NVarRef, Tok: t}, nil

	case ast.TDollarLParen:
		return p.parseCmdSub()

	case ast.TDollarLBrace:
		return p.parseParamExpand()

	case ast.TDollarDLParen:
		return p.parseArithSub()

	case ast.TLParen:
		p.advance()
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(ast.TRParen); err != nil {
			return nil, fmt.Errorf("expected )")
		}
		return expr, nil

	case ast.TLBracket:
		return p.parseList()

	case ast.TLBrace:
		return p.parseTuple()

	case ast.TBackslash:
		return p.parseLambda()

	case ast.TPercent:
		if p.peek(1).Type == ast.TLBrace {
			return p.parseMapLiteral()
		}
		return nil, fmt.Errorf("unexpected %% in expression")

	case ast.TTilde:
		return p.parsePath()

	case ast.TAmpersand:
		p.advance()
		name, err := p.expect(ast.TIdent)
		if err != nil {
			return nil, fmt.Errorf("expected function name after &")
		}
		return &ast.Node{Kind: ast.NIdent, Tok: ast.Token{Type: ast.TIdent, Val: "&" + name.Val, Pos: t.Pos}}, nil

	default:
		return nil, fmt.Errorf("unexpected token in expression: %d (%q) at pos %d", t.Type, t.Val, t.Pos)
	}
}

func isCallable(n *ast.Node) bool {
	return n.Kind == ast.NIdent || n.Kind == ast.NCall || n.Kind == ast.NAccess
}

func isExprArgStart(tt ast.TokenType) bool {
	switch tt {
	case ast.TIdent, ast.TInt, ast.TFloat, ast.TString, ast.TStringStart, ast.TAtom,
		ast.TDollar, ast.TDollarLParen, ast.TDollarLBrace, ast.TSpecialVar,
		ast.TLParen, ast.TLBracket, ast.TLBrace, ast.TBackslash,
		ast.TAmpersand, ast.TPercent:
		return true
	}
	return false
}

// ============================================================
// Shared helpers
// ============================================================

func (p *Parser) parseInterpString() (*ast.Node, error) {
	p.advance() // skip TStringStart
	node := &ast.Node{Kind: ast.NInterpStr}
	for p.cur().Type != ast.TStringEnd && p.cur().Type != ast.TEOF {
		t := p.cur()
		switch t.Type {
		case ast.TString:
			p.advance()
			node.Children = append(node.Children, &ast.Node{Kind: ast.NLit, Tok: t})
		case ast.TDollar:
			p.advance()
			if p.cur().Type == ast.TIdent {
				name := p.advance()
				node.Children = append(node.Children, &ast.Node{Kind: ast.NVarRef, Tok: name})
			}
		case ast.TSpecialVar:
			p.advance()
			node.Children = append(node.Children, &ast.Node{Kind: ast.NVarRef, Tok: t})
		case ast.TDollarLParen:
			sub, err := p.parseCmdSub()
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, sub)
		case ast.TDollarLBrace:
			pe, err := p.parseParamExpand()
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, pe)
		case ast.TDollarDLParen:
			arith, err := p.parseArithSub()
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, arith)
		case ast.THashLBrace:
			p.advance() // skip #{
			expr, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, expr)
			if _, err := p.expect(ast.TRBrace); err != nil {
				return nil, fmt.Errorf("expected } after #{expr}")
			}
		default:
			p.advance()
			node.Children = append(node.Children, &ast.Node{Kind: ast.NLit, Tok: t})
		}
	}
	if p.cur().Type == ast.TStringEnd {
		p.advance()
	}
	return node, nil
}

func (p *Parser) parseCallArgsWithCallee(callee *ast.Node) (*ast.Node, error) {
	p.advance() // skip (
	var args []*ast.Node
	for p.cur().Type != ast.TRParen && p.cur().Type != ast.TEOF {
		p.skipNewlines()
		arg, err := p.parseExprPipe()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.cur().Type == ast.TComma {
			p.advance()
		}
	}
	if _, err := p.expect(ast.TRParen); err != nil {
		return nil, fmt.Errorf("expected ) in call")
	}
	children := append([]*ast.Node{callee}, args...)
	return &ast.Node{Kind: ast.NCall, Tok: callee.Tok, Children: children}, nil
}

func (p *Parser) parseCallArgs(name ast.Token) (*ast.Node, error) {
	p.advance() // skip (
	var args []*ast.Node
	for p.cur().Type != ast.TRParen && p.cur().Type != ast.TEOF {
		p.skipNewlines()
		arg, err := p.parseExprPipe()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.cur().Type == ast.TComma {
			p.advance()
		}
	}
	if _, err := p.expect(ast.TRParen); err != nil {
		return nil, fmt.Errorf("expected ) in call")
	}
	callee := &ast.Node{Kind: ast.NIdent, Tok: name}
	children := append([]*ast.Node{callee}, args...)
	return &ast.Node{Kind: ast.NCall, Tok: name, Children: children}, nil
}

func (p *Parser) parseList() (*ast.Node, error) {
	p.advance() // skip [
	var elems []*ast.Node
	for p.cur().Type != ast.TRBracket && p.cur().Type != ast.TEOF {
		p.skipNewlines()
		elem, err := p.parseExprPipe()
		if err != nil {
			return nil, err
		}
		elems = append(elems, elem)
		if p.cur().Type == ast.TPipe {
			p.advance()
			tail, err := p.parseExprPipe()
			if err != nil {
				return nil, err
			}
			heads := make([]*ast.Node, len(elems))
			copy(heads, elems)
			children := append(heads, tail)
			if _, err := p.expect(ast.TRBracket); err != nil {
				return nil, fmt.Errorf("expected ] after cons tail")
			}
			return &ast.Node{Kind: ast.NCons, Children: children}, nil
		}
		if p.cur().Type == ast.TComma {
			p.advance()
		}
	}
	if _, err := p.expect(ast.TRBracket); err != nil {
		return nil, fmt.Errorf("expected ]")
	}
	return &ast.Node{Kind: ast.NList, Children: elems}, nil
}

func (p *Parser) parseTuple() (*ast.Node, error) {
	p.advance() // skip {
	var elems []*ast.Node
	for p.cur().Type != ast.TRBrace && p.cur().Type != ast.TEOF {
		p.skipNewlines()
		elem, err := p.parseExprPipe()
		if err != nil {
			return nil, err
		}
		elems = append(elems, elem)
		if p.cur().Type == ast.TComma {
			p.advance()
		}
	}
	if _, err := p.expect(ast.TRBrace); err != nil {
		return nil, fmt.Errorf("expected }")
	}
	return &ast.Node{Kind: ast.NTuple, Children: elems}, nil
}

func (p *Parser) parseMapLiteral() (*ast.Node, error) {
	p.advance() // skip %
	p.advance() // skip {
	var elems []*ast.Node
	for p.cur().Type != ast.TRBrace && p.cur().Type != ast.TEOF {
		p.skipNewlines()
		if p.cur().Type == ast.TRBrace {
			break
		}
		key, err := p.parseExprPrimary()
		if err != nil {
			return nil, err
		}
		if p.cur().Type == ast.TColon {
			p.advance()
		}
		val, err := p.parseExprPipe()
		if err != nil {
			return nil, err
		}
		elems = append(elems, key, val)
		if p.cur().Type == ast.TComma {
			p.advance()
		}
	}
	if _, err := p.expect(ast.TRBrace); err != nil {
		return nil, fmt.Errorf("expected } in map")
	}
	return &ast.Node{Kind: ast.NMap, Children: elems}, nil
}

func (p *Parser) parseLambda() (*ast.Node, error) {
	p.advance() // skip backslash
	var params []*ast.Node
	for p.cur().Type == ast.TIdent {
		t := p.advance()
		params = append(params, &ast.Node{Kind: ast.NIdent, Tok: t})
		if p.cur().Type == ast.TComma {
			p.advance()
		}
	}
	if _, err := p.expect(ast.TArrow); err != nil {
		return nil, fmt.Errorf("expected -> in lambda")
	}
	p.skipNewlines()
	body, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	children := append(params, body)
	return &ast.Node{Kind: ast.NLambda, Children: children}, nil
}

// ============================================================
// Control flow
// ============================================================

func (p *Parser) parseFnDef(exprCtx bool) (*ast.Node, error) {
	p.advance() // skip fn

	// Anonymous fn: fn do ... end
	if p.is("do") {
		return p.parseAnonFn(nil)
	}

	// In expression context, idents after fn are params, not a name.
	// fn x do x * 2 end → anonymous fn with param x
	// fn x, y do x + y end → anonymous fn with params x, y
	if exprCtx {
		var params []*ast.Node
		for !p.is("do") && p.cur().Type != ast.TEOF {
			param, err := p.parseExprPrimary()
			if err != nil {
				break
			}
			params = append(params, param)
			if p.cur().Type == ast.TComma {
				p.advance()
			}
		}
		return p.parseAnonFn(params)
	}

	name, err := p.expect(ast.TIdent)
	if err != nil {
		return nil, fmt.Errorf("expected function name after fn")
	}

	var params []*ast.Node
	for !p.is("do") && !p.is("when") && p.cur().Type != ast.TEOF &&
		p.cur().Type != ast.TNewline && p.cur().Type != ast.TSemicolon {
		param, err := p.parseExprPrimary()
		if err != nil {
			break
		}
		params = append(params, param)
		if p.cur().Type == ast.TComma {
			p.advance()
		}
	}

	var guard *ast.Node
	if p.is("when") {
		p.advance()
		guard, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}

	if _, err := p.expectWord("do"); err != nil {
		return nil, fmt.Errorf("expected 'do' in fn definition")
	}

	p.skipNewlines()
	if p.cur().Type != ast.TEOF && !p.is("end") && p.looksLikeClauseBlock() {
		return p.parseFnClauseBlock(name)
	}

	body, err := p.parseBlockUntil("end")
	if err != nil {
		return nil, err
	}
	if _, err := p.expectWord("end"); err != nil {
		return nil, fmt.Errorf("expected 'end' in fn definition")
	}

	children := []*ast.Node{{Kind: ast.NIdent, Tok: name}}
	children = append(children, params...)
	if guard != nil {
		children = append(children, &ast.Node{Kind: ast.NUnary, Tok: ast.Token{Type: ast.TIdent, Val: "when"}, Children: []*ast.Node{guard}})
	}
	children = append(children, body)
	return &ast.Node{Kind: ast.NFnDef, Tok: name, Children: children}, nil
}

func (p *Parser) parseAnonFn(params []*ast.Node) (*ast.Node, error) {
	p.advance() // skip do
	p.skipNewlines()
	if p.looksLikeClauseBlock() {
		return p.parseFnClauseBlock(ast.Token{Type: ast.TIdent, Val: "<anon>"})
	}
	body, err := p.parseBlockUntil("end")
	if err != nil {
		return nil, err
	}
	if _, err := p.expectWord("end"); err != nil {
		return nil, fmt.Errorf("expected 'end' in anonymous fn")
	}
	tok := ast.Token{Type: ast.TIdent, Val: "<anon>"}
	children := []*ast.Node{{Kind: ast.NIdent, Tok: tok}}
	children = append(children, params...)
	children = append(children, body)
	return &ast.Node{Kind: ast.NFnDef, Tok: tok, Children: children}, nil
}

// looksLikeTuple scans forward from { to decide if this is a tuple {a, b} or
// a brace group { cmd; }. Tuples use commas between elements at depth 1.
func (p *Parser) looksLikeTuple() bool {
	depth := 1
	for i := p.pos + 1; i < len(p.tokens); i++ {
		switch p.tokens[i].Type {
		case ast.TLBrace, ast.TLParen, ast.TLBracket:
			depth++
		case ast.TRBrace:
			depth--
			if depth == 0 {
				return false // hit closing } without comma → group
			}
		case ast.TRParen, ast.TRBracket:
			depth--
		case ast.TComma:
			if depth == 1 {
				return true // comma at top level → tuple
			}
		case ast.TSemicolon, ast.TNewline:
			if depth == 1 {
				return false // statement separator → group
			}
		case ast.TEOF:
			return false
		}
	}
	return false
}

func (p *Parser) looksLikeClauseBlock() bool {
	for i := p.pos; i < len(p.tokens); i++ {
		tt := p.tokens[i].Type
		if tt == ast.TBackslash {
			return false // -> after \ is a lambda, not a clause
		}
		if tt == ast.TArrow {
			return true
		}
		if tt == ast.TNewline || tt == ast.TSemicolon || tt == ast.TEOF {
			return false
		}
		if p.tokens[i].Type == ast.TIdent && p.tokens[i].Val == "end" {
			return false
		}
	}
	return false
}

func (p *Parser) parseFnClauseBlock(name ast.Token) (*ast.Node, error) {
	savedStops := p.stops
	p.stops = append(p.stops, "end", "when")
	var clauses []ast.Clause
	for p.cur().Type != ast.TEOF && !p.is("end") {
		p.skipNewlines()
		if p.is("end") {
			break
		}
		pattern, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		var guard *ast.Node
		if p.is("when") {
			p.advance()
			guard, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
		if _, err := p.expect(ast.TArrow); err != nil {
			return nil, fmt.Errorf("expected -> in fn clause")
		}
		body, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, ast.Clause{Pattern: pattern, Guard: guard, Body: body})
		p.skipNewlines()
	}
	p.stops = savedStops
	if _, err := p.expectWord("end"); err != nil {
		return nil, fmt.Errorf("expected 'end' in fn clause block")
	}
	return &ast.Node{
		Kind:     ast.NFnDef,
		Tok:      name,
		Children: []*ast.Node{{Kind: ast.NIdent, Tok: name}},
		Clauses:  clauses,
	}, nil
}

func (p *Parser) parseIf() (*ast.Node, error) {
	p.advance() // skip if
	cond, err := p.parseIfCondition()
	if err != nil {
		return nil, err
	}

	if p.cur().Type == ast.TSemicolon {
		p.advance()
	}
	p.skipNewlines()

	if !p.is("do") && !p.is("then") {
		return nil, fmt.Errorf("expected 'do' or 'then' after if condition, got %q", p.cur().Val)
	}
	useFi := p.is("then")
	p.advance()

	endWord := "end"
	if useFi {
		endWord = "fi"
	}

	body, err := p.parseBlockUntil(endWord, "else", "elif")
	if err != nil {
		return nil, err
	}

	node := &ast.Node{Kind: ast.NIf, Clauses: []ast.Clause{{Pattern: cond, Body: body}}}

	for p.is("elif") {
		p.advance()
		elifCond, err := p.parseIfCondition()
		if err != nil {
			return nil, err
		}
		if p.cur().Type == ast.TSemicolon {
			p.advance()
		}
		p.skipNewlines()
		if p.is("then") {
			p.advance()
		}
		elifBody, err := p.parseBlockUntil(endWord, "else", "elif")
		if err != nil {
			return nil, err
		}
		node.Clauses = append(node.Clauses, ast.Clause{Pattern: elifCond, Body: elifBody})
	}

	if p.is("else") {
		p.advance()
		p.skipNewlines()
		elseBody, err := p.parseBlockUntil(endWord)
		if err != nil {
			return nil, err
		}
		node.Clauses = append(node.Clauses, ast.Clause{Body: elseBody})
	}

	if p.is("end") || p.is("fi") {
		p.advance()
	} else if p.cur().Type == ast.TEOF {
		return nil, fmt.Errorf("unexpected end of input: expected '%s'", endWord)
	}
	return node, nil
}

func (p *Parser) parseIfCondition() (*ast.Node, error) {
	if p.cur().Type == ast.TLParen {
		p.advance()
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(ast.TRParen); err != nil {
			return nil, fmt.Errorf("expected ) after if condition")
		}
		return expr, nil
	}
	saved := p.stops
	p.stops = append(p.stops, "do", "then")
	result, err := p.parseConditionPipeline()
	p.stops = saved
	if err != nil {
		return nil, err
	}
	if isSingleValue(result) && isExprExtendOp(p.cur().Type) {
		return p.extendAsExpr(result)
	}
	return result, nil
}

func (p *Parser) parseConditionPipeline() (*ast.Node, error) {
	left, err := p.parseConditionLogic()
	if err != nil {
		return nil, err
	}
	for p.cur().Type == ast.TPipe || p.cur().Type == ast.TPipeArrow || p.cur().Type == ast.TPipeAmp {
		op := p.advance()
		p.skipNewlines()
		right, err := p.parseConditionLogic()
		if err != nil {
			return nil, err
		}
		kind := ast.NPipe
		if op.Type == ast.TPipeArrow {
			kind = ast.NPipeFn
		} else if op.Type == ast.TPipeAmp {
			kind = ast.NPipeAmp
		}
		left = &ast.Node{Kind: kind, Tok: op, Children: []*ast.Node{left, right}}
	}
	return left, nil
}

func (p *Parser) parseConditionLogic() (*ast.Node, error) {
	left, err := p.parseConditionForm()
	if err != nil {
		return nil, err
	}
	for p.cur().Type == ast.TAnd || p.cur().Type == ast.TOr {
		op := p.advance()
		p.skipNewlines()
		right, err := p.parseConditionForm()
		if err != nil {
			return nil, err
		}
		kind := ast.NAndList
		if op.Type == ast.TOr {
			kind = ast.NOrList
		}
		left = &ast.Node{Kind: kind, Tok: op, Children: []*ast.Node{left, right}}
	}
	return left, nil
}

func (p *Parser) parseConditionForm() (*ast.Node, error) {
	if p.cur().Type == ast.TBang {
		op := p.advance()
		child, err := p.parseConditionForm()
		if err != nil {
			return nil, err
		}
		return &ast.Node{Kind: ast.NUnary, Tok: op, Children: []*ast.Node{child}}, nil
	}
	head, err := p.parseCmdPrimary()
	if err != nil {
		return nil, err
	}
	args := []*ast.Node{head}
	for p.isConditionArgStart() {
		arg, err := p.parseCmdPrimary()
		if err != nil {
			break
		}
		args = append(args, arg)
	}
	if len(args) == 1 {
		return head, nil
	}
	return &ast.Node{Kind: ast.NApply, Children: args}, nil
}

func (p *Parser) isConditionArgStart() bool {
	t := p.cur()
	if t.Type == ast.TSemicolon || t.Type == ast.TNewline || t.Type == ast.TEOF {
		return false
	}
	if p.isStopped() {
		return false
	}
	return isCmdArgStart(t, p.prev())
}

func (p *Parser) parseFor() (*ast.Node, error) {
	p.advance() // skip for
	varName, err := p.expect(ast.TIdent)
	if err != nil {
		return nil, fmt.Errorf("expected variable name after for")
	}
	if _, err := p.expectWord("in"); err != nil {
		return nil, fmt.Errorf("expected 'in' in for loop")
	}
	var items []*ast.Node
	for p.cur().Type != ast.TSemicolon && !p.is("do") &&
		p.cur().Type != ast.TNewline && p.cur().Type != ast.TEOF {
		item, err := p.parseCmdPrimary()
		if err != nil {
			break
		}
		items = append(items, item)
	}
	if p.cur().Type == ast.TSemicolon {
		p.advance()
	}
	p.skipNewlines()
	if p.is("do") {
		p.advance()
	}
	body, err := p.parseBlockUntil("done", "end")
	if err != nil {
		return nil, err
	}
	if p.is("done") || p.is("end") {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'done' or 'end' in for loop")
	}
	iterNode := &ast.Node{Kind: ast.NList, Children: items}
	return &ast.Node{
		Kind:     ast.NFor,
		Tok:      varName,
		Children: []*ast.Node{{Kind: ast.NIdent, Tok: varName}, iterNode, body},
	}, nil
}

func (p *Parser) parseWhile() (*ast.Node, error) {
	p.advance()
	cond, err := p.parseIfCondition()
	if err != nil {
		return nil, err
	}
	if p.cur().Type == ast.TSemicolon {
		p.advance()
	}
	p.skipNewlines()
	if p.is("do") {
		p.advance()
	}
	body, err := p.parseBlockUntil("done", "end")
	if err != nil {
		return nil, err
	}
	if p.is("done") || p.is("end") {
		p.advance()
	} else {
		return nil, fmt.Errorf("expected 'done' or 'end' in while loop")
	}
	return &ast.Node{Kind: ast.NWhile, Children: []*ast.Node{cond, body}}, nil
}

func (p *Parser) parseUntil() (*ast.Node, error) {
	p.advance()
	cond, err := p.parseIfCondition()
	if err != nil {
		return nil, err
	}
	if p.cur().Type == ast.TSemicolon {
		p.advance()
	}
	p.skipNewlines()
	if p.is("do") {
		p.advance()
	}
	body, err := p.parseBlockUntil("done", "end")
	if err != nil {
		return nil, err
	}
	if p.is("done") || p.is("end") {
		p.advance()
	}
	return &ast.Node{Kind: ast.NWhile, Tok: ast.Token{Val: "until"}, Children: []*ast.Node{cond, body}}, nil
}

func (p *Parser) parseCase() (*ast.Node, error) {
	p.advance() // skip case
	subject, err := p.parseCmdPrimary()
	if err != nil {
		return nil, err
	}
	if _, err := p.expectWord("in"); err != nil {
		return nil, fmt.Errorf("expected 'in' after case subject")
	}
	p.skipNewlines()

	var clauses []ast.Clause
	for !p.is("esac") && p.cur().Type != ast.TEOF {
		p.skipNewlines()
		if p.is("esac") {
			break
		}
		patternVal := ""
		for p.cur().Type != ast.TRParen && p.cur().Type != ast.TEOF &&
			p.cur().Type != ast.TNewline && !p.is("esac") {
			patternVal += p.advance().Val
		}
		if p.cur().Type == ast.TRParen {
			p.advance()
		}
		p.skipNewlines()

		bodyBlock := &ast.Node{Kind: ast.NBlock}
		for !p.is("esac") && p.cur().Type != ast.TDoubleSemicolon && p.cur().Type != ast.TEOF {
			stmt, err := p.parseStatement()
			if err != nil {
				return nil, err
			}
			if stmt != nil {
				bodyBlock.Children = append(bodyBlock.Children, stmt)
			}
			p.skipNewlines()
		}
		if p.cur().Type == ast.TDoubleSemicolon {
			p.advance()
		}

		pattern := &ast.Node{Kind: ast.NLit, Tok: ast.Token{Type: ast.TString, Val: patternVal}}
		clauses = append(clauses, ast.Clause{Pattern: pattern, Body: bodyBlock})
		p.skipNewlines()
	}

	if _, err := p.expectWord("esac"); err != nil {
		return nil, fmt.Errorf("expected 'esac'")
	}
	return &ast.Node{Kind: ast.NCase, Children: []*ast.Node{subject}, Clauses: clauses}, nil
}

func (p *Parser) parseMatch() (*ast.Node, error) {
	p.advance() // skip match
	subject, err := p.parseExprPipe()
	if err != nil {
		return nil, err
	}
	if _, err := p.expectWord("do"); err != nil {
		return nil, fmt.Errorf("expected 'do' in match")
	}
	var clauses []ast.Clause
	savedStops := p.stops
	p.stops = append(p.stops, "end", "when")
	p.skipNewlines()
	for !p.is("end") && p.cur().Type != ast.TEOF {
		pattern, err := p.parseExpr()
		if err != nil {
			p.stops = savedStops
			return nil, err
		}
		var guard *ast.Node
		if p.is("when") {
			p.advance()
			guard, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
		if _, err := p.expect(ast.TArrow); err != nil {
			return nil, fmt.Errorf("expected -> in match clause")
		}
		body, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, ast.Clause{Pattern: pattern, Guard: guard, Body: body})
		p.skipNewlines()
	}
	p.stops = savedStops
	if _, err := p.expectWord("end"); err != nil {
		return nil, fmt.Errorf("expected 'end' in match")
	}
	return &ast.Node{Kind: ast.NMatch, Children: []*ast.Node{subject}, Clauses: clauses}, nil
}

func (p *Parser) parseDefModule() (*ast.Node, error) {
	p.advance() // skip defmodule
	name, err := p.expect(ast.TIdent)
	if err != nil {
		return nil, fmt.Errorf("expected module name")
	}
	if _, err := p.expectWord("do"); err != nil {
		return nil, fmt.Errorf("expected 'do' in defmodule")
	}
	body, err := p.parseBlockUntil("end")
	if err != nil {
		return nil, err
	}
	if _, err := p.expectWord("end"); err != nil {
		return nil, fmt.Errorf("expected 'end' in defmodule")
	}
	return &ast.Node{Kind: ast.NDefModule, Tok: name, Children: []*ast.Node{{Kind: ast.NIdent, Tok: name}, body}}, nil
}

func (p *Parser) parseUseImport() (*ast.Node, error) {
	tok := p.advance()
	name, err := p.expect(ast.TIdent)
	if err != nil {
		return nil, fmt.Errorf("expected module name after %s", tok.Val)
	}
	return &ast.Node{Kind: ast.NUseImport, Tok: tok, Children: []*ast.Node{
		{Kind: ast.NIdent, Tok: name},
	}}, nil
}

func (p *Parser) parseReceive() (*ast.Node, error) {
	tok := p.advance() // skip receive

	// Optional timeout before do: receive TIMEOUT do ... after BODY end
	var timeoutNode *ast.Node
	if !p.is("do") {
		t, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		timeoutNode = t
	}

	if _, err := p.expectWord("do"); err != nil {
		return nil, fmt.Errorf("expected 'do' in receive")
	}
	var clauses []ast.Clause
	savedStops := p.stops
	p.stops = append(p.stops, "end", "after")
	p.skipNewlines()
	for !p.is("end") && !p.is("after") && p.cur().Type != ast.TEOF {
		pattern, err := p.parseExpr()
		if err != nil {
			p.stops = savedStops
			return nil, err
		}
		if _, err := p.expect(ast.TArrow); err != nil {
			p.stops = savedStops
			return nil, fmt.Errorf("expected -> in receive clause")
		}
		body, err := p.parseStatement()
		if err != nil {
			p.stops = savedStops
			return nil, err
		}
		clauses = append(clauses, ast.Clause{Pattern: pattern, Body: body})
		// Handle semicolons for multi-statement clause bodies
		for p.cur().Type == ast.TSemicolon {
			p.advance()
			if p.is("end") || p.is("after") || p.cur().Type == ast.TNewline || p.cur().Type == ast.TEOF {
				break
			}
			next, err := p.parseStatement()
			if err != nil {
				p.stops = savedStops
				return nil, err
			}
			// Wrap in block if needed
			if body.Kind == ast.NBlock {
				body.Children = append(body.Children, next)
			} else {
				block := &ast.Node{Kind: ast.NBlock, Children: []*ast.Node{body, next}}
				clauses[len(clauses)-1].Body = block
				body = block
			}
		}
		p.skipNewlines()
	}
	p.stops = savedStops

	node := &ast.Node{Kind: ast.NReceive, Tok: tok, Clauses: clauses}

	if p.is("after") {
		p.advance()
		p.skipNewlines()
		// after can have timeout -> body OR just body (when timeout is before do)
		if timeoutNode != nil {
			// Timeout already specified, after has just a body
			body, err := p.parseBlockUntil("end")
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, timeoutNode, body)
		} else {
			// after timeout -> body
			timeout, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(ast.TArrow); err != nil {
				return nil, fmt.Errorf("expected -> in after clause")
			}
			p.skipNewlines()
			body, err := p.parseBlockUntil("end")
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, timeout, body)
		}
		p.skipNewlines()
	}

	if _, err := p.expectWord("end"); err != nil {
		return nil, fmt.Errorf("expected 'end' in receive")
	}
	return node, nil
}

func (p *Parser) parseTry() (*ast.Node, error) {
	p.advance() // skip try
	if _, err := p.expectWord("do"); err != nil {
		return nil, fmt.Errorf("expected 'do' in try")
	}
	body, err := p.parseBlockUntil("rescue")
	if err != nil {
		return nil, err
	}
	if _, err := p.expectWord("rescue"); err != nil {
		return nil, fmt.Errorf("expected 'rescue' in try")
	}
	p.skipNewlines()
	var clauses []ast.Clause
	for !p.is("end") && !p.is("after") && p.cur().Type != ast.TEOF {
		pattern, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(ast.TArrow); err != nil {
			return nil, fmt.Errorf("expected -> in rescue clause")
		}
		handler, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, ast.Clause{Pattern: pattern, Body: handler})
		p.skipNewlines()
	}
	if p.is("end") {
		p.advance()
	}
	return &ast.Node{
		Kind:     ast.NTry,
		Children: []*ast.Node{body},
		Clauses:  clauses,
	}, nil
}

// ============================================================
// Expression extension
// ============================================================

func isSingleValue(n *ast.Node) bool {
	switch n.Kind {
	case ast.NIdent, ast.NLit, ast.NAtom, ast.NVarRef, ast.NCall, ast.NAccess,
		ast.NBinOp, ast.NUnary, ast.NList, ast.NTuple, ast.NLambda, ast.NCmdSub,
		ast.NParamExpand:
		return true
	case ast.NApply:
		return len(n.Children) == 1
	}
	return false
}

func isExprExtendOp(tt ast.TokenType) bool {
	switch tt {
	case ast.TPlus, ast.TMinus, ast.TStar, ast.TSlash, ast.TPercent,
		ast.TEqEq, ast.TBangEq, ast.TGt, ast.TLt, ast.TGtEq, ast.TLtEq,
		ast.TPipeArrow, ast.TDot:
		return true
	}
	return false
}

func (p *Parser) extendAsExpr(left *ast.Node) (*ast.Node, error) {
	if left.Kind == ast.NApply && len(left.Children) == 1 {
		left = left.Children[0]
	}
	node, err := p.parseExprBinFrom(left)
	if err != nil {
		return nil, err
	}
	for p.cur().Type == ast.TPipeArrow {
		op := p.advance()
		right, err := p.parseExprLogicOr()
		if err != nil {
			return nil, err
		}
		node = &ast.Node{Kind: ast.NPipeFn, Tok: op, Children: []*ast.Node{node, right}}
	}
	return node, nil
}

func (p *Parser) parseExprBinFrom(left *ast.Node) (*ast.Node, error) {
	if isCompareOp(p.cur().Type) {
		op := p.advance()
		right, err := p.parseExprAdd()
		if err != nil {
			return nil, err
		}
		return &ast.Node{Kind: ast.NBinOp, Tok: op, Children: []*ast.Node{left, right}}, nil
	}
	for p.cur().Type == ast.TPlus || p.cur().Type == ast.TMinus {
		op := p.advance()
		right, err := p.parseExprMul()
		if err != nil {
			return nil, err
		}
		left = &ast.Node{Kind: ast.NBinOp, Tok: op, Children: []*ast.Node{left, right}}
	}
	for p.cur().Type == ast.TStar || p.cur().Type == ast.TSlash || p.cur().Type == ast.TPercent {
		op := p.advance()
		right, err := p.parseExprUnary()
		if err != nil {
			return nil, err
		}
		left = &ast.Node{Kind: ast.NBinOp, Tok: op, Children: []*ast.Node{left, right}}
	}
	return left, nil
}
