package selfhost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/deltama37/cero/internal/driver"
	"github.com/deltama37/cero/internal/ir"
	"github.com/deltama37/cero/internal/lower"
	"github.com/deltama37/cero/internal/typecheck"
)

func TestX86Encoding(t *testing.T) {
	t.Parallel()
	skipNoX86(t)

	root := repoRoot(t)
	file := filepath.Join(root, "compiler/x86/enc_test_main.cero")
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read enc_test_main.cero: %v", err)
	}
	out, err := driver.CompileProgram(
		file,
		src,
		func(name string) ([]byte, error) {
			body, readErr := os.ReadFile(name)
			if readErr != nil {
				return nil, fmt.Errorf("read %s: %w", name, readErr)
			}
			return body, nil
		},
	)
	if err != nil {
		t.Fatalf("compile enc_test_main.cero: %v", err)
	}
	wasmPath := filepath.Join(t.TempDir(), "enc.wasm")
	if err := os.WriteFile(wasmPath, out.Wasm, 0o644); err != nil {
		t.Fatalf("write wasm: %v", err)
	}
	cmd := exec.Command("wasmtime", "run", wasmPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("wasmtime: %v\nstderr:\n%s", err, stderr.String())
	}

	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
		name, hex, ok := strings.Cut(line, " ")
		if !ok || name == "" || hex == "" {
			t.Fatalf("bad line %q", line)
		}
		if _, dup := got[name]; dup {
			t.Fatalf("duplicate %s", name)
		}
		got[name] = hex
	}
	if len(got) < 40 {
		t.Fatalf("encoded instructions = %d, want at least 40", len(got))
	}
	for name, want := range x86EncWant {
		hex, ok := got[name]
		if !ok {
			t.Errorf("missing %s", name)
			continue
		}
		if hex != want {
			t.Errorf("%s = %s, want %s", name, hex, want)
		}
	}
	for name := range got {
		if _, ok := x86EncWant[name]; !ok {
			t.Errorf("unexpected %s", name)
		}
	}
}

func skipNoX86(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("needs linux/amd64")
	}
	skipNoWasmtime(t)
}

var x86EncWant = map[string]string{
	"movrr_rax_rcx":     "4889c8",
	"movrr_r8_r9":       "4d89c8",
	"movri64_rax_42":    "48b82a00000000000000",
	"movri64_r8_m1":     "49b8ffffffffffffffff",
	"movri_rax_42":      "48c7c02a000000",
	"movri_rax_heapend": "48b80000ffff00000000",
	"load64_rax_rbp_16": "488b8510000000",
	"load64_rax_rsp_8":  "488b842408000000",
	"store64_rbp_m8":    "488985f8ffffff",
	"store64_rsp_r10":   "4c89942400000000",
	"load32_eax_rax":    "8b8000000000",
	"store32_rax_ecx":   "898800000000",
	"load8u_rax_8":      "480fb68008000000",
	"store8_al_8":       "888008000000",
	"push_rax":          "50",
	"push_r10":          "4152",
	"pop_rax":           "58",
	"pop_r11":           "415b",
	"add_rax_rcx":       "4801c8",
	"sub_rax_rdx":       "4829d0",
	"imul_rax_rcx":      "480fafc1",
	"cqo":               "4899",
	"idiv_rcx":          "48f7f9",
	"cmp_rax_rcx":       "4839c8",
	"cmpri32_rax_m1":    "4881f8ffffffff",
	"cmpri_rcx_0":       "4883f900",
	"sete_al":           "0f94c0",
	"setne_al":          "0f95c0",
	"setl_al":           "0f9cc0",
	"setle_al":          "0f9ec0",
	"setg_al":           "0f9fc0",
	"setge_al":          "0f9dc0",
	"setb_al":           "0f92c0",
	"setbe_al":          "0f96c0",
	"seta_al":           "0f97c0",
	"setae_al":          "0f93c0",
	"movzx_rax_al":      "480fb6c0",
	"and_rax_rcx":       "4821c8",
	"or_rax_rdx":        "4809d0",
	"xor_rax_rax":       "4831c0",
	"shl_cl":            "48d3e0",
	"sar_cl":            "48d3f8",
	"shr_cl":            "48d3e8",
	"neg_rax":           "48f7d8",
	"ret_16":            "c21000",
	"syscall":           "0f05",
	"call_rax":          "ffd0",
	"jmp_rax":           "ffe0",
	"call_r10":          "41ffd2",
	"jmp_r11":           "41ffe3",
	"lea_rax_rbp_m8":    "488d85f8ffffff",
	"lea_rsp_rbp_8":     "488da508000000",
	"push_10":           "6a0a",
	"add_rsp_8":         "4883c408",
	"shl_rax_32":        "48c1e020",
	"shr_rdx_32":        "48c1ea20",
	"and_rcx_m8":        "4883e1f8",
	"xor_rax_1":         "4883f001",
	"div_rcx":           "48f7f1",
	"mov32_ecx_eax":     "89c1",
	"sub_rsp_32":        "4883ec20",
	"store32_rax_4":     "898804000000",
	"load64_r10_rbp_8":  "4c8b9508000000",
	"jmp_rel0":          "e900000000",
	"call_rel0":         "e800000000",
	"je_rel_m6":         "0f84faffffff",
	"jb_rel_m6":         "0f82faffffff",
	"size_jmp":          "48c7c005000000",
	"size_jcc":          "48c7c006000000",
	"size_call":         "48c7c005000000",
	"size_movlabel":     "48c7c00a000000",
	"size_label":        "48c7c000000000",
	"size_bytes":        "48c7c003000000",
}

