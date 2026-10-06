package driver

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/deltama37/cero/internal/diag"
)

func TestCompileError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "syntax error",
			src:  "fn",
			want: "main.cero:1:3: expected identifier, found end of file",
		},
		{
			name: "type error",
			src:  "fn main() -> Int { missing }",
			want: "main.cero:1:20: undefined name 'missing'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Compile("main.cero", []byte(tt.src))
			var d *diag.Error
			if !errors.As(err, &d) {
				t.Fatalf("Compile() error = %v, want *diag.Error", err)
			}
			if d.File != "main.cero" {
				t.Errorf("File = %q, want %q", d.File, "main.cero")
			}
			if d.Error() != tt.want {
				t.Errorf("Error() = %q, want %q", d.Error(), tt.want)
			}
		})
	}
}

func TestCompileSuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
	}{
		{
			name: "constant",
			src:  "fn main() -> Int { 42 }\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Compile("main.cero", []byte(tt.src))
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			if !bytes.HasPrefix(got, []byte("\x00asm")) {
				t.Errorf("Compile() = %q, want prefix %q", got, "\x00asm")
			}
		})
	}
}

func TestCompileWith(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			filename  string
			src       string
			files     map[string]string
			wantReads map[string]int
		}{
			{
				name:     "two-level import",
				filename: "a.cero",
				src:      "import \"b\"\nfn main() -> Int { m() }\n",
				files: map[string]string{
					"b.cero": "import \"c\"\npub fn m() -> Int { n() }\n",
					"c.cero": "pub fn n() -> Int { 7 }\n",
				},
				wantReads: map[string]int{"b.cero": 1, "c.cero": 1},
			},
			{
				name:     "subdirectory module",
				filename: "main.cero",
				src:      "import \"util/math\"\nfn main() -> Int { square(6) }\n",
				files: map[string]string{
					"util/math.cero": "pub fn square(n: Int) -> Int { n * n }\n",
				},
				wantReads: map[string]int{"util/math.cero": 1},
			},
			{
				name:     "shared module is read once",
				filename: "main.cero",
				src:      "import \"a\"\nimport \"b\"\nfn main() -> Int { fa() + fb() }\n",
				files: map[string]string{
					"shared.cero": "pub fn v() -> Int { 1 }\n",
					"a.cero":      "import \"shared\"\npub fn fa() -> Int { v() }\n",
					"b.cero":      "import \"shared\"\npub fn fb() -> Int { v() }\n",
				},
				wantReads: map[string]int{"a.cero": 1, "b.cero": 1, "shared.cero": 1},
			},
			{
				name:      "std/list import",
				filename:  "main.cero",
				src:       "import \"std/list\"\nfn main() -> Int { length(Cons(1, Nil)) }\n",
				files:     map[string]string{},
				wantReads: map[string]int{},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				reads := make(map[string]int)
				read := func(name string) ([]byte, error) {
					reads[name]++
					src, ok := tt.files[name]
					if !ok {
						return nil, os.ErrNotExist
					}
					return []byte(src), nil
				}
				got, err := CompileWith(tt.filename, []byte(tt.src), read)
				if err != nil {
					t.Fatalf("CompileWith() error = %v", err)
				}
				if !bytes.HasPrefix(got, []byte("\x00asm")) {
					t.Errorf("CompileWith() = %q, want prefix %q", got, "\x00asm")
				}
				if len(reads) != len(tt.wantReads) {
					t.Errorf("reads = %v, want %v", reads, tt.wantReads)
				}
				for name, want := range tt.wantReads {
					if reads[name] != want {
						t.Errorf("reads[%s] = %d, want %d", name, reads[name], want)
					}
				}
			})
		}
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			filename string
			src      string
			files    map[string]string
			want     string
		}{
			{
				name:     "missing module",
				filename: "main.cero",
				src:      "import \"missing\"\nfn main() -> Int { 0 }\n",
				want:     "main.cero:1:8: cannot find module 'missing'",
			},
			{
				name:     "invalid path parent",
				filename: "main.cero",
				src:      "import \"../x\"\nfn main() -> Int { 0 }\n",
				want:     "main.cero:1:8: invalid module path '../x'",
			},
			{
				name:     "invalid path empty segment",
				filename: "main.cero",
				src:      "import \"a//b\"\nfn main() -> Int { 0 }\n",
				want:     "main.cero:1:8: invalid module path 'a//b'",
			},
			{
				name:     "invalid path hyphen",
				filename: "main.cero",
				src:      "import \"a-b\"\nfn main() -> Int { 0 }\n",
				want:     "main.cero:1:8: invalid module path 'a-b'",
			},
			{
				name:     "cycle between two modules",
				filename: "main.cero",
				src:      "import \"a\"\nfn main() -> Int { f() }\n",
				files: map[string]string{
					"a.cero": "import \"b\"\npub fn f() -> Int { 1 }\n",
					"b.cero": "import \"a\"\npub fn g() -> Int { 1 }\n",
				},
				want: "b.cero:1:8: import cycle: a -> b -> a",
			},
			{
				name:     "self import",
				filename: "a.cero",
				src:      "import \"a\"\nfn main() -> Int { 0 }\n",
				want:     "a.cero:1:8: import cycle: a -> a",
			},
			{
				name:     "cycle through the entry",
				filename: "main.cero",
				src:      "import \"lib\"\nfn main() -> Int { f() }\n",
				files: map[string]string{
					"lib.cero": "import \"main\"\npub fn f() -> Int { 1 }\n",
				},
				want: "lib.cero:1:8: import cycle: main -> lib -> main",
			},
			{
				name:     "syntax error in an imported module",
				filename: "main.cero",
				src:      "import \"bad\"\nfn main() -> Int { 0 }\n",
				files: map[string]string{
					"bad.cero": "fn",
				},
				want: "bad.cero:1:3: expected identifier, found end of file",
			},
			{
				name:     "type error in an imported module",
				filename: "main.cero",
				src:      "import \"bad\"\nfn main() -> Int { f() }\n",
				files: map[string]string{
					"bad.cero": "pub fn f() -> Int { true }\n",
				},
				want: "bad.cero:1:21: expected Int, found Bool",
			},
			{
				name:     "missing standard module",
				filename: "main.cero",
				src:      "import \"std/nope\"\nfn main() -> Int { 0 }\n",
				want:     "main.cero:1:8: cannot find module 'std/nope'",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				read := func(name string) ([]byte, error) {
					src, ok := tt.files[name]
					if !ok {
						return nil, os.ErrNotExist
					}
					return []byte(src), nil
				}
				_, err := CompileWith(tt.filename, []byte(tt.src), read)
				if err == nil {
					t.Fatalf("CompileWith() error = nil, want %q", tt.want)
				}
				if err.Error() != tt.want {
					t.Errorf("Error() = %q, want %q", err.Error(), tt.want)
				}
			})
		}
	})
}
