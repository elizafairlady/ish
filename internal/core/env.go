package core

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
)

// CmdSubFunc is a callback for command substitution inside string expansion.
type CmdSubFunc func(cmd string, scope Scope) (string, error)

// DebuggerCopier is implemented by the debugger to support spawn.
type DebuggerCopier interface {
	CopyForSpawn() interface{}
}

const MaxFlatBindings = 4

// ---------------------------------------------------------------------------
// ExecCtx: shared execution context across all scopes in one execution.
// Holds everything that is shared state, not lexically scoped: I/O,
// exit codes, shell identity, flags, traps, aliases, process, positional
// args. Frames and Envs share the same ExecCtx pointer. Redirects create
// a child ExecCtx with different Stdout. Subshells copy the whole thing.
// ---------------------------------------------------------------------------

// ShellState holds mutable map state that must be shared across redirect
// boundaries. ExecCtx holds a pointer to this; ForRedirect copies the
// pointer so both parent and child mutate the same maps. Copy (for
// subshells) deep-copies the maps for isolation.
type ShellState struct {
	SetFlags    map[byte]bool
	Traps       map[string]string
	Aliases     map[string]string
	ReadonlySet map[string]bool
	Exported    map[string]bool
}

type ExecCtx struct {
	// I/O and callbacks
	Stdout   io.Writer
	Debugger interface{}
	CallFn   func(fn *FnValue, args []Value, scope Scope) (Value, error)
	CmdSub   CmdSubFunc

	// Exit state (mutex-protected for concurrent pipe stages)
	ExitMu   sync.Mutex
	LastExit int
	HasExit  bool
	ExprMode int

	// Shell identity (scalars — copy by value in ForRedirect)
	Proc         Pid
	ShellPid     int
	ShellName    string
	LastBg       int
	IsLoginShell bool

	// Positional parameters and source tracking (scalars/slices)
	Args       []string
	Source     string
	SourceName string

	// Shared mutable state (pointer — survives ForRedirect)
	Shell *ShellState
}

func (c *ExecCtx) SetExit(code int) {
	c.ExitMu.Lock()
	c.LastExit = code
	c.HasExit = true
	c.ExitMu.Unlock()
}

func (c *ExecCtx) ExitCode() int {
	c.ExitMu.Lock()
	defer c.ExitMu.Unlock()
	if c.HasExit {
		return c.LastExit
	}
	return 0
}

func (c *ExecCtx) ShouldExitOnError() bool {
	return c.Shell.HasFlag('e') && c.ExitCode() != 0
}

func (c *ExecCtx) EnterExprMode() func() {
	c.ExprMode++
	return func() { c.ExprMode-- }
}

func (c *ExecCtx) InExprMode() bool { return c.ExprMode > 0 }

// ShellState methods — delegate to shared Shell pointer

func (s *ShellState) HasFlag(flag byte) bool {
	if s.SetFlags == nil { return false }
	return s.SetFlags[flag]
}

func (s *ShellState) SetFlag(flag byte, on bool) {
	if s.SetFlags == nil { s.SetFlags = make(map[byte]bool) }
	s.SetFlags[flag] = on
}

func (s *ShellState) GetTrap(sig string) (string, bool) {
	if s.Traps == nil { return "", false }
	cmd, ok := s.Traps[sig]
	return cmd, ok
}

func (s *ShellState) SetTrap(sig, cmd string) {
	if s.Traps == nil { s.Traps = make(map[string]string) }
	s.Traps[sig] = cmd
}

func (s *ShellState) DeleteTrap(sig string) {
	if s.Traps != nil { delete(s.Traps, sig) }
}

func (s *ShellState) AllTraps(fn func(sig, cmd string)) {
	for sig, cmd := range s.Traps { fn(sig, cmd) }
}

func (s *ShellState) GetAlias(name string) (string, bool) {
	if s.Aliases == nil { return "", false }
	v, ok := s.Aliases[name]
	return v, ok
}

