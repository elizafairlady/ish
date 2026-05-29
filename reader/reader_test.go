package reader

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"ish/core"
)

func readOne(t *testing.T, src string) *core.Syntax {
	t.Helper()
	forms, err := ReadAll("test", src)
	if err != nil {
		t.Fatalf("read %q: %v", src, err)
	}
	if len(forms) != 1 {
		t.Fatalf("read %q: expected 1 form, got %d", src, len(forms))
	}
	return forms[0]
}

func mustErr(t *testing.T, src string) error {
	t.Helper()
	_, err := ReadAll("test", src)
	if err == nil {
		t.Fatalf("read %q: expected error", src)
	}
	return err
}

func wantRead(t *testing.T, src, want string) {
	t.Helper()
	got := render(readOne(t, src))
	if got != want {
		t.Fatalf("ReadAll(%q) = %s, want %s", src, got, want)
	}
}

func wantReadAll(t *testing.T, src string, want ...string) {
	t.Helper()
	forms, err := ReadAll("test", src)
	if err != nil {
		t.Fatalf("ReadAll(%q) error = %v", src, err)
	}
	if len(forms) != len(want) {
		t.Fatalf("ReadAll(%q) produced %d forms, want %d", src, len(forms), len(want))
	}
	for i, form := range forms {
		if got := render(form); got != want[i] {
			t.Fatalf("ReadAll(%q)[%d] = %s, want %s", src, i, got, want[i])
		}
	}
}

func render(stx *core.Syntax) string {
	if stx == nil {
		return "<nil>"
	}
	return renderDatum(stx.Node)
}

func renderDatum(d any) string {
	switch v := d.(type) {
	case core.Word:
		return string(v)
	case core.Atom:
		return ":" + string(v)
	case core.Meta:
		return "%:" + string(v)
	case core.Int:
		return strconv.FormatInt(int64(v), 10)
	case core.Float:
		return strconv.FormatFloat(float64(v), 'g', -1, 64)
	case core.String:
		return strconv.Quote(string(v))
	case core.Bytes:
		return "b" + strconv.Quote(string([]byte(v)))
	case core.Nil:
		return "()"
	case core.SyntaxPair:
		return renderList(&core.Syntax{Node: v})
	case core.SyntaxVector:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = render(e)
		}
		return "[" + strings.Join(parts, " ") + "]"
	case core.SyntaxTuple:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = render(e)
		}
		return "{" + strings.Join(parts, " ") + "}"
	case core.SyntaxDict:
		parts := make([]string, 0, len(v)*2)
		for _, e := range v {
			parts = append(parts, render(e.Key), render(e.Value))
		}
		return "%{" + strings.Join(parts, " ") + "}"
	default:
		return "<?>"
	}
}

func renderList(stx *core.Syntax) string {
	var parts []string
	cur := stx
	for {
		switch n := cur.Node.(type) {
		case core.Nil:
			return "(" + strings.Join(parts, " ") + ")"
		case core.SyntaxPair:
			parts = append(parts, render(n.Head))
			cur = n.Tail
		default:
			return "(" + strings.Join(parts, " ") + " . " + render(cur) + ")"
		}
	}
}

func TestScalarLiterals(t *testing.T) {
	intCases := map[string]int64{
		`0`: 0, `42`: 42, `-7`: -7, `+9`: 9,
		`9223372036854775807`:  9223372036854775807,
		`-9223372036854775808`: -9223372036854775808,
	}
	for src, want := range intCases {
		if got := readOne(t, src).Node; got != core.Int(want) {
			t.Errorf("int %q: got %#v want %d", src, got, want)
		}
	}

	floatCases := map[string]float64{
		`1.5`: 1.5, `-1.5`: -1.5, `0.0`: 0, `1e2`: 100, `1.5e-3`: 0.0015,
	}
	for src, want := range floatCases {
		if got := readOne(t, src).Node; got != core.Float(want) {
			t.Errorf("float %q: got %#v want %g", src, got, want)
		}
	}

	atomCases := map[string]string{`:ok`: "ok", `:true`: "true", `:foo-bar`: "foo-bar", `:+`: "+", `:*`: "*", `:==`: "=="}
	for src, want := range atomCases {
		if got := readOne(t, src).Node; got != core.Atom(want) {
			t.Errorf("atom %q: got %#v want %q", src, got, want)
		}
	}
	// `:nil` is the canonical spelling of the empty/nil value, not an atom.
	if got := readOne(t, ":nil").Node; got != (core.Nil{}) {
		t.Errorf("`:nil`: got %#v want core.Nil{}", got)
	}
}

