package core

import (
	"fmt"
	"strings"

	"ish/internal/ast"
)

type ValueKind byte

const (
	VString ValueKind = iota
	VInt
	VAtom
	VTuple
	VList
	VMap
	VPid
	VFn
	VNil
)

type Value struct {
	Kind  ValueKind
	Str   string
	Int   int64
	Elems []Value
	Map   *OrdMap
	Pid   Pid
	Fn    *FnValue
}

type FnValue struct {
	Name    string
	Clauses []FnClause
	Env     *Env // closure environment (nil for non-closures)
}

type FnClause struct {
	Params []ast.Node
	Guard  *ast.Node
	Body   *ast.Node
}

// NativeFn is a Go function callable as an ish function.
type NativeFn func(args []Value, env *Env) (Value, error)

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

var Nil = Value{Kind: VNil}
var True = Value{Kind: VAtom, Str: "true"}
var False = Value{Kind: VAtom, Str: "false"}

func StringVal(s string) Value     { return Value{Kind: VString, Str: s} }
func IntVal(n int64) Value         { return Value{Kind: VInt, Int: n} }
func AtomVal(s string) Value       { return Value{Kind: VAtom, Str: s} }
func TupleVal(elems ...Value) Value { return Value{Kind: VTuple, Elems: elems} }
func ListVal(elems ...Value) Value  { return Value{Kind: VList, Elems: elems} }

func BoolVal(b bool) Value {
	if b {
		return True
	}
	return False
}

func (v Value) String() string {
	switch v.Kind {
	case VString:
		return v.Str
	case VInt:
		return fmt.Sprintf("%d", v.Int)
	case VAtom:
		return ":" + v.Str
	case VTuple:
		parts := make([]string, len(v.Elems))
		for i, e := range v.Elems {
			parts[i] = e.Inspect()
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case VList:
		parts := make([]string, len(v.Elems))
		for i, e := range v.Elems {
			parts[i] = e.Inspect()
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case VMap:
		if v.Map == nil {
			return "%{}"
		}
		parts := make([]string, len(v.Map.Keys))
		for i, k := range v.Map.Keys {
			parts[i] = k + ": " + v.Map.Vals[k].Inspect()
		}
		return "%{" + strings.Join(parts, ", ") + "}"
	case VPid:
		if v.Pid != nil {
			return fmt.Sprintf("#PID<%d>", v.Pid.ID())
		}
		return "#PID<nil>"
	case VFn:
		if v.Fn != nil {
			return fmt.Sprintf("#Function<%s/%d>", v.Fn.Name, len(v.Fn.Clauses))
		}
		return "#Function<>"
	case VNil:
		return "nil"
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
		return v.Int != 0
	default:
		return true
	}
}

// Equal checks structural equality.
func (v Value) Equal(other Value) bool {
	if v.Kind != other.Kind {
		return false
	}
	switch v.Kind {
	case VString, VAtom:
		return v.Str == other.Str
	case VInt:
		return v.Int == other.Int
	case VNil:
		return true
	case VTuple, VList:
		if len(v.Elems) != len(other.Elems) {
			return false
		}
		for i := range v.Elems {
			if !v.Elems[i].Equal(other.Elems[i]) {
				return false
			}
		}
		return true
	case VMap:
		if v.Map == nil && other.Map == nil {
			return true
		}
		if v.Map == nil || other.Map == nil {
			return false
		}
		if len(v.Map.Keys) != len(other.Map.Keys) {
			return false
		}
		for _, k := range v.Map.Keys {
			ov, ok := other.Map.Get(k)
			if !ok || !v.Map.Vals[k].Equal(ov) {
				return false
			}
		}
		return true
	case VPid:
		if v.Pid == nil && other.Pid == nil {
			return true
		}
		if v.Pid == nil || other.Pid == nil {
			return false
		}
		return v.Pid.ID() == other.Pid.ID()
	case VFn:
		return v.Fn == other.Fn
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
