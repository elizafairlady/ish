package main

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// stdlib list functions
// ---------------------------------------------------------------------------

func TestStdlibHd(t *testing.T) {
	env := testEnv()
	script := `result = hd [1, 2, 3]`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(IntVal(1)) {
		t.Errorf("hd [1,2,3] = %s, want 1", got.Inspect())
	}
}

func TestStdlibHdEmpty(t *testing.T) {
	env := testEnv()
	script := `result = hd []`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err == nil {
		t.Fatal("expected error for hd on empty list")
	}
}

func TestStdlibTl(t *testing.T) {
	env := testEnv()
	script := `result = tl [1, 2, 3]`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	want := ListVal(IntVal(2), IntVal(3))
	if !got.Equal(want) {
		t.Errorf("tl [1,2,3] = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibTlEmpty(t *testing.T) {
	env := testEnv()
	script := `result = tl []`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err == nil {
		t.Fatal("expected error for tl on empty list")
	}
}

func TestStdlibLength(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   Value
	}{
		{"list", `length [1, 2, 3]`, IntVal(3)},
		{"empty list", `length []`, IntVal(0)},
		{"string", `length "hello"`, IntVal(5)},
		{"tuple", `length {1, 2}`, IntVal(2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			script := "result = " + tt.script
			tokens := Lex(script)
			node, err := Parse(tokens)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			_, err = Eval(node, env)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}
			got, _ := env.Get("result")
			if !got.Equal(tt.want) {
				t.Errorf("%s = %s, want %s", tt.script, got.Inspect(), tt.want.Inspect())
			}
		})
	}
}

