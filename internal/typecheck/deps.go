package typecheck

import (
	"fmt"
	"sort"

	"github.com/deltama37/cero/internal/ast"
)

// funcRefs returns the names of top-level functions that body (with
// parameters params) refers to, in order of first occurrence and without
// duplicates. A name bound by a parameter, a let, a variable pattern, or a
// parameter of an anonymous function is local while it is in scope and is
// not reported. isFunc reports whether a name is a top-level function.
func funcRefs(
	params []*ast.Param,
	body *ast.BlockExpr,
	isFunc func(string) bool,
) []string {
	var refs []string
	seen := make(map[string]bool)
	var scopes []map[string]bool

	push := func() {
		scopes = append(scopes, make(map[string]bool))
	}
	pop := func() {
		scopes = scopes[:len(scopes)-1]
	}
	bind := func(name string) {
		scopes[len(scopes)-1][name] = true
	}
	inScope := func(name string) bool {
		for i := len(scopes) - 1; i >= 0; i-- {
			if scopes[i][name] {
				return true
			}
		}
		return false
	}
	add := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		refs = append(refs, name)
	}

	var walkExpr func(ast.Expr)
	var walkPat func(ast.Pattern)

	walkPat = func(p ast.Pattern) {
		switch p := p.(type) {
		case *ast.WildcardPat, *ast.IntPat, *ast.BoolPat, *ast.StrPat:
		case *ast.VarPat:
			bind(p.Name)
		case *ast.CtorPat:
			for _, arg := range p.Args {
				walkPat(arg)
			}
		default:
			panic(fmt.Sprintf("typecheck: unhandled pattern %T", p))
		}
	}

	walkExpr = func(e ast.Expr) {
		switch e := e.(type) {
		case *ast.IntLit, *ast.BoolLit, *ast.StringLit:
		case *ast.Ident:
			if !inScope(e.Name) && isFunc(e.Name) {
				add(e.Name)
			}
		case *ast.UnaryExpr:
			walkExpr(e.X)
		case *ast.BinaryExpr:
			walkExpr(e.X)
			walkExpr(e.Y)
		case *ast.CallExpr:
			walkExpr(e.Fn)
			for _, arg := range e.Args {
				walkExpr(arg)
			}
		case *ast.IfExpr:
			walkExpr(e.Cond)
			walkExpr(e.Then)
			walkExpr(e.Else)
		case *ast.BlockExpr:
			push()
			for _, letStmt := range e.Lets {
				walkExpr(letStmt.Value)
				bind(letStmt.Name)
			}
			walkExpr(e.Result)
			pop()
		case *ast.FuncLit:
			push()
			for _, p := range e.Params {
				bind(p.Name)
			}
			walkExpr(e.Body)
			pop()
		case *ast.MatchExpr:
			walkExpr(e.Scrutinee)
			for _, arm := range e.Arms {
				push()
				walkPat(arm.Pattern)
				walkExpr(arm.Body)
				pop()
			}
		default:
			panic(fmt.Sprintf("typecheck: unhandled expression %T", e))
		}
	}

	push()
	for _, p := range params {
		bind(p.Name)
	}
	walkExpr(body)
	return refs
}

// sccs returns the strongly connected components of the graph whose nodes
// are 0..n-1 and whose edges are edges[i], using Tarjan's algorithm. Nodes
// are visited in increasing order and edges in slice order. A component is
// listed after every component it has an edge to, and the nodes in a
// component are sorted in increasing order.
func sccs(n int, edges [][]int) [][]int {
	index := 0
	indices := make([]int, n)
	lowlink := make([]int, n)
	for i := range indices {
		indices[i] = -1
	}
	stack := make([]int, 0, n)
	onStack := make([]bool, n)
	var out [][]int

	var strongconnect func(int)
	strongconnect = func(v int) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		var succ []int
		if v < len(edges) {
			succ = edges[v]
		}
		for _, w := range succ {
			if indices[w] < 0 {
				strongconnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] && indices[w] < lowlink[v] {
				lowlink[v] = indices[w]
			}
		}
		if lowlink[v] == indices[v] {
			var comp []int
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			sort.Ints(comp)
			out = append(out, comp)
		}
	}

	for v := 0; v < n; v++ {
		if indices[v] < 0 {
			strongconnect(v)
		}
	}
	return out
}
