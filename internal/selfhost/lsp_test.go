package selfhost

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deltama37/cero/internal/diag"
	"github.com/deltama37/cero/internal/driver"
	"github.com/deltama37/cero/internal/typecheck"
)

func TestLSP(t *testing.T) {
	t.Parallel()
	skipNoWasmtime(t)

	root := repoRoot(t)
	tests := []struct {
		name string
		run  func(t *testing.T, root string)
	}{
		{name: "initialize", run: lspInitialize},
		{name: "didOpen type error", run: lspDidOpenError},
		{name: "didOpen import type error", run: lspDidOpenImportError},
		{name: "clean document", run: lspClean},
		{name: "didChange then didSave", run: lspDidChangeSave},
		{name: "hover", run: lspHover},
		{name: "definition", run: lspDefinition},
		{name: "definition builtin", run: lspBuiltin},
		{name: "formatting", run: lspFormatting},
		{name: "unknown request", run: lspUnknown},
		{name: "shutdown exit 0", run: lspShutdownExit},
		{name: "exit without shutdown", run: lspExitNoShutdown},
		{name: "batched and split messages", run: lspFraming},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t, root)
		})
	}
}

type lspClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr bytes.Buffer
	buf    []byte
	done   bool
}

func startLSP(t *testing.T, root string) *lspClient {
	t.Helper()

	wasm, err := buildCompiler(root)
	if err != nil {
		t.Fatalf("build compiler: %v", err)
	}
	cmd := exec.Command("wasmtime", "run", "--dir=.", wasm, "lsp")
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout: %v", err)
	}
	c := &lspClient{cmd: cmd, stdin: stdin, stdout: stdout}
	cmd.Stderr = &c.stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		if !c.done {
			c.stop()
		}
	})
	return c
}

func (c *lspClient) stop() {
	if c.done {
		return
	}
	c.done = true
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
}

func (c *lspClient) waitExit(t *testing.T) int {
	t.Helper()

	c.done = true
	_ = c.stdin.Close()
	done := make(chan error, 1)
	go func() {
		done <- c.cmd.Wait()
	}()
	select {
	case <-time.After(20 * time.Second):
		t.Fatalf("timeout waiting for exit\nstderr:\n%s", c.stderr.String())
	case err := <-done:
		if err == nil {
			return 0
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		t.Fatalf("wait: %v\nstderr:\n%s", err, c.stderr.String())
	}
	return 1
}

func lspFrame(body []byte) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "Content-Length: %d\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n", len(body))
	b.Write(body)
	return b.Bytes()
}

func (c *lspClient) writeRaw(t *testing.T, b []byte) {
	t.Helper()

	for len(b) > 0 {
		n, err := c.stdin.Write(b)
		if err != nil {
			t.Fatalf("write: %v\nstderr:\n%s", err, c.stderr.String())
		}
		b = b[n:]
	}
}

func (c *lspClient) send(t *testing.T, v any) {
	t.Helper()

	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	c.writeRaw(t, lspFrame(body))
}

func (c *lspClient) readMsg() (map[string]any, error) {
	for {
		if msg, rest, ok, err := takeFrame(c.buf); ok || err != nil {
			if ok {
				c.buf = rest
			}
			return msg, err
		}
		tmp := make([]byte, 4096)
		n, err := c.stdout.Read(tmp)
		if n > 0 {
			c.buf = append(c.buf, tmp[:n]...)
		}
		if err != nil {
			if len(c.buf) == 0 {
				return nil, err
			}
			return nil, fmt.Errorf("read lsp: %w", err)
		}
	}
}

