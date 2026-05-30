package ish

import (
	"testing"

	"ish/core"
)

// evalEnum runs source that pulls in the std/enum package and returns the datum.
func evalEnum(t *testing.T, src string) core.Datum {
	t.Helper()
	v, err := NewRuntime().EvalSource("enum", src)
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	d, _ := v.(core.Datum)
	return d
}

// std/enum is not auto-loaded; it is addressed by its full path std/enum and
// resolved from the embedded std tree on first use. `use std/enum` brings its
// exports in unqualified.
func TestEnum_UseUnqualified(t *testing.T) {
	cases := map[string]core.Datum{
		"use std/enum\nreduce '(1 2 3 4) 0 (fn x acc -> x + acc)": core.Int(10),
		"use std/enum\nsum '(1 2 3 4 5)":                          core.Int(15),
		"use std/enum\nproduct '(1 2 3 4)":                        core.Int(24),
		"use std/enum\ncount '(1 2 3 4 5)":                        core.Int(5),
		"use std/enum\nat '(10 20 30) 2":                          core.Int(30),
		"use std/enum\nmember? '(1 2 3) 2":                        core.Atom("true"),
		"use std/enum\nmember? '(1 2 3) 9":                        core.Atom("false"),
		"use std/enum\nempty? :nil":                               core.Atom("true"),
		"use std/enum\nempty? '(1)":                               core.Atom("false"),
		"use std/enum\njoin '(1 2 3) \"-\"":                       core.String("1-2-3"),
		"use std/enum\nfind '(1 2 3 4) (fn x -> x > 2)":           core.Int(3),
		"use std/enum\nany? '(1 2 3) (fn x -> x > 2)":             core.Atom("true"),
		"use std/enum\nall? '(1 2 3) (fn x -> x > 0)":             core.Atom("true"),
		"use std/enum\nall? '(1 2 3) (fn x -> x > 1)":             core.Atom("false"),
		"use std/enum\nsum (take '(1 2 3 4 5) 3)":                 core.Int(6),
		"use std/enum\nsum (drop '(1 2 3 4 5) 3)":                 core.Int(9),
		// polymorphic over the kernel's to-list: vectors enumerate too
		"use std/enum\nsum (vector 1 2 3 4)": core.Int(10),
	}
	for src, want := range cases {
		if got := evalEnum(t, src); !core.DatumEqual(got, want) {
			t.Errorf("%q = %#v, want %#v", src, got, want)
		}
	}
}

// Collection-first, Elixir-style, so a data pipeline threads left-to-right with
// the thread-first pipe.
func TestEnum_PipelinesThreadFirst(t *testing.T) {
	cases := map[string]core.Datum{
		"use std/enum\n'(1 2 3 4 5) |> filter (fn x -> (x % 2) == 1) |> sum":                   core.Int(9),
		"use std/enum\n'(1 2 3) |> map (fn x -> x * x) |> sum":                                 core.Int(14),
		"use std/enum\n'(1 2 3 4 5) |> map (fn x -> x + 1) |> filter (fn x -> x > 3) |> count": core.Int(3),
	}
	for src, want := range cases {
		if got := evalEnum(t, src); !core.DatumEqual(got, want) {
			t.Errorf("%q = %#v, want %#v", src, got, want)
		}
	}
}

// `import std/enum` enables qualified access (enum.fn), and `as` rebinds the
// alias.
func TestEnum_ImportQualified(t *testing.T) {
	cases := map[string]core.Datum{
		"import std/enum\nenum.sum '(1 2 3)":       core.Int(6),
		"import std/enum\nenum.count '(1 2 3 4)":   core.Int(4),
		"import std/enum as E\nE.product '(2 3 4)": core.Int(24),
	}
	for src, want := range cases {
		if got := evalEnum(t, src); !core.DatumEqual(got, want) {
			t.Errorf("%q = %#v, want %#v", src, got, want)
		}
	}
}

// The thread-first pipe composes with qualified access on the right: a dotted
// access `enum.map f` reaches the thread macro as the raw reader parts
// `enum . map f`, which it splits with dotted-parts and rebuilds with the left
// operand spliced in. Without that handling the bare `.` is misread as a cons
// tail and the call is mangled.
func TestEnum_QualifiedAccessPipelines(t *testing.T) {
	cases := map[string]core.Datum{
		"import std/enum\n'(1 2 3) |> enum.map (fn x -> x + 1) |> enum.sum":               core.Int(9),
		"import std/enum\n'(1 2 3 4 5) |> enum.filter (fn x -> (x % 2) == 1) |> enum.sum": core.Int(9),
		"import std/enum\n'(1 2 3) |> enum.sum":                                           core.Int(6),
		"import std/enum as E\n'(2 3 4) |> E.product":                                     core.Int(24),
		"import std/enum\nto-list ('(1 2 3) |> enum.map (fn x -> x * 10))":                list(core.Int(10), core.Int(20), core.Int(30)),
	}
	for src, want := range cases {
		if got := evalEnum(t, src); !core.DatumEqual(got, want) {
			t.Errorf("%q = %#v, want %#v", src, got, want)
		}
	}
}

// Thread-last is the correct choice when the piped value is the SUBJECT that
// belongs in the last argument position. Several enum functions take the
// collection first and the subject second (member? coll x, at coll i), so
// threading the element/index LAST reads naturally and is genuinely distinct
// from thread-first. These also exercise the qualified-access path through
// thread-last (`|>>` + enum.fn), where the left operand is spliced after the
// resolved member's existing arguments.
func TestEnum_ThreadLastSubjectLast(t *testing.T) {
	cases := map[string]core.Datum{
		// thread the index as the last arg: (at '(10 20 30) 1)
		"import std/enum\n1 |>> enum.at '(10 20 30)": core.Int(20),
		// thread the searched element as the last arg: (member? '(1 2 3) 2)
		"import std/enum\n2 |>> enum.member? '(1 2 3)": core.Atom("true"),
		"import std/enum\n9 |>> enum.member? '(1 2 3)": core.Atom("false"),
		// unqualified thread-last into a collection-last kernel function:
		// (cons 1 '(2 3)) prepends, threading the list last.
		"to-list ('(2 3) |>> cons 1)": list(core.Int(1), core.Int(2), core.Int(3)),
		// argument order matters, so thread-last differs from thread-first:
		// (sub3 9 1 5) = 3, whereas thread-first would compute (sub3 5 9 1).
		"defn sub3 a b c do a - b - c end\n5 |>> sub3 9 1": core.Int(3),
	}
	for src, want := range cases {
		if got := evalEnum(t, src); !core.DatumEqual(got, want) {
			t.Errorf("%q = %#v, want %#v", src, got, want)
		}
	}
}

// Resolution is std-first by full path; a bare name or an unknown std package
// is an error (no invented shorthands).
func TestEnum_PathErrors(t *testing.T) {
	for _, src := range []string{"use enum\n1", "use std/nonesuch\n1", "import enum\n1"} {
		if _, err := NewRuntime().EvalSource("enum", src); err == nil {
			t.Errorf("%q: expected error, got none", src)
		}
	}
}
