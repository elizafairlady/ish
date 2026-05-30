package eval

import (
	"strings"

	"ish/core"
)

// evalSyntaxParse evaluates an expanded syntax-parse/syntax-case form. It
// matches the target syntax against each clause's compiled pattern in order;
// the first clause whose pattern matches — and whose fender guard, if present,
// is truthy — binds its attributes into a frame and runs the body under them.
// A non-matching clause (or a falsy guard) falls through; a guard that errors
// aborts. If no clause matches it is a syntax error reporting the collected
// per-clause reasons.
func evalSyntaxParse(n core.SyntaxParse, env *Env) (Value, error) {
	tv, err := EvalExpr(n.Target, env)
	if err != nil {
		return nil, err
	}
	target, ok := tv.(*core.Syntax)
	if !ok {
		return nil, &core.SyntaxError{Message: "syntax-parse: target must be syntax"}
	}
	var failures []string
	for _, clause := range n.Clauses {
		frame := map[core.BindingID]Value{}
		matched, fail := match(clause.Pattern, target, env, frame)
		if !matched {
			failures = append(failures, fail)
			continue
		}
		clauseEnv := env.extend(frame)
		if clause.Guard != nil {
			// A guard that errors or is non-truthy is a clause failure, not an
			// abort — uniform with fn/match/receive (Erlang/Elixir semantics):
			// move on to the next clause.
			g, gerr := EvalExpr(clause.Guard, clauseEnv)
			if gerr != nil || !truthy(g) {
				failures = append(failures, "guard rejected match")
				continue
			}
		}
		return EvalExpr(clause.Body, clauseEnv)
	}
	return nil, &core.SyntaxError{Syntax: target, Message: spFailureMessage(failures)}
}

func spFailureMessage(failures []string) string {
	seen := map[string]bool{}
	var parts []string
	for _, f := range failures {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		parts = append(parts, f)
	}
	if len(parts) == 0 {
		return "syntax-parse: no matching clause"
	}
	return "syntax-parse: no matching clause: " + strings.Join(parts, "; ")
}
