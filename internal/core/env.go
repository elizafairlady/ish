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
// Frames and Envs share the same ExecCtx pointer. Redirects create a
// child ExecCtx with different Stdout. Subshells copy the whole thing.
// ---------------------------------------------------------------------------

type ExecCtx struct {
	Stdout   io.Writer
	Debugger interface{}
	CallFn   func(fn *FnValue, args []Value, scope Scope) (Value, error)
	CmdSub   CmdSubFunc
	ExitMu   sync.Mutex
	LastExit int
	HasExit  bool
	ExprMode int
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

func (c *ExecCtx) ShouldExitOnError(hasFlag func(byte) bool) bool {
	if !hasFlag('e') {
		return false
	}
	return c.ExitCode() != 0
}

func (c *ExecCtx) EnterExprMode() func() {
	c.ExprMode++
	return func() { c.ExprMode-- }
}

func (c *ExecCtx) InExprMode() bool { return c.ExprMode > 0 }

// ForRedirect creates a child ExecCtx with different Stdout but shared state.
func (c *ExecCtx) ForRedirect(w io.Writer) *ExecCtx {
	return &ExecCtx{
		Stdout:   w,
		Debugger: c.Debugger,
		CallFn:   c.CallFn,
		CmdSub:   c.CmdSub,
		LastExit: c.LastExit,
		HasExit:  c.HasExit,
		ExprMode: c.ExprMode,
	}
}

// Copy creates a fully independent ExecCtx for subshells/spawn.
func (c *ExecCtx) Copy() *ExecCtx {
	cp := &ExecCtx{
		Stdout:   c.Stdout,
		CallFn:   c.CallFn,
		CmdSub:   c.CmdSub,
		LastExit:  c.LastExit,
		HasExit:  c.HasExit,
		ExprMode: c.ExprMode,
	}
	if dc, ok := c.Debugger.(DebuggerCopier); ok {
		cp.Debugger = dc.CopyForSpawn()
	} else {
		cp.Debugger = c.Debugger
	}
	return cp
}

// ---------------------------------------------------------------------------
// Scope: interface for both Frame (lightweight function scope) and Env
// (full shell scope). The evaluator works with Scope so function calls
// can use Frame instead of allocating a full Env.
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
// No shell state. No map allocation. Used by CallFn for function call frames.
//
// Flat arrays cover 0-4 params (the vast majority of function arities).
// If overflow occurs, spills to a heap-allocated map.
// ---------------------------------------------------------------------------

type Frame struct {
	parent   Scope
	Ctx      *ExecCtx
	flatKeys [MaxFlatBindings]string
	flatVals [MaxFlatBindings]Value
	flatN    int8
	spill    map[string]Value // nil until overflow
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
	// Overflow: spill to map
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

func (f *Frame) ResetFlat() {
	for i := int8(0); i < f.flatN; i++ {
		f.flatKeys[i] = ""
		f.flatVals[i] = Nil
	}
	f.flatN = 0
	f.spill = nil
}

// EachBinding iterates over all bindings in the frame (flat + spill).
func (f *Frame) EachBinding(fn func(name string, val Value)) {
	for i := int8(0); i < f.flatN; i++ {
		fn(f.flatKeys[i], f.flatVals[i])
	}
	for k, v := range f.spill {
		fn(k, v)
	}
}

// ---------------------------------------------------------------------------
// Env: shell scope. Has bindings, shell state (fns, modules, aliases, etc.),
// parent chain, and ExecCtx pointer. This is the full scope — used for
// top-level, module definitions, control flow scopes, pipe stages, etc.
// ---------------------------------------------------------------------------

type Env struct {
	Bindings map[string]Value
	Parent   Scope
	Ctx      *ExecCtx

	// Shell state
	Fns         map[string]*FnValue
	NativeFns   map[string]NativeFn
	Modules     map[string]*Module
	Proc        Pid
	ReadonlySet map[string]bool
	Exported    map[string]bool
	SetFlags    map[byte]bool
	Traps       map[string]string
	Aliases     map[string]string
	Source      string
	SourceName  string
	ShellPid    int
	ShellName   string
	LastBg      int
	Args        []string
	IsLoginShell bool
}

// envChain iterates over *Env nodes in the scope chain, skipping Frames.
// Usage: for c := range e.envChain() { ... }
// (Go 1.23+ range-over-func, but we use a callback for compatibility)
func (e *Env) eachEnv(fn func(*Env) bool) {
	for s := Scope(e); s != nil; s = s.GetParent() {
		if env, ok := s.(*Env); ok {
			if !fn(env) { return }
		}
	}
}

// parentEnv returns the nearest *Env ancestor (may be self).
func (e *Env) parentEnv() *Env {
	for s := e.Parent; s != nil; s = s.GetParent() {
		if env, ok := s.(*Env); ok { return env }
	}
	return nil
}

// Scope interface implementation for Env
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
		env.Ctx = &ExecCtx{Stdout: os.Stdout}
	}
	return env
}

