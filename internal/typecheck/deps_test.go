package typecheck

import (
	"fmt"
	"testing"

	"github.com/deltama37/cero/internal/parser"
)

func TestFuncRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "calls another function",
			src:  "fn f() { g(1) }\nfn g(x) { x }\n",
			want: []string{"g"},
		},
		{
			name: "calls itself",
			src:  "fn f(n) { f(n) }\n",
			want: []string{"f"},
		},
		{
			name: "same function twice",
			src:  "fn f() { g(g(1)) }\nfn g(x) { x }\n",
			want: []string{"g"},
		},
		{
			name: "parameter hides a function",
			src:  "fn f(g) { g(1) }\nfn g(x) { x }\n",
			want: nil,
		},
		{
			name: "let hides a function",
			src:  "fn f() { let g = 1 g }\nfn g() { 1 }\n",
			want: nil,
		},
		{
			name: "right-hand side of let sees the outer function",
			src:  "fn f() { let g = g g }\nfn g() { 1 }\n",
			want: []string{"g"},
		},
		{
			name: "anonymous function parameter hides a function",
			src:  "fn f() { fn(g) { g(1) } }\nfn g(x) { x }\n",
			want: nil,
		},
		{
			name: "name is visible outside an anonymous function",
			src:  "fn f() { let k = fn(g) { 1 } g }\nfn g() { 1 }\n",
			want: []string{"g"},
		},
		{
			name: "pattern variable hides a function in that arm only",
			src:  "fn f() { match 0 { g => 0, _ => g } }\nfn g() { 1 }\n",
			want: []string{"g"},
		},
		{
			name: "constructors are not functions",
			src: `type List[T] =
    | Nil
    | Cons(T, List[T])

fn f() { Cons(1, Nil) }
`,
			want: nil,
		},
		{
			name: "order of first occurrence",
			src:  "fn f() { h(g(1)) }\nfn g(x) { x }\nfn h(x) { x }\n",
			want: []string{"h", "g"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, err := parser.ParseFile([]byte(tt.src))
			if err != nil {
				t.Fatalf("ParseFile() error = %v", err)
			}
			if len(file.Funcs) == 0 {
				t.Fatal("no functions")
			}
			names := make(map[string]bool, len(file.Funcs))
			for _, d := range file.Funcs {
				names[d.Name] = true
			}
			got := funcRefs(file.Funcs[0].Params, file.Funcs[0].Body, func(name string) bool {
				return names[name]
			})
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("funcRefs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSCCs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		n     int
		edges [][]int
		want  [][]int
	}{
		{
			name: "no edges",
			n:    3,
			want: [][]int{{0}, {1}, {2}},
		},
		{
			name:  "dependency first",
			n:     3,
			edges: [][]int{{1}, {2}, nil},
			want:  [][]int{{2}, {1}, {0}},
		},
		{
			name:  "self loop",
			n:     1,
			edges: [][]int{{0}},
			want:  [][]int{{0}},
		},
		{
			name:  "mutual recursion",
			n:     2,
			edges: [][]int{{1}, {0}},
			want:  [][]int{{0, 1}},
		},
		{
			name:  "dependent after component",
			n:     3,
			edges: [][]int{{1}, {2}, {1}},
			want:  [][]int{{1, 2}, {0}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := sccs(tt.n, tt.edges)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("sccs() = %v, want %v", got, tt.want)
			}
		})
	}
}
