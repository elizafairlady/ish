package eval

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"ish/internal/ast"
	"ish/internal/lexer"
	"ish/internal/parser"
	"ish/internal/value"
)

var (
	errBreak    = fmt.Errorf("break")
	errContinue = fmt.Errorf("continue")
)

type errReturn struct{ val value.Value }

func (e *errReturn) Error() string { return "return" }


type errExit struct{ code int }

func (e *errExit) Error() string { return fmt.Sprintf("exit %d", e.code) }


func Run(input string, scope Scope) value.Value {
	tokens := lexer.Lex(input)
	p := parser.New(tokens)
	tree, err := p.Parse()
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %s\n", err)
		return value.Nil
	}
	v, evalErr := eval(tree, scope, false)
	if evalErr != nil {
		if _, ok := evalErr.(*errExit); ok {
			return v
		}
	}
	return v
}

// RunExitTraps runs any registered EXIT trap. Called by the shell's main loop on exit.
func RunExitTraps(scope Scope) {
	if scope.GetCtx().Shell.Traps != nil {
		if trap, ok := scope.GetCtx().Shell.Traps["EXIT"]; ok {
			delete(scope.GetCtx().Shell.Traps, "EXIT")
			Run(trap, scope)
		}
	}
}

func eval(node *ast.Node, scope Scope, tailPos bool) (value.Value, error) {
	switch node.Kind {
	case ast.NBlock:
		// Prefix assign block: save/set/run/restore
		if node.Tok.Val == "prefix" && len(node.Children) == 2 {
			assign := node.Children[0]
			cmd := node.Children[1]
			// Save old value
			varName := assign.Children[0].Tok.Val
			oldVal, hadOld := scope.Get(varName)
			// Set temporary
			rhs, err := eval(assign.Children[1], scope, false)
			if err != nil {
				return value.Nil, err
			}
			scope.Set(varName, rhs)
			// Run command
			result, err := eval(cmd, scope, false)
			// Restore
			if hadOld {
				scope.Set(varName, oldVal)
			} else {
				scope.NearestEnv().Delete(varName)
			}
			return result, err
		}
		var last value.Value
		lastIdx := len(node.Children) - 1
		for i, child := range node.Children {
			isTail := tailPos && i == lastIdx
			v, err := eval(child, scope, isTail)
			if err != nil {
				return v, err
			}
			last = v
			scope.GetCtx().SetExit(v.ExitCode())
			if scope.GetCtx().Shell.HasFlag('e') && !v.Truthy() {
				if child.Kind != ast.NIf && child.Kind != ast.NAndList && child.Kind != ast.NOrList {
					return v, nil
				}
			}
		}
		return last, nil

	case ast.NLit:
		if len(node.Children) > 0 {
			return eval(node.Children[0], scope, false)
		}
		return evalLit(node.Tok), nil

	case ast.NAtom:
		return value.AtomVal(node.Tok.Val), nil

	case ast.NIdent:
		name := node.Tok.Val
		// &fn capture — lookup without auto-call
		if strings.HasPrefix(name, "&") {
			if v, ok := scope.Get(name[1:]); ok {
				return v, nil
			}
			return value.Nil, nil
		}
		if v, ok := scope.Get(name); ok {
			// Zero-arity auto-call: only when the ident matches the fn's own name.
			// This distinguishes definitions (fn greeting do ... end; greeting)
			// from variables holding fn values (x = &greeting; typeof(x)).
			if v.Kind == value.VFn && v.Fn() != nil && v.Fn().Name == name &&
				v.Fn().Native == nil && len(v.Fn().Clauses) > 0 &&
				len(v.Fn().Clauses[0].Params) == 0 {
				return callFn(v.Fn(), nil, nil, nil, scope)
			}
			return v, nil
		}
		switch name {
		case "nil":
			return value.Nil, nil
		case "true":
			return value.True, nil
		case "false":
			return value.False, nil
		case "self":
			if p, ok := scope.Get("__self"); ok {
				return p, nil
			}
		}
		return value.StringVal(name), nil

	case ast.NVarRef:
		name := node.Tok.Val
		switch name {
		case "$?":
			return value.IntVal(int64(scope.GetCtx().ExitCode())), nil
		case "$$":
			return value.IntVal(int64(scope.GetCtx().Pid())), nil
		case "$!":
			return value.IntVal(int64(scope.GetCtx().LastBg)), nil
		case "$@", "$*":
			return value.StringVal(strings.Join(scope.GetCtx().Args, " ")), nil
		case "$#":
			return value.IntVal(int64(len(scope.GetCtx().Args))), nil
		}
		// Strip leading $ for lookup (special vars like $1 stored as "1")
		lookup := name
		if len(name) > 0 && name[0] == '$' {
			lookup = name[1:]
		}
		if v, ok := scope.Get(lookup); ok {
			return v, nil
		}
		if v, ok := scope.Get(name); ok {
			return v, nil
		}
		// Positional params from Ctx.Args
		if n, err := strconv.Atoi(lookup); err == nil {
			if n == 0 {
				return value.StringVal(scope.GetCtx().ShellName), nil
			}
			if n >= 1 && n <= len(scope.GetCtx().Args) {
				return value.StringVal(scope.GetCtx().Args[n-1]), nil
			}
			return value.StringVal(""), nil
		}
		if ev, ok := os.LookupEnv(lookup); ok {
			return value.StringVal(ev), nil
		}
		return value.StringVal(""), nil

	case ast.NBind:
		return evalBind(node, scope)

	case ast.NBinOp:
		return evalBinOp(node, scope)

	case ast.NUnary:
		return evalUnary(node, scope)

	case ast.NList:
		return evalList(node, scope)

	case ast.NTuple:
		return evalTuple(node, scope)

	case ast.NApply:
		return evalApply(node, scope, tailPos)

	case ast.NCall:
		return evalCall(node, scope, tailPos)

	case ast.NAndList:
		left, err := eval(node.Children[0], scope, false)
		if err != nil {
			return left, err
		}
		if !left.Truthy() {
			return left, nil
		}
		return eval(node.Children[1], scope, false)

	case ast.NOrList:
		left, err := eval(node.Children[0], scope, false)
		if err != nil {
			return left, err
		}
		if left.Truthy() {
			return left, nil
		}
		return eval(node.Children[1], scope, false)

	case ast.NPipe:
		return evalPipe(node, scope)

	case ast.NPipeAmp:
		return evalPipeAmp(node, scope)

	case ast.NPipeFn:
		return evalPipeFn(node, scope)

	case ast.NIf:
		return evalIf(node, scope, tailPos)

	case ast.NFor:
		return evalFor(node, scope)

	case ast.NWhile:
		return evalWhile(node, scope)

	case ast.NFnDef:
		return evalFnDef(node, scope)

	case ast.NLambda:
		return evalLambda(node, scope)

	case ast.NAccess:
		return evalAccess(node, scope)

	case ast.NFlag:
		return value.StringVal(node.Tok.Val), nil

	case ast.NPath:
		path := node.Tok.Val
		if strings.HasPrefix(path, "~") {
			home := os.Getenv("HOME")
			path = home + path[1:]
		}
		return value.StringVal(path), nil

	case ast.NIPv4, ast.NIPv6:
		return value.StringVal(node.Tok.Val), nil

	case ast.NCmdSub:
		return evalCmdSub(node, scope)

	case ast.NParamExpand:
		return evalParamExpand(node, scope)

	case ast.NInterpStr:
		return evalInterpStr(node, scope)

	case ast.NMatch:
		return evalMatch(node, scope)

	case ast.NCons:
		return evalCons(node, scope)

	case ast.NMap:
		return evalMap(node, scope)

	case ast.NCase:
		return evalCase(node, scope)

	case ast.NDefModule:
		return evalDefModule(node, scope)

	case ast.NUseImport:
		return evalUseImport(node, scope)

	case ast.NReceive:
		return evalReceive(node, scope)

	case ast.NTry:
		return evalTry(node, scope)

	case ast.NSubshell:
		origCwd, _ := os.Getwd()
		sub := copyEnv(scope)
		var last value.Value
		for _, child := range node.Children {
			v, err := eval(child, sub, false)
			if err != nil {
				os.Chdir(origCwd)
				if exitErr, ok := err.(*errExit); ok {
					if exitErr.code == 0 {
						return value.OkVal(value.Nil), nil
					}
					return value.ErrorVal(exitErr.code), nil
				}
				return v, err
			}
			last = v
		}
		os.Chdir(origCwd)
		return last, nil

	case ast.NBg:
		return evalBg(node, scope)

	default:
		return value.Nil, fmt.Errorf("unhandled node kind: %d", node.Kind)
	}
}

func evalLit(tok ast.Token) value.Value {
	switch tok.Type {
	case ast.TInt:
		n, _ := strconv.ParseInt(tok.Val, 10, 64)
		return value.IntVal(n)
	case ast.TFloat:
		f, _ := strconv.ParseFloat(tok.Val, 64)
		return value.FloatVal(f)
	case ast.TString:
		return value.StringVal(tok.Val)
	}
	switch tok.Val {
	case "nil":
		return value.Nil
	case "true":
		return value.True
	case "false":
		return value.False
	}
	return value.StringVal(tok.Val)
}

func evalBind(node *ast.Node, scope Scope) (value.Value, error) {
	lhs := node.Children[0]
	rhs, err := eval(node.Children[1], scope, false)
	if err != nil {
		return value.Nil, err
	}
	if lhs.Kind == ast.NIdent {
		if node.Tok.SpaceAfter {
			// Ish binding (x = val) — lexical scope
			scope.SetLocal(lhs.Tok.Val, rhs)
		} else {
			// POSIX assign (X=val) — walks parent chain
			scope.Set(lhs.Tok.Val, rhs)
		}
		return rhs, nil
	}
	if !matchPattern(lhs, rhs, scope) {
		return value.Nil, fmt.Errorf("match error: pattern does not match value %s", rhs)
	}
	return rhs, nil
}