func TopEnv() *Env {
	ctx := &ExecCtx{
		Stdout: os.Stdout,
	}
	e := &Env{
		Bindings: make(map[string]Value),
		Ctx:      ctx,
		ShellPid: os.Getpid(),
		Exported: make(map[string]bool),
	}
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			e.Bindings[parts[0]] = StringVal(parts[1])
			e.Exported[parts[0]] = true
		}
	}
	return e
}

func CopyEnv(src *Env) *Env {
	e := &Env{
		Bindings: make(map[string]Value),
		Exported: make(map[string]bool),
		Ctx:      src.Ctx.Copy(),
	}
	// Walk the scope chain from outermost to innermost so inner values shadow outer.
	// Collect both Frame bindings and Env shell state.
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
			for k, v := range c.Exported { if v { e.Exported[k] = true } }
			if c.NativeFns != nil {
				if e.NativeFns == nil { e.NativeFns = make(map[string]NativeFn) }
				for k, v := range c.NativeFns { e.NativeFns[k] = v }
			}
			if c.Modules != nil {
				if e.Modules == nil { e.Modules = make(map[string]*Module) }
				for k, v := range c.Modules { e.Modules[k] = v }
			}
			if c.ShellPid != 0 { e.ShellPid = c.ShellPid }
			if c.ShellName != "" { e.ShellName = c.ShellName }
			if c.Aliases != nil {
				if e.Aliases == nil { e.Aliases = make(map[string]string) }
				for k, v := range c.Aliases { e.Aliases[k] = v }
			}
			if c.ReadonlySet != nil {
				if e.ReadonlySet == nil { e.ReadonlySet = make(map[string]bool) }
				for k, v := range c.ReadonlySet { if v { e.ReadonlySet[k] = true } }
			}
			if c.SetFlags != nil {
				if e.SetFlags == nil { e.SetFlags = make(map[byte]bool) }
				for k, v := range c.SetFlags { e.SetFlags[k] = v }
			}
			if c.Traps != nil {
				if e.Traps == nil { e.Traps = make(map[string]string) }
				for k, v := range c.Traps { e.Traps[k] = v }
			}
		case *Frame:
			c.EachBinding(func(k string, v Value) { e.Bindings[k] = v })
		}
	}
	e.Args = src.PosArgs()
	return e
}

// ---------------------------------------------------------------------------
// Env methods: variable access
// ---------------------------------------------------------------------------

func (e *Env) Get(name string) (Value, bool) {
	if v, ok := e.Bindings[name]; ok {
		return v, true
	}
	if e.Parent != nil {
		return e.Parent.Get(name)
	}
	return Nil, false
}

