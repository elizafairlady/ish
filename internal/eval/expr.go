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
	// With the new parser, string interpolation is handled by NInterpString.
	// NLit strings are plain literal segments — no expansion needed.
	return litToValue(node)
}

func litToValue(node *ast.Node) (core.Value, error) {
	switch node.Tok.Type {
	case ast.TInt:
		n, err := strconv.ParseInt(node.Tok.Val, 10, 64)
		if err != nil {
			return core.Nil, fmt.Errorf("integer literal out of range: %s", node.Tok.Val)
		}
		return core.IntVal(n), nil
	case ast.TFloat:
		f, err := strconv.ParseFloat(node.Tok.Val, 64)
		if err != nil {
			return core.Nil, fmt.Errorf("float literal out of range: %s", node.Tok.Val)
		}
		return core.FloatVal(f), nil
	case ast.TString:
		return core.StringVal(node.Tok.Val), nil
	case ast.TAtom:
		return core.AtomVal(node.Tok.Val), nil
	case ast.TNil:
		return core.Nil, nil
	case ast.TTrue:
		return core.True, nil
	case ast.TFalse:
		return core.False, nil
	default:
		return core.StringVal(node.Tok.Val), nil
	}
}

// isZeroArity returns true if all clauses of a function accept 0 parameters.
func isZeroArity(fn *core.FnValue) bool {
	if fn.Native != nil {
		return false
	}
	if len(fn.Clauses) == 0 {
		return false
	}
	for _, c := range fn.Clauses {
		if len(c.Params) > 0 {
			return false
		}
	}
	return true
}

// evalCapture handles &name — returns the function value without auto-calling.
func evalCapture(node *ast.Node, env *core.Env) (core.Value, error) {
	name := node.Tok.Val
	r := ResolveCmd(name, env)
	switch r.Kind {
	case KindModuleFn, KindUserFn, KindVarFn:
		return core.Value{Kind: core.VFn, Fn: r.Fn}, nil
	case KindModuleNativeFn, KindNativeFn:
		return core.Value{Kind: core.VFn, Fn: &core.FnValue{
			Name: name, Native: r.NativeFn,
		}}, nil
	}
	return core.Nil, fmt.Errorf("undefined function: %s", name)
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
		case ast.TLt:
			return core.BoolVal(left.Int < right.Int), nil
		case ast.TGt:
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
		case ast.TLt:
			return core.BoolVal(lf < rf), nil
		case ast.TGt:
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
		case ast.TLt:
			return core.BoolVal(left.Str < right.Str), nil
		case ast.TGt:
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
	elems := make([]core.Value, 0, len(node.Children))
	for _, child := range node.Children {
		v, err := Eval(child, env)
		if err != nil {
			return core.Nil, err
		}
		elems = append(elems, v)
	}
	if node.Rest != nil {
		tail, err := Eval(node.Rest, env)
		if err != nil {
			return core.Nil, err
		}
		if tail.Kind != core.VList {
			return core.Nil, fmt.Errorf("cons tail must be a list, got %s", tail.Inspect())
		}
		elems = append(elems, tail.Elems...)
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
	if node.Children[0].Kind == ast.NIdent {
		modName := node.Children[0].Tok.Val
		if mod, ok := env.GetModule(modName); ok {
			field := node.Tok.Val
			if fn, ok := mod.Fns[field]; ok {
				if isZeroArity(fn) {
					return CallFn(fn, nil, env)
				}
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



func evalCmdSub(cmdStr string, env *core.Env) (core.Value, error) {
	node, err := parser.Parse(lexer.New(cmdStr))
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
