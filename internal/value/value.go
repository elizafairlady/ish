package value

import (
	"fmt"
	"math"
	"strings"
)

type Kind byte

// Bit layout for fast dispatch:
//   bit 0: numeric (int or float)
//   bit 1: integer (implies bit 0)
//   bit 2: enumerable (list, tuple, map)
//   bit 3: string-like (string, atom)
//
// Fast checks via bitwise AND:
//   Both int:       (a.Kind & b.Kind & KindInt) != 0
//   Both numeric:   (a.Kind & b.Kind & KindNumeric) != 0
//   Is enumerable:  (a.Kind & KindEnum) != 0
//   Is string-like: (a.Kind & KindStr) != 0
const (
	KindNumeric Kind = 1 << 0
	KindInt     Kind = 1 << 1
	KindEnum    Kind = 1 << 2
	KindStr     Kind = 1 << 3

	VNil      Kind = 0
	VInt      Kind = KindNumeric | KindInt          // 3
	VFloat    Kind = KindNumeric                     // 1
	VList     Kind = KindEnum                        // 4
	VString   Kind = KindStr                         // 8
	VTuple    Kind = KindEnum | 16                   // 20
	VAtom     Kind = KindStr | 16                    // 24
	VMap      Kind = KindEnum | 32                   // 36
	VFn       Kind = 64
	VPid      Kind = 128
	VTailCall Kind = 192
)

type Value struct {
	Kind  Kind
	str   string
	num   int64
	extra *compound
}

type compound struct {
	Elems    []Value
	Map      *OrdMap
	Fn       *FnDef
	Pid      int64
	TailFn   *FnDef
	TailArgs []Value
	TailEnv  interface{}
}

type FnClause struct {
	Params   []string
	Patterns []interface{} // *ast.Node patterns for multi-clause dispatch
	Guard    interface{}   // *ast.Node guard expression
	Body     interface{}   // *ast.Node
}

type FnDef struct {
	Name    string
	Params  []string
	Body    interface{}
	Env     interface{}
	Native  func(args []Value) (Value, error)
	Clauses []FnClause
}

type OrdMap struct {
	Keys []string
	Vals map[string]Value
}

// --- Constructors ---

var Nil = Value{Kind: VNil}
var True = Value{Kind: VAtom, str: "true"}
var False = Value{Kind: VAtom, str: "false"}

func IntVal(n int64) Value     { return Value{Kind: VInt, num: n} }
func FloatVal(f float64) Value { return Value{Kind: VFloat, num: int64(math.Float64bits(f))} }
func StringVal(s string) Value { return Value{Kind: VString, str: s} }
func AtomVal(s string) Value   { return Value{Kind: VAtom, str: s} }

func BoolVal(b bool) Value {
	if b {
		return True
	}
	return False
}

func TupleVal(elems ...Value) Value {
	return Value{Kind: VTuple, extra: &compound{Elems: elems}}
}

func ListVal(elems ...Value) Value {
	return Value{Kind: VList, extra: &compound{Elems: elems}}
}

func MapVal(m *OrdMap) Value {
	return Value{Kind: VMap, extra: &compound{Map: m}}
}

func FnVal(fn *FnDef) Value {
	return Value{Kind: VFn, extra: &compound{Fn: fn}}
}

func PidVal(id int64) Value {
	return Value{Kind: VPid, extra: &compound{Pid: id}}
}

func TailCallVal(fn *FnDef, args []Value, env interface{}) Value {
	return Value{Kind: VTailCall, extra: &compound{TailFn: fn, TailArgs: args, TailEnv: env}}
}

func (v Value) GetTailFn() *FnDef    { return v.extra.TailFn }
func (v Value) GetTailArgs() []Value  { return v.extra.TailArgs }
func (v Value) GetTailEnv() interface{} { return v.extra.TailEnv }

func (v Value) Pid() int64 {
	if v.extra == nil {
		return 0
	}
	return v.extra.Pid
}

// OkVal creates {:ok, val}.
func OkVal(v Value) Value { return TupleVal(AtomVal("ok"), v) }

// ErrorVal creates {:error, code}.
func ErrorVal(code int) Value { return TupleVal(AtomVal("error"), IntVal(int64(code))) }