func coerceNumeric(v value.Value) value.Value {
	if v.Kind != value.VString {
		return v
	}
	s := v.Str()
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return value.IntVal(n)
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return value.FloatVal(f)
	}
	return v
}

func evalBinOp(node *ast.Node, scope Scope) (value.Value, error) {
	left, err := eval(node.Children[0], scope, false)
	if err != nil {
		return value.Nil, err
	}
	right, err := eval(node.Children[1], scope, false)
	if err != nil {
		return value.Nil, err
	}

	op := node.Tok.Type
	kinds := left.Kind & right.Kind

	// String concat fast path: + with a string operand is always concat
	if op == ast.TPlus && (left.Kind == value.VString || right.Kind == value.VString) {
		return value.StringVal(left.ToStr() + right.ToStr()), nil
	}

	// Stage 1: coerce strings to numeric only for arithmetic/comparison ops
	if left.Kind&value.KindStr != 0 || right.Kind&value.KindStr != 0 {
		left = coerceNumeric(left)
		right = coerceNumeric(right)
		kinds = left.Kind & right.Kind
	}

	// Stage 2: dispatch by combined type flags
	if kinds&value.KindInt != 0 {
		// Both int — fast path
		a, b := left.Int(), right.Int()
		switch op {
		case ast.TPlus:    return value.IntVal(a + b), nil
		case ast.TMinus:   return value.IntVal(a - b), nil
		case ast.TStar:    return value.IntVal(a * b), nil
		case ast.TSlash:
			if b == 0 { return value.Nil, fmt.Errorf("division by zero") }
			return value.IntVal(a / b), nil
		case ast.TPercent:
			if b == 0 { return value.Nil, fmt.Errorf("modulo by zero") }
			return value.IntVal(a % b), nil
		case ast.TGt:      return value.BoolVal(a > b), nil
		case ast.TLt:      return value.BoolVal(a < b), nil
		case ast.TGtEq:    return value.BoolVal(a >= b), nil
		case ast.TLtEq:    return value.BoolVal(a <= b), nil
		case ast.TEqEq:    return value.BoolVal(a == b), nil
		case ast.TBangEq:  return value.BoolVal(a != b), nil
		}
	}

	// Float promotion: both numeric but not both int (at least one float)
	if kinds&value.KindNumeric != 0 {
		a := left.Float()
		if left.Kind&value.KindInt != 0 {
			a = float64(left.Int())
		}
		b := right.Float()
		if right.Kind&value.KindInt != 0 {
			b = float64(right.Int())
		}
		switch node.Tok.Type {
		case ast.TPlus:
			return value.FloatVal(a + b), nil
		case ast.TMinus:
			return value.FloatVal(a - b), nil
		case ast.TStar:
			return value.FloatVal(a * b), nil
		case ast.TSlash:
			if b == 0 {
				return value.Nil, fmt.Errorf("division by zero")
			}
			return value.FloatVal(a / b), nil
		case ast.TGt:
			return value.BoolVal(a > b), nil
		case ast.TLt:
			return value.BoolVal(a < b), nil
		case ast.TGtEq:
			return value.BoolVal(a >= b), nil
		case ast.TLtEq:
			return value.BoolVal(a <= b), nil
		case ast.TEqEq:
			return value.BoolVal(a == b), nil
		case ast.TBangEq:
			return value.BoolVal(a != b), nil
		}
	}

	if node.Tok.Type == ast.TEqEq {
		return value.BoolVal(left.Equal(right)), nil
	}
	if node.Tok.Type == ast.TBangEq {
		return value.BoolVal(!left.Equal(right)), nil
	}

	return value.Nil, fmt.Errorf("unsupported binop: %s %s %s", left, node.Tok.Val, right)
}

func evalUnary(node *ast.Node, scope Scope) (value.Value, error) {
	operand, err := eval(node.Children[0], scope, false)
	if err != nil {
		return value.Nil, err
	}
	switch node.Tok.Type {
	case ast.TBang:
		return value.BoolVal(!operand.Truthy()), nil
	case ast.TMinus:
		if operand.Kind == value.VInt {
			return value.IntVal(-operand.Int()), nil
		}
		if operand.Kind == value.VFloat {
			return value.FloatVal(-operand.Float()), nil
		}
	}
	return value.Nil, fmt.Errorf("invalid unary: %s %s", node.Tok.Val, operand)
}

func evalList(node *ast.Node, scope Scope) (value.Value, error) {
	elems := make([]value.Value, len(node.Children))
	for i, child := range node.Children {
		v, err := eval(child, scope, false)
		if err != nil {
			return value.Nil, err
		}
		elems[i] = v
	}
	return value.ListVal(elems...), nil
}

func evalTuple(node *ast.Node, scope Scope) (value.Value, error) {
	elems := make([]value.Value, len(node.Children))
	for i, child := range node.Children {
		v, err := eval(child, scope, false)
		if err != nil {
			return value.Nil, err
		}
		elems[i] = v
	}
	return value.TupleVal(elems...), nil
}

func evalApply(node *ast.Node, scope Scope, tailPos bool) (value.Value, error) {
	if len(node.Children) == 0 {
		return value.Nil, nil
	}

	head := node.Children[0]

	// Single-element apply with a non-command head is a value, not a command
	// Exception: compound words with dots may be module paths (e.g., M.foo)
	if len(node.Children) == 1 && head.Kind != ast.NIdent && head.Kind != ast.NPath {
		if head.Kind != ast.NLit || !strings.Contains(head.Tok.Val, ".") {
			return eval(head, scope, tailPos)
		}
	}

	var name string
	if head.Kind == ast.NIdent {
		name = head.Tok.Val
	} else {
		v, err := eval(head, scope, false)
		if err != nil {
			return value.Nil, err
		}
		// Head evaluated to a function (e.g., module access) — call directly
		if v.Kind == value.VFn {
			args := make([]value.Value, 0, len(node.Children)-1)
			for _, child := range node.Children[1:] {
				a, err := eval(child, scope, false)
				if err != nil {
					return value.Nil, err
				}
				args = append(args, a)
			}
			return callFn(v.Fn(), nil, args, nil, scope)
		}
		name = v.ToStr()
	}

	// Alias expansion
	if scope.GetCtx().Shell.Aliases != nil {
		if expanded, ok := scope.GetCtx().Shell.Aliases[name]; ok {
			// Re-parse and evaluate the expanded alias + remaining args
			var sb strings.Builder
			sb.WriteString(expanded)
			for _, child := range node.Children[1:] {
				if child.Kind == ast.NIdent {
					sb.WriteByte(' ')
					sb.WriteString(child.Tok.Val)
				} else {
					v, err := eval(child, scope, false)
					if err != nil {
						return value.Nil, err
					}
					sb.WriteByte(' ')
					sb.WriteString(v.ToStr())
				}
			}
			return Run(sb.String(), scope), nil
		}
	}

	// Break/continue/return as commands
	switch name {
	case "break":
		return value.Nil, errBreak
	case "continue":
		return value.Nil, errContinue
	case "return":
		if len(node.Children) > 1 {
			v, err := eval(node.Children[1], scope, false)
			if err != nil {
				return value.Nil, err
			}
			// Integer arg → POSIX exit code
			if v.Kind == value.VInt {
				code := int(v.Int())
				if code == 0 {
					return value.Nil, &errReturn{val: value.OkVal(value.Nil)}
				}
				return value.Nil, &errReturn{val: value.ErrorVal(code)}
			}
			return value.Nil, &errReturn{val: v}
		}
		return value.Nil, &errReturn{val: value.OkVal(value.Nil)}
	}

	// OTP primitives
	switch name {
	case "spawn":
		return evalSpawn(node, scope)
	case "spawn_link":
		return evalSpawnLink(node, scope)
	case "send":
		return evalSend(node, scope)
	case "await":
		return evalAwait(node, scope)
	case "monitor":
		return evalMonitor(node, scope)
	case "self":
		if p, ok := scope.Get("__self"); ok {
			return p, nil
		}
		return value.Nil, nil
	}

	// Single-element apply: if name is a bound non-fn value, return it
	if len(node.Children) == 1 {
		if val, ok := scope.Get(name); ok && val.Kind != value.VFn {
			return val, nil
		}
	}

	// User-defined function
	if fn, ok := scope.Get(name); ok && fn.Kind == value.VFn {
		// Check for $@ expansion — needs special handling
		hasDollarAt := false
		for _, child := range node.Children[1:] {
			if child.Kind == ast.NVarRef && (child.Tok.Val == "$@" || child.Tok.Val == "$*") {
				hasDollarAt = true
				break
			}
		}
		if hasDollarAt {
			// $@ expands to multiple args — can't use stack buffer
			var args []value.Value
			for _, child := range node.Children[1:] {
				if child.Kind == ast.NVarRef && (child.Tok.Val == "$@" || child.Tok.Val == "$*") {
					for _, a := range scope.GetCtx().Args {
						args = append(args, value.StringVal(a))
					}
					continue
				}
				v, err := eval(child, scope, false)
				if err != nil {
					return value.Nil, err
				}
				args = append(args, v)
			}
			if tailPos {
				return value.TailCallVal(fn.Fn(), args, scope), nil
			}
			return callFn(fn.Fn(), nil, args, nil, scope)
		}
		if tailPos {
			argc := len(node.Children) - 1
			tcArgs := make([]value.Value, argc)
			for i, child := range node.Children[1:] {
				v, err := eval(child, scope, false)
				if err != nil { return value.Nil, err }
				tcArgs[i] = v
			}
			return value.TailCallVal(fn.Fn(), tcArgs, scope), nil
		}
		return callFn(fn.Fn(), node, nil, nil, scope)
	}

	// Dot path: Module.fn or map.field
	if dotIdx := strings.IndexByte(name, '.'); dotIdx > 0 {
		modName, fieldName := name[:dotIdx], name[dotIdx+1:]
		if mod, ok := scope.Get(modName); ok && mod.Kind == value.VMap {
			if val, ok := mod.Map().Vals[fieldName]; ok {
				if val.Kind == value.VFn {
					// Evaluate args in caller's scope
					dotArgc := len(node.Children) - 1
					dotArgs := make([]value.Value, dotArgc)
					for i, child := range node.Children[1:] {
						v, err := eval(child, scope, false)
						if err != nil { return value.Nil, err }
						dotArgs[i] = v
					}
					return callFn(val.Fn(), nil, dotArgs, nil, scope)
				}
				// Non-fn map field access (e.g., m.name)
				if len(node.Children) == 1 {
					return val, nil
				}
			}
		}
	}

	// String args for builtins and external commands.
	// NIdent is a literal string (bare word). NVarRef resolves.
	strArgs := make([]string, 0, len(node.Children)-1)
	for _, child := range node.Children[1:] {
		var s string
		if child.Kind == ast.NIdent {
			s = child.Tok.Val
		} else {
			v, err := eval(child, scope, false)
			if err != nil {
				return value.Nil, err
			}
			s = v.ToStr()
		}
		// Glob expansion for args containing * or ?
		if strings.ContainsAny(s, "*?") {
			if matches, err := filepath.Glob(s); err == nil && len(matches) > 0 {
				strArgs = append(strArgs, matches...)
				continue
			}
		}
		strArgs = append(strArgs, s)
	}

	if b, ok := getBuiltin(name); ok {
		return b(strArgs, scope, node.Redirs)
	}

	// External command
	return execExternal(name, strArgs, scope, node.Redirs)
}

