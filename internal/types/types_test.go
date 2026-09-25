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
		{
			name: "named without arguments",
			typ:  &Named{Data: &Data{Name: "IntList"}},
			want: "IntList",
		},
		{
			name: "function of data type",
			typ:  &Func{Params: []Type{&Named{Data: &Data{Name: "IntList"}}}, Result: Int},
			want: "IntList -> Int",
		},
		{
			name: "option of int",
			typ: &Named{
				Data: &Data{Name: "Option", Params: []*TypeParam{{Name: "T"}}},
				Args: []Type{Int},
			},
			want: "Option[Int]",
		},
		{
			name: "pair of int and bool",
			typ: &Named{
				Data: &Data{Name: "Pair", Params: []*TypeParam{{Name: "A"}, {Name: "B"}}},
				Args: []Type{Int, Bool},
			},
			want: "Pair[Int, Bool]",
		},
		{
			name: "option of function",
			typ: &Named{
				Data: &Data{Name: "Option", Params: []*TypeParam{{Name: "T"}}},
				Args: []Type{&Func{Params: []Type{Int}, Result: Int}},
			},
			want: "Option[Int -> Int]",
		},
		{
			name: "list of list of int",
			typ: func() Type {
				list := &Data{Name: "List", Params: []*TypeParam{{Name: "T"}}}
				return &Named{Data: list, Args: []Type{&Named{Data: list, Args: []Type{Int}}}}
			}(),
			want: "List[List[Int]]",
		},
		{
			name: "function of option",
			typ: &Func{
				Params: []Type{&Named{
					Data: &Data{Name: "Option", Params: []*TypeParam{{Name: "T"}}},
					Args: []Type{Int},
				}},
				Result: Int,
			},
			want: "Option[Int] -> Int",
		},
		{
			name: "type parameter",
			typ:  &TypeParam{Name: "T"},
			want: "T",
		},
		{
			name: "function of type parameter",
			typ: func() Type {
				tp := &TypeParam{Name: "T"}
				return &Func{Params: []Type{tp}, Result: tp}
			}(),
			want: "T -> T",
		},
		{
			name: "unsolved meta",
			typ:  &Meta{Name: "T"},
			want: "?T",
		},
		{
			name: "meta solved to int",
			typ:  &Meta{Name: "T", Solution: Int},
			want: "Int",
		},
		{
			name: "meta solved to meta solved to bool",
			typ:  &Meta{Name: "a", Solution: &Meta{Name: "b", Solution: Bool}},
			want: "Bool",
		},
		{
			name: "function of meta solved to function",
			typ: &Func{
				Params: []Type{&Meta{Name: "a", Solution: &Func{Params: []Type{Int}, Result: Int}}},
				Result: Int,
			},
			want: "(Int -> Int) -> Int",
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
	listData := &Data{Name: "IntList"}
	list := &Named{Data: listData}
	listData.Ctors = []*Ctor{{
		Name:   "Cons",
		Index:  0,
		Fields: []Type{Int, list},
		Data:   listData,
	}}
	sameNameData := &Data{Name: "IntList"}
	sameName := &Named{Data: sameNameData}
	sameNameData.Ctors = []*Ctor{{
		Name:   "Cons",
		Index:  0,
		Fields: []Type{Int, sameName},
		Data:   sameNameData,
	}}
	opt := &Data{Name: "Option", Params: []*TypeParam{{Name: "T"}}}
	optInt := &Named{Data: opt, Args: []Type{Int}}
	optIntAgain := &Named{Data: opt, Args: []Type{Int}}
	optBool := &Named{Data: opt, Args: []Type{Bool}}
	otherOpt := &Data{Name: "Option", Params: []*TypeParam{{Name: "T"}}}
	otherOptInt := &Named{Data: otherOpt, Args: []Type{Int}}
	tp := &TypeParam{Name: "T"}
	tpAgain := &TypeParam{Name: "T"}
	solved := &Meta{Name: "a", Solution: Int}
	unsolved := &Meta{Name: "a"}
	unsolvedOther := &Meta{Name: "a"}
	recData := &Data{Name: "Rec"}
	recA := &Named{Data: recData}
	recB := &Named{Data: recData}
	recData.Ctors = []*Ctor{{
		Name:   "Rec",
		Index:  0,
		Fields: []Type{recA},
		Data:   recData,
	}}

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
		{name: "same data pointer", a: list, b: list, want: true},
		{name: "same data name different pointer", a: list, b: sameName, want: false},
		{name: "data and Int", a: list, b: Int, want: false},
		{name: "named same arguments", a: optInt, b: optIntAgain, want: true},
		{name: "named different arguments", a: optInt, b: optBool, want: false},
		{name: "named same name different data", a: optInt, b: otherOptInt, want: false},
		{name: "same type parameter", a: tp, b: tp, want: true},
		{name: "type parameters same name", a: tp, b: tpAgain, want: false},
		{name: "type parameter and Int", a: tp, b: Int, want: false},
		{name: "meta solved to Int", a: solved, b: Int, want: true},
		{name: "same unsolved meta", a: unsolved, b: unsolved, want: true},
		{name: "different unsolved metas", a: unsolved, b: unsolvedOther, want: false},
		{name: "recursive named terminates", a: recA, b: recB, want: true},
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

func TestPrune(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "unsolved meta",
			run: func(t *testing.T) {
				m := &Meta{Name: "T"}
				if got := Prune(m); got != m {
					t.Errorf("Prune(unsolved) = %s, want the meta itself", got)
				}
			},
		},
		{
			name: "chain to Int",
			run: func(t *testing.T) {
				b := &Meta{Name: "b", Solution: Int}
				a := &Meta{Name: "a", Solution: b}
				if got := Prune(a); got != Int {
					t.Errorf("Prune(chain) = %s, want Int", got)
				}
			},
		},
		{
			name: "Int",
			run: func(t *testing.T) {
				if got := Prune(Int); got != Int {
					t.Errorf("Prune(Int) = %s, want Int", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t)
		})
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "solved and unsolved metas",
			run: func(t *testing.T) {
				a := &Meta{Name: "a", Solution: Int}
				b := &Meta{Name: "b"}
				list := &Data{Name: "List"}
				named := &Named{Data: list, Args: []Type{b}}
				original := &Func{Params: []Type{a}, Result: named}

				got := Resolve(original)
				if got.String() != "Int -> List[?b]" {
					t.Errorf("Resolve() = %s, want Int -> List[?b]", got)
				}
				if hasSolvedMeta(got) {
					t.Errorf("Resolve() = %s, contains a solved meta", got)
				}
				if original.Params[0] != a || a.Solution != Int {
					t.Error("Resolve modified the original parameter")
				}
				if original.Result != named || named.Args[0] != b || b.Solution != nil {
					t.Error("Resolve modified the original result")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t)
		})
	}
}

