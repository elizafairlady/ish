# ish Language Reference

## What is ish

ish is a POSIX-compatible shell combined with a functional programming language inspired by Elixir. Written in Go, it supports standard shell features (pipelines, redirections, control flow, job control) alongside algebraic data types, pattern matching, first-class functions with multi-clause dispatch and guards, lightweight processes with message passing, and OTP-style supervision trees. POSIX and ish syntax coexist in the same session without ambiguity.

## Running ish

- **Interactive REPL:** `ish`
- **Script file:** `ish script.ish`
- **One-liner:** `ish -c 'command'`
- **Startup file:** `~/.ishrc` is sourced on every launch (interactive and script modes)
- **`$SHELL`** is set to the ish binary path automatically

In interactive mode, expression results that are not `nil` are printed automatically.

When invoked as `ish script.ish arg1 arg2`, the positional parameters `$1`, `$2`, etc. are set to `arg1`, `arg2`, etc. and `$0` is set to the script filename.

## Shell Basics

### Commands

```
cmd arg1 arg2
```

Commands follow a uniform call model. Resolution order:

1. Alias expansion (if the command name has a matching alias, it is expanded and re-parsed)
2. User-defined function (ish `fn` or POSIX `name() {}`)
3. Native standard library function
4. Built-in command
5. External command on `$PATH`

Arguments are separated by whitespace. Commas between arguments are allowed and ignored (for ish-style function calls like `add 3, 4`).

**Prefix assignments** apply to a single command, POSIX-style:

```
FOO=bar cmd args...    # FOO is set for the duration of cmd
```

### Quoting

- **Single quotes:** literal text, no expansion whatsoever. `'$HOME'` stays `$HOME`.
- **Double quotes:** `$var`, `${var}`, and `#{expr}` are expanded. Backslash escapes `"`, `\`, `$`, `` ` ``, and newline. Other backslash sequences pass through literally (e.g. `\n` stays `\n`). Note: `$(cmd)` and `$((expr))` inside double-quoted strings do not currently substitute correctly; use `#{expr}` interpolation or unquoted `$(cmd)` instead.
- **Backtick substitution:** `` `cmd` `` is equivalent to `$(cmd)`. Inside backticks, `\` before `$`, `` ` ``, `\`, or `"` strips the backslash (POSIX).
- **Backslash:** outside quotes, escapes the next character.
- Quotes can appear inside words: `FOO="hello world"` is a single assignment token. Mixed quoting is supported: `"hello"'world'` produces `helloworld`.

### Comments

`#` starts a comment that runs to the end of the line, unless it appears as `#{` which begins string interpolation.

```
echo hello  # this is a comment
```

### Variables

**POSIX assignment** (no spaces around `=`):

```
VAR=value
NAME="hello world"
```

The value undergoes `$var` expansion (unless single-quoted).

**Special parameters:**

| Parameter | Meaning |
|-----------|---------|
| `$?` | Exit code of last command |
| `$$` | PID of the shell process |
| `$!` | PID of last background process |
| `$@` | All positional parameters as separate words |
| `$*` | All positional parameters joined by the first character of `$IFS` (or space if `IFS` is unset) |
| `$#` | Number of positional parameters |
| `$0` | Name of the shell or script |
| `$1`..`$9` | Individual positional parameters |
| `$10`, `$11`, ... | Multi-digit positional parameters (consumed greedily) |

Undefined variables expand to the empty string (unless `set -u` is active, which causes an error).

**Parameter expansion:**

| Form | Meaning |
|------|---------|
| `${VAR}` | Braced form for disambiguation |
| `${#VAR}` | Length of the value of `VAR` (string length) |
| `${VAR:-default}` | Use `default` if `VAR` is unset or empty |
| `${VAR-default}` | Use `default` if `VAR` is unset (empty is OK) |
| `${VAR:+alt}` | Use `alt` if `VAR` is set and non-empty |
| `${VAR+alt}` | Use `alt` if `VAR` is set |
| `${VAR:=default}` | Set `VAR` to `default` if unset or empty, then expand |
| `${VAR:?message}` | Error with `message` if `VAR` is unset or empty |
| `${VAR#pattern}` | Remove shortest prefix matching glob `pattern` |
| `${VAR##pattern}` | Remove longest prefix matching glob `pattern` |
| `${VAR%pattern}` | Remove shortest suffix matching glob `pattern` |
| `${VAR%%pattern}` | Remove longest suffix matching glob `pattern` |
| `${VAR/pattern/replacement}` | Replace first occurrence of `pattern` |
| `${VAR//pattern/replacement}` | Replace all occurrences of `pattern` |

The `default` value in `:-`, `:+`, `:=`, and `:?` forms is itself subject to parameter expansion.

Prefix/suffix removal (`#`, `##`, `%`, `%%`) uses shell glob patterns where `*` matches any characters (including `/`, unlike `filepath.Match`).

### Pipelines

```
cmd1 | cmd2          # stdout of cmd1 to stdin of cmd2
cmd1 |& cmd2         # stdout + stderr of cmd1 to stdin of cmd2
```

Pipelines can be chained: `cmd1 | cmd2 | cmd3`.

**Auto-coercion:** if the left side of `|` produces a value (not bytes), it is automatically converted to lines and fed as stdin. Lists become one element per line; scalars become their string representation:

```
[1, 2, 3] | grep 2              # prints "2"
range 1, 5 | wc -l              # prints "4"
42 | cat                         # prints "42"
```

### Operators

```
cmd1 && cmd2         # run cmd2 only if cmd1 succeeds (exit 0)
cmd1 || cmd2         # run cmd2 only if cmd1 fails (exit non-0)
cmd1 ; cmd2          # run sequentially
cmd &                # run cmd in background
```

