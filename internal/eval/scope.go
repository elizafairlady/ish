package eval

import (
	"fmt"
	"io"
	"os"
	"sync"

	"ish/internal/stdlib"
	"ish/internal/value"
)

const MaxFlatBindings = 4

// ShellState holds mutable map state shared across redirect boundaries.
// Deep-copied for subshells.
type ShellState struct {
	SetFlags    map[byte]bool
	Traps       map[string]string
	Aliases     map[string]string
	ReadonlySet map[string]bool
	Exported    map[string]bool
}

func (s *ShellState) HasFlag(flag byte) bool {
	if s.SetFlags == nil {
		return false
	}
	return s.SetFlags[flag]
}

func (s *ShellState) SetFlag(flag byte, on bool) {
	if s.SetFlags == nil {
		s.SetFlags = make(map[byte]bool)
	}
	s.SetFlags[flag] = on
}

func (s *ShellState) IsReadonly(name string) bool {
	if s.ReadonlySet == nil {
		return false
	}
	return s.ReadonlySet[name]
}

func (s *ShellState) IsExported(name string) bool {
	if s.Exported == nil {
		return false
	}
	return s.Exported[name]
}

func (s *ShellState) Copy() *ShellState {
	cp := &ShellState{}
	if s == nil {
		return cp
	}
	if s.SetFlags != nil {
		cp.SetFlags = make(map[byte]bool, len(s.SetFlags))
		for k, v := range s.SetFlags {
			cp.SetFlags[k] = v
		}
	}
	if s.Traps != nil {
		cp.Traps = make(map[string]string, len(s.Traps))
		for k, v := range s.Traps {
			cp.Traps[k] = v
		}
	}
	if s.Aliases != nil {
		cp.Aliases = make(map[string]string, len(s.Aliases))
		for k, v := range s.Aliases {
			cp.Aliases[k] = v
		}
	}
	if s.ReadonlySet != nil {
		cp.ReadonlySet = make(map[string]bool, len(s.ReadonlySet))
		for k, v := range s.ReadonlySet {
			cp.ReadonlySet[k] = v
		}
	}
	if s.Exported != nil {
		cp.Exported = make(map[string]bool, len(s.Exported))
		for k, v := range s.Exported {
			cp.Exported[k] = v
		}
	}
	return cp
}