func (s *ShellState) SetAlias(name, value string) {
	if s.Aliases == nil { s.Aliases = make(map[string]string) }
	s.Aliases[name] = value
}

func (s *ShellState) DeleteAlias(name string) {
	if s.Aliases != nil { delete(s.Aliases, name) }
}

func (s *ShellState) AllAliases() map[string]string {
	if s.Aliases == nil { return nil }
	result := make(map[string]string, len(s.Aliases))
	for k, v := range s.Aliases { result[k] = v }
	return result
}

func (s *ShellState) IsReadonly(name string) bool {
	if s.ReadonlySet == nil { return false }
	return s.ReadonlySet[name]
}

func (s *ShellState) SetReadonly(name string) {
	if s.ReadonlySet == nil { s.ReadonlySet = make(map[string]bool) }
	s.ReadonlySet[name] = true
}

func (s *ShellState) AllReadonly(fn func(name string)) {
	for name := range s.ReadonlySet { fn(name) }
}

func (s *ShellState) Export(name, val string) {
	if s.Exported == nil { s.Exported = make(map[string]bool) }
	s.Exported[name] = true
}

func (s *ShellState) ExportName(name string) {
	if s.Exported == nil { s.Exported = make(map[string]bool) }
	s.Exported[name] = true
}

func (s *ShellState) IsExported(name string) bool {
	if s.Exported == nil { return false }
	return s.Exported[name]
}

// Positional params
func (c *ExecCtx) PosArgs() []string { return c.Args }

// Shell identity
func (c *ExecCtx) Pid() int {
	if c.ShellPid != 0 { return c.ShellPid }
	return os.Getpid()
}

func (c *ExecCtx) BgPid() int { return c.LastBg }

func (c *ExecCtx) GetShellName() string {
	if c.ShellName != "" { return c.ShellName }
	return "ish"
}

// ForRedirect creates a child ExecCtx with different Stdout.
// Shell (shared mutable maps) is the same pointer — mutations in the
// redirect child are visible to the parent. Scalars are copied by value.
func (c *ExecCtx) ForRedirect(w io.Writer) *ExecCtx {
	return &ExecCtx{
		Stdout:       w,
		Debugger:     c.Debugger,
		CallFn:       c.CallFn,
		CmdSub:       c.CmdSub,
		LastExit:     c.LastExit,
		HasExit:      c.HasExit,
		ExprMode:     c.ExprMode,
		Proc:         c.Proc,
		ShellPid:     c.ShellPid,
		ShellName:    c.ShellName,
		LastBg:       c.LastBg,
		IsLoginShell: c.IsLoginShell,
		Args:         c.Args,
		Source:       c.Source,
		SourceName:   c.SourceName,
		Shell:        c.Shell, // shared pointer
	}
}

// Copy creates a fully independent ExecCtx for subshells/spawn.
// Shell maps are deep-copied for isolation.
func (c *ExecCtx) Copy() *ExecCtx {
	cp := &ExecCtx{
		Stdout:       c.Stdout,
		CallFn:       c.CallFn,
		CmdSub:       c.CmdSub,
		LastExit:     c.LastExit,
		HasExit:      c.HasExit,
		ExprMode:     c.ExprMode,
		Proc:         c.Proc,
		ShellPid:     c.ShellPid,
		ShellName:    c.ShellName,
		LastBg:       c.LastBg,
		IsLoginShell: c.IsLoginShell,
		Args:         c.Args,
		Source:       c.Source,
		SourceName:   c.SourceName,
		Shell:        c.Shell.Copy(),
	}
	if dc, ok := c.Debugger.(DebuggerCopier); ok {
		cp.Debugger = dc.CopyForSpawn()
	} else {
		cp.Debugger = c.Debugger
	}
	return cp
}