func evalCall(node *ast.Node, scope Scope, tailPos bool) (value.Value, error) {
	callee, err := eval(node.Children[0], scope, false)
	if err != nil {
		return value.Nil, err
	}

	// Module path resolution for compound callees (e.g., List.append)
	if callee.Kind == value.VString {
		name := callee.Str()
		if dotIdx := strings.IndexByte(name, '.'); dotIdx > 0 {
			modName, fieldName := name[:dotIdx], name[dotIdx+1:]
			if mod, ok := scope.Get(modName); ok && mod.Kind == value.VMap {
				if fn, ok := mod.Map().Vals[fieldName]; ok && fn.Kind == value.VFn {
					callee = fn
				}
			}
		}
	}
	if callee.Kind != value.VFn {
		return value.Nil, fmt.Errorf("not callable: %s", callee)
	}
	if tailPos {
		argc := len(node.Children) - 1
		tcArgs := make([]value.Value, argc)
		for i, child := range node.Children[1:] {
			v, err := eval(child, scope, false)
			if err != nil { return value.Nil, err }
			tcArgs[i] = v
		}
		return value.TailCallVal(callee.Fn(), tcArgs, scope), nil
	}
	return callFn(callee.Fn(), node, nil, scope, scope)
}

func callFn(fn *value.FnDef, node *ast.Node, preArgs []value.Value, evalScope Scope, scope Scope) (value.Value, error) {
	var argBuf [MaxFlatBindings]value.Value
	var args []value.Value
	var argc int
	var useHeap bool

	if node != nil {
		argScope := scope
		if evalScope != nil {
			argScope = evalScope
		}
		argc = len(node.Children) - 1
		useHeap = argc > MaxFlatBindings
		if !useHeap {
			for i, c := range node.Children[1:] {
				v, err := eval(c, argScope, false)
				if err != nil { return value.Nil, err }
				argBuf[i] = v
			}
		} else {
			args = make([]value.Value, argc)
			for i, c := range node.Children[1:] {
				v, err := eval(c, argScope, false)
				if err != nil { return value.Nil, err }
				args[i] = v
			}
		}
	} else {
		argc = len(preArgs)
		useHeap = argc > MaxFlatBindings
		if !useHeap {
			copy(argBuf[:argc], preArgs)
		} else {
			args = preArgs
		}
	}

	parentScope := Scope(scope)
	if fn.Env != nil {
		if captured, ok := fn.Env.(Scope); ok {
			parentScope = captured
		}
	}

	for {
		if fn.Native != nil {
			nativeArgs := make([]value.Value, argc)
			for i := 0; i < argc; i++ {
				if useHeap { nativeArgs[i] = args[i] } else { nativeArgs[i] = argBuf[i] }
			}
			return fn.Native(nativeArgs)
		}

		frame := NewFrame(parentScope)

		// POSIX compat: set positional args for zero-param functions
		if argc > 0 && len(fn.Params) == 0 {
			strArgs := make([]string, argc)
			for i := 0; i < argc; i++ {
				if useHeap { strArgs[i] = args[i].ToStr() } else { strArgs[i] = argBuf[i].ToStr() }
			}
			ctxCopy := frame.Ctx.ForRedirect(frame.Ctx.Stdout)
			ctxCopy.Args = strArgs
			frame.Ctx = ctxCopy
		}

		if len(fn.Clauses) > 1 {
			matched := false
			for _, clause := range fn.Clauses {
				if len(clause.Patterns) != 0 && len(clause.Patterns) != argc { continue }
				frame.ResetFlat()
				ok := true
				for i, pat := range clause.Patterns {
					var a value.Value
					if i < argc {
						if useHeap { a = args[i] } else { a = argBuf[i] }
					}
					if !matchPattern(pat.(*ast.Node), a, frame) { ok = false; break }
				}
				if !ok { continue }
				if clause.Guard != nil {
					gv, gerr := eval(clause.Guard.(*ast.Node), frame, false)
					if gerr != nil || !gv.Truthy() { continue }
				}
				if clause.Body == nil { putFrame(frame); return value.Nil, nil }
				val, err := eval(clause.Body.(*ast.Node), frame, true)
				if ret, ok := err.(*errReturn); ok { putFrame(frame); return ret.val, nil }
				if err != nil { putFrame(frame); return val, err }
				if val.Kind == value.VTailCall {
					newFn := val.GetTailFn()
					tailArgs := val.GetTailArgs()
					if newFn != fn {
						if newFn.Env != nil {
							if captured, ok := newFn.Env.(Scope); ok { parentScope = captured }
						} else if val.GetTailEnv() != nil {
							if s, ok := val.GetTailEnv().(Scope); ok { parentScope = captureEnv(s) }
						}
					}
					fn = newFn; argc = len(tailArgs)
					useHeap = argc > MaxFlatBindings
					if !useHeap { copy(argBuf[:argc], tailArgs) } else { args = tailArgs }
					putFrame(frame)
					matched = true
					break
				}
				putFrame(frame)
				return val, nil
			}
			if !matched {
				putFrame(frame)
				return value.Nil, fmt.Errorf("no matching clause for %s", fn.Name)
			}
		} else {
			for i, param := range fn.Params {
				if i < argc {
					if useHeap { frame.SetLocal(param, args[i]) } else { frame.SetLocal(param, argBuf[i]) }
				} else {
					frame.SetLocal(param, value.Nil)
				}
			}
			if fn.Body == nil { putFrame(frame); return value.Nil, nil }
			val, err := eval(fn.Body.(*ast.Node), frame, true)
			if ret, ok := err.(*errReturn); ok { putFrame(frame); return ret.val, nil }
			if err != nil { putFrame(frame); return val, err }
			if val.Kind == value.VTailCall {
				newFn := val.GetTailFn()
				tailArgs := val.GetTailArgs()
				if newFn != fn {
					if newFn.Env != nil {
						if captured, ok := newFn.Env.(Scope); ok { parentScope = captured }
					} else if val.GetTailEnv() != nil {
						if s, ok := val.GetTailEnv().(Scope); ok { parentScope = captureEnv(s) }
					}
				}
				fn = newFn; argc = len(tailArgs)
				useHeap = argc > MaxFlatBindings
				if !useHeap { copy(argBuf[:argc], tailArgs) } else { args = tailArgs }
				putFrame(frame)
				continue
			}
			putFrame(frame)
			return val, nil
		}
	}
}

func evalBg(node *ast.Node, scope Scope) (value.Value, error) {
	child := node.Children[0]

	// For external commands, use OS-level background with process groups
	if child.Kind == ast.NApply && len(child.Children) > 0 && child.Children[0].Kind == ast.NIdent {
		name := child.Children[0].Tok.Val
		if _, ok := getBuiltin(name); !ok {
			if _, err := exec.LookPath(name); err == nil {
				return execExternalBg(child, name, scope)
			}
		}
	}

	// For builtins and ish functions, run in goroutine with job tracking
	cmdStr := "bg"
	if child.Kind == ast.NApply && len(child.Children) > 0 {
		cmdStr = child.Children[0].Tok.Val
	}
	j := scope.GetCtx().Jobs.Add(0, cmdStr, nil)
	scope.GetCtx().BgJobs.Add(1)
	go func() {
		defer scope.GetCtx().BgJobs.Done()
		eval(child, scope, false)
		j.SetDone(0)
		scope.GetCtx().Jobs.Remove(j.ID)
	}()
	return value.OkVal(value.Nil), nil
}

