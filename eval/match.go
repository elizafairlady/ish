package eval

import (
	"fmt"

	"ish/core"
)

// match is the single pattern matcher. It matches a compiled core.Pattern
// against a target term — either a runtime value (fn/match/bind/receive) or a
// syntax object (syntax-parse/syntax-case) — writing captures into frame and
// returning success plus, on failure, a human-readable reason (used for
// syntax-parse diagnostics; ignored by value matching). On any failure the
// frame may be partially written and the caller must discard it.
//
// Structural destructuring is shared across both worlds: a sequence pattern
// matches a Pair/Vector/Tuple/Dict value or its SyntaxPair/SyntaxVector/
// SyntaxTuple/SyntaxDict counterpart. Ellipsis/optional/group repetition and
// the combinator nodes arise only in syntax patterns and so see syntax targets.
func match(p core.Pattern, target Value, env *Env, frame map[core.BindingID]Value) (bool, string) {
	switch pat := p.(type) {
	case core.PatWild:
		return true, ""
	case core.PatLit:
		if litEqual(pat.Value, target) {
			return true, ""
		}
		return false, "literal did not match"
	case core.PatVar:
		if prior, ok := frame[pat.Ref.ID]; ok {
			if termEqual(prior, target) {
				return true, ""
			}
			return false, fmt.Sprintf("%s bound to differing terms", pat.Ref.Name)
		}
		frame[pat.Ref.ID] = target
		return true, ""
	case core.PatPin:
		existing, ok := env.lookup(pat.Ref.ID)
		if !ok {
			existing = pat.Ref.Value
		}
		if termEqual(existing, target) {
			return true, ""
		}
		return false, "pinned value did not match"
	case core.PatSeq:
		return matchSeq(pat, target, env, frame)
	case core.PatDict:
		return matchDict(pat, target, env, frame)
	case core.PatLiteral:
		t, ok := target.(*core.Syntax)
		if ok && literalIdentifierMatches(pat.Ident, t, env) {
			return true, ""
		}
		return false, fmt.Sprintf("expected literal identifier %v", core.SyntaxToDatum(pat.Ident))
	case core.PatAnd:
		for _, s := range pat {
			if ok, fail := match(s, target, env, frame); !ok {
				return false, fail
			}
		}
		return true, ""
	case core.PatOr:
		for _, s := range pat {
			trial := cloneFrame(frame)
			if ok, _ := match(s, target, env, trial); ok {
				adoptFrame(frame, trial)
				return true, ""
			}
		}
		return false, "no ~or alternative matched"
	case core.PatNot:
		trial := cloneFrame(frame)
		if ok, _ := match(pat.Sub, target, env, trial); ok {
			return false, "~not pattern unexpectedly matched"
		}
		return true, ""
	case core.PatDescribe:
		if ok, _ := match(pat.Sub, target, env, frame); ok {
			return true, ""
		}
		return false, pat.Message
	case core.PatFail:
		return false, pat.Message
	}
	return false, fmt.Sprintf("unsupported pattern %T", p)
}

// matchSeq matches an ordered-sequence pattern against a list/vector/tuple
// value or its syntax counterpart. A rest pattern (PatSeq.Tail) binds the
// remainder after the fixed leading elements, rebound as the same kind of
// sequence; for a list it also carries the (possibly improper) tail.
func matchSeq(pat core.PatSeq, target Value, env *Env, frame map[core.BindingID]Value) (bool, string) {
	switch pat.Kind {
	case core.SeqVector:
		terms, ok := asVecElems(target)
		if !ok {
			return false, "expected a vector"
		}
		if pat.Tail == nil {
			return matchSeqElems(pat.Elems, terms, env, frame)
		}
		return matchSeqRest(pat, terms, env, frame, func(rest []Value) Value { return rebuildVector(rest) })
	case core.SeqTuple:
		terms, ok := asTupleElems(target)
		if !ok {
			return false, "expected a tuple"
		}
		if pat.Tail == nil {
			return matchSeqElems(pat.Elems, terms, env, frame)
		}
		return matchSeqRest(pat, terms, env, frame, func(rest []Value) Value { return rebuildTuple(rest) })
	case core.SeqList:
		terms, tail, ok := asListElems(target)
		if !ok {
			return false, "expected a list"
		}
		if isNilLit(pat.Tail) {
			if ok, fail := matchSeqElems(pat.Elems, terms, env, frame); !ok {
				return false, fail
			}
			if !litEqual(core.Nil{}, tail) {
				return false, "unexpected improper list tail"
			}
			return true, ""
		}
		return matchSeqRest(pat, terms, env, frame, func(rest []Value) Value { return rebuildList(rest, tail) })
	}
	return false, "unknown sequence kind"
}

