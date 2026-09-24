package driver

import (
	"bytes"
	"errors"
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