func TestX86Diff(t *testing.T) {
	t.Parallel()
	skipNoX86(t)

	root := repoRoot(t)
	var run, deferred []string
	for _, file := range x86Corpus(t, root) {
		ok, err := x86Unit1(root, file)
		if err != nil {
			t.Fatalf("lower %s: %v", file, err)
		}
		if ok {
			run = append(run, file)
		} else {
			deferred = append(deferred, file)
		}
	}
	t.Logf("x86 unit1 %d deferred %d", len(run), len(deferred))
	for _, must := range x86Must {
		found := false
		for _, file := range run {
			if file == must {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("required program %s is not in the unit 1 set", must)
		}
	}
	if len(run) == 0 {
		t.Fatal("no unit 1 programs")
	}

	outDir := filepath.Join(root, "internal/selfhost/testdata/.out/x86")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, file := range run {
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			x86DiffOne(t, root, outDir, file)
		})
	}
}

func x86DiffOne(t *testing.T, root, outDir, file string) {
	t.Helper()

	wantOut, wantCode, wantStop := x86WasmMain(t, root, file)
	name := strings.ReplaceAll(file, "/", "_")
	rel := filepath.Join("internal/selfhost/testdata/.out/x86", name)
	stdout, stderr, code := runCero(t, root, "build", "--target", "x86_64-linux", file, "-o", rel)
	if code != 0 {
		t.Fatalf("build exit %d\nstdout %q\nstderr %q", code, stdout, stderr)
	}
	bin := filepath.Join(outDir, name)
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	gotOut, gotCode, gotStop := x86Run(t, bin)
	if wantStop || gotStop {
		if wantStop && gotStop {
			return
		}
		t.Fatalf("timeout native %v wasm %v", gotStop, wantStop)
	}
	if gotCode != wantCode || gotOut != wantOut {
		t.Fatalf("stdout %q exit %d, want stdout %q exit %d", gotOut, gotCode, wantOut, wantCode)
	}
}

func x86Corpus(t *testing.T, root string) []string {
	t.Helper()

	files := relGlob(t, root, "examples/*.cero")
	files = append(files, relGlob(t, root, "examples/io/*.cero")...)
	files = append(files, "examples/modules/main.cero")
	files = append(files, checkOKFiles(t, root)...)
	files = append(files, relGlob(t, root, "internal/selfhost/testdata/programs/*.cero")...)
	return files
}

func x86Unit1(root, file string) (bool, error) {
	mod, err := x86Lower(root, file)
	if err != nil {
		return false, err
	}
	if mod.Command {
		return false, nil
	}
	fn := mod.Funcs[mod.Main]
	if len(fn.Sig.Params) != 0 || fn.Sig.Result != ir.Int {
		return false, nil
	}
	for _, f := range mod.Funcs {
		if x86BadPrim(f.Body) {
			return false, nil
		}
	}
	return true, nil
}

