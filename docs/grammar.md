# ish Grammar

ish is a POSIX sh superset with Elixir-inspired functional extensions. This grammar defines the complete syntax. Where POSIX sh and ish conflict, POSIX sh correctness takes priority.

## Notation

- `WS` = required whitespace (spaces/tabs between tokens)
- `ws` = optional whitespace
- `ADJACENT(a b c)` = tokens a, b, c with NO whitespace between them (checked via SpaceAfter on preceding token)
- `|` = alternative
- `*` = zero or more
- `+` = one or more
- `?` = optional
- Capitalized names = token types
- lowercase names = grammar productions
- `--` = comment

## Program structure

```
program        = ws statement_list ws EOF
statement_list = (statement separator)* statement?
separator      = NEWLINE | SEMICOLON
statement      = pipeline (AND pipeline | OR pipeline | AMPERSAND)*
pipeline       = stmt_with_ops (PIPE ws stmt_with_ops | PIPE_STDERR ws stmt_with_ops | PIPE_ARROW ws stmt_with_ops)*
stmt_with_ops  = primary_stmt (binop primary_stmt)*
```

## Primary statement dispatch

At statement start, the parser examines the first token(s) to determine the form.
Tentative parsing with commitment points resolves the command-vs-expression ambiguity:
the parser tries expression first, and if a commitment point is reached (spaced infix
operator, comma, data structure literal), keeps it. Otherwise rolls back to command.

```
primary_stmt   = keyword_stmt
               | binding              -- IDENT ws EQUALS ws pipeline
               | posix_assign         -- ADJACENT(IDENT EQUALS value)  (no whitespace around =)
               | expression_stmt      -- starts with literal, operator, bracket, $-expansion, or IDENT followed by operator/DOT
               | command_stmt         -- IDENT/path/dot followed by args (juxtaposition)
               | bracket_stmt         -- ( { [ at statement start, disambiguated by lookahead

keyword_stmt   = if_stmt | for_stmt | while_stmt | until_stmt | case_stmt
               | fn_stmt | defmodule_stmt | use_stmt | match_stmt
               | spawn_stmt | spawn_link_stmt | send_stmt | receive_stmt
               | monitor_stmt | await_stmt | supervise_stmt | try_stmt
```

## How the parser decides at statement start

Given first token and what follows (with SpaceAfter visibility):

```
IDENT EQUALS (no WS around =)  ->  posix_assign        -- FOO=bar
IDENT ws EQUALS ws              ->  binding              -- x = expr
ADJACENT(IDENT DOT)             ->  expression_stmt      -- Module.func (field access)
IDENT ws binop (spaced)         ->  expression_stmt      -- x + y, x * 2
IDENT ws MINUS/PLUS (adjacent)  ->  command_stmt         -- ls -la, cmd +x (flag)
IDENT ws GT/LT                  ->  command_stmt         -- echo > file (redirect)
                                    UNLESS exprContext   -- \x -> x > 2 (comparison)
IDENT ws REDIR_APPEND/etc       ->  command_stmt         -- echo >> file
IDENT ws DOT                    ->  command_stmt         -- echo .hidden (path arg)
IDENT ws LPAREN                 ->  expression_stmt      -- func (expr) (grouped arg)
IDENT ws terminator             ->  standalone_value     -- x (alone on line)
IDENT ws value                  ->  tentative parse      -- could be command or function call
keyword                         ->  keyword_stmt
DOT ws IDENT                    ->  command_stmt         -- . script (source)
ADJACENT(DOT IDENT)             ->  command_stmt         -- .hidden as command
ADJACENT(DOT DOT)               ->  command_stmt         -- .. as command
ADJACENT(DOT DIV IDENT)         ->  command_stmt         -- ./script
DIV                             ->  command_stmt         -- /usr/bin/foo
TILDE                           ->  command_stmt         -- ~/bin/script
INT | FLOAT | STRING | ATOM     ->  expression_stmt
DOLLAR | DOLLAR_LPAREN | ...    ->  expression_stmt
LBRACKET                        ->  list_expr or test_builtin (lookahead for commas/pipe)
LBRACE                          ->  tuple_expr or group_cmd (lookahead for commas/atoms/newlines)
LPAREN                          ->  subshell (or grouped expr if exprContext)
BACKSLASH                       ->  lambda
BANG                            ->  expression (unary not)
PERCENT LBRACE                  ->  map_expr
```

### Tentative parsing with commitment points

When the parser cannot determine from syntax alone whether a statement is a command
or an expression (IDENT followed by a value), it tries parsing as an expression first.
If a commitment point is reached, the expression interpretation is kept. Otherwise the
parser rolls back and tries command invocation.

