package debug

import (
	"fmt"
	"sort"
)

// SourceMap converts byte offsets to line:col positions on demand.
// It is read-only after construction and safe to share across goroutines.
type SourceMap struct {
	Filename string
	src      string
	lines    []int // lines[i] = byte offset where line i+1 starts
}

// NewSourceMap builds a line table by scanning src for newlines.
func NewSourceMap(filename, src string) *SourceMap {
	lines := []int{0} // line 1 starts at offset 0
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			lines = append(lines, i+1)
		}
	}
	return &SourceMap{Filename: filename, src: src, lines: lines}
}

// Resolve converts a byte offset to 1-based line and column numbers.
func (sm *SourceMap) Resolve(pos int) (line, col int) {
	if pos < 0 {
		return 1, 1
	}
	// Binary search: find the last line whose start offset <= pos
	idx := sort.Search(len(sm.lines), func(i int) bool {
		return sm.lines[i] > pos
	}) - 1
	if idx < 0 {
		idx = 0
	}
	return idx + 1, pos - sm.lines[idx] + 1
}

// FormatPos formats a byte offset as "filename:line:col" or "line:col".
func (sm *SourceMap) FormatPos(pos int) string {
	line, col := sm.Resolve(pos)
	if sm.Filename != "" {
		return fmt.Sprintf("%s:%d:%d", sm.Filename, line, col)
	}
	return fmt.Sprintf("%d:%d", line, col)
}
