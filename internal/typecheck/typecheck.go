// Package typecheck resolves names and type-checks Cero v0.1 programs.
package typecheck

import (
	"strings"

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
	SymLocal                   // let binding or variable pattern
	SymCtor                    // constructor
)

// Symbol is one declared name. Each declaration (every let, even when
// shadowing) gets its own *Symbol.
type Symbol struct {
	Kind SymbolKind
	Name string
	Type types.Type
	Pos  diag.Pos      // position of the declaring name
	Decl *ast.FuncDecl // set only for SymFunc
	Ctor *types.Ctor   // set only for SymCtor
	// TypeParams are the type parameters Type is generic over: the function's
	// own for a generic SymFunc, Ctor.Data.Params for a SymCtor, and nil
	// otherwise.
	TypeParams []*types.TypeParam
}

// Info records the result of a successful type check.
type Info struct {
	Types    map[ast.Expr]types.Type       // type of every expression node
	Uses     map[*ast.Ident]*Symbol        // symbol referenced by every Ident expression
	Defs     map[*ast.LetStmt]*Symbol      // symbol declared by every let
	Params   map[*ast.Param]*Symbol        // symbol declared by every parameter
	Funcs    map[*ast.FuncDecl]*Symbol     // symbol of every top-level function
	FuncLits map[*ast.FuncLit]*types.Func  // signature of every anonymous function
	Datas    map[*ast.TypeDecl]*types.Data // every type declaration
	CtorPats map[*ast.CtorPat]*types.Ctor  // constructor of every constructor pattern
	PatVars  map[*ast.VarPat]*Symbol       // symbol declared by every variable pattern
	// TypeArgs holds, for every Ident whose symbol has TypeParams, the type
	// arguments of that use in TypeParams order.
	TypeArgs map[*ast.Ident][]types.Type
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
			Datas:    make(map[*ast.TypeDecl]*types.Data),
			CtorPats: make(map[*ast.CtorPat]*types.Ctor),
			PatVars:  make(map[*ast.VarPat]*Symbol),
			TypeArgs: make(map[*ast.Ident][]types.Type),
		},
		globals: make(map[string]*Symbol),
		datas:   make(map[string]*types.Data),
		ctors:   make(map[string]*types.Ctor),
	}

	// Type names are registered before constructors so a field type can refer
	// to any declared type, including this one and ones declared later.
	for _, d := range file.Types {
		if d.Name == "Int" || d.Name == "Bool" {
			return nil, diag.Errorf(d.NamePos, "cannot redefine built-in type '%s'", d.Name)
		}
		if _, ok := c.datas[d.Name]; ok {
			return nil, diag.Errorf(d.NamePos, "duplicate type '%s'", d.Name)
		}
		data := &types.Data{Name: d.Name}
		c.datas[d.Name] = data
		c.info.Datas[d] = data
	}

	for _, d := range file.Types {
		params, err := c.declareTypeParams(d.TypeParams)
		if err != nil {
			return nil, err
		}
		c.datas[d.Name].Params = params
	}

	for _, d := range file.Types {
		data := c.datas[d.Name]
		c.tparams = scopeOf(data.Params)
		for i, cd := range d.Ctors {
			if _, ok := c.ctors[cd.Name]; ok {
				return nil, diag.Errorf(cd.Pos, "duplicate constructor '%s'", cd.Name)
			}
			fields := make([]types.Type, len(cd.Fields))
			for j, f := range cd.Fields {
				ft, err := c.resolveType(f)
				if err != nil {
					return nil, err
				}
				fields[j] = ft
			}
			ctor := &types.Ctor{
				Name:   cd.Name,
				Index:  i,
				Fields: fields,
				Data:   data,
			}
			data.Ctors = append(data.Ctors, ctor)
			c.ctors[cd.Name] = ctor
			c.globals[cd.Name] = &Symbol{
				Kind:       SymCtor,
				Name:       cd.Name,
				Type:       ctorType(ctor),
				Pos:        cd.Pos,
				Ctor:       ctor,
				TypeParams: data.Params,
			}
		}
		c.tparams = nil
	}

	sigs := make([]*types.Func, len(file.Funcs))
	for i, d := range file.Funcs {
		if sym := c.globals[d.Name]; sym != nil && sym.Kind == SymCtor {
			return nil, diag.Errorf(d.NamePos, "function '%s' conflicts with constructor '%s'", d.Name, d.Name)
		}
		if _, ok := c.globals[d.Name]; ok {
			return nil, diag.Errorf(d.NamePos, "duplicate function '%s'", d.Name)
		}
		tps, err := c.declareTypeParams(d.TypeParams)
		if err != nil {
			return nil, err
		}
		c.tparams = scopeOf(tps)
		sig, err := c.resolveFuncType(d.Params, d.Result)
		if err != nil {
			return nil, err
		}
		for j, tp := range tps {
			if !types.Mentions(sig, tp) {
				return nil, diag.Errorf(d.TypeParams[j].Pos, "type parameter '%s' is not used in the signature of '%s'", tp.Name, d.Name)
			}
		}
		c.tparams = nil
		sigs[i] = sig
		sym := &Symbol{
			Kind:       SymFunc,
			Name:       d.Name,
			Type:       sig,
			Pos:        d.NamePos,
			Decl:       d,
			TypeParams: tps,
		}
		c.globals[d.Name] = sym
		c.info.Funcs[d] = sym
	}

	for i, d := range file.Funcs {
		c.tparams = scopeOf(c.info.Funcs[d].TypeParams)
		c.metas = nil
		ctx := &funcCtx{}
		if err := c.checkFunc(ctx, d.Params, sigs[i], d.Body); err != nil {
			return nil, err
		}
		if err := c.checkSolved(); err != nil {
			return nil, err
		}
		c.tparams = nil
	}

	main, ok := c.globals["main"]
	if !ok {
		return nil, diag.Errorf(diag.Pos{Line: 1, Col: 1}, "missing function 'main'")
	}
	if len(main.TypeParams) > 0 {
		return nil, diag.Errorf(main.Pos, "function 'main' cannot have type parameters")
	}
	want := &types.Func{Result: types.Int}
	if !types.Equal(main.Type, want) {
		return nil, diag.Errorf(main.Pos, "function 'main' must have type () -> Int, found %s", main.Type)
	}
	c.resolveInfo()
	return c.info, nil
}

