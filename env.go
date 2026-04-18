package main

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

// Env is a lexical scope. Each scope has bindings and a parent.
type Env struct {
	bindings    map[string]Value
	fns         map[string]*FnValue
	nativeFns   map[string]NativeFn // stdlib native functions
	parent      *Env
	proc        *Process  // owning process (for self/receive)
	stdout      io.Writer // output destination (defaults to os.Stdout)
	cmdSub      CmdSubFunc // for $() expansion in strings
	readonlySet map[string]bool // variables that cannot be reassigned
	exported    map[string]bool // variables marked for export to child processes
	setFlags    map[byte]bool   // shell options: e, u, x, etc.
	traps       map[string]string // signal -> command
	aliases     map[string]string // alias name -> expansion

	// Shell state (exitMu protects lastExit/hasExit for concurrent pipe access)
	exitMu   sync.Mutex
	lastExit int    // $?
	hasExit  bool   // true once setExit has been called in this scope
	shellPid int    // $$
	shellName string // $0
	lastBg   int    // $!
	args     []string // $1, $2, ...
}

// Stdout returns the output writer for this env, walking up to parent if needed.
func (e *Env) Stdout() io.Writer {
	for c := e; c != nil; c = c.parent {
		if c.stdout != nil {
			return c.stdout
		}
	}
	return os.Stdout
}

func NewEnv(parent *Env) *Env {
	return &Env{
		bindings: make(map[string]Value),
		fns:      make(map[string]*FnValue),
		parent:   parent,
	}
}

// CopyEnv creates a snapshot of all visible bindings and functions.
// The new env has no parent — all values are copied into it.
// This is used for spawn to give processes isolated state.
func CopyEnv(src *Env) *Env {
	e := &Env{
		bindings: make(map[string]Value),
		fns:      make(map[string]*FnValue),
		exported: make(map[string]bool),
	}
	// Walk the chain from outermost to innermost so inner values shadow outer
	var chain []*Env
	for c := src; c != nil; c = c.parent {
		chain = append(chain, c)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		c := chain[i]
		for k, v := range c.bindings {
			e.bindings[k] = v
		}
		// Deep-copy FnValues to prevent cross-process mutation
		for k, v := range c.fns {
			copied := &FnValue{Name: v.Name, Clauses: make([]FnClause, len(v.Clauses))}
			copy(copied.Clauses, v.Clauses)
			e.fns[k] = copied
		}
		for k, v := range c.exported {
			if v {
				e.exported[k] = true
			}
		}
		if c.stdout != nil {
			e.stdout = c.stdout
		}
		if c.cmdSub != nil {
			e.cmdSub = c.cmdSub
		}
		// Copy native functions (stdlib) — these are stateless, safe to share
		if c.nativeFns != nil {
			if e.nativeFns == nil {
				e.nativeFns = make(map[string]NativeFn)
			}
			for k, v := range c.nativeFns {
				e.nativeFns[k] = v
			}
		}
		if c.proc != nil && e.proc == nil {
			// Don't copy proc — spawned process gets its own
		}
		if c.shellPid != 0 {
			e.shellPid = c.shellPid
		}
		if c.shellName != "" {
			e.shellName = c.shellName
		}
		// Copy aliases
		if c.aliases != nil {
			if e.aliases == nil {
				e.aliases = make(map[string]string)
			}
			for k, v := range c.aliases {
				e.aliases[k] = v
			}
		}
		// Copy readonly set
		if c.readonlySet != nil {
			if e.readonlySet == nil {
				e.readonlySet = make(map[string]bool)
			}
			for k, v := range c.readonlySet {
				if v {
					e.readonlySet[k] = true
				}
			}
		}
		// Copy shell flags (set -e, -u, -x, etc.)
		if c.setFlags != nil {
			if e.setFlags == nil {
				e.setFlags = make(map[byte]bool)
			}
			for k, v := range c.setFlags {
				e.setFlags[k] = v
			}
		}
		// Copy traps
		if c.traps != nil {
			if e.traps == nil {
				e.traps = make(map[string]string)
			}
			for k, v := range c.traps {
				e.traps[k] = v
			}
		}
	}
	e.setExit(src.exitCode())
	e.args = src.posArgs()
	return e
}

