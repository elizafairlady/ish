package main

type TokenType byte

const (
	TWord      TokenType = iota // bare word (command name, arg, identifier)
	TInt                        // integer literal
	TString                     // "..." or '...'
	TAtom                       // :name
	TNewline                    // statement separator
	TEOF                        // end of input

	// Operators
	TPipe       // |
	TPipeArrow  // |>
	TAnd        // &&
	TOr         // ||
	TSemicolon  // ;
	TAmpersand  // &
	TEquals     // = (with spaces — ish match)
	TLParen     // (
	TRParen     // )
	TLBracket   // [
	TRBracket   // ]
	TLBrace     // {
	TRBrace     // }
	TComma      // ,
	TDot        // .
	TArrow      // ->
	TLeftArrow  // <-
	TPercent    // %

	// Redirections
	TRedirOut    // >
	TRedirAppend // >>
	TRedirIn     // <
	THeredoc     // <<
	THereString  // <<<

	// Arithmetic / comparison
	TPlus  // +
	TMinus // -
	TMul   // *
	TDiv   // /
	TEq    // ==
	TNe    // !=
	TLe    // <=
	TGe    // >=
	TBang  // !

)

type Token struct {
	Type   TokenType
	Val    string
	Pos    int
	Quoted bool // true for single-quoted strings (no expansion)
}

func (t Token) String() string {
	return t.Val
}
