package eval_test

import (
	"fmt"
	"testing"

	"ish/internal/eval"
	"ish/internal/lexer"
	"ish/internal/parser"
	"ish/internal/ast"
	"ish/internal/testutil"
)

func TestParamExpand(t *testing.T) {
	input := `export EDITOR=${EDITOR:-/bin/nano}`
	
	l := lexer.New(input)
	fmt.Println("=== TOKENS ===")
	for {
		tok := l.NextToken()
		fmt.Printf("  %s %q SpaceAfter=%v\n", tok.Type, tok.Val, tok.SpaceAfter)
		if tok.Type == ast.TEOF { break }
	}
	
	l2 := lexer.New(input)
	node, err := parser.Parse(l2)
	if err != nil {
		t.Fatal(err)
	}
	
	// Walk the NParamExpand children
	var walkParam func(n *ast.Node, depth int)
	walkParam = func(n *ast.Node, depth int) {
		if n == nil { return }
		prefix := ""
		for i := 0; i < depth; i++ { prefix += "  " }
		fmt.Printf("%s%d %q\n", prefix, n.Kind, n.Tok.Val)
		for _, c := range n.Children {
			walkParam(c, depth+1)
		}
	}
	
	var findParam func(n *ast.Node)
	findParam = func(n *ast.Node) {
		if n == nil { return }
		if n.Kind == ast.NParamExpand {
			fmt.Println("=== NParamExpand children ===")
			for i, c := range n.Children {
				fmt.Printf("  [%d] kind=%d tok=%q\n", i, c.Kind, c.Tok.Val)
			}
		}
		for _, c := range n.Children {
			findParam(c)
		}
	}
	findParam(node)
	
	env := testutil.TestEnv()
	eval.RunSource(input, env)
	if v, ok := env.Get("EDITOR"); ok {
		fmt.Printf("EDITOR=%q\n", v.ToStr())
	} else {
		fmt.Println("EDITOR not set")
	}
}
