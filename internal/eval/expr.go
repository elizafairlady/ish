package eval

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"ish/internal/ast"
	"ish/internal/core"
	"ish/internal/lexer"
	"ish/internal/parser"
)

func evalLit(node *ast.Node, env *core.Env) (core.Value, error) {
	if node.Tok.Type == ast.TString && !node.Tok.Quoted {
		return core.StringVal(env.Expand(node.Tok.Val)), nil
	}
	return litToValue(node)
}

func litToValue(node *ast.Node) (core.Value, error) {
	switch node.Tok.Type {
	case ast.TInt:
		n, _ := strconv.ParseInt(node.Tok.Val, 10, 64)
		return core.IntVal(n), nil
	case ast.TFloat:
		f, _ := strconv.ParseFloat(node.Tok.Val, 64)
		return core.FloatVal(f), nil
	case ast.TString:
		return core.StringVal(node.Tok.Val), nil
	case ast.TAtom:
		return core.AtomVal(node.Tok.Val), nil
	default:
		return core.StringVal(node.Tok.Val), nil
	}
}

func evalWord(node *ast.Node, env *core.Env) (core.Value, error) {
	name := node.Tok.Val
	if name == "nil" {
		return core.Nil, nil
	}
	if name == "true" {
		return core.True, nil
	}
	if name == "false" {
		return core.False, nil
	}
	if name == "self" {
		if proc := env.GetProc(); proc != nil {
			return core.Value{Kind: core.VPid, Pid: proc}, nil
		}
		return core.Nil, nil
	}

	if strings.HasPrefix(name, "~") {
		return core.StringVal(env.ExpandTilde(name)), nil
	}

	if strings.HasPrefix(name, "$((") && strings.HasSuffix(name, "))") {
		return evalArithExpansion(name, env)
	}

	if strings.HasPrefix(name, "$(") && strings.HasSuffix(name, ")") {
		inner := name[2 : len(name)-1]
		return evalCmdSub(inner, env)
	}

	if strings.Contains(name, "$") || strings.Contains(name, "#{") {
		if env.HasFlag('u') {
			if err := checkUnsetVars(name, env); err != nil {
				return core.Nil, err
			}
		}
		// Simple $var reference: preserve the value type instead of stringifying.
		// Only stringify when $ is part of a larger interpolation.
		if len(name) > 1 && name[0] == '$' && !strings.ContainsAny(name[1:], "$#{") {
			varName := name[1:]
			if v, ok := env.Get(varName); ok {
				return v, nil
			}
		}
		expanded := env.Expand(name)
		return core.StringVal(expanded), nil
	}

	if v, ok := env.Get(name); ok {
		return v, nil
	}

	if fn, ok := env.GetFn(name); ok {
		return core.Value{Kind: core.VFn, Fn: fn}, nil
	}

	if _, ok := env.GetNativeFn(name); ok {
		return core.StringVal(name), nil
	}

	if dotIdx := strings.IndexByte(name, '.'); dotIdx > 0 {
		modName := name[:dotIdx]
		fnName := name[dotIdx+1:]
		if mod, ok := env.GetModule(modName); ok {
			if fn, ok := mod.Fns[fnName]; ok {
				return core.Value{Kind: core.VFn, Fn: fn}, nil
			}
			if nfn, ok := mod.NativeFns[fnName]; ok {
				return core.Value{Kind: core.VFn, Fn: &core.FnValue{
					Name: modName + "." + fnName, Native: nfn,
				}}, nil
			}
		}
		// Map field access fallback
		if obj, ok := env.Get(modName); ok && obj.Kind == core.VMap && obj.Map != nil {
			if v, ok := obj.Map.Get(fnName); ok {
				return v, nil
			}
		}
	}

	if env.InExprMode() {
		fmt.Fprintf(os.Stderr, "ish: warning: undefined variable '%s' used as string\n", name)
	}
	return core.StringVal(name), nil
}