func x86Lower(root, file string) (*ir.Module, error) {
	src, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	mods, err := driver.Load(file, src, func(name string) ([]byte, error) {
		body, readErr := os.ReadFile(filepath.Join(root, name))
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", name, readErr)
		}
		return body, nil
	})
	if err != nil {
		return nil, err
	}
	info, err := typecheck.CheckProgram(mods)
	if err != nil {
		return nil, err
	}
	return lower.LowerProgram(mods, info), nil
}

func x86InlinePrim(op ir.PrimOp) bool {
	switch op {
	case ir.StrLength, ir.BitAnd, ir.BitOr, ir.BitXor, ir.ShiftLeft, ir.ShiftRight, ir.ShiftRightUnsigned:
		return true
	default:
		return false
	}
}

func x86BadPrim(e ir.Expr) bool {
	if e == nil {
		return false
	}
	switch e := e.(type) {
	case *ir.IntConst, *ir.BoolConst, *ir.StrConst, *ir.LocalGet, *ir.Field:
		return false
	case *ir.FuncValue:
		return e.Env != nil && x86BadPrim(e.Env)
	case *ir.Unary:
		return x86BadPrim(e.X)
	case *ir.Binary:
		return x86BadPrim(e.X) || x86BadPrim(e.Y)
	case *ir.If:
		return x86BadPrim(e.Cond) || x86BadPrim(e.Then) || x86BadPrim(e.Else)
	case *ir.Block:
		for _, let := range e.Lets {
			if x86BadPrim(let.Value) {
				return true
			}
		}
		return x86BadPrim(e.Result)
	case *ir.Call:
		for _, arg := range e.Args {
			if x86BadPrim(arg) {
				return true
			}
		}
		return false
	case *ir.CallIndirect:
		if x86BadPrim(e.Callee) {
			return true
		}
		for _, arg := range e.Args {
			if x86BadPrim(arg) {
				return true
			}
		}
		return false
	case *ir.Construct:
		for _, field := range e.Fields {
			if x86BadPrim(field) {
				return true
			}
		}
		return false
	case *ir.SwitchTag:
		for _, c := range e.Cases {
			if x86BadPrim(c.Body) {
				return true
			}
		}
		return e.Default != nil && x86BadPrim(e.Default)
	case *ir.Prim:
		if !x86InlinePrim(e.Op) {
			return true
		}
		for _, arg := range e.Args {
			if x86BadPrim(arg) {
				return true
			}
		}
		return false
	default:
		panic(fmt.Sprintf("x86: unhandled expr %T", e))
	}
}

func x86WasmMain(t *testing.T, root, file string) (string, int, bool) {
	t.Helper()

	src, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	out, err := driver.CompileProgram(file, src, func(name string) ([]byte, error) {
		body, readErr := os.ReadFile(filepath.Join(root, name))
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", name, readErr)
		}
		return body, nil
	})
	if err != nil {
		t.Fatalf("compile %s: %v", file, err)
	}
	path := filepath.Join(t.TempDir(), "main.wasm")
	if err := os.WriteFile(path, out.Wasm, 0o644); err != nil {
		t.Fatalf("write wasm: %v", err)
	}
	return x86Run(t, "wasmtime", "run", "--invoke", "main", path)
}

func x86Run(t *testing.T, name string, args ...string) (string, int, bool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), 0, false
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", 0, true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), exitErr.ExitCode(), false
	}
	t.Fatalf("run %s: %v\nstderr:\n%s", name, err, stderr.String())
	return "", 1, false
}

var x86Must = []string{
	"examples/tail_sum.cero",
	"internal/selfhost/testdata/programs/deep-indirect-tail-call.cero",
	"internal/selfhost/testdata/programs/tail-call-different-arity.cero",
	"internal/selfhost/testdata/programs/division-by-zero.cero",
	"internal/selfhost/testdata/programs/remainder-by-zero.cero",
	"internal/selfhost/testdata/programs/division-min-by-neg-one.cero",
	"internal/selfhost/testdata/programs/remainder-min-by-neg-one.cero",
}
