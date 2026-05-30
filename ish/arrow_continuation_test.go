package ish

import (
	"testing"

	"ish/core"
)

// A clause arrow `->` always expects a body, so a newline after it continues the
// form: the body (including a multi-line match/case) may start on the next line.
// This keeps clauses clean without trailing parentheses or forcing the block
// form. A genuinely missing body is still an error.
func TestArrowBodyOnNextLine(t *testing.T) {
	cases := map[string]core.Datum{
		// fn arrow body on the next line
		"use std/enum\nreduce '(1 2 3) 0 fn x acc ->\n  x + acc": core.Int(6),
		// match clause body on the next line
		"match 5 do\n  5 ->\n    :five\n  _ -> :other\nend": core.Atom("five"),
		// fn whose arrow body is a multi-line match (the config.ish shape)
		"use std/enum\nreduce '(\"a = 1\" \"b = 2\") dict fn line acc ->\n" +
			"  match (to-list (str-split line \" = \")) do\n" +
			"    (k v . _) -> dict-put acc k v\n" +
			"    _ -> acc\n" +
			"  end": core.Dict{
			core.DictEntry{Key: core.String("a"), Value: core.String("1")},
			core.DictEntry{Key: core.String("b"), Value: core.String("2")},
		},
	}
	for src, want := range cases {
		v, err := NewRuntime().EvalSource("arrow", src)
		if err != nil {
			t.Errorf("%q ERR: %v", src, err)
			continue
		}
		if d, _ := v.(core.Datum); !core.DatumEqual(d, want) {
			t.Errorf("%q = %#v, want %#v", src, d, want)
		}
	}
	if _, err := NewRuntime().EvalSource("arrow", "f = fn x ->\n"); err == nil {
		t.Error("expected an error for an arrow with no body")
	}
}
