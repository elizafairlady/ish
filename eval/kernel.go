package eval

import (
	"fmt"
	"math"
	"reflect"

	"ish/core"
	"ish/expand"
)

// InstallRuntimeKernel registers runtime kernel value bindings into tbl at
// the empty scope set, dual-phased so phase-1 (macro-expansion-time) code
// can also call these primitives.
func InstallRuntimeKernel(tbl *expand.BindingTable) {
	for _, ph := range []core.Phase{core.PhaseRuntime, core.PhaseExpand} {
		def := func(name string, fn GoFunc) {
			tbl.Define(core.Word(name), ph, expand.DefaultSpace, core.ScopeSet{}, expand.ValueBinding, Native(name, fn))
		}
		def("cons", consFn)
		def("append", appendFn)
		def("first", firstFn)
		def("rest", restFn)
		def("to-list", toListFn)
		def("tuple", tupleFn)
		def("vector", vectorFn)
		def("dict", dictFn)
		def("dict-get", dictGetFn)
		def("dict-put", dictPutFn)
		def("make-tagged", makeTaggedFn)
		def("tag-of", tagOfFn)
		def("tagged-fields", taggedFieldsFn)
		def("panic", panicFn)
		def("rescue", rescueFn)
		def("ensure", ensureFn)
		def("str-concat", strConcatFn)
		def("str-length", strLengthFn)
		def("str-slice", strSliceFn)
		def("str-split", strSplitFn)
		def("to-string", toStringFn)
		def("print", printFn)
		def("local-expand", localExpandFn)
		def("bind!", bindIdentifierFn)
		def("apply", applyFn)
		def("eq?", eqFn)
		def("not", notFn)
		def("lt?", binCmp("lt?", func(c int) bool { return c < 0 }))
		def("gt?", binCmp("gt?", func(c int) bool { return c > 0 }))
		def("lte?", binCmp("lte?", func(c int) bool { return c <= 0 }))
		def("gte?", binCmp("gte?", func(c int) bool { return c >= 0 }))
		def("add", numFold("add", 0, func(a, b float64) float64 { return a + b }, checkedAddInt))
		def("sub", subFn)
		def("mul", numFold("mul", 1, func(a, b float64) float64 { return a * b }, checkedMulInt))
		def("div", divFn)
		def("mod", modFn)
		def("neg", negFn)
		def("kind", kindFn)
		def("datum->syntax", datumToSyntaxFn)
		def("syntax->datum", syntaxToDatumFn)
		def("syntax-list", syntaxListFn)
		def("syntax-vector", syntaxVectorFn)
		def("syntax-tuple", syntaxTupleFn)
		def("syntax-dict", syntaxDictFn)
		def("syntax-splice", syntaxSpliceFn)
		def("syntax-repeat", syntaxRepeatFn)
		def("list-splice", listSpliceFn)
		def("syntax-error", syntaxErrorFn)
		def("syntax-kind", syntaxKindFn)
		def("syntax-property", syntaxPropertyFn)
		def("syntax-word?", syntaxWordPredFn)
		def("dotted-parts", dottedPartsFn)
		def("space-of", spaceOfFn)
		def("resolve", resolveFn)
		def("binding-kind", bindingKindFn)
		def("ref->syntax", refToSyntaxFn)
		def("bound-identifier=?", boundIdentifierEqFn)
		def("free-identifier=?", freeIdentifierEqFn)
	}
	{
		ph := core.PhaseRuntime
		def := func(name string, fn GoFunc) {
			tbl.Define(core.Word(name), ph, expand.DefaultSpace, core.ScopeSet{}, expand.ValueBinding, Native(name, fn))
		}
		tbl.Define(core.Word("self"), ph, expand.DefaultSpace, core.ScopeSet{}, expand.ValueBinding, selfValue{})
		def("spawn", spawnFn)
		def("spawn-monitor", spawnMonitorFn)
		def("spawn-link", spawnLinkFn)
		def("send", sendFn)
		def("monitor", monitorFn)
		def("unmonitor", unmonitorFn)
		def("link", linkFn)
		def("unlink", unlinkFn)
		def("trap-exit", trapExitFn)
		def("register", registerFn)
		def("unregister", unregisterFn)
		def("whereis", whereisFn)
	}
}

// Argument helpers shared by the kernel primitives, so arity and type checks
// are spelled and worded one way rather than re-authored at every primitive.
func wantArgs(name string, args []Value, n int) error {
	if len(args) != n {
		return fmt.Errorf("%s: want %d arg(s), got %d", name, n, len(args))
	}
	return nil
}

func argDatum(name string, args []Value, i int) (core.Datum, error) {
	d, ok := args[i].(core.Datum)
	if !ok {
		return nil, fmt.Errorf("%s: argument %d must be data, got %T", name, i+1, args[i])
	}
	return d, nil
}

func argSyntax(name string, args []Value, i int) (*core.Syntax, error) {
	s, ok := args[i].(*core.Syntax)
	if !ok {
		return nil, fmt.Errorf("%s: argument %d must be syntax, got %T", name, i+1, args[i])
	}
	return s, nil
}

func consFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("cons", args, 2); err != nil {
		return nil, err
	}
	h, err := argDatum("cons", args, 0)
	if err != nil {
		return nil, err
	}
	t, err := argDatum("cons", args, 1)
	if err != nil {
		return nil, err
	}
	return core.Pair{Head: h, Tail: t}, nil
}

func appendFn(args []Value, _ *Env) (Value, error) {
	var elems []core.Datum
	for i, arg := range args {
		if inner, _, ok := spliceParts(arg); ok {
			arg = inner
		}
		d, ok := arg.(core.Datum)
		if !ok {
			return nil, fmt.Errorf("append: argument %d is not data", i)
		}
		seq, ok := asSequence(d)
		if !ok {
			return nil, fmt.Errorf("append: argument %d is not a sequence", i)
		}
		elems = append(elems, seq...)
	}
	return listFromDatums(elems), nil
}

// spliceParts decodes the `{:splice datum [depth]}` marker tuple shared by the
// list- and syntax-splicing primitives. depth defaults to 1 when omitted.
func spliceParts(v Value) (core.Datum, int, bool) {
	t, ok := v.(core.Tuple)
	if !ok || len(t) < 2 || t[0] != core.Atom("splice") {
		return nil, 0, false
	}
	depth := 1
	if len(t) >= 3 {
		if d, ok := t[2].(core.Int); ok {
			depth = int(d)
		}
	}
	d, ok := t[1].(core.Datum)
	if !ok {
		return nil, 0, false
	}
	return d, depth, true
}

// asSequence is the single sequence abstraction: it views a datum as an ordered
// element slice — the empty list, a proper Pair-list, a Vector, or a Tuple.
// first/rest/append/to-list all consume it (first/rest keep a Pair fast path so
// cons-list traversal stays O(1) and preserves improper tails).
func asSequence(d core.Datum) ([]core.Datum, bool) {
	switch v := d.(type) {
	case core.Nil:
		return nil, true
	case core.Vector:
		return []core.Datum(v), true
	case core.Tuple:
		return []core.Datum(v), true
	case core.Pair:
		elems, tail := core.ListElems(v)
		if _, ok := tail.(core.Nil); ok {
			return elems, true
		}
	}
	return nil, false
}

func firstFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("first", args, 1); err != nil {
		return nil, err
	}
	if p, ok := args[0].(core.Pair); ok {
		return p.Head, nil
	}
	d, err := argDatum("first", args, 0)
	if err != nil {
		return nil, err
	}
	seq, ok := asSequence(d)
	if !ok || len(seq) == 0 {
		return nil, fmt.Errorf("first: empty or not a sequence")
	}
	return seq[0], nil
}

func restFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("rest", args, 1); err != nil {
		return nil, err
	}
	if p, ok := args[0].(core.Pair); ok {
		return p.Tail, nil
	}
	d, err := argDatum("rest", args, 0)
	if err != nil {
		return nil, err
	}
	seq, ok := asSequence(d)
	if !ok || len(seq) == 0 {
		return nil, fmt.Errorf("rest: empty or not a sequence")
	}
	return listFromDatums(seq[1:]), nil
}

func toListFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("to-list", args, 1); err != nil {
		return nil, err
	}
	d, err := argDatum("to-list", args, 0)
	if err != nil {
		return nil, err
	}
	seq, ok := asSequence(d)
	if !ok {
		return nil, fmt.Errorf("to-list: unsupported %T", args[0])
	}
	return listFromDatums(seq), nil
}

func listFromDatums(ds []core.Datum) core.Datum {
	var cur core.Datum = core.Nil{}
	for i := len(ds) - 1; i >= 0; i-- {
		cur = core.Pair{Head: ds[i], Tail: cur}
	}
	return cur
}

func tupleFn(args []Value, _ *Env) (Value, error) {
	out := make(core.Tuple, len(args))
	for i, a := range args {
		d, ok := a.(core.Datum)
		if !ok {
			return nil, fmt.Errorf("tuple: element %d is not data", i)
		}
		out[i] = d
	}
	return out, nil
}

func vectorFn(args []Value, _ *Env) (Value, error) {
	out := make(core.Vector, len(args))
	for i, a := range args {
		d, ok := a.(core.Datum)
		if !ok {
			return nil, fmt.Errorf("vector: element %d is not data", i)
		}
		out[i] = d
	}
	return out, nil
}

func dictFn(args []Value, _ *Env) (Value, error) {
	if len(args)%2 != 0 {
		return nil, fmt.Errorf("dict: requires even number of arguments")
	}
	out := make(core.Dict, 0, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		k, kok := args[i].(core.Datum)
		v, vok := args[i+1].(core.Datum)
		if !kok || !vok {
			return nil, fmt.Errorf("dict: entry %d is not data", i/2)
		}
		out = append(out, core.DictEntry{Key: k, Value: v})
	}
	return out, nil
}

func dictGetFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("dict-get", args, 2); err != nil {
		return nil, err
	}
	d, ok := args[0].(core.Dict)
	if !ok {
		return nil, fmt.Errorf("dict-get: not a dict")
	}
	k, ok := args[1].(core.Datum)
	if !ok {
		return nil, fmt.Errorf("dict-get: key must be data")
	}
	for _, e := range d {
		if core.DatumEqual(e.Key, k) {
			return e.Value, nil
		}
	}
	return core.Nil{}, nil
}

func dictPutFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("dict-put", args, 3); err != nil {
		return nil, err
	}
	d, ok := args[0].(core.Dict)
	if !ok {
		return nil, fmt.Errorf("dict-put: not a dict")
	}
	k, kok := args[1].(core.Datum)
	v, vok := args[2].(core.Datum)
	if !kok || !vok {
		return nil, fmt.Errorf("dict-put: key/value must be data")
	}
	out := make(core.Dict, 0, len(d)+1)
	replaced := false
	for _, e := range d {
		if core.DatumEqual(e.Key, k) {
			out = append(out, core.DictEntry{Key: k, Value: v})
			replaced = true
		} else {
			out = append(out, e)
		}
	}
	if !replaced {
		out = append(out, core.DictEntry{Key: k, Value: v})
	}
	return out, nil
}

func applyFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("apply", args, 2); err != nil {
		return nil, err
	}
	if !isCallable(args[0]) {
		return nil, fmt.Errorf("apply: first arg not callable")
	}
	var flat []Value
	cur := args[1]
	for {
		switch v := cur.(type) {
		case core.Nil:
			return apply(args[0], flat, env)
		case core.Pair:
			flat = append(flat, v.Head.(Value))
			cur = v.Tail.(Value)
		default:
			return nil, fmt.Errorf("apply: arg list ended improperly with %T", cur)
		}
	}
}

func eqFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("eq?", args, 2); err != nil {
		return nil, err
	}
	if datumEqual(args[0], args[1]) {
		return core.Atom("true"), nil
	}
	return core.Atom("false"), nil
}

func notFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("not", args, 1); err != nil {
		return nil, err
	}
	if truthy(args[0]) {
		return core.Atom("false"), nil
	}
	return core.Atom("true"), nil
}

func binCmp(name string, accept func(int) bool) GoFunc {
	return func(args []Value, _ *Env) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("%s: want 2 args", name)
		}
		c, err := compareValues(args[0], args[1])
		if err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		if accept(c) {
			return core.Atom("true"), nil
		}
		return core.Atom("false"), nil
	}
}

