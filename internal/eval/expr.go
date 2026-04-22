package eval

import (
	"fmt"
	"strconv"
	"strings"

	"ish/internal/ast"
	"ish/internal/core"
)

func evalLit(node *ast.Node, scope core.Scope) (core.Value, error) {
	tok := node.Tok
	switch tok.Type {
	case ast.TInt:
		n, err := strconv.ParseInt(tok.Val, 10, 64)
		if err != nil { return core.Nil, err }
		return core.IntVal(n), nil
	case ast.TFloat:
		f, err := strconv.ParseFloat(tok.Val, 64)
		if err != nil { return core.Nil, err }
		return core.FloatVal(f), nil
	case ast.TString:
		return core.StringVal(tok.Val), nil
	case ast.TAtom:
		return core.AtomVal(tok.Val), nil
	case ast.TNil:
		return core.Nil, nil
	case ast.TTrue:
		return core.True, nil
	case ast.TFalse:
		return core.False, nil
	}
	return core.StringVal(tok.Val), nil
}

// litToValue returns the Value for a literal node (used by TryBind for pattern matching).
func litToValue(node *ast.Node) (core.Value, error) {
	return evalLit(node, nil)
}

func isZeroArity(fn *core.FnValue) bool {
	if fn.Native != nil { return false }
	if len(fn.Clauses) == 0 { return false }
	for _, c := range fn.Clauses {
		if len(c.Params) > 0 { return false }
	}
	return true
}

func evalIdent(node *ast.Node, scope core.Scope) (core.Value, error) {
	name := node.Tok.Val

	if v, ok := scope.Get(name); ok {
		// Zero-arity auto-call for functions
		if v.Kind == core.VFn && v.GetFn() != nil {
			fn := v.GetFn()
			if len(fn.Clauses) > 0 && len(fn.Clauses[0].Params) == 0 {
				return CallFn(fn, nil, scope)
			}
		}
		return v, nil
	}
	// Try function lookup
	if fn, ok := scope.GetFn(name); ok {
		if len(fn.Clauses) > 0 && len(fn.Clauses[0].Params) == 0 {
			return CallFn(fn, nil, scope)
		}
		return core.FnVal(fn), nil
	}
	// Try native function
	if nfn, ok := scope.GetNativeFn(name); ok {
		return core.FnVal(&core.FnValue{Name: name, Native: nfn}), nil
	}
	// self keyword
	if name == "self" {
		if proc := scope.NearestEnv().GetProc(); proc != nil {
			return core.PidVal(proc), nil
		}
		return core.Nil, nil
	}
	// Fallback: unknown identifier becomes its own name as string
	return core.StringVal(name), nil
}

