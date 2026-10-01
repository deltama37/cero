// Package types defines the types of Cero.
package types

import "strings"

// Type is a Cero type.
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

// Data is a declared algebraic data type. It is not a Type: a use of the
// type is a *Named that points to it. Each type declaration has exactly one
// *Data.
type Data struct {
	Name   string
	Params []*TypeParam // declaration order; nil when the type has no parameters
	Ctors  []*Ctor      // declaration order
}

// Ctor is one constructor of a Data type. Fields may refer to Data.Params.
type Ctor struct {
	Name   string
	Index  int // position in Data.Ctors; used as the runtime tag
	Fields []Type
	Data   *Data
}

// Named is a declared data type applied to type arguments.
// len(Args) == len(Data.Params), and Args is nil when Data has no parameters.
type Named struct {
	Data *Data
	Args []Type
}

// TypeParam is a rigid type variable declared in "[T, U]" of a function or
// type declaration. Each declaration gets its own *TypeParam, and a type
// parameter is equal only to itself.
type TypeParam struct {
	Name string
}

// Meta is a unification variable. The type checker creates one per type
// parameter each time it instantiates a generic name. Solution is nil until
// unification assigns a type to it.
type Meta struct {
	Name     string // name of the type parameter it stands for; used by String
	Solution Type
}

// Int is the Int type.
var Int Type = IntType{}

// Bool is the Bool type.
var Bool Type = BoolType{}

var (
	_ Type = IntType{}
	_ Type = BoolType{}
	_ Type = (*Func)(nil)
	_ Type = (*Named)(nil)
	_ Type = (*TypeParam)(nil)
	_ Type = (*Meta)(nil)
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
		if _, ok := Prune(t.Params[0]).(*Func); ok {
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

// String returns Data.Name, or Name[A1, A2, ...] when there are type arguments.
func (t *Named) String() string {
	if len(t.Args) == 0 {
		return t.Data.Name
	}
	var b strings.Builder
	b.WriteString(t.Data.Name)
	b.WriteByte('[')
	for i, a := range t.Args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(a.String())
	}
	b.WriteByte(']')
	return b.String()
}

func (*Named) isType() {}

// String returns the type parameter's name.
func (t *TypeParam) String() string { return t.Name }

func (*TypeParam) isType() {}

// String returns the solution's string, or "?" plus the name when unsolved.
func (t *Meta) String() string {
	if t.Solution != nil {
		return t.Solution.String()
	}
	return "?" + t.Name
}

func (*Meta) isType() {}

// Prune follows solved metas starting at t and returns the first type that
// is not a solved *Meta. It returns t itself when t is not a solved *Meta.
func Prune(t Type) Type {
	for {
		m, ok := t.(*Meta)
		if !ok || m.Solution == nil {
			return t
		}
		t = m.Solution
	}
}

// Resolve returns t with every solved *Meta replaced by its resolved
// solution. Unsolved metas are kept. It builds new *Func and *Named values
// where needed and never modifies t.
func Resolve(t Type) Type {
	t = Prune(t)
	switch t := t.(type) {
	case *Func:
		params := make([]Type, len(t.Params))
		for i, p := range t.Params {
			params[i] = Resolve(p)
		}
		return &Func{Params: params, Result: Resolve(t.Result)}
	case *Named:
		if len(t.Args) == 0 {
			return t
		}
		args := make([]Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = Resolve(a)
		}
		return &Named{Data: t.Data, Args: args}
	default:
		return t
	}
}

// Subst returns t with each params[i] replaced by args[i]. Solved metas are
// followed. It panics when len(params) != len(args). When params is empty it
// returns t unchanged.
func Subst(t Type, params []*TypeParam, args []Type) Type {
	if len(params) != len(args) {
		panic("types.Subst: parameter and argument counts differ")
	}
	if len(params) == 0 {
		return t
	}
	t = Prune(t)
	switch t := t.(type) {
	case *TypeParam:
		for i, p := range params {
			if t == p {
				return args[i]
			}
		}
		return t
	case *Func:
		ps := make([]Type, len(t.Params))
		for i, p := range t.Params {
			ps[i] = Subst(p, params, args)
		}
		return &Func{Params: ps, Result: Subst(t.Result, params, args)}
	case *Named:
		if len(t.Args) == 0 {
			return t
		}
		as := make([]Type, len(t.Args))
		for i, a := range t.Args {
			as[i] = Subst(a, params, args)
		}
		return &Named{Data: t.Data, Args: as}
	default:
		return t
	}
}

// Mentions reports whether v, a *TypeParam or *Meta, occurs in t, comparing
// by pointer and following solved metas.
func Mentions(t Type, v Type) bool {
	t = Prune(t)
	if t == v {
		return true
	}
	switch t := t.(type) {
	case *Func:
		for _, p := range t.Params {
			if Mentions(p, v) {
				return true
			}
		}
		return Mentions(t.Result, v)
	case *Named:
		for _, a := range t.Args {
			if Mentions(a, v) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// SelfType returns d applied to its own parameters, e.g. Option[T] for
// "type Option[T]". Args is nil when d has no parameters.
func SelfType(d *Data) *Named {
	if len(d.Params) == 0 {
		return &Named{Data: d}
	}
	args := make([]Type, len(d.Params))
	for i, p := range d.Params {
		args[i] = p
	}
	return &Named{Data: d, Args: args}
}

// Equal reports structural equality of a and b.
// Named types are nominal: they are equal only when they point at the same
// Data and their type arguments are equal, so a recursive data type does not
// make Equal recurse forever. Solved metas are followed.
func Equal(a, b Type) bool {
	a, b = Prune(a), Prune(b)
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
	case *Named:
		nb, ok := b.(*Named)
		if !ok || a.Data != nb.Data || len(a.Args) != len(nb.Args) {
			return false
		}
		for i := range a.Args {
			if !Equal(a.Args[i], nb.Args[i]) {
				return false
			}
		}
		return true
	case *TypeParam:
		tb, ok := b.(*TypeParam)
		return ok && a == tb
	case *Meta:
		mb, ok := b.(*Meta)
		return ok && a == mb
	default:
		return false
	}
}
