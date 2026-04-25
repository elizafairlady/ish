package ast

type TokenType byte

const (
	TEOF TokenType = iota
	TNewline
	TSemicolon

	// Literals and identifiers — all keywords are TIdent,
	// the parser checks values at statement-head position.
	TIdent
	TInt
	TFloat
	TString
	TAtom // :name

	// Delimiters
	TLParen   // (
	TRParen   // )
	TLBracket // [
	TRBracket // ]
	TLBrace   // {
	TRBrace   // }

	// Operators — the parser determines meaning by context
	TPlus      // +
	TMinus     // -
	TStar      // *
	TSlash     // /
	TPercent   // %
	TBang      // !
	TGt        // >  — redirect in command context, comparison in expression context
	TLt        // <  — redirect in command context, comparison in expression context
	TGtEq      // >=
	TLtEq      // <=
	TEqEq      // ==
	TBangEq    // !=
	TAssign    // =
	TDot       // .
	TComma     // ,
	TPipe      // |
	TPipeArrow // |>
	TPipeAmp   // |&
	TAnd       // &&
	TOr        // ||
	TAmpersand // &
	TArrow     // ->
	TBackslash // \
	TDollar    // $
	THash      // #
	TAt        // @
	TTilde     // ~
	TColon          // :
	TDoubleSemicolon // ;;
	TDoubleRParen   // ))

	// Multi-char operators (unambiguous)
	TAppend    // >>
	THeredoc   // <<
	THereString // <<<

	// Expansion delimiters
	TDollarLParen  // $(
	TDollarDLParen // $((
	TDollarLBrace  // ${
	TSpecialVar    // $?, $$, $!, $@, $*, $#, $0-$9

	// String interpolation
	TStringStart // opening " of interpolated string
	TStringEnd   // closing " of interpolated string
	THashLBrace  // #{

)

type Token struct {
	Type       TokenType
	Val        string
	Pos        int
	SpaceAfter bool // whitespace follows this token
}

