// Package cli implements the command dispatch for the ceroc compiler CLI.
//
// The command surface follows ADR-0001: the top-level command is `ceroc`
// with the subcommands `build`, `run` and `fmt`. Because the language is at
// the v0.1 bootstrap stage, the compiler subcommands are intentionally still
// stubs; only `version` and `help` are fully wired up. Keeping the dispatch
// isolated here (rather than in main) makes it directly unit-testable.
package cli

import (
	"fmt"
	"io"
)

// Version is the current ceroc version. v0.x indicates the pre-self-hosting
// bootstrap compiler written in Go, per ADR-0001.
const Version = "0.0.1-dev"

// Exit codes returned by Run. They are stable so tests and shell callers can
// rely on them.
const (
	exitOK             = 0
	exitUsageError     = 2
	exitNotImplemented = 3
)

const usage = `ceroc - the Cero compiler (bootstrap, Go implementation)

Usage:
    ceroc <command> [arguments]

Commands:
    build      Compile a Cero source file to WebAssembly (not yet implemented)
    run        Compile and run a Cero source file (not yet implemented)
    fmt        Format Cero source code (not yet implemented)
    version    Print the ceroc version
    help       Print this help message

See docs/adr/0001-cero-language-initial-policy.md for the language roadmap.
`

// Run dispatches a ceroc invocation. args is the argument list *excluding* the
// program name (i.e. os.Args[1:]). It writes user-facing output to stdout and
// diagnostics to stderr, and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return exitOK
	}

	command := args[0]
	switch command {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "ceroc %s\n", Version)
		return exitOK
	case "build", "run", "fmt":
		// These subcommands are declared by ADR-0001 but the v0.1 compiler
		// pipeline (lexer -> parser -> type checker -> IR -> WASM) is not yet
		// implemented. Fail explicitly rather than silently succeeding.
		fmt.Fprintf(stderr, "ceroc %s: not yet implemented (v0.1 work in progress)\n", command)
		return exitNotImplemented
	default:
		fmt.Fprintf(stderr, "ceroc: unknown command %q\n\n", command)
		fmt.Fprint(stderr, usage)
		return exitUsageError
	}
}
