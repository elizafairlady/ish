package stdlib_test

import (
	"testing"
	"time"

	"ish/internal/core"
	"ish/internal/eval"
	"ish/internal/lexer"
	"ish/internal/parser"
	"ish/internal/testutil"
)

func evalScript(t *testing.T, env *core.Env, script string) {
	t.Helper()
	node, err := parser.Parse(lexer.New(script))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = eval.Eval(node, env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
}

func evalScriptErr(t *testing.T, env *core.Env, script string) error {
	t.Helper()
	node, err := parser.Parse(lexer.New(script))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_, err = eval.Eval(node, env)
	return err
}

// ---------------------------------------------------------------------------
// stdlib list functions
// ---------------------------------------------------------------------------

func TestStdlibHd(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Kernel.hd [1, 2, 3]`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(1)) {
		t.Errorf("Kernel.hd [1,2,3] = %s, want 1", got.Inspect())
	}
}

func TestStdlibHdEmpty(t *testing.T) {
	env := testutil.TestEnv()
	err := evalScriptErr(t, env, `result = Kernel.hd []`)
	if err == nil {
		t.Fatal("expected error for Kernel.hd on empty list")
	}
}

func TestStdlibTl(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Kernel.tl [1, 2, 3]`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(2), core.IntVal(3))
	if !got.Equal(want) {
		t.Errorf("Kernel.tl [1,2,3] = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibTlEmpty(t *testing.T) {
	env := testutil.TestEnv()
	err := evalScriptErr(t, env, `result = Kernel.tl []`)
	if err == nil {
		t.Fatal("expected error for Kernel.tl on empty list")
	}
}

func TestStdlibLength(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   core.Value
	}{
		{"list", `Kernel.length [1, 2, 3]`, core.IntVal(3)},
		{"empty list", `Kernel.length []`, core.IntVal(0)},
		{"string", `Kernel.length "hello"`, core.IntVal(5)},
		{"tuple", `Kernel.length {1, 2}`, core.IntVal(2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testutil.TestEnv()
			evalScript(t, env, "result = "+tt.script)
			got, _ := env.Get("result")
			if !got.Equal(tt.want) {
				t.Errorf("%s = %s, want %s", tt.script, got.Inspect(), tt.want.Inspect())
			}
		})
	}
}

func TestStdlibAppend(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = List.append([1, 2], 3)`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(1), core.IntVal(2), core.IntVal(3))
	if !got.Equal(want) {
		t.Errorf("List.append = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibConcat(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = List.concat([1, 2], [3, 4])`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(1), core.IntVal(2), core.IntVal(3), core.IntVal(4))
	if !got.Equal(want) {
		t.Errorf("List.concat = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibMap(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
f = fn do
  x -> x * 2
end
result = [1, 2, 3] |> List.map f
`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(2), core.IntVal(4), core.IntVal(6))
	if !got.Equal(want) {
		t.Errorf("List.map = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibFilter(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
f = fn do
  x -> x >= 3
end
result = [1, 2, 3, 4] |> List.filter f
`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(3), core.IntVal(4))
	if !got.Equal(want) {
		t.Errorf("List.filter = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibReduce(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
f = fn do
  acc, x -> acc + x
end
result = [1, 2, 3, 4] |> List.reduce(0, f)
`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(10)) {
		t.Errorf("List.reduce = %s, want 10", got.Inspect())
	}
}

func TestStdlibRange(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = List.range(0, 5)`)
	got, _ := env.Get("result")
	want := core.ListVal(core.IntVal(0), core.IntVal(1), core.IntVal(2), core.IntVal(3), core.IntVal(4))
	if !got.Equal(want) {
		t.Errorf("List.range = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibRangeEmpty(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = List.range(5, 3)`)
	got, _ := env.Get("result")
	want := core.ListVal()
	if !got.Equal(want) {
		t.Errorf("List.range = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibAt(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = List.at([10, 20, 30], 1)`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(20)) {
		t.Errorf("List.at = %s, want 20", got.Inspect())
	}
}

func TestStdlibAtOutOfBounds(t *testing.T) {
	env := testutil.TestEnv()
	err := evalScriptErr(t, env, `result = List.at([10, 20], 5)`)
	if err == nil {
		t.Fatal("expected error for out-of-bounds index")
	}
}

// ---------------------------------------------------------------------------
// stdlib string functions
// ---------------------------------------------------------------------------

func TestStdlibSplit(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.split("a,b,c", ",")`)
	got, _ := env.Get("result")
	want := core.ListVal(core.StringVal("a"), core.StringVal("b"), core.StringVal("c"))
	if !got.Equal(want) {
		t.Errorf("String.split = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStdlibJoin(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.join(["a", "b", "c"], "-")`)
	got, _ := env.Get("result")
	if !got.Equal(core.StringVal("a-b-c")) {
		t.Errorf("String.join = %s, want \"a-b-c\"", got.Inspect())
	}
}

func TestStdlibTrim(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.trim "  hello  "`)
	got, _ := env.Get("result")
	if !got.Equal(core.StringVal("hello")) {
		t.Errorf("String.trim = %s, want \"hello\"", got.Inspect())
	}
}

func TestStdlibUpcase(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.upcase "hello"`)
	got, _ := env.Get("result")
	if !got.Equal(core.StringVal("HELLO")) {
		t.Errorf("String.upcase = %s, want \"HELLO\"", got.Inspect())
	}
}

func TestStdlibDowncase(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.downcase "HELLO"`)
	got, _ := env.Get("result")
	if !got.Equal(core.StringVal("hello")) {
		t.Errorf("String.downcase = %s, want \"hello\"", got.Inspect())
	}
}

func TestStdlibReplaceStr(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.replace("hello world hello", "hello", "bye")`)
	got, _ := env.Get("result")
	if !got.Equal(core.StringVal("bye world hello")) {
		t.Errorf("String.replace = %s, want \"bye world hello\"", got.Inspect())
	}
}

func TestStdlibReplaceAllStr(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.replace_all("hello world hello", "hello", "bye")`)
	got, _ := env.Get("result")
	if !got.Equal(core.StringVal("bye world bye")) {
		t.Errorf("String.replace_all = %s, want \"bye world bye\"", got.Inspect())
	}
}

func TestStdlibStartsWith(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.starts_with("hello world", "hello")`)
	got, _ := env.Get("result")
	if !got.Equal(core.AtomVal("true")) {
		t.Errorf("String.starts_with = %s, want :true", got.Inspect())
	}
}

func TestStdlibEndsWith(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.ends_with("hello world", "world")`)
	got, _ := env.Get("result")
	if !got.Equal(core.AtomVal("true")) {
		t.Errorf("String.ends_with = %s, want :true", got.Inspect())
	}
}

func TestStdlibContains(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.contains("hello world", "lo wo")`)
	got, _ := env.Get("result")
	if !got.Equal(core.AtomVal("true")) {
		t.Errorf("String.contains = %s, want :true", got.Inspect())
	}
}

