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
type CmdSubFunc func(cmd string, env *Env) (string, error)

// DebuggerCopier is implemented by the debugger to support spawn.
type DebuggerCopier interface {
	CopyForSpawn() interface{}
}

// Env is a lexical scope. Each scope has bindings and a parent.
// MaxFlatBindings is the maximum number of bindings a flat-scope env can hold
// before upgrading to a map. Covers the vast majority of function arities (0-4 params).
const MaxFlatBindings = 4

// ShellState holds shell-scope fields that are only meaningful on
// shell-scope envs (created by NewEnv/TopEnv/CopyEnv). Function frames
// created by NewFlatEnv leave Shell nil, cutting ~15 pointer-containing
// fields from GC scanning per function call.
type ShellState struct {
	Fns         map[string]*FnValue
	NativeFns   map[string]NativeFn    // stdlib native functions
	Modules     map[string]*Module     // registered modules
	Proc        Pid                    // owning process (for self/receive)
	CmdSub      CmdSubFunc             // for $() expansion in strings
	ReadonlySet map[string]bool        // variables that cannot be reassigned
	Exported    map[string]bool        // variables marked for export to child processes
	SetFlags    map[byte]bool          // shell options: e, u, x, etc.
	Traps       map[string]string      // signal -> command
	Aliases     map[string]string      // alias name -> expansion
	Source      string                 // current source text
	SourceName  string                 // filename, "<repl>", or "<stdin>"
	ShellPid    int                    // $$
	ShellName   string                 // $0
	LastBg      int                    // $!
	IsLoginShell bool
}

type Env struct {
	// Flat bindings for small function scopes (≤MaxFlatBindings entries).
	// FlatN >= 0 means flat mode (Bindings is nil).
	// FlatN == -1 means map mode (use Bindings).
	FlatKeys [MaxFlatBindings]string
	FlatVals [MaxFlatBindings]Value
	FlatN    int8

	Bindings map[string]Value
	Parent   *Env
	Shell    *ShellState // nil on function frames (NewFlatEnv)
	Stdout_  io.Writer   // output destination (defaults to os.Stdout)

	// CallFn is set by eval to allow stdlib/process to call user functions.
	CallFn func(fn *FnValue, args []Value, env *Env) (Value, error)

	// Debugger holds a *debug.Debugger when active, nil otherwise.
	// Uses interface{} to avoid circular import between core and debug.
	Debugger interface{}

	ExprMode int // >0 when evaluating ish expressions (not POSIX commands)

	// Shell state (ExitMu protects LastExit/HasExit for concurrent pipe access)
	ExitMu   sync.Mutex
	LastExit int      // $?
	HasExit  bool     // true once SetExit has been called in this scope
	Args     []string // $1, $2, ...
}

// Stdout returns the output writer for this env, walking up to parent if needed.
func (e *Env) Stdout() io.Writer {
	for c := e; c != nil; c = c.Parent {
		if c.Stdout_ != nil {
			return c.Stdout_
		}
	}
	return os.Stdout
}

// ensureShell returns the ShellState, panicking if called on a function frame.
func (e *Env) ensureShell() *ShellState {
	if e.Shell == nil {
		panic("bug: shell-scope operation on function frame")
	}
	return e.Shell
}

func NewEnv(parent *Env) *Env {
	env := &Env{
		FlatN:    -1, // map mode
		Bindings: make(map[string]Value),
		Parent:   parent,
		Shell:    &ShellState{},
	}
	if parent != nil {
		env.CallFn = parent.CallFn
		env.Debugger = parent.Debugger
	}
	return env
}

// NewFlatEnv creates an env that uses flat arrays for bindings (no map allocation).
// Used for function scopes where parameter count is small (≤4).
// Automatically upgrades to map mode if more bindings are added.
func NewFlatEnv(parent *Env) *Env {
	env := &Env{
		// FlatN defaults to 0: flat mode, empty
		Parent: parent,
	}
	if parent != nil {
		env.CallFn = parent.CallFn
		env.Debugger = parent.Debugger
	}
	return env
}

