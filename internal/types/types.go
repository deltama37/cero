// Package types defines the monomorphic types of Cero v0.1.
package types

import "strings"

// Type is a monomorphic Cero type.
type Type interface {
	String() string
	isType()
}

// IntType is the type of 64-bit signed integers.
type IntType struct{}

// BoolType is the type of booleans.
type BoolType struct{}

// Func is a function type. Function types are always represented as *Func.
type Func struct {
	Params []Type
	Result Type
}

// Int is the Int type.
var Int Type = IntType{}

// Bool is the Bool type.
var Bool Type = BoolType{}

var (
	_ Type = IntType{}
	_ Type = BoolType{}
	_ Type = (*Func)(nil)
)

// String returns "Int".
func (IntType) String() string { return "Int" }

func (IntType) isType() {}

// String returns "Bool".
func (BoolType) String() string { return "Bool" }

func (BoolType) isType() {}

// String formats a function type.
//
//	() -> R
//	P -> R                  (parenthesized when P is itself a function type)
//	(P1, P2, ...) -> R
//
// A function type in result position is not parenthesized: Int -> Int -> Int.
func (t *Func) String() string {
	var b strings.Builder
	switch len(t.Params) {
	case 0:
		b.WriteString("()")
	case 1:
		if _, ok := t.Params[0].(*Func); ok {
			b.WriteByte('(')
			b.WriteString(t.Params[0].String())
			b.WriteByte(')')
		} else {
			b.WriteString(t.Params[0].String())
		}
	default:
		b.WriteByte('(')
		for i, p := range t.Params {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(p.String())
		}
		b.WriteByte(')')
	}
	b.WriteString(" -> ")
	b.WriteString(t.Result.String())
	return b.String()
}

func (*Func) isType() {}

// Equal reports structural equality of a and b.
func Equal(a, b Type) bool {
	switch a := a.(type) {
	case IntType:
		_, ok := b.(IntType)
		return ok
	case BoolType:
		_, ok := b.(BoolType)
		return ok
	case *Func:
		fb, ok := b.(*Func)
		if !ok || len(a.Params) != len(fb.Params) {
			return false
		}
		for i := range a.Params {
			if !Equal(a.Params[i], fb.Params[i]) {
				return false
			}
		}
		return Equal(a.Result, fb.Result)
	default:
		return false
	}
}
