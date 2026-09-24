// Package cli implements the command dispatch for the ceroc compiler CLI.
//
// The command surface follows ADR-0001: the top-level command is `ceroc`
// with the subcommands `build`, `run` and `fmt`. `fmt` stays unimplemented
// in v0.1. `build` writes a WebAssembly module and `run` executes it with
// an external wasmtime (ADR-0002).
package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/deltama37/cero/internal/driver"
)

// Version is the current ceroc version. v0.x indicates the pre-self-hosting
// bootstrap compiler written in Go, per ADR-0001.
const Version = "0.0.1-dev"

// Exit codes returned by Run. They are stable so tests and shell callers can
// rely on them.
const (
	exitOK             = 0
	exitFailure        = 1 // compile error, I/O error, runtime failure
	exitUsageError     = 2
	exitNotImplemented = 3
)

const usage = `ceroc - the Cero compiler (bootstrap, Go implementation)

Usage:
    ceroc <command> [arguments]
    ceroc build [-o output.wasm] <file.cero>
    ceroc run <file.cero>

Commands:
    build      Compile a Cero source file to WebAssembly
    run        Compile and run a Cero source file with wasmtime
    fmt        Format Cero source code (not yet implemented)
    version    Print the ceroc version
    help       Print this help message

See docs/adr/0001-cero-language-initial-policy.md for the language roadmap.
`

// Run dispatches a ceroc invocation. args is the argument list *excluding* the
// program name (i.e. os.Args[1:]). It writes user-facing output to stdout and
// diagnostics to stderr, and returns the process exit code.
//
// Run calls run(args, stdout, stderr, os.Getenv).
func Run(args []string, stdout, stderr io.Writer) int {
	return run(args, stdout, stderr, os.Getenv)
}

func run(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	getenv func(string) string,
) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return exitOK
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "ceroc %s\n", Version)
		return exitOK
	case "build":
		return build(args[1:], stderr)
	case "run":
		return runSource(args[1:], stdout, stderr, getenv)
	case "fmt":
		fmt.Fprintf(stderr, "ceroc fmt: not yet implemented (v0.1 work in progress)\n")
		return exitNotImplemented
	default:
		fmt.Fprintf(stderr, "ceroc: unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return exitUsageError
	}
}

func build(args []string, stderr io.Writer) int {
	input, output, err := parseBuildArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "ceroc build: %s\n", err.Error())
		return exitUsageError
	}
	if output == "" {
		output = defaultOutput(input)
	}
	src, err := os.ReadFile(input)
	if err != nil {
		fmt.Fprintf(stderr, "ceroc: %v\n", fmt.Errorf("read %s: %w", input, err))
		return exitFailure
	}
	compiled, err := driver.Compile(input, src)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}
	if err := os.WriteFile(output, compiled, 0o644); err != nil {
		fmt.Fprintf(stderr, "ceroc: %v\n", fmt.Errorf("write %s: %w", output, err))
		return exitFailure
	}
	return exitOK
}

func parseBuildArgs(args []string) (string, string, error) {
	var input, output string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-o" {
			if i+1 >= len(args) {
				return "", "", errors.New("-o requires an argument")
			}
			i++
			output = args[i]
			continue
		}
		if strings.HasPrefix(a, "-") {
			return "", "", fmt.Errorf("unknown flag %q", a)
		}
		if input != "" {
			return "", "", errors.New("too many arguments")
		}
		input = a
	}
	if input == "" {
		return "", "", errors.New("missing input file")
	}
	return input, output, nil
}

func defaultOutput(input string) string {
	const ext = ".cero"
	if strings.HasSuffix(input, ext) {
		return strings.TrimSuffix(input, ext) + ".wasm"
	}
	return input + ".wasm"
}

func runSource(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	getenv func(string) string,
) int {
	input, err := parseRunArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "ceroc run: %s\n", err.Error())
		return exitUsageError
	}
	runtime := getenv("CERO_WASM_RUNTIME")
	if runtime == "" {
		runtime = "wasmtime"
	}
	path, err := exec.LookPath(runtime)
	if err != nil {
		fmt.Fprintf(stderr, "ceroc run: WebAssembly runtime %q not found (install wasmtime or set CERO_WASM_RUNTIME)\n", runtime)
		return exitFailure
	}
	src, err := os.ReadFile(input)
	if err != nil {
		fmt.Fprintf(stderr, "ceroc: %v\n", fmt.Errorf("read %s: %w", input, err))
		return exitFailure
	}
	compiled, err := driver.Compile(input, src)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}
	dir, err := os.MkdirTemp("", "ceroc-run-*")
	if err != nil {
		fmt.Fprintf(stderr, "ceroc: %v\n", fmt.Errorf("create temp dir: %w", err))
		return exitFailure
	}
	defer os.RemoveAll(dir)
	wasmPath := filepath.Join(dir, "main.wasm")
	if err := os.WriteFile(wasmPath, compiled, 0o644); err != nil {
		fmt.Fprintf(stderr, "ceroc: %v\n", fmt.Errorf("write %s: %w", wasmPath, err))
		return exitFailure
	}
	cmd := exec.Command(path, "run", "--invoke", "main", wasmPath)
	var runtimeStderr bytes.Buffer
	cmd.Stdout = stdout
	cmd.Stderr = &runtimeStderr
	if err := cmd.Run(); err != nil {
		_, _ = stderr.Write(runtimeStderr.Bytes())
		fmt.Fprintf(stderr, "ceroc run: runtime failed: %v\n", err)
		return exitFailure
	}
	return exitOK
}

func parseRunArgs(args []string) (string, error) {
	var input string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return "", fmt.Errorf("unknown flag %q", a)
		}
		if input != "" {
			return "", errors.New("too many arguments")
		}
		input = a
	}
	if input == "" {
		return "", errors.New("missing input file")
	}
	return input, nil
}