// CopyEnv creates a snapshot of all visible bindings and functions.
// The new env has no parent -- all values are copied into it.
// This is used for spawn to give processes isolated state.
func CopyEnv(src *Env) *Env {
	sh := &ShellState{
		Exported: make(map[string]bool),
	}
	e := &Env{
		FlatN:    -1, // map mode
		Bindings: make(map[string]Value),
		Shell:    sh,
	}
	// Walk the chain from outermost to innermost so inner values shadow outer
	var chain []*Env
	for c := src; c != nil; c = c.Parent {
		chain = append(chain, c)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		c := chain[i]
		if c.FlatN >= 0 {
			for j := int8(0); j < c.FlatN; j++ {
				e.Bindings[c.FlatKeys[j]] = c.FlatVals[j]
			}
		} else {
			for k, v := range c.Bindings {
				e.Bindings[k] = v
			}
		}
		if c.Shell != nil {
			// Deep-copy FnValues to prevent cross-process mutation
			for k, v := range c.Shell.Fns {
				if sh.Fns == nil {
					sh.Fns = make(map[string]*FnValue)
				}
				copied := &FnValue{Name: v.Name, Clauses: make([]FnClause, len(v.Clauses))}
				copy(copied.Clauses, v.Clauses)
				sh.Fns[k] = copied
			}
			for k, v := range c.Shell.Exported {
				if v {
					sh.Exported[k] = true
				}
			}
			if c.Shell.CmdSub != nil {
				sh.CmdSub = c.Shell.CmdSub
			}
			// Copy native functions (stdlib) -- these are stateless, safe to share
			if c.Shell.NativeFns != nil {
				if sh.NativeFns == nil {
					sh.NativeFns = make(map[string]NativeFn)
				}
				for k, v := range c.Shell.NativeFns {
					sh.NativeFns[k] = v
				}
			}
			// Copy modules -- immutable after construction, safe to share
			if c.Shell.Modules != nil {
				if sh.Modules == nil {
					sh.Modules = make(map[string]*Module)
				}
				for k, v := range c.Shell.Modules {
					sh.Modules[k] = v
				}
			}
			if c.Shell.ShellPid != 0 {
				sh.ShellPid = c.Shell.ShellPid
			}
			if c.Shell.ShellName != "" {
				sh.ShellName = c.Shell.ShellName
			}
			// Copy aliases
			if c.Shell.Aliases != nil {
				if sh.Aliases == nil {
					sh.Aliases = make(map[string]string)
				}
				for k, v := range c.Shell.Aliases {
					sh.Aliases[k] = v
				}
			}
			// Copy readonly set
			if c.Shell.ReadonlySet != nil {
				if sh.ReadonlySet == nil {
					sh.ReadonlySet = make(map[string]bool)
				}
				for k, v := range c.Shell.ReadonlySet {
					if v {
						sh.ReadonlySet[k] = true
					}
				}
			}
			// Copy shell flags (set -e, -u, -x, etc.)
			if c.Shell.SetFlags != nil {
				if sh.SetFlags == nil {
					sh.SetFlags = make(map[byte]bool)
				}
				for k, v := range c.Shell.SetFlags {
					sh.SetFlags[k] = v
				}
			}
			// Copy traps
			if c.Shell.Traps != nil {
				if sh.Traps == nil {
					sh.Traps = make(map[string]string)
				}
				for k, v := range c.Shell.Traps {
					sh.Traps[k] = v
				}
			}
		}
		if c.Stdout_ != nil {
			e.Stdout_ = c.Stdout_
		}
		// Copy CallFn
		if c.CallFn != nil {
			e.CallFn = c.CallFn
		}
	}
	e.SetExit(src.ExitCode())
	e.Args = src.PosArgs()
	// Copy debugger for spawned processes (fresh stack, shared source maps)
	if dc, ok := src.Debugger.(DebuggerCopier); ok {
		e.Debugger = dc.CopyForSpawn()
	}
	return e
}

