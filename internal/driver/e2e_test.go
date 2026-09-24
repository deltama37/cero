package driver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2E(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("wasmtime"); err != nil {
		t.Skip("wasmtime not found")
	}

	root := moduleRoot(t)
	tests := []struct {
		name string
		src  string
		file string
		want string
	}{
		{
			name: "fib",
			file: "examples/fib.cero",
			want: "55",
		},
		{
			name: "higher order",
			file: "examples/higher_order.cero",
			want: "42",
		},
		{
			name: "abs",
			src: `fn abs(n: Int) -> Int {
    if n < 0 {
        -n
    } else {
        n
    }
}

fn main() -> Int {
    abs(-7) + abs(3)
}
`,
			want: "10",
		},
		{
			name: "calc",
			src: `fn calc(x: Int) -> Int {
    let doubled = x * 2
    let added = doubled + 10
    added
}

fn main() -> Int {
    calc(5)
}
`,
			want: "20",
		},
		{
			name: "apply anonymous double",
			src: `fn apply(f: Int -> Int, x: Int) -> Int {
    f(x)
}

fn main() -> Int {
    let double = fn(x: Int) -> Int { x * 2 }
    apply(double, 21)
}
`,
			want: "42",
		},
		{
			name: "top-level function as a value",
			src: `fn inc(n: Int) -> Int {
    n + 1
}

fn apply(f: Int -> Int, x: Int) -> Int {
    f(x)
}

fn main() -> Int {
    apply(inc, 41)
}
`,
			want: "42",
		},
		{
			name: "pick true",
			src: `fn inc(x: Int) -> Int {
    x + 1
}

fn dec(x: Int) -> Int {
    x - 1
}

fn pick(b: Bool) -> Int -> Int {
    if b { inc } else { dec }
}

fn main() -> Int {
    pick(true)(3)
}
`,
			want: "4",
		},
		{
			name: "pick false",
			src: `fn inc(x: Int) -> Int {
    x + 1
}

fn dec(x: Int) -> Int {
    x - 1
}

fn pick(b: Bool) -> Int -> Int {
    if b { inc } else { dec }
}

fn main() -> Int {
    pick(false)(3)
}
`,
			want: "2",
		},
		{
			name: "mutual recursion",
			src: `fn isEven(n: Int) -> Bool {
    if n == 0 {
        true
    } else {
        isOdd(n - 1)
    }
}

fn isOdd(n: Int) -> Bool {
    if n == 0 {
        false
    } else {
        isEven(n - 1)
    }
}

fn main() -> Int {
    if isEven(10) { 1 } else { 0 }
}
`,
			want: "1",
		},
		{
			name: "and short-circuit",
			src: `fn main() -> Int {
    if false && 1 / 0 == 0 { 1 } else { 2 }
}
`,
			want: "2",
		},
		{
			name: "or short-circuit",
			src: `fn main() -> Int {
    if true || 1 / 0 == 0 { 9 } else { 8 }
}
`,
			want: "9",
		},
		{
			name: "else if chain",
			src: `fn main() -> Int {
    let n = 2
    if n == 0 {
        10
    } else if n == 1 {
        20
    } else if n == 2 {
        30
    } else {
        40
    }
}
`,
			want: "30",
		},
		{
			name: "unary minus and division",
			src: `fn main() -> Int {
    -7 / 2
}
`,
			want: "-3",
		},
		{
			name: "bool equality and not",
			src: `fn toInt(b: Bool) -> Int {
    if b { 1 } else { 0 }
}

fn main() -> Int {
    toInt(true == true) + toInt(false == true) + toInt(true != false) + toInt(!false) + toInt(!true)
}
`,
			want: "3",
		},
		{
			name: "shadowing",
			src: `fn main() -> Int {
    let x = 1
    let x = x + 10
    let x = x * 4
    x
}
`,
			want: "44",
		},
		{
			name: "int wraparound",
			src: `fn main() -> Int {
    9223372036854775807 + 1
}
`,
			want: "-9223372036854775808",
		},
		{
			name: "deep recursion",
			src: `fn sum(n: Int) -> Int {
    if n == 0 {
        0
    } else {
        n + sum(n - 1)
    }
}

fn main() -> Int {
    sum(10000)
}
`,
			want: "50005000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := []byte(tt.src)
			filename := "main.cero"
			if tt.file != "" {
				filename = tt.file
				var err error
				src, err = os.ReadFile(filepath.Join(root, tt.file))
				if err != nil {
					t.Fatalf("read %s: %v", tt.file, err)
				}
			}
			wasm, err := Compile(filename, src)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			path := filepath.Join(t.TempDir(), "main.wasm")
			if err := os.WriteFile(path, wasm, 0o644); err != nil {
				t.Fatalf("write wasm: %v", err)
			}
			cmd := exec.Command("wasmtime", "run", "--invoke", "main", path)
			out, err := cmd.Output()
			if err != nil {
				stderr := ""
				if ee, ok := err.(*exec.ExitError); ok {
					stderr = string(ee.Stderr)
				}
				t.Fatalf("wasmtime: %v\nstderr:\n%s", err, stderr)
			}
			if got := strings.TrimSpace(string(out)); got != tt.want {
				t.Errorf("stdout = %q, want %q", got, tt.want)
			}
		})
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