func execExternalBg(node *ast.Node, name string, scope Scope) (value.Value, error) {
	var args []string
	for _, child := range node.Children[1:] {
		if child.Kind == ast.NIdent {
			args = append(args, child.Tok.Val)
		} else {
			v, err := eval(child, scope, false)
			if err != nil {
				return value.Nil, err
			}
			args = append(args, v.ToStr())
		}
	}

	cmd := exec.Command(name, args...)
	cmd.Stdin = scope.GetCtx().StdinOrDefault()
	cmd.Stdout = scope.GetCtx().Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = scope.NearestEnv().BuildEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return value.ErrorVal(127), nil
	}

	pid := cmd.Process.Pid
	cmdStr := name
	if len(args) > 0 {
		cmdStr += " " + strings.Join(args, " ")
	}
	j := scope.GetCtx().Jobs.Add(pid, cmdStr, cmd.Process)
	scope.GetCtx().LastBg = pid
	fmt.Fprintf(os.Stderr, "[%d] %d\n", j.ID, pid)

	scope.GetCtx().BgJobs.Add(1)
	go func() {
		defer scope.GetCtx().BgJobs.Done()
		state, _ := cmd.Process.Wait()
		code := 0
		if state != nil {
			code = state.ExitCode()
		}
		j.SetDone(code)
		scope.GetCtx().Jobs.Remove(j.ID)
	}()

	return value.OkVal(value.Nil), nil
}

func execExternal(name string, args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = scope.GetCtx().StdinOrDefault()
	cmd.Stdout = scope.GetCtx().Stdout
	cmd.Stderr = scope.GetCtx().StderrOrDefault()
	cmd.Env = scope.NearestEnv().BuildEnv()

	for _, r := range redirs {
		target, _ := eval(r.Target, scope, false)
		if r.FdDup {
			fdStr := target.ToStr()
			switch fdStr {
			case "1":
				if r.Fd == 2 {
					cmd.Stderr = cmd.Stdout
				}
			case "2":
				if r.Fd == 1 {
					cmd.Stdout = cmd.Stderr
				}
			}
			continue
		}
		switch r.Op {
		case ast.THeredoc:
			cmd.Stdin = strings.NewReader(target.ToStr())
			continue
		case ast.TGt:
			f, err := os.Create(target.ToStr())
			if err != nil {
				return value.ErrorVal(1), nil
			}
			defer f.Close()
			if r.Fd == 1 {
				cmd.Stdout = f
			} else if r.Fd == 2 {
				cmd.Stderr = f
			}
		case ast.TAppend:
			f, err := os.OpenFile(target.ToStr(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return value.ErrorVal(1), nil
			}
			defer f.Close()
			if r.Fd == 1 {
				cmd.Stdout = f
			} else if r.Fd == 2 {
				cmd.Stderr = f
			}
		case ast.TLt:
			f, err := os.Open(target.ToStr())
			if err != nil {
				return value.ErrorVal(1), nil
			}
			defer f.Close()
			cmd.Stdin = f
		}
	}

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return value.ErrorVal(exitErr.ExitCode()), nil
		}
		return value.ErrorVal(127), nil
	}
	return value.OkVal(value.Nil), nil
}

func evalPipe(node *ast.Node, scope Scope) (value.Value, error) {
	// Byte pipe: run left, pipe stdout to right stdin
	r, w, err := os.Pipe()
	if err != nil {
		return value.Nil, err
	}

	leftEnv := copyEnv(scope)
	leftEnv.Ctx.Stdout = w

	var leftResult value.Value
	done := make(chan struct{})
	go func() {
		v, _ := eval(node.Children[0], leftEnv, false)
		leftResult = v
		// Auto-coerce value to text for pipe
		if v.Kind != value.VNil && v.Kind != value.VTuple {
			if v.Kind == value.VList {
				for _, elem := range v.Elems() {
					fmt.Fprintln(w, elem.ToStr())
				}
			} else if v.Kind == value.VMap {
				m := v.Map()
				for _, k := range m.Keys {
					fmt.Fprintf(w, "%s: %s\n", k, m.Vals[k].ToStr())
				}
			} else {
				fmt.Fprintln(w, v.ToStr())
			}
		}
		w.Close()
		close(done)
	}()

	rightEnv := copyEnv(scope)
	rightEnv.Ctx.Stdin = r
	result, evalErr := eval(node.Children[1], rightEnv, false)
	r.Close()
	<-done

	// Pipefail: if any stage failed, report highest exit code
	if scope.GetCtx().Shell.HasFlag('P') {
		leftCode := leftResult.ExitCode()
		rightCode := result.ExitCode()
		if leftCode != 0 && rightCode == 0 {
			return value.ErrorVal(leftCode), evalErr
		}
	}
	return result, evalErr
}

func evalPipeAmp(node *ast.Node, scope Scope) (value.Value, error) {
	// |& pipes both stdout and stderr from left to right's stdin
	r, w, err := os.Pipe()
	if err != nil {
		return value.Nil, err
	}

	leftEnv := copyEnv(scope)
	leftEnv.Ctx.Stdout = w
	leftEnv.Ctx.Stderr = w // stderr also goes to pipe

	done := make(chan struct{})
	go func() {
		eval(node.Children[0], leftEnv, false)
		w.Close()
		close(done)
	}()

	rightEnv := copyEnv(scope)
	rightEnv.Ctx.Stdin = r
	result, evalErr := eval(node.Children[1], rightEnv, false)
	r.Close()
	<-done
	return result, evalErr
}

func evalPipeFn(node *ast.Node, scope Scope) (value.Value, error) {
	leftNode := node.Children[0]
	right := node.Children[1]

	left, err := eval(leftNode, scope, false)
	if err != nil {
		return value.Nil, err
	}

	// Right side is expression context. Resolve callable and explicit args.
	// NCall: f(a, b) → call f(left, a, b)
	// NApply: f a, b → call f(left, a, b)  (bare-arg call in expr context)
	// NAccess: M.f → call f(left)
	// NIdent: f → call f(left)
	// NLambda: \x -> body → call lambda(left)
	var callee value.Value
	var extraArgs []value.Value

	switch right.Kind {
	case ast.NCall:
		// Paren call: callee is right.Children[0], args are right.Children[1:]
		callee, err = eval(right.Children[0], scope, false)
		if err != nil {
			return value.Nil, err
		}
		for _, child := range right.Children[1:] {
			v, e := eval(child, scope, false)
			if e != nil {
				return value.Nil, e
			}
			extraArgs = append(extraArgs, v)
		}
	case ast.NApply:
		// Bare-arg call: head is right.Children[0], args are right.Children[1:]
		callee, err = eval(right.Children[0], scope, false)
		if err != nil {
			return value.Nil, err
		}
		for _, child := range right.Children[1:] {
			v, e := eval(child, scope, false)
			if e != nil {
				return value.Nil, e
			}
			extraArgs = append(extraArgs, v)
		}
	case ast.NLambda:
		callee, err = evalLambda(right, scope)
		if err != nil {
			return value.Nil, err
		}
	default:
		// NIdent, NAccess, anything else: evaluate to get callable
		callee, err = eval(right, scope, false)
		if err != nil {
			return value.Nil, err
		}
	}

	if callee.Kind != value.VFn {
		return value.Nil, fmt.Errorf("|> right side not callable: %s", callee)
	}

	// Build args: left first, then any explicit args from the call
	args := make([]value.Value, 0, 1+len(extraArgs))
	args = append(args, left)
	args = append(args, extraArgs...)

	return callFn(callee.Fn(), nil, args, nil, scope)
}


func evalIf(node *ast.Node, scope Scope, tailPos bool) (value.Value, error) {
	for _, clause := range node.Clauses {
		if clause.Pattern == nil {
			return eval(clause.Body, scope, tailPos)
		}
		cond, err := eval(clause.Pattern, scope, false)
		if err != nil {
			return value.Nil, err
		}
		if cond.Truthy() {
			return eval(clause.Body, scope, tailPos)
		}
	}
	return value.Nil, nil
}

func evalFor(node *ast.Node, scope Scope) (value.Value, error) {
	varName := node.Children[0].Tok.Val
	iterNode := node.Children[1]
	body := node.Children[2]

	var items []value.Value
	switch iterNode.Kind {
	case ast.NList:
		for _, item := range iterNode.Children {
			v, err := eval(item, scope, false)
			if err != nil {
				return value.Nil, err
			}
			// "$@" expands to a list — flatten it
			if v.Kind == value.VList {
				items = append(items, v.Elems()...)
			} else if v.Kind == value.VString && (item.Kind == ast.NCmdSub || item.Kind == ast.NVarRef || item.Kind == ast.NParamExpand) {
				// Word-split string results from expansions (POSIX field splitting)
				for _, word := range strings.Fields(v.ToStr()) {
					items = append(items, value.StringVal(word))
				}
			} else {
				items = append(items, v)
			}
		}
	default:
		// Evaluate and word-split the result
		v, err := eval(iterNode, scope, false)
		if err != nil {
			return value.Nil, err
		}
		if v.Kind == value.VList {
			items = v.Elems()
		} else {
			// Word split on whitespace (IFS)
			for _, word := range strings.Fields(v.ToStr()) {
				items = append(items, value.StringVal(word))
			}
		}
	}

	var last value.Value
	for _, item := range items {
		scope.Set(varName, item)
		var err error
		last, err = eval(body, scope, false)
		if err == errBreak {
			return last, nil
		}
		if err == errContinue {
			continue
		}
		if err != nil {
			return last, err
		}
	}
	return last, nil
}