// --- Accessors ---

func (v Value) Int() int64     { return v.num }
func (v Value) Float() float64 { return math.Float64frombits(uint64(v.num)) }
func (v Value) Str() string    { return v.str }

func (v Value) Elems() []Value {
	if v.extra == nil {
		return nil
	}
	return v.extra.Elems
}

func (v Value) Map() *OrdMap {
	if v.extra == nil {
		return nil
	}
	return v.extra.Map
}

func (v Value) Fn() *FnDef {
	if v.extra == nil {
		return nil
	}
	return v.extra.Fn
}

// --- Core projections ---

// Truthy returns the truthiness of a value.
// This is the foundational projection: value -> bool -> exit code.
//
// Falsy: nil, :false, :nil, 0, 0.0, "", {:error, _}
// Truthy: everything else
func (v Value) Truthy() bool {
	switch v.Kind {
	case VNil:
		return false
	case VAtom:
		return v.str != "false" && v.str != "nil"
	case VInt:
		return v.num != 0
	case VFloat:
		return v.Float() != 0
	case VString:
		return v.str != ""
	case VTuple:
		elems := v.Elems()
		if len(elems) > 0 && elems[0].Kind == VAtom && elems[0].str == "error" {
			return false
		}
		return true
	default:
		return true
	}
}

// ExitCode projects a value to a process exit code.
// Truthy values -> 0. {:error, int} -> that int. Other falsy -> 1.
func (v Value) ExitCode() int {
	if v.Truthy() {
		return 0
	}
	if v.Kind == VTuple {
		elems := v.Elems()
		if len(elems) == 2 && elems[0].Kind == VAtom && elems[0].str == "error" {
			if elems[1].Kind == VInt {
				return int(elems[1].num)
			}
		}
	}
	return 1
}

// --- Structural equality ---

func (v Value) Equal(other Value) bool {
	if v.Kind != other.Kind {
		if v.Kind == VInt && other.Kind == VFloat {
			return float64(v.num) == other.Float()
		}
		if v.Kind == VFloat && other.Kind == VInt {
			return v.Float() == float64(other.num)
		}
		return false
	}
	switch v.Kind {
	case VNil:
		return true
	case VInt:
		return v.num == other.num
	case VFloat:
		return v.num == other.num
	case VString, VAtom:
		return v.str == other.str
	case VTuple, VList:
		ve, oe := v.Elems(), other.Elems()
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
		vm, om := v.Map(), other.Map()
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
			ov, ok := om.Vals[k]
			if !ok || !vm.Vals[k].Equal(ov) {
				return false
			}
		}
		return true
	case VFn:
		return v.Fn() == other.Fn()
	}
	return false
}

// --- Display ---

func (v Value) String() string {
	switch v.Kind {
	case VNil:
		return "nil"
	case VInt:
		return fmt.Sprintf("%d", v.num)
	case VFloat:
		s := fmt.Sprintf("%g", v.Float())
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	case VString:
		return v.str
	case VAtom:
		return ":" + v.str
	case VTuple:
		return "{" + joinValues(v.Elems()) + "}"
	case VList:
		return "[" + joinValues(v.Elems()) + "]"
	case VMap:
		m := v.Map()
		if m == nil {
			return "%{}"
		}
		parts := make([]string, len(m.Keys))
		for i, k := range m.Keys {
			parts[i] = k + ": " + m.Vals[k].Inspect()
		}
		return "%{" + strings.Join(parts, ", ") + "}"
	case VFn:
		fn := v.Fn()
		if fn != nil {
			return fmt.Sprintf("#Function<%s>", fn.Name)
		}
		return "#Function<>"
	case VPid:
		return fmt.Sprintf("#PID<%d>", v.Pid())
	}
	return "?"
}

func (v Value) Inspect() string {
	if v.Kind == VString {
		return fmt.Sprintf("%q", v.str)
	}
	return v.String()
}

func joinValues(elems []Value) string {
	parts := make([]string, len(elems))
	for i, e := range elems {
		parts[i] = e.Inspect()
	}
	return strings.Join(parts, ", ")
}

// ToStr converts any value to a string for command argument use.
func (v Value) ToStr() string {
	if v.Kind == VString {
		return v.str
	}
	return v.String()
}
