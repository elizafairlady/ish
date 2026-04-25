package value

import "testing"

func TestTruthy(t *testing.T) {
	tests := []struct {
		name string
		val  Value
		want bool
	}{
		// Falsy values
		{"nil", Nil, false},
		{"false atom", False, false},
		{"zero int", IntVal(0), false},
		{"zero float", FloatVal(0.0), false},
		{"empty string", StringVal(""), false},
		{"error tuple", TupleVal(AtomVal("error"), IntVal(1)), false},
		{"error tuple code 2", TupleVal(AtomVal("error"), IntVal(2)), false},

		// Truthy values
		{"true atom", True, true},
		{"nonzero int", IntVal(42), true},
		{"negative int", IntVal(-1), true},
		{"nonzero float", FloatVal(3.14), true},
		{"nonempty string", StringVal("hello"), true},
		{"ok tuple", TupleVal(AtomVal("ok"), Nil), true},
		{"ok with value", TupleVal(AtomVal("ok"), StringVal("data")), true},
		{"plain tuple", TupleVal(IntVal(1), IntVal(2)), true},
		{"nonempty list", ListVal(IntVal(1)), true},
		{"empty list", ListVal(), true}, // Elixir convention: empty list is truthy
		{"atom", AtomVal("anything"), true},
		{"ok atom alone", AtomVal("ok"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.val.Truthy(); got != tt.want {
				t.Errorf("(%s).Truthy() = %v, want %v", tt.val, got, tt.want)
			}
		})
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		val  Value
		want int
	}{
		{"truthy int", IntVal(42), 0},
		{"ok tuple", TupleVal(AtomVal("ok"), Nil), 0},
		{"true", True, 0},
		{"nonempty string", StringVal("hi"), 0},

		{"nil", Nil, 1},
		{"false", False, 1},
		{"zero", IntVal(0), 1},
		{"empty string", StringVal(""), 1},

		// {:error, code} extracts the code
		{"error 1", TupleVal(AtomVal("error"), IntVal(1)), 1},
		{"error 2", TupleVal(AtomVal("error"), IntVal(2)), 2},
		{"error 127", TupleVal(AtomVal("error"), IntVal(127)), 127},
		{"error 128+signal", TupleVal(AtomVal("error"), IntVal(130)), 130},

		// {:error, non-int} falls back to 1
		{"error string", TupleVal(AtomVal("error"), StringVal("oops")), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.val.ExitCode(); got != tt.want {
				t.Errorf("(%s).ExitCode() = %d, want %d", tt.val, got, tt.want)
			}
		})
	}
}

func TestEqual(t *testing.T) {
	tests := []struct {
		a, b Value
		want bool
	}{
		{IntVal(42), IntVal(42), true},
		{IntVal(42), IntVal(43), false},
		{StringVal("hi"), StringVal("hi"), true},
		{AtomVal("ok"), AtomVal("ok"), true},
		{AtomVal("ok"), AtomVal("error"), false},
		{Nil, Nil, true},
		{True, True, true},
		{True, False, false},
		{TupleVal(AtomVal("ok"), Nil), TupleVal(AtomVal("ok"), Nil), true},
		{TupleVal(AtomVal("ok"), Nil), TupleVal(AtomVal("error"), IntVal(1)), false},
		{ListVal(IntVal(1), IntVal(2)), ListVal(IntVal(1), IntVal(2)), true},
		{ListVal(IntVal(1)), ListVal(IntVal(2)), false},
	}
	for i, tt := range tests {
		if got := tt.a.Equal(tt.b); got != tt.want {
			t.Errorf("test %d: (%s).Equal(%s) = %v, want %v", i, tt.a, tt.b, got, tt.want)
		}
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		val  Value
		want string
	}{
		{Nil, "nil"},
		{True, ":true"},
		{False, ":false"},
		{IntVal(42), "42"},
		{FloatVal(3.14), "3.14"},
		{StringVal("hello"), "hello"},
		{AtomVal("ok"), ":ok"},
		{TupleVal(AtomVal("ok"), Nil), "{:ok, nil}"},
		{TupleVal(AtomVal("error"), IntVal(1)), "{:error, 1}"},
		{ListVal(IntVal(1), IntVal(2), IntVal(3)), "[1, 2, 3]"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.val.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOkValErrorVal(t *testing.T) {
	ok := OkVal(StringVal("data"))
	if !ok.Truthy() {
		t.Error("OkVal should be truthy")
	}
	if ok.ExitCode() != 0 {
		t.Errorf("OkVal exit code should be 0, got %d", ok.ExitCode())
	}

	err := ErrorVal(42)
	if err.Truthy() {
		t.Error("ErrorVal should be falsy")
	}
	if err.ExitCode() != 42 {
		t.Errorf("ErrorVal(42) exit code should be 42, got %d", err.ExitCode())
	}
}