func takeFrame(buf []byte) (map[string]any, []byte, bool, error) {
	sep := bytes.Index(buf, []byte("\r\n\r\n"))
	if sep < 0 {
		return nil, buf, false, nil
	}
	n := -1
	for _, line := range bytes.Split(buf[:sep], []byte("\r\n")) {
		if len(line) >= 15 && bytes.EqualFold(line[:15], []byte("content-length:")) {
			v := strings.TrimSpace(string(line[15:]))
			var parsed int
			if _, err := fmt.Sscan(v, &parsed); err != nil {
				return nil, buf, false, fmt.Errorf("content-length: %w", err)
			}
			n = parsed
		}
	}
	if n < 0 {
		return nil, buf, false, errors.New("missing Content-Length")
	}
	start := sep + 4
	if len(buf) < start+n {
		return nil, buf, false, nil
	}
	var msg map[string]any
	if err := json.Unmarshal(buf[start:start+n], &msg); err != nil {
		return nil, buf, false, fmt.Errorf("json: %w", err)
	}
	return msg, buf[start+n:], true, nil
}

func (c *lspClient) read(t *testing.T) map[string]any {
	t.Helper()

	type result struct {
		msg map[string]any
		err error
	}
	ch := make(chan result, 1)
	go func() {
		msg, err := c.readMsg()
		ch <- result{msg, err}
	}()
	select {
	case <-time.After(30 * time.Second):
		t.Fatalf("timeout reading lsp message\nstderr:\n%s", c.stderr.String())
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("read: %v\nstderr:\n%s", r.err, c.stderr.String())
		}
		return r.msg
	}
	return nil
}

func (c *lspClient) until(t *testing.T, id int) (map[string]any, []map[string]any) {
	t.Helper()

	var notes []map[string]any
	for {
		msg := c.read(t)
		if idEqual(msg["id"], id) {
			return msg, notes
		}
		notes = append(notes, msg)
	}
}

func idEqual(got any, want int) bool {
	f, ok := got.(float64)
	return ok && f == float64(want)
}

func numEqual(got any, want int) bool {
	f, ok := got.(float64)
	return ok && f == float64(want)
}

func fileURI(root, rel string) string {
	return "file://" + root + "/" + filepath.ToSlash(rel)
}

func (c *lspClient) initialize(t *testing.T, root string) map[string]any {
	t.Helper()

	c.send(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"rootUri":      "file://" + root,
			"capabilities": map[string]any{},
		},
	})
	msg := c.read(t)
	c.send(t, map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]any{},
	})
	return msg
}

func (c *lspClient) didOpen(t *testing.T, uri, text string) {
	t.Helper()

	c.send(t, map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": "cero",
				"version":    1,
				"text":       text,
			},
		},
	})
}

func (c *lspClient) sync(t *testing.T, id int) []map[string]any {
	t.Helper()

	c.send(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "$/sync",
		"params":  map[string]any{},
	})
	resp, notes := c.until(t, id)
	checkRPCError(t, resp, -32601)
	return notes
}

func checkRPCError(t *testing.T, msg map[string]any, code int) {
	t.Helper()

	if msg["jsonrpc"] != "2.0" {
		t.Fatalf("jsonrpc = %v", msg["jsonrpc"])
	}
	errObj, ok := msg["error"].(map[string]any)
	if !ok {
		t.Fatalf("error = %v", msg)
	}
	if !numEqual(errObj["code"], code) || errObj["message"] != "method not found" {
		t.Fatalf("error = %v", errObj)
	}
}

func readTestdata(t *testing.T, root, rel string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(body)
}

func goTypeError(t *testing.T, root, rel string) *diag.Error {
	t.Helper()

	src, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	mods, err := driver.Load(rel, src, func(name string) ([]byte, error) {
		body, readErr := os.ReadFile(filepath.Join(root, name))
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", name, readErr)
		}
		return body, nil
	})
	if err != nil {
		t.Fatalf("load %s: %v", rel, err)
	}
	_, err = typecheck.CheckProgram(mods)
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("check %s: %v", rel, err)
	}
	return de
}

