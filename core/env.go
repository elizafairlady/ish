package core

// Env is an immutable lexical environment: a chain of frames mapping a
// BindingID to its runtime value. Binding never mutates a frame — it pushes a
// new child frame with Extend — so an Env can be shared freely (captured by a
// closure, read by another process) without locks or copying. This is what
// makes functions first-class data that cross process boundaries safely: the
// captured environment is immutable, so share-nothing holds by construction
// rather than by deep-copying on send.
//
// Values are `any` because core does not depend on the evaluator's value
// universe; concretely a frame holds Datums (including *Closure and *Native).
type Env struct {
	parent *Env
	frame  map[BindingID]any
}

// NewEnv returns the empty environment. A nil *Env is also a valid empty
// environment, so callers may start from nil.
func NewEnv() *Env { return nil }

// Lookup walks the parent chain for id.
func (e *Env) Lookup(id BindingID) (any, bool) {
	for cur := e; cur != nil; cur = cur.parent {
		if v, ok := cur.frame[id]; ok {
			return v, true
		}
	}
	return nil, false
}

// Extend returns a new environment with frame layered over e. e is unchanged;
// nil (the empty environment) is a valid receiver. The frame is taken by
// reference so a letrec can fill it after the child env (and the closures that
// captured it) exist; once construction finishes it is never written again.
func (e *Env) Extend(frame map[BindingID]any) *Env {
	return &Env{parent: e, frame: frame}
}

// Closure is a user-defined function value: its clauses plus the immutable
// lexical environment captured at definition. It is a first-class Datum, so it
// can be stored in compounds and sent in messages. The evaluator supplies the
// dynamic call context (process, runtime) at call time; the closure carries
// only lexical state.
type Closure struct {
	Clauses []LambdaClause
	Env     *Env
}

// Native is a primitive function value backed by a host implementation. Impl is
// opaque to core (the evaluator stores and invokes its own function type);
// Native is a first-class Datum so primitives are first-class too. It is used by
// pointer so distinct primitives are distinguishable by identity (its Impl is
// not comparable).
type Native struct {
	Name Word
	Impl any
}

func (*Closure) datum() {}
func (*Native) datum()  {}