func evalWhile(node *ast.Node, scope Scope) (value.Value, error) {
	cond := node.Children[0]
	body := node.Children[1]
	negate := node.Tok.Val == "until"
	var last value.Value
	for {
		cv, err := eval(cond, scope, false)
		if err != nil {
			return value.Nil, err
		}
		truthy := cv.Truthy()
		if negate {
			truthy = !truthy
		}
		if !truthy {
			break
		}
		last, err = eval(body, scope, false)
		if err == errBreak {
			return last, nil
		}
		if err == errContinue {
			continue
		}
		if err != nil {
			return last, err
		}
	}
	return last, nil
}

func evalFnDef(node *ast.Node, scope Scope) (value.Value, error) {
	name := node.Children[0].Tok.Val

	var params []string
	var patterns []interface{}
	var bodyNode *ast.Node
	hasPatterns := false

	var guardNode interface{}
	for i := 1; i < len(node.Children); i++ {
		child := node.Children[i]
		if child.Kind == ast.NBlock {
			bodyNode = child
			break
		}
		if child.Kind == ast.NUnary && child.Tok.Val == "when" {
			guardNode = child.Children[0]
			continue
		}
		if child.Kind == ast.NIdent {
			params = append(params, child.Tok.Val)
			patterns = append(patterns, child)
		} else {
			hasPatterns = true
			params = append(params, fmt.Sprintf("_arg%d", i))
			patterns = append(patterns, child)
		}
	}

	clause := value.FnClause{
		Params:   params,
		Patterns: patterns,
		Guard:    guardNode,
		Body:     bodyNode,
	}

	// Clause-block fn: fn name do\n pattern -> body\n end
	if len(node.Clauses) > 0 {
		var clauses []value.FnClause
		for _, cl := range node.Clauses {
			clauses = append(clauses, value.FnClause{
				Params:   []string{"_arg"},
				Patterns: []interface{}{cl.Pattern},
				Body:     cl.Body,
			})
		}
		fn := &value.FnDef{
			Name:    name,
			Params:  []string{"_arg"},
			Clauses: clauses,
		}
		scope.Set(name, value.FnVal(fn))
		return value.FnVal(fn), nil
	}

	if existing, ok := scope.Get(name); ok && existing.Kind == value.VFn && existing.Fn() != nil {
		fn := existing.Fn()
		fn.Clauses = append(fn.Clauses, clause)
		if !hasPatterns {
			fn.Params = params
			fn.Body = bodyNode
		}
		return existing, nil
	}

	fn := &value.FnDef{
		Name:    name,
		Params:  params,
		Body:    bodyNode,
		Clauses: []value.FnClause{clause},
	}
	scope.Set(name, value.FnVal(fn))
	return value.FnVal(fn), nil
}

func evalLambda(node *ast.Node, scope Scope) (value.Value, error) {
	params := make([]string, 0, len(node.Children)-1)
	for _, child := range node.Children[:len(node.Children)-1] {
		params = append(params, child.Tok.Val)
	}
	body := node.Children[len(node.Children)-1]
	fn := &value.FnDef{
		Name:   "<lambda>",
		Params: params,
		Body:   body,
		Env:    captureEnv(scope),
	}
	return value.FnVal(fn), nil
}

// captureEnv returns a scope suitable for closure capture.
// If scope is a Frame, snapshots it into an Env so the Frame
// can be safely returned to the pool.
func captureEnv(scope Scope) Scope {
	if f, ok := scope.(*Frame); ok {
		return f.Snapshot()
	}
	return scope
}

func evalAccess(node *ast.Node, scope Scope) (value.Value, error) {
	obj, err := eval(node.Children[0], scope, false)
	if err != nil {
		return value.Nil, err
	}
	field := node.Tok.Val
	if obj.Kind == value.VMap {
		m := obj.Map()
		if m != nil {
			if v, ok := m.Vals[field]; ok {
				// Zero-arity auto-call for user-defined module functions
				if v.Kind == value.VFn && v.Fn() != nil && v.Fn().Native == nil &&
					len(v.Fn().Params) == 0 && v.Fn().Name == field {
					return callFn(v.Fn(), nil, nil, nil, scope)
				}
				return v, nil
			}
		}
	}
	return value.Nil, fmt.Errorf("cannot access .%s on %s", field, obj)
}

func evalCmdSub(node *ast.Node, scope Scope) (value.Value, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return value.Nil, err
	}
	subEnv := copyEnv(scope)
	subEnv.Ctx.Stdout = w

	go func() {
		var last value.Value
		for _, child := range node.Children {
			v, _ := eval(child, subEnv, false)
			last = v
		}
		if last.Kind != value.VNil && last.Kind != value.VTuple && last.Kind != value.VString {
			fmt.Fprint(w, last.ToStr())
		} else if last.Kind == value.VString && last.Str() != "" {
			fmt.Fprint(w, last.Str())
		}
		w.Close()
	}()

	var buf strings.Builder
	io.Copy(&buf, r)
	r.Close()
	result := strings.TrimRight(buf.String(), "\n")
	return value.StringVal(result), nil
}

func evalParamExpand(node *ast.Node, scope Scope) (value.Value, error) {
	raw := node.Tok.Val

	// ${#var} — length
	if strings.HasPrefix(raw, "#") {
		varName := raw[1:]
		if v, ok := scope.Get(varName); ok {
			return value.IntVal(int64(len(v.ToStr()))), nil
		}
		return value.IntVal(0), nil
	}

	// ${var:-default}, ${var:=default}, ${var:+alt}
	for _, op := range []string{":-", ":=", ":+"} {
		if idx := strings.Index(raw, op); idx >= 0 {
			varName := raw[:idx]
			defVal := raw[idx+len(op):]
			val, ok := scope.Get(varName)
			if !ok {
				if ev, exists := os.LookupEnv(varName); exists {
					val = value.StringVal(ev)
					ok = true
				}
			}
			isEmpty := !ok || val.ToStr() == ""
			switch op {
			case ":-":
				if isEmpty {
					return value.StringVal(defVal), nil
				}
				return val, nil
			case ":=":
				if isEmpty {
					scope.Set(varName, value.StringVal(defVal))
					return value.StringVal(defVal), nil
				}
				return val, nil
			case ":+":
				if !isEmpty {
					return value.StringVal(defVal), nil
				}
				return value.StringVal(""), nil
			}
		}
	}

	// ${var%%pattern} — remove longest suffix (check before single %)
	if idx := strings.Index(raw, "%%"); idx >= 0 {
		varName, pattern := raw[:idx], raw[idx+2:]
		s := getVar(varName, scope)
		for i := 0; i <= len(s); i++ {
			if shellMatch(pattern, s[i:]) {
				return value.StringVal(s[:i]), nil
			}
		}
		return value.StringVal(s), nil
	}
	// ${var%pattern} — remove shortest suffix
	if idx := strings.Index(raw, "%"); idx >= 0 {
		varName, pattern := raw[:idx], raw[idx+1:]
		s := getVar(varName, scope)
		for i := len(s); i >= 0; i-- {
			if shellMatch(pattern, s[i:]) {
				return value.StringVal(s[:i]), nil
			}
		}
		return value.StringVal(s), nil
	}
	// ${var##pattern} — remove longest prefix (check before single #)
	if idx := strings.Index(raw, "##"); idx >= 0 {
		varName, pattern := raw[:idx], raw[idx+2:]
		s := getVar(varName, scope)
		for i := len(s); i >= 0; i-- {
			if shellMatch(pattern, s[:i]) {
				return value.StringVal(s[i:]), nil
			}
		}
		return value.StringVal(s), nil
	}
	// ${var#pattern} — remove shortest prefix (already handled length above)
	if idx := strings.Index(raw, "#"); idx >= 0 {
		varName, pattern := raw[:idx], raw[idx+1:]
		s := getVar(varName, scope)
		for i := 0; i <= len(s); i++ {
			if shellMatch(pattern, s[:i]) {
				return value.StringVal(s[i:]), nil
			}
		}
		return value.StringVal(s), nil
	}
	// ${var//pat/rep} — replace all (check before single /)
	if idx := strings.Index(raw, "//"); idx >= 0 {
		varName := raw[:idx]
		rest := raw[idx+2:]
		slashIdx := strings.Index(rest, "/")
		if slashIdx >= 0 {
			pat, rep := rest[:slashIdx], rest[slashIdx+1:]
			return value.StringVal(strings.ReplaceAll(getVar(varName, scope), pat, rep)), nil
		}
	}
	// ${var/pat/rep} — replace first
	if idx := strings.Index(raw, "/"); idx >= 0 {
		varName := raw[:idx]
		rest := raw[idx+1:]
		slashIdx := strings.Index(rest, "/")
		if slashIdx >= 0 {
			pat, rep := rest[:slashIdx], rest[slashIdx+1:]
			return value.StringVal(strings.Replace(getVar(varName, scope), pat, rep, 1)), nil
		}
	}

	// Plain ${var}
	if v, ok := scope.Get(raw); ok {
		return v, nil
	}
	if ev, ok := os.LookupEnv(raw); ok {
		return value.StringVal(ev), nil
	}
	return value.StringVal(""), nil
}

// shellMatch does shell-style pattern matching where * matches any characters
// including /. This differs from filepath.Match which treats / as special.
func shellMatch(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	// Simple cases: prefix*, *suffix, *middle*
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		return strings.Contains(s, pattern[1:len(pattern)-1])
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(s, pattern[1:])
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(s, pattern[:len(pattern)-1])
	}
	// No wildcard — literal
	if !strings.Contains(pattern, "*") && !strings.Contains(pattern, "?") {
		return pattern == s
	}
	// Fall back to filepath.Match for ? and complex patterns
	matched, _ := filepath.Match(pattern, s)
	return matched
}

