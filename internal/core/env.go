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
type Env struct {
	Bindings    map[string]Value
	Fns         map[string]*FnValue
	NativeFns   map[string]NativeFn // stdlib native functions
	Parent      *Env
	Proc        Pid       // owning process (for self/receive)
	Stdout_     io.Writer // output destination (defaults to os.Stdout)
	CmdSub      CmdSubFunc // for $() expansion in strings
	ReadonlySet map[string]bool // variables that cannot be reassigned
	Exported    map[string]bool // variables marked for export to child processes
	SetFlags    map[byte]bool   // shell options: e, u, x, etc.
	Traps       map[string]string // signal -> command
	Aliases     map[string]string // alias name -> expansion

	// CallFn is set by eval to allow stdlib/process to call user functions.
	CallFn func(fn *FnValue, args []Value, env *Env) (Value, error)

	// Debugger holds a *debug.Debugger when active, nil otherwise.
	// Uses interface{} to avoid circular import between core and debug.
	Debugger interface{}

	// Source and SourceName track the current source text for lazy debugger creation.
	Source     string // current source text
	SourceName string // filename, "<repl>", or "<stdin>"

	ExprMode int // >0 when evaluating ish expressions (not POSIX commands)

	// Shell state (ExitMu protects LastExit/HasExit for concurrent pipe access)
	ExitMu   sync.Mutex
	LastExit int    // $?
	HasExit  bool   // true once SetExit has been called in this scope
	ShellPid int    // $$
	ShellName    string // $0
	LastBg       int    // $!
	Args         []string // $1, $2, ...
	IsLoginShell bool
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

func NewEnv(parent *Env) *Env {
	env := &Env{
		Bindings: make(map[string]Value),
		Fns:      make(map[string]*FnValue),
		Parent:   parent,
	}
	// Inherit CallFn and Debugger from parent
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
	e := &Env{
		Bindings: make(map[string]Value),
		Fns:      make(map[string]*FnValue),
		Exported: make(map[string]bool),
	}
	// Walk the chain from outermost to innermost so inner values shadow outer
	var chain []*Env
	for c := src; c != nil; c = c.Parent {
		chain = append(chain, c)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		c := chain[i]
		for k, v := range c.Bindings {
			e.Bindings[k] = v
		}
		// Deep-copy FnValues to prevent cross-process mutation
		for k, v := range c.Fns {
			copied := &FnValue{Name: v.Name, Clauses: make([]FnClause, len(v.Clauses))}
			copy(copied.Clauses, v.Clauses)
			e.Fns[k] = copied
		}
		for k, v := range c.Exported {
			if v {
				e.Exported[k] = true
			}
		}
		if c.Stdout_ != nil {
			e.Stdout_ = c.Stdout_
		}
		if c.CmdSub != nil {
			e.CmdSub = c.CmdSub
		}
		// Copy native functions (stdlib) -- these are stateless, safe to share
		if c.NativeFns != nil {
			if e.NativeFns == nil {
				e.NativeFns = make(map[string]NativeFn)
			}
			for k, v := range c.NativeFns {
				e.NativeFns[k] = v
			}
		}
		if c.ShellPid != 0 {
			e.ShellPid = c.ShellPid
		}
		if c.ShellName != "" {
			e.ShellName = c.ShellName
		}
		// Copy CallFn
		if c.CallFn != nil {
			e.CallFn = c.CallFn
		}
		// Copy aliases
		if c.Aliases != nil {
			if e.Aliases == nil {
				e.Aliases = make(map[string]string)
			}
			for k, v := range c.Aliases {
				e.Aliases[k] = v
			}
		}
		// Copy readonly set
		if c.ReadonlySet != nil {
			if e.ReadonlySet == nil {
				e.ReadonlySet = make(map[string]bool)
			}
			for k, v := range c.ReadonlySet {
				if v {
					e.ReadonlySet[k] = true
				}
			}
		}
		// Copy shell flags (set -e, -u, -x, etc.)
		if c.SetFlags != nil {
			if e.SetFlags == nil {
				e.SetFlags = make(map[byte]bool)
			}
			for k, v := range c.SetFlags {
				e.SetFlags[k] = v
			}
		}
		// Copy traps
		if c.Traps != nil {
			if e.Traps == nil {
				e.Traps = make(map[string]string)
			}
			for k, v := range c.Traps {
				e.Traps[k] = v
			}
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
	e.ShellPid = os.Getpid()
	e.Exported = make(map[string]bool)
	// Import environment variables and mark them as exported
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			e.Bindings[parts[0]] = StringVal(parts[1])
			e.Exported[parts[0]] = true
		}
	}
	e.Stdout_ = os.Stdout
	return e
}

func (e *Env) GetProc() Pid {
	for c := e; c != nil; c = c.Parent {
		if c.Proc != nil {
			return c.Proc
		}
	}
	return nil
}

func (e *Env) GetCmdSub() CmdSubFunc {
	for c := e; c != nil; c = c.Parent {
		if c.CmdSub != nil {
			return c.CmdSub
		}
	}
	return nil
}

func (e *Env) Get(name string) (Value, bool) {
	if v, ok := e.Bindings[name]; ok {
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
		if _, ok := c.Bindings[name]; ok {
			c.Bindings[name] = v
			return nil
		}
	}
	// Not found anywhere -- write to current scope
	e.Bindings[name] = v
	return nil
}

// SetLocal always writes to the current scope, never walking the parent chain.
func (e *Env) SetLocal(name string, v Value) error {
	if e.IsReadonly(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	e.Bindings[name] = v
	return nil
}

func (e *Env) IsReadonly(name string) bool {
	for c := e; c != nil; c = c.Parent {
		if c.ReadonlySet != nil && c.ReadonlySet[name] {
			return true
		}
	}
	return false
}

func (e *Env) SetReadonly(name string) {
	if e.ReadonlySet == nil {
		e.ReadonlySet = make(map[string]bool)
	}
	e.ReadonlySet[name] = true
}

func (e *Env) HasFlag(flag byte) bool {
	for c := e; c != nil; c = c.Parent {
		if c.SetFlags != nil {
			if v, ok := c.SetFlags[flag]; ok {
				return v
			}
		}
	}
	return false
}

func (e *Env) SetFlag(flag byte, on bool) {
	if e.SetFlags == nil {
		e.SetFlags = make(map[byte]bool)
	}
	e.SetFlags[flag] = on
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
		if c.Traps != nil {
			if cmd, ok := c.Traps[sig]; ok {
				return cmd, true
			}
		}
	}
	return "", false
}

func (e *Env) SetTrap(sig, cmd string) {
	if e.Traps == nil {
		e.Traps = make(map[string]string)
	}
	e.Traps[sig] = cmd
}

func (e *Env) DeleteTrap(sig string) {
	if e.Traps != nil {
		delete(e.Traps, sig)
	}
}

func (e *Env) GetFn(name string) (*FnValue, bool) {
	if f, ok := e.Fns[name]; ok {
		return f, true
	}
	if e.Parent != nil {
		return e.Parent.GetFn(name)
	}
	return nil, false
}

// GetNativeFn looks up a stdlib native function in the scope chain.
func (e *Env) GetNativeFn(name string) (NativeFn, bool) {
	for c := e; c != nil; c = c.Parent {
		if c.NativeFns != nil {
			if fn, ok := c.NativeFns[name]; ok {
				return fn, true
			}
		}
	}
	return nil, false
}

// SetNativeFn registers a native function in this scope.
func (e *Env) SetNativeFn(name string, fn NativeFn) {
	if e.NativeFns == nil {
		e.NativeFns = make(map[string]NativeFn)
	}
	e.NativeFns[name] = fn
}

// SetFn appends clauses to an existing function (for multi-definition
// single-clause dispatch like fn fib 0 do...end; fn fib 1 do...end).
func (e *Env) SetFn(name string, f *FnValue) {
	if existing, ok := e.Fns[name]; ok {
		existing.Clauses = append(existing.Clauses, f.Clauses...)
		return
	}
	e.Fns[name] = f
}

// ReplaceFn fully replaces a function definition (for arrow-clause
// dispatch tables like fn name do pattern -> body ... end).
func (e *Env) ReplaceFn(name string, f *FnValue) {
	e.Fns[name] = f
}

// Export marks a variable as exported and sets its value.
func (e *Env) Export(name, val string) {
	e.Set(name, StringVal(val))
	if e.Exported == nil {
		e.Exported = make(map[string]bool)
	}
	e.Exported[name] = true
}

// ExportName marks an existing variable as exported without changing its value.
func (e *Env) ExportName(name string) {
	if e.Exported == nil {
		e.Exported = make(map[string]bool)
	}
	e.Exported[name] = true
}

func (e *Env) IsExported(name string) bool {
	for c := e; c != nil; c = c.Parent {
		if c.Exported != nil && c.Exported[name] {
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

			if handled := e.expandParamOp(expr, &buf); handled {
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
		if c.ShellName != "" {
			return c.ShellName
		}
	}
	return "ish"
}

// GetAlias looks up an alias in the scope chain.
func (e *Env) GetAlias(name string) (string, bool) {
	for c := e; c != nil; c = c.Parent {
		if c.Aliases != nil {
			if v, ok := c.Aliases[name]; ok {
				return v, true
			}
		}
	}
	return "", false
}

func (e *Env) SetAlias(name, value string) {
	if e.Aliases == nil {
		e.Aliases = make(map[string]string)
	}
	e.Aliases[name] = value
}

func (e *Env) DeleteAlias(name string) {
	if e.Aliases != nil {
		delete(e.Aliases, name)
	}
}

func (e *Env) AllAliases() map[string]string {
	result := make(map[string]string)
	var chain []*Env
	for c := e; c != nil; c = c.Parent {
		chain = append(chain, c)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		for k, v := range chain[i].Aliases {
			result[k] = v
		}
	}
	return result
}

func (e *Env) DeleteVar(name string) {
	for c := e; c != nil; c = c.Parent {
		if _, ok := c.Bindings[name]; ok {
			delete(c.Bindings, name)
			return
		}
	}
}

func (e *Env) DeleteFn(name string) {
	for c := e; c != nil; c = c.Parent {
		if _, ok := c.Fns[name]; ok {
			delete(c.Fns, name)
			return
		}
	}
}

func (e *Env) Pid() int {
	for c := e; c != nil; c = c.Parent {
		if c.ShellPid != 0 {
			return c.ShellPid
		}
	}
	return os.Getpid()
}

func (e *Env) BgPid() int {
	for c := e; c != nil; c = c.Parent {
		if c.LastBg != 0 {
			return c.LastBg
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
