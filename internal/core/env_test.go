package core

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestEnvGetSet(t *testing.T) {
	t.Run("set and get", func(t *testing.T) {
		e := NewEnv(nil)
		e.Set("x", IntVal(42))
		v, ok := e.Get("x")
		if !ok {
			t.Fatal("expected x to exist")
		}
		if v.Int != 42 {
			t.Errorf("expected 42, got %d", v.Int)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		e := NewEnv(nil)
		_, ok := e.Get("nope")
		if ok {
			t.Error("expected missing key to return false")
		}
	})

	t.Run("Set walks chain to update parent", func(t *testing.T) {
		parent := NewEnv(nil)
		parent.Set("x", IntVal(1))
		child := NewEnv(parent)
		child.Set("x", IntVal(2))

		v, _ := child.Get("x")
		if v.Int != 2 {
			t.Errorf("child should see updated value: got %d, want 2", v.Int)
		}

		v, _ = parent.Get("x")
		if v.Int != 2 {
			t.Errorf("parent should be updated by child Set: got %d, want 2", v.Int)
		}
	})

	t.Run("SetLocal shadows parent", func(t *testing.T) {
		parent := NewEnv(nil)
		parent.Set("x", IntVal(1))
		child := NewEnv(parent)
		child.SetLocal("x", IntVal(2))

		v, _ := child.Get("x")
		if v.Int != 2 {
			t.Errorf("child should shadow parent: got %d, want 2", v.Int)
		}

		v, _ = parent.Get("x")
		if v.Int != 1 {
			t.Errorf("parent should be unchanged: got %d, want 1", v.Int)
		}
	})

	t.Run("Set creates local when not in parent", func(t *testing.T) {
		parent := NewEnv(nil)
		child := NewEnv(parent)
		child.Set("newvar", IntVal(99))

		v, ok := child.Get("newvar")
		if !ok || v.Int != 99 {
			t.Errorf("child should have newvar=99, got %v", v)
		}

		_, ok = parent.Get("newvar")
		if ok {
			t.Error("parent should not have newvar")
		}
	})

	t.Run("child reads from parent", func(t *testing.T) {
		parent := NewEnv(nil)
		parent.Set("y", StringVal("from_parent"))
		child := NewEnv(parent)

		v, ok := child.Get("y")
		if !ok {
			t.Fatal("expected to find y via parent")
		}
		if v.Str != "from_parent" {
			t.Errorf("got %q, want %q", v.Str, "from_parent")
		}
	})
}

func TestEnvGetSetFn(t *testing.T) {
	t.Run("set and get fn", func(t *testing.T) {
		e := NewEnv(nil)
		fn := &FnValue{Name: "add", Clauses: []FnClause{{}}}
		e.AddFnClauses("add", fn)

		got, ok := e.GetFn("add")
		if !ok {
			t.Fatal("expected fn add to exist")
		}
		if got.Name != "add" {
			t.Errorf("name: got %q, want %q", got.Name, "add")
		}
	})

	t.Run("multi-clause append", func(t *testing.T) {
		e := NewEnv(nil)
		fn1 := &FnValue{Name: "fib", Clauses: []FnClause{{}}}
		fn2 := &FnValue{Name: "fib", Clauses: []FnClause{{}, {}}}

		e.AddFnClauses("fib", fn1)
		e.AddFnClauses("fib", fn2) // should append clauses

		got, _ := e.GetFn("fib")
		if len(got.Clauses) != 3 {
			t.Errorf("expected 3 clauses after append, got %d", len(got.Clauses))
		}
	})

	t.Run("child inherits parent fn", func(t *testing.T) {
		parent := NewEnv(nil)
		parent.AddFnClauses("greet", &FnValue{Name: "greet", Clauses: []FnClause{{}}})
		child := NewEnv(parent)

		got, ok := child.GetFn("greet")
		if !ok {
			t.Fatal("expected child to find parent fn")
		}
		if got.Name != "greet" {
			t.Errorf("name: got %q, want %q", got.Name, "greet")
		}
	})

	t.Run("missing fn", func(t *testing.T) {
		e := NewEnv(nil)
		_, ok := e.GetFn("missing")
		if ok {
			t.Error("expected missing fn to return false")
		}
	})
}

func TestEnvExpand(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e *Env)
		input string
		want  string
	}{
		{
			name:  "no expansion needed",
			setup: func(e *Env) {},
			input: "hello",
			want:  "hello",
		},
		{
			name:  "simple $var",
			setup: func(e *Env) { e.Set("name", StringVal("ish")) },
			input: "hello $name",
			want:  "hello ish",
		},
		{
			name:  "braced ${var}",
			setup: func(e *Env) { e.Set("x", StringVal("world")) },
			input: "hello ${x}!",
			want:  "hello world!",
		},
		{
			name:  "$? exit code",
			setup: func(e *Env) { e.SetExit(42) },
			input: "exit=$?",
			want:  "exit=42",
		},
		{
			name:  "$$ pid",
			setup: func(e *Env) { e.Shell.ShellPid = 1234 },
			input: "pid=$$",
			want:  "pid=1234",
		},
		{
			name:  "$# arg count",
			setup: func(e *Env) { e.Args = []string{"a", "b", "c"} },
			input: "count=$#",
			want:  "count=3",
		},
		{
			name:  "$@ all args",
			setup: func(e *Env) { e.Args = []string{"x", "y"} },
			input: "args=$@",
			want:  "args=x y",
		},
		{
			name:  "$* all args",
			setup: func(e *Env) { e.Args = []string{"a", "b"} },
			input: "args=$*",
			want:  "args=a b",
		},
		{
			name:  "$1 positional",
			setup: func(e *Env) { e.Args = []string{"first", "second"} },
			input: "arg=$1",
			want:  "arg=first",
		},
		{
			name:  "$2 positional",
			setup: func(e *Env) { e.Args = []string{"first", "second"} },
			input: "arg=$2",
			want:  "arg=second",
		},
		{
			name:  "$9 out of range",
			setup: func(e *Env) { e.Args = []string{"only"} },
			input: "arg=$9",
			want:  "arg=",
		},
		{
			name:  "hash interpolation #{expr}",
			setup: func(e *Env) { e.Set("name", StringVal("world")) },
			input: "hello #{name}!",
			want:  "hello world!",
		},
		{
			name:  "bare $ at end",
			setup: func(e *Env) {},
			input: "cost$",
			want:  "cost$",
		},
		{
			name:  "undefined var",
			setup: func(e *Env) {},
			input: "val=$undefined",
			want:  "val=",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEnv(nil)
			tt.setup(e)
			got := e.Expand(tt.input)
			if got != tt.want {
				t.Errorf("Expand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestEnvGetProc is in core_external_test.go (needs process import)

func TestEnvGetProcNil(t *testing.T) {
	e := NewEnv(nil)
	got := e.GetProc()
	if got != nil {
		t.Error("expected nil when no proc set")
	}
}

func TestEnvStdout(t *testing.T) {
	t.Run("direct stdout", func(t *testing.T) {
		e := NewEnv(nil)
		var buf bytes.Buffer
		e.Stdout_ = &buf

		got := e.Stdout()
		if got != &buf {
			t.Error("expected Stdout() to return the buffer")
		}
	})

	t.Run("walks parent", func(t *testing.T) {
		parent := NewEnv(nil)
		var buf bytes.Buffer
		parent.Stdout_ = &buf

		child := NewEnv(parent)
		got := child.Stdout()
		if got != &buf {
			t.Error("expected Stdout() to find parent's stdout")
		}
	})

	t.Run("defaults to os.Stdout", func(t *testing.T) {
		e := NewEnv(nil)
		got := e.Stdout()
		if got != os.Stdout {
			t.Error("expected Stdout() to default to os.Stdout")
		}
	})
}

func TestEnvExpandParentChain(t *testing.T) {
	parent := NewEnv(nil)
	parent.Set("color", StringVal("blue"))

	child := NewEnv(parent)
	got := child.Expand("the color is $color")
	want := "the color is blue"
	if got != want {
		t.Errorf("Expand with parent: got %q, want %q", got, want)
	}
}

func TestEnvExpandNoMarkers(t *testing.T) {
	e := NewEnv(nil)
	input := "just plain text"
	got := e.Expand(input)
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestEnvBuildEnv(t *testing.T) {
	t.Run("includes exported vars", func(t *testing.T) {
		e := NewEnv(nil)
		e.Shell.Exported = make(map[string]bool)
		e.SetLocal("FOO", StringVal("bar"))
		e.Shell.Exported["FOO"] = true
		e.SetLocal("SECRET", StringVal("hidden"))

		envVars := e.BuildEnv()
		foundFoo := false
		foundSecret := false
		for _, kv := range envVars {
			if kv == "FOO=bar" {
				foundFoo = true
			}
			if strings.HasPrefix(kv, "SECRET=") {
				foundSecret = true
			}
		}
		if !foundFoo {
			t.Error("expected FOO=bar in BuildEnv")
		}
		if foundSecret {
			t.Error("SECRET should not be in BuildEnv (not exported)")
		}
	})

	t.Run("child scope overrides parent", func(t *testing.T) {
		parent := NewEnv(nil)
		parent.Shell.Exported = make(map[string]bool)
		parent.SetLocal("X", StringVal("old"))
		parent.Shell.Exported["X"] = true

		child := NewEnv(parent)
		child.SetLocal("X", StringVal("new"))

		envVars := child.BuildEnv()
		for _, kv := range envVars {
			if kv == "X=old" {
				t.Error("BuildEnv should use child's value, not parent's")
			}
			if kv == "X=new" {
				return // success
			}
		}
		t.Error("expected X=new in BuildEnv")
	})
}

func TestEnvDeleteVar(t *testing.T) {
	parent := NewEnv(nil)
	parent.SetLocal("x", IntVal(1))
	child := NewEnv(parent)

	child.DeleteVar("x")
	if _, ok := parent.Get("x"); ok {
		t.Error("DeleteVar should remove from parent scope")
	}
}

func TestEnvDeleteFn(t *testing.T) {
	parent := NewEnv(nil)
	parent.AddFnClauses("f", &FnValue{Name: "f"})
	child := NewEnv(parent)

	child.DeleteFn("f")
	if _, ok := parent.GetFn("f"); ok {
		t.Error("DeleteFn should remove from parent scope")
	}
}

func TestEnvExpandShellName(t *testing.T) {
	e := NewEnv(nil)
	e.Shell.ShellName = "testshell"
	got := e.Expand("name=$0")
	if got != "name=testshell" {
		t.Errorf("$0 expansion: got %q, want %q", got, "name=testshell")
	}
}

func TestEnvExpandMultiDigitPositional(t *testing.T) {
	e := NewEnv(nil)
	e.Args = make([]string, 12)
	for i := range e.Args {
		e.Args[i] = fmt.Sprintf("arg%d", i+1)
	}

	got := e.Expand("$10")
	if got != "arg10" {
		t.Errorf("$10 expansion: got %q, want %q", got, "arg10")
	}

	got = e.Expand("$12")
	if got != "arg12" {
		t.Errorf("$12 expansion: got %q, want %q", got, "arg12")
	}
}

func TestEnvExpandIFSStar(t *testing.T) {
	e := NewEnv(nil)
	e.Args = []string{"a", "b", "c"}
	e.SetLocal("IFS", StringVal(","))

	got := e.Expand("$*")
	if got != "a,b,c" {
		t.Errorf("$* with IFS=',': got %q, want %q", got, "a,b,c")
	}
}

func TestEnvExitCodeIsolation(t *testing.T) {
	parent := NewEnv(nil)
	parent.SetExit(1)
	child := NewEnv(parent)

	if child.ExitCode() != 1 {
		t.Errorf("expected 1 from parent, got %d", child.ExitCode())
	}

	child.SetExit(0)
	if child.ExitCode() != 0 {
		t.Errorf("after child SetExit(0), expected 0, got %d", child.ExitCode())
	}

	if parent.ExitCode() != 1 {
		t.Errorf("parent should still have 1, got %d", parent.ExitCode())
	}
}

func TestExpandParamOps(t *testing.T) {
	tests := []struct{ name, setup, input, want string }{
		{"length", "FOO=hello", "${#FOO}", "5"},
		{"prefix short", "PATH=/usr/local/bin:/usr/bin", "${PATH#*/}", "usr/local/bin:/usr/bin"},
		{"prefix long", "PATH=/usr/local/bin:/usr/bin", "${PATH##*/}", "bin"},
		{"suffix short", "FILE=archive.tar.gz", "${FILE%.*}", "archive.tar"},
		{"suffix long", "FILE=archive.tar.gz", "${FILE%%.*}", "archive"},
		{"replace first", "STR=hello-hello-world", "${STR/hello/bye}", "bye-hello-world"},
		{"replace all", "STR=hello-hello-world", "${STR//hello/bye}", "bye-bye-world"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEnv(nil)
			parts := strings.SplitN(tt.setup, "=", 2)
			e.SetLocal(parts[0], StringVal(parts[1]))
			got := e.Expand(tt.input)
			if got != tt.want {
				t.Errorf("Expand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEnvExitCodeWalksParent(t *testing.T) {
	parent := NewEnv(nil)
	parent.SetExit(5)
	child := NewEnv(parent)

	got := child.Expand("$?")
	if !strings.Contains(got, "5") {
		t.Errorf("expected exit code from parent, got %q", got)
	}

	child.SetExit(0)
	got = child.Expand("$?")
	if got != "0" {
		t.Errorf("after child SetExit(0), expected 0, got %q", got)
	}
}

func TestCopyEnvPreservesReadonly(t *testing.T) {
	env := TopEnv()
	env.Set("X", StringVal("1"))
	env.SetReadonly("X")
	cp := CopyEnv(env)
	if !cp.IsReadonly("X") {
		t.Error("CopyEnv should preserve readonly status")
	}
}

func TestCopyEnvPreservesFlags(t *testing.T) {
	env := TopEnv()
	env.SetFlag('e', true)
	cp := CopyEnv(env)
	if !cp.HasFlag('e') {
		t.Error("CopyEnv should preserve set -e flag")
	}
}

func TestCopyEnvPreservesTraps(t *testing.T) {
	env := TopEnv()
	env.SetTrap("EXIT", "echo bye")
	cp := CopyEnv(env)
	cmd, ok := cp.GetTrap("EXIT")
	if !ok || cmd != "echo bye" {
		t.Errorf("CopyEnv should preserve traps, got %q ok=%v", cmd, ok)
	}
}
