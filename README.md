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
