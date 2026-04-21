package eval

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"ish/internal/ast"
	"ish/internal/core"
)

// evalIdent evaluates a bare identifier. Looks up in env as variable, then
// as function, then falls back to string.
func evalIdent(node *ast.Node, env *core.Env) (core.Value, error) {
	name := node.Tok.Val

	// Keyword literals that might appear as identifiers
	switch node.Tok.Type {
	case ast.TNil:
		return core.Nil, nil
	case ast.TTrue:
		return core.True, nil
	case ast.TFalse:
		return core.False, nil
	}

	if name == "self" {
		if proc := env.GetProc(); proc != nil {
			return core.Value{Kind: core.VPid, Pid: proc}, nil
		}
		return core.Nil, nil
	}

	if v, ok := env.Get(name); ok {
		return v, nil
	}

	r := ResolveCmd(name, env)
	switch r.Kind {
	case KindUserFn:
		// Zero-arity functions auto-call when used as values.
		// Use &name to get the function value without calling.
		if isZeroArity(r.Fn) {
			return CallFn(r.Fn, nil, env)
		}
		return core.Value{Kind: core.VFn, Fn: r.Fn}, nil
	case KindNativeFn:
		return core.Value{Kind: core.VFn, Fn: &core.FnValue{
			Name: name, Native: r.NativeFn,
		}}, nil
	case KindModuleFn:
		if isZeroArity(r.Fn) {
			return CallFn(r.Fn, nil, env)
		}
		return core.Value{Kind: core.VFn, Fn: r.Fn}, nil
	case KindModuleNativeFn:
		return core.Value{Kind: core.VFn, Fn: &core.FnValue{
			Name: r.ModName + "." + r.FnName, Native: r.NativeFn,
		}}, nil
	}

	if env.InExprMode() {
		fmt.Fprintf(os.Stderr, "ish: warning: undefined variable '%s' used as string\n", name)
	}
	return core.StringVal(name), nil
}

// evalVarRef evaluates a $var variable reference (NVarRef node).
// The variable name is in Tok.Val (without the $ prefix).
func evalVarRef(node *ast.Node, env *core.Env) (core.Value, error) {
	name := node.Tok.Val

	// Special variables: $?, $$, $!, $@, $*, $#, $0-$9
	if node.Tok.Type == ast.TSpecialVar {
		return evalSpecialVar(name, env), nil
	}

	if v, ok := env.Get(name); ok {
		return v, nil
	}

	if env.HasFlag('u') {
		return core.Nil, fmt.Errorf("ish: %s: unbound variable", name)
	}
	return core.StringVal(""), nil
}

func evalSpecialVar(name string, env *core.Env) core.Value {
	if len(name) < 2 || name[0] != '$' {
		return core.StringVal(name)
	}
	switch name[1] {
	case '?':
		return core.IntVal(int64(env.ExitCode()))
	case '$':
		return core.IntVal(int64(env.Pid()))
	case '!':
		return core.IntVal(int64(env.BgPid()))
	case '#':
		return core.IntVal(int64(len(env.PosArgs())))
	case '@':
		args := env.PosArgs()
		vals := make([]core.Value, len(args))
		for i, a := range args {
			vals[i] = core.StringVal(a)
		}
		return core.ListVal(vals...)
	case '*':
		sep := " "
		if ifs, ok := env.Get("IFS"); ok {
			ifsStr := ifs.ToStr()
			if len(ifsStr) > 0 {
				sep = ifsStr[:1]
			} else {
				sep = ""
			}
		}
		return core.StringVal(strings.Join(env.PosArgs(), sep))
	default:
		// $0-$9
		if name[1] >= '0' && name[1] <= '9' {
			idx := int(name[1] - '0')
			args := env.PosArgs()
			if idx == 0 {
				return core.StringVal("ish")
			}
			if idx <= len(args) {
				return core.StringVal(args[idx-1])
			}
			return core.StringVal("")
		}
	}
	return core.StringVal(name)
}

// evalCall evaluates a function call (NCall node).
// Children[0] is the callee (typically NAccess for Module.func).
// Children[1:] are the arguments.
func evalCall(node *ast.Node, env *core.Env) (core.Value, error) {
	callee, err := Eval(node.Children[0], env)
	if err != nil {
		return core.Nil, err
	}

	if callee.Kind != core.VFn || callee.Fn == nil {
		return core.Nil, fmt.Errorf("not a function: %s", callee.Inspect())
	}

	argVals, err := evalFnArgs(node, env)
	if err != nil {
		return core.Nil, err
	}

	if node.Tail {
		return core.TailCallVal(callee.Fn, argVals), nil
	}
	return CallFn(callee.Fn, argVals, env)
}

