// Package lower lowers a type-checked Cero AST to IR.
package lower

import (
	"fmt"

	"github.com/deltama37/cero/internal/ast"
	"github.com/deltama37/cero/internal/ir"
	"github.com/deltama37/cero/internal/token"
	"github.com/deltama37/cero/internal/typecheck"
	"github.com/deltama37/cero/internal/types"
)

// Lower converts a type-checked file into an IR module.
// It panics if info is inconsistent with file (a type checker bug).
func Lower(file *ast.File, info *typecheck.Info) *ir.Module {
	l := &lowerer{
		info:   info,
		module: &ir.Module{Funcs: make([]*ir.Func, 0, len(file.Funcs))},
		declID: make(map[*ast.FuncDecl]ir.FuncID, len(file.Funcs)),
	}

	foundMain := false
	for i, d := range file.Funcs {
		sym := info.Funcs[d]
		if sym == nil {
			panic(fmt.Sprintf("lower: missing symbol for function '%s'", d.Name))
		}
		id := ir.FuncID(i)
		l.module.Funcs = append(l.module.Funcs, &ir.Func{
			Name: d.Name,
			Sig:  l.sigOf(sym.Type),
		})
		l.declID[d] = id
		if d.Name == "main" {
			l.module.Main = id
			foundMain = true
		}
	}
	if !foundMain {
		panic("lower: missing function 'main'")
	}

	for _, d := range file.Funcs {
		l.lowerFunc(l.module.Funcs[l.declID[d]], d.Params, d.Body)
	}
	return l.module
}

type lowerer struct {
	info    *typecheck.Info
	module  *ir.Module
	declID  map[*ast.FuncDecl]ir.FuncID
	lambdas int
}

func (l *lowerer) lowerFunc(fn *ir.Func, params []*ast.Param, body *ast.BlockExpr) {
	locals := make(map[*typecheck.Symbol]ir.LocalID, len(params))
	fn.Locals = make([]ir.ValType, len(params))
	for i, p := range params {
		sym := l.info.Params[p]
		if sym == nil {
			panic(fmt.Sprintf("lower: missing parameter symbol '%s' at %s", p.Name, p.Pos))
		}
		locals[sym] = ir.LocalID(i)
		fn.Locals[i] = l.valType(sym.Type)
	}
	fn.Body = l.lowerExpr(fn, locals, body)
	if got, want := fn.Body.Type(), fn.Sig.Result; got != want {
		panic(fmt.Sprintf("lower: %s body has type %s, signature result is %s", fn.Name, got, want))
	}
	markTailCalls(fn.Body)
}

// markTailCalls sets Tail on every Call and CallIndirect in tail position
// of e (ADR-0005). e must be in tail position itself.
func markTailCalls(e ir.Expr) {
	switch e := e.(type) {
	case *ir.Call:
		e.Tail = true
	case *ir.CallIndirect:
		e.Tail = true
	case *ir.If:
		markTailCalls(e.Then)
		markTailCalls(e.Else)
	case *ir.Block:
		markTailCalls(e.Result)
	case *ir.SwitchTag:
		for _, c := range e.Cases {
			markTailCalls(c.Body)
		}
		if e.Default != nil {
			markTailCalls(e.Default)
		}
	}
}

func (l *lowerer) lowerExpr(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e ast.Expr,
) ir.Expr {
	result := l.convert(fn, locals, e)
	if got, want := result.Type(), l.valType(l.mustType(e)); got != want {
		panic(fmt.Sprintf("lower: %T at %s has IR type %s, type checker has %s", e, e.Position(), got, want))
	}
	return result
}

func (l *lowerer) convert(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e ast.Expr,
) ir.Expr {
	switch e := e.(type) {
	case *ast.IntLit:
		return &ir.IntConst{Value: e.Value}
	case *ast.BoolLit:
		return &ir.BoolConst{Value: e.Value}
	case *ast.Ident:
		return l.lowerIdent(e, locals)
	case *ast.UnaryExpr:
		return l.lowerUnary(fn, locals, e)
	case *ast.BinaryExpr:
		return l.lowerBinary(fn, locals, e)
	case *ast.CallExpr:
		return l.lowerCall(fn, locals, e)
	case *ast.IfExpr:
		return l.lowerIf(fn, locals, e)
	case *ast.BlockExpr:
		return l.lowerBlock(fn, locals, e)
	case *ast.FuncLit:
		return l.lowerFuncLit(e)
	case *ast.MatchExpr:
		return l.lowerMatch(fn, locals, e)
	default:
		panic(fmt.Sprintf("lower: unhandled expression %T", e))
	}
}