func compareValues(a, b Value) (int, error) {
	ai, aIsInt := a.(core.Int)
	bi, bIsInt := b.(core.Int)
	af, aIsFlt := a.(core.Float)
	bf, bIsFlt := b.(core.Float)
	if aIsInt && bIsInt {
		switch {
		case ai < bi:
			return -1, nil
		case ai > bi:
			return 1, nil
		}
		return 0, nil
	}
	if (aIsInt || aIsFlt) && (bIsInt || bIsFlt) {
		var av, bv float64
		if aIsInt {
			av = float64(ai)
		} else {
			av = float64(af)
		}
		if bIsInt {
			bv = float64(bi)
		} else {
			bv = float64(bf)
		}
		switch {
		case av < bv:
			return -1, nil
		case av > bv:
			return 1, nil
		}
		return 0, nil
	}
	as, aIsStr := a.(core.String)
	bs, bIsStr := b.(core.String)
	if aIsStr && bIsStr {
		switch {
		case as < bs:
			return -1, nil
		case as > bs:
			return 1, nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("incomparable types %T and %T", a, b)
}

func numFold(name string, zero int64, fpFn func(float64, float64) float64, intFn func(int64, int64) (int64, error)) GoFunc {
	return func(args []Value, _ *Env) (Value, error) {
		if len(args) == 0 {
			return core.Int(zero), nil
		}
		hasFloat := false
		for _, a := range args {
			if _, ok := a.(core.Float); ok {
				hasFloat = true
				break
			}
		}
		if hasFloat {
			var acc float64
			set := false
			for _, a := range args {
				v, err := asFloat(a)
				if err != nil {
					return nil, fmt.Errorf("%s: %v", name, err)
				}
				if !set {
					acc = v
					set = true
				} else {
					acc = fpFn(acc, v)
				}
			}
			return core.Float(acc), nil
		}
		var acc int64
		set := false
		for _, a := range args {
			i, ok := a.(core.Int)
			if !ok {
				return nil, fmt.Errorf("%s: non-numeric %T", name, a)
			}
			if !set {
				acc = int64(i)
				set = true
			} else {
				var err error
				if acc, err = intFn(acc, int64(i)); err != nil {
					return nil, fmt.Errorf("%s: %v", name, err)
				}
			}
		}
		return core.Int(acc), nil
	}
}

// Checked integer ops: ish integers are fixed-width int64, so an operation that
// would wrap is reported as an error rather than silently producing a wrong
// value. (A future numeric tower would promote instead; until then, erroring is
// the only answer that is never silently incorrect.)
func checkedAddInt(a, b int64) (int64, error) {
	s := a + b
	if (a > 0 && b > 0 && s < 0) || (a < 0 && b < 0 && s >= 0) {
		return 0, fmt.Errorf("integer overflow")
	}
	return s, nil
}

func checkedSubInt(a, b int64) (int64, error) {
	d := a - b
	if (b < 0 && d < a) || (b > 0 && d > a) {
		return 0, fmt.Errorf("integer overflow")
	}
	return d, nil
}

func checkedMulInt(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	if (a == math.MinInt64 && b == -1) || (b == math.MinInt64 && a == -1) {
		return 0, fmt.Errorf("integer overflow")
	}
	p := a * b
	if p/b != a {
		return 0, fmt.Errorf("integer overflow")
	}
	return p, nil
}

func asFloat(v Value) (float64, error) {
	switch x := v.(type) {
	case core.Int:
		return float64(x), nil
	case core.Float:
		return float64(x), nil
	}
	return 0, fmt.Errorf("not numeric: %T", v)
}

func subFn(args []Value, env *Env) (Value, error) {
	if len(args) == 0 {
		return core.Int(0), nil
	}
	if len(args) == 1 {
		return negFn(args, env)
	}
	hasFloat := false
	for _, a := range args {
		if _, ok := a.(core.Float); ok {
			hasFloat = true
			break
		}
	}
	if hasFloat {
		acc, err := asFloat(args[0])
		if err != nil {
			return nil, err
		}
		for _, a := range args[1:] {
			v, err := asFloat(a)
			if err != nil {
				return nil, err
			}
			acc -= v
		}
		return core.Float(acc), nil
	}
	first, ok := args[0].(core.Int)
	if !ok {
		return nil, fmt.Errorf("sub: non-numeric %T", args[0])
	}
	acc := int64(first)
	for _, a := range args[1:] {
		i, ok := a.(core.Int)
		if !ok {
			return nil, fmt.Errorf("sub: non-numeric %T", a)
		}
		var err error
		if acc, err = checkedSubInt(acc, int64(i)); err != nil {
			return nil, fmt.Errorf("sub: %v", err)
		}
	}
	return core.Int(acc), nil
}

func divFn(args []Value, _ *Env) (Value, error) {
	if len(args) == 0 {
		return core.Int(0), nil
	}
	if len(args) == 1 {
		v, err := asFloat(args[0])
		if err != nil {
			return nil, err
		}
		if v == 0 {
			return nil, fmt.Errorf("div: division by zero")
		}
		return core.Float(1.0 / v), nil
	}
	acc, err := asFloat(args[0])
	if err != nil {
		return nil, err
	}
	for _, a := range args[1:] {
		v, err := asFloat(a)
		if err != nil {
			return nil, err
		}
		if v == 0 {
			return nil, fmt.Errorf("div: division by zero")
		}
		acc /= v
	}
	return core.Float(acc), nil
}

func modFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("mod", args, 2); err != nil {
		return nil, err
	}
	a, aok := args[0].(core.Int)
	b, bok := args[1].(core.Int)
	if !aok || !bok {
		return nil, fmt.Errorf("mod: integers only")
	}
	if b == 0 {
		return nil, fmt.Errorf("mod: division by zero")
	}
	// Floored modulo (result takes the sign of the divisor), matching the
	// mathematical `mod` the name implies: `mod -7 3` is 2, not Go's truncated -1.
	m := int64(a) % int64(b)
	if m != 0 && (m < 0) != (int64(b) < 0) {
		m += int64(b)
	}
	return core.Int(m), nil
}

func negFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("neg", args, 1); err != nil {
		return nil, err
	}
	switch x := args[0].(type) {
	case core.Int:
		return -x, nil
	case core.Float:
		return -x, nil
	}
	return nil, fmt.Errorf("neg: not numeric: %T", args[0])
}

func kindFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("kind", args, 1); err != nil {
		return nil, err
	}
	switch args[0].(type) {
	case core.Word:
		return core.Atom("word"), nil
	case core.Atom:
		return core.Atom("atom"), nil
	case core.Int:
		return core.Atom("int"), nil
	case core.Float:
		return core.Atom("float"), nil
	case core.String:
		return core.Atom("string"), nil
	case core.Bytes:
		return core.Atom("bytes"), nil
	case core.Nil:
		return core.Nil{}, nil
	case core.Pair:
		return core.Atom("pair"), nil
	case core.Vector:
		return core.Atom("vector"), nil
	case core.Tuple:
		return core.Atom("tuple"), nil
	case core.Dict:
		return core.Atom("dict"), nil
	case core.Tagged:
		return core.Atom("tagged"), nil
	case core.PID:
		return core.Atom("pid"), nil
	case core.Ref:
		return core.Atom("ref"), nil
	case *core.Syntax:
		return core.Atom("syntax"), nil
	case *core.Closure, *core.Native:
		return core.Atom("function"), nil
	}
	return core.Atom("unknown"), nil
}

// makeTaggedFn constructs a Tagged record from a tag atom and a fields payload.
// Tagged is the one extensible runtime value kind; together with tag-of and
// tagged-fields it lets ish source define record types and dispatch on a
// value's tag, building value-level generics/protocols in the language.
func makeTaggedFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("make-tagged", args, 2); err != nil {
		return nil, err
	}
	tag, ok := args[0].(core.Atom)
	if !ok {
		return nil, fmt.Errorf("make-tagged: tag must be an atom, got %T", args[0])
	}
	fields, ok := args[1].(core.Datum)
	if !ok {
		return nil, fmt.Errorf("make-tagged: fields must be a datum, got %T", args[1])
	}
	return core.Tagged{Tag: tag, Fields: fields}, nil
}

