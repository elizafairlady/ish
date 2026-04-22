package core

import (
	"os"
	"testing"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Struct size verification
// ---------------------------------------------------------------------------

func TestEnvSize(t *testing.T) {
	size := unsafe.Sizeof(Env{})
	t.Logf("Env: %d bytes", size)
	if size > 64 {
		t.Errorf("Env should be <= 64 bytes, got %d", size)
	}
}

func TestFrameSize(t *testing.T) {
	size := unsafe.Sizeof(Frame{})
	t.Logf("Frame: %d bytes", size)
	// Frame has flat arrays: 4 strings (4*16) + 4 Values (4*40) + parent + ctx + flatN + spill
	// This is necessarily larger than Env because of the inline arrays
}

func TestExecCtxSize(t *testing.T) {
	size := unsafe.Sizeof(ExecCtx{})
	t.Logf("ExecCtx: %d bytes", size)
}

func TestValueSize(t *testing.T) {
	size := unsafe.Sizeof(Value{})
	t.Logf("Value: %d bytes", size)
	if size > 40 {
		t.Errorf("Value should be <= 40 bytes, got %d", size)
	}
}

// ---------------------------------------------------------------------------
// ExecCtx: shared execution state. Exit codes, flags, traps, aliases,
// args, proc — all shared by pointer across all scopes in one execution.
// ---------------------------------------------------------------------------

func TestExecCtxExitCodeShared(t *testing.T) {
	// Exit codes are on ExecCtx, shared by pointer.
	// false; true should leave $? = 0 (last command wins).
	ctx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	ctx.SetExit(1) // false
	ctx.SetExit(0) // true
	if ctx.ExitCode() != 0 {
		t.Errorf("expected 0, got %d", ctx.ExitCode())
	}
}

func TestExecCtxSharedAcrossScopes(t *testing.T) {
	// Parent env and child frame share the same ExecCtx pointer.
	// Exit code set in child is visible in parent.
	ctx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	parent := &Env{Bindings: make(map[string]Value), Ctx: ctx}
	child := NewFrame(parent)

	child.GetCtx().SetExit(42)
	if parent.Ctx.ExitCode() != 42 {
		t.Errorf("parent should see child's exit code: got %d", parent.Ctx.ExitCode())
	}
}

func TestExecCtxSharedAcrossEnvs(t *testing.T) {
	// Child Env shares parent's ExecCtx.
	ctx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	parent := &Env{Bindings: make(map[string]Value), Ctx: ctx}
	child := NewEnv(parent)

	child.Ctx.SetExit(7)
	if parent.Ctx.ExitCode() != 7 {
		t.Errorf("parent should see child env's exit code: got %d", parent.Ctx.ExitCode())
	}
}

func TestExecCtxFlagsShared(t *testing.T) {
	ctx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	parent := &Env{Bindings: make(map[string]Value), Ctx: ctx}
	child := NewFrame(parent)

	// Set flag from child scope
	child.GetCtx().Shell.SetFlag('e', true)
	if !parent.Ctx.Shell.HasFlag('e') {
		t.Error("flag set in child should be visible in parent via shared ctx")
	}
}

func TestExecCtxAliasesShared(t *testing.T) {
	ctx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	parent := &Env{Bindings: make(map[string]Value), Ctx: ctx}
	child := NewFrame(parent)

	child.GetCtx().Shell.SetAlias("ll", "ls -la")
	if v, ok := parent.Ctx.Shell.GetAlias("ll"); !ok || v != "ls -la" {
		t.Errorf("alias set in child should be visible in parent: got %q, %v", v, ok)
	}
}

func TestExecCtxTrapsShared(t *testing.T) {
	ctx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	parent := &Env{Bindings: make(map[string]Value), Ctx: ctx}
	child := NewEnv(parent)

	child.Ctx.Shell.SetTrap("EXIT", "echo bye")
	if cmd, ok := parent.Ctx.Shell.GetTrap("EXIT"); !ok || cmd != "echo bye" {
		t.Errorf("trap set in child should be visible: got %q, %v", cmd, ok)
	}
}

func TestExecCtxArgsShared(t *testing.T) {
	ctx := &ExecCtx{Stdout: os.Stdout, Args: []string{"a", "b", "c"}}
	parent := &Env{Bindings: make(map[string]Value), Ctx: ctx}
	child := NewFrame(parent)

	args := child.GetCtx().PosArgs()
	if len(args) != 3 || args[0] != "a" {
		t.Errorf("child should see parent's args: got %v", args)
	}
}

// ---------------------------------------------------------------------------
// NearestEnv: why it exists and when it's needed.
//
// NearestEnv walks the scope chain (Frame → Frame → Env) to find the
// nearest Env. This is needed for LEXICALLY SCOPED state:
//   - Fns, NativeFns, Modules: function definitions are lexical
//   - Bindings iteration: BuildEnv, CopyEnv need to walk Env bindings
//   - DeleteVar, DeleteFn: need to find which Env owns the binding
//   - Export: writes binding to Env AND marks exported on Ctx
//   - Expand: needs Get() which walks the scope chain (works via Scope)
//     but also needs CmdSub which is on Ctx
//
// NearestEnv is NOT needed for:
//   - Exit codes ($?) — on ExecCtx, shared by pointer
//   - Flags (set -e) — on ExecCtx
//   - Traps — on ExecCtx
//   - Aliases — on ExecCtx
//   - Args ($1, $@) — on ExecCtx
//   - Pid ($$) — on ExecCtx
//   - Proc (self) — on ExecCtx
// ---------------------------------------------------------------------------

func TestNearestEnvFromFrame(t *testing.T) {
	ctx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	env := &Env{Bindings: make(map[string]Value), Ctx: ctx}
	env.SetFnClauses("greet", &FnValue{Name: "greet"})

	frame := NewFrame(env)
	frame.SetLocal("x", IntVal(42))

	// Frame can find functions via NearestEnv
	ne := frame.NearestEnv()
	if ne != env {
		t.Fatal("NearestEnv should return parent env")
	}
	fn, ok := ne.GetFn("greet")
	if !ok || fn.Name != "greet" {
		t.Error("should find fn via NearestEnv")
	}
}

func TestNearestEnvDeepChain(t *testing.T) {
	ctx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	env := &Env{Bindings: make(map[string]Value), Ctx: ctx}
	env.SetFnClauses("deep", &FnValue{Name: "deep"})

	f1 := NewFrame(env)
	f2 := NewFrame(f1)
	f3 := NewFrame(f2)

	// Three frames deep, NearestEnv still finds the Env
	ne := f3.NearestEnv()
	if ne != env {
		t.Fatal("deep NearestEnv should find root env")
	}
	fn, ok := ne.GetFn("deep")
	if !ok || fn.Name != "deep" {
		t.Error("should find fn through deep frame chain")
	}
}

func TestFnLexicalScoping(t *testing.T) {
	// Functions defined inside a function body (via NearestEnv) land on
	// the nearest Env, which is the function's closure env — not the
	// caller's env. This is lexical scoping.
	ctx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	outer := &Env{Bindings: make(map[string]Value), Ctx: ctx}
	inner := NewEnv(outer)
	inner.SetFnClauses("local_fn", &FnValue{Name: "local_fn"})

	// local_fn is visible from inner
	_, ok := inner.GetFn("local_fn")
	if !ok {
		t.Error("local_fn should be visible from inner env")
	}

	// local_fn is NOT visible from outer (lexical scoping)
	_, ok = outer.GetFn("local_fn")
	if ok {
		t.Error("local_fn should NOT be visible from outer env")
	}
}

func TestFrameSetWalksToEnv(t *testing.T) {
	// When a frame does Set("x", val) and x exists on a parent Env,
	// the set walks up and modifies the Env's binding.
	ctx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	env := &Env{Bindings: make(map[string]Value), Ctx: ctx}
	env.Bindings["counter"] = IntVal(0)

	frame := NewFrame(env)
	frame.Set("counter", IntVal(1))

	v, _ := env.Get("counter")
	if v.GetInt() != 1 {
		t.Errorf("Set from frame should update env binding: got %d", v.GetInt())
	}
}

func TestFrameSetLocalShadows(t *testing.T) {
	// SetLocal on a frame creates a local binding that shadows the parent.
	ctx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	env := &Env{Bindings: make(map[string]Value), Ctx: ctx}
	env.Bindings["x"] = IntVal(10)

	frame := NewFrame(env)
	frame.SetLocal("x", IntVal(20))

	// Frame sees shadow
	v, _ := frame.Get("x")
	if v.GetInt() != 20 {
		t.Errorf("frame should see shadow: got %d", v.GetInt())
	}

	// Env unchanged
	v, _ = env.Get("x")
	if v.GetInt() != 10 {
		t.Errorf("env should be unchanged: got %d", v.GetInt())
	}
}

func TestFrameResetAndReuse(t *testing.T) {
	ctx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	env := &Env{Bindings: make(map[string]Value), Ctx: ctx}
	frame := NewFrame(env)

	// Bind for clause attempt 1
	frame.SetLocal("n", IntVal(5))
	v, ok := frame.Get("n")
	if !ok || v.GetInt() != 5 {
		t.Fatal("n should be 5")
	}

	// Reset for clause attempt 2
	frame.ResetFlat()
	_, ok = frame.Get("n")
	if ok {
		t.Error("n should be gone after reset")
	}

	// Rebind
	frame.SetLocal("x", IntVal(10))
	v, _ = frame.Get("x")
	if v.GetInt() != 10 {
		t.Errorf("x should be 10, got %d", v.GetInt())
	}

	// Parent still accessible
	env.Bindings["GLOBAL"] = StringVal("visible")
	v, ok = frame.Get("GLOBAL")
	if !ok || v.Str != "visible" {
		t.Errorf("parent access after reset: ok=%v val=%s", ok, v.Str)
	}
}

// ---------------------------------------------------------------------------
// ForRedirect: creates child ExecCtx with different Stdout.
// Exit codes set in the redirect child MUST be visible to the parent
// because POSIX requires $? to reflect the last command.
// ---------------------------------------------------------------------------

func TestForRedirectCreatesNewCtx(t *testing.T) {
	parent := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	parent.SetExit(0)
	parent.Shell.SetAlias("ll", "ls -la")
	parent.Shell.SetTrap("EXIT", "echo bye")
	parent.Shell.SetFlag('e', true)

	child := parent.ForRedirect(os.Stderr)

	// ForRedirect shares map pointers — same maps, different ExecCtx struct.
	// This means alias/trap/flag mutations in the child ARE visible to parent
	// because they share the same map pointers.
	child.Shell.SetAlias("new", "echo new")
	if _, ok := parent.Shell.GetAlias("new"); !ok {
		t.Error("ForRedirect shares alias map — parent should see child's alias")
	}

	// But exit codes are NOT shared — they're value fields on the struct.
	child.SetExit(42)
	if parent.ExitCode() != 0 {
		t.Error("ForRedirect should NOT share exit codes (different struct)")
	}
}

// ---------------------------------------------------------------------------
// THE CRITICAL TEST: NearestEnv vs GetCtx in redirect context.
//
// When a builtin runs inside a redirect (e.g., `alias ll='ls' > /dev/null`),
// the scope has a redirect ExecCtx with different Stdout. If the builtin
// uses scope.GetCtx() to set state, it sets it on the redirect's ExecCtx.
//
// ForRedirect currently shares map pointers (aliases, traps, flags), so
// mutations via GetCtx() ARE visible to the parent. This is correct for
// aliases/traps/flags because they're shared mutable state.
//
// But if ForRedirect ever deep-copies maps (for isolation), then GetCtx()
// mutations would be lost. NearestEnv would still work because it walks
// to the Env, which doesn't change across redirects.
//
// Currently: GetCtx() and NearestEnv() give the same result for shared
// state because ForRedirect shares map pointers. The risk is future changes
// to ForRedirect breaking this assumption.
// ---------------------------------------------------------------------------

func TestBuiltinInRedirectContext_PreInitMaps(t *testing.T) {
	// When parent has maps pre-initialized, ForRedirect copies the map pointer.
	parentCtx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	parentCtx.Shell.SetAlias("existing", "echo hi")

	env := &Env{Bindings: make(map[string]Value), Ctx: parentCtx}
	redirectCtx := parentCtx.ForRedirect(os.Stderr)
	frame := NewFrame(env)
	frame.Ctx = redirectCtx

	// Redirect ctx and parent ctx are different structs
	if frame.GetCtx() == env.Ctx {
		t.Error("redirect frame should have different ExecCtx pointer")
	}

	// Alias set via redirect ctx IS visible to parent (shared map pointer)
	frame.GetCtx().Shell.SetAlias("new_alias", "echo new")
	if _, ok := env.Ctx.Shell.GetAlias("new_alias"); !ok {
		t.Error("alias set via redirect ctx should be visible to parent (shared map)")
	}
}

func TestBuiltinInRedirectContext_NilMaps(t *testing.T) {
	// With ShellState as a shared pointer, even when alias map starts nil,
	// ForRedirect copies the Shell pointer. Both parent and child share
	// the same ShellState. Child's lazy init is visible to parent.
	parentCtx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}

	env := &Env{Bindings: make(map[string]Value), Ctx: parentCtx}
	redirectCtx := parentCtx.ForRedirect(os.Stderr)
	frame := NewFrame(env)
	frame.Ctx = redirectCtx

	frame.GetCtx().Shell.SetAlias("ll", "ls -la")

	if _, ok := env.Ctx.Shell.GetAlias("ll"); !ok {
		t.Error("parent SHOULD see alias — Shell is shared pointer, not copied maps")
	}
}