func getVar(name string, scope Scope) string {
	if v, ok := scope.Get(name); ok {
		return v.ToStr()
	}
	if ev, ok := os.LookupEnv(name); ok {
		return ev
	}
	return ""
}

func evalInterpStr(node *ast.Node, scope Scope) (value.Value, error) {
	// "$@" as sole content → expand to list of individual args
	if len(node.Children) == 1 {
		child := node.Children[0]
		if child.Kind == ast.NVarRef && child.Tok.Val == "$@" {
			args := scope.GetCtx().Args
			elems := make([]value.Value, len(args))
			for i, a := range args {
				elems[i] = value.StringVal(a)
			}
			return value.ListVal(elems...), nil
		}
	}
	var buf strings.Builder
	for _, child := range node.Children {
		v, err := eval(child, scope, false)
		if err != nil {
			return value.Nil, err
		}
		buf.WriteString(v.ToStr())
	}
	return value.StringVal(buf.String()), nil
}

// ============================================================
// Pattern matching
// ============================================================

func evalMatch(node *ast.Node, scope Scope) (value.Value, error) {
	subject, err := eval(node.Children[0], scope, false)
	if err != nil {
		return value.Nil, err
	}
	for _, clause := range node.Clauses {
		child := newChildEnv(scope)
		if matchPattern(clause.Pattern, subject, child) {
			if clause.Guard != nil {
				gv, err := eval(clause.Guard, child, false)
				if err != nil || !gv.Truthy() {
					continue
				}
			}
			return eval(clause.Body, child, false)
		}
	}
	return value.Nil, nil
}

func matchPattern(pattern *ast.Node, val value.Value, scope Scope) bool {
	switch pattern.Kind {
	case ast.NIdent:
		name := pattern.Tok.Val
		if name == "_" {
			return true
		}
		// Check for literal match (nil, true, false) vs binding
		switch name {
		case "nil":
			return val.Kind == value.VNil
		case "true":
			return val.Equal(value.True)
		case "false":
			return val.Equal(value.False)
		}
		scope.SetLocal(name, val)
		return true

	case ast.NLit:
		// Fast path: compare int/float literals without allocating a Value
		switch pattern.Tok.Type {
		case ast.TInt:
			if val.Kind&value.KindInt != 0 {
				n, _ := strconv.ParseInt(pattern.Tok.Val, 10, 64)
				return val.Int() == n
			}
			return false
		case ast.TFloat:
			if val.Kind&value.KindNumeric != 0 {
				f, _ := strconv.ParseFloat(pattern.Tok.Val, 64)
				if val.Kind&value.KindInt != 0 {
					return float64(val.Int()) == f
				}
				return val.Float() == f
			}
			return false
		}
		return val.Equal(evalLit(pattern.Tok))

	case ast.NAtom:
		return val.Kind == value.VAtom && val.Str() == pattern.Tok.Val

	case ast.NTuple:
		if val.Kind != value.VTuple {
			return false
		}
		elems := val.Elems()
		if len(elems) != len(pattern.Children) {
			return false
		}
		for i, child := range pattern.Children {
			if !matchPattern(child, elems[i], scope) {
				return false
			}
		}
		return true

	case ast.NList:
		if val.Kind != value.VList {
			return false
		}
		elems := val.Elems()
		if len(elems) != len(pattern.Children) {
			return false
		}
		for i, child := range pattern.Children {
			if !matchPattern(child, elems[i], scope) {
				return false
			}
		}
		return true

	case ast.NMap:
		if val.Kind != value.VMap {
			return false
		}
		m := val.Map()
		if m == nil {
			return false
		}
		for i := 0; i+1 < len(pattern.Children); i += 2 {
			key := pattern.Children[i].Tok.Val
			v, ok := m.Vals[key]
			if !ok {
				return false
			}
			if !matchPattern(pattern.Children[i+1], v, scope) {
				return false
			}
		}
		return true

	case ast.NCons:
		if val.Kind != value.VList {
			return false
		}
		elems := val.Elems()
		heads := pattern.Children[:len(pattern.Children)-1]
		tail := pattern.Children[len(pattern.Children)-1]
		if len(elems) < len(heads) {
			return false
		}
		for i, h := range heads {
			if !matchPattern(h, elems[i], scope) {
				return false
			}
		}
		rest := value.ListVal(elems[len(heads):]...)
		return matchPattern(tail, rest, scope)
	}
	return false
}

// ============================================================
// Destructuring in evalBind
// ============================================================

// ============================================================
// Cons, Map, Case
// ============================================================

func evalCons(node *ast.Node, scope Scope) (value.Value, error) {
	heads := node.Children[:len(node.Children)-1]
	tailNode := node.Children[len(node.Children)-1]
	tail, err := eval(tailNode, scope, false)
	if err != nil {
		return value.Nil, err
	}
	if tail.Kind != value.VList {
		return value.Nil, fmt.Errorf("cons tail must be a list, got %s", tail)
	}
	elems := make([]value.Value, 0, len(heads)+len(tail.Elems()))
	for _, h := range heads {
		v, err := eval(h, scope, false)
		if err != nil {
			return value.Nil, err
		}
		elems = append(elems, v)
	}
	elems = append(elems, tail.Elems()...)
	return value.ListVal(elems...), nil
}

func evalMap(node *ast.Node, scope Scope) (value.Value, error) {
	m := &value.OrdMap{Vals: make(map[string]value.Value)}
	for i := 0; i+1 < len(node.Children); i += 2 {
		k := node.Children[i].Tok.Val
		val, err := eval(node.Children[i+1], scope, false)
		if err != nil {
			return value.Nil, err
		}
		m.Keys = append(m.Keys, k)
		m.Vals[k] = val
	}
	return value.MapVal(m), nil
}

func evalCase(node *ast.Node, scope Scope) (value.Value, error) {
	subject, err := eval(node.Children[0], scope, false)
	if err != nil {
		return value.Nil, err
	}
	word := subject.ToStr()
	for _, clause := range node.Clauses {
		pat := clause.Pattern.Tok.Val
		if pat == "*" || pat == word {
			return eval(clause.Body, scope, false)
		}
		// Handle alternation: a|b
		for _, alt := range strings.Split(pat, "|") {
			if alt == word || alt == "*" {
				return eval(clause.Body, scope, false)
			}
		}
	}
	return value.Nil, nil
}

func evalSpawn(node *ast.Node, scope Scope) (value.Value, error) {
	if len(node.Children) < 2 {
		return value.Nil, fmt.Errorf("spawn requires a function argument")
	}
	fnVal, err := eval(node.Children[1], scope, false)
	if err != nil {
		return value.Nil, err
	}
	if fnVal.Kind != value.VFn {
		return value.Nil, fmt.Errorf("spawn requires a function, got %s", fnVal)
	}
	proc := newProcess()
	pid := value.PidVal(proc.id)
	childEnv := copyEnv(scope)
	childEnv.SetLocal("__self", pid)
	go func() {
		result, _ := callFn(fnVal.Fn(), nil, nil, nil, childEnv)
		proc.close(result)
	}()
	return pid, nil
}

func evalSend(node *ast.Node, scope Scope) (value.Value, error) {
	if len(node.Children) < 3 {
		return value.Nil, fmt.Errorf("send requires pid and message")
	}
	pidVal, err := eval(node.Children[1], scope, false)
	if err != nil {
		return value.Nil, err
	}
	msg, err := eval(node.Children[2], scope, false)
	if err != nil {
		return value.Nil, err
	}
	if pidVal.Kind != value.VPid {
		return value.Nil, fmt.Errorf("send requires a pid, got %s", pidVal)
	}
	proc := findProcess(pidVal.Pid())
	if proc == nil {
		return value.Nil, fmt.Errorf("process not found: %d", pidVal.Pid())
	}
	proc.send(msg)
	return msg, nil
}

func evalAwait(node *ast.Node, scope Scope) (value.Value, error) {
	if len(node.Children) < 2 {
		return value.Nil, fmt.Errorf("await requires a pid")
	}
	pidVal, err := eval(node.Children[1], scope, false)
	if err != nil {
		return value.Nil, err
	}
	if pidVal.Kind != value.VPid {
		return value.Nil, fmt.Errorf("await requires a pid, got %s", pidVal)
	}
	proc := findProcess(pidVal.Pid())
	if proc == nil {
		return value.Nil, nil
	}
	result := proc.await()
	if proc.err != nil {
		return value.Nil, proc.err
	}
	return result, nil
}

func evalMonitor(node *ast.Node, scope Scope) (value.Value, error) {
	if len(node.Children) < 2 {
		return value.Nil, fmt.Errorf("monitor requires a pid")
	}
	pidVal, err := eval(node.Children[1], scope, false)
	if err != nil {
		return value.Nil, err
	}
	if pidVal.Kind != value.VPid {
		return value.Nil, fmt.Errorf("monitor requires a pid, got %s", pidVal)
	}
	selfPid, ok := scope.Get("__self")
	if !ok || selfPid.Kind != value.VPid {
		return value.Nil, fmt.Errorf("monitor: not inside a process")
	}
	target := findProcess(pidVal.Pid())
	if target == nil {
		// Already dead — send DOWN immediately
		self := findProcess(selfPid.Pid())
		if self != nil {
			self.send(value.TupleVal(value.AtomVal("DOWN"), pidVal, value.AtomVal("noproc")))
		}
		return value.OkVal(value.Nil), nil
	}
	target.monitor(selfPid.Pid())
	return value.OkVal(value.Nil), nil
}