// tagOfFn returns a tagged value's tag atom, or :false for any other value. It
// is the dispatch key for value-level generics.
func tagOfFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("tag-of", args, 1); err != nil {
		return nil, err
	}
	if t, ok := args[0].(core.Tagged); ok {
		return t.Tag, nil
	}
	return core.Atom("false"), nil
}

// taggedFieldsFn returns a tagged value's fields payload, or :false otherwise.
func taggedFieldsFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("tagged-fields", args, 1); err != nil {
		return nil, err
	}
	if t, ok := args[0].(core.Tagged); ok {
		return t.Fields, nil
	}
	return core.Atom("false"), nil
}

func datumToSyntaxFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("datum->syntax", args, 2); err != nil {
		return nil, err
	}
	ctx, _ := args[0].(*core.Syntax)
	d, err := argDatum("datum->syntax", args, 1)
	if err != nil {
		return nil, err
	}
	return core.DatumToSyntax(ctx, d), nil
}

func syntaxToDatumFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("syntax->datum", args, 1); err != nil {
		return nil, err
	}
	s, err := argSyntax("syntax->datum", args, 0)
	if err != nil {
		return nil, err
	}
	return core.SyntaxToDatum(core.LowerReaderSyntax(s)), nil
}

func syntaxListFn(args []Value, _ *Env) (Value, error) {
	elems, err := syntaxArgs("syntax-list", args)
	if err != nil {
		return nil, err
	}
	return core.SyntaxList(core.Span{}, elems...), nil
}

func syntaxErrorFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("syntax-error", args, 2); err != nil {
		return nil, err
	}
	stx, err := argSyntax("syntax-error", args, 0)
	if err != nil {
		return nil, err
	}
	msg, ok := args[1].(core.String)
	if !ok {
		return nil, fmt.Errorf("syntax-error: second arg must be string")
	}
	return nil, &core.SyntaxError{Syntax: stx, Message: fmt.Sprintf("syntax error: %s", msg)}
}

func syntaxCaptureEqual(a, b *core.Syntax) bool {
	return core.DatumEqual(core.SyntaxToDatum(a), core.SyntaxToDatum(b)) && reflect.DeepEqual(a.Scopes, b.Scopes)
}

func literalIdentifierMatches(a, b *core.Syntax, env *Env) bool {
	if env != nil && env.Resolver != nil {
		return env.Resolver.FreeIdentifierEqual(a, b)
	}
	// With no binding resolver in scope, fall back to bound-identifier
	// semantics: same word and the literal's scope set is a subset of the
	// target's. No name-only escape hatch — equal spelling alone never wins.
	aw, aok := a.Node.(core.Word)
	bw, bok := b.Node.(core.Word)
	if !aok || !bok || aw != bw {
		return false
	}
	return a.Scopes[core.PhaseRuntime].Subset(b.Scopes[core.PhaseRuntime])
}

func syntaxKindFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("syntax-kind", args, 1); err != nil {
		return nil, err
	}
	stx, err := argSyntax("syntax-kind", args, 0)
	if err != nil {
		return nil, err
	}
	return kindFn([]Value{stx.Node}, nil)
}

// syntaxPropertyFn is the syntax-property accessor (after Racket): with two
// arguments it reads the property under a key (atom or string), returning :nil
// when absent; with three it returns a copy of the syntax carrying the property.
// This is what makes the reader's per-token metadata (token-raw, token-kind,
// leading-space, adjacent-previous, reader-shape) — and any macro-attached
// property — observable in the language.
func syntaxPropertyFn(args []Value, _ *Env) (Value, error) {
	if len(args) != 2 && len(args) != 3 {
		return nil, fmt.Errorf("syntax-property expects 2 or 3 arguments, got %d", len(args))
	}
	stx, err := argSyntax("syntax-property", args, 0)
	if err != nil {
		return nil, err
	}
	key, err := propertyKey(args[1])
	if err != nil {
		return nil, err
	}
	if len(args) == 3 {
		val, ok := args[2].(core.Datum)
		if !ok {
			return nil, fmt.Errorf("syntax-property: value must be data, got %T", args[2])
		}
		return &core.Syntax{
			Node:       stx.Node,
			Span:       stx.Span,
			Scopes:     stx.Scopes,
			Properties: stx.Properties.With(key, val),
			Origin:     append([]core.Origin(nil), stx.Origin...),
		}, nil
	}
	if v, ok := stx.Properties.Get(key); ok {
		return v, nil
	}
	return core.Nil{}, nil
}

// propertyKey accepts an atom or string as a syntax-property key.
func propertyKey(v Value) (string, error) {
	switch k := v.(type) {
	case core.Atom:
		return string(k), nil
	case core.String:
		return string(k), nil
	}
	return "", fmt.Errorf("syntax-property: key must be an atom or string, got %T", v)
}

func syntaxWordPredFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("syntax-word?", args, 1); err != nil {
		return nil, err
	}
	stx, err := argSyntax("syntax-word?", args, 0)
	if err != nil {
		return nil, err
	}
	_, isWord := stx.Node.(core.Word)
	if isWord {
		return core.Atom("true"), nil
	}
	return core.Atom("false"), nil
}

func boundIdentifierEqFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("bound-identifier=?", args, 2); err != nil {
		return nil, err
	}
	a, aok := args[0].(*core.Syntax)
	b, bok := args[1].(*core.Syntax)
	if !aok || !bok {
		return nil, fmt.Errorf("bound-identifier=?: arguments must be syntax")
	}
	// Compare at the resolver's phase when one is in scope; otherwise fall back
	// to the runtime phase.
	equal := false
	if env != nil && env.Resolver != nil {
		equal = env.Resolver.BoundIdentifierEqual(a, b)
	} else {
		equal = core.BoundIdentEqual(a, b, core.PhaseRuntime)
	}
	if equal {
		return core.Atom("true"), nil
	}
	return core.Atom("false"), nil
}

func freeIdentifierEqFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("free-identifier=?", args, 2); err != nil {
		return nil, err
	}
	a, aok := args[0].(*core.Syntax)
	b, bok := args[1].(*core.Syntax)
	if !aok || !bok {
		return nil, fmt.Errorf("free-identifier=?: arguments must be syntax")
	}
	if env == nil || env.Resolver == nil {
		return nil, fmt.Errorf("free-identifier=?: no binding resolver in scope")
	}
	if env.Resolver.FreeIdentifierEqual(a, b) {
		return core.Atom("true"), nil
	}
	return core.Atom("false"), nil
}

// dottedPartsFn splits a dotted reader access `(%-expr base . member args...)`
// into `{base member args chain}`: base and member are syntax; args is the list
// of trailing application arguments (the tokens after member, up to any further
// access `.`); chain is the remaining `. member ...` continuation of a *chained*
// access like `a.b.c` (empty for a plain single-segment access). Splitting the
// chain out is what stops a literal `.` from leaking into member's argument
// list. It returns `false` for any form that is not a dotted access.
//
// It is the one substrate definition of the dotted-access *shape* (the reader's
// `.` infix convention), so every access-like protocol — default qualified
// access, a `missing` lookup, future field/method protocols — shares the same
// split. It commits to no *meaning*: whether `base.member` is a package access,
// a method, or a field, and what a chain means, is the resolving protocol's
// decision, not this helper's.
func dottedPartsFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("dotted-parts", args, 1); err != nil {
		return nil, err
	}
	stx, err := argSyntax("dotted-parts", args, 0)
	if err != nil {
		return nil, err
	}
	parts, ok := core.ReaderExprElems(stx)
	if !ok || len(parts) < 3 {
		return core.Atom("false"), nil
	}
	if w, ok := parts[1].Node.(core.Word); !ok || w != "." {
		return core.Atom("false"), nil
	}
	rest := parts[3:]
	cut := len(rest)
	for i, e := range rest {
		if w, ok := e.Node.(core.Word); ok && w == "." {
			cut = i
			break
		}
	}
	argData := make([]core.Datum, cut)
	for i := 0; i < cut; i++ {
		argData[i] = rest[i]
	}
	chainData := make([]core.Datum, len(rest)-cut)
	for i := cut; i < len(rest); i++ {
		chainData[i-cut] = rest[i]
	}
	return core.Tuple{parts[0], parts[2], listFromDatums(argData), listFromDatums(chainData)}, nil
}

// spaceOfFn returns the binding space an identifier's binding introduces — for
// a package alias, the space its exports inhabit — reified so a protocol can
// pass it to resolve. It returns `:nospace` for an identifier with no space.
func spaceOfFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("space-of", args, 1); err != nil {
		return nil, err
	}
	stx, err := argSyntax("space-of", args, 0)
	if err != nil {
		return nil, err
	}
	if env == nil || env.Resolver == nil {
		return nil, fmt.Errorf("space-of: no binding resolver in scope")
	}
	sv, ok := env.Resolver.SpaceOf(stx)
	if !ok {
		return core.Atom("nospace"), nil
	}
	return sv, nil
}

// resolveFn resolves a member identifier in a space, the substrate operation
// `resolve member space`. It returns the resolution ref on success, or the
// atoms `:unbound` / `:ambiguous`. A lookup protocol claims on `:unbound`; the
// default access protocol claims on a found value ref.
func resolveFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("resolve", args, 2); err != nil {
		return nil, err
	}
	member, err := argSyntax("resolve", args, 0)
	if err != nil {
		return nil, err
	}
	sv, ok := args[1].(expand.SpaceValue)
	if !ok {
		return nil, fmt.Errorf("resolve: second argument must be a space (from space-of)")
	}
	if env == nil || env.Resolver == nil {
		return nil, fmt.Errorf("resolve: no binding resolver in scope")
	}
	b, r := env.Resolver.ResolveMember(member, sv)
	switch r {
	case expand.ResolveFound:
		return b, nil
	case expand.ResolveAmbiguous:
		return core.Atom("ambiguous"), nil
	default:
		return core.Atom("unbound"), nil
	}
}