func evalBinOp(node *ast.Node, env *core.Env) (core.Value, error) {
	defer env.EnterExprMode()()
	left, err := Eval(node.Children[0], env)
	if err != nil {
		return core.Nil, err
	}
	right, err := Eval(node.Children[1], env)
	if err != nil {
		return core.Nil, err
	}

	if left.Kind == core.VInt && right.Kind == core.VInt {
		switch node.Tok.Type {
		case ast.TPlus:
			result := left.Int + right.Int
			if (right.Int > 0 && result < left.Int) || (right.Int < 0 && result > left.Int) {
				return core.Nil, fmt.Errorf("integer overflow: %d + %d", left.Int, right.Int)
			}
			return core.IntVal(result), nil
		case ast.TMinus:
			result := left.Int - right.Int
			if (right.Int > 0 && result > left.Int) || (right.Int < 0 && result < left.Int) {
				return core.Nil, fmt.Errorf("integer overflow: %d - %d", left.Int, right.Int)
			}
			return core.IntVal(result), nil
		case ast.TMul:
			result := left.Int * right.Int
			if left.Int != 0 && right.Int != 0 && result/left.Int != right.Int {
				return core.Nil, fmt.Errorf("integer overflow: %d * %d", left.Int, right.Int)
			}
			return core.IntVal(result), nil
		case ast.TDiv:
			if right.Int == 0 {
				return core.Nil, fmt.Errorf("division by zero")
			}
			if left.Int == math.MinInt64 && right.Int == -1 {
				return core.Nil, fmt.Errorf("integer overflow: %d / %d", left.Int, right.Int)
			}
			return core.IntVal(left.Int / right.Int), nil
		case ast.TPercent:
			if right.Int == 0 {
				return core.Nil, fmt.Errorf("modulo by zero")
			}
			return core.IntVal(left.Int % right.Int), nil
		case ast.TEq:
			return core.BoolVal(left.Int == right.Int), nil
		case ast.TNe:
			return core.BoolVal(left.Int != right.Int), nil
		case ast.TRedirIn:
			return core.BoolVal(left.Int < right.Int), nil
		case ast.TRedirOut:
			return core.BoolVal(left.Int > right.Int), nil
		case ast.TLe:
			return core.BoolVal(left.Int <= right.Int), nil
		case ast.TGe:
			return core.BoolVal(left.Int >= right.Int), nil
		}
	}

	// Float arithmetic: if either side is float, promote to float
	if (left.Kind == core.VFloat || left.Kind == core.VInt) && (right.Kind == core.VFloat || right.Kind == core.VInt) && (left.Kind == core.VFloat || right.Kind == core.VFloat) {
		lf := left.Float
		if left.Kind == core.VInt {
			lf = float64(left.Int)
		}
		rf := right.Float
		if right.Kind == core.VInt {
			rf = float64(right.Int)
		}
		switch node.Tok.Type {
		case ast.TPlus:
			return core.FloatVal(lf + rf), nil
		case ast.TMinus:
			return core.FloatVal(lf - rf), nil
		case ast.TMul:
			return core.FloatVal(lf * rf), nil
		case ast.TDiv:
			if rf == 0 {
				return core.Nil, fmt.Errorf("division by zero")
			}
			return core.FloatVal(lf / rf), nil
		case ast.TPercent:
			if rf == 0 {
				return core.Nil, fmt.Errorf("modulo by zero")
			}
			return core.FloatVal(math.Mod(lf, rf)), nil
		case ast.TEq:
			return core.BoolVal(lf == rf), nil
		case ast.TNe:
			return core.BoolVal(lf != rf), nil
		case ast.TRedirIn:
			return core.BoolVal(lf < rf), nil
		case ast.TRedirOut:
			return core.BoolVal(lf > rf), nil
		case ast.TLe:
			return core.BoolVal(lf <= rf), nil
		case ast.TGe:
			return core.BoolVal(lf >= rf), nil
		}
	}

	// String comparison
	if left.Kind == core.VString && right.Kind == core.VString {
		switch node.Tok.Type {
		case ast.TRedirIn:
			return core.BoolVal(left.Str < right.Str), nil
		case ast.TRedirOut:
			return core.BoolVal(left.Str > right.Str), nil
		case ast.TLe:
			return core.BoolVal(left.Str <= right.Str), nil
		case ast.TGe:
			return core.BoolVal(left.Str >= right.Str), nil
		}
	}

	if node.Tok.Type == ast.TPlus && (left.Kind == core.VString || right.Kind == core.VString) {
		return core.StringVal(left.ToStr() + right.ToStr()), nil
	}

	switch node.Tok.Type {
	case ast.TEq:
		return core.BoolVal(left.Equal(right)), nil
	case ast.TNe:
		return core.BoolVal(!left.Equal(right)), nil
	}

	return core.Nil, fmt.Errorf("unsupported operation: %s %s %s", left.Inspect(), node.Tok.Val, right.Inspect())
}

