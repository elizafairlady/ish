# ish Grammar Reference

ish is a recursive descent parser with two contexts: **statement context** and
**expression context**. The lexer is stateless and produces a flat token array;
each token carries a `SpaceAfter` flag that drives adjacency disambiguation.
All keywords are `TIdent` tokens — the parser checks `.Val` at statement head.

---

## Notation

```
=       definition
|       alternative
*       zero or more
+       one or more
?       optional
( )     grouping
```

`SPACE` means the preceding token's `SpaceAfter` flag is true.
`NOSPACE` means it is false.
Newlines and semicolons are both statement terminators and may be skipped
between clauses and sub-structures wherever the grammar says `skipNewlines`.

---

## Top-level structure

```
program  = statement* EOF
block    = statement*
```

---

## Statement context

Dispatch on the first token, examined in order:

```
statement   = destructBind
            | binding
            | posixAssign [ pipeline ]
            | posixFnDef
            | keywordStmt
            | tupleExpr              -- { when looksLikeTuple (comma at depth 1)
            | braceGroup             -- { otherwise
            | subshell               -- (
            | pipeline [ & ]
```

After `parsePipeline`, if the result is a single value and the next token is
`+ - * / % == != > < >= <= |> .`, the value is extended into an expression
(`extendAsExpr`).

**Destructuring bind** — triggered when the statement starts with `{` or `[`
and a lookahead finds `=` immediately after the matching close bracket:

```
destructBind    = tuplePattern = expr
                | listPattern  = expr
```

**Simple binding** — IDENT with `SPACE` before `=`:

```
binding         = IDENT SPACE = expr
```

**POSIX assignment** — IDENT with no space before `=`:

```
posixAssign     = IDENT NOSPACE = [ cmdPrimary ]
```

If the token after the assignment is not a statement terminator (`\n ; EOF |
&& || ) }`), the assignment is a *prefix assignment* followed by a pipeline on
the same line:

```
prefixAssign    = posixAssign pipeline
```

**POSIX function definition** — IDENT with no space before `()`:

```
posixFnDef      = IDENT NOSPACE ( ) braceGroup
```

**Background:**

```
backgroundStmt  = pipeline &
```

---

## Pipeline (command context)

```
pipeline    = logicCmd ( pipeOp skipNewlines logicCmd
                       | |> skipNewlines exprLogicOr )*

logicCmd    = commandForm ( ( && | || ) skipNewlines commandForm )*

commandForm = ! commandForm
            | cmdApply redirect*
```

`|>` takes a single `exprLogicOr` operand on its right-hand side, not another
command.

**Redirects:**

```
redirect    = [ INT NOSPACE ] >  [ & ] cmdPrimary    -- stdout; >& = fd dup
            | [ INT NOSPACE ] <  cmdPrimary           -- stdin
            | [ INT NOSPACE ] >> cmdPrimary            -- append
            | << IDENT heredocBody                    -- heredoc
            | <<< cmdPrimary                          -- here-string
```

The fd prefix is recognised by looking at the last child of the command: if it
is an INT literal and the preceding token has no SpaceAfter, and a redirect
operator follows, that INT is consumed as the fd number and removed from the
argument list.

---

## Command application

```
cmdApply    = cmdPrimary wordJoin? cmdArg*
cmdArg      = { tupleElems }     -- { in argument position is always a tuple
            | ( expr )           -- ( in argument position is expression context
            | cmdPrimary wordJoin?
```

Arguments are collected while `isCmdArgStart` is true. Commas between arguments
are skipped silently.

**`isCmdArgStart`** returns true for:

| Token | Condition |
|---|---|
| TIdent, TInt, TFloat, TString, TStringStart, TAtom | always |
| TDollar, TDollarLParen, TDollarLBrace, TDollarDLParen, TSpecialVar | always |
| TLParen, TBackslash, TTilde, TLBracket, TRBracket, TLBrace | always |
| TBang, TAssign | always |
| TStar | NOSPACE only |
| TMinus, TPlus, TPercent, TSlash, TDot, THash, TAt, TColon | NOSPACE only |

