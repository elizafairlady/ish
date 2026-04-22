package builtin_test

import (
	"os"
	"strings"
	"testing"

	"ish/internal/builtin"
	"ish/internal/core"
	"ish/internal/testutil"
)

// ---------------------------------------------------------------------------
// builtinEcho
// ---------------------------------------------------------------------------

func TestBuiltinEcho(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"hello world", []string{"hello", "world"}, "hello world\n"},
		{"dash n suppresses newline", []string{"-n", "hello"}, "hello"},
		{"no args prints newline", []string{}, "\n"},
		{"single arg", []string{"foo"}, "foo\n"},
		{"dash n no args", []string{"-n"}, ""},
		{"flag bundle -neE last E wins", []string{"-neE", "hello\\nworld"}, "hello\\nworld"},
		{"flag bundle -nEe last e wins", []string{"-nEe", "hello\\nworld"}, "hello\nworld"},
		{"flag bundle -ne", []string{"-ne", "hello\\nworld"}, "hello\nworld"},
		{"flag bundle -eee", []string{"-eee", "a\\nb"}, "a\nb\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testutil.TestEnv()
			got := testutil.CaptureOutput(env, func() {
				b := builtin.Builtins["echo"]
				code, err := b(tt.args, env)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if code != 0 {
					t.Fatalf("expected exit code 0, got %d", code)
				}
			})
			if got != tt.want {
				t.Errorf("builtinEcho(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// builtinCd
// ---------------------------------------------------------------------------

func TestBuiltinCd(t *testing.T) {
	t.Run("cd to /tmp", func(t *testing.T) {
		origDir, _ := os.Getwd()
		t.Cleanup(func() { os.Chdir(origDir) })

		env := testutil.TestEnv()
		b := builtin.Builtins["cd"]
		code, err := b([]string{"/tmp"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		cwd, _ := os.Getwd()
		if !strings.HasSuffix(cwd, "tmp") {
			t.Errorf("expected cwd to end with tmp, got %q", cwd)
		}
	})

	t.Run("cd with no args goes to HOME", func(t *testing.T) {
		origDir, _ := os.Getwd()
		t.Cleanup(func() { os.Chdir(origDir) })

		home := os.Getenv("HOME")
		if home == "" {
			t.Skip("HOME not set")
		}
		env := testutil.TestEnv()
		b := builtin.Builtins["cd"]
		code, err := b([]string{}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	})

	t.Run("cd to nonexistent returns error code 1", func(t *testing.T) {
		origDir, _ := os.Getwd()
		t.Cleanup(func() { os.Chdir(origDir) })

		env := testutil.TestEnv()
		b := builtin.Builtins["cd"]
		code, err := b([]string{"/nonexistent_dir_xyz_123"}, env)
		if code != 1 {
			t.Errorf("expected exit code 1, got %d", code)
		}
		if err == nil {
			t.Error("expected error for nonexistent directory")
		}
	})

	t.Run("cd sets PWD and OLDPWD", func(t *testing.T) {
		origDir, _ := os.Getwd()
		t.Cleanup(func() { os.Chdir(origDir) })

		env := testutil.TestEnv()
		b := builtin.Builtins["cd"]
		b([]string{"/tmp"}, env)

		pwd, ok := env.Get("PWD")
		if !ok {
			t.Fatal("PWD not set")
		}
		if !strings.HasSuffix(pwd.ToStr(), "tmp") {
			t.Errorf("PWD = %q, expected to end with tmp", pwd.ToStr())
		}

		oldpwd, ok := env.Get("OLDPWD")
		if !ok {
			t.Fatal("OLDPWD not set")
		}
		if oldpwd.ToStr() != origDir {
			t.Errorf("OLDPWD = %q, want %q", oldpwd.ToStr(), origDir)
		}
	})

	t.Run("cd dash goes to OLDPWD", func(t *testing.T) {
		origDir, _ := os.Getwd()
		t.Cleanup(func() { os.Chdir(origDir) })

		env := testutil.TestEnv()
		env.Set("OLDPWD", core.StringVal("/tmp"))
		b := builtin.Builtins["cd"]
		code, err := b([]string{"-"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		cwd, _ := os.Getwd()
		if !strings.HasSuffix(cwd, "tmp") {
			t.Errorf("cd - should go to OLDPWD, got %q", cwd)
		}
	})
}

// ---------------------------------------------------------------------------
// builtinExport
// ---------------------------------------------------------------------------

func TestBuiltinExport(t *testing.T) {
	t.Run("export FOO=bar", func(t *testing.T) {
		env := testutil.TestEnv()
		b := builtin.Builtins["export"]
		code, err := b([]string{"FOO_TEST_EXPORT=bar"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		v, ok := env.Get("FOO_TEST_EXPORT")
		if !ok {
			t.Fatal("expected FOO_TEST_EXPORT to be set in env")
		}
		if v.ToStr() != "bar" {
			t.Errorf("env binding = %q, want %q", v.ToStr(), "bar")
		}
		found := false
		for _, kv := range env.BuildEnv() {
			if kv == "FOO_TEST_EXPORT=bar" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected FOO_TEST_EXPORT=bar in BuildEnv output")
		}
	})

	t.Run("export existing var", func(t *testing.T) {
		env := testutil.TestEnv()
		env.SetLocal("EXPORT_EXISTING_TEST", core.StringVal("existing_value"))
		b := builtin.Builtins["export"]
		code, err := b([]string{"EXPORT_EXISTING_TEST"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		found := false
		for _, kv := range env.BuildEnv() {
			if kv == "EXPORT_EXISTING_TEST=existing_value" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected EXPORT_EXISTING_TEST=existing_value in BuildEnv output")
		}
	})
}

// ---------------------------------------------------------------------------
// builtinUnset
// ---------------------------------------------------------------------------

func TestBuiltinUnset(t *testing.T) {
	env := testutil.TestEnv()
	env.SetLocal("UNSET_TEST_VAR", core.StringVal("to_remove"))

	b := builtin.Builtins["unset"]
	code, err := b([]string{"UNSET_TEST_VAR"}, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if _, ok := env.Get("UNSET_TEST_VAR"); ok {
		t.Error("expected UNSET_TEST_VAR to be removed from env")
	}
}

func TestBuiltinUnsetFn(t *testing.T) {
	env := testutil.TestEnv()
	env.AddFnClauses("myfunc", &core.FnValue{Name: "myfunc", Clauses: []core.FnClause{{}}})

	if _, ok := env.GetFn("myfunc"); !ok {
		t.Fatal("myfunc should exist before unset")
	}

	b := builtin.Builtins["unset"]
	b([]string{"-f", "myfunc"}, env)

	if _, ok := env.GetFn("myfunc"); ok {
		t.Error("myfunc should be gone after unset -f")
	}
}

// ---------------------------------------------------------------------------
// builtinReturn
// ---------------------------------------------------------------------------

func TestBuiltinReturn(t *testing.T) {
	t.Run("return with code", func(t *testing.T) {
		env := testutil.TestEnv()
		b := builtin.Builtins["return"]
		code, err := b([]string{"42"}, env)
		if code != 42 {
			t.Errorf("expected code 42, got %d", code)
		}
		if err != core.ErrReturn {
			t.Errorf("expected ErrReturn, got %v", err)
		}
	})

	t.Run("return with no args defaults to 0", func(t *testing.T) {
		env := testutil.TestEnv()
		b := builtin.Builtins["return"]
		code, err := b([]string{}, env)
		if code != 0 {
			t.Errorf("expected code 0, got %d", code)
		}
		if err != core.ErrReturn {
			t.Errorf("expected ErrReturn, got %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// builtinTrue / builtinFalse
// ---------------------------------------------------------------------------

func TestBuiltinTrueFalse(t *testing.T) {
	t.Run("true returns 0", func(t *testing.T) {
		env := testutil.TestEnv()
		b := builtin.Builtins["true"]
		code, err := b(nil, env)
		if code != 0 || err != nil {
			t.Errorf("builtinTrue: code=%d, err=%v", code, err)
		}
	})

	t.Run("false returns 1", func(t *testing.T) {
		env := testutil.TestEnv()
		b := builtin.Builtins["false"]
		code, err := b(nil, env)
		if code != 1 || err != nil {
			t.Errorf("builtinFalse: code=%d, err=%v", code, err)
		}
	})
}

// ---------------------------------------------------------------------------
// builtinBreak / builtinContinue
// ---------------------------------------------------------------------------

func TestBuiltinBreakContinue(t *testing.T) {
	t.Run("break returns ErrBreak", func(t *testing.T) {
		env := testutil.TestEnv()
		b := builtin.Builtins["break"]
		code, err := b(nil, env)
		if code != 0 {
			t.Errorf("expected code 0, got %d", code)
		}
		if err != core.ErrBreak {
			t.Errorf("expected ErrBreak, got %v", err)
		}
	})

	t.Run("continue returns ErrContinue", func(t *testing.T) {
		env := testutil.TestEnv()
		b := builtin.Builtins["continue"]
		code, err := b(nil, env)
		if code != 0 {
			t.Errorf("expected code 0, got %d", code)
		}
		if err != core.ErrContinue {
			t.Errorf("expected ErrContinue, got %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// builtinTest
// ---------------------------------------------------------------------------

func TestBuiltinTest(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
	}{
		{"empty args", []string{}, 1},
		{"non-empty string is true", []string{"hello"}, 0},
		{"empty string is false", []string{""}, 1},
		{"-n non-empty", []string{"-n", "hello"}, 0},
		{"-n empty", []string{"-n", ""}, 1},
		{"-z empty", []string{"-z", ""}, 0},
		{"-z non-empty", []string{"-z", "hello"}, 1},
		{"-d existing dir", []string{"-d", "."}, 0},
		{"str = str true", []string{"foo", "=", "foo"}, 0},
		{"str = str false", []string{"foo", "=", "bar"}, 1},
		{"str == str true", []string{"abc", "==", "abc"}, 0},
		{"str != str true", []string{"a", "!=", "b"}, 0},
		{"str != str false", []string{"a", "!=", "a"}, 1},
		{"n -eq n true", []string{"5", "-eq", "5"}, 0},
		{"n -eq n false", []string{"5", "-eq", "3"}, 1},
		{"n -ne n true", []string{"5", "-ne", "3"}, 0},
		{"n -lt n true", []string{"3", "-lt", "5"}, 0},
		{"n -gt n true", []string{"5", "-gt", "3"}, 0},
		{"n -le n true eq", []string{"3", "-le", "3"}, 0},
		{"n -ge n true eq", []string{"3", "-ge", "3"}, 0},
		{"bracket stripping", []string{"hello", "]"}, 0},
		{"! empty string", []string{"!", ""}, 0},
		{"! non-empty string", []string{"!", "hello"}, 1},
		{"compound -a true true", []string{"-d", ".", "-a", "-d", "."}, 0},
		{"compound -o true false", []string{"-d", ".", "-o", "-f", "no_such_xyz"}, 0},
		{"parens simple", []string{"(", "hello", ")"}, 0},
		{"-t 0 in test", []string{"-t", "0"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testutil.TestEnv()
			b := builtin.Builtins["test"]
			code, err := b(tt.args, env)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if code != tt.wantCode {
				t.Errorf("builtinTest(%v) = %d, want %d", tt.args, code, tt.wantCode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// builtinEval
// ---------------------------------------------------------------------------

func TestBuiltinEval(t *testing.T) {
	t.Run("eval echo hello", func(t *testing.T) {
		env := testutil.TestEnv()
		got := testutil.CaptureOutput(env, func() {
			b := builtin.Builtins["eval"]
			code, err := b([]string{"echo", "hello"}, env)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if code != 0 {
				t.Fatalf("expected exit code 0, got %d", code)
			}
		})
		if got != "hello\n" {
			t.Errorf("builtinEval got %q, want %q", got, "hello\n")
		}
	})

	t.Run("eval with no args", func(t *testing.T) {
		env := testutil.TestEnv()
		b := builtin.Builtins["eval"]
		code, err := b([]string{}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	})

	t.Run("eval expression returns value", func(t *testing.T) {
		env := testutil.TestEnv()
		got := testutil.CaptureOutput(env, func() {
			b := builtin.Builtins["eval"]
			code, err := b([]string{"1", "+", "2"}, env)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if code != 0 {
				t.Fatalf("expected exit code 0, got %d", code)
			}
		})
		if got != "3\n" {
			t.Errorf("builtinEval(1 + 2) got %q, want %q", got, "3\n")
		}
	})
}

// ---------------------------------------------------------------------------
// builtinPwd
// ---------------------------------------------------------------------------

func TestBuiltinPwd(t *testing.T) {
	env := testutil.TestEnv()
	cwd, _ := os.Getwd()
	got := testutil.CaptureOutput(env, func() {
		b := builtin.Builtins["pwd"]
		code, err := b(nil, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	})
	if strings.TrimSpace(got) != cwd {
		t.Errorf("builtinPwd = %q, want %q", strings.TrimSpace(got), cwd)
	}
}

// ---------------------------------------------------------------------------
// builtinType
// ---------------------------------------------------------------------------

func TestBuiltinTypeBuiltin(t *testing.T) {
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		b := builtin.Builtins["type"]
		b([]string{"echo"}, env)
	})
	if !strings.Contains(got, "shell builtin") {
		t.Errorf("expected 'shell builtin' for echo, got %q", got)
	}
}

func TestBuiltinTypeFunction(t *testing.T) {
	env := testutil.TestEnv()
	env.AddFnClauses("myfunc", &core.FnValue{Name: "myfunc", Clauses: []core.FnClause{{}}})
	got := testutil.CaptureOutput(env, func() {
		b := builtin.Builtins["type"]
		b([]string{"myfunc"}, env)
	})
	if !strings.Contains(got, "function") {
		t.Errorf("expected 'function' for myfunc, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// builtinTimes
// ---------------------------------------------------------------------------

func TestBuiltinTimes(t *testing.T) {
	env := testutil.TestEnv()
	got := testutil.CaptureOutput(env, func() {
		b := builtin.Builtins["times"]
		b(nil, env)
	})
	if !strings.Contains(got, "0m0.000s") {
		t.Errorf("expected times output, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// builtinAlias / builtinUnalias
// ---------------------------------------------------------------------------

func TestBuiltinAlias(t *testing.T) {
	t.Run("set and list alias", func(t *testing.T) {
		env := testutil.TestEnv()
		b := builtin.Builtins["alias"]
		code, err := b([]string{"ll=ls -la"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		v, ok := env.Ctx.Shell.GetAlias("ll")
		if !ok {
			t.Fatal("expected alias ll to be set")
		}
		if v != "ls -la" {
			t.Errorf("alias ll = %q, want %q", v, "ls -la")
		}
	})

	t.Run("list specific alias", func(t *testing.T) {
		env := testutil.TestEnv()
		env.Ctx.Shell.SetAlias("ll", "ls -la")
		got := testutil.CaptureOutput(env, func() {
			b := builtin.Builtins["alias"]
			b([]string{"ll"}, env)
		})
		if !strings.Contains(got, "alias ll='ls -la'") {
			t.Errorf("expected alias listing, got %q", got)
		}
	})
}

func TestBuiltinUnalias(t *testing.T) {
	t.Run("remove alias", func(t *testing.T) {
		env := testutil.TestEnv()
		env.Ctx.Shell.SetAlias("ll", "ls -la")
		b := builtin.Builtins["unalias"]
		b([]string{"ll"}, env)
		if _, ok := env.Ctx.Shell.GetAlias("ll"); ok {
			t.Error("expected alias ll to be removed")
		}
	})

	t.Run("remove all aliases with -a", func(t *testing.T) {
		env := testutil.TestEnv()
		env.Ctx.Shell.SetAlias("ll", "ls -la")
		env.Ctx.Shell.SetAlias("gs", "git status")
		b := builtin.Builtins["unalias"]
		b([]string{"-a"}, env)
		if len(env.Ctx.Shell.AllAliases()) != 0 {
			t.Error("expected all aliases to be removed")
		}
	})
}

// ---------------------------------------------------------------------------
// builtinCommand
// ---------------------------------------------------------------------------

func TestBuiltinCommand(t *testing.T) {
	t.Run("command -v builtin", func(t *testing.T) {
		env := testutil.TestEnv()
		got := testutil.CaptureOutput(env, func() {
			b := builtin.Builtins["command"]
			code, err := b([]string{"-v", "echo"}, env)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if code != 0 {
				t.Fatalf("expected exit code 0, got %d", code)
			}
		})
		if strings.TrimSpace(got) != "echo" {
			t.Errorf("command -v echo = %q, want %q", strings.TrimSpace(got), "echo")
		}
	})

	t.Run("command -v nonexistent", func(t *testing.T) {
		env := testutil.TestEnv()
		b := builtin.Builtins["command"]
		code, _ := b([]string{"-v", "nonexistent_cmd_xyz"}, env)
		if code != 1 {
			t.Errorf("expected exit code 1, got %d", code)
		}
	})

	t.Run("command runs builtin directly", func(t *testing.T) {
		env := testutil.TestEnv()
		got := testutil.CaptureOutput(env, func() {
			b := builtin.Builtins["command"]
			code, err := b([]string{"echo", "hello"}, env)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if code != 0 {
				t.Fatalf("expected exit code 0, got %d", code)
			}
		})
		if got != "hello\n" {
			t.Errorf("command echo hello = %q, want %q", got, "hello\n")
		}
	})
}

func TestBuiltinPrintfFormats(t *testing.T) {
	env := testutil.TestEnv()
	tests := []struct {
		input string
		want  string
	}{
		{`printf "%e" 1.5`, "1.500000e+00"},
		{`printf "%E" 1.5`, "1.500000E+00"},
		{`printf "%g" 1.5`, "1.5"},
		{`printf "%G" 1.5`, "1.5"},
		{`printf "%u" 42`, "42"},
	}
	for _, tt := range tests {
		got := testutil.CaptureOutput(env, func() {
			testutil.RunSource(tt.input, env)
		})
		if got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.input, got, tt.want)
		}
	}
}
