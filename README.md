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

The bootstrap compiler and CLI, `ceroc`, is written in Go (per ADR-0001).
It remains the bootstrap; `compiler/` is the same compiler written in Cero.
See [Self-hosting](#self-hosting).

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
* v0.4: let polymorphism and type inference for anonymous functions, such as
  `let id = fn(x) { x }`
  ([ADR-0006](docs/adr/0006-v0.4-let-polymorphism-and-local-inference.md);
  `examples/inference.cero`)
* v0.5: type inference for top-level functions, such as `fn identity(x) { x }`
  ([ADR-0007](docs/adr/0007-v0.5-top-level-type-inference.md);
  `examples/toplevel_inference.cero`)
* v0.6: closures that capture outer variables, such as `fn(x) { x + n }`
  ([ADR-0008](docs/adr/0008-v0.6-closures.md);
  `examples/closures.cero`)
* v0.7: nested patterns, such as `Cons(_, Cons(y, _))`
  ([ADR-0009](docs/adr/0009-v0.7-nested-patterns.md);
  `examples/nested_patterns.cero`)
* v0.8: the built-in `String` type, string literals, `++`, and functions such
  as `stringLength` and `intToString`. `List` remains a type the program
  declares
  ([ADR-0010](docs/adr/0010-v0.8-string-list-and-memory.md);
  `examples/strings.cero`)
* v0.9: modules. `import "std/list"` brings a module's public names in
  unqualified, and `pub fn` / `pub type` are what an importer can see. A
  module's own declarations hide imported names. Two imports that export
  the same name are an error, and imports are not transitive. The standard
  library, written in Cero, is `std/list` (`List`, `length`, `reverse`,
  `map`, `filter`, `foldl`, `foldr`, `append`, `concat`, `any`, `all`,
  `range`, `sum`, `take`, `drop`), `std/option` (`Option`, `withDefault`,
  `mapOption`, `andThen`, `isSome`, `isNone`), `std/pair` (`Pair`, `fst`,
  `snd`), and `std/string` (`join`, `startsWith`, `endsWith`, `isDigit`,
  `isAlpha`, `isSpace`)
  ([ADR-0011](docs/adr/0011-v0.9-modules.md);
  `examples/modules/main.cero`, `examples/stdlib.cero`)
* v0.10: the `IO[T]` type and the value `()`. `pure` and `bind` build
  actions; `print`, `eprint`, `readStdin`, `readFile`, `fileExists`,
  `writeFile`, `argCount`, `argAt` and `exit` are the built-in actions.
  A value of `IO[T]` does nothing until it is run, and the only way to run
  one is for `main` to return it. `main: () -> IO[Unit]` is a WASI command:
  the module imports `wasi_snapshot_preview1` and exports `_start` and
  `memory`. `ceroc run file.cero args...` preopens the current directory
  (`wasmtime run --dir=.`) and passes the arguments, standard input, standard
  output, standard error and the exit code through. `std/io` adds `then`,
  `mapIO`, `println`, `eprintln`, `forEach`, `args` and `unit`
  ([ADR-0012](docs/adr/0012-v0.10-io.md);
  `examples/io/hello.cero`, `examples/io/cat.cero`, `examples/io/wc.cero`)
* v0.11: small conveniences for writing a compiler. `%` is integer
  remainder (the sign follows the dividend; a zero divisor traps). A
  character literal such as `'a'` or `'\n'` is the `Int` of that one
  byte. `bitAnd`, `bitOr`, `bitXor`, `shiftLeft`, `shiftRight` and
  `shiftRightUnsigned` are the 64-bit bit operations. `let! x = e` is
  sugar for `bind(e, fn(x) { ... })`, and a module may declare its own
  `bind`. A top-level declaration or an import hides a built-in of the
  same name. `let Pair(a, b) = e` is sugar for a one-arm `match`, and
  the pattern must be exhaustive. A parameter may be named `_`, more
  than once
  ([ADR-0013](docs/adr/0013-v0.11-conveniences-for-writing-a-compiler.md);
  `examples/conveniences.cero`)

`ceroc fmt` is not implemented yet.

## Self-hosting

`compiler/` is ceroc written in Cero
([ADR-0014](docs/adr/0014-cero-written-ceroc.md)). It follows the Go
compiler's algorithms, so a successful compile emits the same WebAssembly
bytes. The Go compiler stays as the bootstrap: with Go and wasmtime, the
self-hosted compiler is rebuilt from source, and no WebAssembly binary is
committed
([ADR-0015](docs/adr/0015-self-hosting.md)).

`make selfhost` checks that fixed point with three builds:

```text
stage1.wasm  Go ceroc compiling compiler/main.cero
stage2.wasm  stage1.wasm compiling compiler/main.cero
stage3.wasm  stage2.wasm compiling compiler/main.cero
```

Self-hosting means `stage2.wasm` and `stage3.wasm` are identical. The port
is faithful when `stage1.wasm` matches them too. The command prints each
stage's size and time, and, when `/usr/bin/time -v` is available, the peak
resident set of the stage 2 build. It exits with an error if either pair
differs.

`make selfhost-build` writes stage 1 to `bin/ceroc-cero.wasm`.
`./scripts/ceroc-cero` runs that module with `wasmtime run --dir=.`, so
paths are relative to the current directory:

```bash
make selfhost-build
./scripts/ceroc-cero build examples/fib.cero
wasmtime run --invoke main examples/fib.wasm   # prints 55
```

## License

Cero is released under the [MIT License](LICENSE).
