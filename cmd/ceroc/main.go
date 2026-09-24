// Command ceroc is the bootstrap compiler and CLI for the Cero programming
// language. See docs/adr/0001-cero-language-initial-policy.md for the design.
package main

import (
	"os"

	"github.com/deltama37/cero/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