// matchSeqRest matches a rest pattern: the fixed leading Elems against the first
// terms, then PatSeq.Tail against the remainder built by `build`.
func matchSeqRest(pat core.PatSeq, terms []Value, env *Env, frame map[core.BindingID]Value, build func([]Value) Value) (bool, string) {
	if len(terms) < len(pat.Elems) {
		return false, "too few elements for rest pattern"
	}
	for i, e := range pat.Elems {
		if ok, fail := match(e.Sub, terms[i], env, frame); !ok {
			return false, fail
		}
	}
	return match(pat.Tail, build(terms[len(pat.Elems):]), env, frame)
}

// matchSeqElems matches pattern elements against the full term slice (vectors,
// tuples, and proper lists consume every term), with backtracking for
// ellipsis, ~optional, and ~seq groups.
func matchSeqElems(elems []core.PatSeqElem, terms []Value, env *Env, frame map[core.BindingID]Value) (bool, string) {
	return matchSeqAt(elems, 0, terms, 0, env, frame)
}

func matchSeqAt(elems []core.PatSeqElem, ei int, terms []Value, ti int, env *Env, frame map[core.BindingID]Value) (bool, string) {
	if ei == len(elems) {
		if ti == len(terms) {
			return true, ""
		}
		return false, "too many elements in sequence"
	}
	elem := elems[ei]
	switch elem.Rep {
	case core.RepEllipsis:
		return matchEllipsis(elems, ei, terms, ti, env, frame)
	case core.RepOptional:
		return matchOptional(elems, ei, terms, ti, env, frame)
	}
	width := groupWidth(elem.Sub)
	if ti+width > len(terms) {
		return false, "too few elements in sequence"
	}
	trial := cloneFrame(frame)
	if ok, fail := matchElem(elem.Sub, width, terms, ti, env, trial); !ok {
		return false, fail
	}
	if ok, fail := matchSeqAt(elems, ei+1, terms, ti+width, env, trial); ok {
		adoptFrame(frame, trial)
		return true, ""
	} else {
		return false, fail
	}
}

func matchEllipsis(elems []core.PatSeqElem, ei int, terms []Value, ti int, env *Env, frame map[core.BindingID]Value) (bool, string) {
	elem := elems[ei]
	width := groupWidth(elem.Sub)
	if width < 1 {
		return false, "ellipsis element has no width"
	}
	ids := patternVarIDs(elem.Sub)
	maxReps := (len(terms) - ti) / width
	for k := maxReps; k >= 0; k-- {
		trial := cloneFrame(frame)
		cols := map[core.BindingID][]*core.Syntax{}
		cur := ti
		ok := true
		for r := 0; r < k; r++ {
			local := map[core.BindingID]Value{}
			if matched, _ := matchElem(elem.Sub, width, terms, cur, env, local); !matched {
				ok = false
				break
			}
			cur += width
			for _, id := range ids {
				cols[id] = append(cols[id], asSyntax(local[id]))
			}
		}
		if !ok {
			continue
		}
		for _, id := range ids {
			trial[id] = &core.Syntax{Node: core.SyntaxVector(cols[id])}
		}
		if matched, _ := matchSeqAt(elems, ei+1, terms, cur, env, trial); matched {
			adoptFrame(frame, trial)
			return true, ""
		}
	}
	return false, "ellipsis sequence did not match"
}

func matchOptional(elems []core.PatSeqElem, ei int, terms []Value, ti int, env *Env, frame map[core.BindingID]Value) (bool, string) {
	elem := elems[ei]
	width := groupWidth(elem.Sub)
	if ti+width <= len(terms) {
		trial := cloneFrame(frame)
		if matched, _ := matchElem(elem.Sub, width, terms, ti, env, trial); matched {
			if ok, _ := matchSeqAt(elems, ei+1, terms, ti+width, env, trial); ok {
				adoptFrame(frame, trial)
				return true, ""
			}
		}
	}
	trial := cloneFrame(frame)
	for _, id := range patternVarIDs(elem.Sub) {
		trial[id] = &core.Syntax{Node: core.Nil{}}
	}
	if ok, fail := matchSeqAt(elems, ei+1, terms, ti, env, trial); ok {
		adoptFrame(frame, trial)
		return true, ""
	} else {
		return false, fail
	}
}