func (s *ShellState) Copy() *ShellState {
	cp := &ShellState{}
	if s == nil { return cp }
	if s.SetFlags != nil {
		cp.SetFlags = make(map[byte]bool, len(s.SetFlags))
		for k, v := range s.SetFlags { cp.SetFlags[k] = v }
	}
	if s.Traps != nil {
		cp.Traps = make(map[string]string, len(s.Traps))
		for k, v := range s.Traps { cp.Traps[k] = v }
	}
	if s.Aliases != nil {
		cp.Aliases = make(map[string]string, len(s.Aliases))
		for k, v := range s.Aliases { cp.Aliases[k] = v }
	}
	if s.ReadonlySet != nil {
		cp.ReadonlySet = make(map[string]bool, len(s.ReadonlySet))
		for k, v := range s.ReadonlySet { cp.ReadonlySet[k] = v }
	}
	if s.Exported != nil {
		cp.Exported = make(map[string]bool, len(s.Exported))
		for k, v := range s.Exported { cp.Exported[k] = v }
	}
	return cp
}

// ---------------------------------------------------------------------------
// Scope: interface for both Frame (lightweight function scope) and Env
// (shell scope). The evaluator works with Scope.
// ---------------------------------------------------------------------------

type Scope interface {
	Get(name string) (Value, bool)
	Set(name string, v Value) error
	SetLocal(name string, v Value) error
	GetParent() Scope
	GetCtx() *ExecCtx
	GetFn(name string) (*FnValue, bool)
	GetNativeFn(name string) (NativeFn, bool)
	GetModule(name string) (*Module, bool)
	NearestEnv() *Env
}

// ---------------------------------------------------------------------------
// Frame: lightweight function scope. Flat-array bindings + parent + Ctx.
// No shell state. No map allocation.
// ---------------------------------------------------------------------------

type Frame struct {
	parent   Scope
	Ctx      *ExecCtx
	flatKeys [MaxFlatBindings]string
	flatVals [MaxFlatBindings]Value
	flatN    int8
	spill    map[string]Value
}

func NewFrame(parent Scope) *Frame {
	f := &Frame{parent: parent}
	if parent != nil {
		f.Ctx = parent.GetCtx()
	}
	return f
}

func (f *Frame) Get(name string) (Value, bool) {
	for i := int8(0); i < f.flatN; i++ {
		if f.flatKeys[i] == name { return f.flatVals[i], true }
	}
	if f.spill != nil {
		if v, ok := f.spill[name]; ok { return v, true }
	}
	if f.parent != nil { return f.parent.Get(name) }
	return Nil, false
}

func (f *Frame) Set(name string, v Value) error {
	for i := int8(0); i < f.flatN; i++ {
		if f.flatKeys[i] == name { f.flatVals[i] = v; return nil }
	}
	if f.spill != nil {
		if _, ok := f.spill[name]; ok { f.spill[name] = v; return nil }
	}
	if f.parent != nil {
		if _, ok := f.parent.Get(name); ok { return f.parent.Set(name, v) }
	}
	return f.SetLocal(name, v)
}

func (f *Frame) SetLocal(name string, v Value) error {
	for i := int8(0); i < f.flatN; i++ {
		if f.flatKeys[i] == name { f.flatVals[i] = v; return nil }
	}
	if f.flatN < MaxFlatBindings {
		f.flatKeys[f.flatN] = name
		f.flatVals[f.flatN] = v
		f.flatN++
		return nil
	}
	if f.spill == nil { f.spill = make(map[string]Value) }
	f.spill[name] = v
	return nil
}

func (f *Frame) GetParent() Scope { return f.parent }
func (f *Frame) GetCtx() *ExecCtx { return f.Ctx }

func (f *Frame) GetFn(name string) (*FnValue, bool) {
	if f.parent != nil { return f.parent.GetFn(name) }
	return nil, false
}

func (f *Frame) GetNativeFn(name string) (NativeFn, bool) {
	if f.parent != nil { return f.parent.GetNativeFn(name) }
	return nil, false
}

func (f *Frame) GetModule(name string) (*Module, bool) {
	if f.parent != nil { return f.parent.GetModule(name) }
	return nil, false
}

func (f *Frame) NearestEnv() *Env {
	if f.parent != nil { return f.parent.NearestEnv() }
	return nil
}

