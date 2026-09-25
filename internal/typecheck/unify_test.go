package typecheck

import (
	"testing"

	"github.com/deltama37/cero/internal/types"
)

func TestUnify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "int and int",
			run: func(t *testing.T) {
				if !unify(types.Int, types.Int) {
					t.Fatal("unify(Int, Int) = false")
				}
			},
		},
		{
			name: "int and bool",
			run: func(t *testing.T) {
				if unify(types.Int, types.Bool) {
					t.Fatal("unify(Int, Bool) = true")
				}
			},
		},
		{
			name: "same type parameter",
			run: func(t *testing.T) {
				tp := &types.TypeParam{Name: "T"}
				if !unify(tp, tp) {
					t.Fatal("unify(T, T) = false")
				}
			},
		},
		{
			name: "different type parameters",
			run: func(t *testing.T) {
				if unify(&types.TypeParam{Name: "T"}, &types.TypeParam{Name: "T"}) {
					t.Fatal("unify(T, other T) = true")
				}
			},
		},
		{
			name: "type parameter and int",
			run: func(t *testing.T) {
				if unify(&types.TypeParam{Name: "T"}, types.Int) {
					t.Fatal("unify(T, Int) = true")
				}
			},
		},
		{
			name: "meta and int",
			run: func(t *testing.T) {
				a := &types.Meta{Name: "a"}
				if !unify(a, types.Int) {
					t.Fatal("unify(?a, Int) = false")
				}
				if a.Solution != types.Int {
					t.Errorf("?a.Solution = %v, want Int", a.Solution)
				}
			},
		},
		{
			name: "int and meta",
			run: func(t *testing.T) {
				a := &types.Meta{Name: "a"}
				if !unify(types.Int, a) {
					t.Fatal("unify(Int, ?a) = false")
				}
				if a.Solution != types.Int {
					t.Errorf("?a.Solution = %v, want Int", a.Solution)
				}
			},
		},
		{
			name: "meta and itself",
			run: func(t *testing.T) {
				a := &types.Meta{Name: "a"}
				if !unify(a, a) {
					t.Fatal("unify(?a, ?a) = false")
				}
				if a.Solution != nil {
					t.Errorf("?a.Solution = %v, want nil", a.Solution)
				}
			},
		},
		{
			name: "two metas",
			run: func(t *testing.T) {
				a := &types.Meta{Name: "a"}
				b := &types.Meta{Name: "b"}
				if !unify(a, b) {
					t.Fatal("unify(?a, ?b) = false")
				}
				if a.Solution != b || b.Solution != nil {
					t.Errorf("?a.Solution = %v, ?b.Solution = %v", a.Solution, b.Solution)
				}
			},
		},
		{
			name: "function metas",
			run: func(t *testing.T) {
				a := &types.Meta{Name: "a"}
				b := &types.Meta{Name: "b"}
				left := &types.Func{Params: []types.Type{a}, Result: types.Int}
				right := &types.Func{Params: []types.Type{types.Bool}, Result: b}
				if !unify(left, right) {
					t.Fatal("unify(?a -> Int, Bool -> ?b) = false")
				}
				if a.Solution != types.Bool || b.Solution != types.Int {
					t.Errorf("?a.Solution = %v, ?b.Solution = %v", a.Solution, b.Solution)
				}
			},
		},
		{
			name: "different function arity",
			run: func(t *testing.T) {
				left := &types.Func{Params: []types.Type{types.Int, types.Int}, Result: types.Int}
				right := &types.Func{Params: []types.Type{types.Int}, Result: types.Int}
				if unify(left, right) {
					t.Fatal("unify((Int, Int) -> Int, Int -> Int) = true")
				}
			},
		},
		{
			name: "list meta",
			run: func(t *testing.T) {
				list := &types.Data{Name: "List"}
				a := &types.Meta{Name: "a"}
				left := &types.Named{Data: list, Args: []types.Type{a}}
				right := &types.Named{Data: list, Args: []types.Type{types.Bool}}
				if !unify(left, right) {
					t.Fatal("unify(List[?a], List[Bool]) = false")
				}
				if a.Solution != types.Bool {
					t.Errorf("?a.Solution = %v, want Bool", a.Solution)
				}
			},
		},
		{
			name: "different data types",
			run: func(t *testing.T) {
				left := &types.Named{Data: &types.Data{Name: "List"}, Args: []types.Type{types.Int}}
				right := &types.Named{Data: &types.Data{Name: "Option"}, Args: []types.Type{types.Int}}
				if unify(left, right) {
					t.Fatal("unify(List[Int], Option[Int]) = true")
				}
			},
		},
		{
			name: "occurs in function",
			run: func(t *testing.T) {
				a := &types.Meta{Name: "a"}
				right := &types.Func{Params: []types.Type{a}, Result: types.Int}
				if unify(a, right) {
					t.Fatal("unify(?a, ?a -> Int) = true")
				}
				if a.Solution != nil {
					t.Errorf("?a.Solution = %v, want nil", a.Solution)
				}
			},
		},
		{
			name: "occurs in named",
			run: func(t *testing.T) {
				a := &types.Meta{Name: "a"}
				list := &types.Data{Name: "List"}
				right := &types.Named{Data: list, Args: []types.Type{a}}
				if unify(a, right) {
					t.Fatal("unify(?a, List[?a]) = true")
				}
				if a.Solution != nil {
					t.Errorf("?a.Solution = %v, want nil", a.Solution)
				}
			},
		},
		{
			name: "solved meta and bool",
			run: func(t *testing.T) {
				a := &types.Meta{Name: "a", Solution: types.Int}
				if unify(a, types.Bool) {
					t.Fatal("unify(?a solved to Int, Bool) = true")
				}
			},
		},
		{
			name: "meta chain and int",
			run: func(t *testing.T) {
				b := &types.Meta{Name: "b"}
				a := &types.Meta{Name: "a", Solution: b}
				if !unify(a, types.Int) {
					t.Fatal("unify(?a solved to ?b, Int) = false")
				}
				if b.Solution != types.Int {
					t.Errorf("?b.Solution = %v, want Int", b.Solution)
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
