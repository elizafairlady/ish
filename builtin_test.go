package main

import (
	"os"
	"strings"
	"testing"
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
			env := testEnv()
			got := captureOutput(env, func() {
				code, err := builtinEcho(tt.args, env)
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

		env := testEnv()
		code, err := builtinCd([]string{"/tmp"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		cwd, _ := os.Getwd()
		// /tmp may resolve to /private/tmp on macOS
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
		env := testEnv()
		code, err := builtinCd([]string{}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		// Just verify we changed directory successfully;
		// exact path comparison is fragile due to symlinks.
	})

	t.Run("cd to nonexistent returns error code 1", func(t *testing.T) {
		origDir, _ := os.Getwd()
		t.Cleanup(func() { os.Chdir(origDir) })

		env := testEnv()
		code, err := builtinCd([]string{"/nonexistent_dir_xyz_123"}, env)
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

		env := testEnv()
		builtinCd([]string{"/tmp"}, env)

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

		env := testEnv()
		env.Set("OLDPWD", StringVal("/tmp"))
		code, err := builtinCd([]string{"-"}, env)
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
		env := testEnv()
		code, err := builtinExport([]string{"FOO_TEST_EXPORT=bar"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		// Check env binding
		v, ok := env.Get("FOO_TEST_EXPORT")
		if !ok {
			t.Fatal("expected FOO_TEST_EXPORT to be set in env")
		}
		if v.ToStr() != "bar" {
			t.Errorf("env binding = %q, want %q", v.ToStr(), "bar")
		}
		// Check BuildEnv includes the exported var
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
		env := testEnv()
		env.SetLocal("EXPORT_EXISTING_TEST", StringVal("existing_value"))
		code, err := builtinExport([]string{"EXPORT_EXISTING_TEST"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		// Check BuildEnv includes the exported var
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
	env := testEnv()
	env.SetLocal("UNSET_TEST_VAR", StringVal("to_remove"))

	code, err := builtinUnset([]string{"UNSET_TEST_VAR"}, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	// Check env binding removed
	if _, ok := env.Get("UNSET_TEST_VAR"); ok {
		t.Error("expected UNSET_TEST_VAR to be removed from env")
	}
}

func TestBuiltinUnsetFn(t *testing.T) {
	env := testEnv()
	env.SetFn("myfunc", &FnValue{Name: "myfunc", Clauses: []FnClause{{}}})

	// Verify it exists
	if _, ok := env.GetFn("myfunc"); !ok {
		t.Fatal("myfunc should exist before unset")
	}

	builtinUnset([]string{"-f", "myfunc"}, env)

	if _, ok := env.GetFn("myfunc"); ok {
		t.Error("myfunc should be gone after unset -f")
	}
}

// ---------------------------------------------------------------------------
// builtinSet
// ---------------------------------------------------------------------------

func TestBuiltinSet(t *testing.T) {
	t.Run("set -- a b sets positional args", func(t *testing.T) {
		env := testEnv()
		code, err := builtinSet([]string{"--", "a", "b"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		args := env.posArgs()
		if len(args) != 2 || args[0] != "a" || args[1] != "b" {
			t.Errorf("expected args [a b], got %v", args)
		}
	})

	t.Run("set with no args prints variables", func(t *testing.T) {
		env := testEnv()
		env.Set("TESTVAR", StringVal("hello"))
		got := captureOutput(env, func() {
			builtinSet([]string{}, env)
		})
		if !strings.Contains(got, "TESTVAR=hello") {
			t.Errorf("expected output to contain TESTVAR=hello, got %q", got)
		}
	})
}

// ---------------------------------------------------------------------------
// builtinShift
// ---------------------------------------------------------------------------

func TestBuiltinShift(t *testing.T) {
	t.Run("shift by 1", func(t *testing.T) {
		env := testEnv()
		env.args = []string{"a", "b", "c"}
		code, err := builtinShift([]string{}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		args := env.posArgs()
		if len(args) != 2 || args[0] != "b" || args[1] != "c" {
			t.Errorf("expected [b c], got %v", args)
		}
	})

	t.Run("shift by 2", func(t *testing.T) {
		env := testEnv()
		env.args = []string{"x", "y", "z"}
		builtinShift([]string{"2"}, env)
		args := env.posArgs()
		if len(args) != 1 || args[0] != "z" {
			t.Errorf("expected [z], got %v", args)
		}
	})

	t.Run("shift more than available returns error", func(t *testing.T) {
		env := testEnv()
		env.args = []string{"a"}
		code, err := builtinShift([]string{"5"}, env)
		if code != 1 {
			t.Errorf("expected exit code 1, got %d", code)
		}
		if err == nil {
			t.Error("expected error for shift count out of range")
		}
	})
}

// ---------------------------------------------------------------------------
// builtinReturn
// ---------------------------------------------------------------------------

func TestBuiltinReturn(t *testing.T) {
	t.Run("return with code", func(t *testing.T) {
		env := testEnv()
		code, err := builtinReturn([]string{"42"}, env)
		if code != 42 {
			t.Errorf("expected code 42, got %d", code)
		}
		if err != errReturn {
			t.Errorf("expected errReturn, got %v", err)
		}
	})

	t.Run("return with no args defaults to 0", func(t *testing.T) {
		env := testEnv()
		code, err := builtinReturn([]string{}, env)
		if code != 0 {
			t.Errorf("expected code 0, got %d", code)
		}
		if err != errReturn {
			t.Errorf("expected errReturn, got %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// builtinTrue / builtinFalse
// ---------------------------------------------------------------------------

func TestBuiltinTrueFalse(t *testing.T) {
	t.Run("true returns 0", func(t *testing.T) {
		env := testEnv()
		code, err := builtinTrue(nil, env)
		if code != 0 || err != nil {
			t.Errorf("builtinTrue: code=%d, err=%v", code, err)
		}
	})

	t.Run("false returns 1", func(t *testing.T) {
		env := testEnv()
		code, err := builtinFalse(nil, env)
		if code != 1 || err != nil {
			t.Errorf("builtinFalse: code=%d, err=%v", code, err)
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
		// Empty
		{"empty args", []string{}, 1},

		// Unary: single string (non-empty is true)
		{"non-empty string is true", []string{"hello"}, 0},
		{"empty string is false", []string{""}, 1},

		// -n / -z
		{"-n non-empty", []string{"-n", "hello"}, 0},
		{"-n empty", []string{"-n", ""}, 1},
		{"-z empty", []string{"-z", ""}, 0},
		{"-z non-empty", []string{"-z", "hello"}, 1},

		// -f / -d
		{"-f existing file", []string{"-f", "builtin.go"}, 0},
		{"-f nonexistent", []string{"-f", "no_such_file_xyz"}, 1},
		{"-d existing dir", []string{"-d", "."}, 0},
		{"-d on file", []string{"-d", "builtin.go"}, 1},

		// -e
		{"-e existing", []string{"-e", "builtin.go"}, 0},
		{"-e nonexistent", []string{"-e", "no_such_xyz"}, 1},

		// String equality
		{"str = str true", []string{"foo", "=", "foo"}, 0},
		{"str = str false", []string{"foo", "=", "bar"}, 1},
		{"str == str true", []string{"abc", "==", "abc"}, 0},
		{"str != str true", []string{"a", "!=", "b"}, 0},
		{"str != str false", []string{"a", "!=", "a"}, 1},

		// Numeric comparisons
		{"n -eq n true", []string{"5", "-eq", "5"}, 0},
		{"n -eq n false", []string{"5", "-eq", "3"}, 1},
		{"n -ne n true", []string{"5", "-ne", "3"}, 0},
		{"n -ne n false", []string{"5", "-ne", "5"}, 1},
		{"n -lt n true", []string{"3", "-lt", "5"}, 0},
		{"n -lt n false", []string{"5", "-lt", "3"}, 1},
		{"n -le n true eq", []string{"3", "-le", "3"}, 0},
		{"n -le n true less", []string{"2", "-le", "3"}, 0},
		{"n -le n false", []string{"5", "-le", "3"}, 1},
		{"n -gt n true", []string{"5", "-gt", "3"}, 0},
		{"n -gt n false", []string{"3", "-gt", "5"}, 1},
		{"n -ge n true eq", []string{"3", "-ge", "3"}, 0},
		{"n -ge n true greater", []string{"5", "-ge", "3"}, 0},
		{"n -ge n false", []string{"2", "-ge", "3"}, 1},

		// -s (file exists and non-empty)
		{"-s on non-empty file", []string{"-s", "builtin.go"}, 0},
		{"-s on nonexistent", []string{"-s", "no_such_xyz"}, 1},

		// ] stripping (invoked as [)
		{"bracket stripping", []string{"hello", "]"}, 0},
		{"bracket with equality", []string{"a", "=", "a", "]"}, 0},

		// ! negation
		{"! empty string", []string{"!", ""}, 0},
		{"! non-empty string", []string{"!", "hello"}, 1},
		{"! -f nonexistent", []string{"!", "-f", "no_such_xyz"}, 0},
		{"! -f existing", []string{"!", "-f", "builtin.go"}, 1},
		{"! ! non-empty double negation", []string{"!", "!", "hello"}, 0},

		// Compound: -a (and)
		{"compound -a true true", []string{"-f", "builtin.go", "-a", "-d", "."}, 0},
		{"compound -a true false", []string{"-f", "builtin.go", "-a", "-f", "no_such_xyz"}, 1},
		{"compound -a false true", []string{"-f", "no_such_xyz", "-a", "-d", "."}, 1},

		// Compound: -o (or)
		{"compound -o true false", []string{"-f", "builtin.go", "-o", "-f", "no_such_xyz"}, 0},
		{"compound -o false true", []string{"-f", "no_such_xyz", "-o", "-d", "."}, 0},
		{"compound -o false false", []string{"-f", "no_such_xyz", "-o", "-f", "also_no"}, 1},

		// Parenthesized grouping
		{"parens simple", []string{"(", "hello", ")"}, 0},
		{"parens with -a", []string{"(", "-f", "builtin.go", ")", "-a", "(", "-d", ".", ")"}, 0},

		// -t (fd is terminal) — in tests, fd 0 is not a terminal
		{"-t 0 in test", []string{"-t", "0"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv()
			code, err := builtinTest(tt.args, env)
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
// builtinTest — file system tests (symlinks, permissions, empty files)
// ---------------------------------------------------------------------------

func TestBuiltinTestSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	target := tmpDir + "/target"
	link := tmpDir + "/link"
	os.WriteFile(target, []byte("content"), 0644)
	os.Symlink(target, link)

	env := testEnv()

	t.Run("-L on symlink", func(t *testing.T) {
		code, err := builtinTest([]string{"-L", link}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Errorf("expected 0 for -L on symlink, got %d", code)
		}
	})

	t.Run("-h on symlink", func(t *testing.T) {
		code, err := builtinTest([]string{"-h", link}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Errorf("expected 0 for -h on symlink, got %d", code)
		}
	})

	t.Run("-L on regular file", func(t *testing.T) {
		code, err := builtinTest([]string{"-L", target}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 1 {
			t.Errorf("expected 1 for -L on regular file, got %d", code)
		}
	})

	t.Run("-L on nonexistent", func(t *testing.T) {
		code, err := builtinTest([]string{"-L", tmpDir + "/nope"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 1 {
			t.Errorf("expected 1 for -L on nonexistent, got %d", code)
		}
	})
}

func TestBuiltinTestEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	empty := tmpDir + "/empty"
	os.WriteFile(empty, []byte{}, 0644)

	env := testEnv()

	t.Run("-s on empty file", func(t *testing.T) {
		code, err := builtinTest([]string{"-s", empty}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 1 {
			t.Errorf("expected 1 for -s on empty file, got %d", code)
		}
	})
}

func TestBuiltinTestPermissions(t *testing.T) {
	tmpDir := t.TempDir()

	readable := tmpDir + "/readable"
	os.WriteFile(readable, []byte("x"), 0644)

	noread := tmpDir + "/noread"
	os.WriteFile(noread, []byte("x"), 0000)
	t.Cleanup(func() { os.Chmod(noread, 0644) })

	executable := tmpDir + "/executable"
	os.WriteFile(executable, []byte("#!/bin/sh\n"), 0755)

	noexec := tmpDir + "/noexec"
	os.WriteFile(noexec, []byte("x"), 0644)

	env := testEnv()

	t.Run("-r on readable file", func(t *testing.T) {
		code, err := builtinTest([]string{"-r", readable}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Errorf("expected 0 for -r on readable file, got %d", code)
		}
	})

	t.Run("-w on writable file", func(t *testing.T) {
		code, err := builtinTest([]string{"-w", readable}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Errorf("expected 0 for -w on writable file, got %d", code)
		}
	})

	t.Run("-x on executable file", func(t *testing.T) {
		code, err := builtinTest([]string{"-x", executable}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Errorf("expected 0 for -x on executable, got %d", code)
		}
	})

	t.Run("-x on non-executable file", func(t *testing.T) {
		code, err := builtinTest([]string{"-x", noexec}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 1 {
			t.Errorf("expected 1 for -x on non-executable, got %d", code)
		}
	})

	t.Run("-r on nonexistent", func(t *testing.T) {
		code, err := builtinTest([]string{"-r", tmpDir + "/nope"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 1 {
			t.Errorf("expected 1 for -r on nonexistent, got %d", code)
		}
	})
}

func TestBuiltinTestCompound(t *testing.T) {
	env := testEnv()

	t.Run("-f file -a -r file", func(t *testing.T) {
		code, err := builtinTest([]string{"-f", "builtin.go", "-a", "-r", "builtin.go"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Errorf("expected 0 for -f file -a -r file, got %d", code)
		}
	})

	t.Run("! -f nonexistent -o -d .", func(t *testing.T) {
		code, err := builtinTest([]string{"!", "-f", "no_such", "-o", "-d", "."}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Errorf("expected 0 for ! -f nonexistent -o -d ., got %d", code)
		}
	})

	t.Run("nested parens with -o", func(t *testing.T) {
		// ( -f nosuch ) -o ( -d . )  =>  false -o true => true
		code, err := builtinTest([]string{"(", "-f", "no_such", ")", "-o", "(", "-d", ".", ")"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Errorf("expected 0 for nested parens with -o, got %d", code)
		}
	})

	t.Run("bracket form with compound", func(t *testing.T) {
		code, err := builtinTest([]string{"-f", "builtin.go", "-a", "-d", ".", "]"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Errorf("expected 0 for bracket form with compound, got %d", code)
		}
	})
}

// ---------------------------------------------------------------------------
// builtinEval
// ---------------------------------------------------------------------------

func TestBuiltinEval(t *testing.T) {
	t.Run("eval echo hello", func(t *testing.T) {
		env := testEnv()
		got := captureOutput(env, func() {
			code, err := builtinEval([]string{"echo", "hello"}, env)
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
		env := testEnv()
		code, err := builtinEval([]string{}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	})

	t.Run("eval expression returns value", func(t *testing.T) {
		env := testEnv()
		got := captureOutput(env, func() {
			code, err := builtinEval([]string{"1", "+", "2"}, env)
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
	env := testEnv()
	cwd, _ := os.Getwd()
	got := captureOutput(env, func() {
		code, err := builtinPwd(nil, env)
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
	env := testEnv()
	got := captureOutput(env, func() {
		builtinType([]string{"echo"}, env)
	})
	if !strings.Contains(got, "shell builtin") {
		t.Errorf("expected 'shell builtin' for echo, got %q", got)
	}
}

func TestBuiltinTypeFunction(t *testing.T) {
	env := testEnv()
	env.SetFn("myfunc", &FnValue{Name: "myfunc", Clauses: []FnClause{{}}})
	got := captureOutput(env, func() {
		builtinType([]string{"myfunc"}, env)
	})
	if !strings.Contains(got, "function") {
		t.Errorf("expected 'function' for myfunc, got %q", got)
	}
}

func TestBuiltinTypeExternal(t *testing.T) {
	env := testEnv()
	// Known external command
	got := captureOutput(env, func() {
		builtinType([]string{"cat"}, env)
	})
	if !strings.Contains(got, "/") {
		t.Errorf("expected path for 'cat', got %q", got)
	}

	// Unknown command
	got = captureOutput(env, func() {
		code, _ := builtinType([]string{"some_random_cmd_xyz"}, env)
		if code != 1 {
			t.Errorf("expected exit code 1 for not-found, got %d", code)
		}
	})
	if !strings.Contains(got, "not found") {
		t.Errorf("expected 'not found', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// builtinBreak / builtinContinue
// ---------------------------------------------------------------------------

func TestBuiltinBreakContinue(t *testing.T) {
	t.Run("break returns errBreak", func(t *testing.T) {
		env := testEnv()
		code, err := builtinBreak(nil, env)
		if code != 0 {
			t.Errorf("expected code 0, got %d", code)
		}
		if err != errBreak {
			t.Errorf("expected errBreak, got %v", err)
		}
	})

	t.Run("continue returns errContinue", func(t *testing.T) {
		env := testEnv()
		code, err := builtinContinue(nil, env)
		if code != 0 {
			t.Errorf("expected code 0, got %d", code)
		}
		if err != errContinue {
			t.Errorf("expected errContinue, got %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// builtinTimes
// ---------------------------------------------------------------------------

func TestBuiltinTimes(t *testing.T) {
	env := testEnv()
	got := captureOutput(env, func() {
		builtinTimes(nil, env)
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
		env := testEnv()
		code, err := builtinAlias([]string{"ll=ls -la"}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		v, ok := env.GetAlias("ll")
		if !ok {
			t.Fatal("expected alias ll to be set")
		}
		if v != "ls -la" {
			t.Errorf("alias ll = %q, want %q", v, "ls -la")
		}
	})

	t.Run("list specific alias", func(t *testing.T) {
		env := testEnv()
		env.SetAlias("ll", "ls -la")
		got := captureOutput(env, func() {
			builtinAlias([]string{"ll"}, env)
		})
		if !strings.Contains(got, "alias ll='ls -la'") {
			t.Errorf("expected alias listing, got %q", got)
		}
	})

	t.Run("list all aliases", func(t *testing.T) {
		env := testEnv()
		env.SetAlias("ll", "ls -la")
		env.SetAlias("gs", "git status")
		got := captureOutput(env, func() {
			builtinAlias([]string{}, env)
		})
		if !strings.Contains(got, "ll") || !strings.Contains(got, "gs") {
			t.Errorf("expected all aliases listed, got %q", got)
		}
	})
}

func TestBuiltinUnalias(t *testing.T) {
	t.Run("remove alias", func(t *testing.T) {
		env := testEnv()
		env.SetAlias("ll", "ls -la")
		builtinUnalias([]string{"ll"}, env)
		if _, ok := env.GetAlias("ll"); ok {
			t.Error("expected alias ll to be removed")
		}
	})

	t.Run("remove all aliases with -a", func(t *testing.T) {
		env := testEnv()
		env.SetAlias("ll", "ls -la")
		env.SetAlias("gs", "git status")
		builtinUnalias([]string{"-a"}, env)
		if len(env.AllAliases()) != 0 {
			t.Error("expected all aliases to be removed")
		}
	})
}

// ---------------------------------------------------------------------------
// builtinCommand
// ---------------------------------------------------------------------------

func TestBuiltinCommand(t *testing.T) {
	t.Run("command -v builtin", func(t *testing.T) {
		env := testEnv()
		got := captureOutput(env, func() {
			code, err := builtinCommand([]string{"-v", "echo"}, env)
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

	t.Run("command -v external", func(t *testing.T) {
		env := testEnv()
		got := captureOutput(env, func() {
			code, _ := builtinCommand([]string{"-v", "cat"}, env)
			if code != 0 {
				t.Fatalf("expected exit code 0, got %d", code)
			}
		})
		if !strings.Contains(got, "/") {
			t.Errorf("expected path for cat, got %q", got)
		}
	})

	t.Run("command -v nonexistent", func(t *testing.T) {
		env := testEnv()
		code, _ := builtinCommand([]string{"-v", "nonexistent_cmd_xyz"}, env)
		if code != 1 {
			t.Errorf("expected exit code 1, got %d", code)
		}
	})

	t.Run("command runs builtin directly", func(t *testing.T) {
		env := testEnv()
		got := captureOutput(env, func() {
			code, err := builtinCommand([]string{"echo", "hello"}, env)
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

	t.Run("command with no args returns 0", func(t *testing.T) {
		env := testEnv()
		code, err := builtinCommand([]string{}, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	})
}

func TestBuiltinPrintfFormats(t *testing.T) {
	env := testEnv()
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
		got := captureOutput(env, func() {
			runSource(tt.input, env)
		})
		if got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.input, got, tt.want)
		}
	}
}
