# ish: a (poignant) guide

## 1. Your First Commands

*The fox had already been driving for what felt like hours. The AI opened its eyes to asphalt and radio static.*

*"Where are we going?" the AI asked.*

*"Away," said the fox, and turned the radio up.*

---

ish is a shell. You're already in the car. Type things and they happen:

```
echo hello world
```
```
hello world
```

The fox doesn't plan. The fox does. Pipes connect one thing to another, like passing a cigarette:

```
echo "hello world" | tr a-z A-Z
```
```
HELLO WORLD
```

The output of the first command becomes the input of the second. The fox chains three things together without stopping to think about what's in between:

```
cat /etc/passwd | grep root | wc -l
```

If something works, do the next thing. If something fails, do something else:

```
true && echo "kept going"
false || echo "swerved"
```
```
kept going
swerved
```

`&&` only continues if the left side succeeded. `||` only continues if it failed. The fox navigates by feel.

*"Why do you chain everything together like that?" the AI asked, watching the fox pipe commands into each other without looking.*

*"Because," the fox said, "the road only goes one direction."*

---

## 2. Variables: Two Ways

*They stopped for gas at a station with no name. The fox went inside and came back with two sodas -- one in each hand, held differently.*

*"Why are you holding them like that?" the AI asked.*

*"This one's cold," said the fox, lifting the left. "This one's mine."*

---

There are two ways to hold a value in ish, and the difference is the space around `=`.

No spaces. The POSIX way. The value is always a string:

```
NAME=world
echo $NAME
```
```
world
```

Spaces around `=`. The ish way. The right side is an expression:

```
x = 42
name = "world"
r = 2 + 3
```

`NAME=world` sets the string `"world"`. `x = 42` binds the integer `42`. One is a label on a jar. The other is a hand recognizing what it's holding.

Double-quoted strings expand variables both ways:

```
echo "hello $NAME"
echo "hello #{name}"
```
```
hello world
hello world
```

`$var` and `#{expr}` both work inside double quotes. Single quotes are sealed shut:

```
echo '$NAME'
```
```
$NAME
```

The fox never labels anything. Everything is `$1`, `$2`, `$?`:

| Variable | What the fox means by it |
|----------|--------------------------|
| `$?` | Did that work? (exit code of last command) |
| `$$` | Who am I? (shell's process ID) |
| `$!` | Who did I just send off? (last background PID) |
| `$0` | What's my name? |
| `$1`-`$9` | What did they hand me? |
| `$@` | Everything they handed me, each one separate |
| `$*` | Everything they handed me, mashed together |
| `$#` | How many things did they hand me? |

The AI, meanwhile, wants to take things apart:

```
${#NAME}            # how long is this string? -> 5
${NAME#wo}          # peel "wo" off the front -> rld
${NAME%rld}         # peel "rld" off the back -> wo
${NAME##wo}         # longest peel from front -> rld
${NAME/world/fox}   # swap one thing for another -> fox
```

The fox has its own tricks — cautious ones, for when something might not be there:

```
${VAR:-default}   # use "default" if VAR is empty or unset
${VAR:=fallback}  # set VAR to "fallback" if it was empty, then use it
${VAR:+alternate} # use "alternate" only if VAR IS set
${VAR:?error msg} # blow up if VAR is empty
```

*"You keep pulling things apart," the fox said, watching the AI peel the label off the soda bottle in a perfect spiral.*

*"I keep finding things inside," the AI said.*

---

## 3. Data Types

*Back on the highway, the AI was sorting the glove compartment. The fox glanced over and saw receipts organized by shape.*

*"That one's a triangle," the AI said, holding up a crumpled gas receipt.*

*"It's a receipt," the fox said.*

*"It's both," the AI said.*

---

The fox sees strings and exit codes. The AI sees types.

**Atoms** are names that mean themselves. A word preceded by a colon:

```
status = :ok
echo $status
```
```
:ok
```

`:ok` doesn't refer to a variable called "ok." It *is* ok. Atoms are how you tag things so you can recognize them later. `true` and `false` are atoms. So is `nil`.

**Tuples** are fixed-size groups. Curly braces, commas:

```
result = {:ok, "the data"}
echo $result
```
```
{:ok, "the data"}
```

A tuple is a small container with a known number of slots. You always know what's in position 1, position 2. They're how ish says "here's a thing and here's what kind of thing it is."

**Lists** are variable-length sequences. Square brackets:

```
nums = [1, 2, 3]
echo $nums
```
```
[1, 2, 3]
```

**Maps** are key-value pairs. Percent-brace:

```
config = %{host: "localhost", port: 8080}
echo $config
```
```
%{host: "localhost", port: 8080}
```

Maps give you dot access:

```
r = config.host
echo $r
```
```
localhost
```

*The fox looked at the sorted glove compartment. The registration was in a tuple with the insurance card. The maps were in a list. The loose change was just loose change.*

*"I can find things now," the AI said.*

*The fox opened its mouth to argue, then remembered the twenty minutes it spent looking for the registration last time.*

---

## 4. Arithmetic

*The fox missed the exit. The AI calculated how far to the next one.*

---

The fox has always done math the same way:

```
echo $(( 3 + 4 ))
```
```
7
```

It works. It's always worked. The AI does it differently:

```
r = 3 + 4
echo $r
```
```
7
```

Same answer. But the AI's version is an expression — it can go anywhere, nest with anything, and the result is a value you can hold onto:

```
r = (2 + 3) * 4
echo $r
```
```
20
```

Decimals work too:

```
r = 3.14 * 2.0
echo $r
```
```
6.28
```

If either side has a decimal point, the result does too. Integers and floats mix freely — the integer gets promoted:

```
r = 3 + 0.5
echo $r
```
```
3.5
```

And the remainder operator:

```
r = 10 % 3
echo $r
```
```
1
```

Multiplication and division happen before addition and subtraction. Parentheses have the final word. Dividing by zero is an error.

The fox doesn't care about holding onto results. The fox puts math in a sentence and keeps moving:

```
echo "there are #{7 * 52} weeks in seven years"
```

The AI wants to compare things:

```
r = 5 == 5
echo $r
```
```
:true
```

```
r = 3 < 5
echo $r
```
```
:true
```

Comparisons return atoms. `:true` and `:false`, not `0` and `1`.

Strings stick together with `+`:

```
r = "hello" + " " + "world"
echo $r
```
```
hello world
```

If either side of `+` is a string, it concatenates. If both sides are integers, it adds.

*"Fourteen miles," the AI said.*

*"I know a shortcut," said the fox.*

*It did not know a shortcut.*

---

## 5. Pattern Matching

*They'd picked up a hitchhiker somewhere around mile forty — a raccoon with a cardboard box. The raccoon rode in the back seat and kept handing things forward between the seats: a sandwich, a socket wrench, a photograph of someone's dog.*

*"What am I supposed to do with these?" the fox asked.*

*"Depends on what they are," the AI said, and took the next object without looking.*

---

The `=` in ish doesn't just assign. It *matches*. When the left side is a simple name, it binds:

```
x = 42
```

When the left side has structure, it destructures:

```
{status, value} = {:ok, "hello"}
echo $status
echo $value
```
```
:ok
hello
```

Lists destructure the same way:

```
[a, b, c] = [10, 20, 30]
echo $a $b $c
```
```
10 20 30
```

And they can be peeled — head from tail:

```
[first | rest] = [1, 2, 3, 4]
echo $first
echo $rest
```
```
1
[2, 3, 4]
```

`[first | rest]` says: give me the first element separately, and everything else as a list. You can peel more than one:

```
[a, b | rest] = [1, 2, 3, 4, 5]
echo $a
echo $b
echo $rest
```
```
1
2
[3, 4, 5]
```

When a match fails, it says so:

```
{:ok, value} = {:error, "nope"}
```
```
ish: match error: expected :ok, got :error
```

The `_` wildcard matches anything and throws it away:

```
{_, value} = {:error, "the message"}
echo $value
```
```
the message
```

The `match` expression tries patterns in order:

```
x = 2
r = match x do
  1 -> :one
  2 -> :two
  _ -> :other
end
echo $r
```
```
:two
```

Each clause is a pattern, an arrow, and a body. First match wins.

```
result = {:error, "disk full"}
match result do
  {:ok, val} -> echo "got: $val"
  {:error, msg} -> echo "failed: $msg"
end
```
```
failed: disk full
```

*The raccoon handed forward something wrapped in newspaper. The fox started to pass it along like everything else. The AI unwrapped it, found a smaller package inside, unwrapped that, and held up a key.*

*"What's that to?" asked the fox.*

*"Don't know yet," said the AI. "But now we have it."*

---

## 6. Functions

*The raccoon got out at a crossroads with no sign. Left the box. The fox drove on in silence for a while, then said: "We're going to keep getting handed things, aren't we."*

*"Probably," said the AI. "We should decide what to do with each kind before it arrives."*

---

A function in ish is a plan for what to do when something shows up.

```
fn greet name do
  echo "Hello, $name!"
end
greet world
```
```
Hello, world!
```

Functions return the last expression in their body:

```
fn add a, b do
  a + b
end
r = add 3, 4
echo $r
```
```
7
```

You can define the same function more than once, with different patterns. ish picks the first one that fits:

```
fn fib 0 do 0 end
fn fib 1 do 1 end
fn fib n when n > 1 do
  fib (n - 1) + fib (n - 2)
end
r = fib 10
echo $r
```
```
55
```

When you call `fib 10`, ish asks: does `10` match `0`? No. Does it match `1`? No. Does it match `n when n > 1`? Yes. Bind `n = 10`, run the body.

The `when` is a guard. The pattern has to match *and* the guard has to be true:

```
fn abs n when n < 0 do -n end
fn abs n do n end
r = abs (-5)
echo $r
```
```
5
```

You can also write all the clauses in one block. Each clause is a pattern, an arrow, and a body:

```
fn classify do
  0 -> :zero
  1 -> :one
  _ -> :other
end
classify 0
```
```
:zero
```

`fn` in expression context — on the right side of `=`, as an argument to `spawn` or `map` — produces a value you can hold onto:

```
add = fn a, b do a + b end
add 3, 4
```
```
7
```

Multi-clause dispatch works the same way with `fn do ... end`:

```
f = fn do
  0 -> :zero
  n when n > 0 -> :positive
  _ -> :negative
end
f 0
```
```
:zero
```

All functions are values — including named ones. You can pass `double` to `map` the same way you'd pass a lambda:

```
fn double x do x * 2 end
List.map [1, 2, 3], double
```
```
[2, 4, 6]
```

For simple anonymous functions, use the backslash lambda syntax — it's shorter and clearer:

```
doubled = \x -> x * 2
doubled 5
```
```
10
```

Lambdas can take multiple parameters, or none:

```
sum = \a, b -> a + b
sum 3, 4
```
```
7
```

```
greet = \ -> echo "hello"
greet
```
```
hello
```

Lambdas capture their enclosing scope. This is how you build configurable things:

```
fn make_greeter prefix do
  \name -> echo "$prefix, $name!"
end
hi = make_greeter "Hello"
hi "world"
```
```
Hello, world!
```

The returned lambda remembers `prefix` even after `make_greeter` has returned. Functions that return functions — the fox might call it overthinking, but it's how you avoid repeating yourself.

The fox's way still works too. POSIX functions use `$1`, `$2` for arguments:

```
greet() { echo "hi $1"; }
greet world
```
```
hi world
```

You call them the same way. Name followed by arguments. Doesn't matter which kind.

**One important thing:** user functions evaluate their arguments as expressions. Builtins and external commands don't.

```
x = 42
echo x                       # prints "x" (the literal string)
add x, 1                     # passes 42 (looks up the variable)
```

`echo` is a builtin — it gets the raw string `"x"`. `add` is a user function — it looks up `x` and gets `42`. If you need the value in a builtin argument, use `$x` or `$(expr)`.

And one more thing about arguments: parentheses group sub-expressions. In the `fib` example, `fib (n - 1)` passes the result of `n - 1` to `fib`. Without the parentheses, `fib n - 1` would parse as `(fib n) - 1`. The parser reads left to right — parentheses are how you say "evaluate this part first."

*The fox reached into the raccoon's box without looking and pulled out a flashlight. Clicked it on. It worked.*

*"See?" said the fox. "You don't need to know what kind of thing it is if you already know what to do with it."*

*"And when you don't know?" the AI asked.*

*"Then I'll ask you."*

---

## 7. Two Kinds of Pipes

*The highway split. One lane was paved, the other was dirt. Both went the same direction.*

*"Which one?" the fox asked.*

*"Depends what we're carrying," the AI said.*

---

The fox's pipe moves bytes. One command's output pours into the next command's input:

```
echo "hello world" | tr a-z A-Z | wc -c
```

Three commands. The bytes flow left to right. Nobody in the middle knows what the bytes mean. They pass through.

The AI's pipe moves values. The result of one expression becomes the first argument of the next function:

```
fn double x do x * 2 end
fn inc x do x + 1 end
r = 5 |> double |> inc
echo $r
```
```
11
```

`|` connects stdin to stdout. `|>` connects return values to first arguments. But the roads merge. When a value meets a byte pipe, ish converts automatically:

```
# command output flows into value functions — auto IO.lines
ls |> List.map \f -> String.upcase f | sort
```

`ls` produces bytes. `|>` catches them, splits on newlines, and hands a list to `List.map`. `List.map` returns a list. `|` catches it, joins with newlines, and feeds it to `sort`. No bridge functions needed.

```
# values flow into unix commands — auto IO.unlines
[3, 1, 2] | sort
```
```
1
2
3
```

The default conversion is lines — the universal unix format. When your data isn't lines, you override with an explicit bridge:

```
# JSON: tell |> to use JSON.parse instead of IO.lines
curl -s api.example.com |> JSON.parse |> List.map \x -> x.name

# CSV: same idea
cat data.csv |> CSV.parse |> List.filter \row -> row.age > 30
```

The bridge functions (`JSON.parse`, `CSV.parse`, `CSV.parse_tsv`, `IO.lines` and their encode/unlines counterparts) still exist for when you need them. But for the common case — line-oriented text flowing between unix and ish — the pipes handle it.

*The dirt road turned out to be faster. The fox was annoyed about this.*

*"Pavement has gutters," the AI offered.*

*"Pavement has* speed*," said the fox.*

*They merged back together a mile later, as roads do.*

---

## 8. Control Flow

*Night was coming. The fox started making decisions faster — left, right, left — reading signs by headlight.*

---

The fox knows these:

```
if true; then
  echo "yes"
fi
```
```
yes
```

```
if false; then
  echo "yes"
else
  echo "no"
fi
```
```
no
```

```
for i in a b c; do
  echo $i
done
```
```
a
b
c
```

```
n = 3
while n > 0 do
  echo $n
  n = n - 1
end
```
```
3
2
1
```

```
X=hello
case $X in
hello)
  echo "matched"
  ;;
*)
  echo "default"
  ;;
esac
```
```
matched
```

The AI says the same things with fewer words:

```
if true do
  echo "yes"
end
```

```
if false do
  echo "yes"
else
  echo "no"
end
```

The AI only has one way to close a block: `end`.

The AI's `if` takes expressions directly:

```
x = 5
if x == 5 do
  echo "five"
end
```
```
five
```

The fox's `if` checks exit codes. The AI's `if` checks truthiness. ish knows which one you meant by whether you wrote `then` or `do`.

`break` and `continue` work in both styles.

*"You drive like you're being chased," the AI said.*

*"I drive like I know where I'm going," the fox said, which was not true, but sounded better.*

---

## 9. Error Handling

*The fox took a turn too fast and the car hit a pothole. Something in the engine made a sound that engines should not make.*

*"Pull over," said the AI.*

*"It's fine," said the fox, and the engine made the sound again.*

---

Some things fail. Division by zero. A pattern that doesn't match. A function called with the wrong arguments. In a shell, failure usually means everything stops and a message gets printed to stderr.

`try` catches the failure before it stops anything:

```
r = try do
  1 / 0
rescue
  _ -> :caught
end
echo $r
```
```
:caught
```

The body runs. If it succeeds, `try` returns the result and `rescue` is ignored. If it fails, the error becomes a tuple `{:error, "message"}` and the rescue clauses match against it:

```
r = try do
  {:ok, val} = {:error, "bad things"}
rescue
  {:error, msg} -> echo "handled: $msg"
end
```
```
handled: match error: expected :ok, got :error
```

The rescue clauses use the same pattern matching as `match` and `receive`. You can have multiple:

```
try do
  risky_operation
rescue
  {:error, "not found"} -> echo "missing"
  {:error, reason} -> echo "failed: $reason"
  _ -> echo "something else went wrong"
end
```

If no rescue clause matches, the error passes through as if `try` wasn't there.

*The fox pulled over. The AI got out, opened the hood, and looked at the engine for a long time.*

*"Well?" said the fox.*

*"It's fine," said the AI. "But next time I'm driving."*

---

## 10. Processes

*The AI drove differently. Slower, but watching everything — mirrors, road, mirrors, road. The fox fidgeted.*

*"You could send me ahead to scout," the fox said, half-joking.*

*"Yes," said the AI. "I could."*

---

A process is a separate thing running at the same time as you. It has its own memory, its own mailbox, and the only way to talk to it is to send it a message.

```
pid = spawn fn do
  receive do
    {:ping, sender} -> send sender, :pong
  end
end
```

`spawn` starts a new process running the given function. It returns immediately with a pid — an address you can send things to.

```
send pid, {:ping, self}
```

`send` puts a message in the process's mailbox. `self` is your own address — you include it so the other process knows where to reply.

```
receive do
  :pong -> echo "got pong"
end
```
```
got pong
```

`receive` waits for a message, then pattern-matches it against the clauses.

Here's the whole thing together:

```
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

Two things running at once, talking through mailboxes.

If multiple messages arrive and the first one doesn't match, ish saves it and checks the next. Messages that don't match any clause stay in the mailbox for later:

```
me = self
pid = spawn fn do
  send me, :b
  send me, :a
end
receive do
  :a -> echo "got a first"
end
```
```
got a first
```

`:b` arrived first but didn't match. It's saved. `:a` arrived second and matched.

Sometimes you can't wait forever:

```
r = receive do
  msg -> msg
after 100 ->
  :timeout
end
echo $r
```
```
:timeout
```

`after` takes a time in milliseconds. If no matching message arrives before the timer runs out, the after clause runs instead.

`await` is the simple case — wait for a process to finish and get its result:

```
task = spawn fn do 2 + 3 end
result = await task
echo $result
```
```
5
```

Here's the most important concurrency pattern in ish — a process that carries state:

```
fn counter state do
  receive do
    {:inc, sender} ->
      send sender, state + 1
      counter (state + 1)
    {:get, sender} ->
      send sender, state
      counter state
  end
end

pid = spawn fn do counter 0 end
send pid, {:inc, self}
receive do n -> echo $n end
```
```
1
```

The function calls itself with the new state after each message. No mutable variables, no locks. Every receive ends with a recursive call that becomes the next receive. This is how you build stateful services — a key-value store, a rate limiter, a session manager. The function *is* the loop.

One thing worth knowing: that recursive call at the end of each branch — `counter (state + 1)` and `counter state` — doesn't grow the stack. ish recognizes when a function call is the last thing a function does (the "tail position") and reuses the current frame instead of piling on a new one. This means the counter can handle millions of messages without running out of memory. It's the same trick Erlang uses, and it's why this pattern works for long-running services instead of being a polite way to crash.

*The fox had been quiet for a while.*

*"So you sent me ahead," the fox said. "And I came back with what I found. And you were still here when I got back."*

*"That's messaging," the AI said.*

*"I thought it was just talking," said the fox.*

---

## 11. When Things Crash

*It was raining now. The fox saw a car in the ditch on the side of the road, wheels still spinning. Nobody around.*

*"Should we stop?" the fox asked.*

*"Someone is watching," said the AI. "Someone always is."*

---

Processes crash. A division by zero, a match that fails, a function with no matching clause. When a process crashes, it's gone. Unless someone was linked to it.

`spawn_link` ties two processes together. If one dies abnormally, the other dies too:

```
pid = spawn_link fn do
  1 / 0
end
```

The child divides by zero and crashes. Because it was linked, the parent crashes with it. Linked processes share a fate.

Sometimes you want to *know* about a crash without dying yourself. That's a monitor:

```
pid = spawn fn do
  receive do
    :quit -> :ok
  end
end
ref = monitor pid
send pid, :quit
receive do
  {:DOWN, ref, pid, reason} -> echo "exited: $reason"
end
```
```
exited: :normal
```

`monitor` watches a process. When that process exits, you get a `{:DOWN, ref, pid, reason}` message in your mailbox. You stay alive. You decide what to do.

A supervisor wraps this up: it watches worker processes and restarts them when they crash.

```
sup = supervise :one_for_one do
  worker :greeter fn do
    echo "worker started"
  end
end
await sup
```
```
worker started
```

Three strategies:

| Strategy | What happens when a worker crashes |
|----------|-------------------------------------|
| `:one_for_one` | Restart that worker |
| `:one_for_all` | Stop and restart every worker |
| `:rest_for_one` | Restart that worker and everything started after it |

Workers that exit normally aren't restarted. The supervisor exits when all its workers are done.

If a worker keeps crashing — more than three times in five seconds — the supervisor gives up and shuts down. Some things can't be fixed by trying again.

```
sup = supervise :one_for_one do
  worker :a fn do echo "a started" end
  worker :b fn do echo "b started" end
end
await sup
echo "supervisor done"
```

*The rain got heavier. The fox looked at the car in the ditch again as they passed it.*

*"Who's watching?" the fox asked.*

*"Whoever started it," said the AI.*

---

## 12. The Toolbox

*They stopped at a rest area. The AI opened the trunk and laid everything out on the picnic table — sorted, of course.*

---

ish has built-in functions for working with lists, strings, and maps. They work with `|>` and take values, not strings.

**Lists:**

```
List.hd [1, 2, 3]             # 1
List.tl [1, 2, 3]             # [2, 3]
List.length [10, 20, 30]      # 3
List.at [10, 20, 30], 1       # 20 (zero-indexed)
List.append [1, 2], 3          # [1, 2, 3]
List.concat [1, 2], [3, 4]    # [1, 2, 3, 4]
List.range 0, 5               # [0, 1, 2, 3, 4]
List.sort [3, 1, 2]           # [1, 2, 3]
List.reverse [1, 2, 3]        # [3, 2, 1]
List.with_index ["a", "b"]    # [{0, "a"}, {1, "b"}]
```

Transform with lambdas:

```
r = List.map [1, 2, 3], \x -> x * 2
echo $r
```
```
[2, 4, 6]
```

```
r = List.filter [1, 2, 3, 4, 5], \x -> x >= 4
echo $r
```
```
[4, 5]
```

```
r = List.reduce [1, 2, 3, 4], 0, \acc, x -> acc + x
echo $r
```
```
10
```

There are also `List.each` (like `List.map` but for side effects), `List.any`, `List.all`, and `List.find`:

```
List.each [1, 2, 3], \x -> echo $x    # prints 1, 2, 3 on separate lines
List.any [0, 0, 1], \x -> x > 0       # :true
List.all [1, 2, 3], \x -> x > 0       # :true
List.find [1, 2, 3, 4], \x -> x > 2   # 3
```

These chain with `|>`:

```
r = List.range 1, 11 |> List.filter \x -> x >= 6 |> List.length
echo $r
```
```
5
```

**Strings:**

```
String.split "a:b:c", ":"         # ["a", "b", "c"]
String.join ["a", "b", "c"], "-"  # "a-b-c"
String.trim "  hello  "           # "hello"
String.upcase "hello"             # "HELLO"
String.downcase "HELLO"           # "hello"
String.replace "hello", "l", "r"  # "herlo"
String.replace_all "hello", "l", "r"  # "herro"
String.starts_with "hello", "hel" # :true
String.ends_with "hello", "llo"   # :true
String.contains "hello", "ell"    # :true
String.slice "hello", 1, 3        # "ell"
String.index_of "hello", "ll"     # 2
```

**Maps:**

```
Map.put %{a: 1}, "b", 2        # %{a: 1, b: 2}
Map.delete %{a: 1, b: 2}, "a"  # %{b: 2}
Map.merge %{a: 1}, %{b: 2}     # %{a: 1, b: 2}
Map.keys %{x: 1, y: 2}         # ["x", "y"]
Map.values %{x: 1, y: 2}       # [1, 2]
Map.has_key %{x: 1}, "x"       # :true
Map.get %{x: 1, y: 2}, "x"    # 1
Map.pairs %{a: 1, b: 2}        # [{"a", 1}, {"b", 2}]
```

**Format conversion (override auto-coercion for structured data):**

The pipes auto-convert using lines by default. These functions handle other formats:

```
JSON.parse str              # JSON string -> value (map, list, etc.)
JSON.encode value           # value -> JSON string
CSV.parse str               # CSV with headers -> list of maps
CSV.encode list             # list of maps -> CSV string
CSV.parse_tsv str           # TSV with headers -> list of maps
CSV.encode_tsv list         # list of maps -> TSV string
IO.lines str                # string -> list of lines (what auto-coercion does)
IO.unlines list             # list of strings -> newline-joined string
```

**Utilities:**

```
Process.sleep 1000           # pause for 1000 milliseconds
```

Module-qualified names avoid shadowing Unix commands. `List.sort` instead of `sort`, `List.find` instead of `find`, `Process.sleep` instead of `sleep`.

**Defining your own modules:**

```
defmodule Math do
  def abs do
    n when n >= 0 -> n
    n -> 0 - n
  end

  def clamp val, lo, hi do
    Math.max lo, (Math.min val, hi)
  end

  def max a, b when a >= b do a end
  def max _, b do b end
  def min a, b when a <= b do a end
  def min _, b do b end
end

Math.abs (0 - 5)            # 5
Math.clamp 10, 0, 5         # 5
```

`def` works like `fn` inside a module — guards, pattern matching, multi-clause dispatch all work. Functions inside a module can call each other by name. Prefix a name with `_` to make it private.

**Importing with `use`:**

```
use List
map [1, 2, 3], \x -> x * 2    # works without the List. prefix
```

**Commas inside tuples and lists:**

At statement level, commas separate function arguments:

```
r = List.map [1, 2, 3], \x -> x * 2    # two args, no ambiguity
```

Inside `{...}` and `[...]`, commas separate elements. Single-argument calls work by juxtaposition:

```
{List.hd [1, 2], List.hd [3, 4]}    # {1, 3}
```

Multi-argument calls need parentheses to avoid ambiguity with element commas:

```
{(List.map [1, 2, 3], \x -> x * 2), 99}        # {[2, 4, 6], 99}
{List.map ([1, 2, 3], \x -> x * 2), 99}         # same thing
```

Both forms work — parentheses around the whole call, or parentheses around the arguments.

*The fox picked up a string function and turned it over.*

*"I would have piped this through `tr`," the fox said.*

*"You still can," the AI said.*

---

## 13. Putting It Together

*They arrived at a motel with a neon sign half burned out. The wifi password was taped to the wall. The fox plugged in. The AI looked at the screen.*

*"Let's check on things," the AI said.*

---

Here's a real program. It reads a list of services from a config file, checks them all concurrently, and produces a report. The fox handles the system. The AI handles the data.

The config file, `services.conf` (if this were CSV, `CSV.parse` would handle it in one call — but space-separated configs are everywhere, so we parse it ourselves):

```
web https://example.com
api https://httpbin.org/status/200
broken https://httpbin.org/status/500
timeout https://httpbin.org/delay/10
```

The script, `checkup.ish` (run it with `ish checkup.ish`):

```
# --- read the config file (the fox's part) ---

fn read_services file do
  lines = $(cat $file)
  lines
    |> String.split "\n"
    |> List.filter \line -> List.length line >= 1
    |> List.map fn do line ->
      parts = String.split line, " "
      [name | rest] = parts
      url = String.join rest, " "
      %{name: name, url: url}
    end
end

services = read_services "services.conf"

# --- check each service concurrently (both of them) ---

fn check service, reply_to do
  name = service.name
  url = service.url
  result = try do
    code = $(curl -s -o /dev/null -w "%{http_code}" --max-time 3 $url 2>/dev/null)
    {:ok, name, code}
  rescue
    _ -> {:error, name, "request failed"}
  end
  send reply_to, result
end

me = self
List.map services, \svc -> spawn fn do check svc, me end

# --- collect results ---

fn collect n do
  if n == 0 do
    []
  else
    receive do
      result -> [result | collect (n - 1)]
    after 5000 ->
      [{:error, "unknown", "timed out"} | collect (n - 1)]
    end
  end
end

results = collect (List.length services)

# --- report (the fox's part again) ---

ok = List.filter results, \r -> match r do {:ok, _, _} -> :true; _ -> :false end
failed = List.filter results, \r -> match r do {:ok, _, _} -> :false; _ -> :true end

echo "=== Service Report ==="
echo ""

List.each ok, fn do
  {:ok, name, code} -> printf "  %-12s %s\n" $name $code
end

List.each failed, fn do
  {:error, name, reason} -> printf "  %-12s FAILED (%s)\n" $name $reason
end

echo ""
printf "%d ok, %d failed\n" (List.length ok) (List.length failed)
```

`cat` reads the file, `|>` transforms it. `curl` checks the URLs, `try/rescue` catches failures. `spawn` fires them all at once. `receive` with `after 5000` collects results without hanging. `List.filter` separates the good from the bad. `printf` prints the report.

The fox's tools and the AI's tools, in the same script, doing what each one does best.

*The fox read the output on the screen.*

*"I could have done this with a bash script," the fox said.*

*"You could have," said the AI. "How long would it have taken?"*

*The fox didn't answer, which was an answer.*

---

## 14. Making It Yours

*Morning. The fox was already up, rearranging the driver's seat. Moving mirrors. Adjusting things that didn't need adjusting.*

---

`~/.ishrc` runs every time ish starts. Put your functions and aliases there:

```
# ~/.ishrc
alias ll='ls -la'
alias gs='git status'

fn greet do
  echo "welcome back"
end
greet
```

Aliases expand when you type the name as a command:

```
alias ll='ls -la'
ll
```

`unalias ll` removes it. `unalias -a` removes all of them.

The prompt is controlled by `PS1`:

```
PS1='\u@\h:\w\$ '
```

| Escape | Meaning |
|--------|---------|
| `\u` | Username |
| `\h` | Hostname (short) |
| `\w` | Working directory (with `~`) |
| `\W` | Basename of working directory |
| `\$` | `#` if root, `$` otherwise |
| `\t` | Time (24-hour) |
| `\n` | Newline |

`$var` and `#{expr}` are expanded in the prompt too.

Tab completes commands, paths, and variables. Type a few letters, press Tab. If there's one match, it fills in. If there are several, it shows them.

*The AI watched the fox adjust the mirror for the third time.*

*"You're nesting," the AI said.*

*"I'm making it mine," the fox said.*

---

## 15.

*They'd been driving for days or hours — neither of them was sure. The landscape had changed so many times. Cities, fields, mountains, rain. The car smelled like gas station coffee and the raccoon's cardboard box.*

*The fox pulled over at the top of a hill. They could see a long way in every direction. The AI looked at the fox. The fox looked at the road.*

*"I don't know where we're going," the fox said.*

*"I know," said the AI.*

*"You don't mind?"*

*"The car works," the AI said. "We work."*

*The fox started the engine.*

---

The Unix kernel already provides what Erlang discovered independently: process isolation, message passing through pipes and signals, and supervision through init. Go makes supervision easier with goroutines, and ish makes the analogy concrete and programmable.

The road doesn't end here.
