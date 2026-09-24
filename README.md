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

## Getting started

```bash
# Build the ceroc binary into ./bin/ceroc
make build

# Run the CLI
./bin/ceroc version
./bin/ceroc help

# Run the test suite, formatting and vet in one shot
make check
```

The compiler subcommands declared by ADR-0001 (`build`, `run`, `fmt`) are still
stubs while the v0.1 pipeline is under construction; `version` and `help` are
fully implemented.