// TopEnv creates a top-level environment. Does NOT register stdlib -- caller must do that.
func TopEnv() *Env {
	e := NewEnv(nil)
	e.Shell.ShellPid = os.Getpid()
	e.Shell.Exported = make(map[string]bool)
	// Import environment variables and mark them as exported
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			e.Bindings[parts[0]] = StringVal(parts[1])
			e.Shell.Exported[parts[0]] = true
		}
	}
	e.Stdout_ = os.Stdout
	return e
}

func (e *Env) GetProc() Pid {
	for c := e; c != nil; c = c.Parent {
		if c.Shell != nil && c.Shell.Proc != nil {
			return c.Shell.Proc
		}
	}
	return nil
}

func (e *Env) GetCmdSub() CmdSubFunc {
	for c := e; c != nil; c = c.Parent {
		if c.Shell != nil && c.Shell.CmdSub != nil {
			return c.Shell.CmdSub
		}
	}
	return nil
}

func (e *Env) Get(name string) (Value, bool) {
	if e.FlatN >= 0 {
		for i := int8(0); i < e.FlatN; i++ {
			if e.FlatKeys[i] == name {
				return e.FlatVals[i], true
			}
		}
	} else if v, ok := e.Bindings[name]; ok {
		return v, true
	}
	if e.Parent != nil {
		return e.Parent.Get(name)
	}
	return Nil, false
}

