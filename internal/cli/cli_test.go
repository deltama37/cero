package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()

	const fibSrc = `fn fib(n: Int) -> Int {
    if n <= 1 {
        n
    } else {
        fib(n - 1) + fib(n - 2)
    }
}

fn main() -> Int {
    fib(10)
}
`
	const mainSrc = "fn main() -> Int { 42 }\n"

	tests := []struct {
		name            string
		args            []string
		files           map[string]string
		getenv          func(string) string
		needsWasmtime   bool
		wantExit        int
		wantStdoutHas   string
		wantStdout      string
		checkStdout     bool
		wantStderrHas   string
		wantStderrEmpty bool
		wantWasm        string
	}{
		{
			name:          "no args prints usage",
			args:          nil,
			wantExit:      exitOK,
			wantStdoutHas: "Usage:",
		},
		{
			name:          "help flag",
			args:          []string{"--help"},
			wantExit:      exitOK,
			wantStdoutHas: "Usage:",
		},
		{
			name:          "usage lists build and run",
			args:          []string{"help"},
			wantExit:      exitOK,
			wantStdoutHas: "    ceroc build [-o output.wasm] <file.cero>\n    ceroc run <file.cero>\n",
		},
		{
			name:          "commands describe build and run",
			args:          []string{"help"},
			wantExit:      exitOK,
			wantStdoutHas: "    build      Compile a Cero source file to WebAssembly\n    run        Compile and run a Cero source file with wasmtime\n",
		},
		{
			name:          "version subcommand",
			args:          []string{"version"},
			wantExit:      exitOK,
			wantStdoutHas: "ceroc " + Version,
		},
		{
			name:          "version flag",
			args:          []string{"-v"},
			wantExit:      exitOK,
			wantStdoutHas: "ceroc " + Version,
		},
		{
			name:            "build default output",
			args:            []string{"build", "$DIR/x.cero"},
			files:           map[string]string{"x.cero": mainSrc},
			wantExit:        exitOK,
			checkStdout:     true,
			wantStderrEmpty: true,
			wantWasm:        "$DIR/x.wasm",
		},
		{
			name:            "build -o before input",
			args:            []string{"build", "-o", "$DIR/out.wasm", "$DIR/x.cero"},
			files:           map[string]string{"x.cero": mainSrc},
			wantExit:        exitOK,
			checkStdout:     true,
			wantStderrEmpty: true,
			wantWasm:        "$DIR/out.wasm",
		},
		{
			name:            "build -o after input",
			args:            []string{"build", "$DIR/x.cero", "-o", "$DIR/out.wasm"},
			files:           map[string]string{"x.cero": mainSrc},
			wantExit:        exitOK,
			checkStdout:     true,
			wantStderrEmpty: true,
			wantWasm:        "$DIR/out.wasm",
		},
		{
			name:          "build missing input",
			args:          []string{"build"},
			wantExit:      exitUsageError,
			wantStderrHas: "ceroc build: missing input file",
		},
		{
			name:          "build -o without value",
			args:          []string{"build", "-o"},
			wantExit:      exitUsageError,
			wantStderrHas: "ceroc build: -o requires an argument",
		},
		{
			name:          "build unknown flag",
			args:          []string{"build", "-x"},
			wantExit:      exitUsageError,
			wantStderrHas: `ceroc build: unknown flag "-x"`,
		},
		{
			name:          "build too many arguments",
			args:          []string{"build", "a.cero", "b.cero"},
			wantExit:      exitUsageError,
			wantStderrHas: "ceroc build: too many arguments",
		},
		{
			name:          "build compile error",
			args:          []string{"build", "$DIR/bad.cero"},
			files:         map[string]string{"bad.cero": "fn main() -> Int { missing }\n"},
			wantExit:      exitFailure,
			wantStderrHas: "$DIR/bad.cero:1:20: undefined name 'missing'",
		},
		{
			name:          "build read error",
			args:          []string{"build", "$DIR/missing.cero"},
			wantExit:      exitFailure,
			wantStderrHas: "ceroc: read",
		},
		{
			name:          "run missing input",
			args:          []string{"run"},
			wantExit:      exitUsageError,
			wantStderrHas: "ceroc run: missing input file",
		},
		{
			name:          "run unknown flag",
			args:          []string{"run", "-z"},
			wantExit:      exitUsageError,
			wantStderrHas: `ceroc run: unknown flag "-z"`,
		},
		{
			name:          "run too many arguments",
			args:          []string{"run", "a.cero", "b.cero"},
			wantExit:      exitUsageError,
			wantStderrHas: "ceroc run: too many arguments",
		},
		{
			name: "run runtime not found",
			args: []string{"run", "main.cero"},
			getenv: func(key string) string {
				if key == "CERO_WASM_RUNTIME" {
					return "no-such-wasm-runtime"
				}
				return ""
			},
			wantExit:      exitFailure,
			wantStderrHas: "not found",
		},
		{
			name:          "run success",
			args:          []string{"run", "$DIR/fib.cero"},
			files:         map[string]string{"fib.cero": fibSrc},
			getenv:        func(string) string { return "" },
			needsWasmtime: true,
			wantExit:      exitOK,
			checkStdout:   true,
			wantStdout:    "55\n",
		},
		{
			name:          "run division by zero",
			args:          []string{"run", "$DIR/div.cero"},
			files:         map[string]string{"div.cero": "fn main() -> Int { 1 / 0 }\n"},
			getenv:        func(string) string { return "" },
			needsWasmtime: true,
			wantExit:      exitFailure,
			wantStderrHas: "runtime failed",
		},
		{
			name:          "fmt is not yet implemented",
			args:          []string{"fmt"},
			wantExit:      exitNotImplemented,
			wantStderrHas: "not yet implemented",
		},
		{
			name:          "unknown command is a usage error",
			args:          []string{"frobnicate"},
			wantExit:      exitUsageError,
			wantStderrHas: "unknown command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.needsWasmtime {
				if _, err := exec.LookPath("wasmtime"); err != nil {
					t.Skip("wasmtime not found")
				}
			}

			dir := t.TempDir()
			for name, src := range tt.files {
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			args := make([]string, len(tt.args))
			for i, a := range tt.args {
				args[i] = strings.ReplaceAll(a, "$DIR", dir)
			}
			getenv := tt.getenv
			if getenv == nil {
				getenv = os.Getenv
			}

			var stdout, stderr bytes.Buffer
			got := run(args, &stdout, &stderr, getenv)
			if got != tt.wantExit {
				t.Errorf("run(%v) exit code = %d, want %d\nstderr: %s", args, got, tt.wantExit, stderr.String())
			}
			if tt.wantStdoutHas != "" && !strings.Contains(stdout.String(), tt.wantStdoutHas) {
				t.Errorf("stdout = %q, want to contain %q", stdout.String(), tt.wantStdoutHas)
			}
			if tt.checkStdout && stdout.String() != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantStdout)
			}
			wantStderr := strings.ReplaceAll(tt.wantStderrHas, "$DIR", dir)
			if wantStderr != "" && !strings.Contains(stderr.String(), wantStderr) {
				t.Errorf("stderr = %q, want to contain %q", stderr.String(), wantStderr)
			}
			if tt.wantStderrEmpty && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
			if tt.wantWasm != "" {
				wasmPath := strings.ReplaceAll(tt.wantWasm, "$DIR", dir)
				data, err := os.ReadFile(wasmPath)
				if err != nil {
					t.Fatalf("read %s: %v", wasmPath, err)
				}
				if !bytes.HasPrefix(data, []byte("\x00asm")) {
					t.Errorf("%s = %q, want prefix %q", wasmPath, data, "\x00asm")
				}
			}
		})
	}
}