func checkDiag(
	t *testing.T,
	note map[string]any,
	root string,
	de *diag.Error,
) {
	t.Helper()

	if note["method"] != "textDocument/publishDiagnostics" {
		t.Fatalf("method = %v", note["method"])
	}
	params, ok := note["params"].(map[string]any)
	if !ok {
		t.Fatalf("params = %v", note["params"])
	}
	if params["uri"] != fileURI(root, filepath.ToSlash(de.File)) {
		t.Fatalf("uri = %v, want %s", params["uri"], fileURI(root, de.File))
	}
	diags, ok := params["diagnostics"].([]any)
	if !ok || len(diags) != 1 {
		t.Fatalf("diagnostics = %v", params["diagnostics"])
	}
	d, ok := diags[0].(map[string]any)
	if !ok {
		t.Fatalf("diagnostic = %v", diags[0])
	}
	if d["message"] != de.Msg || d["source"] != "ceroc" || !numEqual(d["severity"], 1) {
		t.Fatalf("diagnostic = %v", d)
	}
	rng, ok := d["range"].(map[string]any)
	if !ok {
		t.Fatalf("range = %v", d["range"])
	}
	start, ok := rng["start"].(map[string]any)
	if !ok {
		t.Fatalf("start = %v", rng["start"])
	}
	end, ok := rng["end"].(map[string]any)
	if !ok {
		t.Fatalf("end = %v", rng["end"])
	}
	if !numEqual(start["line"], de.Pos.Line-1) || !numEqual(start["character"], de.Pos.Col-1) {
		t.Fatalf("start = %v, want %d:%d", start, de.Pos.Line-1, de.Pos.Col-1)
	}
	if !numEqual(end["line"], de.Pos.Line-1) || !numEqual(end["character"], de.Pos.Col) {
		t.Fatalf("end = %v, want %d:%d", end, de.Pos.Line-1, de.Pos.Col)
	}
}

func emptyOn(t *testing.T, note map[string]any, uri string) {
	t.Helper()

	if note["method"] != "textDocument/publishDiagnostics" {
		t.Fatalf("method = %v", note["method"])
	}
	params, ok := note["params"].(map[string]any)
	if !ok {
		t.Fatalf("params = %v", note["params"])
	}
	if params["uri"] != uri {
		t.Fatalf("uri = %v, want %s", params["uri"], uri)
	}
	diags, ok := params["diagnostics"].([]any)
	if !ok || len(diags) != 0 {
		t.Fatalf("diagnostics = %v", params["diagnostics"])
	}
}

func noteWithDiag(notes []map[string]any) map[string]any {
	for _, note := range notes {
		params, _ := note["params"].(map[string]any)
		diags, _ := params["diagnostics"].([]any)
		if len(diags) > 0 {
			return note
		}
	}
	return nil
}

func lspInitialize(t *testing.T, root string) {
	c := startLSP(t, root)
	msg := c.initialize(t, root)
	if msg["jsonrpc"] != "2.0" || !idEqual(msg["id"], 1) {
		t.Fatalf("response = %v", msg)
	}
	result, ok := msg["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %v", msg["result"])
	}
	caps, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities = %v", result["capabilities"])
	}
	if !numEqual(caps["textDocumentSync"], 1) || caps["hoverProvider"] != true || caps["definitionProvider"] != true || caps["documentFormattingProvider"] != true {
		t.Fatalf("capabilities = %v", caps)
	}
	info, ok := result["serverInfo"].(map[string]any)
	if !ok || info["name"] != "ceroc-lsp" {
		t.Fatalf("serverInfo = %v", result["serverInfo"])
	}
}

func lspDidOpenError(t *testing.T, root string) {
	rel := "internal/selfhost/testdata/lsp/bad.cero"
	de := goTypeError(t, root, rel)
	c := startLSP(t, root)
	c.initialize(t, root)
	uri := fileURI(root, rel)
	c.didOpen(t, uri, readTestdata(t, root, rel))
	notes := c.sync(t, 2)
	if len(notes) != 1 {
		t.Fatalf("notes = %v", notes)
	}
	checkDiag(t, notes[0], root, de)
}

func lspDidOpenImportError(t *testing.T, root string) {
	rel := "internal/selfhost/testdata/lsp/uses_bad.cero"
	de := goTypeError(t, root, rel)
	c := startLSP(t, root)
	c.initialize(t, root)
	uri := fileURI(root, rel)
	c.didOpen(t, uri, readTestdata(t, root, rel))
	notes := c.sync(t, 2)
	got := noteWithDiag(notes)
	if got == nil {
		t.Fatalf("notes = %v", notes)
	}
	checkDiag(t, got, root, de)
	if de.File == rel {
		t.Fatalf("error file = entry %s", rel)
	}
}