// Set updates an existing variable in the scope chain, or creates a new one
// in the current scope if not found.
func (e *Env) Set(name string, v Value) error {
	if e.IsReadonly(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	// Walk parent chain to find existing binding
	for c := e; c != nil; c = c.Parent {
		if c.FlatN >= 0 {
			for i := int8(0); i < c.FlatN; i++ {
				if c.FlatKeys[i] == name {
					c.FlatVals[i] = v
					return nil
				}
			}
		} else if _, ok := c.Bindings[name]; ok {
			c.Bindings[name] = v
			return nil
		}
	}
	// Not found anywhere -- write to current scope
	return e.SetLocal(name, v)
}

// SetLocal always writes to the current scope, never walking the parent chain.
func (e *Env) SetLocal(name string, v Value) error {
	if e.IsReadonly(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	if e.FlatN >= 0 {
		for i := int8(0); i < e.FlatN; i++ {
			if e.FlatKeys[i] == name {
				e.FlatVals[i] = v
				return nil
			}
		}
		if e.FlatN < MaxFlatBindings {
			e.FlatKeys[e.FlatN] = name
			e.FlatVals[e.FlatN] = v
			e.FlatN++
			return nil
		}
		// Overflow — upgrade to map
		e.Bindings = make(map[string]Value, MaxFlatBindings*2)
		for i := int8(0); i < MaxFlatBindings; i++ {
			e.Bindings[e.FlatKeys[i]] = e.FlatVals[i]
		}
		e.FlatN = -1
	}
	e.Bindings[name] = v
	return nil
}

func (e *Env) IsReadonly(name string) bool {
	for c := e; c != nil; c = c.Parent {
		if c.Shell != nil && c.Shell.ReadonlySet != nil && c.Shell.ReadonlySet[name] {
			return true
		}
	}
	return false
}

func (e *Env) SetReadonly(name string) {
	sh := e.ensureShell()
	if sh.ReadonlySet == nil {
		sh.ReadonlySet = make(map[string]bool)
	}
	sh.ReadonlySet[name] = true
}

func (e *Env) HasFlag(flag byte) bool {
	for c := e; c != nil; c = c.Parent {
		if c.Shell != nil && c.Shell.SetFlags != nil {
			if v, ok := c.Shell.SetFlags[flag]; ok {
				return v
			}
		}
	}
	return false
}

func (e *Env) SetFlag(flag byte, on bool) {
	sh := e.ensureShell()
	if sh.SetFlags == nil {
		sh.SetFlags = make(map[byte]bool)
	}
	sh.SetFlags[flag] = on
}

// EnterExprMode increments the expression mode counter and returns a
// function that restores it. Use: defer env.EnterExprMode()()
func (e *Env) EnterExprMode() func() {
	e.ExprMode++
	return func() { e.ExprMode-- }
}

// InExprMode reports whether we are inside an expression evaluation.
func (e *Env) InExprMode() bool { return e.ExprMode > 0 }

func (e *Env) GetTrap(sig string) (string, bool) {
	for c := e; c != nil; c = c.Parent {
		if c.Shell != nil && c.Shell.Traps != nil {
			if cmd, ok := c.Shell.Traps[sig]; ok {
				return cmd, true
			}
		}
	}
	return "", false
}

func (e *Env) SetTrap(sig, cmd string) {
	sh := e.ensureShell()
	if sh.Traps == nil {
		sh.Traps = make(map[string]string)
	}
	sh.Traps[sig] = cmd
}

func (e *Env) DeleteTrap(sig string) {
	sh := e.ensureShell()
	if sh.Traps != nil {
		delete(sh.Traps, sig)
	}
}

func (e *Env) GetFn(name string) (*FnValue, bool) {
	for c := e; c != nil; c = c.Parent {
		if c.Shell != nil && c.Shell.Fns != nil {
			if f, ok := c.Shell.Fns[name]; ok {
				return f, true
			}
		}
	}
	return nil, false
}

// GetNativeFn looks up a stdlib native function in the scope chain.
func (e *Env) GetNativeFn(name string) (NativeFn, bool) {
	for c := e; c != nil; c = c.Parent {
		if c.Shell != nil && c.Shell.NativeFns != nil {
			if fn, ok := c.Shell.NativeFns[name]; ok {
				return fn, true
			}
		}
	}
	return nil, false
}

// SetNativeFn registers a native function in this scope.
func (e *Env) SetNativeFn(name string, fn NativeFn) {
	sh := e.ensureShell()
	if sh.NativeFns == nil {
		sh.NativeFns = make(map[string]NativeFn)
	}
	sh.NativeFns[name] = fn
}

// AddFnClauses appends clauses to an existing function, or registers it
// fresh if no function with that name exists yet. Use this for sequential
// single-clause definitions (fn fib 0 do...end; fn fib 1 do...end).
func (e *Env) AddFnClauses(name string, f *FnValue) {
	sh := e.ensureShell()
	if sh.Fns == nil {
		sh.Fns = make(map[string]*FnValue)
	}
	if existing, ok := sh.Fns[name]; ok {
		existing.Clauses = append(existing.Clauses, f.Clauses...)
		return
	}
	sh.Fns[name] = f
}

// SetFnClauses unconditionally replaces a function's complete clause list.
// Use this for dispatch-table definitions (fn name do pattern -> body end)
// and for POSIX function redefinitions (name() { ... }), where a new
// definition should fully replace the old one.
func (e *Env) SetFnClauses(name string, f *FnValue) {
	sh := e.ensureShell()
	if sh.Fns == nil {
		sh.Fns = make(map[string]*FnValue)
	}
	sh.Fns[name] = f
}

// GetModule looks up a module in the scope chain.
func (e *Env) GetModule(name string) (*Module, bool) {
	for c := e; c != nil; c = c.Parent {
		if c.Shell != nil && c.Shell.Modules != nil {
			if m, ok := c.Shell.Modules[name]; ok {
				return m, true
			}
		}
	}
	return nil, false
}

// SetModule registers a module in this scope. If a module with the same
// name already exists, new functions are merged into it.
func (e *Env) SetModule(name string, mod *Module) {
	sh := e.ensureShell()
	if sh.Modules == nil {
		sh.Modules = make(map[string]*Module)
	}
	if existing, ok := sh.Modules[name]; ok {
		if existing.Fns == nil {
			existing.Fns = make(map[string]*FnValue)
		}
		for fname, fn := range mod.Fns {
			existing.Fns[fname] = fn
		}
		if existing.NativeFns == nil {
			existing.NativeFns = make(map[string]NativeFn)
		}
		for fname, nfn := range mod.NativeFns {
			existing.NativeFns[fname] = nfn
		}
		return
	}
	sh.Modules[name] = mod
}

// Export marks a variable as exported and sets its value.
func (e *Env) Export(name, val string) {
	e.Set(name, StringVal(val))
	sh := e.ensureShell()
	if sh.Exported == nil {
		sh.Exported = make(map[string]bool)
	}
	sh.Exported[name] = true
}

// ExportName marks an existing variable as exported without changing its value.
func (e *Env) ExportName(name string) {
	sh := e.ensureShell()
	if sh.Exported == nil {
		sh.Exported = make(map[string]bool)
	}
	sh.Exported[name] = true
}

func (e *Env) IsExported(name string) bool {
	for c := e; c != nil; c = c.Parent {
		if c.Shell != nil && c.Shell.Exported != nil && c.Shell.Exported[name] {
			return true
		}
	}
	return false
}

// BuildEnv returns the environment variables for child processes.
func (e *Env) BuildEnv() []string {
	seen := make(map[string]bool)
	var result []string
	for c := e; c != nil; c = c.Parent {
		if c.FlatN >= 0 {
			for i := int8(0); i < c.FlatN; i++ {
				name := c.FlatKeys[i]
				if seen[name] {
					continue
				}
				seen[name] = true
				if e.IsExported(name) {
					result = append(result, name+"="+c.FlatVals[i].ToStr())
				}
			}
		} else {
			for name, val := range c.Bindings {
				if seen[name] {
					continue
				}
				seen[name] = true
				if e.IsExported(name) {
					result = append(result, name+"="+val.ToStr())
				}
			}
		}
	}
	return result
}

// Expand performs tilde expansion, parameter expansion, and ish interpolation.
func (e *Env) Expand(s string) string {
	// Tilde expansion (before parameter expansion)
	s = e.ExpandTilde(s)

	if !strings.Contains(s, "$") && !strings.Contains(s, "#{") {
		return s
	}

	var buf strings.Builder
	i := 0
	for i < len(s) {
		// ish interpolation: #{expr}
		if i+1 < len(s) && s[i] == '#' && s[i+1] == '{' {
			i += 2
			start := i
			depth := 1
			for i < len(s) && depth > 0 {
				if s[i] == '{' {
					depth++
				} else if s[i] == '}' {
					depth--
				}
				if depth > 0 {
					i++
				}
			}
			expr := s[start:i]
			if i < len(s) {
				i++ // skip }
			}
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
		i++ // skip $
		if i >= len(s) {
			buf.WriteByte('$')
			break
		}

		switch s[i] {
		case '(':
			if i+1 < len(s) && s[i+1] == '(' {
				// $(( arithmetic ))
				i += 2
				start := i
				depth := 1
				for i < len(s) && depth > 0 {
					if i+1 < len(s) && s[i] == ')' && s[i+1] == ')' {
						depth--
						if depth == 0 {
							break
						}
						i += 2
					} else {
						i++
					}
				}
				expr := s[start:i]
				if i+1 < len(s) {
					i += 2
				}
				if fn := e.GetCmdSub(); fn != nil {
					result, _ := fn("echo $(("+expr+"))", e)
					buf.WriteString(result)
				}
				continue
			}
			// $(...) command substitution
			i++
			start := i
			depth := 1
			for i < len(s) && depth > 0 {
				if s[i] == '(' {
					depth++
				} else if s[i] == ')' {
					depth--
				}
				if depth > 0 {
					i++
				}
			}
			cmdStr := s[start:i]
			if i < len(s) {
				i++
			}
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
				if len(ifsStr) > 0 {
					sep = ifsStr[:1]
				} else {
					sep = ""
				}
			}
			buf.WriteString(strings.Join(e.PosArgs(), sep))
			i++
		case '{':
			i++
			start := i
			for i < len(s) && s[i] != '}' {
				i++
			}
			expr := s[start:i]
			if i < len(s) {
				i++
			}
			// ${#var} -- string length
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
				if op[0] == ':' {
					hasColon = true
					op = op[1:]
				}
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
						} else {
							buf.WriteString(v.ToStr())
						}
					case '+':
						if !isUnset && !(hasColon && isEmpty) {
							buf.WriteString(e.Expand(defaultVal))
						}
					case '=':
						if isUnset || (hasColon && isEmpty) {
							expanded := e.Expand(defaultVal)
							e.Set(name, StringVal(expanded))
							buf.WriteString(expanded)
						} else {
							buf.WriteString(v.ToStr())
						}
					case '?':
						if isUnset || (hasColon && isEmpty) {
							fmt.Fprintf(os.Stderr, "ish: %s: %s\n", name, defaultVal)
						} else {
							buf.WriteString(v.ToStr())
						}
					}
				} else {
					if v, ok := e.Get(expr); ok {
						buf.WriteString(v.ToStr())
					}
				}
			} else {
				if handled := e.expandParamOp(expr, &buf); handled {
					continue
				}
				if v, ok := e.Get(expr); ok {
					buf.WriteString(v.ToStr())
				}
			}
		default:
			if s[i] >= '0' && s[i] <= '9' {
				start := i
				for i < len(s) && s[i] >= '0' && s[i] <= '9' {
					i++
				}
				idxStr := s[start:i]
				idx, _ := strconv.Atoi(idxStr)
				if idx == 0 {
					buf.WriteString(e.GetShellName())
				} else {
					args := e.PosArgs()
					if idx > 0 && idx <= len(args) {
						buf.WriteString(args[idx-1])
					}
				}
			} else {
				start := i
				for i < len(s) && IsVarChar(s[i]) {
					i++
				}
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
			px++
			sx++
		} else if px < len(pattern) && pattern[px] == '*' {
			starPx = px
			starSx = sx
			px++
		} else if starPx >= 0 {
			px = starPx + 1
			starSx++
			sx = starSx
		} else {
			return false
		}
	}
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}

