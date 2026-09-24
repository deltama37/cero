// Package ast defines the Cero v0.1 abstract syntax tree.
package ast

import (
	"github.com/deltama37/cero/internal/diag"
	"github.com/deltama37/cero/internal/token"
)

// File is a Cero source file: a sequence of function declarations.
type File struct {
	Funcs []*FuncDecl
}

// FuncDecl is a top-level function declaration.
type FuncDecl struct {
	Pos     diag.Pos // 'fn'
	Name    string
	NamePos diag.Pos
	Params  []*Param
	Result  TypeExpr
	Body    *BlockExpr
}

// Param is a function parameter.
type Param struct {
	Pos  diag.Pos // name
	Name string
	Type TypeExpr
}

// TypeExpr is a type written in source.
type TypeExpr interface {
	Position() diag.Pos
	typeExpr()
}

// NamedType is a type referred to by name.
type NamedType struct {
	Pos  diag.Pos
	Name string // "Int", "Bool", or an unknown name (rejected by the type checker)
}

// Position returns the position of the type name.
func (t *NamedType) Position() diag.Pos { return t.Pos }

func (*NamedType) typeExpr() {}

// FuncType is a function type. -> is right-associative.
type FuncType struct {
	Pos    diag.Pos // first token of the type
	Params []TypeExpr
	Result TypeExpr
}

// Position returns the position of the first token of the type.
func (t *FuncType) Position() diag.Pos { return t.Pos }

func (*FuncType) typeExpr() {}

// Expr is an expression.
type Expr interface {
	Position() diag.Pos
	expr()
}

// IntLit is an integer literal.
type IntLit struct {
	Pos   diag.Pos
	Value int64
}

// Position returns the position of the literal.
func (e *IntLit) Position() diag.Pos { return e.Pos }

func (*IntLit) expr() {}

// BoolLit is a boolean literal.
type BoolLit struct {
	Pos   diag.Pos
	Value bool
}

// Position returns the position of the literal.
func (e *BoolLit) Position() diag.Pos { return e.Pos }

func (*BoolLit) expr() {}

// Ident is an identifier expression.
type Ident struct {
	Pos  diag.Pos
	Name string
}

// Position returns the position of the identifier.
func (e *Ident) Position() diag.Pos { return e.Pos }

func (*Ident) expr() {}

// UnaryExpr is a prefix operator expression.
type UnaryExpr struct {
	Pos diag.Pos   // operator
	Op  token.Kind // token.Minus or token.Bang
	X   Expr
}

// Position returns the position of the operator.
func (e *UnaryExpr) Position() diag.Pos { return e.Pos }

func (*UnaryExpr) expr() {}

// BinaryExpr is an infix operator expression.
type BinaryExpr struct {
	Pos   diag.Pos // X.Position()
	OpPos diag.Pos
	Op    token.Kind // Plus, Minus, Star, Slash, Eq, NotEq, Lt, LtEq, Gt, GtEq, AndAnd, OrOr
	X, Y  Expr
}

// Position returns the position of the left operand.
func (e *BinaryExpr) Position() diag.Pos { return e.Pos }

func (*BinaryExpr) expr() {}

// CallExpr is a function call.
type CallExpr struct {
	Pos    diag.Pos // Fn.Position()
	Fn     Expr
	LParen diag.Pos
	Args   []Expr
}

// Position returns the position of the callee.
func (e *CallExpr) Position() diag.Pos { return e.Pos }

func (*CallExpr) expr() {}

// IfExpr is a conditional expression. Else is a *BlockExpr or *IfExpr.
type IfExpr struct {
	Pos  diag.Pos // 'if'
	Cond Expr
	Then *BlockExpr
	Else Expr // *BlockExpr or *IfExpr
}

// Position returns the position of 'if'.
func (e *IfExpr) Position() diag.Pos { return e.Pos }

func (*IfExpr) expr() {}

// BlockExpr is a block expression.
type BlockExpr struct {
	Pos    diag.Pos // '{'
	Lets   []*LetStmt
	Result Expr
}

// Position returns the position of '{'.
func (e *BlockExpr) Position() diag.Pos { return e.Pos }

func (*BlockExpr) expr() {}

// LetStmt is a local binding. Type is nil when the annotation is omitted.
type LetStmt struct {
	Pos     diag.Pos // 'let'
	Name    string
	NamePos diag.Pos
	Type    TypeExpr // nil when omitted
	Value   Expr
}

// FuncLit is an anonymous function.
type FuncLit struct {
	Pos    diag.Pos // 'fn'
	Params []*Param
	Result TypeExpr
	Body   *BlockExpr
}

// Position returns the position of 'fn'.
func (e *FuncLit) Position() diag.Pos { return e.Pos }

func (*FuncLit) expr() {}

var (
	_ TypeExpr = (*NamedType)(nil)
	_ TypeExpr = (*FuncType)(nil)

	_ Expr = (*IntLit)(nil)
	_ Expr = (*BoolLit)(nil)
	_ Expr = (*Ident)(nil)
	_ Expr = (*UnaryExpr)(nil)
	_ Expr = (*BinaryExpr)(nil)
	_ Expr = (*CallExpr)(nil)
	_ Expr = (*IfExpr)(nil)
	_ Expr = (*BlockExpr)(nil)
	_ Expr = (*FuncLit)(nil)
)