type checker struct {
	info    *Info
	globals map[string]*Symbol
	datas   map[string]*types.Data
	ctors   map[string]*types.Ctor
	tparams map[string]*types.TypeParam // type parameters in scope; nil outside generic declarations
	metas   []*pendingMeta              // created while checking the current top-level function, in creation order
}

// pendingMeta remembers where a unification variable was created so that an
// unsolved one can be reported.
type pendingMeta struct {
	meta  *types.Meta
	owner string   // name of the instantiated function or constructor
	pos   diag.Pos // position of the instantiating Ident
}

// scopeOf returns a name-to-parameter map, or nil when ps is empty.
func scopeOf(ps []*types.TypeParam) map[string]*types.TypeParam {
	if len(ps) == 0 {
		return nil
	}
	m := make(map[string]*types.TypeParam, len(ps))
	for _, p := range ps {
		m[p.Name] = p
	}
	return m
}

func (c *checker) declareTypeParams(ps []*ast.TypeParam) ([]*types.TypeParam, *diag.Error) {
	if len(ps) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(ps))
	out := make([]*types.TypeParam, 0, len(ps))
	for _, p := range ps {
		if p.Name == "Int" || p.Name == "Bool" || c.datas[p.Name] != nil {
			return nil, diag.Errorf(p.Pos, "type parameter '%s' conflicts with type '%s'", p.Name, p.Name)
		}
		if seen[p.Name] {
			return nil, diag.Errorf(p.Pos, "duplicate type parameter '%s'", p.Name)
		}
		seen[p.Name] = true
		out = append(out, &types.TypeParam{Name: p.Name})
	}
	return out, nil
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
		m := len(t.Args)
		if tp := c.tparams[t.Name]; tp != nil {
			if m != 0 {
				return nil, diag.Errorf(t.Pos, "wrong number of type arguments for '%s': expected 0, found %d", t.Name, m)
			}
			return tp, nil
		}
		if t.Name == "Int" || t.Name == "Bool" {
			if m != 0 {
				return nil, diag.Errorf(t.Pos, "wrong number of type arguments for '%s': expected 0, found %d", t.Name, m)
			}
			if t.Name == "Int" {
				return types.Int, nil
			}
			return types.Bool, nil
		}
		if data, ok := c.datas[t.Name]; ok {
			n := len(data.Params)
			if m != n {
				return nil, diag.Errorf(t.Pos, "wrong number of type arguments for '%s': expected %d, found %d", t.Name, n, m)
			}
			if n == 0 {
				return &types.Named{Data: data}, nil
			}
			args := make([]types.Type, m)
			for i, a := range t.Args {
				at, err := c.resolveType(a)
				if err != nil {
					return nil, err
				}
				args[i] = at
			}
			return &types.Named{Data: data, Args: args}, nil
		}
		return nil, diag.Errorf(t.Pos, "unknown type '%s'", t.Name)
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
		if err := c.checkBindable(p.Name, p.Pos); err != nil {
			return err
		}
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
	if !unify(want, got) {
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
	case *ast.MatchExpr:
		return c.inferMatch(ctx, e)
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
	if sym.Kind == SymCtor && len(sym.Ctor.Fields) > 0 {
		return nil, diag.Errorf(e.Pos, "constructor '%s' cannot be used as a value; call it with its fields", sym.Name)
	}
	c.info.Uses[e] = sym
	return c.instantiate(sym, e), nil
}