func (e *Env) expandParamOp(expr string, buf *strings.Builder) bool {
	for i := 1; i < len(expr); i++ {
		ch := expr[i]
		// Skip over $(...) content — operators inside it aren't ours
		if ch == '$' && i+1 < len(expr) && expr[i+1] == '(' {
			depth := 1
			i += 2
			for i < len(expr) && depth > 0 {
				if expr[i] == '(' {
					depth++
				} else if expr[i] == ')' {
					depth--
				}
				i++
			}
			i-- // compensate for loop increment
			continue
		}
		if ch == '#' || ch == '%' || ch == '/' {
			name := expr[:i]
			op := expr[i:]
			v, ok := e.Get(name)
			if !ok {
				return true
			}
			val := v.ToStr()

			switch {
			case strings.HasPrefix(op, "##"):
				pattern := op[2:]
				for j := len(val); j >= 0; j-- {
					if ShellGlobMatch(pattern, val[:j]) {
						buf.WriteString(val[j:])
						return true
					}
				}
				buf.WriteString(val)
				return true
			case ch == '#':
				pattern := op[1:]
				for j := 0; j <= len(val); j++ {
					if ShellGlobMatch(pattern, val[:j]) {
						buf.WriteString(val[j:])
						return true
					}
				}
				buf.WriteString(val)
				return true
			case strings.HasPrefix(op, "%%"):
				pattern := op[2:]
				for j := 0; j <= len(val); j++ {
					if ShellGlobMatch(pattern, val[j:]) {
						buf.WriteString(val[:j])
						return true
					}
				}
				buf.WriteString(val)
				return true
			case ch == '%':
				pattern := op[1:]
				for j := len(val); j >= 0; j-- {
					if ShellGlobMatch(pattern, val[j:]) {
						buf.WriteString(val[:j])
						return true
					}
				}
				buf.WriteString(val)
				return true
			case strings.HasPrefix(op, "//"):
				parts := strings.SplitN(op[2:], "/", 2)
				old := parts[0]
				newStr := ""
				if len(parts) > 1 {
					newStr = parts[1]
				}
				buf.WriteString(strings.ReplaceAll(val, old, newStr))
				return true
			case ch == '/':
				parts := strings.SplitN(op[1:], "/", 2)
				old := parts[0]
				newStr := ""
				if len(parts) > 1 {
					newStr = parts[1]
				}
				buf.WriteString(strings.Replace(val, old, newStr, 1))
				return true
			}
		}
	}
	return false
}

func IsVarChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func (e *Env) ExpandTilde(s string) string {
	if len(s) == 0 || s[0] != '~' {
		return s
	}
	if len(s) == 1 || s[1] == '/' {
		home := ""
		if v, ok := e.Get("HOME"); ok {
			home = v.ToStr()
		}
		if home == "" {
			return s
		}
		return home + s[1:]
	}
	end := strings.IndexByte(s, '/')
	if end < 0 {
		end = len(s)
	}
	username := s[1:end]
	u, err := user.Lookup(username)
	if err != nil {
		return s
	}
	return u.HomeDir + s[end:]
}

// ExpandTildeStatic is the standalone version used before an Env is available.
func ExpandTildeStatic(s string) string {
	if len(s) == 0 || s[0] != '~' {
		return s
	}
	if len(s) == 1 || s[1] == '/' {
		home := os.Getenv("HOME")
		if home == "" {
			return s
		}
		return home + s[1:]
	}
	end := strings.IndexByte(s, '/')
	if end < 0 {
		end = len(s)
	}
	username := s[1:end]
	u, err := user.Lookup(username)
	if err != nil {
		return s
	}
	return u.HomeDir + s[end:]
}

// SplitFieldsIFS splits on IFS characters.
func (e *Env) SplitFieldsIFS(s string) []string {
	if ifs, ok := e.Get("IFS"); ok {
		ifsStr := ifs.ToStr()
		if ifsStr == "" {
			return []string{s}
		}
		return SplitOnChars(s, ifsStr)
	}
	return strings.Fields(s)
}

