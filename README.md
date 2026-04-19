# ish - it's shit

`ish` (from the word some youtubers use to censor themselves, when saying
'shit') is not a shell for you. It will give your computer herpes. It will give
your girlfriend bad credit. It will give your friends good reason to make fun
of you. It is not designed to be the next big thing, nor is it designed to
support whatever language feature you are most passionate about. All it is, is
my superset of POSIX sh, inspired by Elixir, and refined to my own tastes.

The inspiration came from looking at Erlang, and thinking, "wait, the kernel
gives me almost all of this; and what it doesn't, goroutines can." But I did
not want to merely recreate `fish` or make a worse `iex`, so I took on the
constraint of POSIX sh compatibility, because then no pre-existing POSIX sh
scripts will fail when run in `ish`, unless they use `bash` or `zsh`-specific
features.

That being said; you have been warned.

## What it does

POSIX sh + Elixir-like extensions in the same session:

- Atoms, tuples, lists, maps, floats
- Pattern matching and destructuring
- First-class functions with multi-clause dispatch and guards
- Lambdas (`\x -> x * 2`)
- Value pipes (`|>`) alongside Unix pipes (`|`) with transparent auto-coercion
- `try`/`rescue` error handling
- Lightweight processes with message passing (`spawn`, `send`, `receive`)
- OTP-style supervision trees
- JSON/CSV/TSV bridge functions for structured data
- Tail call optimization for unbounded process loops
- Debugger with stack traces (`-D`) and Elixir-style tracing (`set -X`)
- Login shell support with profile sourcing

Every POSIX sh script runs unmodified. The two syntaxes coexist without ambiguity.

## Build

```
go build -o ish ./cmd/ish
```

Requires Go 1.21+.

## Usage

```
./ish                        # interactive REPL
./ish script.ish             # run a script
./ish -c 'echo hello'        # one-liner
./ish --version              # print version
./ish -l                     # login shell mode
./ish -D script.ish          # run with debugger enabled
```

## Quick taste

```sh
# POSIX works as expected
ls -la | grep .go | wc -l

# ish adds typed data and pattern matching
{status, val} = {:ok, 42}
echo $status                  # :ok

# functions with dispatch
fn fib 0 do 0 end
fn fib 1 do 1 end
fn fib n when n > 1 do
  fib (n - 1) + fib (n - 2)
end
fib 10                        # 55

# value pipes
range 1, 11 |> filter \x -> x > 5 |> length   # 5

# pipes auto-coerce between bytes and values
ls |> map \f -> upcase f | sort
[3, 1, 2] | sort | cat

# concurrency
pid = spawn fn do
  receive do
    {:ping, sender} -> send sender, :pong
  end
end
send pid, {:ping, self}
receive do
  :pong -> echo "got pong"
end
```

## Documentation

- **[Tutorial](docs/tutorial.md)** -- a narrative guide with a fox and an AI on a road trip
- **[Language Reference](docs/language.md)** -- every feature, every builtin, every disambiguation rule
- **[Examples](examples/)** -- complete working scripts (key-value store, pub-sub, health checker, supervision trees)

## Tests

```
go test -race ./...
```
