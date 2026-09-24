package types

import "testing"

func TestString(t *testing.T) {
	t.Parallel()

	intToInt := &Func{Params: []Type{Int}, Result: Int}
	tests := []struct {
		name string
		typ  Type
		want string
	}{
		{name: "Int", typ: Int, want: "Int"},
		{name: "Bool", typ: Bool, want: "Bool"},
		{name: "Int to Int", typ: intToInt, want: "Int -> Int"},
		{
			name: "pair to Int",
			typ:  &Func{Params: []Type{Int, Bool}, Result: Int},
			want: "(Int, Bool) -> Int",
		},
		{
			name: "unit to Int",
			typ:  &Func{Result: Int},
			want: "() -> Int",
		},
		{
			name: "function param",
			typ:  &Func{Params: []Type{intToInt}, Result: Int},
			want: "(Int -> Int) -> Int",
		},
		{
			name: "right nested result",
			typ:  &Func{Params: []Type{Int}, Result: intToInt},
			want: "Int -> Int -> Int",
		},
		{
			name: "unit function param",
			typ: &Func{
				Params: []Type{&Func{Result: Int}},
				Result: Int,
			},
			want: "(() -> Int) -> Int",
		},
		{
			name: "function among params",
			typ: &Func{
				Params: []Type{Int, intToInt},
				Result: Bool,
			},
			want: "(Int, Int -> Int) -> Bool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.typ.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEqual(t *testing.T) {
	t.Parallel()

	nested := func() *Func {
		return &Func{
			Params: []Type{
				Int,
				&Func{Params: []Type{Bool}, Result: Int},
			},
			Result: &Func{Result: Bool},
		}
	}

	tests := []struct {
		name string
		a, b Type
		want bool
	}{
		{name: "Int", a: Int, b: Int, want: true},
		{name: "Bool", a: Bool, b: Bool, want: true},
		{name: "Int and Bool", a: Int, b: Bool, want: false},
		{name: "same function structure", a: nested(), b: nested(), want: true},
		{
			name: "different param count",
			a:    &Func{Result: Int},
			b:    &Func{Params: []Type{Int}, Result: Int},
			want: false,
		},
		{
			name: "different param types",
			a:    &Func{Params: []Type{Int}, Result: Int},
			b:    &Func{Params: []Type{Bool}, Result: Int},
			want: false,
		},
		{
			name: "different result",
			a:    &Func{Params: []Type{Int}, Result: Int},
			b:    &Func{Params: []Type{Int}, Result: Bool},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Equal(tt.a, tt.b); got != tt.want {
				t.Errorf("Equal(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