func evalBinOp(node *ast.Node, scope core.Scope) (core.Value, error) {
	left, err := Eval(node.Children[0], scope)
	if err != nil { return core.Nil, err }
	right, err := Eval(node.Children[1], scope)
	if err != nil { return core.Nil, err }

	op := node.Tok.Type

	// Coerce strings to numbers for arithmetic operators
	if isArithOp(op) || left.Kind == core.VString || right.Kind == core.VString {
		left = coerceNumeric(left)
		right = coerceNumeric(right)
	}

	// Int op Int
	if left.Kind == core.VInt && right.Kind == core.VInt {
		l, r := left.GetInt(), right.GetInt()
		switch op {
		case ast.TPlus:  return core.IntVal(l + r), nil
		case ast.TMinus: return core.IntVal(l - r), nil
		case ast.TMul:   return core.IntVal(l * r), nil
		case ast.TDiv:
			if r == 0 { return core.Nil, fmt.Errorf("division by zero") }
			return core.IntVal(l / r), nil
		case ast.TPercent:
			if r == 0 { return core.Nil, fmt.Errorf("modulo by zero") }
			return core.IntVal(l % r), nil
		case ast.TEq:  return core.BoolVal(l == r), nil
		case ast.TNe:  return core.BoolVal(l != r), nil
		case ast.TLt:  return core.BoolVal(l < r), nil
		case ast.TGt:  return core.BoolVal(l > r), nil
		case ast.TLe:  return core.BoolVal(l <= r), nil
		case ast.TGe:  return core.BoolVal(l >= r), nil
		}
	}

	// Float or mixed
	if (left.Kind == core.VInt || left.Kind == core.VFloat) && (right.Kind == core.VInt || right.Kind == core.VFloat) {
		lf := float64(left.GetInt())
		if left.Kind == core.VFloat { lf = left.GetFloat() }
		rf := float64(right.GetInt())
		if right.Kind == core.VFloat { rf = right.GetFloat() }
		switch op {
		case ast.TPlus:  return core.FloatVal(lf + rf), nil
		case ast.TMinus: return core.FloatVal(lf - rf), nil
		case ast.TMul:   return core.FloatVal(lf * rf), nil
		case ast.TDiv:
			if rf == 0 { return core.Nil, fmt.Errorf("division by zero") }
			return core.FloatVal(lf / rf), nil
		case ast.TEq:  return core.BoolVal(lf == rf), nil
		case ast.TNe:  return core.BoolVal(lf != rf), nil
		case ast.TLt:  return core.BoolVal(lf < rf), nil
		case ast.TGt:  return core.BoolVal(lf > rf), nil
		case ast.TLe:  return core.BoolVal(lf <= rf), nil
		case ast.TGe:  return core.BoolVal(lf >= rf), nil
		}
	}

	// String concatenation
	if op == ast.TPlus && (left.Kind == core.VString || right.Kind == core.VString) {
		return core.StringVal(left.ToStr() + right.ToStr()), nil
	}

	// Equality on any types
	switch op {
	case ast.TEq:
		return core.BoolVal(left.Equal(right)), nil
	case ast.TNe:
		return core.BoolVal(!left.Equal(right)), nil
	}

	return core.Nil, fmt.Errorf("unsupported binop %s on %s / %s", node.Tok.Val, left.Inspect(), right.Inspect())
}

func isArithOp(tt ast.TokenType) bool {
	switch tt {
	case ast.TPlus, ast.TMinus, ast.TMul, ast.TDiv, ast.TPercent,
		ast.TLt, ast.TGt, ast.TLe, ast.TGe:
		return true
	}
	return false
}

func coerceNumeric(v core.Value) core.Value {
	if v.Kind != core.VString { return v }
	s := strings.TrimSpace(v.Str)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return core.IntVal(n)
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return core.FloatVal(f)
	}
	return v
}

func evalUnary(node *ast.Node, scope core.Scope) (core.Value, error) {
	operand, err := Eval(node.Children[0], scope)
	if err != nil { return core.Nil, err }
	switch node.Tok.Type {
	case ast.TMinus:
		if operand.Kind == core.VInt { return core.IntVal(-operand.GetInt()), nil }
		if operand.Kind == core.VFloat { return core.FloatVal(-operand.GetFloat()), nil }
	case ast.TBang:
		return core.BoolVal(!operand.Truthy()), nil
	}
	return core.Nil, fmt.Errorf("unsupported unary %s", node.Tok.Val)
}

func evalMatch(node *ast.Node, scope core.Scope) (core.Value, error) {
	if len(node.Children) != 2 { return core.Nil, fmt.Errorf("invalid match") }
	val, err := Eval(node.Children[1], scope)
	if err != nil { return core.Nil, err }
	if !TryBind(node.Children[0], val, scope) {
		return core.Nil, patternError(node.Children[0], val)
	}
	return val, nil
}

func evalTuple(node *ast.Node, scope core.Scope) (core.Value, error) {
	elems := make([]core.Value, len(node.Children))
	for i, c := range node.Children {
		v, err := Eval(c, scope)
		if err != nil { return core.Nil, err }
		elems[i] = v
	}
	return core.TupleVal(elems...), nil
}

