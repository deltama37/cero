# Cero for VS Code

Development extension for the Cero language. It is loaded from this
repository. It is not packaged or published.

From the repository root:

```bash
make selfhost-build
code --extensionDevelopmentPath=editors/vscode
```

Open a workspace that contains `bin/ceroc-cero.wasm` and `std/`. The
extension starts

```text
wasmtime run --dir=. <compilerWasm> lsp --std <stdPath>
```

in the first workspace folder. It highlights Cero, and the language server
reports the first error, hover types, go to definition, and formatting.

Settings:

* `cero.wasmtimePath` (default `wasmtime`): the wasmtime executable
* `cero.compilerWasm` (default `${workspaceFolder}/bin/ceroc-cero.wasm`):
  the language server module. `${workspaceFolder}` is replaced with the
  first workspace folder
* `cero.stdPath` (default `std`): the standard library directory

The language server does not free memory. When the process stops, the
extension starts it again and sends `didOpen` for Cero documents that are
still open. It restarts at most five times in one minute. Another stop in
that minute leaves the server stopped and shows an error.

The client tests do not need npm:

```bash
make vscode-test
```