**Word joining** — after parsing a `cmdPrimary`, if the previous token has
NOSPACE and the current token is one of `TDollar TDollarLParen TDollarLBrace
TDollarDLParen TSpecialVar TIdent TInt TString TStringStart`, the tokens are
joined into a single `NInterpStr` node.

**Compound words** — an ident (or int, dot, etc.) followed NOSPACE by any of
`TIdent TInt TFloat TDot TSlash TMinus TPlus TPercent THash TAt TColon TStar
TAssign TBang` is concatenated into a single literal. A compound word containing
only digits and dots is classified as an IPv4 address; one containing `:` is
an IPv6 address.

---

## Command primaries

```
cmdPrimary  = IDENT NOSPACE ( callArgs )         -- NCall
            | IDENT NOSPACE compoundContinue+    -- compound word → NLit (IPv4/IPv6/path)
            | IDENT                              -- NIdent
            | INT                               -- NLit
            | FLOAT                             -- NLit
            | STRING                            -- NLit
            | interpString
            | ATOM                              -- NAtom  (:name)
            | $ IDENT ( NOSPACE . IDENT )*      -- NVarRef with optional field access
            | SPECIAL_VAR                       -- NVarRef ($? $$ $! $@ $* $# $0-$9)
            | $( block )                        -- NCmdSub
            | ${ paramBody }                    -- NParamExpand
            | $(( expr ))                       -- arithmetic substitution
            | ( block )                         -- grouped block; single child unwrapped
            | [ SPACE testArgs ]                -- POSIX test command [ ... ]
            | [ NOSPACE listElems ]             -- list literal
            | ]                                 -- literal ] token (for test arguments)
            | { block }                         -- NBlock (brace group)
            | - NOSPACE - NOSPACE IDENT         -- NFlag  --flag
            | - NOSPACE -                       -- NFlag  --
            | - NOSPACE IDENT                   -- NFlag  -flag
            | ~ path | / path                   -- NPath
            | << IDENT heredocBody              -- heredoc
            | %{ mapPairs }                     -- NMap
            | \ params -> expr                  -- NLambda
            | . # @ = * : + % !                 -- single-char literal (NOSPACE compound)
```

**`[` disambiguation**: `[` with SpaceAfter → POSIX test command `[ ... ]`;
`[` without SpaceAfter → list literal.

---

## Paths

```
path        = ( ~ | / ) ( NOSPACE pathToken )*
pathToken   = / | IDENT | . | INT | * | -
```

Tilde is expanded at eval time.

---

## Expression context

`parseExpr` is the entry point. After parsing `exprPipe`, if the result is
callable (NIdent, NCall, or NAccess) and the next token is an argument-start
token, additional arguments are collected by juxtaposition:

```
expr        = exprPipe arg*         -- juxtaposition; commas skipped between args
arg         = exprPipe

exprPipe    = exprLogicOr ( |> exprLogicOr )*
exprLogicOr = exprLogicAnd ( || exprLogicAnd )*
exprLogicAnd= exprCompare ( && exprCompare )*
exprCompare = exprAdd ( cmpOp exprAdd )*   -- chained: a<b<c → (a<b)&&(b<c)
exprAdd     = exprMul ( ( + | - ) exprMul )*
exprMul     = exprUnary ( ( * | / | % ) exprUnary )*
exprUnary   = ( ! | - ) exprUnary | exprPostfix
exprPostfix = exprPrimary ( . IDENT | NOSPACE ( callArgs ) )*

cmpOp       = == | != | > | < | >= | <=
```

**Operator precedence** (lowest to highest):

| Level | Operators |
|---|---|
| 1 | `\|>` |
| 2 | `\|\|` |
| 3 | `&&` |
| 4 | `== != > < >= <=` |
| 5 | `+ -` |
| 6 | `* / %` |
| 7 | `! -` (unary, right-associative) |
| 8 | `.field`   `NOSPACE(args)` (postfix, left-associative) |
| 9 | juxtaposition application (tightest, left-associative) |

**Comparison chaining**: `a < b < c` desugars to `(a < b) && (b < c)`. Each
additional comparison operator at the same level introduces a new `&&` node
sharing the middle operand.