func TestStdlibAppend(t *testing.T) {
	env := testEnv()
	script := `result = append [1, 2], 3`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	want := ListVal(IntVal(1), IntVal(2), IntVal(3))
	if !got.Equal(want) {
		t.Errorf("append [1,2], 3 = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibConcat(t *testing.T) {
	env := testEnv()
	script := `result = concat [1, 2], [3, 4]`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	want := ListVal(IntVal(1), IntVal(2), IntVal(3), IntVal(4))
	if !got.Equal(want) {
		t.Errorf("concat = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibMap(t *testing.T) {
	env := testEnv()
	script := `
f = fn do
  x -> x * 2
end
result = map [1, 2, 3], f
`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	want := ListVal(IntVal(2), IntVal(4), IntVal(6))
	if !got.Equal(want) {
		t.Errorf("map = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibFilter(t *testing.T) {
	env := testEnv()
	script := `
f = fn do
  x -> x >= 3
end
result = filter [1, 2, 3, 4], f
`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	want := ListVal(IntVal(3), IntVal(4))
	if !got.Equal(want) {
		t.Errorf("filter = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibReduce(t *testing.T) {
	env := testEnv()
	script := `
f = fn do
  acc, x -> acc + x
end
result = reduce [1, 2, 3, 4], 0, f
`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(IntVal(10)) {
		t.Errorf("reduce = %s, want 10", got.Inspect())
	}
}

func TestStdlibRange(t *testing.T) {
	env := testEnv()
	script := `result = range 0, 5`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	want := ListVal(IntVal(0), IntVal(1), IntVal(2), IntVal(3), IntVal(4))
	if !got.Equal(want) {
		t.Errorf("range 0,5 = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibRangeEmpty(t *testing.T) {
	env := testEnv()
	script := `result = range 5, 3`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	want := ListVal()
	if !got.Equal(want) {
		t.Errorf("range 5,3 = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibAt(t *testing.T) {
	env := testEnv()
	script := `result = at [10, 20, 30], 1`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(IntVal(20)) {
		t.Errorf("at [10,20,30], 1 = %s, want 20", got.Inspect())
	}
}

func TestStdlibAtOutOfBounds(t *testing.T) {
	env := testEnv()
	script := `result = at [10, 20], 5`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err == nil {
		t.Fatal("expected error for out-of-bounds index")
	}
}

// ---------------------------------------------------------------------------
// stdlib string functions
// ---------------------------------------------------------------------------

func TestStdlibSplit(t *testing.T) {
	env := testEnv()
	script := `result = split "a,b,c", ","`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	want := ListVal(StringVal("a"), StringVal("b"), StringVal("c"))
	if !got.Equal(want) {
		t.Errorf("split = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibJoin(t *testing.T) {
	env := testEnv()
	script := `result = join ["a", "b", "c"], "-"`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(StringVal("a-b-c")) {
		t.Errorf("join = %s, want \"a-b-c\"", got.Inspect())
	}
}

func TestStdlibTrim(t *testing.T) {
	env := testEnv()
	script := `result = trim "  hello  "`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(StringVal("hello")) {
		t.Errorf("trim = %s, want \"hello\"", got.Inspect())
	}
}

func TestStdlibUpcase(t *testing.T) {
	env := testEnv()
	script := `result = upcase "hello"`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(StringVal("HELLO")) {
		t.Errorf("upcase = %s, want \"HELLO\"", got.Inspect())
	}
}

func TestStdlibDowncase(t *testing.T) {
	env := testEnv()
	script := `result = downcase "HELLO"`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(StringVal("hello")) {
		t.Errorf("downcase = %s, want \"hello\"", got.Inspect())
	}
}

func TestStdlibReplaceStr(t *testing.T) {
	env := testEnv()
	script := `result = replace "hello world hello", "hello", "bye"`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(StringVal("bye world hello")) {
		t.Errorf("replace = %s, want \"bye world hello\"", got.Inspect())
	}
}

func TestStdlibReplaceAllStr(t *testing.T) {
	env := testEnv()
	script := `result = replace_all "hello world hello", "hello", "bye"`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(StringVal("bye world bye")) {
		t.Errorf("replace_all = %s, want \"bye world bye\"", got.Inspect())
	}
}

func TestStdlibStartsWith(t *testing.T) {
	env := testEnv()
	script := `result = starts_with "hello world", "hello"`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(AtomVal("true")) {
		t.Errorf("starts_with = %s, want :true", got.Inspect())
	}
}

func TestStdlibEndsWith(t *testing.T) {
	env := testEnv()
	script := `result = ends_with "hello world", "world"`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(AtomVal("true")) {
		t.Errorf("ends_with = %s, want :true", got.Inspect())
	}
}

func TestStdlibContains(t *testing.T) {
	env := testEnv()
	script := `result = contains "hello world", "lo wo"`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(AtomVal("true")) {
		t.Errorf("contains = %s, want :true", got.Inspect())
	}
}

func TestStdlibSubstring(t *testing.T) {
	env := testEnv()
	script := `result = substring "hello world", 6, 5`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(StringVal("world")) {
		t.Errorf("substring = %s, want \"world\"", got.Inspect())
	}
}

func TestStdlibIndexOf(t *testing.T) {
	env := testEnv()
	script := `result = index_of "hello world", "world"`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(IntVal(6)) {
		t.Errorf("index_of = %s, want 6", got.Inspect())
	}
}

func TestStdlibIndexOfNotFound(t *testing.T) {
	env := testEnv()
	script := `result = index_of "hello", "xyz"`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(IntVal(-1)) {
		t.Errorf("index_of = %s, want -1", got.Inspect())
	}
}

// ---------------------------------------------------------------------------
// stdlib with pipe arrow
// ---------------------------------------------------------------------------

func TestStdlibPipeArrow(t *testing.T) {
	env := testEnv()
	script := `result = [1, 2, 3] |> hd`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(IntVal(1)) {
		t.Errorf("[1,2,3] |> hd = %s, want 1", got.Inspect())
	}
}

// ---------------------------------------------------------------------------
// receive with after timeout
// ---------------------------------------------------------------------------

func TestReceiveAfterTimeoutFires(t *testing.T) {
	env := testEnv()
	script := `
result = receive do
  msg -> msg
after 50 ->
  :timeout
end
`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	start := time.Now()
	_, err = Eval(node, env)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(AtomVal("timeout")) {
		t.Errorf("receive after timeout = %s, want :timeout", got.Inspect())
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("timeout fired too fast: %v", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}

func TestReceiveAfterMessageArrivesBeforeTimeout(t *testing.T) {
	env := testEnv()

	// Send a message to our own process before the receive
	proc := env.getProc()
	go func() {
		time.Sleep(10 * time.Millisecond)
		proc.Send(IntVal(42))
	}()

	script := `
result = receive do
  msg -> msg
after 5000 ->
  :timeout
end
`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(IntVal(42)) {
		t.Errorf("receive = %s, want 42", got.Inspect())
	}
}

func TestReceiveAfterWithPatternMatch(t *testing.T) {
	env := testEnv()

	proc := env.getProc()
	go func() {
		time.Sleep(10 * time.Millisecond)
		proc.Send(TupleVal(AtomVal("ok"), IntVal(99)))
	}()

	script := `
result = receive do
  {:ok, val} -> val
  {:error, reason} -> reason
after 5000 ->
  :timeout
end
`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if !got.Equal(IntVal(99)) {
		t.Errorf("receive = %s, want 99", got.Inspect())
	}
}

// ---------------------------------------------------------------------------
// stdlib map operations
// ---------------------------------------------------------------------------

func TestStdlibPut(t *testing.T) {
	env := testEnv()
	script := `m = %{a: 1, b: 2}
result = put m, "c", 3`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if got.Kind != VMap {
		t.Fatalf("expected map, got %s", got.Inspect())
	}
	if v, ok := got.Map.Get("c"); !ok || !v.Equal(IntVal(3)) {
		t.Errorf("put: expected c=3, got %s", got.Inspect())
	}
	// original map should still have a and b
	if v, ok := got.Map.Get("a"); !ok || !v.Equal(IntVal(1)) {
		t.Errorf("put: expected a=1, got %s", got.Inspect())
	}
}

func TestStdlibDelete(t *testing.T) {
	env := testEnv()
	script := `m = %{a: 1, b: 2, c: 3}
result = delete m, "b"`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if got.Kind != VMap {
		t.Fatalf("expected map, got %s", got.Inspect())
	}
	if _, ok := got.Map.Get("b"); ok {
		t.Error("delete: expected b to be removed")
	}
	if len(got.Map.Keys) != 2 {
		t.Errorf("delete: expected 2 keys, got %d", len(got.Map.Keys))
	}
}

func TestStdlibMerge(t *testing.T) {
	env := testEnv()
	script := `m1 = %{a: 1, b: 2}
m2 = %{b: 99, c: 3}
result = merge m1, m2`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if got.Kind != VMap {
		t.Fatalf("expected map, got %s", got.Inspect())
	}
	// b should have m2's value (99)
	if v, ok := got.Map.Get("b"); !ok || !v.Equal(IntVal(99)) {
		t.Errorf("merge: expected b=99, got %s", got.Inspect())
	}
	if v, ok := got.Map.Get("a"); !ok || !v.Equal(IntVal(1)) {
		t.Errorf("merge: expected a=1, got %s", got.Inspect())
	}
	if v, ok := got.Map.Get("c"); !ok || !v.Equal(IntVal(3)) {
		t.Errorf("merge: expected c=3, got %s", got.Inspect())
	}
}

func TestStdlibKeys(t *testing.T) {
	env := testEnv()
	script := `m = %{x: 1, y: 2}
result = keys m`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if got.Kind != VList {
		t.Fatalf("expected list, got %s", got.Inspect())
	}
	if len(got.Elems) != 2 {
		t.Errorf("keys: expected 2 elements, got %d", len(got.Elems))
	}
}

func TestStdlibValues(t *testing.T) {
	env := testEnv()
	script := `m = %{x: 10, y: 20}
result = values m`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	got, _ := env.Get("result")
	if got.Kind != VList {
		t.Fatalf("expected list, got %s", got.Inspect())
	}
	if len(got.Elems) != 2 {
		t.Errorf("values: expected 2 elements, got %d", len(got.Elems))
	}
}

func TestStdlibHasKey(t *testing.T) {
	env := testEnv()
	script := `m = %{x: 1, y: 2}
r1 = has_key m, "x"
r2 = has_key m, "z"`
	tokens := Lex(script)
	node, err := Parse(tokens)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	r1, _ := env.Get("r1")
	r2, _ := env.Get("r2")
	if !r1.Equal(True) {
		t.Errorf("has_key m, x = %s, want :true", r1.Inspect())
	}
	if !r2.Equal(False) {
		t.Errorf("has_key m, z = %s, want :false", r2.Inspect())
	}
}

func TestStdlibRangeLimit(t *testing.T) {
	env := testEnv()
	_, err := stdlibRange([]Value{IntVal(0), IntVal(10_000_001)}, env)
	if err == nil {
		t.Fatal("expected error for oversized range")
	}
}