func lspClean(t *testing.T, root string) {
	rel := "internal/selfhost/testdata/lsp/uses_lib.cero"
	c := startLSP(t, root)
	c.initialize(t, root)
	uri := fileURI(root, rel)
	c.didOpen(t, uri, readTestdata(t, root, rel))
	notes := c.sync(t, 2)
	if len(notes) != 1 {
		t.Fatalf("notes = %v", notes)
	}
	emptyOn(t, notes[0], uri)
}

func lspDidChangeSave(t *testing.T, root string) {
	rel := "internal/selfhost/testdata/lsp/bad.cero"
	de := goTypeError(t, root, rel)
	c := startLSP(t, root)
	c.initialize(t, root)
	uri := fileURI(root, rel)
	c.didOpen(t, uri, readTestdata(t, root, rel))
	notes := c.sync(t, 2)
	if len(notes) != 1 {
		t.Fatalf("open notes = %v", notes)
	}
	checkDiag(t, notes[0], root, de)
	fixed := "fn main() -> Int {\n    1\n}\n"
	c.send(t, map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didChange",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"contentChanges": []any{
				map[string]any{"text": "fn main() -> Int {\n    true\n}\n"},
				map[string]any{"text": fixed},
			},
		},
	})
	if notes := c.sync(t, 3); len(notes) != 0 {
		t.Fatalf("didChange notes = %v", notes)
	}
	c.send(t, map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didSave",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
		},
	})
	notes = c.sync(t, 4)
	if len(notes) != 1 {
		t.Fatalf("save notes = %v", notes)
	}
	emptyOn(t, notes[0], uri)
}

func lspHover(t *testing.T, root string) {
	rel := "internal/selfhost/testdata/lsp/ok.cero"
	src := readTestdata(t, root, rel)
	c := startLSP(t, root)
	c.initialize(t, root)
	uri := fileURI(root, rel)
	c.didOpen(t, uri, src)
	_ = c.sync(t, 2)
	cases := []struct {
		name string
		nth  int
		want string
	}{
		{name: "y", nth: 1, want: "y : Int"},
		{name: "id", nth: 1, want: "id : Int -> Int"},
		{name: "id", nth: 0, want: "id : forall T. T -> T"},
	}
	for i, tt := range cases {
		line, ch := findIdent(t, src, tt.name, tt.nth)
		c.send(t, hoverReq(10+i, uri, line, ch))
		msg := c.read(t)
		if !idEqual(msg["id"], 10+i) {
			t.Fatalf("id = %v", msg["id"])
		}
		got := hoverValue(t, msg)
		want := "```cero\n" + tt.want + "\n```"
		if got != want {
			t.Fatalf("hover %s #%d = %q, want %q", tt.name, tt.nth, got, want)
		}
	}
}

func lspDefinition(t *testing.T, root string) {
	rel := "internal/selfhost/testdata/lsp/ok.cero"
	src := readTestdata(t, root, rel)
	libRel := "internal/selfhost/testdata/lsp/lib.cero"
	lib := readTestdata(t, root, libRel)
	c := startLSP(t, root)
	c.initialize(t, root)
	uri := fileURI(root, rel)
	c.didOpen(t, uri, src)
	_ = c.sync(t, 2)
	nLine, nChar := findIdent(t, src, "n", 1)
	declLine, declChar := findIdent(t, src, "n", 0)
	c.send(t, defReq(10, uri, nLine, nChar))
	checkLoc(t, c.read(t), uri, declLine, declChar, declChar+1)

	funLine, funChar := findIdent(t, src, "libFun", 0)
	libLine, libChar := findIdent(t, lib, "libFun", 0)
	c.send(t, defReq(11, uri, funLine, funChar))
	checkLoc(t, c.read(t), fileURI(root, libRel), libLine, libChar, libChar+len("libFun"))

	redLine, redChar := findIdent(t, src, "Red", 0)
	ctorLine, ctorChar := findIdent(t, lib, "Red", 0)
	c.send(t, defReq(12, uri, redLine, redChar))
	checkLoc(t, c.read(t), fileURI(root, libRel), ctorLine, ctorChar, ctorChar+len("Red"))
}