func evalSpawnLink(node *ast.Node, scope Scope) (value.Value, error) {
	if len(node.Children) < 2 {
		return value.Nil, fmt.Errorf("spawn_link requires a function argument")
	}
	fnVal, err := eval(node.Children[1], scope, false)
	if err != nil {
		return value.Nil, err
	}
	if fnVal.Kind != value.VFn {
		return value.Nil, fmt.Errorf("spawn_link requires a function, got %s", fnVal)
	}
	proc := newProcess()
	pid := value.PidVal(proc.id)
	parentPid, _ := scope.Get("__self")
	childEnv := copyEnv(scope)
	childEnv.SetLocal("__self", pid)
	go func() {
		result, err := callFn(fnVal.Fn(), nil, nil, nil, childEnv)
		if err != nil {
			proc.err = err
			proc.close(value.ErrorVal(1))
			if parentPid.Kind == value.VPid {
				if parent := findProcess(parentPid.Pid()); parent != nil {
					parent.send(value.TupleVal(value.AtomVal("EXIT"), pid, value.StringVal(err.Error())))
				}
			}
			return
		}
		proc.close(result)
	}()
	return pid, nil
}

func evalReceive(node *ast.Node, scope Scope) (value.Value, error) {
	selfPid, ok := scope.Get("__self")
	if !ok || selfPid.Kind != value.VPid {
		return value.Nil, fmt.Errorf("receive: not inside a process")
	}
	proc := findProcess(selfPid.Pid())
	if proc == nil {
		return value.Nil, fmt.Errorf("receive: process not found")
	}

	matcher := func(msg value.Value) bool {
		for _, clause := range node.Clauses {
			child := newChildEnv(scope)
			if matchPattern(clause.Pattern, msg, child) {
				return true
			}
		}
		return false
	}

	var msg value.Value
	var matched bool

	// Check for timeout: Children[0] = timeout expr, Children[1] = after body
	if len(node.Children) >= 2 {
		timeoutVal, err := eval(node.Children[0], scope, false)
		if err != nil {
			return value.Nil, err
		}
		ms := timeoutVal.Int()
		msg, matched = proc.receiveTimeout(matcher, time.Duration(ms)*time.Millisecond)
		if !matched {
			// Timeout: execute after body
			return eval(node.Children[1], scope, false)
		}
	} else {
		msg, _ = proc.receive(matcher)
	}

	for _, clause := range node.Clauses {
		child := newChildEnv(scope)
		if matchPattern(clause.Pattern, msg, child) {
			return eval(clause.Body, child, false)
		}
	}
	return value.Nil, nil
}

func evalDefModule(node *ast.Node, scope Scope) (value.Value, error) {
	name := node.Children[0].Tok.Val
	modEnv := newChildEnv(scope)
	body := node.Children[1]
	_, err := eval(body, modEnv, false)
	if err != nil {
		return value.Nil, err
	}
	// If module already exists, extend it (module reopening)
	m := &value.OrdMap{Vals: make(map[string]value.Value)}
	if existing, ok := scope.Get(name); ok && existing.Kind == value.VMap {
		src := existing.Map()
		for _, k := range src.Keys {
			m.Keys = append(m.Keys, k)
			m.Vals[k] = src.Vals[k]
		}
	}
	for k, v := range modEnv.Bindings {
		if _, exists := m.Vals[k]; !exists {
			m.Keys = append(m.Keys, k)
		}
		m.Vals[k] = v
	}
	// Attach module scope to each function so siblings are accessible by bare name.
	modScopeEnv := &Env{
		Bindings: make(map[string]value.Value, len(m.Vals)),
		Parent:   scope,
		Ctx:      scope.GetCtx(),
	}
	for k, v := range m.Vals {
		modScopeEnv.Bindings[k] = v
	}
	for _, v := range m.Vals {
		if v.Kind == value.VFn && v.Fn() != nil && v.Fn().Native == nil {
			v.Fn().Env = modScopeEnv
		}
	}
	scope.Set(name, value.MapVal(m))
	return value.Nil, nil
}

func evalUseImport(node *ast.Node, scope Scope) (value.Value, error) {
	modName := node.Children[0].Tok.Val
	mod, ok := scope.Get(modName)
	if !ok || mod.Kind != value.VMap {
		return value.Nil, fmt.Errorf("%s: module not found", modName)
	}
	m := mod.Map()
	for _, k := range m.Keys {
		scope.Set(k, m.Vals[k])
	}
	return value.Nil, nil
}

func evalTry(node *ast.Node, scope Scope) (value.Value, error) {
	body := node.Children[0]
	result, err := eval(body, scope, false)
	if err == nil {
		return result, nil
	}
	for _, clause := range node.Clauses {
		child := newChildEnv(scope)
		errVal := value.StringVal(err.Error())
		if matchPattern(clause.Pattern, errVal, child) {
			return eval(clause.Body, child, false)
		}
	}
	return value.Nil, err
}

type builtinFn func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error)

var builtinTable map[string]builtinFn
var builtinOnce sync.Once

func getBuiltin(name string) (builtinFn, bool) {
	builtinOnce.Do(func() { builtinTable = initBuiltins() })
	b, ok := builtinTable[name]
	return b, ok
}

var testBuiltin builtinFn = func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
	// Strip trailing ] if present
	if len(args) > 0 && args[len(args)-1] == "]" {
		args = args[:len(args)-1]
	}
	if len(args) == 0 {
		return value.ErrorVal(1), nil
	}
	result := evalTestExpr(args)
	if result {
		return value.OkVal(value.Nil), nil
	}
	return value.ErrorVal(1), nil
}

func evalTestExpr(args []string) bool {
	if len(args) == 0 {
		return false
	}
	// Negation
	if args[0] == "!" {
		return !evalTestExpr(args[1:])
	}
	// Unary operators
	if len(args) == 1 {
		return args[0] != "" // -n equivalent
	}
	if len(args) == 2 {
		switch args[0] {
		case "-z":
			return args[1] == ""
		case "-n":
			return args[1] != ""
		case "-e", "-f":
			_, err := os.Stat(args[1])
			return err == nil
		case "-d":
			fi, err := os.Stat(args[1])
			return err == nil && fi.IsDir()
		}
	}
	// Binary operators
	if len(args) == 3 {
		a, op, b := args[0], args[1], args[2]
		switch op {
		case "=", "==":
			return a == b
		case "!=":
			return a != b
		case "-eq":
			return atoiDef(a, 0) == atoiDef(b, 0)
		case "-ne":
			return atoiDef(a, 0) != atoiDef(b, 0)
		case "-lt":
			return atoiDef(a, 0) < atoiDef(b, 0)
		case "-le":
			return atoiDef(a, 0) <= atoiDef(b, 0)
		case "-gt":
			return atoiDef(a, 0) > atoiDef(b, 0)
		case "-ge":
			return atoiDef(a, 0) >= atoiDef(b, 0)
		}
	}
	return false
}

