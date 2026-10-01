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

The compiler pipeline is `lexer → parser → type checker → IR → WebAssembly`.
The language grows one version at a time; each step is recorded as an ADR,
with design documents in [`docs/design/`](docs/design/):

* v0.1: `Int` / `Bool`, first-class functions and recursion
  ([ADR-0002](docs/adr/0002-v0.1-syntax-and-wasm-abi.md);
  `examples/fib.cero`, `examples/higher_order.cero`)
* v0.2: algebraic data types and pattern matching
  ([ADR-0003](docs/adr/0003-v0.2-algebraic-data-types-and-pattern-matching.md);
  `examples/list.cero`)
* v0.3: parametric polymorphism such as `fn map[T, U]` and `type Option[T]`
  ([ADR-0004](docs/adr/0004-v0.3-parametric-polymorphism.md);
  `examples/option.cero`, `examples/generic_list.cero`)

`ceroc fmt` is not implemented yet.

## License

Cero is released under the [MIT License](LICENSE).
