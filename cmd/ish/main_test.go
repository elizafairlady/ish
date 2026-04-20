package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"ish/internal/core"
	"ish/internal/stdlib"
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

func TestCompleterModules(t *testing.T) {
	env := core.TopEnv()
	stdlib.Register(env)
	completer := makeCompleter(env)

	t.Run("bare module name completes with dot", func(t *testing.T) {
		candidates := completer("JS", true)
		found := false
		for _, c := range candidates {
			if c == "JSON." {
				found = true
			}
		}
		if !found {
			t.Errorf("expected JSON. in candidates, got %v", candidates)
		}
	})

	t.Run("module name does not end with space", func(t *testing.T) {
		candidates := completer("JS", true)
		for _, c := range candidates {
			if strings.HasPrefix(c, "JSON") && strings.HasSuffix(c, " ") {
				t.Errorf("candidate %q should not end with space", c)
			}
		}
	})

	t.Run("module dot completes function names", func(t *testing.T) {
		candidates := completer("List.", true)
		if len(candidates) == 0 {
			t.Fatal("expected function candidates for List.")
		}
		found := false
		for _, c := range candidates {
			if c == "List.map" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected List.map in candidates, got %v", candidates)
		}
	})

	t.Run("module dot with prefix filters", func(t *testing.T) {
		candidates := completer("List.ma", true)
		for _, c := range candidates {
			if !strings.HasPrefix(c, "List.ma") {
				t.Errorf("candidate %q doesn't match prefix List.ma", c)
			}
		}
		found := false
		for _, c := range candidates {
			if c == "List.map" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected List.map in candidates, got %v", candidates)
		}
	})

	t.Run("unknown module returns no module completions", func(t *testing.T) {
		candidates := completer("FakeModule.", true)
		for _, c := range candidates {
			if strings.HasPrefix(c, "FakeModule.") {
				t.Errorf("unexpected candidate %q for unknown module", c)
			}
		}
	})

	t.Run("all registered modules are completable", func(t *testing.T) {
		modules := []string{"Kernel", "List", "String", "Map", "Math", "Regex", "Path", "Process", "JSON", "CSV", "IO"}
		for _, mod := range modules {
			prefix := mod[:2]
			candidates := completer(prefix, true)
			found := false
			for _, c := range candidates {
				if c == mod+"." {
					found = true
				}
			}
			if !found {
				t.Errorf("module %s not completable from prefix %q, got %v", mod, prefix, candidates)
			}
		}
	})

	t.Run("Kernel auto-imported functions are completable bare", func(t *testing.T) {
		candidates := completer("is_int", true)
		found := false
		for _, c := range candidates {
			if c == "is_integer" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected is_integer in candidates, got %v", candidates)
		}
	})
}

func TestReadlineNoSpaceAfterDot(t *testing.T) {
	// Verify the contract: candidates ending in "." should not get a trailing space.
	// This tests the invariant that the readline layer checks for.
	candidates := []string{"JSON."}
	for _, c := range candidates {
		if strings.HasSuffix(c, "/") || strings.HasSuffix(c, ".") {
			// readline should NOT append space — this is the desired behavior
		} else {
			t.Errorf("candidate %q would get a trailing space from readline", c)
		}
	}

	// Also verify candidates from actual completer end with "."
	env := core.TopEnv()
	stdlib.Register(env)
	completer := makeCompleter(env)
	results := completer("JSON", true)
	sort.Strings(results)
	foundDot := false
	for _, r := range results {
		if r == "JSON." {
			foundDot = true
		}
	}
	if !foundDot {
		t.Errorf("JSON completion should produce 'JSON.' candidate, got %v", results)
	}
}
