// Package driver runs the Cero compile pipeline from source to WebAssembly.
package driver

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/deltama37/cero/internal/ast"
	"github.com/deltama37/cero/internal/diag"
	"github.com/deltama37/cero/internal/lower"
	"github.com/deltama37/cero/internal/parser"
	"github.com/deltama37/cero/internal/typecheck"
	"github.com/deltama37/cero/internal/wasm"
	"github.com/deltama37/cero/std"
)

const (
	unvisited = 0
	visiting  = 1
	done      = 2
)

// Compile compiles the program whose entry module is filename with source
// src. Non-standard imports are read from disk relative to the directory of
// filename.
func Compile(filename string, src []byte) ([]byte, error) {
	return CompileWith(filename, src, os.ReadFile)
}

// CompileWith is Compile with read used to read non-standard modules by
// file name.
func CompileWith(
	filename string,
	src []byte,
	read func(name string) ([]byte, error),
) ([]byte, error) {
	mods, err := load(filename, src, read)
	if err != nil {
		return nil, err
	}
	info, err := typecheck.CheckProgram(mods)
	if err != nil {
		return nil, err
	}
	return wasm.Encode(lower.LowerProgram(mods, info)), nil
}

func load(
	filename string,
	src []byte,
	read func(name string) ([]byte, error),
) ([]*typecheck.Module, error) {
	file, err := parser.ParseFile(src)
	if err != nil {
		return nil, withFile(filename, err)
	}
	entry := &typecheck.Module{
		Path: modulePath(filename),
		File: filename,
		Ast:  file,
	}
	l := &loader{
		root:   filepath.Dir(filename),
		read:   read,
		byFile: map[string]*typecheck.Module{filepath.Clean(filename): entry},
		state:  make(map[*typecheck.Module]int),
	}
	if err := l.visit(entry); err != nil {
		return nil, err
	}
	return l.order, nil
}

type loader struct {
	root   string
	read   func(name string) ([]byte, error)
	byFile map[string]*typecheck.Module
	state  map[*typecheck.Module]int
	stack  []*typecheck.Module
	order  []*typecheck.Module
}

func (l *loader) visit(m *typecheck.Module) error {
	l.state[m] = visiting
	l.stack = append(l.stack, m)
	for _, imp := range m.Ast.Imports {
		dep, err := l.resolve(m, imp)
		if err != nil {
			return err
		}
		if l.state[dep] == visiting {
			return l.cycleError(m, imp, dep)
		}
		if l.state[dep] == unvisited {
			if err := l.visit(dep); err != nil {
				return err
			}
		}
		m.Imports = append(m.Imports, dep)
	}
	l.state[m] = done
	l.order = append(l.order, m)
	l.stack = l.stack[:len(l.stack)-1]
	return nil
}

func (l *loader) resolve(m *typecheck.Module, imp *ast.Import) (*typecheck.Module, error) {
	p := imp.Path
	if !validPath(p) {
		return nil, moduleErrorf(m.File, imp.PathPos, "invalid module path '%s'", p)
	}
	fileName, readSrc := l.locate(p)
	if dep, ok := l.byFile[fileName]; ok {
		return dep, nil
	}
	src, err := readSrc()
	if err != nil {
		return nil, moduleErrorf(m.File, imp.PathPos, "cannot find module '%s'", p)
	}
	astFile, err := parser.ParseFile(src)
	if err != nil {
		return nil, withFile(fileName, err)
	}
	dep := &typecheck.Module{
		Path: p,
		File: fileName,
		Ast:  astFile,
	}
	l.byFile[fileName] = dep
	return dep, nil
}

// locate reports the file name of module path p and how to read it.
// Standard modules come from the embedded library; every other path is
// read relative to the entry module's directory.
func (l *loader) locate(p string) (string, func() ([]byte, error)) {
	if strings.HasPrefix(p, "std/") {
		name := p + ".cero"
		rel := strings.TrimPrefix(p, "std/") + ".cero"
		return name, func() ([]byte, error) {
			return std.FS.ReadFile(rel)
		}
	}
	name := filepath.Join(l.root, p+".cero")
	return name, func() ([]byte, error) {
		return l.read(name)
	}
}

func (l *loader) cycleError(m *typecheck.Module, imp *ast.Import, dep *typecheck.Module) *diag.Error {
	start := 0
	for start < len(l.stack) && l.stack[start] != dep {
		start++
	}
	parts := make([]string, 0, len(l.stack)-start+1)
	for _, mod := range l.stack[start:] {
		parts = append(parts, mod.Path)
	}
	parts = append(parts, dep.Path)
	return moduleErrorf(m.File, imp.PathPos, "import cycle: %s", strings.Join(parts, " -> "))
}

func modulePath(filename string) string {
	return strings.TrimSuffix(filepath.Base(filename), ".cero")
}

func validPath(p string) bool {
	if p == "" {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if !validPathPart(part) {
			return false
		}
	}
	return true
}

func validPathPart(part string) bool {
	if part == "" {
		return false
	}
	for i := 0; i < len(part); i++ {
		c := part[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			return false
		}
	}
	return true
}

func moduleErrorf(
	file string,
	pos diag.Pos,
	format string,
	args ...any,
) *diag.Error {
	err := diag.Errorf(pos, format, args...)
	err.File = file
	return err
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