func (l *lowerer) lowerIdent(e *ast.Ident, locals map[*typecheck.Symbol]ir.LocalID) ir.Expr {
	sym := l.info.Uses[e]
	if sym == nil {
		panic(fmt.Sprintf("lower: missing symbol for '%s' at %s", e.Name, e.Pos))
	}
	switch sym.Kind {
	case typecheck.SymParam, typecheck.SymLocal:
		id, ok := locals[sym]
		if !ok {
			panic(fmt.Sprintf("lower: no local for '%s' at %s", e.Name, e.Pos))
		}
		return &ir.LocalGet{Local: id, T: l.valType(l.mustType(e))}
	case typecheck.SymFunc:
		id, ok := l.declID[sym.Decl]
		if !ok {
			panic(fmt.Sprintf("lower: no function id for '%s' at %s", e.Name, e.Pos))
		}
		l.addTable(id)
		return &ir.FuncValue{Func: id}
	case typecheck.SymCtor:
		if len(sym.Ctor.Fields) != 0 {
			panic(fmt.Sprintf("lower: constructor '%s' with fields used as a value at %s", e.Name, e.Pos))
		}
		return &ir.Construct{Tag: sym.Ctor.Index}
	default:
		panic(fmt.Sprintf("lower: unhandled symbol kind %d for '%s'", sym.Kind, e.Name))
	}
}

func (l *lowerer) lowerUnary(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e *ast.UnaryExpr,
) ir.Expr {
	x := l.lowerExpr(fn, locals, e.X)
	switch e.Op {
	case token.Minus:
		return &ir.Unary{Op: ir.Neg, X: x}
	case token.Bang:
		return &ir.Unary{Op: ir.Not, X: x}
	default:
		panic(fmt.Sprintf("lower: unhandled unary operator %s at %s", e.Op, e.Pos))
	}
}

func (l *lowerer) lowerBinary(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e *ast.BinaryExpr,
) ir.Expr {
	x := l.lowerExpr(fn, locals, e.X)
	switch e.Op {
	case token.AndAnd:
		return &ir.If{
			Cond: x,
			Then: l.lowerExpr(fn, locals, e.Y),
			Else: &ir.BoolConst{Value: false},
			T:    ir.Bool,
		}
	case token.OrOr:
		return &ir.If{
			Cond: x,
			Then: &ir.BoolConst{Value: true},
			Else: l.lowerExpr(fn, locals, e.Y),
			T:    ir.Bool,
		}
	}
	y := l.lowerExpr(fn, locals, e.Y)
	return &ir.Binary{Op: binOp(e), X: x, Y: y}
}

func (l *lowerer) lowerCall(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e *ast.CallExpr,
) ir.Expr {
	if id, ok := e.Fn.(*ast.Ident); ok {
		sym := l.info.Uses[id]
		if sym == nil {
			panic(fmt.Sprintf("lower: missing symbol for '%s' at %s", id.Name, id.Pos))
		}
		if sym.Kind == typecheck.SymCtor {
			return &ir.Construct{
				Tag:    sym.Ctor.Index,
				Fields: l.lowerArgs(fn, locals, e.Args),
			}
		}
		if sym.Kind == typecheck.SymFunc {
			fid, ok := l.declID[sym.Decl]
			if !ok {
				panic(fmt.Sprintf("lower: no function id for '%s' at %s", id.Name, id.Pos))
			}
			return &ir.Call{
				Func: fid,
				Args: l.lowerArgs(fn, locals, e.Args),
				T:    l.valType(l.mustType(e)),
			}
		}
	}
	ft, ok := l.mustType(e.Fn).(*types.Func)
	if !ok {
		panic(fmt.Sprintf("lower: callee type at %s is %s, want a function", e.Fn.Position(), l.mustType(e.Fn)))
	}
	return &ir.CallIndirect{
		Callee: l.lowerExpr(fn, locals, e.Fn),
		Sig:    l.sigOf(ft),
		Args:   l.lowerArgs(fn, locals, e.Args),
	}
}

func (l *lowerer) lowerIf(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e *ast.IfExpr,
) ir.Expr {
	return &ir.If{
		Cond: l.lowerExpr(fn, locals, e.Cond),
		Then: l.lowerExpr(fn, locals, e.Then),
		Else: l.lowerExpr(fn, locals, e.Else),
		T:    l.valType(l.mustType(e)),
	}
}