func lspBuiltin(t *testing.T, root string) {
	rel := "internal/selfhost/testdata/lsp/ok.cero"
	src := readTestdata(t, root, rel)
	c := startLSP(t, root)
	c.initialize(t, root)
	uri := fileURI(root, rel)
	c.didOpen(t, uri, src)
	_ = c.sync(t, 2)
	line, ch := findIdent(t, src, "stringLength", 0)
	c.send(t, defReq(10, uri, line, ch))
	msg := c.read(t)
	if !idEqual(msg["id"], 10) || msg["result"] != nil {
		t.Fatalf("definition = %v", msg)
	}
}

func lspFormatting(t *testing.T, root string) {
	rel := "internal/selfhost/testdata/lsp/messy.cero"
	src := readTestdata(t, root, rel)
	c := startLSP(t, root)
	c.initialize(t, root)
	uri := fileURI(root, rel)
	c.didOpen(t, uri, src)
	_ = c.sync(t, 2)
	c.send(t, formatReq(10, uri))
	msg := c.read(t)
	edits, ok := msg["result"].([]any)
	if !ok || len(edits) != 1 {
		t.Fatalf("edits = %v", msg["result"])
	}
	edit, ok := edits[0].(map[string]any)
	if !ok {
		t.Fatalf("edit = %v", edits[0])
	}
	want := "fn main() -> Int {\n    1\n}\n"
	if edit["newText"] != want {
		t.Fatalf("newText = %q", edit["newText"])
	}
	rng, ok := edit["range"].(map[string]any)
	if !ok {
		t.Fatalf("range = %v", edit["range"])
	}
	start, _ := rng["start"].(map[string]any)
	end, _ := rng["end"].(map[string]any)
	if !numEqual(start["line"], 0) || !numEqual(start["character"], 0) {
		t.Fatalf("start = %v", start)
	}
	if !numEqual(end["line"], strings.Count(src, "\n")) || !numEqual(end["character"], 0) {
		t.Fatalf("end = %v", end)
	}

	brokenURI := fileURI(root, "internal/selfhost/testdata/lsp/broken.cero")
	c.didOpen(t, brokenURI, "fn {\n")
	_ = c.sync(t, 11)
	c.send(t, formatReq(12, brokenURI))
	msg = c.read(t)
	edits, ok = msg["result"].([]any)
	if !ok || len(edits) != 0 {
		t.Fatalf("failed format = %v", msg["result"])
	}
}

func lspUnknown(t *testing.T, root string) {
	c := startLSP(t, root)
	c.initialize(t, root)
	c.send(t, map[string]any{
		"jsonrpc": "2.0",
		"method":  "workspace/didChangeConfiguration",
		"params":  map[string]any{},
	})
	c.send(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "textDocument/completion",
		"params":  map[string]any{},
	})
	msg := c.read(t)
	if !idEqual(msg["id"], 7) {
		t.Fatalf("id = %v", msg["id"])
	}
	checkRPCError(t, msg, -32601)
}

func lspShutdownExit(t *testing.T, root string) {
	c := startLSP(t, root)
	c.send(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "shutdown",
		"params":  nil,
	})
	msg := c.read(t)
	if !idEqual(msg["id"], 1) || msg["result"] != nil {
		t.Fatalf("shutdown = %v", msg)
	}
	c.send(t, map[string]any{
		"jsonrpc": "2.0",
		"method":  "exit",
	})
	if code := c.waitExit(t); code != 0 {
		t.Fatalf("exit = %d", code)
	}
}

