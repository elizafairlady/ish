package debug

import (
	"fmt"
	"os"

	"ish/internal/ast"
)

// TraceNode prints a trace line for the given AST node to stderr.
// Called when set -X is active.
func (d *Debugger) TraceNode(node *ast.Node) {
	if node == nil {
		return
	}

	// Skip non-execution nodes (definitions, containers)
	switch node.Kind {
	case ast.NIshFn, ast.NFnDef, ast.NAssign, ast.NBlock, ast.NLambda:
		return
	}

	loc := ""
	if d.current != nil {
		loc = d.current.FormatPos(node.Pos)
	}

	desc := traceDescription(node)
	if desc == "" {
		return
	}

	if loc != "" {
		fmt.Fprintf(os.Stderr, "+ [%s] %s\n", loc, desc)
	} else {
		fmt.Fprintf(os.Stderr, "+ %s\n", desc)
	}
}

func traceDescription(node *ast.Node) string {
	switch node.Kind {
	case ast.NCmd:
		if len(node.Children) > 0 {
			parts := make([]string, 0, len(node.Children))
			for _, c := range node.Children {
				parts = append(parts, c.Tok.Val)
			}
			result := parts[0]
			for i := 1; i < len(parts); i++ {
				result += " " + parts[i]
			}
			return result
		}
		return ""

	case ast.NLit:
		return node.Tok.Val

	case ast.NWord:
		return node.Tok.Val

	case ast.NBinOp:
		left := ""
		right := ""
		if len(node.Children) > 0 {
			left = node.Children[0].Tok.Val
		}
		if len(node.Children) > 1 {
			right = node.Children[1].Tok.Val
		}
		return left + " " + node.Tok.Val + " " + right

	case ast.NUnary:
		if len(node.Children) > 0 {
			return node.Tok.Val + node.Children[0].Tok.Val
		}
		return node.Tok.Val

	case ast.NIshMatch:
		return "match"

	case ast.NPipe:
		return "pipe"

	case ast.NPipeFn:
		return "pipe_fn"

	case ast.NAndList:
		return "&&"

	case ast.NOrList:
		return "||"

	case ast.NIf:
		return "if"

	case ast.NFor:
		return "for"

	case ast.NWhile:
		return "while"

	case ast.NUntil:
		return "until"

	case ast.NCase:
		return "case"

	case ast.NSubshell:
		return "subshell"

	case ast.NGroup:
		return "group"

	case ast.NTuple:
		return "tuple"

	case ast.NList:
		return "list"

	case ast.NMap:
		return "map"

	case ast.NAccess:
		return "access"

	case ast.NIshSpawn:
		return "spawn"

	case ast.NIshSpawnLink:
		return "spawn_link"

	case ast.NIshSend:
		return "send"

	case ast.NIshReceive:
		return "receive"

	case ast.NIshMonitor:
		return "monitor"

	case ast.NIshAwait:
		return "await"

	case ast.NIshSupervise:
		return "supervise"

	case ast.NIshTry:
		return "try"

	case ast.NRedir:
		return "redirect"

	case ast.NBg:
		return "background"

	case ast.NMatch:
		return "match_bind"

	default:
		return ""
	}
}
