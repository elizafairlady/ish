package ast

type NodeKind byte

const (
	// Values
	NLit    NodeKind = iota // literal: int, float, string, nil, true, false
	NIdent                  // bare identifier
	NAtom                   // :name
	NVarRef                 // $var

	// Data structures
	NList  // [items]
	NTuple // {items}

	// Operations
	NBinOp  // left op right
	NUnary  // op expr
	NAccess // expr.field

	// Application (the core unification: commands ARE expressions)
	NApply // head arg arg arg — juxtaposition
	NCall  // callee(arg, arg) — paren-delimited

	// Pipeline and composition
	NPipe    // left | right (byte pipe)
	NPipeFn  // left |> right (value pipe)
	NPipeAmp // left |& right (pipe stdout+stderr)
	NAndList // left && right
	NOrList  // left || right
	NBg      // expr &

	// Binding and control flow
	NBind  // name = expr
	NIf    // if cond do body [else body] end
	NFor   // for var in expr do body end/done
	NWhile // while cond do body end/done
	NMatch // match expr do clauses end
	NCase  // POSIX case word in patterns esac
	NFnDef // fn name params do body end
	NLambda    // \params -> expr
	NCons      // [head | tail] — cons pattern/construction
	NMap       // %{key: val, ...}
	NBlock     // sequence of statements
	NDefModule // defmodule Name do body end
	NUseImport // use Module / import Module
	NReceive   // receive do clauses [after timeout -> body] end
	NTry       // try do body rescue clauses end

	NSubshell // (cmd; cmd) — isolated scope

	// Shell-specific
	NFlag // -flag in command context
	NPath // ~/path or /path in command context
	NIPv4 // 192.168.1.1
	NIPv6 // ::1, fe80::1

	// Interpolation and expansion
	NCmdSub      // $(cmd)
	NParamExpand // ${var} ${var:-default}
	NInterpStr   // "hello #{expr} world"
)

// Redirect describes an I/O redirection.
type Redirect struct {
	Op     TokenType // TGt, TAppend, TLt
	Fd     int       // 0=stdin, 1=stdout, 2=stderr
	Target *Node
	FdDup  bool // true for >&N fd duplication
}

// Clause is used by NIf (condition+body pairs) and NMatch (pattern+body pairs).
type Clause struct {
	Pattern *Node // nil for else/default
	Guard   *Node // when guard expression
	Body    *Node
}

// Node is the AST node.
type Node struct {
	Kind     NodeKind
	Tok      Token      // the relevant token (operator, identifier, literal value)
	Children []*Node    // sub-expressions (meaning depends on Kind)
	Clauses  []Clause   // for NIf, NMatch, NCase
	Redirs   []Redirect // for NApply only
}