func hasSolvedMeta(t Type) bool {
	switch t := t.(type) {
	case *Meta:
		return t.Solution != nil || hasSolvedMeta(t.Solution)
	case *Func:
		for _, p := range t.Params {
			if hasSolvedMeta(p) {
				return true
			}
		}
		return hasSolvedMeta(t.Result)
	case *Named:
		for _, a := range t.Args {
			if hasSolvedMeta(a) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func TestSubst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "replace T in function and named",
			run: func(t *testing.T) {
				tp := &TypeParam{Name: "T"}
				list := &Data{Name: "List", Params: []*TypeParam{tp}}
				typ := &Func{
					Params: []Type{tp},
					Result: &Named{Data: list, Args: []Type{tp}},
				}
				got := Subst(typ, []*TypeParam{tp}, []Type{Int})
				if got.String() != "Int -> List[Int]" {
					t.Errorf("Subst() = %s, want Int -> List[Int]", got)
				}
			},
		},
		{
			name: "leave a different type parameter",
			run: func(t *testing.T) {
				tp := &TypeParam{Name: "T"}
				u := &TypeParam{Name: "U"}
				typ := &Func{Params: []Type{tp}, Result: u}
				got := Subst(typ, []*TypeParam{tp}, []Type{Int})
				fn, ok := got.(*Func)
				if !ok || fn.Result != u || fn.String() != "Int -> U" {
					t.Errorf("Subst() = %s, want Int -> U with the same U", got)
				}
			},
		},
		{
			name: "empty params returns the same value",
			run: func(t *testing.T) {
				typ := &Func{Params: []Type{Int}, Result: Bool}
				if got := Subst(typ, nil, nil); got != typ {
					t.Errorf("Subst() = %s, want the same value", got)
				}
			},
		},
		{
			name: "count mismatch panics",
			run: func(t *testing.T) {
				defer func() {
					if recover() == nil {
						t.Fatal("Subst did not panic")
					}
				}()
				Subst(Int, []*TypeParam{{Name: "T"}}, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t)
		})
	}
}

func TestMentions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "type parameter in function parameter",
			run: func(t *testing.T) {
				tp := &TypeParam{Name: "T"}
				typ := &Func{Params: []Type{tp}, Result: Int}
				if !Mentions(typ, tp) {
					t.Error("Mentions(param) = false, want true")
				}
			},
		},
		{
			name: "type parameter in function result",
			run: func(t *testing.T) {
				tp := &TypeParam{Name: "T"}
				typ := &Func{Params: []Type{Int}, Result: tp}
				if !Mentions(typ, tp) {
					t.Error("Mentions(result) = false, want true")
				}
			},
		},
		{
			name: "type parameter in named argument",
			run: func(t *testing.T) {
				tp := &TypeParam{Name: "T"}
				list := &Data{Name: "List"}
				typ := &Named{Data: list, Args: []Type{tp}}
				if !Mentions(typ, tp) {
					t.Error("Mentions(named) = false, want true")
				}
			},
		},
		{
			name: "meta through a solution chain",
			run: func(t *testing.T) {
				target := &Meta{Name: "c"}
				mid := &Meta{Name: "b", Solution: target}
				outer := &Meta{Name: "a", Solution: mid}
				if !Mentions(outer, target) {
					t.Error("Mentions(chain) = false, want true")
				}
			},
		},
		{
			name: "absent type parameter",
			run: func(t *testing.T) {
				tp := &TypeParam{Name: "T"}
				other := &TypeParam{Name: "T"}
				list := &Data{Name: "List"}
				typ := &Func{
					Params: []Type{Int, &Named{Data: list, Args: []Type{other}}},
					Result: Bool,
				}
				if Mentions(typ, tp) {
					t.Error("Mentions(absent) = true, want false")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t)
		})
	}
}

func TestSelfType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "option",
			run: func(t *testing.T) {
				tp := &TypeParam{Name: "T"}
				data := &Data{Name: "Option", Params: []*TypeParam{tp}}
				got := SelfType(data)
				if got.String() != "Option[T]" {
					t.Errorf("SelfType() = %s, want Option[T]", got)
				}
				if len(got.Args) != 1 || got.Args[0] != data.Params[0] {
					t.Errorf("SelfType().Args[0] = %v, want Params[0]", got.Args)
				}
			},
		},
		{
			name: "no parameters",
			run: func(t *testing.T) {
				data := &Data{Name: "Unit"}
				got := SelfType(data)
				if got.Args != nil {
					t.Errorf("SelfType().Args = %v, want nil", got.Args)
				}
				if got.String() != "Unit" {
					t.Errorf("SelfType() = %s, want Unit", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t)
		})
	}
}
