package ast

import "fmt"

type TokenType byte

const (
	// Identifiers and literals
	TIdent  TokenType = iota // bare identifier [a-zA-Z_][a-zA-Z0-9_]* (not a keyword)
	TInt                     // integer literal
	TFloat                   // float literal (e.g. 3.14)
	TString                  // literal string segment (no interpolation, or segment between interpolations)
	TStringStart             // opening " of an interpolated double-quoted string
	TStringEnd               // closing " of an interpolated double-quoted string
	TAtom                    // :name

	// Structural
	TWhitespace // spaces/tabs (parser uses for word boundaries, discards from AST)
	TNewline    // statement separator
	TEOF        // end of input
	TSemicolon  // ;
	TComma      // ,

	// Delimiters
	TLParen   // (
	TRParen   // )
	TLBracket // [
	TRBracket // ]
	TLBrace   // {
	TRBrace   // }

	// Operators (always emitted unconditionally — no mode checks)
	TDot       // .
	TDiv       // /
	TMinus     // -
	TPlus      // +
	TMul       // *
	TPercent   // %
	TTilde     // ~
	TAt        // @
	TColon     // : (bare, not followed by identifier — for map key: value syntax)
	THash      // # (mid-word literal, not a comment)
	TEquals    // =
	TEq        // ==
	TNe        // !=
	TGt        // > (parser determines: comparison in expr, redirect in cmd args)
	TLt        // < (parser determines: comparison in expr, redirect in cmd args)
	TLe        // <=
	TGe        // >=
	TBang      // !
	TArrow     // ->
	TLeftArrow // <-
	TBackslash // \ (lambda)

	// Pipes and control
	TPipe       // |
	TPipeStderr // |&
	TPipeArrow  // |>
	TAnd        // &&
	TOr         // ||
	TAmpersand  // &

	// Multi-char redirections (unambiguous at lexer level)
	TRedirAppend // >>
	THeredoc     // <<
	THereString  // <<<

	// Expansion delimiters (lexer distinguishes these unambiguously)
	TDollar       // $ followed by identifier (emitted before the var name)
	TDollarLBrace // ${
	TDollarLParen // $(
	TDollarDLParen // $((
	THashLBrace    // #{
	TPercentLBrace // %{
	TDollarDQuote  // $"
	TSpecialVar    // $?, $$, $!, $@, $*, $#, $0-$9

	// Keywords (lexer looks up scanned identifier in keyword table)
	TIf
	TThen
	TElif
	TElse
	TFi
	TFor
	TIn
	TDo
	TDone
	TWhile
	TUntil
	TCase
	TEsac
	TFn
	TEnd
	TDefModule
	TUse
	TImport
	TMatch
	TSpawn
	TSpawnLink
	TSend
	TMonitor
	TAwait
	TSupervise
	TReceive
	TTry
	TRescue
	TAfter
	TNil
	TTrue
	TFalse
)

var tokenNames [256]string

