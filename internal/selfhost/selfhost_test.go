package selfhost

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/deltama37/cero/internal/ast"
	"github.com/deltama37/cero/internal/driver"
)

// buildCompiler compiles compiler/main.cero once and returns the wasm path.
// runCero runs that module from the repository root with wasmtime.

var (
	compileOnce sync.Once
	wasmPath    string
	compileErr  error
)

func TestParse(t *testing.T) {
	t.Parallel()
	skipNoWasmtime(t)

	root := repoRoot(t)
	files := relGlob(t, root, "examples/*.cero")
	files = append(files, relGlob(t, root, "examples/io/*.cero")...)
	files = append(files, "examples/modules/main.cero")
	files = append(files, relGlob(t, root, "std/*.cero")...)
	files = append(files, "compiler/main.cero")

	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			want, err := goParse(root, file)
			if err != nil {
				t.Fatalf("Load(%s): %v", file, err)
			}
			stdout, stderr, code := runCero(t, root, "parse", file)
			if code != 0 {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			if stderr != "" {
				t.Errorf("stderr = %q", stderr)
			}
			if stdout != want {
				t.Errorf("stdout mismatch for %s\n got %q\nwant %q", file, stdout, want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()
	skipNoWasmtime(t)

	root := repoRoot(t)
	files := relGlob(t, root, "internal/selfhost/testdata/parse/*.cero")
	files = append(files, relGlob(t, root, "internal/selfhost/testdata/parse/modules/*/main.cero")...)
	if len(files) < 40 {
		t.Fatalf("testdata files = %d, want at least 40", len(files))
	}

	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			_, err := goParse(root, file)
			if err == nil {
				t.Fatal("Load succeeded, want an error")
			}
			want := err.Error() + "\n"
			stdout, stderr, code := runCero(t, root, "parse", file)
			if code != 1 {
				t.Fatalf("exit %d, want 1\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q", stdout)
			}
			if stderr != want {
				t.Errorf("stderr = %q, want %q", stderr, want)
			}
		})
	}
}

func TestParseCorpusCoversErrors(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	files := relGlob(t, root, "internal/selfhost/testdata/parse/*.cero")
	files = append(files, relGlob(t, root, "internal/selfhost/testdata/parse/modules/*/main.cero")...)
	var got []string
	for _, file := range files {
		_, err := goParse(root, file)
		if err == nil {
			t.Fatalf("Load(%s) succeeded, want an error", file)
		}
		got = append(got, err.Error())
	}

	formats := errorFormats(t, root)
	if len(formats) == 0 {
		t.Fatal("no diag.Errorf formats found")
	}
	for _, format := range formats {
		re, err := formatRegexp(format)
		if err != nil {
			t.Fatalf("format %q: %v", format, err)
		}
		found := false
		for _, msg := range got {
			if re.MatchString(msg) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("format %q is not covered by testdata", format)
		}
	}
}

func skipNoWasmtime(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("wasmtime"); err != nil {
		t.Skip("wasmtime not found")
	}
}

func repoRoot(t *testing.T) string {
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

func relGlob(t *testing.T, root, pattern string) []string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(root, pattern))
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		rel, err := filepath.Rel(root, match)
		if err != nil {
			t.Fatalf("rel %s: %v", match, err)
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

func goParse(root, file string) (string, error) {
	src, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", file, err)
	}
	mods, err := driver.Load(file, src, func(name string) ([]byte, error) {
		body, readErr := os.ReadFile(filepath.Join(root, name))
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", name, readErr)
		}
		return body, nil
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, mod := range mods {
		fmt.Fprintf(&b, "== %s\n%s\n", mod.File, ast.FormatFile(mod.Ast))
	}
	return b.String(), nil
}

func buildCompiler(root string) (string, error) {
	compileOnce.Do(func() {
		src, err := os.ReadFile(filepath.Join(root, "compiler/main.cero"))
		if err != nil {
			compileErr = fmt.Errorf("read compiler/main.cero: %w", err)
			return
		}
		out, err := driver.CompileProgram(
			"compiler/main.cero",
			src,
			func(name string) ([]byte, error) {
				body, readErr := os.ReadFile(filepath.Join(root, name))
				if readErr != nil {
					return nil, fmt.Errorf("read %s: %w", name, readErr)
				}
				return body, nil
			},
		)
		if err != nil {
			compileErr = fmt.Errorf("compile compiler/main.cero: %w", err)
			return
		}
		dir, err := os.MkdirTemp("", "ceroc-selfhost-")
		if err != nil {
			compileErr = fmt.Errorf("create temp dir: %w", err)
			return
		}
		path := filepath.Join(dir, "ceroc.wasm")
		if err := os.WriteFile(path, out.Wasm, 0o644); err != nil {
			compileErr = fmt.Errorf("write %s: %w", path, err)
			return
		}
		wasmPath = path
	})
	if compileErr != nil {
		return "", compileErr
	}
	return wasmPath, nil
}

func runCero(t *testing.T, root string, args ...string) (string, string, int) {
	t.Helper()

	wasm, err := buildCompiler(root)
	if err != nil {
		t.Fatalf("build compiler: %v", err)
	}
	cmdArgs := make([]string, 0, 3+len(args))
	cmdArgs = append(cmdArgs, "run", "--dir=.", wasm)
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("wasmtime", cmdArgs...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), stderr.String(), exitErr.ExitCode()
	}
	t.Fatalf("wasmtime: %v\nstderr:\n%s", err, stderr.String())
	return "", "", 1
}

var errorfRE = regexp.MustCompile(`diag\.Errorf\([^"\n]*"((?:\\.|[^"\\])*)"`)

func errorFormats(t *testing.T, root string) []string {
	t.Helper()

	var formats []string
	for _, rel := range []string{"internal/lexer/lexer.go", "internal/parser/parser.go"} {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for _, match := range errorfRE.FindAllStringSubmatch(string(body), -1) {
			format, err := strconv.Unquote(`"` + match[1] + `"`)
			if err != nil {
				t.Fatalf("unquote %q: %v", match[1], err)
			}
			formats = append(formats, format)
		}
	}
	return formats
}

var verbRE = regexp.MustCompile(`%(?:%|[a-z])`)

func formatRegexp(format string) (*regexp.Regexp, error) {
	var b strings.Builder
	last := 0
	for _, loc := range verbRE.FindAllStringIndex(format, -1) {
		b.WriteString(regexp.QuoteMeta(format[last:loc[0]]))
		if format[loc[0]:loc[1]] == "%%" {
			b.WriteString("%")
		} else {
			b.WriteString(".*")
		}
		last = loc[1]
	}
	b.WriteString(regexp.QuoteMeta(format[last:]))
	return regexp.Compile(b.String())
}
