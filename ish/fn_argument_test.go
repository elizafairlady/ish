package ish

import (
	"testing"

	"ish/core"
)

// A clause binder (fn/macro) in argument position consumes the rest of the
// application as its clause, so it is the final argument and needs no
// surrounding parentheses. Parentheses are only required when another argument
// must follow the function. This keeps higher-order calls free of trailing
// `)` noise.
func TestFnArgumentNeedsNoParens(t *testing.T) {
	cases := map[string]core.Datum{
		// do-thunk as the last argument
		"defn run f do f end\nrun fn do 42 end": core.Int(42),
		// trailing arrow fn into a collection function
		"use std/enum\nreduce '(1 2 3) 0 fn x acc -> x + acc": core.Int(6),
		"use std/enum\nto-list (map '(1 2) fn x -> x * 10)":   list(core.Int(10), core.Int(20)),
		// multi-line block-bodied fn as the last argument
		"use std/enum\nreduce '(1 2 3) 0 fn x acc do\n  x + acc\nend": core.Int(6),
		// parentheses still work (and are needed mid-pipe / before more args)
		"use std/enum\n'(1 2 3) |> filter (fn x -> x > 1) |> count": core.Int(2),
	}
	for src, want := range cases {
		v, err := NewRuntime().EvalSource("fnarg", src)
		if err != nil {
			t.Errorf("%q ERR: %v", src, err)
			continue
		}
		if d, _ := v.(core.Datum); !core.DatumEqual(d, want) {
			t.Errorf("%q = %#v, want %#v", src, d, want)
		}
	}
}
