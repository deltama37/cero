package lower

import (
	"testing"

	"github.com/deltama37/cero/internal/ast"
	"github.com/deltama37/cero/internal/parser"
	"github.com/deltama37/cero/internal/typecheck"
)

func TestCaptures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		src   string
		fn    string
		index int
		want  []string
	}{
		{
			name: "no captures",
			src: `fn main() -> Int {
    let f = fn(x: Int) -> Int { x }
    f(1)
}
`,
			want: []string{},
		},
		{
			name: "captures a parameter",
			fn:   "f",
			src: `fn f(n: Int) -> Int {
    fn() -> Int { n }()
}
fn main() -> Int { f(1) }
`,
			want: []string{"n"},
		},
		{
			name: "captures a let",
			src: `fn main() -> Int {
    let k = 1
    fn() -> Int { k }()
}
`,
			want: []string{"k"},
		},
		{
			name: "captures a pattern variable",
			src: `fn main() -> Int {
    match 1 {
        x => fn() -> Int { x }(),
    }
}
`,
			want: []string{"x"},
		},
		{
			name: "ignores its own parameter, let, and pattern variable",
			src: `fn main() -> Int {
    fn(x: Int) -> Int {
        let k = 1
        match k {
            y => x + y,
        }
    }(1)
}
`,
			want: []string{},
		},
		{
			name: "first occurrence order without duplicates",
			src: `fn main() -> Int {
    let a = 1
    let b = 2
    fn() -> Int { b + a + b }()
}
`,
			want: []string{"b", "a"},
		},
		{
			name: "nested anonymous function propagates captures",
			src: `fn main() -> Int {
    let n = 1
    fn() -> Int {
        fn() -> Int { n }()
    }()
}
`,
			want: []string{"n"},
		},
		{
			name: "nested anonymous function capturing a parameter is not an outer capture",
			src: `fn main() -> Int {
    fn(n: Int) -> Int {
        fn() -> Int { n }()
    }(1)
}
`,
			want: []string{},
		},
		{
			name:  "generalized let propagates captures of its value",
			index: 1,
			src: `fn main() -> Int {
    let y = 1
    let id = fn(x) { y }
    fn() -> Int { id(1) }()
}
`,
			want: []string{"y"},
		},
		{
			name: "ignores top-level functions and constructors",
			src: `fn id(x: Int) -> Int { x }

type Color =
    | Red
    | Blue

fn main() -> Int {
    fn() -> Int {
        if id(1) == 1 {
            match Red {
                Red => 1,
                Blue => 0,
            }
        } else {
            0
        }
    }()
}
`,
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, err := parser.ParseFile([]byte(tt.src))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			info, err := typecheck.Check(file)
			if err != nil {
				t.Fatalf("typecheck: %v", err)
			}
			fnName := tt.fn
			if fnName == "" {
				fnName = "main"
			}
			var body *ast.BlockExpr
			for _, fn := range file.Funcs {
				if fn.Name == fnName {
					body = fn.Body
					break
				}
			}
			if body == nil {
				t.Fatalf("function %s not found", fnName)
			}
			lits := funcLits(body)
			if tt.index >= len(lits) {
				t.Fatalf("func lit index %d, found %d", tt.index, len(lits))
			}
			cs := &captureSet{
				info:     info,
				polyLets: polyLetsOf(info),
				memo:     make(map[*ast.FuncLit][]*typecheck.Symbol),
			}
			caps := cs.of(lits[tt.index])
			got := make([]string, len(caps))
			for i, s := range caps {
				got[i] = s.Name
			}
			if len(got) != len(tt.want) {
				t.Fatalf("captures = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("captures = %v, want %v", got, tt.want)
					break
				}
			}
		})
	}
}

func polyLetsOf(info *typecheck.Info) map[*typecheck.Symbol]*ast.LetStmt {
	out := make(map[*typecheck.Symbol]*ast.LetStmt)
	for stmt, sym := range info.Defs {
		if len(sym.TypeParams) > 0 {
			out[sym] = stmt
		}
	}
	return out
}

func funcLits(e ast.Expr) []*ast.FuncLit {
	var out []*ast.FuncLit
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		if e == nil {
			return
		}
		if lit, ok := e.(*ast.FuncLit); ok {
			out = append(out, lit)
		}
		switch e := e.(type) {
		case *ast.IntLit, *ast.BoolLit, *ast.Ident:
		case *ast.UnaryExpr:
			walk(e.X)
		case *ast.BinaryExpr:
			walk(e.X)
			walk(e.Y)
		case *ast.CallExpr:
			walk(e.Fn)
			for _, arg := range e.Args {
				walk(arg)
			}
		case *ast.IfExpr:
			walk(e.Cond)
			walk(e.Then)
			walk(e.Else)
		case *ast.BlockExpr:
			for _, let := range e.Lets {
				walk(let.Value)
			}
			walk(e.Result)
		case *ast.FuncLit:
			walk(e.Body)
		case *ast.MatchExpr:
			walk(e.Scrutinee)
			for _, arm := range e.Arms {
				walk(arm.Body)
			}
		default:
			panic("unhandled expression")
		}
	}
	walk(e)
	return out
}
