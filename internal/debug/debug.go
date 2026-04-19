package debug

import (
	"fmt"
	"strings"
)

// Frame represents a single call in the stack trace.
type Frame struct {
	FnName string
	Arity  int
	Pos    int
	Source *SourceMap
}

// FormatLocation returns "file:line:col" for this frame.
func (f Frame) FormatLocation() string {
	if f.Source != nil {
		return f.Source.FormatPos(f.Pos)
	}
	return "<unknown>"
}

// Debugger tracks call stacks and manages source maps for debugging.
// When nil, all debugging is disabled with zero overhead.
type Debugger struct {
	stack      []Frame
	sources    []*SourceMap // all pushed source maps
	current    *SourceMap   // active source map
	currentPos int          // updated by Eval on each node
	TraceAll   bool         // enhanced set -X
	StackTrace bool         // set -D: enrich errors with stack traces
}

// New creates a new Debugger.
func New() *Debugger {
	return &Debugger{
		StackTrace: true,
	}
}

// SetNode records the current AST node position. Called at the top of Eval.
func (d *Debugger) SetNode(pos int) {
	d.currentPos = pos
}

// PushFrame pushes a call frame onto the stack.
// It uses currentPos and current source map set by SetNode.
func (d *Debugger) PushFrame(name string, arity int) {
	d.stack = append(d.stack, Frame{
		FnName: name,
		Arity:  arity,
		Pos:    d.currentPos,
		Source: d.current,
	})
}

// PopFrame removes the top frame from the stack.
func (d *Debugger) PopFrame() {
	if len(d.stack) > 0 {
		d.stack = d.stack[:len(d.stack)-1]
	}
}

// PushSource pushes a new source map as the active source.
func (d *Debugger) PushSource(sm *SourceMap) {
	d.sources = append(d.sources, sm)
	d.current = sm
}

// PopSource restores the previous source map.
func (d *Debugger) PopSource() {
	if len(d.sources) > 0 {
		d.sources = d.sources[:len(d.sources)-1]
	}
	if len(d.sources) > 0 {
		d.current = d.sources[len(d.sources)-1]
	} else {
		d.current = nil
	}
}

// CurrentSource returns the active source map.
func (d *Debugger) CurrentSource() *SourceMap {
	return d.current
}

// CopyForSpawn creates a new Debugger for a spawned process.
// Fresh stack, shared source maps (read-only), same settings.
func (d *Debugger) CopyForSpawn() *Debugger {
	cp := &Debugger{
		sources:    d.sources,
		current:    d.current,
		TraceAll:   d.TraceAll,
		StackTrace: d.StackTrace,
	}
	return cp
}

// WrapError wraps an error with the current stack trace.
// If the error is already a TraceError, it is returned as-is to avoid double-wrapping.
func (d *Debugger) WrapError(err error) error {
	if !d.StackTrace || len(d.stack) == 0 {
		return err
	}
	// Don't double-wrap
	if _, ok := err.(*TraceError); ok {
		return err
	}
	frames := make([]Frame, len(d.stack))
	copy(frames, d.stack)
	return &TraceError{Err: err, Frames: frames}
}

// FormatStack formats the current stack as a string.
func (d *Debugger) FormatStack() string {
	if len(d.stack) == 0 {
		return ""
	}
	var b strings.Builder
	// Print from bottom (outermost) to top (innermost)
	for i := len(d.stack) - 1; i >= 0; i-- {
		f := d.stack[i]
		fmt.Fprintf(&b, "    %s/%d", f.FnName, f.Arity)
		loc := f.FormatLocation()
		if loc != "<unknown>" {
			// Pad to align locations
			name := fmt.Sprintf("%s/%d", f.FnName, f.Arity)
			padding := 14 - len(name)
			if padding < 1 {
				padding = 1
			}
			fmt.Fprintf(&b, "%s%s", strings.Repeat(" ", padding), loc)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TraceError wraps an error with a stack trace.
type TraceError struct {
	Err    error
	Frames []Frame
}

func (e *TraceError) Error() string {
	var b strings.Builder
	b.WriteString(e.Err.Error())
	if len(e.Frames) > 0 {
		b.WriteByte('\n')
		// Print from bottom (outermost) to top (innermost)
		for i := len(e.Frames) - 1; i >= 0; i-- {
			f := e.Frames[i]
			name := fmt.Sprintf("%s/%d", f.FnName, f.Arity)
			fmt.Fprintf(&b, "    %s", name)
			loc := f.FormatLocation()
			if loc != "<unknown>" {
				padding := 14 - len(name)
				if padding < 1 {
					padding = 1
				}
				fmt.Fprintf(&b, "%s%s", strings.Repeat(" ", padding), loc)
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (e *TraceError) Unwrap() error {
	return e.Err
}