func TestStringAndBytesLiterals(t *testing.T) {
	stringCases := map[string]string{
		`""`: "", `"hello"`: "hello", `"a\nb"`: "a\nb", `"a\tb"`: "a\tb",
		`"a\\b"`: "a\\b", `"a\"b"`: `a"b`, `"\0"`: "\x00", `"αβγ"`: "αβγ",
	}
	for src, want := range stringCases {
		if got := readOne(t, src).Node; got != core.String(want) {
			t.Errorf("string %q: got %#v want %q", src, got, want)
		}
	}

	byteCases := map[string][]byte{
		`b""`: {}, `b"hello"`: []byte("hello"), `b"\x01\xFF"`: {0x01, 0xFF}, `b"αβγ"`: []byte("αβγ"),
	}
	for src, want := range byteCases {
		b, ok := readOne(t, src).Node.(core.Bytes)
		if !ok {
			t.Errorf("bytes %q: got non-bytes %#v", src, b)
			continue
		}
		if !reflect.DeepEqual([]byte(b), want) {
			t.Errorf("bytes %q: got %v want %v", src, []byte(b), want)
		}
	}
}

func TestWordsAndUnicodeIdentifiers(t *testing.T) {
	for _, src := range []string{`hello`, `x`, `_private`, `map->list`, `foo?`, `set!`, `αβγ`, `λ`, `日本語`, `naïve`} {
		if got := readOne(t, src).Node; got != core.Word(src) {
			t.Errorf("word %q: got %#v", src, got)
		}
	}
}

func TestReaderProtocolPositiveGoldens(t *testing.T) {
	cases := map[string]string{
		`foo bar`:          `(%-expr foo bar)`,
		`foo (bar baz)`:    `(%-expr foo (%-group (%-expr bar baz)))`,
		`(foo bar)`:        `(%-group (%-expr foo bar))`,
		`()`:               `(%-group)`,
		`'(foo bar)`:       `(quote (%-group (%-expr foo bar)))`,
		"`(a ,b ,@c)":      `(quasiquote (%-group (%-expr a (unquote b) (unquote-splicing c))))`,
		`%'(foo bar)`:      `(syntax (%-group (%-expr foo bar)))`,
		`&x`:               `(%-expression x)`,
		`^x`:               `(pin x)`,
		`%:precedence`:     `%:precedence`,
		`&(foo bar)`:       `(%-expression (%-group (%-expr foo bar)))`,
		`enum.reduce xs`:   `(%-expr enum . reduce xs)`,
		`pkg.utility.func`: `(%-expr pkg . utility . func)`,
		`grep -c *.go`:     `(%-expr grep -c *.go)`,
		`FOO=bar cmd`:      `(%-expr FOO = bar cmd)`,
		`cmd > out`:        `(%-expr cmd > out)`,
		`cmd 2>&1`:         `(%-expr cmd 2 >& 1)`,
		`cat <<EOF`:        `(%-expr cat << EOF)`,
		`cmd | next`:       `(%-expr cmd | next)`,
		`x |> f`:           `(%-expr x |> f)`,
		`x |>> f`:          `(%-expr x |>> f)`,
		`a :: b`:           `(%-expr a :: b)`,
		`a => b`:           `(%-expr a => b)`,
		`@target value`:    `(%-expr @ target value)`,
		`~path`:            `(%-expr ~ path)`,
		`$name`:            `(%-expr $ name)`,
		`x + y * z`:        `(%-expr x + y * z)`,
		`x+y`:              `(%-expr x + y)`,
		`x%y`:              `(%-expr x % y)`,
		`x=1`:              `(%-expr x = 1)`,
		`x * y + z`:        `(%-expr x * y + z)`,
		`x == y`:           `(%-expr x == y)`,
		`x != y`:           `(%-expr x != y)`,
		`x && y`:           `(%-expr x && y)`,
		`f x + y`:          `(%-expr f x + y)`,
		`build: [main.o]`:  `(%-expr build : [main . o])`,
		`[1 2 3]`:          `[1 2 3]`,
		`{:ok 42}`:         `{:ok 42}`,
		`%{:a 1 :b 2}`:     `%{:a 1 :b 2}`,
	}
	for src, want := range cases {
		wantRead(t, src, want)
	}
}

