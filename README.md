# cero

**Cero** is a small, from-scratch pure functional programming language built to
understand language implementation—lexing, parsing, type checking / inference,
an intermediate representation, code generation to WebAssembly, and eventually
self-hosting.

`cero` means "zero" in Spanish: the language starts from zero and grows one
minimal, well-understood step at a time.

See [`docs/adr/0001-cero-language-initial-policy.md`](docs/adr/0001-cero-language-initial-policy.md)
for the full design rationale and roadmap.

## Toolchain

The bootstrap compiler and CLI, `ceroc`, is written in Go (per ADR-0001) and
will later be re-implemented in Cero itself to reach self-hosting.

* Go 1.22+
* [wasmtime](https://wasmtime.dev/) for `ceroc run` and the end-to-end tests
  (the end-to-end tests are skipped when `wasmtime` is not in `PATH`)

## Getting started

```bash
# Build the ceroc binary into ./bin/ceroc
make build

# Compile Cero to WebAssembly and run it
./bin/ceroc build examples/fib.cero          # writes examples/fib.wasm
wasmtime run --invoke main examples/fib.wasm # prints 55
./bin/ceroc run examples/fib.cero            # compile + run in one step

# Run the test suite, formatting and vet in one shot
make check
```

The v0.1 pipeline (`lexer → parser → type checker → IR → WebAssembly`) is
described in [`docs/adr/0002-v0.1-syntax-and-wasm-abi.md`](docs/adr/0002-v0.1-syntax-and-wasm-abi.md)
and [`docs/design/`](docs/design/). `ceroc fmt` is not implemented yet.