func init() {
	tokenNames[TWhitespace] = "whitespace"
	tokenNames[TIdent] = "identifier"
	tokenNames[TInt] = "integer"
	tokenNames[TFloat] = "float"
	tokenNames[TString] = "string"
	tokenNames[TStringStart] = "string-start"
	tokenNames[TStringEnd] = "string-end"
	tokenNames[TAtom] = "atom"
	tokenNames[TNewline] = "newline"
	tokenNames[TEOF] = "EOF"
	tokenNames[TSemicolon] = ";"
	tokenNames[TComma] = ","
	tokenNames[TLParen] = "("
	tokenNames[TRParen] = ")"
	tokenNames[TLBracket] = "["
	tokenNames[TRBracket] = "]"
	tokenNames[TLBrace] = "{"
	tokenNames[TRBrace] = "}"
	tokenNames[TDot] = "."
	tokenNames[TDiv] = "/"
	tokenNames[TMinus] = "-"
	tokenNames[TPlus] = "+"
	tokenNames[TMul] = "*"
	tokenNames[TPercent] = "%"
	tokenNames[TTilde] = "~"
	tokenNames[TAt] = "@"
	tokenNames[TColon] = ":"
	tokenNames[THash] = "#"
	tokenNames[TEquals] = "="
	tokenNames[TEq] = "=="
	tokenNames[TNe] = "!="
	tokenNames[TGt] = ">"
	tokenNames[TLt] = "<"
	tokenNames[TLe] = "<="
	tokenNames[TGe] = ">="
	tokenNames[TBang] = "!"
	tokenNames[TArrow] = "->"
	tokenNames[TLeftArrow] = "<-"
	tokenNames[TBackslash] = `\`
	tokenNames[TPipe] = "|"
	tokenNames[TPipeStderr] = "|&"
	tokenNames[TPipeArrow] = "|>"
	tokenNames[TAnd] = "&&"
	tokenNames[TOr] = "||"
	tokenNames[TAmpersand] = "&"
	tokenNames[TRedirAppend] = ">>"
	tokenNames[THeredoc] = "<<"
	tokenNames[THereString] = "<<<"
	tokenNames[TDollar] = "$"
	tokenNames[TDollarLBrace] = "${"
	tokenNames[TDollarLParen] = "$("
	tokenNames[TDollarDLParen] = "$(("
	tokenNames[THashLBrace] = "#{"
	tokenNames[TPercentLBrace] = "%{"
	tokenNames[TDollarDQuote] = `$"`
	tokenNames[TSpecialVar] = "$special"
	tokenNames[TIf] = "if"
	tokenNames[TThen] = "then"
	tokenNames[TElif] = "elif"
	tokenNames[TElse] = "else"
	tokenNames[TFi] = "fi"
	tokenNames[TFor] = "for"
	tokenNames[TIn] = "in"
	tokenNames[TDo] = "do"
	tokenNames[TDone] = "done"
	tokenNames[TWhile] = "while"
	tokenNames[TUntil] = "until"
	tokenNames[TCase] = "case"
	tokenNames[TEsac] = "esac"
	tokenNames[TFn] = "fn"
	tokenNames[TEnd] = "end"
	tokenNames[TDefModule] = "defmodule"
	tokenNames[TUse] = "use"
	tokenNames[TImport] = "import"
	tokenNames[TMatch] = "match"
	tokenNames[TSpawn] = "spawn"
	tokenNames[TSpawnLink] = "spawn_link"
	tokenNames[TSend] = "send"
	tokenNames[TMonitor] = "monitor"
	tokenNames[TAwait] = "await"
	tokenNames[TSupervise] = "supervise"
	tokenNames[TReceive] = "receive"
	tokenNames[TTry] = "try"
	tokenNames[TRescue] = "rescue"
	tokenNames[TAfter] = "after"
	tokenNames[TNil] = "nil"
	tokenNames[TTrue] = "true"
	tokenNames[TFalse] = "false"
}

func (tt TokenType) String() string {
	if name := tokenNames[tt]; name != "" {
		return name
	}
	return fmt.Sprintf("token(%d)", tt)
}

// IsKeyword returns true if the token type is a keyword.
func (tt TokenType) IsKeyword() bool {
	return tt >= TIf && tt <= TFalse
}

type Token struct {
	Type       TokenType
	Val        string
	Pos        int
	Quoted     bool // true for single-quoted strings (no expansion)
	SpaceAfter bool // true if whitespace follows this token (set by lexer)
}

func (t Token) String() string {
	return t.Val
}

type NodeKind byte