func TestReaderTokenMetadata(t *testing.T) {
	stx := readOne(t, `build: deps`)
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) < 4 {
		t.Fatalf("shape = %s", render(stx))
	}
	colon := elems[2]
	if kind, ok := colon.Properties.Get(core.PropTokenKind); !ok || kind != core.String("operator") {
		t.Fatalf("colon kind = %#v/%v, want operator", kind, ok)
	}
	if raw, ok := colon.Properties.Get(core.PropTokenRaw); !ok || raw != core.String(":") {
		t.Fatalf("colon raw = %#v/%v, want :", raw, ok)
	}
	if adjacent, ok := colon.Properties.Get(core.PropTokenAdjacentPrev); !ok || adjacent != core.Atom("true") {
		t.Fatalf("colon adjacency = %#v/%v, want true", adjacent, ok)
	}
	deps := elems[3]
	if leading, ok := deps.Properties.Get(core.PropTokenLeadingSpace); !ok || leading != core.Atom("true") {
		t.Fatalf("deps leading space = %#v/%v, want true", leading, ok)
	}
}

func TestReadProgramWrapsPackageBeginWithMetadata(t *testing.T) {
	stx, err := ReadProgram("test.ish", "x=1\nx")
	if err != nil {
		t.Fatalf("ReadProgram failed: %v", err)
	}
	if got, want := render(stx), `(%-package-begin %:file "test.ish" (%-expr x = 1) x)`; got != want {
		t.Fatalf("ReadProgram = %s, want %s", got, want)
	}
	if shape, ok := stx.Properties.Get(core.PropReaderShape); !ok || shape != core.String("package-begin") {
		t.Fatalf("package-begin reader-shape = %#v/%v", shape, ok)
	}
}

func TestReaderMultipleFormsAndTerminators(t *testing.T) {
	wantReadAll(t, "foo bar\n1; x + y # trailing\n:ok",
		`(%-expr foo bar)`,
		`1`,
		`(%-expr x + y)`,
		`:ok`,
	)
}

func TestReaderPreservesDoEndBlock(t *testing.T) {
	wantRead(t, "do\n  x=1\n  x\nend", `(%-expr do (%-body (%-expr x = 1) x))`)
}

func TestReaderDoEndBlockInsideGroup(t *testing.T) {
	// A do-block nested in a group must close at `end` rather than slurping it
	// as an application argument, so grouped and quasisyntax-templated blocks
	// read identically to top-level ones.
	wantRead(t, "(do x end)", `(%-group (%-expr do (%-body x)))`)
	wantRead(t, "f (do a ; b end)", `(%-expr f (%-group (%-expr do (%-body a b))))`)
}

func TestReaderElseRemainsLiteralInsideGroup(t *testing.T) {
	// `else` closes a do-block body, but inside a group it is an ordinary
	// identifier (e.g. a syntax-case literal), not a terminator.
	wantRead(t, "(if c t else e)", `(%-group (%-expr if c t else e))`)
}

func TestReaderRejectsUnterminatedDoEnd(t *testing.T) {
	err := mustErr(t, "do\n  x=1")
	if !strings.Contains(err.Error(), "unterminated do block") {
		t.Fatalf("error = %v, want unterminated do block", err)
	}
}

