package ish

import (
	"testing"

	"ish/core"
)

// A guard that raises a runtime error is a clause failure, not an abort. The
// match case is covered elsewhere; this pins the newly-unified behaviour for fn
// and syntax-parse (the latter used to abort the whole expansion on a guard
// error instead of trying the next clause).
func TestGuardErrorSkipsClause_FnAndSyntaxParse(t *testing.T) {
	cases := map[string]core.Datum{
		// fn: first clause's guard errors (first of a non-sequence) -> skip
		"f = fn do\n  x when (first x) -> :a\n  x -> :b\nend\nf 5": core.Atom("b"),
		// syntax-parse: guard error inside a macro skips to the next clause
		"defmacro pick stx -> syntax-parse stx do\n  (_ x) when (first x) -> %`:a\n  (_ x) -> %`:b\nend\npick 5": core.Atom("b"),
	}
	for src, want := range cases {
		v, err := NewRuntime().EvalSource("guard", src)
		if err != nil {
			t.Errorf("%q ERR: %v", src, err)
			continue
		}
		if d, _ := v.(core.Datum); !core.DatumEqual(d, want) {
			t.Errorf("%q = %#v, want %#v", src, d, want)
		}
	}
}