// ---------------------------------------------------------------------------
// POSIX function with redirect: `f() { alias ll='ls'; } > /dev/null`
// The alias set inside the function body should persist after the redirect.
// This works because ForRedirect shares alias map pointers.
// ---------------------------------------------------------------------------

func TestAliasInFunctionWithRedirect_PreInit(t *testing.T) {
	parentCtx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	parentCtx.Shell.Aliases = make(map[string]string) // pre-init

	env := &Env{Bindings: make(map[string]Value), Ctx: parentCtx}
	redirectCtx := parentCtx.ForRedirect(os.Stderr)
	fnFrame := NewFrame(env)
	fnFrame.Ctx = redirectCtx

	fnFrame.GetCtx().Shell.SetAlias("ll", "ls -la")

	if v, ok := parentCtx.Shell.GetAlias("ll"); !ok || v != "ls -la" {
		t.Errorf("alias should persist after redirect (pre-init maps): got %q, ok=%v", v, ok)
	}
}

func TestAliasInFunctionWithRedirect_NilInit(t *testing.T) {
	// With ShellState as shared pointer, aliases set in a redirect context
	// persist to the parent even when the alias map was nil at redirect time.
	parentCtx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}

	env := &Env{Bindings: make(map[string]Value), Ctx: parentCtx}
	redirectCtx := parentCtx.ForRedirect(os.Stderr)
	fnFrame := NewFrame(env)
	fnFrame.Ctx = redirectCtx

	fnFrame.GetCtx().Shell.SetAlias("ll", "ls -la")

	if v, ok := parentCtx.Shell.GetAlias("ll"); !ok || v != "ls -la" {
		t.Errorf("alias should persist — Shell is shared pointer: got %q, ok=%v", v, ok)
	}
}

