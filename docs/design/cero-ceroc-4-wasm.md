# 設計: Cero 製 ceroc 段階 4（WebAssembly の出力と `build`）

## 目的

ADR-0014 の段階 4 として、Go 製の `internal/wasm` を Cero に移植し、`build` コマンドで WebAssembly を書き出す。出力が Go 製とバイト単位で一致することを差分テストで確かめる。

対象外にすること:

* self-hosting の不動点の CI（次の段階、ADR-0015）
* 出力の最適化（Go にないもの）

## 関連する ADR

* ADR-0002〜0013（WebAssembly の表現。Go の実装が基準）
* ADR-0014: 移植の方針

## 基準となる Go のコード

| Go | Cero |
| --- | --- |
| `internal/wasm/leb128.go` | `compiler/wasm/leb128.cero` |
| `internal/wasm/encode.go` | `compiler/wasm/encode.cero` |
| `internal/wasm/strfunc.go` | `compiler/wasm/strfunc.cero` |
| `internal/wasm/iofunc.go` | `compiler/wasm/iofunc.cero` |
| `internal/cli/cli.go` の `defaultOutput` | `compiler/main.cero` |

**出力のバイト列は Go と完全に同じにする。** 型の添字の順、補助関数の順と本体、データセグメント、import、エクスポート、局所変数の宣言のまとめ方まで、Go のコードに従う。

## 名前の規則

`wasm/*` の公開する構築子は `Wa...`、関数は `wa...`（公開する `encodeModule` を除く）。

## バイト列の組み立て（`compiler/wasm/bytes.cero`、新規）

```text
// 長さを持つバイト列の木。セクションの大きさ（LEB128）を前に付けるために長さを保つ。
pub type Bytes = | Bytes(Int, Doc)                 // バイト数、内容（util/doc の Doc）
pub fn waEmpty() -> Bytes
pub fn waByte(b: Int) -> Bytes                     // 0..255
pub fn waBytes(bs: List[Int]) -> Bytes
pub fn waString(s: String) -> Bytes                // s のバイト列そのもの
pub fn waCat(a: Bytes, b: Bytes) -> Bytes
pub fn waConcat(xs: List[Bytes]) -> Bytes
pub fn waLength(b: Bytes) -> Int
pub fn waToString(b: Bytes) -> String              // docToString
```

* 1 バイトの文字列は `stringFromByte` で作る。同じバイトを何度も使うので、0〜255 の 1 バイトの文字列を最初に作って表に置き、再利用してよい。
* LEB128（`leb128.cero`）は、符号なし・符号つきとも `Bytes` を返す（`bitAnd`、`shiftRight`、`shiftRightUnsigned` を使う）。

## `compiler/wasm/encode.cero` など

```text
pub fn encodeModule(m: IrModule) -> String          // Go の wasm.Encode。結果のバイト列を String で返す
```

* Go の `encoder` の状態（作業中の関数の結果の型、作業用の局所変数、補助関数の番号の表、型の添字の表、データセグメントのアドレス）は、関数の引数で渡すか、状態モナドで引き回す。どちらでもよい。
* Go の `walk`（前順の走査）は、末尾再帰か作業リストで書く（式の木の深さには比例してよい）。
* 補助関数（`alloc`、`new`、文字列、IO）の本体は、Go が手で並べているバイト列を、同じ順で Cero のリストに移す。`strfunc.go` と `iofunc.go` は大部分がバイト列の定数なので、機械的に変換してよい（変換のスクリプトはコミットしなくてよい）。

## `compiler/main.cero` の変更

* `build FILE [-o OUT]`: `loadProgram` → `checkProgram` → `lowerProgram` → `encodeModule` の後、結果を `OUT` に `writeFile` する。`-o` がなければ Go の `defaultOutput` と同じ規則で決める（`x.cero` → `x.wasm`、それ以外は `+ ".wasm"`）。
* `-o -` なら、標準出力に書く（差分テスト用）。
* エラーは段階 1〜3 と同じ。

## `Makefile`

* `selfhost-build` は、Go 製でビルドした `bin/ceroc-cero.wasm` を作る（段階 1 のまま）。

## 差分テスト（`internal/selfhost`）

`TestBuild`:

* 対象: 段階 3 の `TestIR` と同じ 280 件（`examples/`、`compiler/main.cero`、`testdata/check_ok/`、`testdata/programs/`）。
* Cero 製の `build FILE -o -` の標準出力（バイト列）が、Go の `driver.CompileProgram(...).Wasm` と一致し、終了コードが 0 であること。
* 一致しないときは、最初に異なるバイトの位置と、その前後 16 バイトを `t.Errorf` で示す。

`TestBuildRuns`（少数）:

* `examples/fib.cero` などいくつかについて、Cero 製で書き出した WebAssembly を wasmtime で実行し、Go 製と同じ出力になること（バイト列が一致すれば同じになるが、`-o FILE` の経路の確認を兼ねる。出力先はリポジトリのルートの下の一時ディレクトリ `internal/selfhost/testdata/.out/`（`.gitignore` に追加）とし、テストの終わりに消す）。

## 完了条件

* `make check` が通る（wasmtime がある状態で、`internal/selfhost` を skip しない）。
* Cero 製 ceroc の `build compiler/main.cero -o -` が、Go 製の `compiler/main.cero` のビルド結果とバイト単位で一致する（自己ホストの不動点の前提）。かかった時間とピークのメモリを報告する。
* 本書の差分テストがすべて通る。
