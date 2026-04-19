package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ish/internal/core"
)

func TestCompleterDotSlashPrefix(t *testing.T) {
	// Create a temp dir with a known file
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "script.sh"), []byte("#!/bin/sh"), 0755)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	env := core.NewEnv(nil)
	env.Set("PATH", core.StringVal(""))
	completer := makeCompleter(env)

	t.Run("dot-slash preserves prefix", func(t *testing.T) {
		candidates := completer("./scr", false)
		if len(candidates) == 0 {
			t.Fatal("expected at least one candidate")
		}
		for _, c := range candidates {
			if !strings.HasPrefix(c, "./") {
				t.Errorf("candidate %q missing ./ prefix", c)
			}
		}
	})

	t.Run("dot-slash dir gets trailing slash", func(t *testing.T) {
		candidates := completer("./sub", false)
		if len(candidates) == 0 {
			t.Fatal("expected at least one candidate")
		}
		found := false
		for _, c := range candidates {
			if c == "./subdir/" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected ./subdir/ in candidates, got %v", candidates)
		}
	})

	t.Run("dotdot path works", func(t *testing.T) {
		sub := filepath.Join(dir, "inner")
		os.Mkdir(sub, 0755)
		os.Chdir(sub)
		defer os.Chdir(dir)

		candidates := completer("../scr", false)
		if len(candidates) == 0 {
			t.Fatal("expected at least one candidate for ../scr")
		}
		for _, c := range candidates {
			if !strings.HasPrefix(c, "../") {
				t.Errorf("candidate %q missing ../ prefix", c)
			}
		}
	})
}