const (
	// Values
	NLit     NodeKind = iota // literal value (int, float, string, atom, nil, true, false)
	NIdent                   // bare identifier reference
	NVarRef                  // $var variable reference
	NTuple                   // {a, b, c} or {a,} (single-element tuple)
	NList                    // [a, b, c]
	NMap                     // %{k: v}
	NCapture                 // &name — capture function value without calling
	NLambda                  // \params -> expr

	// Expressions
	NBinOp  // binary operation (+, -, *, /, ==, !=, <, >, etc.)
	NUnary  // unary operation (!, -)
	NAccess // expr.field (field access, module qualification)
	NCall   // function application: func(args) or func args (callee is NAccess or expression)

	// Expansions (interior is full AST, not opaque strings)
	NCmdSub        // $(cmd) command substitution
	NArithSub      // $((expr)) arithmetic expansion
	NParamExpand   // ${var}, ${var:-default}, etc.
	NInterpolation // #{expr} string interpolation
	NInterpString  // "hello $name" — interpolated string, Children are segments (NLit, NVarRef, NCmdSub, etc.)

	// Commands / invocations
	NCmd    // invocation by juxtaposition — resolved at runtime (function? builtin? PATH?)
	NArg    // compound word: adjacent tokens concatenated into one argument (hello$var, ${x}suffix)
	NPath   // file path in command arguments (assembled string: ~/foo, /usr/bin, ./rel)
	NIPv4   // IPv4 address literal (e.g. 192.168.1.1)
	NIPv6   // IPv6 address literal (e.g. ::1, fe80::1)
	NFlag   // command flag (-la, --verbose)
	NAssign // POSIX VAR=value (Tok.Val = "KEY=value")
	NMatch  // pattern = expr (ish match/bind)

	// Pipelines and combinators
	NPipe    // cmd | cmd
	NPipeFn  // expr |> fn
	NAndList // cmd && cmd
	NOrList  // cmd || cmd
	NBg      // cmd &
	NBlock   // sequence of statements
	NSubshell // (cmd; cmd)
	NGroup   // { cmd; cmd; }

	// POSIX control flow
	NIf    // if/then/elif/else/fi (exit-code semantics)
	NIshIf // if/do/elif/else/end (ish truthiness semantics)
	NFor   // for/in/do/done
	NWhile // while/do/done
	NUntil // until/do/done
	NCase  // case/esac
	NFnDef // name() { body; } (POSIX function)

	// ish extensions
	NIshFn        // fn name pattern... do body end
	NIshMatch     // match expr do clauses end
	NIshSpawn     // spawn expr
	NIshSpawnLink // spawn_link expr
	NIshSend      // send pid, msg
	NIshReceive   // receive do clauses end
	NIshMonitor   // monitor pid
	NIshAwait     // await pid [timeout]
	NIshSupervise // supervise strategy do workers end
	NIshTry       // try body rescue clauses end

	// Module system
	NDefModule // defmodule Name do defs end
	NUse       // use Module (mixin into defmodule)
	NImport    // import Module (copy into scope)
)

type Node struct {
	Kind        NodeKind
	Pos         int
	Tok         Token    // associated token (for literals, operators, identifiers, etc.)
	Children    []*Node  // sub-nodes (for NCmd: [nameNode, arg1Node, arg2Node, ...])
	Assigns     []*Node  // prefix assignments for NCmd (FOO=bar cmd)
	Rest        *Node    // for NList: tail variable in [h | t] pattern
	Clauses     []Clause // for NIf, NCase, NIshMatch, NIshReceive
	Redirs      []Redir  // attached redirections
	Timeout     *Node    // for receive: after timeout expression
	TimeoutBody *Node    // for receive: after timeout body
	Tail        bool        // true when this node is in tail position (for TCO)
	CachedVal   interface{} // cached literal value (set on first eval, avoids repeated strconv)
}

type Clause struct {
	Pattern *Node // pattern or condition
	Guard   *Node // optional guard (when)
	Body    *Node // body to execute
}

type Redir struct {
	Op         TokenType // TGt (>), TRedirAppend (>>), TLt (<), THeredoc (<<), THereString (<<<)
	Fd         int       // file descriptor (0 for stdin, 1 for stdout, 2 for stderr)
	TargetNode *Node     // AST node for the target (evaluated by the evaluator)
	Quoted     bool      // true for quoted heredoc delimiter (no expansion)
}

func LitNode(tok Token) *Node {
	return &Node{Kind: NLit, Tok: tok, Pos: tok.Pos}
}

func IdentNode(tok Token) *Node {
	return &Node{Kind: NIdent, Tok: tok, Pos: tok.Pos}
}

func BlockNode(stmts []*Node) *Node {
	if len(stmts) == 1 {
		return stmts[0]
	}
	return &Node{Kind: NBlock, Children: stmts}
}

// WordNode is a compatibility alias during migration. New code should use IdentNode.
func WordNode(tok Token) *Node {
	return &Node{Kind: NIdent, Tok: tok, Pos: tok.Pos}
}