// Init reinitializes a Frame for reuse (from pool or stack).
func (f *Frame) Init(parent Scope) {
	f.parent = parent
	if parent != nil {
		f.Ctx = parent.GetCtx()
	} else {
		f.Ctx = nil
	}
	f.flatN = 0
	f.spill = nil
}

func (f *Frame) ResetFlat() {
	for i := int8(0); i < f.flatN; i++ {
		f.flatKeys[i] = ""
		f.flatVals[i] = Nil
	}
	f.flatN = 0
	f.spill = nil
}

// Snapshot returns a copy of the Frame's bindings as an Env for closure capture.
// Since ish bindings are immutable, this is a value copy — the closure sees
// the bindings as they were at capture time.
func (f *Frame) Snapshot() *Env {
	env := NewEnv(f.parent)
	f.EachBinding(func(name string, val Value) {
		env.Bindings[name] = val
	})
	return env
}

func (f *Frame) EachBinding(fn func(name string, val Value)) {
	for i := int8(0); i < f.flatN; i++ {
		fn(f.flatKeys[i], f.flatVals[i])
	}
	for k, v := range f.spill {
		fn(k, v)
	}
}

// ---------------------------------------------------------------------------
// Env: lexical scope. Bindings + fns + nativeFns + modules + parent + ctx.
// No shell state — that lives on ExecCtx.
// ---------------------------------------------------------------------------

type Env struct {
	Bindings  map[string]Value
	Parent    Scope
	Ctx       *ExecCtx
	Fns       map[string]*FnValue
	NativeFns map[string]NativeFn
	Modules   map[string]*Module
}

func (e *Env) GetParent() Scope {
	if e.Parent != nil { return e.Parent }
	return nil
}
func (e *Env) GetCtx() *ExecCtx { return e.Ctx }
func (e *Env) NearestEnv() *Env { return e }

func NewEnv(parent Scope) *Env {
	env := &Env{
		Bindings: make(map[string]Value),
		Parent:   parent,
	}
	if parent != nil {
		env.Ctx = parent.GetCtx()
	} else {
		env.Ctx = &ExecCtx{Stdout: os.Stdout, Shell: &ShellState{}}
	}
	return env
}

func TopEnv() *Env {
	shell := &ShellState{
		Exported: make(map[string]bool),
	}
	ctx := &ExecCtx{
		Stdout:   os.Stdout,
		ShellPid: os.Getpid(),
		Shell:    shell,
	}
	e := &Env{
		Bindings: make(map[string]Value),
		Ctx:      ctx,
	}
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			e.Bindings[parts[0]] = StringVal(parts[1])
			shell.Exported[parts[0]] = true
		}
	}
	return e
}

func CopyEnv(src *Env) *Env {
	e := &Env{
		Bindings: make(map[string]Value),
		Ctx:      src.Ctx.Copy(),
	}
	var chain []Scope
	for s := Scope(src); s != nil; s = s.GetParent() {
		chain = append(chain, s)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		switch c := chain[i].(type) {
		case *Env:
			for k, v := range c.Bindings { e.Bindings[k] = v }
			for k, v := range c.Fns {
				if e.Fns == nil { e.Fns = make(map[string]*FnValue) }
				copied := &FnValue{Name: v.Name, Clauses: make([]FnClause, len(v.Clauses))}
				copy(copied.Clauses, v.Clauses)
				e.Fns[k] = copied
			}
			if c.NativeFns != nil {
				if e.NativeFns == nil { e.NativeFns = make(map[string]NativeFn) }
				for k, v := range c.NativeFns { e.NativeFns[k] = v }
			}
			if c.Modules != nil {
				if e.Modules == nil { e.Modules = make(map[string]*Module) }
				for k, v := range c.Modules { e.Modules[k] = v }
			}
		case *Frame:
			c.EachBinding(func(k string, v Value) { e.Bindings[k] = v })
		}
	}
	return e
}

