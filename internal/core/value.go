package core

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"ish/internal/ast"
)

type ValueKind byte

const (
	VString ValueKind = iota
	VInt
	VFloat
	VAtom
	VTuple
	VList
	VMap
	VPid
	VFn
	VNil
	VTailCall
)

// Compound holds fields for non-scalar value kinds (tuple, list, map, pid, fn, tailcall).
// Scalar kinds (string, int, float, atom, nil) leave extra nil.
type Compound struct {
	Elems    []Value
	Map      *OrdMap
	Pid      Pid
	Fn       *FnValue
	TailFn   *FnValue
	TailArgs []Value
}

// Value is a 40-byte tagged union. Scalar kinds (string, int, float, atom, nil)
// use only Kind+Str+num with no heap allocation. Compound kinds (tuple, list, map,
// pid, fn, tailcall) allocate a Compound struct on the heap via extra.
type Value struct {
	Kind  ValueKind
	Str   string   // VString, VAtom: the string data
	num   int64    // VInt: the int64 value; VFloat: Float64bits
	extra *Compound // nil for scalar kinds
}

type FnValue struct {
	Name    string
	Clauses []FnClause
	Env     Scope    // closure environment (nil for non-closures)
	Native  NativeFn // non-nil for wrapped native functions
}

type FnClause struct {
	Params []ast.Node
	Guard  *ast.Node
	Body   *ast.Node
}

// NativeFn is a Go function callable as an ish function.
type NativeFn func(args []Value, scope Scope) (Value, error)

// Module is a named collection of functions.
type Module struct {
	Name      string
	Fns       map[string]*FnValue
	NativeFns map[string]NativeFn
}

// OrdMap is a simple ordered map.
type OrdMap struct {
	Keys []string
	Vals map[string]Value
}

func NewOrdMap() *OrdMap {
	return &OrdMap{Vals: make(map[string]Value)}
}

func (m *OrdMap) Set(k string, v Value) {
	if _, ok := m.Vals[k]; !ok {
		m.Keys = append(m.Keys, k)
	}
	m.Vals[k] = v
}

func (m *OrdMap) Get(k string) (Value, bool) {
	v, ok := m.Vals[k]
	return v, ok
}

// ---------------------------------------------------------------------------
// Constructors
// ---------------------------------------------------------------------------

var Nil = Value{Kind: VNil}
var True = Value{Kind: VAtom, Str: "true"}
var False = Value{Kind: VAtom, Str: "false"}

func StringVal(s string) Value { return Value{Kind: VString, Str: s} }
func IntVal(n int64) Value     { return Value{Kind: VInt, num: n} }
func FloatVal(f float64) Value { return Value{Kind: VFloat, num: int64(math.Float64bits(f))} }
func AtomVal(s string) Value   { return Value{Kind: VAtom, Str: s} }

func TupleVal(elems ...Value) Value {
	return Value{Kind: VTuple, extra: &Compound{Elems: elems}}
}

func ListVal(elems ...Value) Value {
	return Value{Kind: VList, extra: &Compound{Elems: elems}}
}

func TailCallVal(fn *FnValue, args []Value) Value {
	return Value{Kind: VTailCall, extra: &Compound{TailFn: fn, TailArgs: args}}
}

func BoolVal(b bool) Value {
	if b {
		return True
	}
	return False
}

func PidVal(p Pid) Value {
	return Value{Kind: VPid, extra: &Compound{Pid: p}}
}

func FnVal(fn *FnValue) Value {
	return Value{Kind: VFn, extra: &Compound{Fn: fn}}
}

func MapVal(m *OrdMap) Value {
	return Value{Kind: VMap, extra: &Compound{Map: m}}
}

// ---------------------------------------------------------------------------
// Accessors for fields behind the compound pointer
// ---------------------------------------------------------------------------

func (v Value) GetInt() int64        { return v.num }
func (v Value) GetFloat() float64    { return math.Float64frombits(uint64(v.num)) }

func (v Value) GetElems() []Value {
	if v.extra == nil { return nil }
	return v.extra.Elems
}

func (v Value) GetMap() *OrdMap {
	if v.extra == nil { return nil }
	return v.extra.Map
}

func (v Value) GetPid() Pid {
	if v.extra == nil { return nil }
	return v.extra.Pid
}

func (v Value) GetFn() *FnValue {
	if v.extra == nil { return nil }
	return v.extra.Fn
}

