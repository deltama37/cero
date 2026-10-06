# ADR-0014: Cero 製 ceroc

* Status: Accepted
* Date: 2026-10-06

## Context

ADR-0001 は、Go 製 ceroc で Cero 製 ceroc をコンパイルし、Cero 製 ceroc で自身をコンパイルできた状態を自己ホストの達成とした。
`ceroc fmt`、LSP、x86-64 backend は、Cero 製 ceroc の機能として追加する（ADR-0001）。

v0.11 までで、Cero は module（ADR-0011）、String（ADR-0010）、IO（ADR-0012）、`let!` と `%` とビット演算（ADR-0013）を持ち、コンパイラを書ける。
Go 製 ceroc は非テストのコードで約 1 万行で、字句解析・構文解析・型検査（推論、usefulness）・IR 生成（特殊化、クロージャ）・WebAssembly の出力（手書きの補助関数を含む）からなる。

## Decision

### 方針: Go 製 ceroc の忠実な移植

Cero 製 ceroc は、Go 製 ceroc の各パッケージのアルゴリズムを、そのまま Cero に移す。

* **同じ入力に対して、同じ出力を出す。** コンパイルに成功するプログラムでは、出力する WebAssembly がバイト単位で一致する。失敗するプログラムでは、最初のエラー（`ファイル:行:列: メッセージ`）が一致する。
* 理由:
  * Go 製 ceroc が仕様と実装の両方の基準（オラクル）になり、差分テストで Cero 製 ceroc を検証できる。言語の仕様を別に書き起こす必要がない。
  * 自己ホストの検査（ADR-0015 で扱う）が単純になる。Go 製 ceroc の出力と Cero 製 ceroc の出力が一致するので、Cero 製 ceroc の出力で自分自身をコンパイルした結果も一致する（不動点）。
  * 別の表現（たとえばすべての値を `i64` にする一様表現）にすると、コード生成の仕様を新たに決めてテストする必要があり、移植より仕事が増える。

### Go 製 ceroc との関係

* Go 製 ceroc はブートストラップのコンパイラとして残す。Cero 製 ceroc のソースが使う言語機能は、Go 製 ceroc が常にコンパイルできなければならない。
* 言語の変更は、両方に同じ変更を入れ、差分テストで一致を保つ。
* `fmt`、LSP、x86-64 backend など、self-hosting の後に追加するツールは、Cero 製 ceroc だけに実装する（ADR-0001）。

### 置き場所と構成

* ソースはリポジトリの `compiler/` に置き、エントリ module は `compiler/main.cero` とする。module パスは `compiler/` からの相対パス（`syntax/lexer`、`check/typecheck` など）。
* Go のパッケージごとに module のまとまりを作る（`syntax/`、`types`、`check/`、`ir/`、`lower/`、`wasm/`、`driver`、`util/`）。対応は設計書で決める。
* import した名前は修飾なしで取り込まれる（ADR-0011）ので、複数の module で同じ名前の公開関数を作らない。汎用的な名前には module の略称を前に付ける（`astFormatExpr`、`irFormatExpr` など）。

### 純粋な言語での表現

Go の実装はポインタの同一性と破壊的な更新を使っている。Cero 製 ceroc では次のように置き換える。

* **ノードの番号**: 構文木の式・パターン・宣言などには、構文解析で通し番号を付ける。Go の `map[ast.Expr]...` は、番号をキーにした表にする。
* **表**: 整数と文字列をキーにした永続的な平衡二分探索木（AVL 木）を `util/` に置く。
* **単一化**: 単一化変数を番号で表し、解を「番号から型への永続的な表」（置換）に持つ。Go の `Meta.Solution` への代入は、置換への追加になる。
* **状態とエラー**: 型検査や IR 生成の状態（表、置換、次の番号）と最初のエラーは、`State -> Result` の関数として表すモナドで引き回し、`let!`（ADR-0013）で書く。モナドの `bind` は、それを使う module が import する。
* **文字列の組み立て**: 出力（AST や IR の表示、WebAssembly のバイト列）は、文字列の木を作ってから 1 回で平らにする（ADR-0010 の 2 乗のコピーを避ける）。

### コマンド

Cero 製 ceroc は WASI のコマンド（`main: () -> IO[Unit]`）で、次のサブコマンドを持つ。

| コマンド | 出力 |
| --- | --- |
| `build FILE [-o OUT]` | WebAssembly（既定の出力先は Go 製と同じ規則） |
| `parse FILE` | Go 製の `ast.FormatFile` と同じ表示を、module ごとに出す（差分テスト用） |
| `check FILE` | 型検査が通れば何も出さない（差分テスト用） |
| `ir FILE` | Go 製の `ir.Format` と同じ表示（差分テスト用） |

* エラーは標準エラー出力に Go 製と同じ形式で書き、終了コード 1 で終わる。
* 標準ライブラリは、カレントディレクトリの `std/` から読む（`--std DIR` で変えられる）。Go 製は `std/` を埋め込んでいるが、Cero には埋め込みの仕組みがない。
* 実行はリポジトリのルートで `wasmtime run --dir=. ceroc.wasm build examples/fib.cero` のように行う（WASI のファイルは preopen したディレクトリの中に限られる）。

### 段階と差分テスト

実装は 4 つの段階に分け、各段階を 1 つの PR として main にマージする。

| 段階 | 範囲 | 差分テスト |
| --- | --- | --- |
| 1 | 汎用の module（表、文字列の木）、字句解析、構文解析、AST の表示、module の読み込み、`parse` | `examples/`・`std/`・構文エラーの集まりで、Go 製の表示とエラーに一致 |
| 2 | 型、単一化、型検査（推論、usefulness、module）、`check` | 型エラーの集まりで最初のエラーが一致、すべてのサンプルが通る |
| 3 | IR、IR 生成（特殊化、クロージャ、IO）、`ir` | IR の表示が一致 |
| 4 | WebAssembly の出力（補助関数を含む）、`build` | 出力のバイト列が一致 |

差分テストは Go のテスト（`internal/selfhost`）とし、Go 製 ceroc で `compiler/main.cero` をビルドして wasmtime で動かし、同じ入力に対する Go のパッケージの結果と比べる。wasmtime がなければ skip する（既存の E2E と同じ）。

### メモリとスタック

* メモリは回収しない（ADR-0010）。
* WebAssembly のスタックは深くできないので、リストをたどる処理は末尾再帰で書く。構文木の深さに比例する再帰は許す。

## Consequences

* Cero 製 ceroc の正しさは、Go 製 ceroc との一致で確かめられる。Go 製 ceroc の不具合も、そのまま移植される（差分テストでは見つからない）。
* 言語を変えるときは、2 つのコンパイラを同時に直す必要がある。
* Cero 製 ceroc は Go 製より遅く、メモリも多く使う見込みである。self-hosting（ADR-0015）で測る。
* 名前の衝突を避けるため、公開する名前が長くなる。
* リポジトリに `compiler/` と `internal/selfhost` が増える。CI の時間が増える。