func (l *lowerer) lowerBlock(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e *ast.BlockExpr,
) ir.Expr {
	if len(e.Lets) == 0 {
		return l.lowerExpr(fn, locals, e.Result)
	}
	lets := make([]*ir.Let, len(e.Lets))
	for i, stmt := range e.Lets {
		value := l.lowerExpr(fn, locals, stmt.Value)
		sym := l.info.Defs[stmt]
		if sym == nil {
			panic(fmt.Sprintf("lower: missing symbol for let '%s' at %s", stmt.Name, stmt.NamePos))
		}
		id := ir.LocalID(len(fn.Locals))
		locals[sym] = id
		fn.Locals = append(fn.Locals, l.valType(sym.Type))
		lets[i] = &ir.Let{Local: id, Value: value}
	}
	return &ir.Block{
		Lets:   lets,
		Result: l.lowerExpr(fn, locals, e.Result),
	}
}

func (l *lowerer) lowerMatch(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e *ast.MatchExpr,
) ir.Expr {
	scrut := l.lowerExpr(fn, locals, e.Scrutinee)
	s := ir.LocalID(len(fn.Locals))
	fn.Locals = append(fn.Locals, l.valType(l.mustType(e.Scrutinee)))

	var result ir.Expr
	switch l.mustType(e.Scrutinee).(type) {
	case *types.Data:
		result = l.lowerDataMatch(fn, locals, e, s)
	case types.IntType, types.BoolType:
		result = l.lowerValueMatch(fn, locals, e, s)
	default:
		panic(fmt.Sprintf("lower: cannot match on %T at %s", l.mustType(e.Scrutinee), e.Pos))
	}
	return &ir.Block{
		Lets:   []*ir.Let{{Local: s, Value: scrut}},
		Result: result,
	}
}

func (l *lowerer) lowerDataMatch(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e *ast.MatchExpr,
	s ir.LocalID,
) ir.Expr {
	cases := make([]*ir.TagCase, 0, len(e.Arms))
	var def ir.Expr
	for _, arm := range e.Arms {
		cp, ok := arm.Pattern.(*ast.CtorPat)
		if !ok {
			// Wildcard or variable. Exhaustiveness puts this arm last.
			def = l.armBody(fn, locals, arm, s)
			continue
		}
		ctor := l.info.CtorPats[cp]
		if ctor == nil {
			panic(fmt.Sprintf("lower: missing constructor for pattern '%s' at %s", cp.Name, cp.Pos))
		}
		cases = append(cases, &ir.TagCase{
			Tag:  ctor.Index,
			Body: l.armBody(fn, locals, arm, s),
		})
	}
	return &ir.SwitchTag{
		Local:   s,
		Cases:   cases,
		Default: def,
		T:       l.valType(l.mustType(e)),
	}
}

func (l *lowerer) lowerValueMatch(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e *ast.MatchExpr,
	s ir.LocalID,
) ir.Expr {
	scrutType := fn.Locals[s]
	matchType := l.valType(l.mustType(e))
	var chain func(int) ir.Expr
	chain = func(i int) ir.Expr {
		arm := e.Arms[i]
		switch arm.Pattern.(type) {
		case *ast.WildcardPat, *ast.VarPat:
			return l.armBody(fn, locals, arm, s)
		}
		if i == len(e.Arms)-1 {
			// Exhaustive: this literal is the only value left.
			return l.armBody(fn, locals, arm, s)
		}
		then := l.armBody(fn, locals, arm, s)
		return &ir.If{
			Cond: &ir.Binary{
				Op: ir.Eq,
				X:  &ir.LocalGet{Local: s, T: scrutType},
				Y:  patLiteral(arm.Pattern),
			},
			Then: then,
			Else: chain(i + 1),
			T:    matchType,
		}
	}
	return chain(0)
}

// armBody allocates the locals the pattern binds, then lowers the arm body.
// A variable pattern that covers the whole scrutinee reuses s.
func (l *lowerer) armBody(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	arm *ast.MatchArm,
	s ir.LocalID,
) ir.Expr {
	var lets []*ir.Let
	switch pat := arm.Pattern.(type) {
	case *ast.WildcardPat, *ast.IntPat, *ast.BoolPat:
	case *ast.VarPat:
		sym := l.patVar(pat)
		locals[sym] = s
	case *ast.CtorPat:
		ctor := l.info.CtorPats[pat]
		if ctor == nil {
			panic(fmt.Sprintf("lower: missing constructor for pattern '%s' at %s", pat.Name, pat.Pos))
		}
		if len(pat.Args) != len(ctor.Fields) {
			panic(fmt.Sprintf("lower: pattern '%s' has %d fields, constructor has %d", pat.Name, len(pat.Args), len(ctor.Fields)))
		}
		for i, arg := range pat.Args {
			switch arg := arg.(type) {
			case *ast.WildcardPat:
			case *ast.VarPat:
				sym := l.patVar(arg)
				id := ir.LocalID(len(fn.Locals))
				locals[sym] = id
				fieldType := l.valType(ctor.Fields[i])
				fn.Locals = append(fn.Locals, fieldType)
				lets = append(lets, &ir.Let{
					Local: id,
					Value: &ir.Field{Local: s, Index: i, T: fieldType},
				})
			default:
				panic(fmt.Sprintf("lower: unhandled constructor pattern argument %T", arg))
			}
		}
	default:
		panic(fmt.Sprintf("lower: unhandled pattern %T", arm.Pattern))
	}
	body := l.lowerExpr(fn, locals, arm.Body)
	if len(lets) == 0 {
		return body
	}
	return &ir.Block{Lets: lets, Result: body}
}

