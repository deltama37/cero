// Package typecheck resolves names and type-checks Cero v0.1 programs.
package typecheck

import (
	"github.com/deltama37/cero/internal/ast"
	"github.com/deltama37/cero/internal/diag"
	"github.com/deltama37/cero/internal/token"
	"github.com/deltama37/cero/internal/types"
)

// SymbolKind classifies a declared name.
type SymbolKind int

const (
	SymFunc  SymbolKind = iota // top-level function
	SymParam                   // parameter of a FuncDecl or FuncLit
	SymLocal                   // let binding
)

// Symbol is one declared name. Each declaration (every let, even when
// shadowing) gets its own *Symbol.
type Symbol struct {
	Kind SymbolKind
	Name string
	Type types.Type
	Pos  diag.Pos      // position of the declaring name
	Decl *ast.FuncDecl // set only for SymFunc
}

// Info records the result of a successful type check.
type Info struct {
	Types    map[ast.Expr]types.Type      // type of every expression node
	Uses     map[*ast.Ident]*Symbol       // symbol referenced by every Ident expression
	Defs     map[*ast.LetStmt]*Symbol     // symbol declared by every let
	Params   map[*ast.Param]*Symbol       // symbol declared by every parameter
	Funcs    map[*ast.FuncDecl]*Symbol    // symbol of every top-level function
	FuncLits map[*ast.FuncLit]*types.Func // signature of every anonymous function
}

// Check resolves names and type-checks file.
// On error it returns (nil, *diag.Error).
func Check(file *ast.File) (*Info, error) {
	c := &checker{
		info: &Info{
			Types:    make(map[ast.Expr]types.Type),
			Uses:     make(map[*ast.Ident]*Symbol),
			Defs:     make(map[*ast.LetStmt]*Symbol),
			Params:   make(map[*ast.Param]*Symbol),
			Funcs:    make(map[*ast.FuncDecl]*Symbol),
			FuncLits: make(map[*ast.FuncLit]*types.Func),
		},
		globals: make(map[string]*Symbol),
	}

	sigs := make([]*types.Func, len(file.Funcs))
	for i, d := range file.Funcs {
		if _, ok := c.globals[d.Name]; ok {
			return nil, diag.Errorf(d.NamePos, "duplicate function '%s'", d.Name)
		}
		sig, err := c.resolveFuncType(d.Params, d.Result)
		if err != nil {
			return nil, err
		}
		sigs[i] = sig
		sym := &Symbol{
			Kind: SymFunc,
			Name: d.Name,
			Type: sig,
			Pos:  d.NamePos,
			Decl: d,
		}
		c.globals[d.Name] = sym
		c.info.Funcs[d] = sym
	}

	for i, d := range file.Funcs {
		ctx := &funcCtx{}
		if err := c.checkFunc(ctx, d.Params, sigs[i], d.Body); err != nil {
			return nil, err
		}
	}

	main, ok := c.globals["main"]
	if !ok {
		return nil, diag.Errorf(diag.Pos{Line: 1, Col: 1}, "missing function 'main'")
	}
	want := &types.Func{Result: types.Int}
	if !types.Equal(main.Type, want) {
		return nil, diag.Errorf(main.Pos, "function 'main' must have type () -> Int, found %s", main.Type)
	}
	return c.info, nil
}

type checker struct {
	info    *Info
	globals map[string]*Symbol
}

// funcCtx is the scope stack of one function (a FuncDecl or a FuncLit).
// parent is the function that encloses an anonymous function.
type funcCtx struct {
	parent *funcCtx
	scope  *scope
}

type scope struct {
	parent *scope
	names  map[string]*Symbol
}

func (c *checker) resolveFuncType(params []*ast.Param, result ast.TypeExpr) (*types.Func, *diag.Error) {
	sig := &types.Func{Params: make([]types.Type, len(params))}
	for i, p := range params {
		pt, err := c.resolveType(p.Type)
		if err != nil {
			return nil, err
		}
		sig.Params[i] = pt
	}
	rt, err := c.resolveType(result)
	if err != nil {
		return nil, err
	}
	sig.Result = rt
	return sig, nil
}

func (c *checker) resolveType(t ast.TypeExpr) (types.Type, *diag.Error) {
	switch t := t.(type) {
	case *ast.NamedType:
		switch t.Name {
		case "Int":
			return types.Int, nil
		case "Bool":
			return types.Bool, nil
		default:
			return nil, diag.Errorf(t.Pos, "unknown type '%s'", t.Name)
		}
	case *ast.FuncType:
		params := make([]types.Type, len(t.Params))
		for i, p := range t.Params {
			pt, err := c.resolveType(p)
			if err != nil {
				return nil, err
			}
			params[i] = pt
		}
		result, err := c.resolveType(t.Result)
		if err != nil {
			return nil, err
		}
		return &types.Func{Params: params, Result: result}, nil
	default:
		return nil, diag.Errorf(t.Position(), "unknown type '%T'", t)
	}
}

