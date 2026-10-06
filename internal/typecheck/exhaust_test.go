package typecheck

import (
	"testing"

	"github.com/deltama37/cero/internal/types"
)

func TestUseful(t *testing.T) {
	t.Parallel()

	none := ctor("None", 0)
	some := ctor("Some", 1)
	data("Option", none, some)
	nilC := ctor("Nil", 0)
	cons := ctor("Cons", 2)
	data("List", nilC, cons)
	pair := ctor("Pair", 2)
	data("Pair", pair)

	w := spat{kind: spWild}
	someWild := spat{kind: spCtor, ctor: some, args: []spat{w}}
	some1 := spat{kind: spCtor, ctor: some, args: []spat{{kind: spInt, ival: 1}}}
	noneP := spat{kind: spCtor, ctor: none}
	nilP := spat{kind: spCtor, ctor: nilC}
	consNil := spat{kind: spCtor, ctor: cons, args: []spat{w, nilP}}
	consCons := spat{kind: spCtor, ctor: cons, args: []spat{w, {kind: spCtor, ctor: cons, args: []spat{w, w}}}}
	consWild := spat{kind: spCtor, ctor: cons, args: []spat{w, w}}
	tP := spat{kind: spBool, bval: true}
	fP := spat{kind: spBool, bval: false}
	pairTT := spat{kind: spCtor, ctor: pair, args: []spat{tP, tP}}
	pairTF := spat{kind: spCtor, ctor: pair, args: []spat{tP, fP}}
	pairFT := spat{kind: spCtor, ctor: pair, args: []spat{fP, tP}}
	pairFF := spat{kind: spCtor, ctor: pair, args: []spat{fP, fP}}

	tests := []struct {
		name string
		rows [][]spat
		q    []spat
		want bool
	}{
		{
			name: "empty matrix",
			q:    []spat{w},
			want: true,
		},
		{
			name: "after a wildcard",
			rows: col(w),
			q:    []spat{someWild},
			want: false,
		},
		{
			name: "different constructor",
			rows: col(noneP),
			q:    []spat{someWild},
			want: true,
		},
		{
			name: "same constructor",
			rows: col(someWild),
			q:    []spat{some1},
			want: false,
		},
		{
			name: "nested constructor not covered",
			rows: col(consNil),
			q:    []spat{consCons},
			want: true,
		},
		{
			name: "nested constructor covered",
			rows: col(nilP, consWild),
			q:    []spat{consNil},
			want: false,
		},
		{
			name: "booleans are complete",
			rows: col(tP, fP),
			q:    []spat{w},
			want: false,
		},
		{
			name: "integers are never complete",
			rows: col(spat{kind: spInt, ival: 0}, spat{kind: spInt, ival: 1}),
			q:    []spat{w},
			want: true,
		},
		{
			name: "strings are never complete",
			rows: col(spat{kind: spStr, sval: "a"}, spat{kind: spStr, sval: "b"}),
			q:    []spat{w},
			want: true,
		},
		{
			name: "same string",
			rows: col(spat{kind: spStr, sval: "a"}),
			q:    []spat{{kind: spStr, sval: "a"}},
			want: false,
		},
		{
			name: "different string",
			rows: col(spat{kind: spStr, sval: "a"}),
			q:    []spat{{kind: spStr, sval: "b"}},
			want: true,
		},
		{
			name: "one pair of booleans left",
			rows: col(pairTT, pairTF, pairFT),
			q:    []spat{pairFF},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := useful(tt.rows, tt.q); got != tt.want {
				t.Errorf("useful() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMissingMessage(t *testing.T) {
	t.Parallel()

	none := ctor("None", 0)
	some := ctor("Some", 1)
	data("Option", none, some)
	nilC := ctor("Nil", 0)
	cons := ctor("Cons", 2)
	data("List", nilC, cons)
	pair := ctor("Pair", 2)
	data("Pair", pair)
	circle := ctor("Circle", 1)
	square := ctor("Square", 2)
	dot := ctor("Dot", 0)
	data("Shape", circle, square, dot)

	w := spat{kind: spWild}
	tests := []struct {
		name string
		rows [][]spat
		want string
	}{
		{
			name: "exhaustive",
			rows: col(
				spat{kind: spCtor, ctor: none},
				spat{kind: spCtor, ctor: some, args: []spat{w}},
			),
			want: "",
		},
		{
			name: "missing leading constructors",
			rows: col(spat{kind: spCtor, ctor: dot}),
			want: "Circle(_), Square(_, _)",
		},
		{
			name: "boolean",
			rows: col(spat{kind: spBool, bval: true}),
			want: "false",
		},
		{
			name: "integer",
			rows: col(spat{kind: spInt, ival: 0}),
			want: "_",
		},
		{
			name: "string",
			rows: col(spat{kind: spStr, sval: "a"}),
			want: "_",
		},
		{
			name: "nested string",
			rows: col(
				spat{kind: spCtor, ctor: some, args: []spat{{kind: spStr, sval: "a"}}},
				spat{kind: spCtor, ctor: none},
			),
			want: "Some(_)",
		},
		{
			name: "nested constructor",
			rows: col(
				spat{kind: spCtor, ctor: nilC},
				spat{kind: spCtor, ctor: cons, args: []spat{w, spat{kind: spCtor, ctor: nilC}}},
			),
			want: "Cons(_, Cons(_, _))",
		},
		{
			name: "nested boolean",
			rows: col(
				spat{kind: spCtor, ctor: some, args: []spat{{kind: spBool, bval: true}}},
				spat{kind: spCtor, ctor: none},
			),
			want: "Some(false)",
		},
		{
			name: "nested integer",
			rows: col(
				spat{kind: spCtor, ctor: some, args: []spat{{kind: spInt, ival: 0}}},
				spat{kind: spCtor, ctor: none},
			),
			want: "Some(_)",
		},
		{
			name: "pair of booleans",
			rows: col(
				spat{kind: spCtor, ctor: pair, args: []spat{{kind: spBool, bval: true}, {kind: spBool, bval: true}}},
				spat{kind: spCtor, ctor: pair, args: []spat{{kind: spBool, bval: true}, {kind: spBool, bval: false}}},
				spat{kind: spCtor, ctor: pair, args: []spat{{kind: spBool, bval: false}, {kind: spBool, bval: true}}},
			),
			want: "Pair(false, false)",
		},
		{
			name: "wildcard suppresses the leading list",
			rows: col(
				spat{kind: spCtor, ctor: pair, args: []spat{{kind: spBool, bval: true}, w}},
				spat{kind: spCtor, ctor: pair, args: []spat{w, {kind: spBool, bval: true}}},
			),
			want: "Pair(false, false)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := missingMessage(tt.rows); got != tt.want {
				t.Errorf("missingMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatSpat(t *testing.T) {
	t.Parallel()

	nilC := ctor("Nil", 0)
	cons := ctor("Cons", 2)
	data("List", nilC, cons)
	some := ctor("Some", 1)
	data("Option", ctor("None", 0), some)
	pair := ctor("Pair", 2)
	data("Pair", pair)

	tests := []struct {
		name string
		p    spat
		want string
	}{
		{name: "wildcard", p: spat{kind: spWild}, want: "_"},
		{name: "int", p: spat{kind: spInt, ival: 42}, want: "42"},
		{name: "string", p: spat{kind: spStr, sval: "a\n"}, want: `"a\n"`},
		{name: "true", p: spat{kind: spBool, bval: true}, want: "true"},
		{name: "nil", p: spat{kind: spCtor, ctor: nilC}, want: "Nil"},
		{
			name: "cons",
			p: spat{kind: spCtor, ctor: cons, args: []spat{
				{kind: spWild},
				{kind: spCtor, ctor: nilC},
			}},
			want: "Cons(_, Nil)",
		},
		{
			name: "pair",
			p: spat{kind: spCtor, ctor: pair, args: []spat{
				{kind: spCtor, ctor: some, args: []spat{{kind: spWild}}},
				{kind: spBool, bval: false},
			}},
			want: "Pair(Some(_), false)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := formatSpat(tt.p); got != tt.want {
				t.Errorf("formatSpat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func ctor(name string, fields int) *types.Ctor {
	c := &types.Ctor{Name: name, Fields: make([]types.Type, fields)}
	for i := range c.Fields {
		c.Fields[i] = types.Int
	}
	return c
}

func data(name string, ctors ...*types.Ctor) *types.Data {
	d := &types.Data{Name: name, Ctors: ctors}
	for i, c := range ctors {
		c.Index = i
		c.Data = d
	}
	return d
}

func col(ps ...spat) [][]spat {
	rows := make([][]spat, len(ps))
	for i, p := range ps {
		rows[i] = []spat{p}
	}
	return rows
}