// matchElem matches a single sequence element (a width-1 sub-pattern or a fixed
// ~seq PatGroup of the given width) against terms[ti:ti+width].
func matchElem(sub core.Pattern, width int, terms []Value, ti int, env *Env, frame map[core.BindingID]Value) (bool, string) {
	if group, ok := sub.(core.PatGroup); ok {
		for j, ip := range group {
			if ok, fail := match(ip, terms[ti+j], env, frame); !ok {
				return false, fail
			}
		}
		return true, ""
	}
	return match(sub, terms[ti], env, frame)
}

// groupWidth is the fixed number of terms an element consumes: a ~seq group's
// length, otherwise one.
func groupWidth(sub core.Pattern) int {
	if group, ok := sub.(core.PatGroup); ok {
		return len(group)
	}
	return 1
}

func matchDict(pat core.PatDict, target Value, env *Env, frame map[core.BindingID]Value) (bool, string) {
	keys, vals, ok := asDictEntries(target)
	if !ok {
		return false, "expected a dict"
	}
	for _, pe := range pat {
		found := false
		for i, k := range keys {
			if !core.DatumEqual(k, pe.Key) {
				continue
			}
			if ok, fail := match(pe.Value, vals[i], env, frame); !ok {
				return false, fail
			}
			found = true
			break
		}
		if !found {
			return false, "dict missing required key"
		}
	}
	return true, ""
}

// patternVarIDs collects the (deduplicated) binding IDs a pattern captures,
// used to accumulate ellipsis sequences and to default ~optional attributes.
func patternVarIDs(p core.Pattern) []core.BindingID {
	var out []core.BindingID
	seen := map[core.BindingID]bool{}
	var walk func(core.Pattern)
	add := func(id core.BindingID) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	walk = func(p core.Pattern) {
		switch x := p.(type) {
		case core.PatVar:
			add(x.Ref.ID)
		case core.PatSeq:
			for _, e := range x.Elems {
				walk(e.Sub)
			}
			if x.Tail != nil {
				walk(x.Tail)
			}
		case core.PatGroup:
			for _, s := range x {
				walk(s)
			}
		case core.PatDict:
			for _, e := range x {
				walk(e.Value)
			}
		case core.PatAnd:
			for _, s := range x {
				walk(s)
			}
		case core.PatOr:
			for _, s := range x {
				walk(s)
			}
		case core.PatDescribe:
			walk(x.Sub)
		}
	}
	walk(p)
	return out
}

func isNilLit(p core.Pattern) bool {
	lit, ok := p.(core.PatLit)
	if !ok {
		return false
	}
	_, isNil := lit.Value.(core.Nil)
	return isNil
}

// asSyntax coerces a captured term to *core.Syntax for ellipsis accumulation.
// Ellipsis appears only in syntax patterns, so captures are always syntax; a
// non-syntax capture (an absent ~optional default) is wrapped.
func asSyntax(v Value) *core.Syntax {
	if s, ok := v.(*core.Syntax); ok {
		return s
	}
	if d, ok := v.(core.Datum); ok {
		return &core.Syntax{Node: d}
	}
	return &core.Syntax{Node: core.Nil{}}
}

func cloneFrame(f map[core.BindingID]Value) map[core.BindingID]Value {
	out := make(map[core.BindingID]Value, len(f))
	for k, v := range f {
		out[k] = v
	}
	return out
}

func adoptFrame(dst, src map[core.BindingID]Value) {
	for k, v := range src {
		dst[k] = v
	}
}

// litEqual compares a literal pattern datum against a target term, reducing a
// syntax target to its datum first.
func litEqual(pat core.Datum, target Value) bool {
	if t, ok := target.(*core.Syntax); ok {
		return core.DatumEqual(pat, core.SyntaxToDatum(t))
	}
	return datumEqual(pat, target)
}