// ExecCtx holds shared execution state across all scopes in one execution.
type ExecCtx struct {
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	ExitMu   sync.Mutex
	LastExit int
	HasExit  bool

	ShellPid     int
	ShellName    string
	LastBg       int
	IsLoginShell bool
	Args         []string
	Source       string
	SourceName   string

	Shell *ShellState
	BgJobs sync.WaitGroup
	Jobs   *JobTable
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

func (c *ExecCtx) PosArgs() []string { return c.Args }
func (c *ExecCtx) Pid() int {
	if c.ShellPid != 0 {
		return c.ShellPid
	}
	return os.Getpid()
}

func (c *ExecCtx) ForRedirect(w io.Writer) *ExecCtx {
	return &ExecCtx{
		Stdin:        c.Stdin,
		Stdout:       w,
		Stderr:       c.Stderr,
		LastExit:     c.LastExit,
		HasExit:      c.HasExit,
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

func (c *ExecCtx) Copy() *ExecCtx {
	c.ExitMu.Lock()
	lastExit := c.LastExit
	hasExit := c.HasExit
	c.ExitMu.Unlock()
	return &ExecCtx{
		Stdin:        c.Stdin,
		Stdout:       c.Stdout,
		Stderr:       c.Stderr,
		LastExit:     lastExit,
		HasExit:      hasExit,
		ShellPid:     c.ShellPid,
		ShellName:    c.ShellName,
		LastBg:       c.LastBg,
		IsLoginShell: c.IsLoginShell,
		Args:         append([]string{}, c.Args...),
		Source:       c.Source,
		SourceName:   c.SourceName,
		Shell:        c.Shell.Copy(),
	}
}

func (c *ExecCtx) StdinOrDefault() io.Reader {
	if c.Stdin != nil {
		return c.Stdin
	}
	return os.Stdin
}

func (c *ExecCtx) StderrOrDefault() io.Writer {
	if c.Stderr != nil {
		return c.Stderr
	}
	return os.Stderr
}

// Scope is the interface for all scope types.
type Scope interface {
	Get(name string) (value.Value, bool)
	Set(name string, v value.Value) error
	SetLocal(name string, v value.Value) error
	GetParent() Scope
	GetCtx() *ExecCtx
	GetFn(name string) (*value.FnDef, bool)
	GetModule(name string) (*value.OrdMap, bool)
	NearestEnv() *Env
}

// Frame is a lightweight function scope with flat-array bindings.
type Frame struct {
	parent   Scope
	env      *Env // cached nearest Env — avoids parent chain walk for fn resolution
	Ctx      *ExecCtx
	flatKeys [MaxFlatBindings]string
	flatVals [MaxFlatBindings]value.Value
	flatN    int8
	spill    map[string]value.Value
}

var framePool = sync.Pool{
	New: func() interface{} { return &Frame{} },
}

func NewFrame(parent Scope) *Frame {
	f := framePool.Get().(*Frame)
	f.parent = parent
	f.Ctx = parent.GetCtx()
	f.env = parent.NearestEnv()
	return f
}

func (f *Frame) ResetFlat() {
	for i := int8(0); i < f.flatN; i++ {
		f.flatKeys[i] = ""
		f.flatVals[i] = value.Nil
	}
	f.flatN = 0
	// Don't clear spill — it holds flattened parent/module bindings
}

func putFrame(f *Frame) {
	f.ResetFlat()
	f.spill = nil
	f.parent = nil
	f.env = nil
	f.Ctx = nil
	framePool.Put(f)
}

// Snapshot copies the Frame's bindings into an Env for closure capture.
// The Frame can then be safely returned to the pool.
func (f *Frame) Snapshot() *Env {
	env := newChildEnv(f.env)
	f.EachBinding(func(name string, val value.Value) {
		env.Bindings[name] = val
	})
	return env
}

func (f *Frame) Get(name string) (value.Value, bool) {
	for i := int8(0); i < f.flatN; i++ {
		if f.flatKeys[i] == name {
			return f.flatVals[i], true
		}
	}
	if f.spill != nil {
		if v, ok := f.spill[name]; ok {
			return v, true
		}
	}
	// Use cached env — direct call, no interface dispatch
	if f.env != nil {
		return f.env.Get(name)
	}
	return value.Nil, false
}

func (f *Frame) Set(name string, v value.Value) error {
	for i := int8(0); i < f.flatN; i++ {
		if f.flatKeys[i] == name {
			f.flatVals[i] = v
			return nil
		}
	}
	if f.spill != nil {
		if _, ok := f.spill[name]; ok {
			f.spill[name] = v
			return nil
		}
	}
	if f.parent != nil {
		if _, ok := f.parent.Get(name); ok {
			return f.parent.Set(name, v)
		}
	}
	return f.SetLocal(name, v)
}

func (f *Frame) SetLocal(name string, v value.Value) error {
	for i := int8(0); i < f.flatN; i++ {
		if f.flatKeys[i] == name {
			f.flatVals[i] = v
			return nil
		}
	}
	if f.flatN < MaxFlatBindings {
		f.flatKeys[f.flatN] = name
		f.flatVals[f.flatN] = v
		f.flatN++
		return nil
	}
	if f.spill == nil {
		f.spill = make(map[string]value.Value)
	}
	f.spill[name] = v
	return nil
}

func (f *Frame) GetParent() Scope  { return f.parent }
func (f *Frame) GetCtx() *ExecCtx { return f.Ctx }

func (f *Frame) GetFn(name string) (*value.FnDef, bool) {
	if f.parent != nil {
		return f.parent.GetFn(name)
	}
	return nil, false
}

func (f *Frame) GetModule(name string) (*value.OrdMap, bool) {
	if f.parent != nil {
		return f.parent.GetModule(name)
	}
	return nil, false
}

func (f *Frame) NearestEnv() *Env {
	return f.env
}

func (f *Frame) EachBinding(fn func(string, value.Value)) {
	for i := int8(0); i < f.flatN; i++ {
		fn(f.flatKeys[i], f.flatVals[i])
	}
	for k, v := range f.spill {
		fn(k, v)
	}
}


// Env is a lexical scope with bindings, functions, and modules.
type Env struct {
	Bindings map[string]value.Value
	Fns      map[string]*value.FnDef
	Modules  map[string]*value.OrdMap
	Parent   Scope
	Ctx      *ExecCtx
}

func NewEnv() *Env {
	proc := newProcess()
	shell := &ShellState{
		Exported: make(map[string]bool),
	}
	ctx := &ExecCtx{
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		ShellPid: os.Getpid(),
		Shell:    shell,
		Jobs:     NewJobTable(),
	}
	env := &Env{
		Bindings: make(map[string]value.Value),
		Ctx:      ctx,
	}
	env.Bindings["__self"] = value.PidVal(proc.id)
	stdlib.Register(env)
	stdlib.Invoke = func(fn *value.FnDef, args []value.Value) (value.Value, error) {
		return callFn(fn, nil, args, nil, env)
	}
	stdlib.RunSource = func(src string, e interface{}) {
		Run(src, e.(*Env))
	}
	stdlib.LoadPrelude(env)
	return env
}

func newChildEnv(parent Scope) *Env {
	return &Env{
		Bindings: make(map[string]value.Value),
		Parent:   parent,
		Ctx:      parent.GetCtx(),
	}
}

func copyEnv(src Scope) *Env {
	e := &Env{
		Bindings: make(map[string]value.Value),
		Ctx:      src.GetCtx().Copy(),
	}
	// Flatten entire scope chain so cmdsubs/subshells see all parent bindings
	for s := Scope(src); s != nil; s = s.GetParent() {
		// Copy from Frame (flat-array + spill)
		if frame, ok := s.(*Frame); ok {
			frame.EachBinding(func(k string, v value.Value) {
				if _, exists := e.Bindings[k]; !exists {
					e.Bindings[k] = v
				}
			})
			continue
		}
		env, ok := s.(*Env)
		if !ok {
			continue
		}
		for k, v := range env.Bindings {
			if _, exists := e.Bindings[k]; !exists {
				e.Bindings[k] = v
			}
		}
		if env.Fns != nil {
			if e.Fns == nil {
				e.Fns = make(map[string]*value.FnDef)
			}
			for k, v := range env.Fns {
				if _, exists := e.Fns[k]; !exists {
					e.Fns[k] = v
				}
			}
		}
		if env.Modules != nil {
			if e.Modules == nil {
				e.Modules = make(map[string]*value.OrdMap)
			}
			for k, v := range env.Modules {
				if _, exists := e.Modules[k]; !exists {
					e.Modules[k] = v
				}
			}
		}
	}
	return e
}

func (e *Env) Get(name string) (value.Value, bool) {
	if v, ok := e.Bindings[name]; ok {
		return v, true
	}
	if e.Parent != nil {
		return e.Parent.Get(name)
	}
	return value.Nil, false
}

func (e *Env) Set(name string, v value.Value) error {
	if e.Ctx.Shell.IsReadonly(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	if _, ok := e.Bindings[name]; ok {
		e.Bindings[name] = v
		return nil
	}
	if e.Parent != nil {
		if _, ok := e.Parent.Get(name); ok {
			return e.Parent.Set(name, v)
		}
	}
	e.Bindings[name] = v
	return nil
}

func (e *Env) SetLocal(name string, v value.Value) error {
	if e.Ctx.Shell.IsReadonly(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	e.Bindings[name] = v
	return nil
}

func (e *Env) Delete(name string) error {
	if e.Ctx.Shell.IsReadonly(name) {
		return fmt.Errorf("unset: %s: readonly variable", name)
	}
	if _, ok := e.Bindings[name]; ok {
		delete(e.Bindings, name)
		return nil
	}
	if e.Parent != nil {
		if ne := e.Parent.NearestEnv(); ne != nil {
			return ne.Delete(name)
		}
	}
	return nil
}

func (e *Env) GetParent() Scope {
	if e.Parent != nil {
		return e.Parent
	}
	return nil
}
func (e *Env) GetCtx() *ExecCtx { return e.Ctx }
func (e *Env) NearestEnv() *Env { return e }

func (e *Env) eachEnv(fn func(*Env) bool) {
	for s := Scope(e); s != nil; s = s.GetParent() {
		if env, ok := s.(*Env); ok {
			if !fn(env) {
				return
			}
		}
	}
}

func (e *Env) GetFn(name string) (*value.FnDef, bool) {
	var fn *value.FnDef
	var found bool
	e.eachEnv(func(c *Env) bool {
		if c.Fns != nil {
			if f, ok := c.Fns[name]; ok {
				fn = f
				found = true
				return false
			}
		}
		return true
	})
	return fn, found
}

func (e *Env) AddFnClauses(name string, f *value.FnDef) {
	if e.Fns == nil {
		e.Fns = make(map[string]*value.FnDef)
	}
	if existing, ok := e.Fns[name]; ok {
		existing.Clauses = append(existing.Clauses, f.Clauses...)
		return
	}
	e.Fns[name] = f
}

func (e *Env) SetFn(name string, f *value.FnDef) {
	if e.Fns == nil {
		e.Fns = make(map[string]*value.FnDef)
	}
	e.Fns[name] = f
}

func (e *Env) GetModule(name string) (*value.OrdMap, bool) {
	var mod *value.OrdMap
	var found bool
	e.eachEnv(func(c *Env) bool {
		if c.Modules != nil {
			if m, ok := c.Modules[name]; ok {
				mod = m
				found = true
				return false
			}
		}
		return true
	})
	return mod, found
}

func (e *Env) SetModule(name string, mod *value.OrdMap) {
	if e.Modules == nil {
		e.Modules = make(map[string]*value.OrdMap)
	}
	e.Modules[name] = mod
}

func (e *Env) Export(name, val string) {
	e.Set(name, value.StringVal(val))
	if e.Ctx.Shell.Exported == nil {
		e.Ctx.Shell.Exported = make(map[string]bool)
	}
	e.Ctx.Shell.Exported[name] = true
}

func (e *Env) BuildEnv() []string {
	// Collect ish-exported overrides
	var overrides []string
	for s := Scope(e); s != nil; s = s.GetParent() {
		if env, ok := s.(*Env); ok {
			for name, val := range env.Bindings {
				if e.Ctx.Shell.IsExported(name) {
					overrides = append(overrides, name+"="+val.ToStr())
				}
			}
		}
	}
	// No overrides → nil tells exec.Cmd to inherit OS env as-is
	if len(overrides) == 0 {
		return nil
	}
	// Append overrides to OS env (later entries win in exec.Cmd)
	return append(os.Environ(), overrides...)
}
