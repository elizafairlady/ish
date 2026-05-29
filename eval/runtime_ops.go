package eval

import (
	"fmt"
	"strings"

	"ish/core"
)

// PanicError is an ish-level raised failure carrying a value. It is produced by
// `panic` and recognized by `rescue`; uncaught, it propagates like any runtime
// error and (in a spawned process) terminates it, notifying monitors/links —
// the Erlang "fail hard" default, with `rescue` as the explicit recovery point.
type PanicError struct{ Value Value }

func (e *PanicError) Error() string {
	return fmt.Sprintf("panic: %s", displayValue(e.Value))
}

// panicFn raises its argument as a PanicError.
func panicFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("panic", args, 1); err != nil {
		return nil, err
	}
	return nil, &PanicError{Value: args[0]}
}

// rescueFn runs a body thunk and, if it fails, calls a handler with the failure
// reified as a value: a `panic`'s value verbatim, or `{:error <message>}` for
// any other runtime error. With no failure it returns the body's value. This is
// the deliberate recovery boundary; without it failures propagate and crash.
func rescueFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("rescue", args, 2); err != nil {
		return nil, err
	}
	if !isCallable(args[0]) {
		return nil, fmt.Errorf("rescue: body must be callable, got %T", args[0])
	}
	if !isCallable(args[1]) {
		return nil, fmt.Errorf("rescue: handler must be callable, got %T", args[1])
	}
	v, err := apply(args[0], nil, env)
	if err == nil {
		return v, nil
	}
	return apply(args[1], []Value{failureValue(err)}, env)
}

// ensureFn runs a body thunk and then a cleanup thunk unconditionally,
// returning the body's result (or propagating its failure) once cleanup has
// run; a failure in cleanup itself takes over only when the body succeeded.
func ensureFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("ensure", args, 2); err != nil {
		return nil, err
	}
	if !isCallable(args[0]) {
		return nil, fmt.Errorf("ensure: body must be callable, got %T", args[0])
	}
	if !isCallable(args[1]) {
		return nil, fmt.Errorf("ensure: cleanup must be callable, got %T", args[1])
	}
	v, bodyErr := apply(args[0], nil, env)
	_, cleanupErr := apply(args[1], nil, env)
	if bodyErr != nil {
		return nil, bodyErr
	}
	if cleanupErr != nil {
		return nil, cleanupErr
	}
	return v, nil
}

// failureValue reifies a Go error as an ish value for a rescue handler.
func failureValue(err error) Value {
	if pe, ok := err.(*PanicError); ok {
		return pe.Value
	}
	return core.Tuple{core.Atom("error"), core.String(err.Error())}
}

func strConcatFn(args []Value, _ *Env) (Value, error) {
	var b strings.Builder
	for i, a := range args {
		s, ok := a.(core.String)
		if !ok {
			return nil, fmt.Errorf("str-concat: argument %d is %T, want string", i, a)
		}
		b.WriteString(string(s))
	}
	return core.String(b.String()), nil
}

func strLengthFn(args []Value, _ *Env) (Value, error) {
	s, err := argString("str-length", args, 0)
	if err != nil {
		return nil, err
	}
	return core.Int(len([]rune(string(s)))), nil
}

// strSliceFn returns the half-open rune slice [start,end) of a string.
func strSliceFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("str-slice", args, 3); err != nil {
		return nil, err
	}
	s, err := argString("str-slice", args, 0)
	if err != nil {
		return nil, err
	}
	start, ok := args[1].(core.Int)
	end, ok2 := args[2].(core.Int)
	if !ok || !ok2 {
		return nil, fmt.Errorf("str-slice: start and end must be ints")
	}
	runes := []rune(string(s))
	if start < 0 || end < start || int(end) > len(runes) {
		return nil, fmt.Errorf("str-slice: range [%d,%d) out of bounds for length %d", start, end, len(runes))
	}
	return core.String(string(runes[start:end])), nil
}

// strSplitFn splits a string by a separator into a vector of strings.
func strSplitFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("str-split", args, 2); err != nil {
		return nil, err
	}
	s, err := argString("str-split", args, 0)
	if err != nil {
		return nil, err
	}
	sep, err := argString("str-split", args, 1)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(s), string(sep))
	out := make(core.Vector, len(parts))
	for i, p := range parts {
		out[i] = core.String(p)
	}
	return out, nil
}