func TopEnv() *Env {
	e := NewEnv(nil)
	e.shellPid = os.Getpid()
	e.exported = make(map[string]bool)
	// Import environment variables and mark them as exported
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			e.bindings[parts[0]] = StringVal(parts[1])
			e.exported[parts[0]] = true
		}
	}
	// Main process gets a mailbox too
	e.proc = NewProcess()
	e.stdout = os.Stdout
	// Register standard library native functions
	RegisterStdlib(e)
	RegisterSerialize(e)
	return e
}

func (e *Env) getProc() *Process {
	for c := e; c != nil; c = c.parent {
		if c.proc != nil {
			return c.proc
		}
	}
	return nil
}

func (e *Env) getCmdSub() CmdSubFunc {
	for c := e; c != nil; c = c.parent {
		if c.cmdSub != nil {
			return c.cmdSub
		}
	}
	return nil
}

func (e *Env) Get(name string) (Value, bool) {
	if v, ok := e.bindings[name]; ok {
		return v, true
	}
	if e.parent != nil {
		return e.parent.Get(name)
	}
	return Nil, false
}

// Set updates an existing variable in the scope chain, or creates a new one
// in the current scope if not found. This implements POSIX semantics where
// assignments in functions update the caller's variables.
func (e *Env) Set(name string, v Value) error {
	if e.IsReadonly(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	// Walk parent chain to find existing binding
	for c := e; c != nil; c = c.parent {
		if _, ok := c.bindings[name]; ok {
			c.bindings[name] = v
			return nil
		}
	}
	// Not found anywhere — write to current scope
	e.bindings[name] = v
	return nil
}

// SetLocal always writes to the current scope, never walking the parent chain.
// Used for pattern matching bindings and the local builtin.
func (e *Env) SetLocal(name string, v Value) error {
	if e.IsReadonly(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	e.bindings[name] = v
	return nil
}

func (e *Env) IsReadonly(name string) bool {
	for c := e; c != nil; c = c.parent {
		if c.readonlySet != nil && c.readonlySet[name] {
			return true
		}
	}
	return false
}

func (e *Env) SetReadonly(name string) {
	if e.readonlySet == nil {
		e.readonlySet = make(map[string]bool)
	}
	e.readonlySet[name] = true
}

func (e *Env) HasFlag(flag byte) bool {
	for c := e; c != nil; c = c.parent {
		if c.setFlags != nil {
			if v, ok := c.setFlags[flag]; ok {
				return v
			}
		}
	}
	return false
}

func (e *Env) SetFlag(flag byte, on bool) {
	if e.setFlags == nil {
		e.setFlags = make(map[byte]bool)
	}
	e.setFlags[flag] = on
}

func (e *Env) GetTrap(sig string) (string, bool) {
	for c := e; c != nil; c = c.parent {
		if c.traps != nil {
			if cmd, ok := c.traps[sig]; ok {
				return cmd, true
			}
		}
	}
	return "", false
}

func (e *Env) SetTrap(sig, cmd string) {
	if e.traps == nil {
		e.traps = make(map[string]string)
	}
	e.traps[sig] = cmd
}

func (e *Env) DeleteTrap(sig string) {
	if e.traps != nil {
		delete(e.traps, sig)
	}
}

func (e *Env) GetFn(name string) (*FnValue, bool) {
	if f, ok := e.fns[name]; ok {
		return f, true
	}
	if e.parent != nil {
		return e.parent.GetFn(name)
	}
	return nil, false
}

// GetNativeFn looks up a stdlib native function in the scope chain.
func (e *Env) GetNativeFn(name string) (NativeFn, bool) {
	for c := e; c != nil; c = c.parent {
		if c.nativeFns != nil {
			if fn, ok := c.nativeFns[name]; ok {
				return fn, true
			}
		}
	}
	return nil, false
}

// SetNativeFn registers a native function in this scope.
func (e *Env) SetNativeFn(name string, fn NativeFn) {
	if e.nativeFns == nil {
		e.nativeFns = make(map[string]NativeFn)
	}
	e.nativeFns[name] = fn
}

func (e *Env) SetFn(name string, f *FnValue) {
	// If fn already exists in this scope, append clause
	if existing, ok := e.fns[name]; ok {
		existing.Clauses = append(existing.Clauses, f.Clauses...)
		return
	}
	e.fns[name] = f
}

// Export marks a variable as exported and sets its value.
// Exported variables are passed to child processes via BuildEnv().
func (e *Env) Export(name, val string) {
	e.Set(name, StringVal(val))
	if e.exported == nil {
		e.exported = make(map[string]bool)
	}
	e.exported[name] = true
}

// ExportName marks an existing variable as exported without changing its value.
func (e *Env) ExportName(name string) {
	if e.exported == nil {
		e.exported = make(map[string]bool)
	}
	e.exported[name] = true
}

// isExported returns whether a variable is marked for export anywhere in the chain.
func (e *Env) isExported(name string) bool {
	for c := e; c != nil; c = c.parent {
		if c.exported != nil && c.exported[name] {
			return true
		}
	}
	return false
}

// BuildEnv returns the environment variables for child processes.
// It collects all exported bindings from the scope chain (innermost wins).
func (e *Env) BuildEnv() []string {
	seen := make(map[string]bool)
	var result []string
	for c := e; c != nil; c = c.parent {
		for name, val := range c.bindings {
			if seen[name] {
				continue
			}
			seen[name] = true
			if e.isExported(name) {
				result = append(result, name+"="+val.ToStr())
			}
		}
	}
	return result
}

// Expand performs tilde expansion, parameter expansion, and ish interpolation.
func (e *Env) Expand(s string) string {
	// Tilde expansion (before parameter expansion)
	s = e.expandTilde(s)

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
			// Try as variable first, then as expression via cmdSub
			if v, ok := e.Get(expr); ok {
				buf.WriteString(v.ToStr())
			} else if fn := e.getCmdSub(); fn != nil {
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
			// Check for $(( — arithmetic expansion
			if i+1 < len(s) && s[i+1] == '(' {
				// $(( arithmetic ))
				i += 2 // skip ((
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
					i += 2 // skip ))
				}
				// Evaluate via cmdSub callback which handles $((expr))
				if fn := e.getCmdSub(); fn != nil {
					result, _ := fn("echo $(("+expr+"))", e)
					buf.WriteString(result)
				}
				continue
			}
			// $(...) command substitution
			i++ // skip (
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
				i++ // skip )
			}
			if fn := e.getCmdSub(); fn != nil {
				result, _ := fn(cmdStr, e)
				buf.WriteString(result)
			}
		case '?':
			buf.WriteString(itoa(e.exitCode()))
			i++
		case '$':
			buf.WriteString(itoa(e.pid()))
			i++
		case '!':
			buf.WriteString(itoa(e.bgPid()))
			i++
		case '#':
			buf.WriteString(itoa(len(e.posArgs())))
			i++
		case '@':
			buf.WriteString(strings.Join(e.posArgs(), " "))
			i++
		case '*':
			// $* joins with first char of IFS (or space if IFS unset)
			sep := " "
			if ifs, ok := e.Get("IFS"); ok {
				ifsStr := ifs.ToStr()
				if len(ifsStr) > 0 {
					sep = ifsStr[:1]
				} else {
					sep = ""
				}
			}
			buf.WriteString(strings.Join(e.posArgs(), sep))
			i++
		case '{':
			i++
			start := i
			for i < len(s) && s[i] != '}' {
				i++
			}
			expr := s[start:i]
			if i < len(s) {
				i++ // skip }
			}
			// ${#var} — string length
			if len(expr) > 1 && expr[0] == '#' {
				varName := expr[1:]
				if v, ok := e.Get(varName); ok {
					buf.WriteString(strconv.Itoa(len(v.ToStr())))
				} else {
					buf.WriteString("0")
				}
				continue
			}

			// ${var#pattern}, ${var##pattern}, ${var%pattern}, ${var%%pattern}
			// ${var/pattern/replacement}, ${var//pattern/replacement}
			if handled := e.expandParamOp(expr, &buf); handled {
				continue
			}

			// Handle ${VAR:-default}, ${VAR:+alt}, ${VAR:=assign}, ${VAR:?err}
			// Also ${VAR-default} (without colon — only checks unset, not empty)
			if idx := strings.IndexAny(expr, ":-+?="); idx > 0 {
				name := expr[:idx]
				op := expr[idx:]
				// Strip leading : if present
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
					case '-': // ${VAR:-default} or ${VAR-default}
						if isUnset || (hasColon && isEmpty) {
							buf.WriteString(e.Expand(defaultVal))
						} else {
							buf.WriteString(v.ToStr())
						}
					case '+': // ${VAR:+alt}
						if !isUnset && !(hasColon && isEmpty) {
							buf.WriteString(e.Expand(defaultVal))
						}
					case '=': // ${VAR:=default}
						if isUnset || (hasColon && isEmpty) {
							expanded := e.Expand(defaultVal)
							e.Set(name, StringVal(expanded))
							buf.WriteString(expanded)
						} else {
							buf.WriteString(v.ToStr())
						}
					case '?': // ${VAR:?error}
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
				// Consume all consecutive digits for multi-digit params ($10, $11, etc.)
				start := i
				for i < len(s) && s[i] >= '0' && s[i] <= '9' {
					i++
				}
				idxStr := s[start:i]
				idx, _ := strconv.Atoi(idxStr)
				if idx == 0 {
					// $0 — shell/script name
					buf.WriteString(e.getShellName())
				} else {
					args := e.posArgs()
					if idx > 0 && idx <= len(args) {
						buf.WriteString(args[idx-1])
					}
				}
			} else {
				start := i
				for i < len(s) && isVarChar(s[i]) {
					i++
				}
				name := s[start:i]
				if name == "" {
					// Bare $ not followed by a var char — emit literal $
					buf.WriteByte('$')
				} else if v, ok := e.Get(name); ok {
					buf.WriteString(v.ToStr())
				}
			}
		}
	}
	return buf.String()
}

