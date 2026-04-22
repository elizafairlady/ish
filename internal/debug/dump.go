package debug

import (
	"fmt"
	"io"
	"strings"

	"ish/internal/ast"
)

// DumpAST writes an indented tree representation of the AST to w.
func DumpAST(node *ast.Node, sm *SourceMap, w io.Writer) {
	dumpNode(node, sm, w, 0)
}

func dumpNode(node *ast.Node, sm *SourceMap, w io.Writer, depth int) {
	if node == nil {
		return
	}

	indent := strings.Repeat("  ", depth)
	kind := nodeKindString(node.Kind)

	// Position info
	pos := ""
	if sm != nil && node.Pos > 0 {
		line, col := sm.Resolve(node.Pos)
		pos = fmt.Sprintf(" [%d:%d]", line, col)
	}

	// Token value
	tok := ""
	if node.Tok.Val != "" {
		tok = fmt.Sprintf(" %q", node.Tok.Val)
	}

	fmt.Fprintf(w, "%s%s%s%s\n", indent, kind, pos, tok)

	// Prefix assignments
	for _, a := range node.Assigns {
		fmt.Fprintf(w, "%s  Assign:\n", indent)
		dumpNode(a, sm, w, depth+2)
	}

	// Children
	for _, c := range node.Children {
		dumpNode(c, sm, w, depth+1)
	}

	// Clauses
	for i, cl := range node.Clauses {
		fmt.Fprintf(w, "%s  Clause %d:\n", indent, i)
		if cl.Pattern != nil {
			fmt.Fprintf(w, "%s    Pattern:\n", indent)
			dumpNode(cl.Pattern, sm, w, depth+3)
		}
		if cl.Guard != nil {
			fmt.Fprintf(w, "%s    Guard:\n", indent)
			dumpNode(cl.Guard, sm, w, depth+3)
		}
		if cl.Body != nil {
			fmt.Fprintf(w, "%s    Body:\n", indent)
			dumpNode(cl.Body, sm, w, depth+3)
		}
	}

	// Rest (list tail)
	if node.Rest != nil {
		fmt.Fprintf(w, "%s  Rest:\n", indent)
		dumpNode(node.Rest, sm, w, depth+2)
	}

	// Redirections
	for _, r := range node.Redirs {
		targetStr := ""
		if r.TargetNode != nil {
			targetStr = r.TargetNode.Tok.Val
		}
		fmt.Fprintf(w, "%s  Redir: fd=%d op=%d target=%q\n", indent, r.Fd, r.Op, targetStr)
	}

	// Timeout
	if node.Timeout != nil {
		fmt.Fprintf(w, "%s  Timeout:\n", indent)
		dumpNode(node.Timeout, sm, w, depth+2)
	}
	if node.TimeoutBody != nil {
		fmt.Fprintf(w, "%s  TimeoutBody:\n", indent)
		dumpNode(node.TimeoutBody, sm, w, depth+2)
	}
}

func nodeKindString(k ast.NodeKind) string {
	switch k {
	case ast.NLit:
		return "NLit"
	case ast.NIdent:
		return "NWord"
	case ast.NCmd:
		return "NCmd"
	case ast.NPipe:
		return "NPipe"
	case ast.NPipeFn:
		return "NPipeFn"
	case ast.NAndList:
		return "NAndList"
	case ast.NOrList:
		return "NOrList"
	case ast.NBg:
		return "NBg"
	case ast.NBlock:
		return "NBlock"
	case ast.NAssign:
		return "NAssign"
	case ast.NMatch:
		return "NMatch"
	case ast.NVarRef:
		return "NVarRef"
	case ast.NCall:
		return "NCall"
	case ast.NCmdSub:
		return "NCmdSub"
	case ast.NArithSub:
		return "NArithSub"
	case ast.NParamExpand:
		return "NParamExpand"
	case ast.NInterpolation:
		return "NInterpolation"
	case ast.NInterpString:
		return "NInterpString"
	case ast.NPath:
		return "NPath"
	case ast.NIPv4:
		return "NIPv4"
	case ast.NIPv6:
		return "NIPv6"
	case ast.NFlag:
		return "NFlag"
	case ast.NIshIf:
		return "NIshIf"
	case ast.NSubshell:
		return "NSubshell"
	case ast.NGroup:
		return "NGroup"
	case ast.NIf:
		return "NIf"
	case ast.NFor:
		return "NFor"
	case ast.NWhile:
		return "NWhile"
	case ast.NUntil:
		return "NUntil"
	case ast.NCase:
		return "NCase"
	case ast.NFnDef:
		return "NFnDef"
	case ast.NIshFn:
		return "NIshFn"
	case ast.NIshMatch:
		return "NIshMatch"
	case ast.NIshSpawn:
		return "NIshSpawn"
	case ast.NIshSpawnLink:
		return "NIshSpawnLink"
	case ast.NIshSend:
		return "NIshSend"
	case ast.NIshReceive:
		return "NIshReceive"
	case ast.NIshMonitor:
		return "NIshMonitor"
	case ast.NIshAwait:
		return "NIshAwait"
	case ast.NIshSupervise:
		return "NIshSupervise"
	case ast.NIshTry:
		return "NIshTry"
	case ast.NBinOp:
		return "NBinOp"
	case ast.NUnary:
		return "NUnary"
	case ast.NTuple:
		return "NTuple"
	case ast.NList:
		return "NList"
	case ast.NMap:
		return "NMap"
	case ast.NAccess:
		return "NAccess"
	case ast.NLambda:
		return "NLambda"
	default:
		return fmt.Sprintf("Unknown(%d)", k)
	}
}
