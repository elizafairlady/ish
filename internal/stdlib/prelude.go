package stdlib

import (
	"embed"
	"sort"
	"strings"

	"ish/internal/core"
)

//go:embed prelude/*.ish
var preludeFS embed.FS

// LoadPrelude evaluates every embedded prelude file against env by calling
// runFn. Files are processed in filename-sorted order for determinism.
// runFn is typically a wrapper around eval.RunSource; passing it as a
// callback avoids a stdlib -> eval import cycle.
func LoadPrelude(env *core.Env, runFn func(src string, env *core.Env)) {
	entries, err := preludeFS.ReadDir("prelude")
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".ish") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		b, err := preludeFS.ReadFile("prelude/" + name)
		if err != nil {
			continue
		}
		runFn(string(b), env)
	}
}
