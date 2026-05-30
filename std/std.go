// Package std embeds the ish standard library and default implementation
// packages into the binary. Packages are folders, Odin/Go style: a package is a
// directory under this tree, and every .ish file in that directory is part of
// the package.
package std

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// The entire std tree is embedded: kernel and impl/kernel are auto-loaded at
// startup, and every other package (e.g. enum) is available to be imported/used
// on demand. New package directories are picked up without editing this list.
//
//go:embed all:*
var files embed.FS

// PackageSource returns the source of an embedded package directory, formed by
// concatenating its .ish files in lexical filename order. dir is the package's
// path relative to this tree (e.g. "impl/kernel"). It is an error if the
// directory has no .ish files.
func PackageSource(dir string) (string, error) {
	entries, err := fs.ReadDir(files, dir)
	if err != nil {
		return "", fmt.Errorf("std: package %q: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".ish") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("std: package %q has no .ish files", dir)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		data, err := files.ReadFile(dir + "/" + name)
		if err != nil {
			return "", fmt.Errorf("std: read %s/%s: %w", dir, name, err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String(), nil
}
