// Package driver runs the Cero compile pipeline from source to WebAssembly.
package driver

import (
	"errors"

	"github.com/deltama37/cero/internal/diag"
	"github.com/deltama37/cero/internal/lower"
	"github.com/deltama37/cero/internal/parser"
	"github.com/deltama37/cero/internal/typecheck"
	"github.com/deltama37/cero/internal/wasm"
)

// Compile runs the whole pipeline (parse, type check, lower, encode) on src.
// Compile errors are returned as *diag.Error with File set to filename.
func Compile(filename string, src []byte) ([]byte, error) {
	file, err := parser.ParseFile(src)
	if err != nil {
		return nil, withFile(filename, err)
	}
	info, err := typecheck.Check(file)
	if err != nil {
		return nil, withFile(filename, err)
	}
	return wasm.Encode(lower.Lower(file, info)), nil
}

func withFile(filename string, err error) error {
	var d *diag.Error
	if errors.As(err, &d) {
		copy := *d
		copy.File = filename
		return &copy
	}
	return err
}