// evalCmdSubNode evaluates a $(cmd) command substitution node.
// Children[0] is the interior AST (NBlock or single statement).
func evalCmdSubNode(node *ast.Node, env *core.Env) (core.Value, error) {
	if len(node.Children) == 0 {
		return core.StringVal(""), nil
	}

	innerNode := node.Children[0]

	r, w, err := os.Pipe()
	if err != nil {
		return core.Nil, fmt.Errorf("command substitution: %w", err)
	}

	childEnv := core.NewEnv(env)
	childEnv.Stdout_ = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	val, evalErr := Eval(innerNode, childEnv)
	if val.Kind != core.VNil && val.Kind != core.VString {
		fmt.Fprint(w, val.String())
	} else if val.Kind == core.VString && val.Str != "" {
		fmt.Fprint(w, val.Str)
	}
	w.Close()
	<-done
	r.Close()

	result := strings.TrimRight(buf.String(), "\n")
	return core.StringVal(result), evalErr
}

// evalArithSubNode evaluates a $((expr)) arithmetic expansion node.
// Children[0] is the interior arithmetic expression AST.
func evalArithSubNode(node *ast.Node, env *core.Env) (core.Value, error) {
	if len(node.Children) == 0 {
		return core.IntVal(0), nil
	}
	val, err := Eval(node.Children[0], env)
	if err != nil {
		return core.Nil, err
	}
	return val, nil
}

// evalParamExpandNode evaluates a ${...} parameter expansion node.
// Children are the interior tokens as NLit nodes.
// For now, reconstruct the expression string and use env.Expand.
// This will be properly structured once ${var:-default} etc. are parsed.
func evalParamExpandNode(node *ast.Node, env *core.Env) (core.Value, error) {
	var expr strings.Builder
	for _, child := range node.Children {
		expr.WriteString(child.Tok.Val)
	}
	expanded := env.Expand("${" + expr.String() + "}")
	return core.StringVal(expanded), nil
}

// evalInterpolationNode evaluates a #{expr} interpolation node.
// Children[0] is the interior expression AST.
func evalInterpolationNode(node *ast.Node, env *core.Env) (core.Value, error) {
	if len(node.Children) == 0 {
		return core.StringVal(""), nil
	}
	val, err := Eval(node.Children[0], env)
	if err != nil {
		return core.Nil, err
	}
	return core.StringVal(val.ToStr()), nil
}

// evalInterpStringNode evaluates an interpolated string node.
// Children are segments: NLit (literal text), NVarRef, NCmdSub, etc.
func evalInterpStringNode(node *ast.Node, env *core.Env) (core.Value, error) {
	var buf strings.Builder
	for _, seg := range node.Children {
		val, err := Eval(seg, env)
		if err != nil {
			return core.Nil, err
		}
		buf.WriteString(val.ToStr())
	}
	return core.StringVal(buf.String()), nil
}

// evalArg evaluates a compound word (NArg). Concatenates all children into a string.
// NIdent children are literal strings (not variable lookups) because compound
// words are command arguments where bare words are literal.
func evalArg(node *ast.Node, env *core.Env) (core.Value, error) {
	var buf strings.Builder
	for _, child := range node.Children {
		if child.Kind == ast.NIdent {
			// Bare identifiers in compound words are literal strings
			buf.WriteString(child.Tok.Val)
			continue
		}
		// Numeric literals in compound words preserve raw text
		// (e.g. 1.120 in IP address 192.168.1.120)
		if child.Kind == ast.NLit && (child.Tok.Type == ast.TFloat || child.Tok.Type == ast.TInt) {
			buf.WriteString(child.Tok.Val)
			continue
		}
		v, err := Eval(child, env)
		if err != nil {
			return core.Nil, err
		}
		buf.WriteString(v.ToStr())
	}
	return core.StringVal(buf.String()), nil
}

// evalIshIf evaluates ish-style if/do/end with truthiness semantics.
func evalIshIf(node *ast.Node, env *core.Env) (core.Value, error) {
	for _, clause := range node.Clauses {
		if clause.Pattern == nil {
			// else clause
			return Eval(clause.Body, env)
		}

		condVal, err := Eval(clause.Pattern, env)
		if err != nil {
			return core.Nil, err
		}

		if condVal.Truthy() {
			return Eval(clause.Body, env)
		}
	}
	return core.Nil, nil
}