func (v Value) GetTailFn() *FnValue {
	if v.extra == nil { return nil }
	return v.extra.TailFn
}

func (v Value) GetTailArgs() []Value {
	if v.extra == nil { return nil }
	return v.extra.TailArgs
}

// ---------------------------------------------------------------------------
// Display / conversion methods
// ---------------------------------------------------------------------------

func (v Value) String() string {
	switch v.Kind {
	case VString:
		return v.Str
	case VInt:
		return fmt.Sprintf("%d", v.num)
	case VFloat:
		s := fmt.Sprintf("%g", v.GetFloat())
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	case VAtom:
		return ":" + v.Str
	case VTuple:
		elems := v.GetElems()
		parts := make([]string, len(elems))
		for i, e := range elems {
			parts[i] = e.Inspect()
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case VList:
		elems := v.GetElems()
		parts := make([]string, len(elems))
		for i, e := range elems {
			parts[i] = e.Inspect()
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case VMap:
		m := v.GetMap()
		if m == nil {
			return "%{}"
		}
		parts := make([]string, len(m.Keys))
		for i, k := range m.Keys {
			parts[i] = k + ": " + m.Vals[k].Inspect()
		}
		return "%{" + strings.Join(parts, ", ") + "}"
	case VPid:
		p := v.GetPid()
		if p != nil {
			return fmt.Sprintf("#PID<%d>", p.ID())
		}
		return "#PID<nil>"
	case VFn:
		fn := v.GetFn()
		if fn != nil {
			return fmt.Sprintf("#Function<%s/%d>", fn.Name, len(fn.Clauses))
		}
		return "#Function<>"
	case VNil:
		return "nil"
	case VTailCall:
		return "<tailcall>"
	}
	return "?"
}

// Inspect returns a representation suitable for display (strings are quoted).
func (v Value) Inspect() string {
	if v.Kind == VString {
		return fmt.Sprintf("%q", v.Str)
	}
	return v.String()
}

// Truthy returns whether the value is considered truthy.
func (v Value) Truthy() bool {
	switch v.Kind {
	case VNil:
		return false
	case VAtom:
		return v.Str != "false" && v.Str != "nil"
	case VString:
		return v.Str != ""
	case VInt:
		return v.num != 0
	case VFloat:
		return v.GetFloat() != 0
	default:
		return true
	}
}

// Equal checks structural equality.
func (v Value) Equal(other Value) bool {
	// Cross-kind int/float comparison
	if v.Kind == VInt && other.Kind == VFloat {
		return float64(v.num) == other.GetFloat()
	}
	if v.Kind == VFloat && other.Kind == VInt {
		return v.GetFloat() == float64(other.num)
	}
	// Cross-kind int/string coercion
	if v.Kind == VInt && other.Kind == VString {
		if n, err := strconv.ParseInt(other.Str, 10, 64); err == nil {
			return v.num == n
		}
		return false
	}
	if v.Kind == VString && other.Kind == VInt {
		if n, err := strconv.ParseInt(v.Str, 10, 64); err == nil {
			return n == other.num
		}
		return false
	}
	if v.Kind != other.Kind {
		return false
	}
	switch v.Kind {
	case VString, VAtom:
		return v.Str == other.Str
	case VInt:
		return v.num == other.num
	case VFloat:
		return v.num == other.num // bitwise: same bits = same float
	case VNil:
		return true
	case VTuple, VList:
		ve, oe := v.GetElems(), other.GetElems()
		if len(ve) != len(oe) {
			return false
		}
		for i := range ve {
			if !ve[i].Equal(oe[i]) {
				return false
			}
		}
		return true
	case VMap:
		vm, om := v.GetMap(), other.GetMap()
		if vm == nil && om == nil {
			return true
		}
		if vm == nil || om == nil {
			return false
		}
		if len(vm.Keys) != len(om.Keys) {
			return false
		}
		for _, k := range vm.Keys {
			ov, ok := om.Get(k)
			if !ok || !vm.Vals[k].Equal(ov) {
				return false
			}
		}
		return true
	case VPid:
		vp, op := v.GetPid(), other.GetPid()
		if vp == nil && op == nil {
			return true
		}
		if vp == nil || op == nil {
			return false
		}
		return vp.ID() == op.ID()
	case VFn:
		return v.GetFn() == other.GetFn()
	}
	return false
}

// ToStr converts any value to a string for use as a command argument.
func (v Value) ToStr() string {
	if v.Kind == VString {
		return v.Str
	}
	return v.String()
}
