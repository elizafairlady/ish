package core

// Datum is any value that may appear as data: scalars, runtime compound
// values produced by quote, and Syntax values themselves.
type Datum interface{ datum() }

type (
	Word   string
	Atom   string
	Int    int64
	Float  float64
	String string
	Bytes  []byte
	Nil    struct{}
	// PID and Ref are opaque process and monitor identities. They are scalar
	// datums so they compose into tuples like {:down ref pid reason}.
	PID uint64
	Ref uint64
)

// Meta is a `%:name` metadata tag. It is reader/expansion-time scaffolding —
// package headers (`%:file`), operator metadata tags — and is NOT a Datum: it
// is never a runtime value, so it does not compose into data and `kind` never
// classifies it. It appears only as a *Syntax.Node (which is `any`).
type Meta string

// Runtime compound kinds. Children are Datum.
type (
	Pair      struct{ Head, Tail Datum }
	Vector    []Datum
	Tuple     []Datum
	Dict      []DictEntry
	DictEntry struct{ Key, Value Datum }
)

// Tagged is a nominal record value: a Tag naming its type plus a Fields
// payload. It is the one extensible runtime value kind — distinct from the
// fixed structural kinds — so ish source can introduce record types and
// dispatch on a value's tag (make-tagged / tag-of / tagged-fields), building
// value-level generics and protocols without further Go support.
type Tagged struct {
	Tag    Atom
	Fields Datum
}

func (Word) datum()    {}
func (Atom) datum()    {}
func (Int) datum()     {}
func (Float) datum()   {}
func (String) datum()  {}
func (Bytes) datum()   {}
func (Nil) datum()     {}
func (PID) datum()     {}
func (Ref) datum()     {}
func (Pair) datum()    {}
func (Vector) datum()  {}
func (Tuple) datum()   {}
func (Dict) datum()    {}
func (Tagged) datum()  {}
func (*Syntax) datum() {}

// ListElems walks a Pair/Nil cons chain into its element data and final tail
// (Nil for a proper list, the improper cdr otherwise). A non-pair datum yields
// (nil, d). It is the single datum cons-walk shared by the runtime sequence
// views (asSequence, the matcher's list view, splice flattening).
func ListElems(d Datum) (elems []Datum, tail Datum) {
	cur := d
	for {
		p, ok := cur.(Pair)
		if !ok {
			return elems, cur
		}
		elems = append(elems, p.Head)
		cur = p.Tail
	}
}

// DatumEqual is the single structural-equality relation over data. It is
// explicit (no reflect) so every datum kind has defined semantics, and it is
// shared by every "are these data equal?" site — `eq?`, dict keys, literal/pin
// patterns, syntax-parse `~datum`/literal matching, and the expander's
// duplicate-key check. Functions are not Datums and are compared by the eval
// layer (identity), which delegates the data case here. Two Syntax values are
// equal iff their datum projections are equal (scopes/spans are not data).
func DatumEqual(a, b Datum) bool {
	switch av := a.(type) {
	case Word:
		bv, ok := b.(Word)
		return ok && av == bv
	case Atom:
		bv, ok := b.(Atom)
		return ok && av == bv
	case Int:
		bv, ok := b.(Int)
		return ok && av == bv
	case Float:
		bv, ok := b.(Float)
		return ok && av == bv
	case String:
		bv, ok := b.(String)
		return ok && av == bv
	case Nil:
		_, ok := b.(Nil)
		return ok
	case PID:
		bv, ok := b.(PID)
		return ok && av == bv
	case Ref:
		bv, ok := b.(Ref)
		return ok && av == bv
	case Bytes:
		bv, ok := b.(Bytes)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if av[i] != bv[i] {
				return false
			}
		}
		return true
	case Pair:
		bv, ok := b.(Pair)
		return ok && DatumEqual(av.Head, bv.Head) && DatumEqual(av.Tail, bv.Tail)
	case Vector:
		bv, ok := b.(Vector)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !DatumEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case Tuple:
		bv, ok := b.(Tuple)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !DatumEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case Dict:
		bv, ok := b.(Dict)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !DatumEqual(av[i].Key, bv[i].Key) || !DatumEqual(av[i].Value, bv[i].Value) {
				return false
			}
		}
		return true
	case *Syntax:
		bv, ok := b.(*Syntax)
		return ok && DatumEqual(SyntaxToDatum(av), SyntaxToDatum(bv))
	case Tagged:
		bv, ok := b.(Tagged)
		return ok && av.Tag == bv.Tag && DatumEqual(av.Fields, bv.Fields)
	case *Closure:
		bv, ok := b.(*Closure)
		return ok && av == bv
	case *Native:
		bv, ok := b.(*Native)
		return ok && av == bv
	}
	return false
}

func CloneDatum(d Datum) Datum {
	switch v := d.(type) {
	case Bytes:
		out := make(Bytes, len(v))
		copy(out, v)
		return out
	case Pair:
		return Pair{Head: CloneDatum(v.Head), Tail: CloneDatum(v.Tail)}
	case Vector:
		out := make(Vector, len(v))
		for i, elem := range v {
			out[i] = CloneDatum(elem)
		}
		return out
	case Tuple:
		out := make(Tuple, len(v))
		for i, elem := range v {
			out[i] = CloneDatum(elem)
		}
		return out
	case Dict:
		out := make(Dict, len(v))
		for i, elem := range v {
			out[i] = DictEntry{Key: CloneDatum(elem.Key), Value: CloneDatum(elem.Value)}
		}
		return out
	case *Syntax:
		return CloneSyntax(v)
	case Tagged:
		return Tagged{Tag: v.Tag, Fields: CloneDatum(v.Fields)}
	case *Closure, *Native:
		// Functions are immutable values: a closure captures an immutable
		// environment, so a copy is the value itself — sharing it across a
		// process boundary cannot violate isolation.
		return v
	default:
		return v
	}
}