func (c *checker) checkFunc(
	ctx *funcCtx,
	params []*ast.Param,
	sig *types.Func,
	body *ast.BlockExpr,
) *diag.Error {
	ctx.scope = &scope{names: make(map[string]*Symbol)}
	for i, p := range params {
		if _, exists := ctx.scope.names[p.Name]; exists {
			return diag.Errorf(p.Pos, "duplicate parameter '%s'", p.Name)
		}
		sym := &Symbol{
			Kind: SymParam,
			Name: p.Name,
			Type: sig.Params[i],
			Pos:  p.Pos,
		}
		c.info.Params[p] = sym
		ctx.scope.names[p.Name] = sym
	}
	return c.expect(ctx, body, sig.Result)
}

func (c *checker) pushScope(ctx *funcCtx) {
	ctx.scope = &scope{parent: ctx.scope, names: make(map[string]*Symbol)}
}

func (c *checker) popScope(ctx *funcCtx) {
	ctx.scope = ctx.scope.parent
}

// lookup walks scopes from the inside out. captured is true when sym was
// found in an enclosing function rather than the current one or the globals.
func (c *checker) lookup(ctx *funcCtx, name string) (sym *Symbol, captured bool) {
	for s := ctx.scope; s != nil; s = s.parent {
		if found, ok := s.names[name]; ok {
			return found, false
		}
	}
	for p := ctx.parent; p != nil; p = p.parent {
		for s := p.scope; s != nil; s = s.parent {
			if found, ok := s.names[name]; ok {
				return found, true
			}
		}
	}
	if found, ok := c.globals[name]; ok {
		return found, false
	}
	return nil, false
}

func (c *checker) expect(ctx *funcCtx, e ast.Expr, want types.Type) *diag.Error {
	got, err := c.infer(ctx, e)
	if err != nil {
		return err
	}
	if !types.Equal(got, want) {
		return diag.Errorf(resultPos(e), "expected %s, found %s", want, got)
	}
	return nil
}

func resultPos(e ast.Expr) diag.Pos {
	if b, ok := e.(*ast.BlockExpr); ok {
		return resultPos(b.Result)
	}
	return e.Position()
}

func (c *checker) infer(ctx *funcCtx, e ast.Expr) (types.Type, *diag.Error) {
	typ, err := c.inferExpr(ctx, e)
	if err != nil {
		return nil, err
	}
	c.info.Types[e] = typ
	return typ, nil
}

func (c *checker) inferExpr(ctx *funcCtx, e ast.Expr) (types.Type, *diag.Error) {
	switch e := e.(type) {
	case *ast.IntLit:
		return types.Int, nil
	case *ast.BoolLit:
		return types.Bool, nil
	case *ast.Ident:
		return c.inferIdent(ctx, e)
	case *ast.UnaryExpr:
		return c.inferUnary(ctx, e)
	case *ast.BinaryExpr:
		return c.inferBinary(ctx, e)
	case *ast.CallExpr:
		return c.inferCall(ctx, e)
	case *ast.IfExpr:
		return c.inferIf(ctx, e)
	case *ast.BlockExpr:
		return c.inferBlock(ctx, e)
	case *ast.FuncLit:
		return c.inferFuncLit(ctx, e)
	default:
		return nil, diag.Errorf(e.Position(), "unhandled expression %T", e)
	}
}

func (c *checker) inferIdent(ctx *funcCtx, e *ast.Ident) (types.Type, *diag.Error) {
	sym, captured := c.lookup(ctx, e.Name)
	if sym == nil {
		return nil, diag.Errorf(e.Pos, "undefined name '%s'", e.Name)
	}
	if captured {
		return nil, diag.Errorf(e.Pos, "cannot capture '%s' in anonymous function: closures are not supported in v0.1", e.Name)
	}
	c.info.Uses[e] = sym
	return sym.Type, nil
}

func (c *checker) inferUnary(ctx *funcCtx, e *ast.UnaryExpr) (types.Type, *diag.Error) {
	switch e.Op {
	case token.Minus:
		if err := c.expect(ctx, e.X, types.Int); err != nil {
			return nil, err
		}
		return types.Int, nil
	case token.Bang:
		if err := c.expect(ctx, e.X, types.Bool); err != nil {
			return nil, err
		}
		return types.Bool, nil
	default:
		return nil, diag.Errorf(e.Pos, "unhandled unary operator %s", e.Op)
	}
}

