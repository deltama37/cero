// Package ir defines the Cero v0.1 intermediate representation.
package ir

import "fmt"

// ValType is the type of an IR value.
type ValType int

const (
	Int     ValType = iota // 64-bit signed integer
	Bool                   // boolean
	FuncRef                // reference to a function (see Module.Table)
)

// String returns "Int", "Bool" or "FuncRef".
func (t ValType) String() string {
	switch t {
	case Int:
		return "Int"
	case Bool:
		return "Bool"
	case FuncRef:
		return "FuncRef"
	default:
		panic(fmt.Sprintf("ir.ValType.String: unknown type %d", int(t)))
	}
}

// Sig is a function signature in IR types.
type Sig struct {
	Params []ValType
	Result ValType
}

// FuncID is an index into Module.Funcs.
type FuncID int

// LocalID is an index into Func.Locals.
type LocalID int

// Module is a lowered compilation unit.
type Module struct {
	Funcs []*Func
	// Table lists every function used as a value, without duplicates,
	// in order of first use. A FuncRef value is an index into Table.
	Table []FuncID
	Main  FuncID
}

// Func is a top-level function, including lifted anonymous functions.
type Func struct {
	Name string // declared name, or "lambda$N" for the N-th (0-based) lifted anonymous function
	Sig  Sig
	// Locals holds the type of every local. The first len(Sig.Params)
	// entries are the parameters.
	Locals []ValType
	Body   Expr
}

// Expr is an IR expression. Every implementation is a pointer type.
type Expr interface {
	Type() ValType
	isExpr()
}

// IntConst is a 64-bit signed integer literal.
type IntConst struct{ Value int64 }

// BoolConst is a boolean literal.
type BoolConst struct{ Value bool }

// LocalGet reads a local variable.
type LocalGet struct {
	Local LocalID
	T     ValType
}

// FuncValue produces a FuncRef value for Func. Func must appear in Module.Table.
type FuncValue struct{ Func FuncID }

// UnOp is a unary operator.
type UnOp int

const (
	Neg UnOp = iota // Int -> Int
	Not             // Bool -> Bool
)

// Unary is a unary operation.
type Unary struct {
	Op UnOp
	X  Expr
}

// BinOp is a binary operator.
type BinOp int

const (
	Add BinOp = iota // Int, Int -> Int
	Sub
	Mul
	Div
	Eq // Int, Int -> Bool  or  Bool, Bool -> Bool
	Ne
	Lt // Int, Int -> Bool
	Le
	Gt
	Ge
)

// Binary is a binary operation.
type Binary struct {
	Op   BinOp
	X, Y Expr
}

// If is a conditional expression.
type If struct {
	Cond       Expr
	Then, Else Expr
	T          ValType
}

// Let evaluates Value and stores it into Local. Let is not an Expr.
type Let struct {
	Local LocalID
	Value Expr
}

// Block runs Lets in order and yields Result.
type Block struct {
	Lets   []*Let
	Result Expr
}

// Call calls a function directly.
type Call struct {
	Func FuncID
	Args []Expr
	T    ValType
}

// CallIndirect calls the FuncRef value Callee, whose signature is Sig.
type CallIndirect struct {
	Callee Expr
	Sig    Sig
	Args   []Expr
}

// Type returns Int.
func (*IntConst) Type() ValType { return Int }

// Type returns Bool.
func (*BoolConst) Type() ValType { return Bool }

// Type returns the local's type.
func (e *LocalGet) Type() ValType { return e.T }

// Type returns FuncRef.
func (*FuncValue) Type() ValType { return FuncRef }

// Type returns Int for Neg and Bool for Not.
func (e *Unary) Type() ValType {
	switch e.Op {
	case Neg:
		return Int
	case Not:
		return Bool
	default:
		panic(fmt.Sprintf("ir.Unary.Type: unknown operator %d", int(e.Op)))
	}
}

// Type returns Int for Add, Sub, Mul and Div, and Bool for every other operator.
func (e *Binary) Type() ValType {
	switch e.Op {
	case Add, Sub, Mul, Div:
		return Int
	default:
		return Bool
	}
}

// Type returns the branch type.
func (e *If) Type() ValType { return e.T }

// Type returns the type of Result.
func (e *Block) Type() ValType { return e.Result.Type() }

// Type returns the call's result type.
func (e *Call) Type() ValType { return e.T }

// Type returns Sig.Result.
func (e *CallIndirect) Type() ValType { return e.Sig.Result }

func (*IntConst) isExpr()     {}
func (*BoolConst) isExpr()    {}
func (*LocalGet) isExpr()     {}
func (*FuncValue) isExpr()    {}
func (*Unary) isExpr()        {}
func (*Binary) isExpr()       {}
func (*If) isExpr()           {}
func (*Block) isExpr()        {}
func (*Call) isExpr()         {}
func (*CallIndirect) isExpr() {}

var (
	_ Expr = (*IntConst)(nil)
	_ Expr = (*BoolConst)(nil)
	_ Expr = (*LocalGet)(nil)
	_ Expr = (*FuncValue)(nil)
	_ Expr = (*Unary)(nil)
	_ Expr = (*Binary)(nil)
	_ Expr = (*If)(nil)
	_ Expr = (*Block)(nil)
	_ Expr = (*Call)(nil)
	_ Expr = (*CallIndirect)(nil)
)
