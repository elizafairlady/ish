package stdlib_test

import (
	"os"
	"path/filepath"
	"testing"

	"ish/internal/core"
	"ish/internal/testutil"
)

func TestPathBasename(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Path.basename "/foo/bar/baz.txt"`)
	got, _ := env.Get("result")
	if got.Kind != core.VString || got.Str != "baz.txt" {
		t.Errorf("basename = %s, want baz.txt", got.Inspect())
	}
}

func TestPathDirname(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Path.dirname "/foo/bar/baz.txt"`)
	got, _ := env.Get("result")
	if got.Kind != core.VString || got.Str != "/foo/bar" {
		t.Errorf("dirname = %s, want /foo/bar", got.Inspect())
	}
}

func TestPathExtname(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `
r1 = Path.extname "baz.txt"
r2 = Path.extname "README"
`)
	if r, _ := env.Get("r1"); r.Str != ".txt" {
		t.Errorf("extname r1 = %s", r.Inspect())
	}
	if r, _ := env.Get("r2"); r.Str != "" {
		t.Errorf("extname r2 = %s", r.Inspect())
	}
}

func TestPathJoin(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Path.join("/foo", "bar", "baz.txt")`)
	got, _ := env.Get("result")
	if got.Kind != core.VString || got.Str != filepath.Join("/foo", "bar", "baz.txt") {
		t.Errorf("join = %s", got.Inspect())
	}
}

func TestPathExists(t *testing.T) {
	f, err := os.CreateTemp("", "ish-path-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Close()

	env := testutil.TestEnv()
	evalScript(t, env, `
r1 = Path.exists "`+f.Name()+`"
r2 = Path.exists "/nonexistent/`+f.Name()+`/xyz"
`)
	if r, _ := env.Get("r1"); !r.Equal(core.True) {
		t.Errorf("r1 = %s, want :true", r.Inspect())
	}
	if r, _ := env.Get("r2"); !r.Equal(core.False) {
		t.Errorf("r2 = %s, want :false", r.Inspect())
	}
}

func TestPathAbs(t *testing.T) {
	env := testutil.TestEnv()
	evalScript(t, env, `result = Path.abs "."`)
	got, _ := env.Get("result")
	if got.Kind != core.VString || !filepath.IsAbs(got.Str) {
		t.Errorf("abs = %s, not absolute", got.Inspect())
	}
}