func lspExitNoShutdown(t *testing.T, root string) {
	c := startLSP(t, root)
	c.send(t, map[string]any{
		"jsonrpc": "2.0",
		"method":  "exit",
	})
	if code := c.waitExit(t); code != 1 {
		t.Fatalf("exit = %d", code)
	}
}

func lspFraming(t *testing.T, root string) {
	c := startLSP(t, root)
	initBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"rootUri":      "file://" + root,
			"capabilities": map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	unkBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "no/such",
		"params":  map[string]any{},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	c.writeRaw(t, append(lspFrame(initBody), lspFrame(unkBody)...))
	initMsg := c.read(t)
	if !idEqual(initMsg["id"], 1) {
		t.Fatalf("initialize = %v", initMsg)
	}
	unk := c.read(t)
	if !idEqual(unk["id"], 2) {
		t.Fatalf("unknown = %v", unk)
	}
	checkRPCError(t, unk, -32601)

	next, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "also/missing",
		"params":  map[string]any{},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	framed := lspFrame(next)
	mid := len(framed) / 2
	c.writeRaw(t, framed[:mid])
	time.Sleep(200 * time.Millisecond)
	c.writeRaw(t, framed[mid:])
	msg := c.read(t)
	if !idEqual(msg["id"], 3) {
		t.Fatalf("split = %v", msg)
	}
	checkRPCError(t, msg, -32601)
}

func hoverReq(
	id int,
	uri string,
	line int,
	ch int,
) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "textDocument/hover",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": line, "character": ch},
		},
	}
}

func defReq(
	id int,
	uri string,
	line int,
	ch int,
) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "textDocument/definition",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": line, "character": ch},
		},
	}
}

func formatReq(id int, uri string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "textDocument/formatting",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"options":      map[string]any{},
		},
	}
}

func hoverValue(t *testing.T, msg map[string]any) string {
	t.Helper()

	result, ok := msg["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %v", msg["result"])
	}
	contents, ok := result["contents"].(map[string]any)
	if !ok {
		t.Fatalf("contents = %v", result["contents"])
	}
	if contents["kind"] != "markdown" {
		t.Fatalf("kind = %v", contents["kind"])
	}
	value, ok := contents["value"].(string)
	if !ok {
		t.Fatalf("value = %v", contents["value"])
	}
	return value
}

func checkLoc(
	t *testing.T,
	msg map[string]any,
	uri string,
	line int,
	ch int,
	endCh int,
) {
	t.Helper()

	result, ok := msg["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %v", msg["result"])
	}
	if result["uri"] != uri {
		t.Fatalf("uri = %v, want %s", result["uri"], uri)
	}
	rng, ok := result["range"].(map[string]any)
	if !ok {
		t.Fatalf("range = %v", result["range"])
	}
	start, ok := rng["start"].(map[string]any)
	if !ok {
		t.Fatalf("start = %v", rng["start"])
	}
	end, ok := rng["end"].(map[string]any)
	if !ok {
		t.Fatalf("end = %v", rng["end"])
	}
	if !numEqual(start["line"], line) || !numEqual(start["character"], ch) {
		t.Fatalf("start = %v, want %d:%d", start, line, ch)
	}
	if !numEqual(end["line"], line) || !numEqual(end["character"], endCh) {
		t.Fatalf("end = %v, want %d:%d", end, line, endCh)
	}
}

func findIdent(
	t *testing.T,
	src string,
	name string,
	nth int,
) (int, int) {
	t.Helper()

	line, ch, count := 0, 0, 0
	for i := 0; i < len(src); {
		if strings.HasPrefix(src[i:], name) && identAt(src, i, len(name)) {
			if count == nth {
				return line, ch
			}
			count++
		}
		if src[i] == '\n' {
			line++
			ch = 0
		} else {
			ch++
		}
		i++
	}
	t.Fatalf("ident %q #%d not found", name, nth)
	return 0, 0
}

func identAt(src string, i, n int) bool {
	if i > 0 && isIdentByte(src[i-1]) {
		return false
	}
	if i+n < len(src) && isIdentByte(src[i+n]) {
		return false
	}
	return true
}

func isIdentByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
