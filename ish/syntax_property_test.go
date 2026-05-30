package ish

import (
	"testing"

	"ish/core"
)

// syntax-property (after Racket) reads and writes per-syntax metadata. The
// two-argument form reads a key (atom or string), the three-argument form
// returns a copy carrying the property. This makes the reader's per-token
// metadata observable instead of write-only.
func TestSyntaxProperty(t *testing.T) {
	cases := map[string]core.Datum{
		// the reader records the raw source token under :token-raw
		"defmacro raw stx -> syntax-parse stx do\n  (_ x) -> datum->syntax stx (syntax-property x :token-raw)\nend\nraw hello": core.String("hello"),
		// set then get a custom property
		"defmacro tagit stx -> syntax-parse stx do\n  (_ x) -> datum->syntax stx (syntax-property (syntax-property x :mark :yes) :mark)\nend\ntagit foo": core.Atom("yes"),
		// an absent property reads as :nil
		"defmacro m stx -> syntax-parse stx do\n  (_ x) -> datum->syntax stx (syntax-property x :nope)\nend\nm foo": core.Nil{},
	}
	for src, want := range cases {
		v, err := NewRuntime().EvalSource("sp", src)
		if err != nil {
			t.Errorf("%q ERR: %v", src, err)
			continue
		}
		if d, _ := v.(core.Datum); !core.DatumEqual(d, want) {
			t.Errorf("%q = %#v, want %#v", src, d, want)
		}
	}
}