// expandParamOp handles ${var#pattern}, ${var##pattern}, ${var%pattern},
// ${var%%pattern}, ${var/pattern/replacement}, ${var//pattern/replacement}.
// Returns true if the expression was handled.
// shellGlobMatch matches a string against a shell glob pattern where * matches
// any character (including /), unlike filepath.Match where * doesn't match /.
func shellGlobMatch(pattern, s string) bool {
	// Simple recursive glob matcher
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
	// Find the operator position — scan for first #, %, or / that's not at position 0
	for i := 1; i < len(expr); i++ {
		ch := expr[i]
		if ch == '#' || ch == '%' || ch == '/' {
			name := expr[:i]
			op := expr[i:]
			v, ok := e.Get(name)
			if !ok {
				return true // variable not set, expand to empty
			}
			val := v.ToStr()

			switch {
			case strings.HasPrefix(op, "##"):
				// Longest prefix removal — try longest first
				pattern := op[2:]
				for j := len(val); j >= 0; j-- {
					if shellGlobMatch(pattern, val[:j]) {
						buf.WriteString(val[j:])
						return true
					}
				}
				buf.WriteString(val)
				return true
			case ch == '#':
				// Shortest prefix removal — try shortest first
				pattern := op[1:]
				for j := 0; j <= len(val); j++ {
					if shellGlobMatch(pattern, val[:j]) {
						buf.WriteString(val[j:])
						return true
					}
				}
				buf.WriteString(val)
				return true
			case strings.HasPrefix(op, "%%"):
				// Longest suffix removal — try from start
				pattern := op[2:]
				for j := 0; j <= len(val); j++ {
					if shellGlobMatch(pattern, val[j:]) {
						buf.WriteString(val[:j])
						return true
					}
				}
				buf.WriteString(val)
				return true
			case ch == '%':
				// Shortest suffix removal — try from end
				pattern := op[1:]
				for j := len(val); j >= 0; j-- {
					if shellGlobMatch(pattern, val[j:]) {
						buf.WriteString(val[:j])
						return true
					}
				}
				buf.WriteString(val)
				return true
			case strings.HasPrefix(op, "//"):
				// Replace all occurrences
				parts := strings.SplitN(op[2:], "/", 2)
				old := parts[0]
				newStr := ""
				if len(parts) > 1 {
					newStr = parts[1]
				}
				buf.WriteString(strings.ReplaceAll(val, old, newStr))
				return true
			case ch == '/':
				// Replace first occurrence
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

func isVarChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func (e *Env) expandTilde(s string) string {
	if len(s) == 0 || s[0] != '~' {
		return s
	}
	// ~ alone or ~/path
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
	// ~user/path
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

// expandTildeStatic is the standalone version used before an Env is available.
func expandTilde(s string) string {
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

// SplitFieldsIFS splits on IFS characters. If IFS is not set, splits on whitespace.
func (e *Env) SplitFieldsIFS(s string) []string {
	if ifs, ok := e.Get("IFS"); ok {
		ifsStr := ifs.ToStr()
		if ifsStr == "" {
			return []string{s} // empty IFS = no splitting
		}
		return splitOnChars(s, ifsStr)
	}
	return strings.Fields(s) // default: split on whitespace
}

func splitOnChars(s, chars string) []string {
	if chars == "" {
		return []string{s}
	}
	// Separate IFS chars into whitespace and non-whitespace
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

	// Trim leading/trailing IFS whitespace
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
			// Non-whitespace delimiter: produces a field boundary
			fields = append(fields, current.String())
			current.Reset()
			i++
			// Skip any adjacent IFS whitespace
			for i < len(runes) && isWS(runes[i]) {
				i++
			}
		} else if isWS(r) {
			// IFS whitespace: skip all consecutive IFS chars (ws + nw)
			fields = append(fields, current.String())
			current.Reset()
			i++
			for i < len(runes) && isWS(runes[i]) {
				i++
			}
			// If we stopped at a non-whitespace delimiter, don't double-split
			// (the next iteration will handle it)
		} else {
			current.WriteRune(r)
			i++
		}
	}
	fields = append(fields, current.String())
	return fields
}

func (e *Env) exitCode() int {
	for c := e; c != nil; c = c.parent {
		c.exitMu.Lock()
		has, code := c.hasExit, c.lastExit
		c.exitMu.Unlock()
		if has {
			return code
		}
	}
	return 0
}

// shouldExitOnError atomically checks both set -e and exit code,
// reducing the TOCTOU window vs separate HasFlag('e') + exitCode() calls.
func (e *Env) shouldExitOnError() bool {
	if !e.HasFlag('e') {
		return false
	}
	return e.exitCode() != 0
}

func (e *Env) setExit(code int) {
	e.exitMu.Lock()
	e.lastExit = code
	e.hasExit = true
	e.exitMu.Unlock()
}

func (e *Env) getShellName() string {
	for c := e; c != nil; c = c.parent {
		if c.shellName != "" {
			return c.shellName
		}
	}
	return "ish"
}

// GetAlias looks up an alias in the scope chain.
func (e *Env) GetAlias(name string) (string, bool) {
	for c := e; c != nil; c = c.parent {
		if c.aliases != nil {
			if v, ok := c.aliases[name]; ok {
				return v, true
			}
		}
	}
	return "", false
}

// SetAlias defines an alias in the current scope.
func (e *Env) SetAlias(name, value string) {
	if e.aliases == nil {
		e.aliases = make(map[string]string)
	}
	e.aliases[name] = value
}

// DeleteAlias removes an alias from the current scope.
func (e *Env) DeleteAlias(name string) {
	if e.aliases != nil {
		delete(e.aliases, name)
	}
}

// AllAliases returns all visible aliases (inner scope wins).
func (e *Env) AllAliases() map[string]string {
	result := make(map[string]string)
	// Walk chain from outermost to innermost (inner wins)
	var chain []*Env
	for c := e; c != nil; c = c.parent {
		chain = append(chain, c)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		for k, v := range chain[i].aliases {
			result[k] = v
		}
	}
	return result
}

// DeleteVar removes a variable from the scope chain.
func (e *Env) DeleteVar(name string) {
	for c := e; c != nil; c = c.parent {
		if _, ok := c.bindings[name]; ok {
			delete(c.bindings, name)
			return
		}
	}
}

// DeleteFn removes a function from the scope chain.
func (e *Env) DeleteFn(name string) {
	for c := e; c != nil; c = c.parent {
		if _, ok := c.fns[name]; ok {
			delete(c.fns, name)
			return
		}
	}
}

func (e *Env) pid() int {
	for c := e; c != nil; c = c.parent {
		if c.shellPid != 0 {
			return c.shellPid
		}
	}
	return os.Getpid()
}

func (e *Env) bgPid() int {
	for c := e; c != nil; c = c.parent {
		if c.lastBg != 0 {
			return c.lastBg
		}
	}
	return 0
}

func (e *Env) posArgs() []string {
	for c := e; c != nil; c = c.parent {
		if c.args != nil {
			return c.args
		}
	}
	return nil
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