func SplitOnChars(s, chars string) []string {
	if chars == "" {
		return []string{s}
	}
	var wsChars, nwChars []rune
	for _, r := range chars {
		if r == ' ' || r == '\t' || r == '\n' {
			wsChars = append(wsChars, r)
		} else {
			nwChars = append(nwChars, r)
		}
	}
	isWS := func(r rune) bool {
		for _, w := range wsChars {
			if r == w {
				return true
			}
		}
		return false
	}
	isNW := func(r rune) bool {
		for _, w := range nwChars {
			if r == w {
				return true
			}
		}
		return false
	}

	s = strings.TrimFunc(s, isWS)
	if s == "" {
		return nil
	}

	var fields []string
	var current strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		r := runes[i]
		if isNW(r) {
			fields = append(fields, current.String())
			current.Reset()
			i++
			for i < len(runes) && isWS(runes[i]) {
				i++
			}
		} else if isWS(r) {
			fields = append(fields, current.String())
			current.Reset()
			i++
			for i < len(runes) && isWS(runes[i]) {
				i++
			}
		} else {
			current.WriteRune(r)
			i++
		}
	}
	fields = append(fields, current.String())
	return fields
}

func (e *Env) ExitCode() int {
	for c := e; c != nil; c = c.Parent {
		c.ExitMu.Lock()
		has, code := c.HasExit, c.LastExit
		c.ExitMu.Unlock()
		if has {
			return code
		}
	}
	return 0
}