**`isExprArgStart`** (controls juxtaposition collection):
`TIdent TInt TFloat TString TStringStart TAtom TDollar TDollarLParen
TDollarLBrace TSpecialVar TLParen TLBracket TLBrace TBackslash TAmpersand
TPercent`

---

## Expression primaries

```
exprPrimary = nil | true | false               -- NLit
            | fn fnDef(exprCtx=true)           -- anonymous fn
            | match matchExpr
            | if ifExpr
            | try tryExpr
            | receive receiveExpr
            | spawn | send                     -- NIdent (callable for juxtaposition)
            | IDENT NOSPACE ( callArgs )       -- NCall
            | IDENT                            -- NIdent
            | INT | FLOAT | STRING             -- NLit
            | interpString
            | ATOM                             -- NAtom
            | $ IDENT                          -- NVarRef
            | SPECIAL_VAR                      -- NVarRef
            | $( block )                       -- NCmdSub
            | ${ paramBody }                   -- NParamExpand
            | $(( expr ))                      -- arithmetic
            | ( expr )                         -- grouped expression
            | [ listElems ]                    -- NList
            | { tupleElems }                   -- NTuple
            | \ params -> expr                 -- NLambda
            | %{ mapPairs }                    -- NMap
            | ~                                -- NPath
            | & IDENT                          -- function capture (&name)
```

---

## Data structures

**List:**

```
list        = [ ]
            | [ exprPipe ( , exprPipe )* ]          -- NList
            | [ exprPipe ( , exprPipe )* | exprPipe ] -- NCons
```

**Tuple:**

```
tuple       = { }
            | { exprPipe ( , exprPipe )* }
```

At statement head, `{` triggers `looksLikeTuple`, which scans forward for a
comma at depth 1 before a newline, semicolon, or closing `}`. Comma found →
tuple expression. No comma → brace group. In argument position, `{` is always a
tuple.

**Map:**

```
map         = %{ }
            | %{ mapPair ( , mapPair )* }
mapPair     = exprPrimary : exprPipe
```

Key is parsed with `parseExprPrimary`; value with `parseExprPipe`. Children are
stored flat: `[key, val, key, val, ...]`.

---

## Calls

**Paren call** — no space between callee and `(`:

```
call        = IDENT NOSPACE ( ( exprPipe ( , exprPipe )* )? )
            | exprPostfix NOSPACE ( ( exprPipe ( , exprPipe )* )? )
```

**Juxtaposition call** — callable head (NIdent, NCall, NAccess) followed by
argument-start tokens:

```
juxtCall    = callableHead arg arg ...
```

Both syntaxes are valid and produce the same `NCall`/`NApply` nodes.

---

## Lambda

```
lambda      = \ ( IDENT , )* IDENT -> expr
```

Params are zero or more comma-separated idents. The `->` is mandatory. Body is
`parseExpr`, which includes `|>` — a `|>` in the lambda body is consumed by
the body, not the enclosing pipeline.

---

## Interpolated strings

```
interpString = STRSTART segment* STREND
segment      = STRING_LITERAL
             | $ IDENT
             | SPECIAL_VAR
             | $( block )
             | ${ paramBody }
             | $(( expr ))
             | #{ expr }
```

`STRSTART` / `STREND` delimit a double-quoted string containing at least one
interpolation. Plain strings with no interpolation are single `TString` tokens.

---

## Function definitions

`parseFnDef` is called with an `exprCtx` flag.

**Named function** (statement context, `exprCtx=false`):

```
namedFn     = fn NAME param* [ when expr ] do body end
            | fn NAME do clauseBlock end
```

**Anonymous function** (expression context, `exprCtx=true`):

```
anonFn      = fn param* do body end
            | fn do clauseBlock end
            | fn do body end
```