func (l *lowerer) patVar(p *ast.VarPat) *typecheck.Symbol {
	sym := l.info.PatVars[p]
	if sym == nil {
		panic(fmt.Sprintf("lower: missing symbol for pattern '%s' at %s", p.Name, p.Pos))
	}
	return sym
}

func patLiteral(p ast.Pattern) ir.Expr {
	switch p := p.(type) {
	case *ast.IntPat:
		return &ir.IntConst{Value: p.Value}
	case *ast.BoolPat:
		return &ir.BoolConst{Value: p.Value}
	default:
		panic(fmt.Sprintf("lower: unhandled literal pattern %T", p))
	}
}

func (l *lowerer) lowerFuncLit(e *ast.FuncLit) ir.Expr {
	sig := l.info.FuncLits[e]
	if sig == nil {
		panic(fmt.Sprintf("lower: missing signature for anonymous function at %s", e.Pos))
	}
	id := ir.FuncID(len(l.module.Funcs))
	fn := &ir.Func{
		Name: fmt.Sprintf("lambda$%d", l.lambdas),
		Sig:  l.sigOf(sig),
	}
	l.lambdas++
	l.module.Funcs = append(l.module.Funcs, fn)
	l.lowerFunc(fn, e.Params, e.Body)
	l.addTable(id)
	return &ir.FuncValue{Func: id}
}

func (l *lowerer) lowerArgs(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	args []ast.Expr,
) []ir.Expr {
	out := make([]ir.Expr, len(args))
	for i, arg := range args {
		out[i] = l.lowerExpr(fn, locals, arg)
	}
	return out
}

// addTable appends id unless it is already present.
// Callers add an id when a FuncRef value is created, after nested
// expressions have been lowered, so earlier uses come first.
func (l *lowerer) addTable(id ir.FuncID) {
	for _, existing := range l.module.Table {
		if existing == id {
			return
		}
	}
	l.module.Table = append(l.module.Table, id)
}

func (l *lowerer) mustType(e ast.Expr) types.Type {
	t, ok := l.info.Types[e]
	if !ok || t == nil {
		panic(fmt.Sprintf("lower: missing type for %T at %s", e, e.Position()))
	}
	return t
}

func (l *lowerer) valType(t types.Type) ir.ValType {
	switch t.(type) {
	case types.IntType:
		return ir.Int
	case types.BoolType:
		return ir.Bool
	case *types.Func:
		return ir.FuncRef
	case *types.Data:
		return ir.Ptr
	default:
		panic(fmt.Sprintf("lower: unhandled type %T", t))
	}
}

func (l *lowerer) sigOf(t types.Type) ir.Sig {
	ft, ok := t.(*types.Func)
	if !ok {
		panic(fmt.Sprintf("lower: expected function type, found %T", t))
	}
	params := make([]ir.ValType, len(ft.Params))
	for i, p := range ft.Params {
		params[i] = l.valType(p)
	}
	return ir.Sig{Params: params, Result: l.valType(ft.Result)}
}

func binOp(e *ast.BinaryExpr) ir.BinOp {
	switch e.Op {
	case token.Plus:
		return ir.Add
	case token.Minus:
		return ir.Sub
	case token.Star:
		return ir.Mul
	case token.Slash:
		return ir.Div
	case token.Eq:
		return ir.Eq
	case token.NotEq:
		return ir.Ne
	case token.Lt:
		return ir.Lt
	case token.LtEq:
		return ir.Le
	case token.Gt:
		return ir.Gt
	case token.GtEq:
		return ir.Ge
	default:
		panic(fmt.Sprintf("lower: unhandled binary operator %s at %s", e.Op, e.OpPos))
	}
}