// bindingKindFn classifies a resolution ref, so a protocol can decide whether a
// found binding is the kind it wants (e.g. the default access protocol claims
// only value members).
func bindingKindFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("binding-kind", args, 1); err != nil {
		return nil, err
	}
	b, ok := args[0].(*expand.Binding)
	if !ok {
		return nil, fmt.Errorf("binding-kind: argument must be a resolution ref")
	}
	return core.Atom(bindingKindName(b.Kind)), nil
}

func bindingKindName(k expand.BindingKind) string {
	switch k {
	case expand.ValueBinding:
		return "value"
	case expand.CoreFormBinding:
		return "core"
	case expand.CompileTimeFormBinding:
		return "compile-time"
	case expand.TransformerBinding:
		return "transformer"
	case expand.ProtocolBinding:
		return "protocol"
	case expand.ProtocolHandlerBinding:
		return "handler"
	case expand.OperatorBinding:
		return "operator"
	case expand.PackageBinding:
		return "package"
	default:
		return "unknown"
	}
}

// refToSyntaxFn turns a resolution ref into a reference syntax that evaluates to
// the bound value — the substrate affordance a space-resolution protocol uses to
// emit a binding it found in some other space (e.g. lowering `pkg.member` to a
// reference to the package's export).
func refToSyntaxFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("ref->syntax", args, 1); err != nil {
		return nil, err
	}
	b, ok := args[0].(*expand.Binding)
	if !ok {
		return nil, fmt.Errorf("ref->syntax: argument must be a resolution ref")
	}
	return &core.Syntax{Node: b.Ref()}, nil
}

func syntaxVectorFn(args []Value, _ *Env) (Value, error) {
	elems, err := syntaxArgs("syntax-vector", args)
	if err != nil {
		return nil, err
	}
	return &core.Syntax{Node: core.SyntaxVector(elems)}, nil
}

func syntaxTupleFn(args []Value, _ *Env) (Value, error) {
	elems, err := syntaxArgs("syntax-tuple", args)
	if err != nil {
		return nil, err
	}
	return &core.Syntax{Node: core.SyntaxTuple(elems)}, nil
}

func syntaxDictFn(args []Value, _ *Env) (Value, error) {
	elems, err := syntaxArgs("syntax-dict", args)
	if err != nil {
		return nil, err
	}
	if len(elems)%2 != 0 {
		return nil, fmt.Errorf("syntax-dict: requires even number of key/value syntax arguments")
	}
	entries := make(core.SyntaxDict, 0, len(elems)/2)
	for i := 0; i < len(elems); i += 2 {
		entries = append(entries, core.SyntaxDictEntry{Key: elems[i], Value: elems[i+1]})
	}
	return &core.Syntax{Node: entries}, nil
}

func syntaxSpliceFn(args []Value, _ *Env) (Value, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("syntax-splice: want 1 or 2 arguments")
	}
	d, ok := args[0].(core.Datum)
	if !ok {
		return nil, fmt.Errorf("syntax-splice: argument is %T, want syntax datum", args[0])
	}
	depth := core.Int(1)
	if len(args) == 2 {
		var ok bool
		depth, ok = args[1].(core.Int)
		if !ok || depth < 1 {
			return nil, fmt.Errorf("syntax-splice: depth must be positive int")
		}
	}
	return core.Tuple{core.Atom("splice"), d, depth}, nil
}

func syntaxRepeatFn(args []Value, _ *Env) (Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("syntax-repeat: want template, depth, sequences")
	}
	template, err := argSyntax("syntax-repeat", args, 0)
	if err != nil {
		return nil, err
	}
	depth, ok := args[1].(core.Int)
	if !ok || depth < 1 {
		return nil, fmt.Errorf("syntax-repeat: depth must be positive int")
	}
	seqs := make([][]*core.Syntax, len(args)-2)
	for i, arg := range args[2:] {
		seq, err := syntaxSequence(arg, int(depth))
		if err != nil {
			return nil, fmt.Errorf("syntax-repeat: sequence %d: %w", i, err)
		}
		seqs[i] = seq
	}
	count := 0
	if len(seqs) > 0 {
		count = len(seqs[0])
	}
	for i, seq := range seqs {
		if len(seq) != count {
			return nil, fmt.Errorf("syntax-repeat: sequence %d length %d, want %d", i, len(seq), count)
		}
	}
	out := make(core.SyntaxVector, count)
	for i := 0; i < count; i++ {
		pos := 0
		out[i] = substituteTemplate(template, i, seqs, &pos)
	}
	return core.Tuple{core.Atom("splice"), &core.Syntax{Node: out}, core.Int(1)}, nil
}