// ---------------------------------------------------------------------------
// POSIX subshell: aliases set in subshell should NOT leak to parent.
// This works because Copy() deep-copies the maps.
// ---------------------------------------------------------------------------

func TestAliasInSubshellDoesNotLeak(t *testing.T) {
	parentCtx := &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	parentCtx.Shell.SetAlias("existing", "echo hi")

	subCtx := parentCtx.Copy()
	subCtx.Shell.SetAlias("sub_only", "echo sub")
	subCtx.Shell.DeleteAlias("existing")

	// Parent should still have "existing" and NOT have "sub_only"
	if _, ok := parentCtx.Shell.GetAlias("existing"); !ok {
		t.Error("subshell should not delete parent's alias")
	}
	if _, ok := parentCtx.Shell.GetAlias("sub_only"); ok {
		t.Error("subshell alias should not leak to parent")
	}
}

// ---------------------------------------------------------------------------
// Copy: fully independent ExecCtx for subshells.
// Changes in subshell must NOT leak to parent.
// ---------------------------------------------------------------------------

func TestCopyIsolatesState(t *testing.T) {
	parent := &ExecCtx{
		Stdout:   os.Stdout,
		ShellPid: 1234,
		Args:     []string{"a", "b"},
		Shell:    &ShellState{},
	}
	parent.Shell.SetFlag('e', true)
	parent.Shell.SetAlias("ll", "ls -la")
	parent.Shell.SetTrap("EXIT", "echo bye")

	child := parent.Copy()

	// Modify child
	child.Shell.SetFlag('e', false)
	child.Shell.SetAlias("ll", "changed")
	child.Shell.DeleteTrap("EXIT")
	child.Args = []string{"x"}
	child.SetExit(99)

	// Parent unchanged
	if !parent.Shell.HasFlag('e') {
		t.Error("parent flag should be unchanged")
	}
	if v, _ := parent.Shell.GetAlias("ll"); v != "ls -la" {
		t.Errorf("parent alias should be unchanged: got %q", v)
	}
	if _, ok := parent.Shell.GetTrap("EXIT"); !ok {
		t.Error("parent trap should be unchanged")
	}
	if len(parent.Args) != 2 {
		t.Errorf("parent args should be unchanged: got %v", parent.Args)
	}
	if parent.ExitCode() != 0 {
		t.Errorf("parent exit code should be unchanged: got %d", parent.ExitCode())
	}
}