func evalList(node *ast.Node, scope core.Scope) (core.Value, error) {
	elems := make([]core.Value, 0, len(node.Children))
	for _, c := range node.Children {
		v, err := Eval(c, scope)
		if err != nil { return core.Nil, err }
		elems = append(elems, v)
	}
	// Cons construction: [h | tail] — Rest is the tail expression
	if node.Rest != nil {
		tail, err := Eval(node.Rest, scope)
		if err != nil { return core.Nil, err }
		if tail.Kind != core.VList {
			return core.Nil, fmt.Errorf("cons tail must be a list, got %s", tail.Inspect())
		}
		elems = append(elems, tail.GetElems()...)
	}
	return core.ListVal(elems...), nil
}

func evalCall(node *ast.Node, scope core.Scope) (core.Value, error) {
	callee, err := Eval(node.Children[0], scope)
	if err != nil { return core.Nil, err }

	args := make([]core.Value, len(node.Children)-1)
	for i, c := range node.Children[1:] {
		v, err := Eval(c, scope)
		if err != nil { return core.Nil, err }
		args[i] = v
	}

	if callee.Kind == core.VFn && callee.GetFn() != nil {
		return CallFn(callee.GetFn(), args, scope)
	}
	return core.Nil, fmt.Errorf("not callable: %s", callee.ToStr())
}

func evalIshFn(node *ast.Node, scope core.Scope) (core.Value, error) {
	name := node.Tok.Val

	// Arrow clause form: fn name do pat -> body; pat -> body end
	if len(node.Children) == 0 && len(node.Clauses) > 0 && node.Clauses[0].Pattern != nil {
		clauses := make([]core.FnClause, len(node.Clauses))
		for i, c := range node.Clauses {
			var params []ast.Node
			if c.Pattern != nil {
				if c.Pattern.Kind == ast.NBlock {
					for _, child := range c.Pattern.Children {
						params = append(params, *child)
					}
				} else {
					params = append(params, *c.Pattern)
				}
			}
			clauses[i] = core.FnClause{Params: params, Guard: c.Guard, Body: c.Body}
		}
		fn := &core.FnValue{Name: name, Clauses: clauses, Env: scope}
		if name == "<anon>" {
			return core.FnVal(fn), nil
		}
		shell := scope.NearestEnv()
		if shell != nil { shell.SetFnClauses(name, fn) }
		return core.Nil, nil
	}

	// Single clause form: fn name params... do body end
	params := make([]ast.Node, len(node.Children))
	for i, c := range node.Children { params[i] = *c }

	clause := core.FnClause{
		Params: params,
		Guard:  node.Clauses[0].Guard,
		Body:   node.Clauses[0].Body,
	}
	fn := &core.FnValue{Name: name, Clauses: []core.FnClause{clause}, Env: scope}

	if name == "<anon>" {
		return core.FnVal(fn), nil
	}

	// Single-clause form accumulates — allows building dispatch tables
	// incrementally like Elixir: fn abs n when n < 0 do ... end
	//                            fn abs n do ... end
	shell := scope.NearestEnv()
	if shell != nil { shell.AddFnClauses(name, fn) }
	return core.Nil, nil
}

func evalLambda(node *ast.Node, scope core.Scope) (core.Value, error) {
	params := make([]ast.Node, len(node.Children))
	for i, c := range node.Children { params[i] = *c }
	fn := &core.FnValue{
		Name: "<lambda>",
		Env:  scope,
		Clauses: []core.FnClause{{
			Params: params,
			Body:   node.Clauses[0].Body,
		}},
	}
	return core.FnVal(fn), nil
}