// ShouldExitOnError atomically checks both set -e and exit code.
func (e *Env) ShouldExitOnError() bool {
	if !e.HasFlag('e') {
		return false
	}
	return e.ExitCode() != 0
}

func (e *Env) SetExit(code int) {
	e.ExitMu.Lock()
	e.LastExit = code
	e.HasExit = true
	e.ExitMu.Unlock()
}

func (e *Env) GetShellName() string {
	for c := e; c != nil; c = c.Parent {
		if c.Shell != nil && c.Shell.ShellName != "" {
			return c.Shell.ShellName
		}
	}
	return "ish"
}

// GetAlias looks up an alias in the scope chain.
func (e *Env) GetAlias(name string) (string, bool) {
	for c := e; c != nil; c = c.Parent {
		if c.Shell != nil && c.Shell.Aliases != nil {
			if v, ok := c.Shell.Aliases[name]; ok {
				return v, true
			}
		}
	}
	return "", false
}

func (e *Env) SetAlias(name, value string) {
	sh := e.ensureShell()
	if sh.Aliases == nil {
		sh.Aliases = make(map[string]string)
	}
	sh.Aliases[name] = value
}

func (e *Env) DeleteAlias(name string) {
	sh := e.ensureShell()
	if sh.Aliases != nil {
		delete(sh.Aliases, name)
	}
}

func (e *Env) AllAliases() map[string]string {
	result := make(map[string]string)
	var chain []*Env
	for c := e; c != nil; c = c.Parent {
		chain = append(chain, c)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		if chain[i].Shell != nil && chain[i].Shell.Aliases != nil {
			for k, v := range chain[i].Shell.Aliases {
				result[k] = v
			}
		}
	}
	return result
}

func (e *Env) DeleteVar(name string) error {
	if e.IsReadonly(name) {
		return fmt.Errorf("unset: %s: readonly variable", name)
	}
	for c := e; c != nil; c = c.Parent {
		if c.FlatN >= 0 {
			for i := int8(0); i < c.FlatN; i++ {
				if c.FlatKeys[i] == name {
					// Compact: shift remaining entries down
					c.FlatN--
					for j := i; j < c.FlatN; j++ {
						c.FlatKeys[j] = c.FlatKeys[j+1]
						c.FlatVals[j] = c.FlatVals[j+1]
					}
					c.FlatKeys[c.FlatN] = ""
					c.FlatVals[c.FlatN] = Nil
					return nil
				}
			}
		} else if _, ok := c.Bindings[name]; ok {
			delete(c.Bindings, name)
			return nil
		}
	}
	return nil
}

func (e *Env) DeleteFn(name string) {
	for c := e; c != nil; c = c.Parent {
		if c.Shell == nil || c.Shell.Fns == nil {
			continue
		}
		if _, ok := c.Shell.Fns[name]; ok {
			delete(c.Shell.Fns, name)
			return
		}
	}
}

func (e *Env) Pid() int {
	for c := e; c != nil; c = c.Parent {
		if c.Shell != nil && c.Shell.ShellPid != 0 {
			return c.Shell.ShellPid
		}
	}
	return os.Getpid()
}

func (e *Env) BgPid() int {
	for c := e; c != nil; c = c.Parent {
		if c.Shell != nil && c.Shell.LastBg != 0 {
			return c.Shell.LastBg
		}
	}
	return 0
}

func (e *Env) PosArgs() []string {
	for c := e; c != nil; c = c.Parent {
		if c.Args != nil {
			return c.Args
		}
	}
	return nil
}

func Itoa(n int) string {
	return strconv.Itoa(n)
}