// instantiate returns sym.Type with sym.TypeParams replaced by fresh metas
// and records the metas in Info.TypeArgs[id]. For a symbol without type
// parameters it returns sym.Type and records nothing.
func (c *checker) instantiate(sym *Symbol, id *ast.Ident) types.Type {
	if len(sym.TypeParams) == 0 {
		return sym.Type
	}
	args := make([]types.Type, len(sym.TypeParams))
	for i, tp := range sym.TypeParams {
		m := &types.Meta{Name: tp.Name}
		c.metas = append(c.metas, &pendingMeta{meta: m, owner: sym.Name, pos: id.Pos})
		args[i] = m
	}
	c.info.TypeArgs[id] = args
	return types.Subst(sym.Type, sym.TypeParams, args)
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
	if id, ok := e.Fn.(*ast.Ident); ok {
		if sym, _ := c.lookup(ctx, id.Name); sym != nil && sym.Kind == SymCtor {
			return c.inferCtorCall(ctx, e, id, sym)
		}
	}
	ft, err := c.infer(ctx, e.Fn)
	if err != nil {
		return nil, err
	}
	sig, ok := types.Prune(ft).(*types.Func)
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
	if !unify(tt, et) {
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
		if err := c.checkBindable(l.Name, l.NamePos); err != nil {
			return nil, err
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

func ctorType(ctor *types.Ctor) types.Type {
	if len(ctor.Fields) == 0 {
		return types.SelfType(ctor.Data)
	}
	return &types.Func{Params: ctor.Fields, Result: types.SelfType(ctor.Data)}
}

func (c *checker) checkBindable(name string, pos diag.Pos) *diag.Error {
	if sym := c.globals[name]; sym != nil && sym.Kind == SymCtor {
		return diag.Errorf(pos, "cannot use constructor name '%s' as a variable", name)
	}
	return nil
}

func (c *checker) inferCtorCall(
	ctx *funcCtx,
	e *ast.CallExpr,
	id *ast.Ident,
	sym *Symbol,
) (types.Type, *diag.Error) {
	ctor := sym.Ctor
	c.info.Uses[id] = sym
	if len(ctor.Fields) == 0 {
		return nil, diag.Errorf(e.LParen, "constructor '%s' has no fields; write it without parentheses", ctor.Name)
	}
	if len(e.Args) != len(ctor.Fields) {
		return nil, diag.Errorf(e.LParen, "wrong number of arguments: expected %d, found %d", len(ctor.Fields), len(e.Args))
	}
	ft := c.instantiate(sym, id).(*types.Func)
	c.info.Types[e.Fn] = ft
	for i, arg := range e.Args {
		if err := c.expect(ctx, arg, ft.Params[i]); err != nil {
			return nil, err
		}
	}
	return ft.Result, nil
}

func (c *checker) inferMatch(ctx *funcCtx, m *ast.MatchExpr) (types.Type, *diag.Error) {
	st, err := c.infer(ctx, m.Scrutinee)
	if err != nil {
		return nil, err
	}
	switch types.Prune(st).(type) {
	case *types.Func:
		return nil, diag.Errorf(resultPos(m.Scrutinee), "cannot match on values of type %s", st)
	case *types.TypeParam:
		return nil, diag.Errorf(resultPos(m.Scrutinee), "cannot match on values of type %s", st)
	case *types.Meta:
		return nil, diag.Errorf(resultPos(m.Scrutinee), "cannot infer the type of the matched value; add a type annotation")
	}
	st = types.Prune(st)

	cov := newCoverage(st)
	var result types.Type
	for _, arm := range m.Arms {
		c.pushScope(ctx)
		t, err := c.inferMatchArm(ctx, arm, st, cov)
		c.popScope(ctx)
		if err != nil {
			return nil, err
		}
		if result == nil {
			result = t
		} else if !unify(result, t) {
			return nil, diag.Errorf(resultPos(arm.Body), "match arms have different types: %s and %s", result, t)
		}
	}
	if missing := cov.missing(); missing != "" {
		return nil, diag.Errorf(m.Pos, "non-exhaustive match: %s", missing)
	}
	return result, nil
}

func (c *checker) inferMatchArm(
	ctx *funcCtx,
	arm *ast.MatchArm,
	st types.Type,
	cov *coverage,
) (types.Type, *diag.Error) {
	if err := c.checkPattern(ctx, arm.Pattern, st); err != nil {
		return nil, err
	}
	if cov.covers(arm.Pattern, c.ctors) {
		return nil, diag.Errorf(arm.Pattern.Position(), "unreachable match arm")
	}
	cov.add(arm.Pattern, c.ctors)
	return c.infer(ctx, arm.Body)
}

func (c *checker) checkPattern(ctx *funcCtx, p ast.Pattern, st types.Type) *diag.Error {
	switch p := p.(type) {
	case *ast.WildcardPat:
		return nil
	case *ast.VarPat:
		if err := c.checkBindable(p.Name, p.Pos); err != nil {
			return err
		}
		c.bindPatVar(ctx, p, st)
		return nil
	case *ast.IntPat:
		if !types.Equal(st, types.Int) {
			return diag.Errorf(p.Pos, "expected %s, found Int", st)
		}
		return nil
	case *ast.BoolPat:
		if !types.Equal(st, types.Bool) {
			return diag.Errorf(p.Pos, "expected %s, found Bool", st)
		}
		return nil
	case *ast.CtorPat:
		ctor := c.ctors[p.Name]
		if ctor == nil {
			return diag.Errorf(p.Pos, "unknown constructor '%s'", p.Name)
		}
		named, ok := st.(*types.Named)
		if !ok || named.Data != ctor.Data {
			return diag.Errorf(p.Pos, "expected %s, found %s", st, dataDisplay(ctor.Data))
		}
		if len(p.Args) != len(ctor.Fields) {
			return diag.Errorf(p.Pos, "wrong number of fields in pattern '%s': expected %d, found %d", p.Name, len(ctor.Fields), len(p.Args))
		}
		seen := make(map[string]bool)
		for i, arg := range p.Args {
			vp, ok := arg.(*ast.VarPat)
			if !ok {
				continue
			}
			if seen[vp.Name] {
				return diag.Errorf(vp.Pos, "duplicate variable '%s' in pattern", vp.Name)
			}
			seen[vp.Name] = true
			if err := c.checkBindable(vp.Name, vp.Pos); err != nil {
				return err
			}
			field := types.Subst(ctor.Fields[i], ctor.Data.Params, named.Args)
			c.bindPatVar(ctx, vp, field)
		}
		c.info.CtorPats[p] = ctor
		return nil
	default:
		return diag.Errorf(p.Position(), "unhandled pattern %T", p)
	}
}

func (c *checker) bindPatVar(ctx *funcCtx, p *ast.VarPat, typ types.Type) {
	sym := &Symbol{
		Kind: SymLocal,
		Name: p.Name,
		Type: typ,
		Pos:  p.Pos,
	}
	c.info.PatVars[p] = sym
	ctx.scope.names[p.Name] = sym
}

// coverage records which values the arms of one match have already handled.
// Constructor-pattern arguments are only variables or '_', so a constructor
// pattern covers every value built with that constructor.
type coverage struct {
	scrutinee types.Type
	all       bool           // a catch-all arm has been seen
	ctors     map[int]bool   // constructor indices seen
	ints      map[int64]bool // integer literals seen
	bools     map[bool]bool  // boolean literals seen
}

func newCoverage(st types.Type) *coverage {
	return &coverage{
		scrutinee: st,
		ctors:     make(map[int]bool),
		ints:      make(map[int64]bool),
		bools:     make(map[bool]bool),
	}
}

// covers reports whether earlier arms already match every value p matches.
func (cov *coverage) covers(p ast.Pattern, ctors map[string]*types.Ctor) bool {
	if cov.all {
		return true
	}
	switch p := p.(type) {
	case *ast.WildcardPat, *ast.VarPat:
		return cov.catchAllCovered()
	case *ast.CtorPat:
		return cov.ctors[ctors[p.Name].Index]
	case *ast.IntPat:
		return cov.ints[p.Value]
	case *ast.BoolPat:
		return cov.bools[p.Value]
	default:
		return false
	}
}

func (cov *coverage) catchAllCovered() bool {
	switch st := cov.scrutinee.(type) {
	case types.BoolType:
		return cov.bools[true] && cov.bools[false]
	case *types.Named:
		for _, ctor := range st.Data.Ctors {
			if !cov.ctors[ctor.Index] {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (cov *coverage) add(p ast.Pattern, ctors map[string]*types.Ctor) {
	switch p := p.(type) {
	case *ast.WildcardPat, *ast.VarPat:
		cov.all = true
	case *ast.CtorPat:
		cov.ctors[ctors[p.Name].Index] = true
	case *ast.IntPat:
		cov.ints[p.Value] = true
	case *ast.BoolPat:
		cov.bools[p.Value] = true
	}
}

// missing describes values not yet covered. It is empty when the match is exhaustive.
func (cov *coverage) missing() string {
	if cov.all {
		return ""
	}
	switch st := cov.scrutinee.(type) {
	case *types.Named:
		var names []string
		for _, ctor := range st.Data.Ctors {
			if !cov.ctors[ctor.Index] {
				names = append(names, ctor.Name)
			}
		}
		return strings.Join(names, ", ")
	case types.BoolType:
		var names []string
		if !cov.bools[true] {
			names = append(names, "true")
		}
		if !cov.bools[false] {
			names = append(names, "false")
		}
		return strings.Join(names, ", ")
	case types.IntType:
		return "Int values need a '_' or variable pattern"
	default:
		return ""
	}
}

func (c *checker) checkSolved() *diag.Error {
	for _, pm := range c.metas {
		if _, ok := types.Prune(pm.meta).(*types.Meta); ok {
			return diag.Errorf(pm.pos, "cannot infer type argument '%s' of '%s'; add a type annotation", pm.meta.Name, pm.owner)
		}
	}
	return nil
}

func (c *checker) resolveInfo() {
	for e, t := range c.info.Types {
		c.info.Types[e] = types.Resolve(t)
	}
	for _, sym := range c.info.Defs {
		sym.Type = types.Resolve(sym.Type)
	}
	for _, sym := range c.info.PatVars {
		sym.Type = types.Resolve(sym.Type)
	}
	for _, args := range c.info.TypeArgs {
		for i := range args {
			args[i] = types.Resolve(args[i])
		}
	}
}

// dataDisplay formats d for an error message: its name, followed by
// "[?P1, ?P2, ...]" when it has parameters (e.g. "Option[?T]").
func dataDisplay(d *types.Data) string {
	if len(d.Params) == 0 {
		return d.Name
	}
	var b strings.Builder
	b.WriteString(d.Name)
	b.WriteByte('[')
	for i, p := range d.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteByte('?')
		b.WriteString(p.Name)
	}
	b.WriteByte(']')
	return b.String()
}
