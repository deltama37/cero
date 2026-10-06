package lower

import (
	"fmt"

	"github.com/deltama37/cero/internal/ast"
	"github.com/deltama37/cero/internal/typecheck"
)

// captureSet computes, for anonymous functions, the local symbols they read
// from enclosing functions.
type captureSet struct {
	info     *typecheck.Info
	polyLets map[*typecheck.Symbol]*ast.LetStmt
	memo     map[*ast.FuncLit][]*typecheck.Symbol
}

// of returns the parameters, lets and pattern variables that lit reads but
// does not declare, in order of first occurrence in a depth-first,
// left-to-right walk of its body. A use of a generalized let contributes
// the captures of the let's value instead of the let itself, since a
// generalized let has no run-time local. A nested anonymous function
// contributes its own captures that lit does not declare.
func (cs *captureSet) of(lit *ast.FuncLit) []*typecheck.Symbol {
	if caps, ok := cs.memo[lit]; ok {
		return caps
	}
	declared := make(map[*typecheck.Symbol]bool)
	for _, p := range lit.Params {
		sym := cs.info.Params[p]
		if sym == nil {
			panic(fmt.Sprintf("lower: missing parameter symbol '%s' at %s", p.Name, p.Pos))
		}
		declared[sym] = true
	}
	out := &orderedSyms{seen: make(map[*typecheck.Symbol]bool)}
	var visit func(ast.Expr)
	visit = func(e ast.Expr) {
		switch e := e.(type) {
		case *ast.Ident:
			sym := cs.info.Uses[e]
			if sym == nil {
				panic(fmt.Sprintf("lower: missing symbol for '%s' at %s", e.Name, e.Pos))
			}
			cs.addSym(sym, declared, out)
		case *ast.FuncLit:
			for _, s := range cs.of(e) {
				if !declared[s] {
					out.add(s)
				}
			}
		case *ast.BlockExpr:
			for _, let := range e.Lets {
				visit(let.Value)
				sym := cs.info.Defs[let]
				if sym == nil {
					panic(fmt.Sprintf("lower: missing symbol for let '%s' at %s", let.Name, let.NamePos))
				}
				declared[sym] = true
			}
			visit(e.Result)
		case *ast.MatchExpr:
			visit(e.Scrutinee)
			for _, arm := range e.Arms {
				cs.bindPattern(arm.Pattern, declared)
				visit(arm.Body)
			}
		default:
			for _, child := range exprChildren(e) {
				visit(child)
			}
		}
	}
	visit(lit.Body)
	cs.memo[lit] = out.list
	return out.list
}

// valueCaptures returns the symbols a generalized let's value reads.
// v is an anonymous function or a name.
func (cs *captureSet) valueCaptures(v ast.Expr) []*typecheck.Symbol {
	switch v := v.(type) {
	case *ast.FuncLit:
		return cs.of(v)
	case *ast.Ident:
		sym := cs.info.Uses[v]
		if sym == nil {
			panic(fmt.Sprintf("lower: missing symbol for '%s' at %s", v.Name, v.Pos))
		}
		out := &orderedSyms{seen: make(map[*typecheck.Symbol]bool)}
		cs.addSym(sym, map[*typecheck.Symbol]bool{}, out)
		return out.list
	default:
		panic(fmt.Sprintf("lower: generalized let value is %T", v))
	}
}

func (cs *captureSet) addSym(
	sym *typecheck.Symbol,
	declared map[*typecheck.Symbol]bool,
	out *orderedSyms,
) {
	if sym.Kind != typecheck.SymParam && sym.Kind != typecheck.SymLocal {
		return
	}
	if len(sym.TypeParams) > 0 {
		stmt := cs.polyLets[sym]
		if stmt == nil {
			panic(fmt.Sprintf("lower: missing let for generalized '%s'", sym.Name))
		}
		for _, s := range cs.valueCaptures(stmt.Value) {
			if !declared[s] {
				out.add(s)
			}
		}
		return
	}
	if !declared[sym] {
		out.add(sym)
	}
}

func (cs *captureSet) bindPattern(p ast.Pattern, declared map[*typecheck.Symbol]bool) {
	switch p := p.(type) {
	case *ast.WildcardPat, *ast.IntPat, *ast.BoolPat:
	case *ast.VarPat:
		sym := cs.info.PatVars[p]
		if sym == nil {
			panic(fmt.Sprintf("lower: missing symbol for pattern '%s' at %s", p.Name, p.Pos))
		}
		declared[sym] = true
	case *ast.CtorPat:
		for _, arg := range p.Args {
			cs.bindPattern(arg, declared)
		}
	default:
		panic(fmt.Sprintf("lower: unhandled pattern %T", p))
	}
}

func exprChildren(e ast.Expr) []ast.Expr {
	switch e := e.(type) {
	case *ast.IntLit, *ast.BoolLit:
		return nil
	case *ast.UnaryExpr:
		return []ast.Expr{e.X}
	case *ast.BinaryExpr:
		return []ast.Expr{e.X, e.Y}
	case *ast.CallExpr:
		out := make([]ast.Expr, 0, 1+len(e.Args))
		out = append(out, e.Fn)
		return append(out, e.Args...)
	case *ast.IfExpr:
		return []ast.Expr{e.Cond, e.Then, e.Else}
	default:
		panic(fmt.Sprintf("lower: unhandled expression %T", e))
	}
}

type orderedSyms struct {
	list []*typecheck.Symbol
	seen map[*typecheck.Symbol]bool
}

func (o *orderedSyms) add(s *typecheck.Symbol) {
	if o.seen[s] {
		return
	}
	o.seen[s] = true
	o.list = append(o.list, s)
}