func (c *checker) inferBinary(ctx *funcCtx, e *ast.BinaryExpr) (types.Type, *diag.Error) {
	switch e.Op {
	case token.Plus, token.Minus, token.Star, token.Slash:
		if err := c.expect(ctx, e.X, types.Int); err != nil {
			return nil, err
		}
		if err := c.expect(ctx, e.Y, types.Int); err != nil {
			return nil, err
		}
		return types.Int, nil
	case token.Lt, token.LtEq, token.Gt, token.GtEq:
		if err := c.expect(ctx, e.X, types.Int); err != nil {
			return nil, err
		}
		if err := c.expect(ctx, e.Y, types.Int); err != nil {
			return nil, err
		}
		return types.Bool, nil
	case token.Eq, token.NotEq:
		t, err := c.infer(ctx, e.X)
		if err != nil {
			return nil, err
		}
		if !types.Equal(t, types.Int) && !types.Equal(t, types.Bool) {
			return nil, diag.Errorf(resultPos(e.X), "cannot compare values of type %s", t)
		}
		if err := c.expect(ctx, e.Y, t); err != nil {
			return nil, err
		}
		return types.Bool, nil
	case token.AndAnd, token.OrOr:
		if err := c.expect(ctx, e.X, types.Bool); err != nil {
			return nil, err
		}
		if err := c.expect(ctx, e.Y, types.Bool); err != nil {
			return nil, err
		}
		return types.Bool, nil
	default:
		return nil, diag.Errorf(e.OpPos, "unhandled binary operator %s", e.Op)
	}
}

func (c *checker) inferCall(ctx *funcCtx, e *ast.CallExpr) (types.Type, *diag.Error) {
	ft, err := c.infer(ctx, e.Fn)
	if err != nil {
		return nil, err
	}
	sig, ok := ft.(*types.Func)
	if !ok {
		return nil, diag.Errorf(resultPos(e.Fn), "cannot call non-function value of type %s", ft)
	}
	if len(e.Args) != len(sig.Params) {
		return nil, diag.Errorf(e.LParen, "wrong number of arguments: expected %d, found %d", len(sig.Params), len(e.Args))
	}
	for i, arg := range e.Args {
		if err := c.expect(ctx, arg, sig.Params[i]); err != nil {
			return nil, err
		}
	}
	return sig.Result, nil
}

func (c *checker) inferIf(ctx *funcCtx, e *ast.IfExpr) (types.Type, *diag.Error) {
	if err := c.expect(ctx, e.Cond, types.Bool); err != nil {
		return nil, err
	}
	tt, err := c.infer(ctx, e.Then)
	if err != nil {
		return nil, err
	}
	et, err := c.infer(ctx, e.Else)
	if err != nil {
		return nil, err
	}
	if !types.Equal(tt, et) {
		return nil, diag.Errorf(resultPos(e.Else), "if branches have different types: %s and %s", tt, et)
	}
	return tt, nil
}

func (c *checker) inferBlock(ctx *funcCtx, e *ast.BlockExpr) (types.Type, *diag.Error) {
	c.pushScope(ctx)
	defer c.popScope(ctx)

	for _, l := range e.Lets {
		var t types.Type
		if l.Type != nil {
			want, err := c.resolveType(l.Type)
			if err != nil {
				return nil, err
			}
			if err := c.expect(ctx, l.Value, want); err != nil {
				return nil, err
			}
			t = want
		} else {
			var err *diag.Error
			t, err = c.infer(ctx, l.Value)
			if err != nil {
				return nil, err
			}
		}
		sym := &Symbol{
			Kind: SymLocal,
			Name: l.Name,
			Type: t,
			Pos:  l.NamePos,
		}
		c.info.Defs[l] = sym
		ctx.scope.names[l.Name] = sym
	}
	return c.infer(ctx, e.Result)
}

func (c *checker) inferFuncLit(ctx *funcCtx, e *ast.FuncLit) (types.Type, *diag.Error) {
	sig, err := c.resolveFuncType(e.Params, e.Result)
	if err != nil {
		return nil, err
	}
	c.info.FuncLits[e] = sig
	child := &funcCtx{parent: ctx}
	if err := c.checkFunc(child, e.Params, sig, e.Body); err != nil {
		return nil, err
	}
	return sig, nil
}