func TestReaderPreservesNestedSpans(t *testing.T) {
	stx := readOne(t, `(foo bar)`)
	elems, ok := core.SyntaxListElems(stx)
	if !ok || len(elems) != 2 || elems[0].Node != core.Word("%-group") {
		t.Fatalf("group shape: %s", render(stx))
	}
	innerElems, ok := core.SyntaxListElems(elems[1])
	if !ok || len(innerElems) != 3 {
		t.Fatalf("inner shape: %s", render(elems[1]))
	}
	if innerElems[1].Span.Start.Col != 2 {
		t.Errorf("foo span col = %d, want 2", innerElems[1].Span.Start.Col)
	}
	if innerElems[2].Span.Start.Col != 6 {
		t.Errorf("bar span col = %d, want 6", innerElems[2].Span.Start.Col)
	}
	if shape, ok := stx.Properties.Get(core.PropReaderShape); !ok || shape != core.String("()") {
		t.Fatalf("group reader-shape = %#v/%v, want ()", shape, ok)
	}
}

func TestReaderShapeProperties(t *testing.T) {
	cases := map[string]string{`[1]`: "[]", `{:ok}`: "{}", `%{:a 1}`: "%{}"}
	for src, want := range cases {
		stx := readOne(t, src)
		if shape, ok := stx.Properties.Get(core.PropReaderShape); !ok || shape != core.String(want) {
			t.Fatalf("%s reader-shape = %#v/%v, want %s", src, shape, ok, want)
		}
	}
}

func TestReaderMismatchGoldens(t *testing.T) {
	group := readOne(t, `(foo bar)`)
	if got := render(group); got == `(foo bar)` {
		t.Fatalf("group collapsed to list/application shape: %s", got)
	}

	dotted := readOne(t, `enum.reduce`)
	if got := render(dotted); got != `(%-expr enum . reduce)` {
		t.Fatalf("dotted access should remain token stream, got %s", got)
	}
	trailingDot := readOne(t, `foo.`)
	if got := render(trailingDot); got != `(%-expr foo .)` {
		t.Fatalf("trailing dot should remain token stream, got %s", got)
	}
	glob := readOne(t, `*.go`)
	if got := render(glob); got != `*.go` {
		t.Fatalf("glob-like dotted word became access: %s", got)
	}

	escaped := readOne(t, `&x`)
	if escaped.Node == core.Word("x") {
		t.Fatal("value escape marker was discarded")
	}
}

func TestReaderFailureGoldens(t *testing.T) {
	for _, src := range []string{
		`"oops`,
		`b"oops`,
		`(foo bar`,
		`[1 2`,
		`{:ok`,
		`%{:a 1`,
		`%{:a}`,
		`"a\q"`,
		`b"\xZZ"`,
		`)`,
		string([]byte{0xff}),
	} {
		t.Run(strconv.Quote(src), func(t *testing.T) {
			mustErr(t, src)
		})
	}
}

func TestErrorIncludesSpan(t *testing.T) {
	err := mustErr(t, "  \"oops")
	rderr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if rderr.Span.File != "test" {
		t.Errorf("error file: %q", rderr.Span.File)
	}
	if rderr.Span.Start.Col != 3 {
		t.Errorf("error start col = %d, want 3", rderr.Span.Start.Col)
	}
}

func TestReadOneReturnsByteCount(t *testing.T) {
	_, n, err := ReadOne("test", "foo bar")
	if err != nil {
		t.Fatal(err)
	}
	if n != len("foo bar") {
		t.Errorf("ReadOne consumed %d bytes, want %d", n, len("foo bar"))
	}
}

func TestReadOneOnEmptyReturnsEOF(t *testing.T) {
	_, _, err := ReadOne("test", "   ")
	if err == nil || !strings.Contains(err.Error(), "EOF") {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestEmptySourceAndTrivia(t *testing.T) {
	for _, src := range []string{"", "   \n  # just a comment\n   \n"} {
		forms, err := ReadAll("test", src)
		if err != nil || len(forms) != 0 {
			t.Errorf("ReadAll(%q) = forms %v err %v, want no forms/no error", src, forms, err)
		}
	}
}