// ---------------------------------------------------------------------------
// Env methods: variable access
// ---------------------------------------------------------------------------

func (e *Env) Get(name string) (Value, bool) {
	if v, ok := e.Bindings[name]; ok { return v, true }
	if e.Parent != nil { return e.Parent.Get(name) }
	return Nil, false
}

func (e *Env) Set(name string, v Value) error {
	if e.Ctx.Shell.IsReadonly(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	if _, ok := e.Bindings[name]; ok { e.Bindings[name] = v; return nil }
	if e.Parent != nil {
		if _, ok := e.Parent.Get(name); ok { return e.Parent.Set(name, v) }
	}
	e.Bindings[name] = v
	return nil
}

func (e *Env) SetLocal(name string, v Value) error {
	if e.Ctx.Shell.IsReadonly(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	e.Bindings[name] = v
	return nil
}

func (e *Env) DeleteVar(name string) error {
	if e.Ctx.Shell.IsReadonly(name) {
		return fmt.Errorf("unset: %s: readonly variable", name)
	}
	if _, ok := e.Bindings[name]; ok {
		delete(e.Bindings, name)
		return nil
	}
	if e.Parent != nil {
		if ne := e.Parent.NearestEnv(); ne != nil {
			return ne.DeleteVar(name)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Env methods: function/module access — walk parent Env chain
// ---------------------------------------------------------------------------

func (e *Env) eachEnv(fn func(*Env) bool) {
	for s := Scope(e); s != nil; s = s.GetParent() {
		if env, ok := s.(*Env); ok {
			if !fn(env) { return }
		}
	}
}

func (e *Env) GetFn(name string) (*FnValue, bool) {
	var fn *FnValue
	var found bool
	e.eachEnv(func(c *Env) bool {
		if c.Fns != nil {
			if f, ok := c.Fns[name]; ok { fn = f; found = true; return false }
		}
		return true
	})
	return fn, found
}

func (e *Env) GetNativeFn(name string) (NativeFn, bool) {
	var nfn NativeFn
	var found bool
	e.eachEnv(func(c *Env) bool {
		if c.NativeFns != nil {
			if f, ok := c.NativeFns[name]; ok { nfn = f; found = true; return false }
		}
		return true
	})
	return nfn, found
}

func (e *Env) SetNativeFn(name string, fn NativeFn) {
	if e.NativeFns == nil { e.NativeFns = make(map[string]NativeFn) }
	e.NativeFns[name] = fn
}

func (e *Env) AddFnClauses(name string, f *FnValue) {
	if e.Fns == nil { e.Fns = make(map[string]*FnValue) }
	if existing, ok := e.Fns[name]; ok {
		existing.Clauses = append(existing.Clauses, f.Clauses...)
		return
	}
	e.Fns[name] = f
}

func (e *Env) SetFnClauses(name string, f *FnValue) {
	if e.Fns == nil { e.Fns = make(map[string]*FnValue) }
	e.Fns[name] = f
}

func (e *Env) DeleteFn(name string) {
	e.eachEnv(func(c *Env) bool {
		if c.Fns != nil {
			if _, ok := c.Fns[name]; ok { delete(c.Fns, name); return false }
		}
		return true
	})
}

func (e *Env) GetModule(name string) (*Module, bool) {
	var mod *Module
	var found bool
	e.eachEnv(func(c *Env) bool {
		if c.Modules != nil {
			if m, ok := c.Modules[name]; ok { mod = m; found = true; return false }
		}
		return true
	})
	return mod, found
}

func (e *Env) SetModule(name string, mod *Module) {
	if e.Modules == nil { e.Modules = make(map[string]*Module) }
	if existing, ok := e.Modules[name]; ok {
		if existing.Fns == nil { existing.Fns = make(map[string]*FnValue) }
		for fname, fn := range mod.Fns { existing.Fns[fname] = fn }
		return
	}
	e.Modules[name] = mod
}

func (e *Env) Export(name, val string) {
	e.Set(name, StringVal(val))
	e.Ctx.Shell.Export(name, val)
}

func (e *Env) BuildEnv() []string {
	seen := make(map[string]bool)
	var result []string
	for s := Scope(e); s != nil; s = s.GetParent() {
		if env, ok := s.(*Env); ok {
			for name, val := range env.Bindings {
				if seen[name] { continue }
				seen[name] = true
				if e.Ctx.Shell.IsExported(name) { result = append(result, name+"="+val.ToStr()) }
			}
		} else if frame, ok := s.(*Frame); ok {
			frame.EachBinding(func(name string, val Value) {
				if seen[name] { return }
				seen[name] = true
				if e.Ctx.Shell.IsExported(name) { result = append(result, name+"="+val.ToStr()) }
			})
		}
	}
	return result
}

func (e *Env) ResetFlat() {
	for k := range e.Bindings { delete(e.Bindings, k) }
}

// ---------------------------------------------------------------------------
// String expansion (Expand, ExpandTilde, SplitFieldsIFS, etc.)
// ---------------------------------------------------------------------------

func (e *Env) Expand(s string) string {
	s = e.ExpandTilde(s)
	if !strings.Contains(s, "$") && !strings.Contains(s, "#{") {
		return s
	}

	var buf strings.Builder
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '#' && s[i+1] == '{' {
			i += 2
			start := i
			depth := 1
			for i < len(s) && depth > 0 {
				if s[i] == '{' { depth++ } else if s[i] == '}' { depth-- }
				if depth > 0 { i++ }
			}
			expr := s[start:i]
			if i < len(s) { i++ }
			if v, ok := e.Get(expr); ok {
				buf.WriteString(v.ToStr())
			} else if fn := e.Ctx.CmdSub; fn != nil {
				result, _ := fn(expr, e)
				buf.WriteString(result)
			}
			continue
		}

		if s[i] != '$' {
			buf.WriteByte(s[i])
			i++
			continue
		}
		i++
		if i >= len(s) {
			buf.WriteByte('$')
			break
		}

		switch s[i] {
		case '(':
			if i+1 < len(s) && s[i+1] == '(' {
				i += 2
				start := i
				depth := 1
				for i < len(s) && depth > 0 {
					if i+1 < len(s) && s[i] == ')' && s[i+1] == ')' {
						depth--
						if depth == 0 { break }
						i += 2
					} else { i++ }
				}
				expr := s[start:i]
				if i+1 < len(s) { i += 2 }
				if fn := e.Ctx.CmdSub; fn != nil {
					result, _ := fn("echo $(("+expr+"))", e)
					buf.WriteString(result)
				}
				continue
			}
			i++
			start := i
			depth := 1
			for i < len(s) && depth > 0 {
				if s[i] == '(' { depth++ } else if s[i] == ')' { depth-- }
				if depth > 0 { i++ }
			}
			cmdStr := s[start:i]
			if i < len(s) { i++ }
			if fn := e.Ctx.CmdSub; fn != nil {
				result, _ := fn(cmdStr, e)
				buf.WriteString(result)
			}
		case '?':
			buf.WriteString(Itoa(e.Ctx.ExitCode()))
			i++
		case '$':
			buf.WriteString(Itoa(e.Ctx.Pid()))
			i++
		case '!':
			buf.WriteString(Itoa(e.Ctx.BgPid()))
			i++
		case '#':
			buf.WriteString(Itoa(len(e.Ctx.PosArgs())))
			i++
		case '@':
			buf.WriteString(strings.Join(e.Ctx.PosArgs(), " "))
			i++
		case '*':
			sep := " "
			if ifs, ok := e.Get("IFS"); ok {
				ifsStr := ifs.ToStr()
				if len(ifsStr) > 0 { sep = ifsStr[:1] } else { sep = "" }
			}
			buf.WriteString(strings.Join(e.Ctx.PosArgs(), sep))
			i++
		case '{':
			i++
			start := i
			for i < len(s) && s[i] != '}' { i++ }
			expr := s[start:i]
			if i < len(s) { i++ }
			if len(expr) > 1 && expr[0] == '#' {
				varName := expr[1:]
				if v, ok := e.Get(varName); ok {
					buf.WriteString(strconv.Itoa(len(v.ToStr())))
				} else {
					buf.WriteString("0")
				}
				continue
			}
			if idx := strings.IndexAny(expr, ":-+?="); idx > 0 {
				name := expr[:idx]
				op := expr[idx:]
				hasColon := false
				if op[0] == ':' { hasColon = true; op = op[1:] }
				if len(op) > 0 {
					operator := op[0]
					defaultVal := op[1:]
					v, ok := e.Get(name)
					isEmpty := ok && v.ToStr() == ""
					isUnset := !ok
					switch operator {
					case '-':
						if isUnset || (hasColon && isEmpty) {
							buf.WriteString(e.Expand(defaultVal))
						} else { buf.WriteString(v.ToStr()) }
					case '+':
						if !isUnset && !(hasColon && isEmpty) {
							buf.WriteString(e.Expand(defaultVal))
						}
					case '=':
						if isUnset || (hasColon && isEmpty) {
							expanded := e.Expand(defaultVal)
							e.Set(name, StringVal(expanded))
							buf.WriteString(expanded)
						} else { buf.WriteString(v.ToStr()) }
					case '?':
						if isUnset || (hasColon && isEmpty) {
							fmt.Fprintf(os.Stderr, "ish: %s: %s\n", name, defaultVal)
						} else { buf.WriteString(v.ToStr()) }
					}
				} else {
					if v, ok := e.Get(expr); ok { buf.WriteString(v.ToStr()) }
				}
			} else {
				if handled := e.expandParamOp(expr, &buf); handled { continue }
				if v, ok := e.Get(expr); ok { buf.WriteString(v.ToStr()) }
			}
		default:
			if s[i] >= '0' && s[i] <= '9' {
				start := i
				for i < len(s) && s[i] >= '0' && s[i] <= '9' { i++ }
				idxStr := s[start:i]
				idx, _ := strconv.Atoi(idxStr)
				if idx == 0 {
					buf.WriteString(e.Ctx.GetShellName())
				} else {
					args := e.Ctx.PosArgs()
					if idx > 0 && idx <= len(args) { buf.WriteString(args[idx-1]) }
				}
			} else {
				start := i
				for i < len(s) && IsVarChar(s[i]) { i++ }
				name := s[start:i]
				if name == "" {
					buf.WriteByte('$')
				} else if v, ok := e.Get(name); ok {
					buf.WriteString(v.ToStr())
				}
			}
		}
	}
	return buf.String()
}

func ShellGlobMatch(pattern, s string) bool {
	px, sx := 0, 0
	starPx, starSx := -1, -1
	for sx < len(s) {
		if px < len(pattern) && (pattern[px] == '?' || pattern[px] == s[sx]) {
			px++; sx++
		} else if px < len(pattern) && pattern[px] == '*' {
			starPx = px; starSx = sx; px++
		} else if starPx >= 0 {
			px = starPx + 1; starSx++; sx = starSx
		} else { return false }
	}
	for px < len(pattern) && pattern[px] == '*' { px++ }
	return px == len(pattern)
}

func (e *Env) expandParamOp(expr string, buf *strings.Builder) bool {
	for i := 1; i < len(expr); i++ {
		ch := expr[i]
		if ch == '$' && i+1 < len(expr) && expr[i+1] == '(' {
			depth := 1
			i += 2
			for i < len(expr) && depth > 0 {
				if expr[i] == '(' { depth++ } else if expr[i] == ')' { depth-- }
				i++
			}
			i--
			continue
		}
		if ch == '#' || ch == '%' || ch == '/' {
			name := expr[:i]
			op := expr[i:]
			v, ok := e.Get(name)
			if !ok { return true }
			val := v.ToStr()

			switch {
			case strings.HasPrefix(op, "##"):
				pattern := op[2:]
				for j := len(val); j >= 0; j-- {
					if ShellGlobMatch(pattern, val[:j]) { buf.WriteString(val[j:]); return true }
				}
				buf.WriteString(val); return true
			case ch == '#':
				pattern := op[1:]
				for j := 0; j <= len(val); j++ {
					if ShellGlobMatch(pattern, val[:j]) { buf.WriteString(val[j:]); return true }
				}
				buf.WriteString(val); return true
			case strings.HasPrefix(op, "%%"):
				pattern := op[2:]
				for j := 0; j <= len(val); j++ {
					if ShellGlobMatch(pattern, val[j:]) { buf.WriteString(val[:j]); return true }
				}
				buf.WriteString(val); return true
			case ch == '%':
				pattern := op[1:]
				for j := len(val); j >= 0; j-- {
					if ShellGlobMatch(pattern, val[j:]) { buf.WriteString(val[:j]); return true }
				}
				buf.WriteString(val); return true
			case strings.HasPrefix(op, "//"):
				parts := strings.SplitN(op[2:], "/", 2)
				old := parts[0]; newStr := ""
				if len(parts) > 1 { newStr = parts[1] }
				buf.WriteString(strings.ReplaceAll(val, old, newStr)); return true
			case ch == '/':
				parts := strings.SplitN(op[1:], "/", 2)
				old := parts[0]; newStr := ""
				if len(parts) > 1 { newStr = parts[1] }
				buf.WriteString(strings.Replace(val, old, newStr, 1)); return true
			}
		}
	}
	return false
}

func IsVarChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func (e *Env) ExpandTilde(s string) string {
	if len(s) == 0 || s[0] != '~' { return s }
	if len(s) == 1 || s[1] == '/' {
		home := ""
		if v, ok := e.Get("HOME"); ok { home = v.ToStr() }
		if home == "" { return s }
		return home + s[1:]
	}
	end := strings.IndexByte(s, '/')
	if end < 0 { end = len(s) }
	username := s[1:end]
	u, err := user.Lookup(username)
	if err != nil { return s }
	return u.HomeDir + s[end:]
}

func ExpandTildeStatic(s string) string {
	if len(s) == 0 || s[0] != '~' { return s }
	if len(s) == 1 || s[1] == '/' {
		home := os.Getenv("HOME")
		if home == "" { return s }
		return home + s[1:]
	}
	end := strings.IndexByte(s, '/')
	if end < 0 { end = len(s) }
	username := s[1:end]
	u, err := user.Lookup(username)
	if err != nil { return s }
	return u.HomeDir + s[end:]
}

func (e *Env) SplitFieldsIFS(s string) []string {
	if ifs, ok := e.Get("IFS"); ok {
		ifsStr := ifs.ToStr()
		if ifsStr == "" { return []string{s} }
		return SplitOnChars(s, ifsStr)
	}
	return strings.Fields(s)
}

func SplitOnChars(s, chars string) []string {
	if chars == "" { return []string{s} }
	var wsChars, nwChars []rune
	for _, r := range chars {
		if r == ' ' || r == '\t' || r == '\n' { wsChars = append(wsChars, r) } else { nwChars = append(nwChars, r) }
	}
	isWS := func(r rune) bool { for _, w := range wsChars { if r == w { return true } }; return false }
	isNW := func(r rune) bool { for _, w := range nwChars { if r == w { return true } }; return false }

	s = strings.TrimFunc(s, isWS)
	if s == "" { return nil }

	var fields []string
	var current strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		r := runes[i]
		if isNW(r) {
			fields = append(fields, current.String()); current.Reset(); i++
			for i < len(runes) && isWS(runes[i]) { i++ }
		} else if isWS(r) {
			fields = append(fields, current.String()); current.Reset(); i++
			for i < len(runes) && isWS(runes[i]) { i++ }
		} else { current.WriteRune(r); i++ }
	}
	fields = append(fields, current.String())
	return fields
}

func Itoa(n int) string { return strconv.Itoa(n) }
