// Package ast defines the Cero v0.1 abstract syntax tree.
package ast

import (
	"github.com/deltama37/cero/internal/diag"
	"github.com/deltama37/cero/internal/token"
)

// File is a Cero source file. Imports, Types, and Funcs each keep source order.
type File struct {
	Imports []*Import
	Types   []*TypeDecl
	Funcs   []*FuncDecl
}

// Import is `import "path"`.
type Import struct {
	Pos     diag.Pos // 'import'
	Path    string
	PathPos diag.Pos // the string literal
}

// TypeParam declares one type parameter in "[T, U]". Name starts with an
// uppercase ASCII letter.
type TypeParam struct {
	Pos  diag.Pos
	Name string
}

// TypeDecl is a top-level algebraic data type declaration.
type TypeDecl struct {
	Pos        diag.Pos // 'type'
	Pub        bool     // declared with 'pub'
	Name       string
	NamePos    diag.Pos
	TypeParams []*TypeParam // nil when written without brackets
	Ctors      []*CtorDecl  // at least one
}

// CtorDecl is one constructor of a TypeDecl. Fields is empty for a
// constructor written without parentheses.
type CtorDecl struct {
	Pos    diag.Pos // name
	Name   string
	Fields []TypeExpr
}

// FuncDecl is a top-level function declaration.
type FuncDecl struct {
	Pos        diag.Pos // 'fn'
	Pub        bool     // declared with 'pub'
	Name       string
	NamePos    diag.Pos
	TypeParams []*TypeParam // nil when written without brackets
	Params     []*Param
	Result     TypeExpr // nil when omitted
	Body       *BlockExpr
}

// Param is a function parameter.
type Param struct {
	Pos  diag.Pos // name
	Name string
	Type TypeExpr // nil when omitted
}

// TypeExpr is a type written in source.
type TypeExpr interface {
	Position() diag.Pos
	typeExpr()
}

// NamedType is a type referred to by name, optionally applied to type
// arguments.
type NamedType struct {
	Pos  diag.Pos
	Name string     // "Int", "Bool", a type parameter, a declared type, or an unknown name (rejected by the type checker)
	Args []TypeExpr // type arguments in "Name[A, B]"; nil when written without brackets
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

// StringLit is a string literal. Value holds the decoded bytes.
type StringLit struct {
	Pos   diag.Pos
	Value string
}

// Position returns the position of the literal.
func (e *StringLit) Position() diag.Pos { return e.Pos }

func (*StringLit) expr() {}

// BoolLit is a boolean literal.
type BoolLit struct {
	Pos   diag.Pos
	Value bool
}

// Position returns the position of the literal.
func (e *BoolLit) Position() diag.Pos { return e.Pos }

func (*BoolLit) expr() {}

// UnitLit is `()`.
type UnitLit struct {
	Pos diag.Pos // '('
}

// Position returns the position of '('.
func (e *UnitLit) Position() diag.Pos { return e.Pos }

func (*UnitLit) expr() {}

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
	Op    token.Kind // Plus, PlusPlus, Minus, Star, Slash, Eq, NotEq, Lt, LtEq, Gt, GtEq, AndAnd, OrOr
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
	Result TypeExpr // nil when omitted
	Body   *BlockExpr
}

// Position returns the position of 'fn'.
func (e *FuncLit) Position() diag.Pos { return e.Pos }

func (*FuncLit) expr() {}

// MatchExpr is a match expression.
type MatchExpr struct {
	Pos       diag.Pos // 'match', or 'let' when Let is set
	Scrutinee Expr
	Arms      []*MatchArm // at least one
	Let       bool        // written as `let PATTERN = value` (ADR-0013)
}

// Position returns the position of 'match', or of 'let' when Let is set.
func (e *MatchExpr) Position() diag.Pos { return e.Pos }

func (*MatchExpr) expr() {}

// MatchArm is one "pattern => body" arm.
type MatchArm struct {
	Pattern Pattern
	Body    Expr
}

// Pattern is a pattern in a match arm.
type Pattern interface {
	Position() diag.Pos
	pattern()
}

// WildcardPat is '_'.
type WildcardPat struct {
	Pos diag.Pos
}

// Position returns the position of '_'.
func (p *WildcardPat) Position() diag.Pos { return p.Pos }

func (*WildcardPat) pattern() {}

// VarPat binds the matched value to Name. Name does not start with an
// uppercase ASCII letter.
type VarPat struct {
	Pos  diag.Pos
	Name string
}

// Position returns the position of the name.
func (p *VarPat) Position() diag.Pos { return p.Pos }

func (*VarPat) pattern() {}

// CtorPat matches values built with constructor Name. Name starts with an
// uppercase ASCII letter. Args is empty when written without parentheses;
// each element is any Pattern.
type CtorPat struct {
	Pos  diag.Pos // name
	Name string
	Args []Pattern
}

// Position returns the position of the constructor name.
func (p *CtorPat) Position() diag.Pos { return p.Pos }

func (*CtorPat) pattern() {}

// IntPat matches one non-negative integer.
type IntPat struct {
	Pos   diag.Pos
	Value int64
}

// Position returns the position of the literal.
func (p *IntPat) Position() diag.Pos { return p.Pos }

func (*IntPat) pattern() {}

// StrPat matches one string.
type StrPat struct {
	Pos   diag.Pos
	Value string
}

// Position returns the position of the literal.
func (p *StrPat) Position() diag.Pos { return p.Pos }

func (*StrPat) pattern() {}

// BoolPat matches true or false.
type BoolPat struct {
	Pos   diag.Pos
	Value bool
}

// Position returns the position of the literal.
func (p *BoolPat) Position() diag.Pos { return p.Pos }

func (*BoolPat) pattern() {}

var (
	_ TypeExpr = (*NamedType)(nil)
	_ TypeExpr = (*FuncType)(nil)

	_ Expr = (*IntLit)(nil)
	_ Expr = (*StringLit)(nil)
	_ Expr = (*BoolLit)(nil)
	_ Expr = (*UnitLit)(nil)
	_ Expr = (*Ident)(nil)
	_ Expr = (*UnaryExpr)(nil)
	_ Expr = (*BinaryExpr)(nil)
	_ Expr = (*CallExpr)(nil)
	_ Expr = (*IfExpr)(nil)
	_ Expr = (*BlockExpr)(nil)
	_ Expr = (*FuncLit)(nil)
	_ Expr = (*MatchExpr)(nil)

	_ Pattern = (*WildcardPat)(nil)
	_ Pattern = (*VarPat)(nil)
	_ Pattern = (*CtorPat)(nil)
	_ Pattern = (*IntPat)(nil)
	_ Pattern = (*StrPat)(nil)
	_ Pattern = (*BoolPat)(nil)
)