func argString(name string, args []Value, i int) (core.String, error) {
	s, ok := args[i].(core.String)
	if !ok {
		return "", fmt.Errorf("%s: argument %d is %T, want string", name, i, args[i])
	}
	return s, nil
}

// toStringFn renders any value as a string using the shared display form.
func toStringFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("to-string", args, 1); err != nil {
		return nil, err
	}
	return core.String(displayValue(args[0])), nil
}

// printFn writes each argument's display form to stdout separated by spaces,
// followed by a newline, and returns :nil.
func printFn(args []Value, _ *Env) (Value, error) {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = displayValue(a)
	}
	fmt.Println(strings.Join(parts, " "))
	return core.Nil{}, nil
}

// displayValue is the human-readable rendering of a runtime value, shared by
// to-string, print, and panic messages.
func displayValue(v Value) string {
	switch x := v.(type) {
	case core.String:
		return string(x)
	case core.Atom:
		return ":" + string(x)
	case core.Word:
		return string(x)
	case core.Int:
		return fmt.Sprintf("%d", int64(x))
	case core.Float:
		return fmt.Sprintf("%g", float64(x))
	case core.Bytes:
		return fmt.Sprintf("#bytes[%d]", len(x))
	case core.Nil:
		return "nil"
	case core.PID:
		return fmt.Sprintf("#pid<%d>", uint64(x))
	case core.Ref:
		return fmt.Sprintf("#ref<%d>", uint64(x))
	case core.Pair:
		return "(" + strings.Join(displayList(x), " ") + ")"
	case core.Vector:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = displayValue(e)
		}
		return "[" + strings.Join(parts, " ") + "]"
	case core.Tuple:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = displayValue(e)
		}
		return "{" + strings.Join(parts, " ") + "}"
	case core.Dict:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = displayValue(e.Key) + " " + displayValue(e.Value)
		}
		return "%{" + strings.Join(parts, " ") + "}"
	case core.Tagged:
		return ":" + string(x.Tag) + displayValue(x.Fields)
	case *core.Closure, *core.Native:
		return "#function"
	}
	return fmt.Sprintf("%v", v)
}

func displayList(p core.Pair) []string {
	var out []string
	cur := core.Datum(p)
	for {
		pair, ok := cur.(core.Pair)
		if !ok {
			if _, isNil := cur.(core.Nil); !isNil {
				out = append(out, "."+" "+displayValue(cur))
			}
			return out
		}
		out = append(out, displayValue(pair.Head))
		cur = pair.Tail
	}
}

// localExpandFn expands a sub-form in the current (use-site) expansion context
// and returns the expanded syntax. Available only inside a macro body (phase
// 1); the result is intended for inspection — re-embedding fully expanded core
// into a template and re-expanding it is not supported.
func localExpandFn(args []Value, env *Env) (Value, error) {
	if err := wantArgs("local-expand", args, 1); err != nil {
		return nil, err
	}
	stx, ok := args[0].(*core.Syntax)
	if !ok {
		return nil, fmt.Errorf("local-expand: argument must be syntax, got %T", args[0])
	}
	if env == nil || env.Expander == nil {
		return nil, fmt.Errorf("local-expand: only available during macro expansion")
	}
	return env.Expander.LocalExpand(stx)
}

// bindIdentifierFn (bind!) mints a fresh hygienic identifier carrying a unique
// scope, optionally from a name hint, for use as a collision-free binding name
// in a macro's output. Available only inside a macro body (phase 1).
func bindIdentifierFn(args []Value, env *Env) (Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("bind!: want zero or one name argument, got %d", len(args))
	}
	if env == nil || env.Expander == nil {
		return nil, fmt.Errorf("bind!: only available during macro expansion")
	}
	name := core.Word("")
	if len(args) == 1 {
		switch n := args[0].(type) {
		case core.Word:
			name = n
		case core.Atom:
			name = core.Word(n)
		case core.String:
			name = core.Word(n)
		case *core.Syntax:
			if w, ok := n.Node.(core.Word); ok {
				name = w
			}
		default:
			return nil, fmt.Errorf("bind!: name hint must be a word, atom, or string, got %T", args[0])
		}
	}
	return env.Expander.FreshIdentifier(name), nil
}
