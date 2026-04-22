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

func evalVarRef(node *ast.Node, scope core.Scope) (core.Value, error) {
	name := node.Tok.Val

	// Special variables: $?, $$, $!, $@, $*, $#, $0-$9
	if node.Tok.Type == ast.TSpecialVar {
		return evalSpecialVar(name, scope), nil
	}

	if v, ok := scope.Get(name); ok {
		return v, nil
	}
	return core.StringVal(""), nil
}

func evalSpecialVar(name string, scope core.Scope) core.Value {
	if len(name) < 2 || name[0] != '$' {
		return core.StringVal(name)
	}
	switch name[1] {
	case '?':
		return core.IntVal(int64(scope.GetCtx().ExitCode()))
	case '$':
		return core.IntVal(int64(scope.NearestEnv().Pid()))
	case '!':
		return core.IntVal(int64(scope.NearestEnv().BgPid()))
	case '#':
		return core.IntVal(int64(len(scope.NearestEnv().PosArgs())))
	case '@':
		args := scope.NearestEnv().PosArgs()
		vals := make([]core.Value, len(args))
		for i, a := range args { vals[i] = core.StringVal(a) }
		return core.ListVal(vals...)
	case '*':
		sep := " "
		if ifs, ok := scope.Get("IFS"); ok {
			ifsStr := ifs.ToStr()
			if len(ifsStr) > 0 { sep = ifsStr[:1] } else { sep = "" }
		}
		return core.StringVal(strings.Join(scope.NearestEnv().PosArgs(), sep))
	default:
		if name[1] >= '0' && name[1] <= '9' {
			idx := int(name[1] - '0')
			args := scope.NearestEnv().PosArgs()
			if idx == 0 { return core.StringVal("ish") }
			if idx <= len(args) { return core.StringVal(args[idx-1]) }
			return core.StringVal("")
		}
	}
	return core.StringVal(name)
}

func evalCmdSubNode(node *ast.Node, scope core.Scope) (core.Value, error) {
	if len(node.Children) == 0 { return core.StringVal(""), nil }

	r, w, err := os.Pipe()
	if err != nil { return core.Nil, err }

	childEnv := core.NewEnv(scope)
	childEnv.Ctx = scope.GetCtx().ForRedirect(w)

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	val, evalErr := Eval(node.Children[0], childEnv)
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

func evalPosixAssign(node *ast.Node, scope core.Scope) (core.Value, error) {
	name := node.Tok.Val
	var val core.Value
	if len(node.Children) > 0 {
		v, err := Eval(node.Children[0], scope)
		if err != nil { return core.Nil, err }
		val = v
	} else {
		val = core.StringVal("")
	}
	scope.Set(name, val)
	scope.GetCtx().SetExit(0)
	return core.Nil, nil
}