func TestStdlibSlice(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.slice("hello world", 6, 5)`)
	got, _ := env.Get("result")
	if !got.Equal(core.StringVal("world")) {
		t.Errorf("String.slice = %s, want \"world\"", got.Inspect())
	}
}

func TestStdlibIndexOf(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.index_of("hello world", "world")`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(6)) {
		t.Errorf("String.index_of = %s, want 6", got.Inspect())
	}
}

func TestStdlibIndexOfNotFound(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.index_of("hello", "xyz")`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(-1)) {
		t.Errorf("String.index_of = %s, want -1", got.Inspect())
	}
}

// ---------------------------------------------------------------------------
// stdlib with pipe arrow
// ---------------------------------------------------------------------------

func TestStdlibPipeArrow(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = [1, 2, 3] |> Kernel.hd`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(1)) {
		t.Errorf("[1,2,3] |> Kernel.hd = %s, want 1", got.Inspect())
	}
}

// ---------------------------------------------------------------------------
// receive with after timeout
// ---------------------------------------------------------------------------

func TestReceiveAfterTimeoutFires(t *testing.T) {
	env := testutil.TestEnv()
	script := `
result = receive do
  msg -> msg
after 50 ->
  :timeout
end
`
	start := time.Now()
	evalScript(t, env, script)
	elapsed := time.Since(start)
	got, _ := env.Get("result")
	if !got.Equal(core.AtomVal("timeout")) {
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
	env := testutil.TestEnv()

	proc := env.Ctx.Proc; //()
	go func() {
		time.Sleep(10 * time.Millisecond)
		proc.Send(core.IntVal(42))
	}()

	evalScript(t, env, `
result = receive do
  msg -> msg
after 5000 ->
  :timeout
end
`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(42)) {
		t.Errorf("receive = %s, want 42", got.Inspect())
	}
}

func TestReceiveAfterWithPatternMatch(t *testing.T) {
	env := testutil.TestEnv()

	proc := env.Ctx.Proc; //()
	go func() {
		time.Sleep(10 * time.Millisecond)
		proc.Send(core.TupleVal(core.AtomVal("ok"), core.IntVal(99)))
	}()

	evalScript(t, env, `
result = receive do
  {:ok, val} -> val
  {:error, reason} -> reason
after 5000 ->
  :timeout
end
`)
	got, _ := env.Get("result")
	if !got.Equal(core.IntVal(99)) {
		t.Errorf("receive = %s, want 99", got.Inspect())
	}
}

// ---------------------------------------------------------------------------
// stdlib map operations
// ---------------------------------------------------------------------------

func TestStdlibPut(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `m = %{a: 1, b: 2}
result = Map.put(m, "c", 3)`)
	got, _ := env.Get("result")
	if got.Kind != core.VMap {
		t.Fatalf("expected map, got %s", got.Inspect())
	}
	if v, ok := got.GetMap().Get("c"); !ok || !v.Equal(core.IntVal(3)) {
		t.Errorf("Map.put: expected c=3, got %s", got.Inspect())
	}
	if v, ok := got.GetMap().Get("a"); !ok || !v.Equal(core.IntVal(1)) {
		t.Errorf("Map.put: expected a=1, got %s", got.Inspect())
	}
}