func (e *Env) Set(name string, v Value) error {
	if e.IsReadonly(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	// Check own bindings first
	if _, ok := e.Bindings[name]; ok {
		e.Bindings[name] = v
		return nil
	}
	// Walk parent chain via Scope interface
	if e.Parent != nil {
		if _, ok := e.Parent.Get(name); ok {
			return e.Parent.Set(name, v)
		}
	}
	e.Bindings[name] = v
	return nil
}

func (e *Env) SetLocal(name string, v Value) error {
	if e.IsReadonly(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	e.Bindings[name] = v
	return nil
}

func (e *Env) DeleteVar(name string) error {
	if e.IsReadonly(name) {
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
// Env methods: shell state — walk parent chain for reads, write to self
// ---------------------------------------------------------------------------

func (e *Env) Stdout() io.Writer {
	return e.Ctx.Stdout
}

func (e *Env) SetExit(code int) {
	e.Ctx.SetExit(code)
}

func (e *Env) ExitCode() int {
	return e.Ctx.ExitCode()
}

func (e *Env) ShouldExitOnError() bool {
	return e.Ctx.ShouldExitOnError(e.HasFlag)
}

func (e *Env) EnterExprMode() func() {
	return e.Ctx.EnterExprMode()
}

func (e *Env) InExprMode() bool {
	return e.Ctx.InExprMode()
}

func (e *Env) GetProc() Pid {
	var result Pid
	e.eachEnv(func(c *Env) bool {
		if c.Proc != nil { result = c.Proc; return false }
		return true
	})
	return result
}

func (e *Env) GetCmdSub() CmdSubFunc {
	return e.Ctx.CmdSub
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
		if existing.NativeFns == nil { existing.NativeFns = make(map[string]NativeFn) }
		for fname, nfn := range mod.NativeFns { existing.NativeFns[fname] = nfn }
		return
	}
	e.Modules[name] = mod
}

func (e *Env) IsReadonly(name string) bool {
	var readonly bool
	e.eachEnv(func(c *Env) bool {
		if c.ReadonlySet != nil && c.ReadonlySet[name] { readonly = true; return false }
		return true
	})
	return readonly
}

func (e *Env) AllReadonly(fn func(name string)) {
	e.eachEnv(func(c *Env) bool {
		for name := range c.ReadonlySet { fn(name) }
		return true
	})
}

func (e *Env) SetReadonly(name string) {
	if e.ReadonlySet == nil { e.ReadonlySet = make(map[string]bool) }
	e.ReadonlySet[name] = true
}

func (e *Env) HasFlag(flag byte) bool {
	var result bool
	e.eachEnv(func(c *Env) bool {
		if c.SetFlags != nil {
			if v, ok := c.SetFlags[flag]; ok { result = v; return false }
		}
		return true
	})
	return result
}

func (e *Env) SetFlag(flag byte, on bool) {
	if e.SetFlags == nil { e.SetFlags = make(map[byte]bool) }
	e.SetFlags[flag] = on
}

func (e *Env) GetTrap(sig string) (string, bool) {
	var cmd string
	var found bool
	e.eachEnv(func(c *Env) bool {
		if c.Traps != nil {
			if v, ok := c.Traps[sig]; ok { cmd = v; found = true; return false }
		}
		return true
	})
	return cmd, found
}

func (e *Env) SetTrap(sig, cmd string) {
	if e.Traps == nil { e.Traps = make(map[string]string) }
	e.Traps[sig] = cmd
}

func (e *Env) DeleteTrap(sig string) {
	if e.Traps != nil { delete(e.Traps, sig) }
}

func (e *Env) AllTraps(fn func(sig, cmd string)) {
	e.eachEnv(func(c *Env) bool {
		for sig, cmd := range c.Traps { fn(sig, cmd) }
		return true
	})
}

func (e *Env) GetAlias(name string) (string, bool) {
	var val string
	var found bool
	e.eachEnv(func(c *Env) bool {
		if c.Aliases != nil {
			if v, ok := c.Aliases[name]; ok { val = v; found = true; return false }
		}
		return true
	})
	return val, found
}

func (e *Env) SetAlias(name, value string) {
	if e.Aliases == nil { e.Aliases = make(map[string]string) }
	e.Aliases[name] = value
}

func (e *Env) DeleteAlias(name string) {
	if e.Aliases != nil { delete(e.Aliases, name) }
}

func (e *Env) AllAliases() map[string]string {
	result := make(map[string]string)
	// Collect in reverse order so inner scopes shadow outer
	var chain []*Env
	e.eachEnv(func(c *Env) bool { chain = append(chain, c); return true })
	for i := len(chain) - 1; i >= 0; i-- {
		for k, v := range chain[i].Aliases { result[k] = v }
	}
	return result
}

func (e *Env) Export(name, val string) {
	e.Set(name, StringVal(val))
	if e.Exported == nil { e.Exported = make(map[string]bool) }
	e.Exported[name] = true
}

func (e *Env) ExportName(name string) {
	if e.Exported == nil { e.Exported = make(map[string]bool) }
	e.Exported[name] = true
}

func (e *Env) IsExported(name string) bool {
	var exported bool
	e.eachEnv(func(c *Env) bool {
		if c.Exported != nil && c.Exported[name] { exported = true; return false }
		return true
	})
	return exported
}

func (e *Env) Pid() int {
	var pid int
	e.eachEnv(func(c *Env) bool {
		if c.ShellPid != 0 { pid = c.ShellPid; return false }
		return true
	})
	if pid != 0 { return pid }
	return os.Getpid()
}

func (e *Env) BgPid() int {
	var pid int
	e.eachEnv(func(c *Env) bool {
		if c.LastBg != 0 { pid = c.LastBg; return false }
		return true
	})
	return pid
}

func (e *Env) GetShellName() string {
	var name string
	e.eachEnv(func(c *Env) bool {
		if c.ShellName != "" { name = c.ShellName; return false }
		return true
	})
	if name != "" { return name }
	return "ish"
}

func (e *Env) PosArgs() []string {
	var args []string
	e.eachEnv(func(c *Env) bool {
		if c.Args != nil { args = c.Args; return false }
		return true
	})
	return args
}

func (e *Env) BuildEnv() []string {
	seen := make(map[string]bool)
	var result []string
	// Walk scope chain for bindings (includes Frame bindings)
	for s := Scope(e); s != nil; s = s.GetParent() {
		if env, ok := s.(*Env); ok {
			for name, val := range env.Bindings {
				if seen[name] { continue }
				seen[name] = true
				if e.IsExported(name) { result = append(result, name+"="+val.ToStr()) }
			}
		} else if frame, ok := s.(*Frame); ok {
			frame.EachBinding(func(name string, val Value) {
				if seen[name] { return }
				seen[name] = true
				if e.IsExported(name) { result = append(result, name+"="+val.ToStr()) }
			})
		}
	}
	return result
}

// ResetFlat clears bindings for reuse across clause attempts.
func (e *Env) ResetFlat() {
	for k := range e.Bindings {
		delete(e.Bindings, k)
	}
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
			} else if fn := e.GetCmdSub(); fn != nil {
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
				if fn := e.GetCmdSub(); fn != nil {
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
			if fn := e.GetCmdSub(); fn != nil {
				result, _ := fn(cmdStr, e)
				buf.WriteString(result)
			}
		case '?':
			buf.WriteString(Itoa(e.ExitCode()))
			i++
		case '$':
			buf.WriteString(Itoa(e.Pid()))
			i++
		case '!':
			buf.WriteString(Itoa(e.BgPid()))
			i++
		case '#':
			buf.WriteString(Itoa(len(e.PosArgs())))
			i++
		case '@':
			buf.WriteString(strings.Join(e.PosArgs(), " "))
			i++
		case '*':
			sep := " "
			if ifs, ok := e.Get("IFS"); ok {
				ifsStr := ifs.ToStr()
				if len(ifsStr) > 0 { sep = ifsStr[:1] } else { sep = "" }
			}
			buf.WriteString(strings.Join(e.PosArgs(), sep))
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
					buf.WriteString(e.GetShellName())
				} else {
					args := e.PosArgs()
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