`&&` and `||` work with both POSIX exit codes and ish expression truthiness. Before evaluating the right side, ish syncs the exit code based on the left value's truthiness.

### Redirections

```
cmd > file           # stdout to file (truncate)
cmd >> file          # stdout to file (append)
cmd < file           # stdin from file
cmd 2> file          # stderr to file
cmd 2>> file         # stderr to file (append)
cmd 2>&1             # stderr to stdout (fd duplication)
cmd >&2              # stdout to stderr (fd duplication)
cmd &> file          # stdout + stderr to file (truncate)
cmd &>> file         # stdout + stderr to file (append)
```

File descriptor numbers can prefix redirect operators (e.g., `2>`, `0<`). The `>&N` syntax duplicates a file descriptor.

### Heredocs and Herestrings

**Heredocs:**

```
cmd <<DELIM
body text here
$var is expanded
DELIM
```

- `<<-DELIM` strips leading tabs from the body.
- `<<'DELIM'` or `<<"DELIM"` (quoted delimiter) suppresses all variable expansion in the body.
- In unquoted heredocs, backslash-newline continuation is processed (the `\` and newline are removed).

**Herestrings:**

```
cmd <<< "string"
```

Feeds the string (plus a trailing newline) to the command's stdin. Variable expansion is performed on unquoted herestrings.

### Control Flow

**if / then / elif / else / fi** (POSIX):

```
if cond; then
  body
elif cond; then
  body
else
  body
fi
```

**for / in / do / done:**

```
for var in word1 word2 word3; do
  body
done
```

Words undergo variable expansion, field splitting, and glob expansion. `for var in word; do` also accepts `end` instead of `done`.

**while / do / done:**

```
while cond; do
  body
done
```

`while` also accepts `end` instead of `done`. Returns exit code 0 on normal termination.

**until / do / done:**

```
until cond; do
  body
done
```

Like `while` but inverts the condition. Also accepts `end` instead of `done`.

**case / in / esac:**

```
case $word in
  pattern1)
    body
    ;;
  pattern2|pattern3)
    body
    ;;
  *)
    default body
    ;;