`body` is a `block`. `clauseBlock` is selected when `looksLikeClauseBlock`
finds a `->` before the next newline, semicolon, `end`, or EOF. A `\` before
`->` means lambda, not a clause.

**Clause block:**

```
clauseBlock = ( pattern [ when expr ] -> statement \n? )+
```

`end` and `when` are pushed as stop words before parsing each clause pattern.

**Multi-definition** — multiple top-level `fn` statements with the same name
produce independent `NFnDef` nodes; the evaluator merges their clauses:

```
fn fib 0 do 0 end
fn fib 1 do 1 end
fn fib n when n > 1 do fib(n-1) + fib(n-2) end
```

---

## Control flow

**if:**

```
ifStmt      = if condition [;] do  body (elif condition [;] [then] body)* [else body] end
            | if condition [;] then body (elif condition [;] [then] body)* [else body] fi
```

`then` selects POSIX mode (closed by `fi`); `do` selects ish mode (closed by
`end`). `elif` is supported in both modes.

**condition** is parsed by `parseIfCondition`:

```
condition   = ( expr )                      -- parenthesised: expression context
            | condPipeline [ exprExtend ]   -- command context, stops at do/then
```

`condPipeline` is identical to `pipeline` but with `do` and `then` as stop
words. After resolving, if the result is a single value and the next token is
an expression operator, `extendAsExpr` re-parses from that value.

**for:**

```
forStmt     = for IDENT in wordList [;] [do] body (done | end)
wordList    = cmdPrimary*              -- until ; do \n EOF
```

**while / until:**

```
whileStmt   = while condition [;] [do] body (done | end)
untilStmt   = until condition [;] [do] body (done | end)
```

**case** (POSIX):

```
caseStmt    = case cmdPrimary in
                ( patStr ) body ;;
                ...
              esac
```

`patStr` is raw token concatenation up to `)`. Body ends at `;;` or `esac`.

**match:**

```
matchStmt   = match exprPipe do
                ( pattern [ when expr ] -> statement \n? )+
              end
```

`end` and `when` are stop words during pattern parsing.

**receive:**

```
receiveStmt = receive [timeout] do
                ( pattern -> statement (; statement)* \n? )+
                [ after ( timeout -> body | body ) ]
              end
```

Two timeout forms: timeout before `do` with `after body`, or no leading timeout
with `after timeout -> body`. `end` and `after` are stop words.

**try:**

```
tryStmt     = try do body rescue
                ( pattern -> statement \n? )+
              end
```

---

## Modules

```
defmodule   = defmodule IDENT do block end
useImport   = ( use | import ) IDENT
```

Module bodies are normal blocks of statements, typically `fn` definitions.
There is no `def` keyword; all functions use `fn`.

---

## OTP keywords

`spawn`, `spawn_link`, `send`, `await`, and `monitor` are statement-level
keywords that collect expression arguments until newline, semicolon, or EOF:

```
otpStmt     = ( spawn | spawn_link | send | await | monitor ) ( exprPipe , )* exprPipe?
```

Each argument is parsed with `parseExprPipe`. The result is `NApply`.

---

## Subshell and brace group

```
subshell    = ( block )    -- NSubshell, isolated scope
braceGroup  = { block }    -- NBlock, current scope
```

At statement head, `{` is a brace group unless `looksLikeTuple` finds a comma
at depth 1. In argument position, `{` is always a tuple.

---

## Stop words

Several constructs push stop words before parsing sub-expressions.
`parseStatement` and `parseExpr` halt on any active stop word.

| Construct | Stop words pushed |
|---|---|
| `fn` clause block | `end`, `when` |
| `match` | `end`, `when` |
| `receive` | `end`, `after` |
| if/while/until condition | `do`, `then` |
| `parseBlockUntil(words...)` | the words passed as arguments |

---

## Adjacency disambiguation summary

The `SpaceAfter` flag on the *preceding* token determines how the next token is
interpreted:

| Pattern | Interpretation |
|---|---|
| `name(args)` — NOSPACE `(` | paren call |
| `name (expr)` — SPACE `(` | command with grouped-expression argument |
| `-flag` — NOSPACE after `-` | flag token |
| `a-b` — NOSPACE on both sides | compound word, not subtraction |
| `*` — NOSPACE | glob / compound-word part |
| `*` — SPACE | multiplication operator (expression context) |
| `[items]` — NOSPACE `[` | list literal |
| `[ items ]` — SPACE `[` | POSIX test command |