Commitment points (things that can ONLY appear in expression syntax):
- Spaced infix operators: `a + b` (adjacent operators like `a-z` do NOT commit)
- Commas between function args: `func a, b`
- Data structure literals as args: `func [1,2,3]`, `func {a, b}`
- Parenthesized call args: `func(a, b)`

Operators inside nested constructs ($(()), $(), #{}, [], {}, %{}, ()) do NOT
propagate commitment to the outer tentative parse — each nested context
saves and restores the committed flag.

### exprContext

Inside ish blocks (do...end), lambda bodies, and binding RHS, `>` and `<` are
treated as comparison operators instead of POSIX redirects. The parser tracks this
with an `exprContext` flag, separate from the tentative parse `committed` flag.

## Binding vs POSIX assignment

Determined by whitespace around TEquals:
```
binding        = pattern ws EQUALS ws pipeline           -- x = expr |> func
posix_assign   = ADJACENT(IDENT EQUALS compound_word?)   -- FOO=bar (no whitespace around =)
prefix_assign  = posix_assign+ WS command_stmt           -- FOO=bar cmd args
```

## Command statement

A command is a callee followed by whitespace-separated arguments until a terminator.
The evaluator resolves the callee at runtime: user function, native function, builtin, or PATH executable.

```
command_stmt   = callee (WS command_arg)* (WS redir)*
callee         = IDENT                                   -- bare command name
               | path                                    -- /usr/bin/foo, ./script, ../dir, ~/bin/cmd
               | DOT WS                                  -- source command (. script)
command_arg    = compound_word                            -- assembled from adjacent tokens
               | LPAREN ws expression ws RPAREN           -- parenthesized expr always evaluated
```

## Compound word

A compound word is a sequence of adjacent tokens (no whitespace between them) that form a single string argument for exec. The parser collects adjacent tokens and builds a structured node.

```
compound_word  = word_part+                               -- all parts adjacent (no whitespace)

word_part      = IDENT                                    -- bare text: hello, foo, txt
               | STRING                                   -- "quoted" or 'quoted' (already one token)
               | STRING_START interp_parts STRING_END     -- interpolated "hello $name"
               | DOLLAR IDENT                             -- $var
               | SPECIAL_VAR                              -- $?, $$, $!, $@, $*, $#, $0-$9
               | DOLLAR_LBRACE ... RBRACE                 -- ${var:-default}
               | DOLLAR_LPAREN ... RPAREN                 -- $(command)
               | DOLLAR_DLPAREN ... RPAREN RPAREN         -- $((arithmetic))
               | HASH_LBRACE ... RBRACE                   -- #{expr}
               | DOT                                      -- literal . (in paths: file.txt, ../dir)
               | DIV                                      -- literal / (in paths: /usr, path/to)
               | MINUS                                    -- literal - (in flags: -la, --verbose)
               | TILDE                                    -- literal ~ (in paths: ~/dir)
               | PLUS                                     -- literal + (in +x flag)
               | EQUALS                                   -- literal = (in --flag=value)
               | COLON                                    -- literal : (in PATH-like values)
               | AT                                       -- literal @
               | MUL                                      -- glob * (in *.sh, path/*)
               | INT                                      -- numeric segment (in 2>/dev/null, -5)
               | FLOAT                                    -- numeric segment
               | PERCENT                                  -- literal % (in %1 job spec)
               | BACKSLASH any                            -- escaped character (\ followed by next token)
               | keyword_token                            -- keywords in arg position are literal strings
```

The evaluator concatenates all parts into a single string for exec, expanding `$var`, `$(cmd)`, `${param}`, `#{expr}` as it goes.

## Redirections (within command_stmt)

```
redir          = GT ws redir_target                     -- > file
               | REDIR_APPEND ws redir_target           -- >> file
               | LT ws redir_target                     -- < file
               | HEREDOC                                -- << (already handled by lexer)
               | HERESTRING ws redir_target             -- <<< word
               | ADJACENT(INT GT) ws redir_target       -- 2> file
               | ADJACENT(INT LT) ws redir_target       -- 0< file
               | GT AMPERSAND ws INT                    -- >& 2 (fd dup)
               | AMPERSAND GT ws redir_target           -- &> file (stdout+stderr)

redir_target   = compound_word
```

## Expression

Expressions use precedence climbing. Function application binds tighter than all
binary operators: `f x + g y` parses as `(f x) + (g y)`.

```
expression     = expr (ws EQUALS ws pipeline)?          -- optional match/bind: expr = rhs

expr           = call_or_value (ws binop ws call_or_value)*   -- precedence climbing

call_or_value  = access_chain call_args?                -- function application
               | unary                                  -- unary op or plain value

unary          = BANG ws value                          -- !expr
               | MINUS ws value                         -- -expr (when at expression start)
               | value

value          = INT | FLOAT
               | STRING                                  -- single-quoted, no interpolation
               | STRING_START interp_parts STRING_END    -- interpolated string
               | DOLLAR_DQUOTE interp_parts STRING_END  -- $"..." with C escapes
               | ATOM
               | NIL | TRUE | FALSE
               | IDENT                                   -- variable reference or function name
               | DOLLAR IDENT                            -- $var
               | SPECIAL_VAR                             -- $?
               | DOLLAR_LPAREN stmt_list RPAREN          -- $(cmd)
               | DOLLAR_DLPAREN expr RPAREN RPAREN       -- $((arith))
               | DOLLAR_LBRACE ... RBRACE                -- ${param}
               | HASH_LBRACE expr RBRACE                 -- #{interp}
               | LPAREN ws pipeline ws RPAREN            -- (grouped expr)
               | tuple_expr                              -- {a, b} or {a,} or {}
               | list_expr                               -- [a, b] or [h | t] or []
               | map_expr                                -- %{k: v}
               | lambda                                  -- \params -> body
               | FN fn_expr                              -- fn as value (always anonymous)

access_chain   = value (DOT IDENT)*                     -- a.b.c field access chain

call_args      = ADJACENT(LPAREN) ws (expr (ws COMMA ws expr)*)? ws RPAREN
                                                         -- func(a, b) — parens ADJACENT to callee
               | WS value                                -- func value — single value by juxtaposition
```

Function call syntax:
- `func(a, b)` — adjacent parens, multi-arg. Required inside data structures.
- `func value` — juxtaposition, single value arg. Application binds tighter than operators.
- `func (expr)` — spaced parens = grouped expression as single arg (NOT multi-arg call).
- At statement level via NCmd: `func arg1, arg2` with commas — resolved by evaluator.

```
binop          = PLUS | MINUS | MUL | DIV | PERCENT
               | EQ | NE | GT | LT | LE | GE
```

Operator precedence (lowest to highest):

| Precedence | Operators | Description |
|------------|-----------|-------------|
| 1 | `==` `!=` | Equality / inequality |
| 2 | `<` `>` `<=` `>=` | Comparison (only in exprContext or when committed) |
| 3 | `+` `-` | Addition / subtraction |
| 4 | `*` `/` `%` | Multiplication / division / modulo |
| 5 | function application | `f x` binds tighter than all operators |

## Tuple / List / Map

Inside data structures, function calls must use adjacent parens for multi-arg:
`{hd(list), List.map(items, fn)}`. Single-value application works bare: `{hd list, tl list}`.

```
tuple_expr     = LBRACE ws RBRACE                                -- {} empty tuple
               | LBRACE ws expr ws COMMA ws RBRACE                -- {a,} single-element tuple
               | LBRACE ws expr (ws COMMA ws expr)+ ws RBRACE     -- {a, b, ...}

list_expr      = LBRACKET ws RBRACKET                              -- [] empty list
               | LBRACKET ws expr (ws COMMA ws expr)* ws RBRACKET  -- [a, b, ...]
               | LBRACKET ws expr+ ws PIPE ws expr ws RBRACKET    -- [h | t] cons

map_expr       = PERCENT LBRACE ws (map_entry (ws COMMA ws map_entry)*)? ws RBRACE
map_entry      = IDENT ws COLON ws expr                            -- key: value (atom key shorthand)
               | expr ws COLON ws expr                             -- expr: value
```

## Function definition

```
fn_stmt        = FN ws IDENT ws fn_params? ws guard? ws fn_body       -- named (at statement level)
fn_expr        = FN ws fn_params? ws guard? ws fn_body                -- anonymous (in expression context)
               | FN ws DO ws clause_list ws END                       -- anonymous multi-clause

fn_params      = pattern (ws COMMA ws pattern)*
guard          = WHEN ws expr                                          -- when condition
fn_body        = DO ws statement_list ws END

-- Multi-clause dispatch (inside do...end):
clause_list    = clause+
clause         = pattern+ (ws guard)? ws ARROW ws statement_list

-- Named fn with multiple definition sites (adds clauses):
-- fn fib 0 do 0 end
-- fn fib 1 do 1 end
-- fn fib n when n > 1 do ... end
-- Each adds a clause to the same function.
```

At statement level, `fn name ...` is a named function definition.
In expression context (binding RHS, data structure element, lambda body, etc.),
`fn params do ... end` is always anonymous — the first identifier after `fn` is
a parameter, not a name.

## Lambda

```
lambda         = BACKSLASH ws params? ws ARROW ws lambda_body
params         = pattern (ws COMMA ws pattern)*
lambda_body    = statement_list ws END                                 -- multi-line (ARROW followed by NEWLINE)
               | stmt_with_ops                                         -- single expression (stops before |>)
```

Single-line lambda bodies parse as `stmt_with_ops`, which does NOT consume `|>`.
This allows `list |> List.filter \x -> x > 1 |> length` to parse as
`(list |> (List.filter (\x -> x > 1)) |> length)`, not
`(list |> List.filter (\x -> x > 1 |> length))`.

Lambda bodies set `exprContext = true`, so `>` and `<` are comparisons.

## Control flow

```
-- POSIX if (exit-code semantics):
if_stmt        = IF ws condition ws THEN ws body
                 (ws ELIF ws condition ws THEN ws body)*
                 (ws ELSE ws body)?
                 ws FI

-- ish if (truthiness semantics):
               | IF ws expression ws DO ws body
                 (ws ELSE ws body)?
                 ws END

-- POSIX for:
for_stmt       = FOR ws IDENT ws IN ws word_list ws DO ws body ws (DONE | END)
word_list      = command_arg (WS command_arg)*

-- While/Until:
while_stmt     = WHILE ws condition ws DO ws body ws (DONE | END)
until_stmt     = UNTIL ws condition ws DO ws body ws (DONE | END)

-- Condition in POSIX if/while/until is a command (exit code checked):
condition      = statement_list

-- Case:
case_stmt      = CASE ws compound_word ws IN ws case_clause* ws ESAC
case_clause    = pattern_list RPAREN ws statement_list (SEMICOLON SEMICOLON)?
pattern_list   = compound_word (PIPE compound_word)*

-- POSIX function:
posix_fn_def   = IDENT LPAREN RPAREN ws LBRACE ws statement_list ws RBRACE

-- defmodule:
defmodule_stmt = DEFMODULE ws IDENT ws DO ws module_body ws END
module_body    = (fn_stmt | use_stmt | statement)*

-- use:
use_stmt       = USE ws IDENT

-- match:
match_stmt     = MATCH ws expr ws DO ws clause_list ws END

-- try/rescue:
try_stmt       = TRY ws DO ws body ws (RESCUE ws clause_list)? ws END

-- spawn/send/receive/monitor/await/supervise:
spawn_stmt      = SPAWN ws stmt_with_ops
spawn_link_stmt = SPAWN_LINK ws stmt_with_ops
send_stmt       = SEND ws expr ws COMMA ws expr
monitor_stmt    = MONITOR ws expr
await_stmt      = AWAIT ws expr
receive_stmt    = RECEIVE ws DO ws clause_list (ws AFTER ws expr ws ARROW ws body)? ws END
supervise_stmt  = SUPERVISE ws expr ws DO ws worker_list ws END
worker_list     = (IDENT ws IDENT ws stmt_with_ops separator)*     -- worker :name fn_expr
```

## Interpolated string

```
interp_string  = STRING_START interp_part* STRING_END
interp_part    = STRING                                    -- literal text segment
               | DOLLAR IDENT                              -- $var
               | SPECIAL_VAR                               -- $?
               | DOLLAR_LPAREN stmt_list RPAREN            -- $(cmd)
               | DOLLAR_DLPAREN expr RPAREN RPAREN         -- $((arith))
               | DOLLAR_LBRACE ... RBRACE                  -- ${param}
               | HASH_LBRACE expr RBRACE                   -- #{expr}
```

## Pattern

Used in fn params, match clauses, and destructuring binds.

```
pattern        = IDENT                                      -- variable binding (or _ for wildcard)
               | INT | FLOAT | STRING | ATOM                -- literal match
               | NIL | TRUE | FALSE                         -- keyword literal match
               | tuple_pattern                              -- {a, b}
               | list_pattern                               -- [h | t]
               | map_pattern                                -- %{key: var}

tuple_pattern  = LBRACE ws (pattern (ws COMMA ws pattern)*)? ws RBRACE
list_pattern   = LBRACKET ws (pattern (ws COMMA ws pattern)*)? (ws PIPE ws pattern)? ws RBRACKET
map_pattern    = PERCENT LBRACE ws (IDENT ws COLON ws pattern (ws COMMA)?)* ws RBRACE
```