func substituteTemplate(template *core.Syntax, index int, seqs [][]*core.Syntax, pos *int) *core.Syntax {
	if elems, ok := core.SyntaxListElems(template); ok && len(elems) > 0 {
		if readerWordEval(elems[0]) == "%-group" {
			if len(elems) == 1 {
				return &core.Syntax{Node: core.Nil{}, Span: template.Span, Scopes: template.Scopes, Properties: template.Properties, Origin: template.Origin}
			}
			if len(elems) == 2 {
				inner := substituteTemplate(elems[1], index, seqs, pos)
				switch inner.Node.(type) {
				case core.SyntaxPair, core.Nil:
					return inner
				default:
					return core.SyntaxList(template.Span, inner)
				}
			}
		}
		if readerWordEval(elems[0]) == "%-expr" {
			out := make([]*core.Syntax, 0, len(elems)-1)
			for _, elem := range elems[1:] {
				out = append(out, substituteTemplate(elem, index, seqs, pos))
			}
			return core.SyntaxList(template.Span, out...)
		}
		if readerWordEval(elems[0]) == "unsyntax" && len(elems) == 2 && *pos < len(seqs) {
			value := seqs[*pos][index]
			*pos = *pos + 1
			return value
		}
		out := make([]*core.Syntax, len(elems))
		for i, elem := range elems {
			out[i] = substituteTemplate(elem, index, seqs, pos)
		}
		return core.SyntaxList(template.Span, out...)
	}
	switch n := template.Node.(type) {
	case core.SyntaxVector:
		out := make(core.SyntaxVector, len(n))
		for i, elem := range n {
			out[i] = substituteTemplate(elem, index, seqs, pos)
		}
		return &core.Syntax{Node: out, Span: template.Span, Scopes: template.Scopes, Properties: template.Properties, Origin: template.Origin}
	case core.SyntaxTuple:
		out := make(core.SyntaxTuple, len(n))
		for i, elem := range n {
			out[i] = substituteTemplate(elem, index, seqs, pos)
		}
		return &core.Syntax{Node: out, Span: template.Span, Scopes: template.Scopes, Properties: template.Properties, Origin: template.Origin}
	case core.SyntaxDict:
		out := make(core.SyntaxDict, len(n))
		for i, entry := range n {
			out[i] = core.SyntaxDictEntry{Key: substituteTemplate(entry.Key, index, seqs, pos), Value: substituteTemplate(entry.Value, index, seqs, pos)}
		}
		return &core.Syntax{Node: out, Span: template.Span, Scopes: template.Scopes, Properties: template.Properties, Origin: template.Origin}
	}
	return template
}

func readerWordEval(stx *core.Syntax) core.Word {
	if w, ok := stx.Node.(core.Word); ok {
		return w
	}
	return ""
}

func listSpliceFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("list-splice", args, 1); err != nil {
		return nil, err
	}
	d, ok := args[0].(core.Datum)
	if !ok {
		return nil, fmt.Errorf("list-splice: argument is %T, want datum sequence", args[0])
	}
	return core.Tuple{core.Atom("splice"), d}, nil
}

func syntaxArgs(name string, args []Value) ([]*core.Syntax, error) {
	var elems []*core.Syntax
	for i, arg := range args {
		if inner, depth, ok := spliceParts(arg); ok {
			spliced, err := syntaxSequence(inner, depth)
			if err != nil {
				return nil, fmt.Errorf("%s: splice argument %d: %w", name, i, err)
			}
			elems = append(elems, spliced...)
			continue
		}
		stx, ok := arg.(*core.Syntax)
		if !ok {
			return nil, fmt.Errorf("%s: argument %d is %T, want syntax", name, i, arg)
		}
		elems = append(elems, stx)
	}
	return elems, nil
}

func syntaxSequence(v Value, depth int) ([]*core.Syntax, error) {
	if depth <= 1 {
		return syntaxSequenceFlat(v)
	}
	seq, err := syntaxSequenceFlat(v)
	if err != nil {
		return nil, err
	}
	var out []*core.Syntax
	for _, elem := range seq {
		nested, err := syntaxSequence(elem, depth-1)
		if err != nil {
			return nil, err
		}
		out = append(out, nested...)
	}
	return out, nil
}

func syntaxSequenceFlat(v Value) ([]*core.Syntax, error) {
	switch seq := v.(type) {
	case *core.Syntax:
		if elems, ok := core.SyntaxListElems(seq); ok {
			return elems, nil
		}
		if vec, ok := seq.Node.(core.SyntaxVector); ok {
			return []*core.Syntax(vec), nil
		}
		return nil, fmt.Errorf("syntax is not a proper list or vector")
	case core.Vector:
		out := make([]*core.Syntax, len(seq))
		for i, elem := range seq {
			stx, ok := elem.(*core.Syntax)
			if !ok {
				return nil, fmt.Errorf("vector element %d is %T, want syntax", i, elem)
			}
			out[i] = stx
		}
		return out, nil
	case core.Pair, core.Nil:
		elems, tail := core.ListElems(seq.(core.Datum))
		if _, ok := tail.(core.Nil); !ok {
			return nil, fmt.Errorf("improper syntax splice list")
		}
		out := make([]*core.Syntax, len(elems))
		for i, elem := range elems {
			stx, ok := elem.(*core.Syntax)
			if !ok {
				return nil, fmt.Errorf("list element is %T, want syntax", elem)
			}
			out[i] = stx
		}
		return out, nil
	}
	return nil, fmt.Errorf("%T is not spliceable syntax sequence", v)
}