// termEqual compares two captured terms (for non-linear patterns and pins):
// two syntax terms compare with scope sensitivity, otherwise structurally.
func termEqual(a, b Value) bool {
	as, aok := a.(*core.Syntax)
	bs, bok := b.(*core.Syntax)
	if aok && bok {
		return syntaxCaptureEqual(as, bs)
	}
	return datumEqual(a, b)
}

// asListElems flattens a list target into element terms plus its (possibly
// improper) tail term. Handles both runtime Pair/Nil and syntax SyntaxPair/Nil.
func asListElems(target Value) (elems []Value, tail Value, ok bool) {
	switch t := target.(type) {
	case core.Nil:
		return nil, core.Nil{}, true
	case core.Pair:
		ds, dtail := core.ListElems(t)
		return datumsToValues(ds), dtail, true
	case *core.Syntax:
		cur := t
		if lowered, ok := core.LowerReaderListSyntax(cur); ok {
			cur = lowered
		}
		for {
			if lowered, ok := core.LowerReaderListSyntax(cur); ok {
				cur = lowered
			}
			if cur == nil {
				return elems, &core.Syntax{Node: core.Nil{}}, true
			}
			p, isPair := cur.Node.(core.SyntaxPair)
			if !isPair {
				return elems, cur, true
			}
			elems = append(elems, p.Head)
			cur = p.Tail
		}
	}
	return nil, nil, false
}

func asVecElems(target Value) ([]Value, bool) {
	switch t := target.(type) {
	case core.Vector:
		return datumsToValues(t), true
	case *core.Syntax:
		if v, ok := t.Node.(core.SyntaxVector); ok {
			return syntaxToValues(v), true
		}
	}
	return nil, false
}

func asTupleElems(target Value) ([]Value, bool) {
	switch t := target.(type) {
	case core.Tuple:
		return datumsToValues(t), true
	case *core.Syntax:
		if v, ok := t.Node.(core.SyntaxTuple); ok {
			return syntaxToValues(v), true
		}
	}
	return nil, false
}

func asDictEntries(target Value) (keys []core.Datum, vals []Value, ok bool) {
	switch t := target.(type) {
	case core.Dict:
		for _, e := range t {
			keys = append(keys, e.Key)
			vals = append(vals, e.Value)
		}
		return keys, vals, true
	case *core.Syntax:
		if d, isDict := t.Node.(core.SyntaxDict); isDict {
			for _, e := range d {
				keys = append(keys, core.SyntaxToDatum(e.Key))
				vals = append(vals, e.Value)
			}
			return keys, vals, true
		}
	}
	return nil, nil, false
}

func datumsToValues[T core.Datum](in []T) []Value {
	out := make([]Value, len(in))
	for i, e := range in {
		out[i] = e
	}
	return out
}

func syntaxToValues(in []*core.Syntax) []Value {
	out := make([]Value, len(in))
	for i, e := range in {
		out[i] = e
	}
	return out
}

// rebuildList reconstructs a list value from remaining elements and a tail term
// for a rest pattern. Rest patterns are value-only, so elements are datums.
func rebuildList(elems []Value, tail Value) Value {
	cur, _ := tail.(core.Datum)
	if cur == nil {
		cur = core.Nil{}
	}
	for i := len(elems) - 1; i >= 0; i-- {
		head, _ := elems[i].(core.Datum)
		cur = core.Pair{Head: head, Tail: cur}
	}
	return cur
}

// rebuildVector / rebuildTuple rebind the remainder of a vector/tuple rest
// pattern as the same kind of sequence. Rest patterns are value-only, so the
// elements are datums.
func rebuildVector(elems []Value) Value {
	out := make(core.Vector, 0, len(elems))
	for _, e := range elems {
		if d, ok := e.(core.Datum); ok {
			out = append(out, d)
		}
	}
	return out
}

func rebuildTuple(elems []Value) Value {
	out := make(core.Tuple, 0, len(elems))
	for _, e := range elems {
		if d, ok := e.(core.Datum); ok {
			out = append(out, d)
		}
	}
	return out
}

// datumEqual compares two values for structural equality. Functions (now
// first-class data: *core.Closure, *core.Native) compare by identity through
// core.DatumEqual, like every other datum.
func datumEqual(a, b any) bool {
	ad, aok := a.(core.Datum)
	bd, bok := b.(core.Datum)
	return aok && bok && core.DatumEqual(ad, bd)
}