func TestStdlibDelete(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `m = %{a: 1, b: 2, c: 3}
result = Map.delete(m, "b")`)
	got, _ := env.Get("result")
	if got.Kind != core.VMap {
		t.Fatalf("expected map, got %s", got.Inspect())
	}
	if _, ok := got.GetMap().Get("b"); ok {
		t.Error("Map.delete: expected b to be removed")
	}
	if len(got.GetMap().Keys) != 2 {
		t.Errorf("Map.delete: expected 2 keys, got %d", len(got.GetMap().Keys))
	}
}

func TestStdlibMerge(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `m1 = %{a: 1, b: 2}
m2 = %{b: 99, c: 3}
result = Map.merge(m1, m2)`)
	got, _ := env.Get("result")
	if got.Kind != core.VMap {
		t.Fatalf("expected map, got %s", got.Inspect())
	}
	if v, ok := got.GetMap().Get("b"); !ok || !v.Equal(core.IntVal(99)) {
		t.Errorf("Map.merge: expected b=99, got %s", got.Inspect())
	}
}

func TestStdlibKeys(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `m = %{x: 1, y: 2}
result = Map.keys m`)
	got, _ := env.Get("result")
	if got.Kind != core.VList {
		t.Fatalf("expected list, got %s", got.Inspect())
	}
	if len(got.GetElems()) != 2 {
		t.Errorf("Map.keys: expected 2 elements, got %d", len(got.GetElems()))
	}
}

func TestStdlibValues(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `m = %{x: 10, y: 20}
result = Map.values m`)
	got, _ := env.Get("result")
	if got.Kind != core.VList {
		t.Fatalf("expected list, got %s", got.Inspect())
	}
	if len(got.GetElems()) != 2 {
		t.Errorf("Map.values: expected 2 elements, got %d", len(got.GetElems()))
	}
}

func TestStdlibHasKey(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `m = %{x: 1, y: 2}
r1 = Map.has_key(m, "x")
r2 = Map.has_key(m, "z")`)
	r1, _ := env.Get("r1")
	r2, _ := env.Get("r2")
	if !r1.Equal(core.True) {
		t.Errorf("Map.has_key m, x = %s, want :true", r1.Inspect())
	}
	if !r2.Equal(core.False) {
		t.Errorf("Map.has_key m, z = %s, want :false", r2.Inspect())
	}
}

func TestListEachReturnsOk(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
result = [1, 2, 3] |> List.each \_ -> nil
`)
	got, _ := env.Get("result")
	if !got.Equal(core.AtomVal("ok")) {
		t.Errorf("List.each return = %s, want :ok", got.Inspect())
	}
}

func TestProcessSendAfter(t *testing.T) {
	env := testutil.TestEnv()
	testutil.RunSource(`
pid = spawn fn do
  receive do
    {:ping, from} -> send from, :pong
  end
end

Process.send_after 10, pid, {:ping, self}

result = receive do
  :pong -> :got_pong
after 2000 ->
  :timeout
end
`, env)
	got, _ := env.Get("result")
	if !got.Equal(core.AtomVal("got_pong")) {
		t.Errorf("result = %s, want :got_pong", got.Inspect())
	}
}

func TestStringChars(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = String.chars "hello"`)
	got, _ := env.Get("result")
	want := core.ListVal(
		core.StringVal("h"),
		core.StringVal("e"),
		core.StringVal("l"),
		core.StringVal("l"),
		core.StringVal("o"),
	)
	if !got.Equal(want) {
		t.Errorf("chars = %s, want %s", got.Inspect(), want.Inspect())
	}
}

func TestStringPad(t *testing.T) {
	cases := []struct {
		script string
		want   string
	}{
		{`result = String.pad_left("7", 3, "0")`, "007"},
		{`result = String.pad_right("7", 3, "0")`, "700"},
		{`result = String.pad_left("hello", 3, "x")`, "hello"},
		{`result = String.pad_left("", 3, "x")`, "xxx"},
	}
	for _, c := range cases {
		env := testutil.TestEnv()
		evalScript(t, env, c.script)
		got, _ := env.Get("result")
		if got.Kind != core.VString || got.Str != c.want {
			t.Errorf("%s = %s, want %q", c.script, got.Inspect(), c.want)
		}
	}
}