func evalUnary(node *ast.Node, env *core.Env) (core.Value, error) {
	operand, err := Eval(node.Children[0], env)
	if err != nil {
		return core.Nil, err
	}
	switch node.Tok.Type {
	case ast.TBang:
		return core.BoolVal(!operand.Truthy()), nil
	case ast.TMinus:
		if operand.Kind == core.VInt {
			return core.IntVal(-operand.Int), nil
		}
		if operand.Kind == core.VFloat {
			return core.FloatVal(-operand.Float), nil
		}
		return core.Nil, fmt.Errorf("cannot negate %s", operand.Inspect())
	}
	return core.Nil, fmt.Errorf("unknown unary op: %s", node.Tok.Val)
}

func evalTuple(node *ast.Node, env *core.Env) (core.Value, error) {
	elems := make([]core.Value, len(node.Children))
	for i, child := range node.Children {
		v, err := Eval(child, env)
		if err != nil {
			return core.Nil, err
		}
		elems[i] = v
	}
	return core.TupleVal(elems...), nil
}

func evalList(node *ast.Node, env *core.Env) (core.Value, error) {
	elems := make([]core.Value, len(node.Children))
	for i, child := range node.Children {
		v, err := Eval(child, env)
		if err != nil {
			return core.Nil, err
		}
		elems[i] = v
	}
	return core.ListVal(elems...), nil
}

func evalMap(node *ast.Node, env *core.Env) (core.Value, error) {
	m := core.NewOrdMap()
	for i := 0; i+1 < len(node.Children); i += 2 {
		key := node.Children[i].Tok.Val
		val, err := Eval(node.Children[i+1], env)
		if err != nil {
			return core.Nil, err
		}
		m.Set(key, val)
	}
	return core.Value{Kind: core.VMap, Map: m}, nil
}

func evalAccess(node *ast.Node, env *core.Env) (core.Value, error) {
	// Module-qualified reference: Module.func
	if node.Children[0].Kind == ast.NWord {
		modName := node.Children[0].Tok.Val
		if mod, ok := env.GetModule(modName); ok {
			field := node.Tok.Val
			if fn, ok := mod.Fns[field]; ok {
				return core.Value{Kind: core.VFn, Fn: fn}, nil
			}
			if nfn, ok := mod.NativeFns[field]; ok {
				return core.Value{Kind: core.VFn, Fn: &core.FnValue{
					Name: modName + "." + field, Native: nfn,
				}}, nil
			}
			return core.Nil, fmt.Errorf("%s.%s: undefined function", modName, field)
		}
	}
	obj, err := Eval(node.Children[0], env)
	if err != nil {
		return core.Nil, err
	}
	field := node.Tok.Val
	if obj.Kind == core.VMap && obj.Map != nil {
		if v, ok := obj.Map.Get(field); ok {
			return v, nil
		}
	}
	return core.Nil, fmt.Errorf("no field %s on %s", field, obj.Inspect())
}

func evalArithExpansion(name string, env *core.Env) (core.Value, error) {
	inner := name[3 : len(name)-2]
	inner = env.Expand(inner)
	tokens := lexer.Lex(inner)
	for i := range tokens {
		if tokens[i].Type == ast.TWord {
			if v, ok := env.Get(tokens[i].Val); ok {
				tokens[i] = ast.Token{Type: ast.TInt, Val: v.ToStr(), Pos: tokens[i].Pos}
			}
		}
	}
	node, err := parser.Parse(lexer.NewFromTokens(tokens))
	if err != nil {
		return core.Nil, err
	}
	val, err := Eval(node, env)
	if err != nil {
		return core.Nil, err
	}
	return core.StringVal(val.ToStr()), nil
}

func evalCmdSub(cmdStr string, env *core.Env) (core.Value, error) {
	node, err := parser.ParseWithCommands(lexer.New(cmdStr), makeIsCommand(env))
	if err != nil {
		return core.Nil, err
	}

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

	val, evalErr := Eval(node, childEnv)
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

func evalMatch(node *ast.Node, env *core.Env) (core.Value, error) {
	if len(node.Children) != 2 {
		return core.Nil, fmt.Errorf("invalid match node")
	}
	lhs := node.Children[0]
	rhs := node.Children[1]

	defer env.EnterExprMode()()
	val, err := Eval(rhs, env)
	if err != nil {
		return core.Nil, err
	}

	if err := PatternBind(lhs, val, env); err != nil {
		return core.Nil, err
	}
	return val, nil
}