func atoiDef(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func initBuiltins() map[string]builtinFn {
	return map[string]builtinFn{
	"echo": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		out := scope.GetCtx().Stdout
		var stderr io.Writer = scope.GetCtx().StderrOrDefault()
		for _, r := range redirs {
			if r.FdDup {
				target, _ := eval(r.Target, scope, false)
				switch target.ToStr() {
				case "2":
					if r.Fd == 1 {
						out = stderr
					}
				case "1":
					if r.Fd == 2 {
						stderr = out
					}
				}
				continue
			}
			if r.Op == ast.TGt || r.Op == ast.TAppend {
				target, _ := eval(r.Target, scope, false)
				var f *os.File
				var err error
				if r.Op == ast.TGt {
					f, err = os.Create(target.ToStr())
				} else {
					f, err = os.OpenFile(target.ToStr(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				}
				if err != nil {
					return value.ErrorVal(1), nil
				}
				defer f.Close()
				if r.Fd == 2 {
					stderr = f
				} else {
					out = f
				}
			}
		}
		fmt.Fprintln(out, strings.Join(args, " "))
		return value.OkVal(value.Nil), nil
	},
	"true": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		return value.True, nil
	},
	"false": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		return value.False, nil
	},
	"export": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		for _, arg := range args {
			if idx := strings.IndexByte(arg, '='); idx >= 0 {
				scope.Set(arg[:idx], value.StringVal(arg[idx+1:]))
				os.Setenv(arg[:idx], arg[idx+1:])
			}
		}
		return value.OkVal(value.Nil), nil
	},
	"set": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		for i, arg := range args {
			if arg == "--" {
				scope.GetCtx().Args = args[i+1:]
				return value.OkVal(value.Nil), nil
			}
			if arg == "-o" && i+1 < len(args) {
				scope.GetCtx().Shell.SetFlag(args[i+1][0], true) // store first char as flag
				// Special handling for named options
				switch args[i+1] {
				case "pipefail":
					scope.GetCtx().Shell.SetFlag('P', true) // P for pipefail
				}
				return value.OkVal(value.Nil), nil
			}
			if len(arg) == 2 && arg[0] == '-' {
				scope.GetCtx().Shell.SetFlag(arg[1], true)
			} else if len(arg) == 2 && arg[0] == '+' {
				scope.GetCtx().Shell.SetFlag(arg[1], false)
			}
		}
		return value.OkVal(value.Nil), nil
	},
	"local": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		for _, arg := range args {
			if idx := strings.IndexByte(arg, '='); idx >= 0 {
				scope.SetLocal(arg[:idx], value.StringVal(arg[idx+1:]))
			}
		}
		return value.OkVal(value.Nil), nil
	},
	"unset": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		for _, name := range args {
			scope.NearestEnv().Delete(name)
		}
		return value.OkVal(value.Nil), nil
	},
	"exit": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		code := 0
		if len(args) > 0 {
			n, err := strconv.Atoi(args[0])
			if err == nil {
				code = n
			}
		}
		return value.Nil, &errExit{code: code}
	},
	"cd": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		dir := os.Getenv("HOME")
		if len(args) > 0 {
			dir = args[0]
		}
		if err := os.Chdir(dir); err != nil {
			fmt.Fprintf(os.Stderr, "cd: %s\n", err)
			return value.ErrorVal(1), nil
		}
		wd, _ := os.Getwd()
		os.Setenv("PWD", wd)
		return value.OkVal(value.Nil), nil
	},
	"shift": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		n := 1
		if len(args) > 0 {
			if v, err := strconv.Atoi(args[0]); err == nil {
				n = v
			}
		}
		if n <= len(scope.GetCtx().Args) {
			scope.GetCtx().Args = scope.GetCtx().Args[n:]
		}
		return value.OkVal(value.Nil), nil
	},
	"command": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		if len(args) >= 2 && args[0] == "-v" {
			name := args[1]
			if _, ok := getBuiltin(name); ok {
				fmt.Fprintln(scope.GetCtx().Stdout, name)
				return value.OkVal(value.Nil), nil
			}
			path, err := exec.LookPath(name)
			if err == nil {
				fmt.Fprintln(scope.GetCtx().Stdout, path)
				return value.OkVal(value.Nil), nil
			}
			return value.ErrorVal(1), nil
		}
		return value.OkVal(value.Nil), nil
	},
	"type": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		for _, name := range args {
			if _, ok := getBuiltin(name); ok {
				fmt.Fprintf(scope.GetCtx().Stdout, "%s is a shell builtin\n", name)
			} else if path, err := exec.LookPath(name); err == nil {
				fmt.Fprintf(scope.GetCtx().Stdout, "%s is %s\n", name, path)
			} else if fn, ok := scope.GetFn(name); ok && fn != nil {
				fmt.Fprintf(scope.GetCtx().Stdout, "%s is a function\n", name)
			} else {
				fmt.Fprintf(os.Stderr, "type: %s: not found\n", name)
				return value.ErrorVal(1), nil
			}
		}
		return value.OkVal(value.Nil), nil
	},
	"eval": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		input := strings.Join(args, " ")
		result := Run(input, scope)
		return result, nil
	},
	"read": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		var input io.Reader = scope.GetCtx().StdinOrDefault()
		for _, r := range redirs {
			if r.Op == ast.THeredoc {
				target, _ := eval(r.Target, scope, false)
				input = strings.NewReader(target.ToStr())
			}
		}
		var line string
		fmt.Fscanln(input, &line)
		if len(args) > 0 {
			scope.SetLocal(args[0], value.StringVal(line))
		}
		return value.OkVal(value.Nil), nil
	},
	"[": testBuiltin,
	"test": testBuiltin,
	"source": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		if len(args) < 1 {
			return value.ErrorVal(1), nil
		}
		data, err := os.ReadFile(args[0])
		if err != nil {
			return value.ErrorVal(1), nil
		}
		return Run(string(data), scope), nil
	},
	".": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		if len(args) < 1 {
			return value.ErrorVal(1), nil
		}
		data, err := os.ReadFile(args[0])
		if err != nil {
			return value.ErrorVal(1), nil
		}
		return Run(string(data), scope), nil
	},
	"readonly": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		for _, name := range args {
			if scope.GetCtx().Shell.ReadonlySet == nil {
				scope.GetCtx().Shell.ReadonlySet = make(map[string]bool)
			}
			scope.GetCtx().Shell.ReadonlySet[name] = true
		}
		return value.OkVal(value.Nil), nil
	},
	"alias": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		for _, arg := range args {
			if idx := strings.IndexByte(arg, '='); idx >= 0 {
				name := arg[:idx]
				val := arg[idx+1:]
				if scope.GetCtx().Shell.Aliases == nil {
					scope.GetCtx().Shell.Aliases = make(map[string]string)
				}
				scope.GetCtx().Shell.Aliases[name] = val
			}
		}
		return value.OkVal(value.Nil), nil
	},
	"unalias": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		for _, name := range args {
			if scope.GetCtx().Shell.Aliases != nil {
				delete(scope.GetCtx().Shell.Aliases, name)
			}
		}
		return value.OkVal(value.Nil), nil
	},
	"trap": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		if len(args) >= 2 {
			if scope.GetCtx().Shell.Traps == nil {
				scope.GetCtx().Shell.Traps = make(map[string]string)
			}
			scope.GetCtx().Shell.Traps[args[1]] = args[0]
		}
		return value.OkVal(value.Nil), nil
	},
	"exec": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		if len(args) < 1 {
			return value.OkVal(value.Nil), nil
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = scope.GetCtx().StdinOrDefault()
		cmd.Stdout = scope.GetCtx().Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = scope.NearestEnv().BuildEnv()
		err := cmd.Run()
		code := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				code = exitErr.ExitCode()
			} else {
				code = 127
			}
		}
		return value.Nil, &errExit{code: code}
	},
	"wait": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		if len(args) > 0 {
			j := scope.GetCtx().Jobs.Resolve(args[0])
			if j != nil {
				<-j.Done
				return value.OkVal(value.Nil), nil
			}
		}
		scope.GetCtx().BgJobs.Wait()
		return value.OkVal(value.Nil), nil
	},
	"jobs": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		all := scope.GetCtx().Jobs.All()
		for i, j := range all {
			marker := " "
			if i == len(all)-1 {
				marker = "+"
			} else if i == len(all)-2 {
				marker = "-"
			}
			fmt.Fprintf(scope.GetCtx().Stdout, "[%d]%s\t%s\t%s\n", j.ID, marker, j.Status(), j.Command)
		}
		return value.OkVal(value.Nil), nil
	},
	"fg": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		var j *Job
		if len(args) > 0 {
			j = scope.GetCtx().Jobs.Resolve(args[0])
		} else {
			j = scope.GetCtx().Jobs.Last()
		}
		if j == nil {
			return value.ErrorVal(1), nil
		}
		if j.Process != nil {
			ttyFd := int(os.Stdin.Fd())
			GiveTerm(ttyFd, j.Pgid)
			syscall.Kill(-j.Pgid, syscall.SIGCONT)
			j.SetStatus("Running")
			ws, err := WaitFg(j.Pid)
			ReclaimTerm(ttyFd)
			if err != nil {
				scope.GetCtx().Jobs.Remove(j.ID)
				return value.ErrorVal(1), nil
			}
			if ws.Stopped() {
				j.SetStatus("Stopped")
				return value.IntVal(148), nil
			}
			j.SetDone(ws.ExitStatus())
			scope.GetCtx().Jobs.Remove(j.ID)
			return value.OkVal(value.Nil), nil
		}
		// OTP process — just wait
		<-j.Done
		scope.GetCtx().Jobs.Remove(j.ID)
		return value.OkVal(value.Nil), nil
	},
	"bg": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		var j *Job
		if len(args) > 0 {
			j = scope.GetCtx().Jobs.Resolve(args[0])
		} else {
			j = scope.GetCtx().Jobs.Last()
		}
		if j == nil {
			return value.ErrorVal(1), nil
		}
		if j.Process != nil {
			syscall.Kill(j.Pid, syscall.SIGCONT)
		}
		j.SetStatus("Running")
		return value.OkVal(value.Nil), nil
	},
	"getopts": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		if len(args) < 2 {
			return value.ErrorVal(1), nil
		}
		optstring := args[0]
		varname := args[1]
		optind := 1
		if v, ok := scope.Get("OPTIND"); ok {
			if n, err := strconv.Atoi(v.ToStr()); err == nil {
				optind = n
			}
		}
		posArgs := scope.GetCtx().Args
		if optind > len(posArgs) {
			return value.ErrorVal(1), nil
		}
		current := posArgs[optind-1]
		if len(current) < 2 || current[0] != '-' {
			return value.ErrorVal(1), nil
		}
		ch := current[1]
		idx := strings.IndexByte(optstring, ch)
		if idx < 0 {
			scope.SetLocal(varname, value.StringVal("?"))
			scope.SetLocal("OPTIND", value.StringVal(strconv.Itoa(optind+1)))
			return value.OkVal(value.Nil), nil
		}
		scope.SetLocal(varname, value.StringVal(string(ch)))
		if idx+1 < len(optstring) && optstring[idx+1] == ':' {
			if len(current) > 2 {
				scope.SetLocal("OPTARG", value.StringVal(current[2:]))
			} else if optind < len(posArgs) {
				scope.SetLocal("OPTARG", value.StringVal(posArgs[optind]))
				optind++
			}
		}
		scope.SetLocal("OPTIND", value.StringVal(strconv.Itoa(optind+1)))
		return value.OkVal(value.Nil), nil
	},
	"umask": func(args []string, scope Scope, redirs []ast.Redirect) (value.Value, error) {
		if len(args) == 0 {
			old := syscall.Umask(0)
			syscall.Umask(old)
			fmt.Fprintf(scope.GetCtx().Stdout, "%04o\n", old)
			return value.OkVal(value.Nil), nil
		}
		mask, err := strconv.ParseUint(args[0], 8, 32)
		if err != nil {
			return value.ErrorVal(1), nil
		}
		syscall.Umask(int(mask))
		return value.OkVal(value.Nil), nil
	},
	}
}
