package debug

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"ish/internal/ast"
	"ish/internal/lexer"
	"ish/internal/parser"
)

func TestSourceMapResolve(t *testing.T) {
	src := "hello\nworld\nfoo"
	sm := NewSourceMap("test.ish", src)

	tests := []struct {
		pos      int
		wantLine int
		wantCol  int
	}{
		{0, 1, 1},   // start of file
		{4, 1, 5},   // 'o' in hello
		{5, 1, 6},   // newline at end of hello
		{6, 2, 1},   // 'w' in world
		{11, 2, 6},  // newline at end of world
		{12, 3, 1},  // 'f' in foo
		{14, 3, 3},  // last char
		{-1, 1, 1},  // negative pos
	}

	for _, tt := range tests {
		line, col := sm.Resolve(tt.pos)
		if line != tt.wantLine || col != tt.wantCol {
			t.Errorf("Resolve(%d) = %d:%d, want %d:%d", tt.pos, line, col, tt.wantLine, tt.wantCol)
		}
	}
}

func TestSourceMapFormatPos(t *testing.T) {
	src := "hello\nworld"
	sm := NewSourceMap("test.ish", src)
	got := sm.FormatPos(6)
	if got != "test.ish:2:1" {
		t.Errorf("FormatPos(6) = %q, want %q", got, "test.ish:2:1")
	}

	sm2 := NewSourceMap("", src)
	got2 := sm2.FormatPos(6)
	if got2 != "2:1" {
		t.Errorf("FormatPos(6) no filename = %q, want %q", got2, "2:1")
	}
}

func TestSourceMapSingleLine(t *testing.T) {
	src := "echo hello"
	sm := NewSourceMap("", src)
	line, col := sm.Resolve(5)
	if line != 1 || col != 6 {
		t.Errorf("Resolve(5) = %d:%d, want 1:6", line, col)
	}
}

func TestDebuggerPushPopFrame(t *testing.T) {
	d := New()
	sm := NewSourceMap("test.ish", "line1\nline2\nline3")
	d.PushSource(sm)

	d.SetNode(6) // line 2, col 1
	d.PushFrame("outer", 0)

	d.SetNode(12) // line 3, col 1
	d.PushFrame("inner", 2)

	stack := d.FormatStack()
	if !strings.Contains(stack, "inner/2") {
		t.Errorf("stack missing inner/2: %s", stack)
	}
	if !strings.Contains(stack, "outer/0") {
		t.Errorf("stack missing outer/0: %s", stack)
	}

	d.PopFrame()
	stack = d.FormatStack()
	if strings.Contains(stack, "inner/2") {
		t.Errorf("stack should not contain inner/2 after pop: %s", stack)
	}
	if !strings.Contains(stack, "outer/0") {
		t.Errorf("stack missing outer/0 after pop: %s", stack)
	}
}

func TestDebuggerWrapError(t *testing.T) {
	d := New()
	sm := NewSourceMap("test.ish", "line1\nline2")
	d.PushSource(sm)

	d.SetNode(0)
	d.PushFrame("main", 0)
	d.SetNode(6)
	d.PushFrame("helper", 1)

	orig := errors.New("something broke")
	wrapped := d.WrapError(orig)

	te, ok := wrapped.(*TraceError)
	if !ok {
		t.Fatalf("expected *TraceError, got %T", wrapped)
	}
	if te.Unwrap() != orig {
		t.Error("Unwrap should return original error")
	}

	msg := te.Error()
	if !strings.Contains(msg, "something broke") {
		t.Errorf("error message missing original: %s", msg)
	}
	if !strings.Contains(msg, "helper/1") {
		t.Errorf("error message missing helper/1: %s", msg)
	}
	if !strings.Contains(msg, "main/0") {
		t.Errorf("error message missing main/0: %s", msg)
	}
}

func TestDebuggerWrapErrorNoStack(t *testing.T) {
	d := New()
	orig := errors.New("bare error")
	wrapped := d.WrapError(orig)
	// No frames pushed, should return original error
	if wrapped != orig {
		t.Error("WrapError with no frames should return original error")
	}
}

func TestDebuggerCopyForSpawn(t *testing.T) {
	d := New()
	sm := NewSourceMap("test.ish", "hello")
	d.PushSource(sm)
	d.SetNode(0)
	d.PushFrame("main", 0)
	d.TraceAll = true

	cp := d.CopyForSpawn()
	if len(cp.stack) != 0 {
		t.Error("spawned debugger should have empty stack")
	}
	if cp.current != sm {
		t.Error("spawned debugger should share source map")
	}
	if !cp.TraceAll {
		t.Error("spawned debugger should inherit TraceAll")
	}
}

func TestDebuggerPushPopSource(t *testing.T) {
	d := New()
	sm1 := NewSourceMap("a.ish", "hello")
	sm2 := NewSourceMap("b.ish", "world")

	d.PushSource(sm1)
	if d.CurrentSource() != sm1 {
		t.Error("current should be sm1")
	}

	d.PushSource(sm2)
	if d.CurrentSource() != sm2 {
		t.Error("current should be sm2")
	}

	d.PopSource()
	if d.CurrentSource() != sm1 {
		t.Error("current should be sm1 after pop")
	}

	d.PopSource()
	if d.CurrentSource() != nil {
		t.Error("current should be nil after all pops")
	}
}

func TestDumpAST(t *testing.T) {
	src := "echo hello"
	l := lexer.New(src)
	node, err := parser.Parse(l)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	sm := NewSourceMap("test.ish", src)
	var buf bytes.Buffer
	DumpAST(node, sm, &buf)

	out := buf.String()
	if !strings.Contains(out, "NCmd") {
		t.Errorf("dump should contain NCmd: %s", out)
	}
	if !strings.Contains(out, "echo") {
		t.Errorf("dump should contain 'echo': %s", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("dump should contain 'hello': %s", out)
	}
}

func TestDumpASTFnDef(t *testing.T) {
	src := "fn greet name do\n  echo hello\nend"
	l := lexer.New(src)
	node, err := parser.Parse(l)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	sm := NewSourceMap("test.ish", src)
	var buf bytes.Buffer
	DumpAST(node, sm, &buf)

	out := buf.String()
	if !strings.Contains(out, "NIshFn") {
		t.Errorf("dump should contain NIshFn: %s", out)
	}
	if !strings.Contains(out, "Clause") {
		t.Errorf("dump should contain Clause: %s", out)
	}
}

func TestNodeKindStringCoverage(t *testing.T) {
	// Verify all known kinds produce a non-Unknown string
	kinds := []ast.NodeKind{
		ast.NLit, ast.NIdent, ast.NCmd, ast.NPipe, ast.NPipeFn,
		ast.NAndList, ast.NOrList, ast.NBg, ast.NBlock, ast.NAssign,
		ast.NMatch, ast.NVarRef, ast.NSubshell, ast.NGroup,
		ast.NIf, ast.NFor, ast.NWhile, ast.NUntil, ast.NCase, ast.NFnDef,
		ast.NIshFn, ast.NIshMatch, ast.NIshSpawn, ast.NIshSpawnLink,
		ast.NIshSend, ast.NIshReceive, ast.NIshMonitor, ast.NIshAwait,
		ast.NIshSupervise, ast.NIshTry,
		ast.NBinOp, ast.NUnary, ast.NTuple, ast.NList, ast.NMap,
		ast.NAccess, ast.NLambda,
	}
	for _, k := range kinds {
		s := nodeKindString(k)
		if strings.HasPrefix(s, "Unknown") {
			t.Errorf("nodeKindString(%d) returned Unknown", k)
		}
	}
}
