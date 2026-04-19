package core

import (
	"testing"
)

func TestStringVal(t *testing.T) {
	v := StringVal("hello")
	if v.Kind != VString {
		t.Fatalf("expected VString, got %d", v.Kind)
	}
	if v.Str != "hello" {
		t.Fatalf("expected hello, got %s", v.Str)
	}
}

func TestIntVal(t *testing.T) {
	v := IntVal(42)
	if v.Kind != VInt {
		t.Fatalf("expected VInt, got %d", v.Kind)
	}
	if v.Int != 42 {
		t.Fatalf("expected 42, got %d", v.Int)
	}
}

func TestAtomVal(t *testing.T) {
	v := AtomVal("ok")
	if v.Kind != VAtom {
		t.Fatalf("expected VAtom, got %d", v.Kind)
	}
	if v.Str != "ok" {
		t.Fatalf("expected ok, got %s", v.Str)
	}
}

func TestTupleVal(t *testing.T) {
	v := TupleVal(IntVal(1), IntVal(2))
	if v.Kind != VTuple {
		t.Fatalf("expected VTuple, got %d", v.Kind)
	}
	if len(v.Elems) != 2 {
		t.Fatalf("expected 2 elems, got %d", len(v.Elems))
	}
}

func TestListVal(t *testing.T) {
	v := ListVal(StringVal("a"), StringVal("b"))
	if v.Kind != VList {
		t.Fatalf("expected VList, got %d", v.Kind)
	}
	if len(v.Elems) != 2 {
		t.Fatalf("expected 2 elems, got %d", len(v.Elems))
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		name string
		val  Value
		want string
	}{
		{"string", StringVal("hello"), "hello"},
		{"empty string", StringVal(""), ""},
		{"int", IntVal(42), "42"},
		{"int zero", IntVal(0), "0"},
		{"int negative", IntVal(-7), "-7"},
		{"atom", AtomVal("ok"), ":ok"},
		{"tuple", TupleVal(IntVal(1), StringVal("x")), `{1, "x"}`},
		{"empty tuple", TupleVal(), "{}"},
		{"list", ListVal(AtomVal("a"), AtomVal("b")), "[:a, :b]"},
		{"empty list", ListVal(), "[]"},
		{"nil", Nil, "nil"},
		{"true", True, ":true"},
		{"false", False, ":false"},
		{"map nil", Value{Kind: VMap, Map: nil}, "%{}"},
		{"pid nil", Value{Kind: VPid, Pid: nil}, "#PID<nil>"},
		{"fn nil", Value{Kind: VFn, Fn: nil}, "#Function<>"},
		{"fn with name", Value{Kind: VFn, Fn: &FnValue{Name: "add", Clauses: []FnClause{{}, {}}}}, "#Function<add/2>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.val.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStringMap(t *testing.T) {
	m := NewOrdMap()
	m.Set("name", StringVal("ish"))
	m.Set("ver", IntVal(1))
	v := Value{Kind: VMap, Map: m}
	got := v.String()
	want := `%{name: "ish", ver: 1}`
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestStringPid is in core_external_test.go (needs process import)

func TestInspect(t *testing.T) {
	tests := []struct {
		name string
		val  Value
		want string
	}{
		{"string is quoted", StringVal("hello"), `"hello"`},
		{"empty string is quoted", StringVal(""), `""`},
		{"int not quoted", IntVal(5), "5"},
		{"atom not quoted", AtomVal("ok"), ":ok"},
		{"nil", Nil, "nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.val.Inspect()
			if got != tt.want {
				t.Errorf("Inspect() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruthy(t *testing.T) {
	tests := []struct {
		name string
		val  Value
		want bool
	}{
		{"nil is false", Nil, false},
		{"atom false is false", AtomVal("false"), false},
		{"atom nil is false", AtomVal("nil"), false},
		{"empty string is false", StringVal(""), false},
		{"int 0 is false", IntVal(0), false},
		{"atom true is true", AtomVal("true"), true},
		{"atom ok is true", AtomVal("ok"), true},
		{"non-empty string is true", StringVal("hi"), true},
		{"int 1 is true", IntVal(1), true},
		{"int -1 is true", IntVal(-1), true},
		{"empty tuple is true", TupleVal(), true},
		{"empty list is true", ListVal(), true},
		{"map is true", Value{Kind: VMap, Map: NewOrdMap()}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.val.Truthy()
			if got != tt.want {
				t.Errorf("Truthy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b Value
		want bool
	}{
		{"same string", StringVal("x"), StringVal("x"), true},
		{"diff string", StringVal("x"), StringVal("y"), false},
		{"same int", IntVal(3), IntVal(3), true},
		{"diff int", IntVal(3), IntVal(4), false},
		{"same atom", AtomVal("ok"), AtomVal("ok"), true},
		{"diff atom", AtomVal("ok"), AtomVal("err"), false},
		{"nil nil", Nil, Nil, true},
		{"diff kind string int", StringVal("3"), IntVal(3), false},
		{"diff kind atom string", AtomVal("x"), StringVal("x"), false},
		{"same tuple", TupleVal(IntVal(1), AtomVal("ok")), TupleVal(IntVal(1), AtomVal("ok")), true},
		{"diff tuple elems", TupleVal(IntVal(1)), TupleVal(IntVal(2)), false},
		{"diff tuple len", TupleVal(IntVal(1)), TupleVal(IntVal(1), IntVal(2)), false},
		{"same list", ListVal(StringVal("a")), ListVal(StringVal("a")), true},
		{"diff list", ListVal(StringVal("a")), ListVal(StringVal("b")), false},
		{"nested tuple", TupleVal(TupleVal(IntVal(1))), TupleVal(TupleVal(IntVal(1))), true},
		{"nested list", ListVal(ListVal(IntVal(1), IntVal(2))), ListVal(ListVal(IntVal(1), IntVal(2))), true},
		{"nested diff", ListVal(ListVal(IntVal(1))), ListVal(ListVal(IntVal(2))), false},
		{"map both nil", Value{Kind: VMap, Map: nil}, Value{Kind: VMap, Map: nil}, true},
		{"map one nil", Value{Kind: VMap, Map: NewOrdMap()}, Value{Kind: VMap, Map: nil}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Equal(tt.b)
			if got != tt.want {
				t.Errorf("Equal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEqualMaps(t *testing.T) {
	m1 := NewOrdMap()
	m1.Set("a", IntVal(1))
	m1.Set("b", IntVal(2))

	m2 := NewOrdMap()
	m2.Set("a", IntVal(1))
	m2.Set("b", IntVal(2))

	v1 := Value{Kind: VMap, Map: m1}
	v2 := Value{Kind: VMap, Map: m2}
	if !v1.Equal(v2) {
		t.Error("expected equal maps to be equal")
	}

	m3 := NewOrdMap()
	m3.Set("a", IntVal(1))
	m3.Set("b", IntVal(99))
	v3 := Value{Kind: VMap, Map: m3}
	if v1.Equal(v3) {
		t.Error("expected maps with different values to not be equal")
	}

	m4 := NewOrdMap()
	m4.Set("a", IntVal(1))
	v4 := Value{Kind: VMap, Map: m4}
	if v1.Equal(v4) {
		t.Error("expected maps with different lengths to not be equal")
	}
}

func TestToStr(t *testing.T) {
	tests := []struct {
		name string
		val  Value
		want string
	}{
		{"string returns Str directly", StringVal("hello"), "hello"},
		{"int uses String()", IntVal(42), "42"},
		{"atom uses String()", AtomVal("ok"), ":ok"},
		{"nil uses String()", Nil, "nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.val.ToStr()
			if got != tt.want {
				t.Errorf("ToStr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOrdMap(t *testing.T) {
	t.Run("new map is empty", func(t *testing.T) {
		m := NewOrdMap()
		if len(m.Keys) != 0 {
			t.Errorf("expected 0 keys, got %d", len(m.Keys))
		}
	})

	t.Run("set and get", func(t *testing.T) {
		m := NewOrdMap()
		m.Set("x", IntVal(10))
		v, ok := m.Get("x")
		if !ok {
			t.Fatal("expected key x to exist")
		}
		if v.Int != 10 {
			t.Errorf("expected 10, got %d", v.Int)
		}
	})

	t.Run("overwrite preserves key order", func(t *testing.T) {
		m := NewOrdMap()
		m.Set("a", IntVal(1))
		m.Set("b", IntVal(2))
		m.Set("a", IntVal(99))
		if len(m.Keys) != 2 {
			t.Errorf("expected 2 keys after overwrite, got %d", len(m.Keys))
		}
		v, _ := m.Get("a")
		if v.Int != 99 {
			t.Errorf("expected 99 after overwrite, got %d", v.Int)
		}
		if m.Keys[0] != "a" || m.Keys[1] != "b" {
			t.Errorf("expected key order [a, b], got %v", m.Keys)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		m := NewOrdMap()
		_, ok := m.Get("nope")
		if ok {
			t.Error("expected missing key to return false")
		}
	})
}

// TestEqualPid is in core_external_test.go (needs process import)

func TestEqualFn(t *testing.T) {
	fn1 := &FnValue{Name: "add"}
	fn2 := &FnValue{Name: "add"}

	v1 := Value{Kind: VFn, Fn: fn1}
	v2 := Value{Kind: VFn, Fn: fn1}
	v3 := Value{Kind: VFn, Fn: fn2}

	if !v1.Equal(v2) {
		t.Error("same fn pointer should be equal")
	}
	if v1.Equal(v3) {
		t.Error("different fn pointers should not be equal")
	}
}