esac
```

Patterns support glob matching (`*`, `?`, `[...]`) and alternation with `|`. An optional leading `(` before the pattern is allowed (POSIX).

**Loop control:** `break`, `continue`

**Function return:** `return [code]`

### Subshells and Groups

```
(cmd1; cmd2)         # subshell: runs in a child environment (variable changes don't leak)
{ cmd1; cmd2; }      # group: runs in the current environment
```

Subshells save and restore the process working directory and umask. The subshell's exit code is propagated to the parent.

### Functions (POSIX)

```
name() {
  body
}
```

Arguments accessed via `$1`, `$2`, etc. inside the body. `$@` expands to all arguments as separate words. `return [code]` exits the function.

### Command Substitution

```
result = $(command)
echo $(date)
```

Captures stdout, strips trailing newlines. The result of ish expressions is also captured (non-nil, non-string values are converted via `.String()`).

**Note:** Use the ish binding form `result = $(command)` (spaces around `=`) to capture command output into a variable. Command substitution works in argument position (`echo $(date)`) and in ish binding right-hand sides, but not currently inside double-quoted strings (`"$(cmd)"` does not substitute correctly -- use `#{expr}` interpolation or unquoted `$(cmd)` instead).

### Arithmetic Expansion

```
echo $((2 + 3))
x=5
echo $((x * 2))
```

`$(( ))` evaluates an arithmetic expression. Variables inside the expression are expanded (both `$var` and bare `var` forms are supported). The result is substituted as a string.

### Glob Expansion

Arguments containing `*`, `?`, or `[` are expanded as file globs before being passed to commands. If no files match, the pattern is passed through literally. Quoted arguments are not glob-expanded.

### Word Splitting

After variable expansion in unquoted contexts, the result is split into fields on IFS characters. If `$IFS` is set, it is used as the delimiter set (whitespace IFS characters are collapsed; non-whitespace delimiters produce empty fields). If `$IFS` is not set, whitespace splitting is used. If `$IFS` is empty, no splitting occurs.

Quoted strings (`"..."` and `'...'`) are not split. Each `$@` expansion produces separate words.

## Test Expressions

The `test` builtin (also invoked as `[`) implements a recursive descent evaluator supporting:

**Grammar:**

```
or_expr   := and_expr ( -o and_expr )*
and_expr  := not_expr ( -a not_expr )*
not_expr  := ! not_expr | primary
primary   := ( or_expr ) | unary_op operand | operand binary_op operand | operand
```

**File test operators:**

| Operator | Meaning |
|----------|---------|
| `-f file` | True if file exists and is a regular file |
| `-d path` | True if path exists and is a directory |
| `-e path` | True if path exists |
| `-s file` | True if file exists and has size > 0 |
| `-r file` | True if file is readable |
| `-w file` | True if file is writable |
| `-x file` | True if file is executable |
| `-L file` / `-h file` | True if file is a symbolic link |
| `-p file` | True if file is a named pipe (FIFO) |
| `-S file` | True if file is a socket |
| `-t fd` | True if file descriptor is a terminal |

**String test operators:**

| Operator | Meaning |
|----------|---------|
| `-n str` | True if string is non-empty |
| `-z str` | True if string is empty |

**String comparison operators:**

| Operator | Meaning |
|----------|---------|
| `str = str` | String equality |
| `str == str` | String equality (alternative) |
| `str != str` | String inequality |

**Numeric comparison operators:**

| Operator | Meaning |
|----------|---------|
| `n -eq n` | Numeric equal |
| `n -ne n` | Numeric not equal |
| `n -lt n` | Numeric less than |
| `n -le n` | Numeric less than or equal |
| `n -gt n` | Numeric greater than |
| `n -ge n` | Numeric greater than or equal |

**Logical operators:**

| Operator | Meaning |
|----------|---------|
| `! expr` | Negation |
| `expr -a expr` | Logical AND |
| `expr -o expr` | Logical OR |
| `( expr )` | Grouping |

A bare string operand is true if non-empty. Returns exit code 0 for true, 1 for false, 2 for syntax errors.

## Builtins

| Builtin | Description |
|---------|-------------|
| `echo [-neE] args...` | Print arguments separated by spaces. `-n` suppresses trailing newline. `-e` enables backslash escape interpretation (`\n`, `\t`, `\r`, `\a`, `\b`, `\f`, `\v`, `\\`, `\0NNN` octal, `\c` stop output). `-E` disables escapes (default). Flags can be combined (e.g. `-ne`). |
| `cd [dir]` | Change directory. No argument goes to `$HOME`. `cd -` goes to `$OLDPWD`. Sets `PWD` and `OLDPWD` (both exported). |
| `exit [code]` | Exit the shell with the given code (default 0). |
| `export [NAME=value] [NAME]...` | With `NAME=value`, set and export a variable to child processes. With bare `NAME`, export an existing variable without changing its value. |
| `unset [-f] [-v] name...` | Remove variables (`-v`, default) or functions (`-f`) from the environment. |
| `set [flags] [-- args...]` | No arguments: print all variables in the current scope. `set -- a b c` sets positional parameters. Flags: `-e` (exit on error), `-u` (error on unset variables), `-x` (print commands before execution), `-o pipefail`. Prefix with `+` to disable (e.g. `+e`). |
| `shift [n]` | Shift positional parameters left by n (default 1). |
| `return [code]` | Return from a function with exit code (default 0). |
| `break` | Break out of a `for`, `while`, or `until` loop. |
| `continue` | Skip to the next iteration of a loop. |
| `true` / `:` | No-op, always succeeds (exit 0). |
| `false` | Always fails (exit 1). |
| `test expr` / `[ expr ]` | Evaluate a conditional expression (see Test Expressions). |
| `read [-p prompt] [-r] [-s] [-t sec] [-n count] [var...]` | Read from stdin. `-p` displays a prompt on stderr. `-r` disables backslash processing. `-s` suppresses echo (silent mode, for passwords). `-t` sets a timeout in seconds. `-n` reads exactly N characters (no newline required). With no variable names, stores in `$REPLY`. Multiple names split the line on IFS (last variable gets the remainder). |
| `exec [cmd args...]` | With arguments: replace the shell process with the given command (uses `execve`). With no arguments but with redirections: apply the redirections to the current shell (e.g. `exec >file` redirects shell stdout). |
| `eval args...` | Concatenate arguments with spaces and evaluate them as ish source code. Non-nil results are printed. |
| `source file [args...]` / `. file [args...]` | Read and execute commands from a file in the current environment. If the filename has no `/`, searches `$PATH`. Additional arguments set positional parameters for the sourced file (restored after). |
| `readonly [-p] [name=value] [name]...` | Mark variables as readonly. `readonly -p` or no arguments lists all readonly variables. `readonly NAME=value` sets and marks. `readonly NAME` marks an existing variable. Readonly variables cannot be reassigned or unset. |
| `trap ['cmd' signal...] [-l] [- signal...]` | No arguments: list all traps. `trap 'cmd' SIG` sets a handler for the signal (runs `cmd` when signal arrives). `trap - SIG` resets to default. `trap -l` lists valid signals. Supported signals: `INT`, `TERM`, `HUP`, `QUIT`, `USR1`, `USR2`. Pseudo-signals: `EXIT` (fired at shell exit), `ERR` (fired when a command fails). An empty command string ignores the signal. Signal names accept optional `SIG` prefix and are case-insensitive. |
| `times` | Print accumulated user and system CPU times for the shell and its children. |
| `type name...` | Show whether a name is a shell builtin, a function (user or native), or an external command (prints its path). Returns exit code 1 if any name is not found. |
| `pwd` | Print the current working directory. |
| `printf format [args...]` | Formatted output. Format specifiers: `%s` (string), `%d`/`%i` (integer), `%o` (octal), `%x`/`%X` (hex), `%f` (float), `%c` (single character), `%%` (literal `%`). Supports field width, precision, and flags (`-`, `+`, `0`, ` `, `#`). Backslash escapes in the format string: `\n`, `\t`, `\r`, `\\`, `\a`, `\b`, `\f`, `\v`, `\0NNN`. If there are more arguments than format specifiers, the format is reused. |
| `wait [pid_or_jobspec]` | No arguments: wait for all background jobs. With a PID or `%N` job spec: wait for that specific job and return its exit code. Also works with ish process PIDs (from `spawn`). |
| `kill [-signal] pid_or_jobspec...` | Send a signal to processes. Default signal is TERM. Signal can be specified as `-SIGNAME`, `-name`, or `-N`. `kill -l` lists signals. Job specs (`%1`, `%2`) are supported. Signals: HUP(1), INT(2), QUIT(3), KILL(9), USR1(10), USR2(12), TERM(15), CONT(18), STOP(19). |
| `getopts optstring name [args...]` | Parse command options. `optstring` is a string of valid option characters; a trailing `:` means the option takes an argument. `name` receives the current option character. Uses `$OPTIND` to track position. If `args` are omitted, parses `$@`. Sets `$OPTARG` for options with arguments. Returns 1 when all options are parsed. Supports bundled options (e.g. `-abc`). |
| `umask [mask]` | No arguments: print the current umask in octal (`%04o`). With an octal argument: set the umask. |
| `ulimit [-a] [-flag] [value]` | Get or set resource limits. `-a` shows all limits. Flags: `-c` (core size), `-d` (data size), `-f` (file size), `-n` (open files), `-s` (stack size), `-t` (CPU time), `-u` (max processes), `-v` (virtual memory). `unlimited` is accepted as a value. |
| `jobs` | List all background jobs with their ID, status (Running/Stopped/Done), and command string. |
| `fg %N` | Bring job N to the foreground. Sends SIGCONT if stopped, then waits for the process to finish. |
| `bg %N` | Resume stopped job N in the background. Sends SIGCONT. |
| `local [name=value] [name]...` | Declare local variables in the current scope. `local x=5` sets and scopes. `local x` declares as empty string. Writes to the current scope only (never walks the parent chain). |
| `alias [name=value]...` | No arguments: list all aliases. `alias ll='ls -la'` defines an alias. `alias name` shows that alias. Aliases are expanded when a command name matches; the expansion is re-parsed with the original arguments appended. Recursive aliases (where the expansion starts with the same name) are not expanded. |
| `unalias [-a] [name...]` | Remove aliases. `-a` removes all aliases. |
| `command [-v\|-V] name [args...]` | `-v name`: print the path of the command (or the name if it is a builtin), exit 1 if not found. `-V name`: verbose, equivalent to `type`. Without flags: run `name` directly, bypassing alias and function lookup. |

## ish Extensions

### Data Types

| Type | Syntax | Example |
|------|--------|---------|
| Integer | Bare digits | `42`, `-3` |
| String | Quoted text | `"hello"`, `'literal'` |
| Atom | Colon-prefixed identifier | `:ok`, `:error`, `:timeout` |
| Tuple | Braces with commas | `{:ok, "data"}`, `{1, 2, 3}` |
| List | Brackets with commas | `[1, 2, 3]`, `[]` |
| Map | Percent-brace | `%{name: "alice", age: 30}` |
| Pid | Process identifier | returned by `spawn`, displayed as `#PID<n>` |
| Function | First-class value | returned by `fn` or `\`, displayed as `#Function<name/clauses>` |
| `nil` | Null value | `nil` |
| `true` | Boolean true | `:true` atom |
| `false` | Boolean false | `:false` atom |

**Truthiness rules:**

- `nil` is falsy
- `:false` and `:nil` atoms are falsy
- Empty string `""` is falsy
- Integer `0` is falsy
- Everything else is truthy (including empty tuples, lists, and maps)

**Structural equality:** Values are compared by kind and content. Tuples, lists, and maps compare element-by-element. Maps compare by key set and values (order-independent for equality). Different kinds are never equal (e.g., `"3"` does not equal `3`).

**Display:** `.String()` converts values to their display form. `.Inspect()` is like `.String()` but quotes strings. `.ToStr()` converts any value to a plain string (strings return their raw content, other types return their `.String()` form).

### Bindings and Pattern Matching

Spaces around `=` create an ish pattern match/bind (as opposed to POSIX `VAR=value` with no spaces):

```
x = 42                           # bind x to 42
name = "world"                   # bind name
result = 2 + 3                   # bind to expression result
```

The right-hand side is evaluated as an expression (supporting pipelines, function calls, and all ish expressions). Pattern matching destructures complex values:

```
{:ok, val} = {:ok, "data"}       # destructure tuple, bind val to "data"
[a, b, c] = [1, 2, 3]           # destructure list
{:ok, {x, y}} = {:ok, {1, 2}}   # nested destructuring
```

**Head | tail patterns** for lists:

```
[h | t] = [1, 2, 3]             # h = 1, t = [2, 3]
[a, b | rest] = [1, 2, 3, 4]    # a = 1, b = 2, rest = [3, 4]
```

Pattern matching rules:

- **Variables** always match and bind the value to the name (using `SetLocal`, so bindings stay in the current scope)
- **`_`** (underscore) matches anything but does not bind
- **Literals** (integers, strings, atoms) must match exactly or a match error is raised
- **Tuples** `{a, b}` match a tuple of the same arity, recursively matching elements
- **Lists** `[a, b, c]` match a list of the same length, recursively matching elements
- **Lists with rest** `[h | t]` match a list with at least as many elements as there are head patterns; the rest variable binds the remaining elements as a list
- A mismatch raises a match error

### String Interpolation

Inside double-quoted strings, both `$var` and `#{expr}` are expanded:

```
name = "world"
echo "hello $name"          # hello world
echo "hello #{name}"        # hello world
echo "2 + 2 = #{2 + 2}"    # 2 + 2 = 4
echo "home is $HOME"        # home is /home/user
```

`#{expr}` first checks if `expr` is a variable name; if so, it expands to the variable's value. Otherwise it evaluates `expr` as ish source code (via the command substitution callback) and uses the result.

Single-quoted strings have no interpolation.

### Functions

**Named function with explicit parameters:**

```
fn name param1, param2 do
  body
end
```

Commas between parameters are optional:

```
fn add a b do
  a + b
end
```

The function returns the value of the last expression in its body.

**Multi-clause functions** (same name, different patterns):

```
fn fib 0 do 0 end
fn fib 1 do 1 end
fn fib n when n > 1 do
  fib (n - 1) + fib (n - 2)
end
```

Clauses are appended to the function in definition order. When called, the first clause whose pattern matches and whose guard passes is executed. If no clause matches, an error is raised showing the arguments.

**Guards:**

```
fn classify n when n > 0 do :positive end
fn classify n when n < 0 do :negative end
fn classify 0 do :zero end
```

Guards appear after `when` and before `do`. They must evaluate to a truthy value. If a guard raises an error, the clause is skipped (as if the guard returned false).

**Multi-clause dispatch in a single block:**

All clauses can be written in one `fn name do ... end` block. Each clause is a pattern, an optional guard, an arrow, and a body:

```
fn classify do
  n when n > 0 -> :positive
  0 -> :zero
  _ -> :negative
end
```

**Anonymous functions (fn as expression):**

In expression context (RHS of `=`, argument to `spawn`/`map`/etc.), `fn` produces a function value without requiring a name:

```
add = fn a, b do
  a + b
end
add 3, 4                     # 7

doubled = fn x do x * 2 end
doubled 5                    # 10
```

Anonymous multi-clause dispatch uses `fn do ... end` with arrow clauses:

```
f = fn do
  0 -> :zero
  n when n > 0 -> :positive
  _ -> :negative
end
```

Anonymous functions can be passed directly as arguments:

```
spawn fn do
  echo "hello from a process"
end

map [1, 2, 3], fn x do x * 2 end
```

The parser distinguishes named vs anonymous by context: at statement level (command position), `fn` expects a name. In expression position, `fn` is anonymous and parameters come directly.

### Lambdas

The `\` (backslash) syntax creates anonymous function values:

```
\x -> x * 2              # single parameter
\a, b -> a + b           # multiple parameters
\ -> 42                  # zero parameters
```

Lambdas are single-expression: the body is everything after `->` on the same line. They are always values (never statements).

Lambdas can be passed directly as arguments:

```
map [1, 2, 3], \x -> x * 2
filter [1, 2, 3, 4], \x -> x > 2
reduce [1, 2, 3], 0, \acc, x -> acc + x
```

And used with the pipe operator:

```
result = [1, 2, 3] |> map \x -> x * 2    # [2, 4, 6]
```

**When to use which:**

| Syntax | Use case |
|--------|----------|
| `fn name params do body end` | Named functions (statement level) |
| `fn name do clauses end` | Named multi-clause dispatch (statement level) |
| `name = fn params do body end` | Function value bound to a variable |
| `name = fn do clauses end` | Multi-clause dispatch bound to a variable |
| `fn do clauses end` | Anonymous multi-clause (inline in spawn, map, etc.) |
| `\params -> expr` | Short anonymous functions (callbacks, transforms) |

### Calling Functions

```
result = add 3, 4       # call named function with two arguments
greet "world"            # call with one argument
r = fib 10              # recursive call
```

Functions are called like commands -- the function name followed by arguments. For user-defined and native functions, arguments are evaluated as ish expressions (variable lookup applies). For builtins and external commands, arguments are evaluated as command arguments (bare words are literal strings, no variable lookup).

**Calling function values stored in variables:**

Functions stored in variables (from lambdas, `fn params do...end`, or `fn do...end`) can be called the same way as named functions:

```
doubled = fn x do x * 2 end
doubled 5                # 10

classify = fn do
  0 -> :zero
  _ -> :other
end
classify 0               # :zero
```

In command position, if a variable holds a function value, it is called with the provided arguments. In expression position (e.g., `x = f`), the function value is returned without calling it, allowing functions to be passed as values.

**All functions are first-class values.** Named functions defined with `fn name do...end` can be passed as values in expression position, just like lambdas and fn expressions:

```
fn double x do x * 2 end
map [1, 2, 3], double       # [2, 4, 6]
f = double                   # store named function in a variable
f 5                          # 10
```

**POSIX vs ish function argument evaluation:**

- User functions (ish `fn` or POSIX `name(){}`): arguments are evaluated as ish expressions. `add x y` passes the *values* of variables `x` and `y`.
- Builtins and external commands: arguments are literal strings with `$var` expansion but no variable lookup. `echo hello` passes the literal string `"hello"`.

### Match Expression

```
match expr do
  pattern1 -> body1
  pattern2 -> body2
  _ -> default_body
end
```

Evaluates `expr`, then tries each clause's pattern in order. The first matching pattern executes its body in a new scope where the pattern's variables are bound. If no clause matches, an error is raised.

```
result = match status do
  {:ok, val} -> val
  {:error, reason} -> echo "error: #{reason}"
  _ -> :unknown
end
```

### Functional Pipe

```
expr |> fn_name
```

Passes the result of `expr` as the **first** argument to `fn_name`. Additional arguments can follow the function name. Chains left to right:

```
fn double x do x * 2 end
fn inc x do x + 1 end

result = 5 |> double |> inc    # inc(double(5)) = 11
result = 3 |> inc |> double    # double(inc(3)) = 8
```

The pipe operator works with user functions, native stdlib functions, builtins, and external commands. For builtins and external commands, the piped value is converted to a string.

**Auto-coercion:** if the left side of `|>` is a command that produces bytes (an external command or builtin), its stdout is captured and automatically split into a list of lines (`from_lines`). This means you can pipe command output directly into value functions:

```
ls |> map \f -> upcase f         # list of uppercased filenames
ls |> filter \f -> ends_with f, ".go" |> length  # count .go files
```

**Explicit bridge override:** if the right side of `|>` is a bridge function (`from_json`, `from_csv`, `from_tsv`, `from_lines`), the raw string is passed instead of auto-coercing to lines:

```
curl -s api.example.com |> from_json |> map \x -> x.name
cat data.csv |> from_csv |> filter \row -> row.age > 30
```

### ish if / do / end

```
if expr do
  body
end

if expr do
  body
else
  other_body
end
```

Evaluates `expr` as an ish expression and checks its truthiness. This coexists with POSIX `if cond; then body; fi` which checks the exit code of `cond`.

The parser distinguishes the two forms: `then` after the condition selects POSIX mode, `do` selects ish mode.

### while / for with end

`while`, `until`, and `for` loops accept `end` as an alternative block terminator to `done`:

```
for i in 1 2 3 do
  echo $i
end

while true do
  echo "loop"
  break
end
```

### Map Access

```
m = %{x: 10, y: 20}
r = m.x                  # 10
```

The dot syntax accesses map fields. In expression context it is parsed as an access node. In string/word expansion, `$m.x` style does not work directly (the dot is part of the variable name resolution), but dot access on variables works via the evaluator: if `varname` contains a dot, it splits on `.` and resolves `obj.field`.

### Arithmetic

Operators (in order of increasing precedence):

| Precedence | Operators | Description |
|------------|-----------|-------------|
| 1 | `==` `!=` | Equality / inequality |
| 2 | `<` `>` `<=` `>=` | Comparison |
| 3 | `+` `-` | Addition / subtraction |
| 4 | `*` `/` | Multiplication / integer division |

Parentheses override precedence:

```
r = 2 + 3 * 4       # 14 (multiplication first)
r = (2 + 3) * 4     # 20
```

**Integer arithmetic:**

```
r = 10 + 5          # 15
r = 10 - 3          # 7
r = 6 * 7           # 42
r = 20 / 4          # 5 (integer division)
```

Division by zero raises an error.

**Comparisons** return `:true` or `:false`:

```
r = 5 == 5          # :true
r = 5 != 6          # :true
r = 3 < 5           # :true
r = 5 > 3           # :true
r = 3 <= 3          # :true
r = 5 >= 5          # :true
```

**General equality** works on all value types (not just integers). Structural comparison for tuples, lists, and maps.

**String concatenation** with `+`:

```
r = "hello" + " " + "world"    # "hello world"
r = 42 + " things"             # "42 things"
```

If either operand is a string, `+` performs concatenation.

**Unary operators:**

```
r = -42             # negation
r = !true           # :false (logical not)
r = !false          # :true
```

`!` returns `:true` or `:false` based on the operand's truthiness. `-` negates integers (error on non-integers).

### try / rescue / end

```
try
  body
rescue
  pattern1 -> handler1
  pattern2 -> handler2
end
```

Evaluates `body`. If it completes without error, returns the result. If it raises an error (but not `return`, `break`, `continue`, or `set -e` signals), the error is wrapped as a tuple `{:error, "message"}` and matched against the rescue clauses:

```
result = try
  x = 1 / 0
rescue
  {:error, msg} -> echo "caught: #{msg}"
end
```

If no rescue clause matches, the error is re-raised. The `do` keyword after `try` is optional.

## Processes and OTP

ish provides lightweight processes backed by goroutines, with Erlang/Elixir-style message passing.

### spawn

```
pid = spawn fn do
  body
end
```

Runs the body in a new lightweight process with an isolated copy of the environment. Returns a pid. If the body evaluates to a function value, that function is called with no arguments.

Each process has a 256-slot buffered mailbox and a save queue for selective receive. Processes are registered in a global registry by ID.

### spawn_link

```
pid = spawn_link fn do
  body
end
```

Like `spawn`, but **bidirectionally links** the new process with the current process. If either process exits abnormally (with a non-`:normal` reason), the linked process is also killed with the same reason. Normal exits do not propagate through links.

### send / receive

```
send pid, message

receive do
  {:pattern, val} -> handle val
  other -> handle_other other
end
```

`send` delivers a message to a process's mailbox. If the mailbox is full or the process is dead, the send is silently dropped.

`receive` blocks until a matching message arrives. It implements **selective receive** semantics:

1. First, the save queue is scanned for a message matching any clause pattern.
2. If none found, messages are pulled from the mailbox channel one at a time. Matching messages are processed; non-matching messages are appended to the save queue for future receives.
3. This continues until a match is found or the process is closed.

Variables in the matching pattern are bound in a new scope for the clause body.

**Receive with timeout:**

```
receive do
  msg -> handle msg
after 5000 ->
  echo "timed out after 5 seconds"
end
```

The `after N ->` clause specifies a timeout in milliseconds. If no matching message arrives within the timeout, the after body is executed. The timeout also uses selective receive -- non-matching messages are saved, not discarded.

### self

```
pid = self
```

Returns the current process's pid. Every environment (including the top-level REPL) has an associated process.

### monitor

```
ref = monitor pid
```

Sets up a **one-way** monitor. When the monitored process exits, the monitoring process receives a message in its mailbox:

```
{:DOWN, ref, pid, reason}
```

Where `reason` is `:normal` for normal exits, or `{:error, message}` for abnormal exits. The ref is a unique integer identifying this monitor.

Monitors are one-directional -- the monitored process is not affected. Multiple monitors can be set on the same process.

### await

```
result = await pid
```

Blocks until the process finishes and returns its result value (the value of the last expression evaluated by the process). This is simpler than receive -- it does not involve the mailbox.

### supervise

```
sup = supervise :one_for_one do
  worker :name1 fn do body1 end
  worker :name2 fn do body2 end
end
```

Creates a supervisor process that manages child worker processes. Returns the supervisor's pid.

**Strategies:**

| Strategy | Behavior on crash |
|----------|-------------------|
| `:one_for_one` | Restart only the crashed child |
| `:one_for_all` | Stop and restart all children |
| `:rest_for_one` | Restart the crashed child and all children defined after it |

**Restart backoff:** The supervisor enforces a maximum restart rate of 3 restarts within 5 seconds. If this rate is exceeded, the supervisor shuts down with reason `{:shutdown, :too_many_restarts}` and stops all children.

Normal exits (`:normal` reason) do not trigger restarts -- the child is marked as dead. When all children have exited normally, the supervisor itself exits normally.

Workers are defined with `worker :name fn_expr` where `:name` is an atom and `fn_expr` is a function value (typically an inline `fn do ... end`). Each worker function is called with no arguments.

```
sup = supervise :one_for_one do
  worker :counter fn do
    # ... long-running process
  end
  worker :logger fn do
    # ... long-running process
  end
end
await sup    # blocks until all workers exit normally
```

## Standard Library

### List Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `hd` | `hd list` | First element of the list. Error on empty list. |
| `tl` | `tl list` | All elements except the first. Error on empty list. |
| `length` | `length val` | Length of a list, tuple, string, or map. |
| `append` | `append list, elem` | New list with `elem` appended at the end. |
| `concat` | `concat list1, list2` | Concatenation of two lists. |
| `map` | `map list, fn` | Apply `fn` to each element, return new list. |
| `filter` | `filter list, fn` | Keep elements where `fn` returns truthy. |
| `reduce` | `reduce list, acc, fn` | Left fold: `fn(acc, elem)` for each element. |
| `range` | `range start, stop` | List of integers `[start, start+1, ..., stop-1]`. Empty if `start >= stop`. |
| `at` | `at list, index` | Element at 0-based index. Error if out of bounds. |
| `each` | `each list, fn` | Apply `fn` to each element for side effects. Returns `nil`. |
| `sorted` | `sorted list` | New list with elements sorted (integers numerically, others by string representation). |
| `reverse` | `reverse list` | New list with elements in reverse order. |
| `any` | `any list, fn` | Returns `:true` if `fn` returns truthy for any element, `:false` otherwise. |
| `all` | `all list, fn` | Returns `:true` if `fn` returns truthy for all elements, `:false` otherwise. |
| `first` | `first list, fn` | Returns the first element where `fn` returns truthy, or `nil` if none. |
| `enumerate` | `enumerate list` | List of `{index, value}` tuples. `enumerate ["a", "b"]` returns `[{0, "a"}, {1, "b"}]`. |

### String Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `split` | `split str, delim` | Split string on delimiter, return list of strings. |
| `join` | `join list, delim` | Join list elements into a string with delimiter. |
| `trim` | `trim str` | Remove leading and trailing whitespace. |
| `upcase` | `upcase str` | Convert to uppercase. |
| `downcase` | `downcase str` | Convert to lowercase. |
| `replace` | `replace str, old, new` | Replace first occurrence of `old` with `new`. |
| `replace_all` | `replace_all str, old, new` | Replace all occurrences of `old` with `new`. |
| `starts_with` | `starts_with str, prefix` | Returns `:true` or `:false`. |
| `ends_with` | `ends_with str, suffix` | Returns `:true` or `:false`. |
| `contains` | `contains str, substr` | Returns `:true` or `:false`. |
| `substring` | `substring str, start, len` | Extract a substring by 0-based start position and length. |
| `index_of` | `index_of str, substr` | Returns 0-based index of first occurrence, or `-1` if not found. |

### Map Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `put` | `put map, key, value` | New map with `key` set to `value`. |
| `delete` | `delete map, key` | New map with `key` removed. |
| `merge` | `merge map1, map2` | Combined map (`map2` wins on key conflicts). |
| `keys` | `keys map` | List of key strings (in insertion order). |
| `values` | `values map` | List of values (in insertion order). |
| `has_key` | `has_key map, key` | Returns `:true` or `:false`. |
| `get` | `get map, key` | Value for `key`, or `nil` if the key does not exist. |
| `pairs` | `pairs map` | List of `{key, value}` tuples (in insertion order). |

All map operations return new maps (maps are immutable values).

### Format Conversion (Bridge Functions)

These functions convert between strings and structured ish values. They are used to override the default auto-coercion (which splits on newlines) when your data has a different format:

| Function | Signature | Description |
|----------|-----------|-------------|
| `from_json` | `from_json str` | Parse JSON string into ish values. Objects become maps, arrays become lists, numbers become ints (when whole), booleans become atoms, null becomes `nil`. |
| `to_json` | `to_json value` | Serialize an ish value to a JSON string. Maps become objects, lists/tuples become arrays, atoms become strings (except `:true`/`:false` which become booleans), `nil` becomes null. |
| `from_csv` | `from_csv str` | Parse CSV text. If multiple rows, the first row is treated as headers and subsequent rows become maps. A single row returns a list of strings. Uses RFC 4180 parsing (handles quoted fields, embedded newlines). |
| `to_csv` | `to_csv list` | Serialize a list of maps to CSV text (keys from the first map become the header row). Also accepts a list of lists. |
| `from_tsv` | `from_tsv str` | Like `from_csv` but tab-delimited. |
| `to_tsv` | `to_tsv list` | Like `to_csv` but tab-delimited. |
| `from_lines` | `from_lines str` | Split a string on newlines into a list of strings. Trailing empty line is removed. |
| `to_lines` | `to_lines list` | Join a list of strings with newlines. |

The pipes handle most conversions automatically. `|>` auto-applies `from_lines` when the left side is a command; `|` auto-converts values to lines when piping to a command. Use explicit bridge functions when you need a different format:

```
# auto-coercion (default: lines)
ls |> map \f -> upcase f | sort

# explicit bridge for structured data
curl -s api.example.com |> from_json |> map \x -> x.name
cat data.csv |> from_csv |> filter \row -> row.age > 30

# explicit bridge for output
to_json data | jq .
```

### Utility Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `delay` | `delay ms` | Pause for `ms` milliseconds. Returns `nil`. Named `delay` instead of `sleep` to avoid shadowing the Unix `sleep` command. |

## How Disambiguation Works

The parser uses position and context to decide between POSIX shell syntax and ish expression syntax:

**`=` with and without spaces:**
- `VAR=value` (no spaces) -- POSIX assignment. The token is a single word containing `=` where the left side is a valid identifier.
- `pattern = expr` (spaces around `=`) -- ish pattern match/bind. Parsed when `=` is a separate token following a word.

**`|` vs `|>`:**
- `|` -- Unix pipe. Connects stdout to stdin. If the left side produces a value instead of bytes, it is auto-converted to lines.
- `|>` -- Functional pipe. Passes the left value as the first argument to the right function. If the left side is a command that produces bytes, its stdout is auto-converted to a list of lines (unless the right side is an explicit bridge function like `from_json`).

**Command position vs argument position:**
- In **command position** (first word of a statement): keywords (`if`, `for`, `while`, `fn`, `match`, `spawn`, etc.) trigger their respective parsers. A word followed by `()` is a POSIX function definition. A word followed by `=` (as a separate token) is an ish binding.
- In **argument position**: bare words are **literal strings** for builtins and external commands (no variable lookup -- `echo hello` passes `"hello"`). For user-defined functions and native functions, arguments are evaluated as **ish expressions** (variable lookup applies -- `add x y` passes the values of `x` and `y`).
- Parenthesized expressions in argument position `(expr)` are always evaluated as expressions, regardless of whether the command is a builtin or function.

**`/` disambiguation:**
- After integer literals, `)`, or `]`: treated as the division operator.
- After a word followed by whitespace: treated as division.
- Otherwise: treated as part of a path (word character).

**`[` disambiguation:**
- At command position: the `[` test builtin.
- At expression position: if the content contains `,`, `|`, or is immediately `]` (empty list), it is a list literal. Otherwise falls back to the `[` builtin.

**`{` disambiguation:**
- If followed by an atom or immediate `}`: tuple expression.
- Otherwise: group command `{ cmd; cmd; }`.

**`then` vs `do` after `if`:**
- `then` -- POSIX if: uses `elif`/`else`/`fi` block terminators.
- `do` -- ish if: uses `else`/`end` block terminators.

**`done` vs `end` for loops:**
- Both `done` (POSIX) and `end` (ish) are accepted as block terminators for `for`, `while`, and `until`.

**`\` (backslash) at token start:**
- At the start of a token (after whitespace or an operator): lambda syntax. `\x -> x * 2` creates an anonymous function.
- Inside a word or string: escape character (existing POSIX behavior). `echo hello\ world` escapes the space.
- The lexer distinguishes these by position: `\` at a token boundary emits a `TBackslash` token; `\` mid-word is consumed by `lexWord` as an escape.

**`fn` disambiguation:**
- `fn do ... end` -- anonymous multi-clause dispatch (value). The `do` immediately after `fn` signals anonymous.
- `fn name ... do ... end` -- named function definition (statement). The word after `fn` is always the function name.
- Lambdas (`\params -> expr`) are the preferred syntax for simple anonymous functions.

**General rule:** ish extensions never use tokens that are valid in POSIX shell at the same position. Atoms (`:word`), tuples (`{a, b}`), maps (`%{}`), pipe arrows (`|>`), lambdas (`\x -> expr`), and the `fn`/`match`/`spawn`/`receive`/`supervise` keywords occupy syntactic positions that are unambiguous.

## Prompt (PS1)

The prompt is controlled by the `PS1` variable. It supports Bash-compatible backslash escapes:

| Escape | Meaning |
|--------|---------|
| `\u` | Current username (`$USER` or `$LOGNAME`) |
| `\h` | Hostname (up to first `.`) |
| `\H` | Full hostname |
| `\w` | Current working directory (with `~` for home) |
| `\W` | Basename of current working directory |
| `\$` | `#` if root (UID 0), `$` otherwise |
| `\n` | Newline |
| `\t` | Current time in 24-hour `HH:MM:SS` format |
| `\T` | Current time in 12-hour `HH:MM:SS` format |
| `\@` | Current time in 12-hour `HH:MM AM/PM` format |
| `\d` | Date in `Mon Jan 02` format |
| `\e` | ASCII escape character (0x1B) |
| `\a` | ASCII bell character (0x07) |
| `\[` | Begin non-printing character sequence |
| `\]` | End non-printing character sequence |
| `\\` | Literal backslash |

After backslash escapes are processed, `$var` and `#{expr}` are expanded via `env.Expand`.

Default prompt (when PS1 is not set): `~/current/dir $ `

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| Left / Right arrows | Move cursor within line |
| Up / Down arrows | Browse command history |
| Home / Ctrl-A | Move to beginning of line |
| End / Ctrl-E | Move to end of line |
| Ctrl-K | Kill (delete) from cursor to end of line |
| Ctrl-U | Kill from cursor to start of line |
| Ctrl-W | Kill word backward |
| Ctrl-L | Clear screen |
| Ctrl-C | Cancel current line |
| Ctrl-D | Exit shell (on empty line) / delete character at cursor |
| Backspace | Delete character before cursor |
| Delete | Delete character at cursor |
| Tab | Tab completion (see below) |

**Tab completion:**

- `$` prefix: completes variable names from all scopes.
- Paths containing `/`, `.`, or `~`: completes filesystem paths (directories get a trailing `/`).
- Command position (first word): completes from builtins, user functions, native functions, and external commands on `$PATH`. Results are sorted and deduplicated.
- Argument position: completes from the current directory.
- Single match: completed inline with a trailing space (or `/` for directories).
- Multiple matches: the longest common prefix is filled in, and all candidates are displayed below the prompt.

Command history is saved to `~/.ish_history` (up to 1000 entries). Consecutive duplicate entries are not recorded. History is loaded on shell startup and saved after each new entry.

## Multi-line Input

In interactive mode, ish uses **speculative parsing** to detect unterminated constructs. After each line, the input is fed to the real parser. If the parser returns an error indicating an unterminated block (missing `fi`, `done`, `end`, `esac`, `}`, `)`, `do`, or `then`), ish prompts for continuation lines with `... ` until the input parses successfully or EOF is reached.

This approach is more accurate than token-counting heuristics. For example, `echo then` parses as a complete command (the word `then` is just an argument) and does not prompt for `fi`. Only genuinely incomplete constructs trigger multi-line continuation.