func evalAccess(node *ast.Node, scope core.Scope) (core.Value, error) {
	field := node.Tok.Val

	// Module access: if child is NIdent, look up module
	if node.Children[0].Kind == ast.NIdent {
		modName := node.Children[0].Tok.Val
		if mod, ok := scope.GetModule(modName); ok {
			if fn, ok := mod.Fns[field]; ok {
				if isZeroArity(fn) {
					return CallFn(fn, nil, scope)
				}
				return core.FnVal(fn), nil
			}
			if nfn, ok := mod.NativeFns[field]; ok {
				return core.FnVal(&core.FnValue{Name: modName + "." + field, Native: nfn}), nil
			}
		}
	}

	obj, err := Eval(node.Children[0], scope)
	if err != nil { return core.Nil, err }

	// Map field access
	if obj.Kind == core.VMap && obj.GetMap() != nil {
		if v, ok := obj.GetMap().Get(field); ok {
			return v, nil
		}
	}

	return core.Nil, fmt.Errorf("access %s on %s", field, obj.ToStr())
}

func evalIshIf(node *ast.Node, scope core.Scope) (core.Value, error) {
	for _, clause := range node.Clauses {
		if clause.Pattern != nil {
			cond, err := Eval(clause.Pattern, scope)
			if err != nil { return core.Nil, err }
			if cond.Truthy() {
				return Eval(clause.Body, scope)
			}
		} else {
			return Eval(clause.Body, scope)
		}
	}
	return core.Nil, nil
}

func evalIshMatch(node *ast.Node, scope core.Scope) (core.Value, error) {
	subject, err := Eval(node.Children[0], scope)
	if err != nil { return core.Nil, err }
	for _, clause := range node.Clauses {
		matchScope := core.NewEnv(scope)
		if TryBind(clause.Pattern, subject, matchScope) {
			return Eval(clause.Body, matchScope)
		}
	}
	return core.Nil, fmt.Errorf("no matching clause for %s", subject.Inspect())
}


func evalMap(node *ast.Node, scope core.Scope) (core.Value, error) {
	m := core.NewOrdMap()
	for i := 0; i+1 < len(node.Children); i += 2 {
		key := node.Children[i].Tok.Val
		val, err := Eval(node.Children[i+1], scope)
		if err != nil { return core.Nil, err }
		m.Set(key, val)
	}
	return core.MapVal(m), nil
}

func evalCapture(node *ast.Node, scope core.Scope) (core.Value, error) {
	name := node.Tok.Val
	if fn, ok := scope.GetFn(name); ok {
		return core.FnVal(fn), nil
	}
	if nfn, ok := scope.GetNativeFn(name); ok {
		return core.FnVal(&core.FnValue{Name: name, Native: nfn}), nil
	}
	return core.Nil, fmt.Errorf("undefined function: %s", name)
}

func evalPosixFnDef(node *ast.Node, scope core.Scope) (core.Value, error) {
	name := node.Tok.Val
	fn := &core.FnValue{
		Name: name,
		Clauses: []core.FnClause{{Body: node.Children[0]}},
	}
	shell := scope.NearestEnv()
	if shell != nil { shell.SetFnClauses(name, fn) }
	return core.Nil, nil
}

func evalInterpString(node *ast.Node, scope core.Scope) (core.Value, error) {
	var buf strings.Builder
	for _, seg := range node.Children {
		val, err := Eval(seg, scope)
		if err != nil { return core.Nil, err }
		buf.WriteString(val.ToStr())
	}
	return core.StringVal(buf.String()), nil
}

func evalParamExpand(node *ast.Node, scope core.Scope) (core.Value, error) {
	var expr strings.Builder
	for _, child := range node.Children {
		expr.WriteString(child.Tok.Val)
	}
	expanded := scope.NearestEnv().Expand("${" + expr.String() + "}")
	return core.StringVal(expanded), nil
}

func evalArg(node *ast.Node, scope core.Scope) (core.Value, error) {
	var buf strings.Builder
	for _, child := range node.Children {
		if child.Kind == ast.NIdent {
			buf.WriteString(child.Tok.Val)
			continue
		}
		if child.Kind == ast.NLit && (child.Tok.Type == ast.TFloat || child.Tok.Type == ast.TInt) {
			buf.WriteString(child.Tok.Val)
			continue
		}
		v, err := Eval(child, scope)
		if err != nil { return core.Nil, err }
		buf.WriteString(v.ToStr())
	}
	return core.StringVal(buf.String()), nil
}
