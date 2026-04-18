package main

type NodeKind byte

const (
	NLit     NodeKind = iota // literal value
	NWord                    // bare word / variable reference
	NCmd                     // simple command: Children[0]=name, Children[1:]=args (all nodes)
	NPipe                    // cmd | cmd (Unix pipe)
	NPipeFn                  // expr |> fn (functional pipe)
	NAndList                 // cmd && cmd
	NOrList                  // cmd || cmd
	NBg                      // cmd &
	NBlock                   // sequence of statements
	NAssign                  // POSIX VAR=value (Tok.Val = "KEY=value")
	NMatch                   // pattern = expr (ish match/bind)
	NRedir                   // command with redirection
	NSubshell                // (cmd; cmd)
	NGroup                   // { cmd; cmd; }

	// POSIX compound commands
	NIf    // if/then/elif/else/fi
	NFor   // for/in/do/done
	NWhile // while/do/done
	NUntil // until/do/done
	NCase  // case/esac
	NFnDef // name() { body; }  (POSIX function)

	// ish extensions
	NIshFn        // fn name pattern... do body end
	NIshMatch     // match expr do clauses end
	NIshSpawn     // spawn expr
	NIshSpawnLink // spawn_link expr
	NIshSend      // send pid, msg
	NIshReceive   // receive do clauses end
	NIshMonitor   // monitor pid
	NIshAwait     // await pid [timeout]
	NIshSupervise // supervise strategy do workers end
	NIshTry       // try body rescue clauses end

	// Expressions
	NBinOp  // binary operation (+, -, *, /, ==, !=, <, >, etc.)
	NUnary  // unary operation (!, -)
	NTuple  // {a, b, c}
	NList   // [a, b, c]
	NMap    // %{k: v}
	NAccess // expr.field
)

type Node struct {
	Kind        NodeKind
	Pos         int
	Tok         Token    // associated token (for literals, operators, etc.)
	Children    []*Node  // sub-nodes (for NCmd: [nameNode, arg1Node, arg2Node, ...])
	Assigns     []*Node  // prefix assignments for NCmd (FOO=bar cmd)
	Rest        *Node    // for NList: tail variable in [h | t] pattern
	Clauses     []Clause // for NIf, NCase, NIshMatch, NIshReceive
	Redirs      []Redir  // attached redirections
	Timeout     *Node    // for receive: after timeout expression
	TimeoutBody *Node    // for receive: after timeout body
}

type Clause struct {
	Pattern *Node // pattern or condition
	Guard   *Node // optional guard (when)
	Body    *Node // body to execute
}

type Redir struct {
	Op     TokenType // TRedirOut, TRedirAppend, TRedirIn, THeredoc
	Fd     int       // file descriptor (0 for stdin, 1 for stdout, 2 for stderr)
	Target string    // filename or heredoc delimiter
	Quoted bool      // true for quoted heredoc delimiter (no expansion)
}

func litNode(tok Token) *Node {
	return &Node{Kind: NLit, Tok: tok, Pos: tok.Pos}
}

func wordNode(tok Token) *Node {
	return &Node{Kind: NWord, Tok: tok, Pos: tok.Pos}
}

func blockNode(stmts []*Node) *Node {
	if len(stmts) == 1 {
		return stmts[0]
	}
	return &Node{Kind: NBlock, Children: stmts}
}
