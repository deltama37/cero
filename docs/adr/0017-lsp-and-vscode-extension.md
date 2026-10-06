# ADR-0017: LSP サーバと VS Code 拡張

* Status: Accepted
* Date: 2026-10-06

## Context

ADR-0001 は、self-hosting の後に、LSP サーバ（`ceroc lsp`）と VS Code 拡張を追加するとした。

* LSP サーバは `ceroc` に組み込み、パーサ・型検査・`fmt` を再利用する。エラー表示、ホバーでの型表示、定義へのジャンプ、フォーマットなどを提供する。
* VS Code 拡張は、シンタックスハイライトと LSP クライアントだけを持つ薄い拡張とする。
* LSP で提供する機能の範囲は、追加する時点で別の ADR として決める。

LSP は標準入出力で JSON-RPC をやり取りする。v0.10 の IO（ADR-0012）には、標準入力を最後まで読む `readStdin` しかなく、メッセージを 1 つずつ読めない。
また、LSP サーバは長く動き続ける最初の Cero のプログラムである。ADR-0010 は「長く動くプログラム（LSP サーバ）を作る段階では、回収の方式を決め直す必要がある」とした。

## Decision

### 標準入力の逐次読み込み

組み込み関数 `readStdinChunk(max: Int) -> IO[String]` を追加する（Go 製と Cero 製の両方）。

* 標準入力から最大 `max` バイトを 1 回の読み込み（`fd_read`）で読み、読めたバイト列を返す。終わりに達していれば `""` を返す。`max <= 0` は実行時エラー。
* 行単位や「ちょうど n バイト」の読み込みは、Cero のコードで、この関数の上に書く。

### LSP サーバ

Cero 製 ceroc の `lsp` コマンドとして実装する。カレントディレクトリ（ワークスペースのルート）を preopen して起動する（`wasmtime run --dir=. ceroc-cero.wasm lsp [--std DIR]`）。

提供する機能:

| メソッド | 内容 |
| --- | --- |
| `initialize` / `initialized` / `shutdown` / `exit` | 起動と終了。`rootUri` をワークスペースのルートとして覚える |
| `textDocument/didOpen` / `didChange` / `didSave` / `didClose` | 文書の内容を覚える（全文の同期）。開いたときと保存したときに解析する |
| `textDocument/publishDiagnostics` | 解析の最初のエラー 1 件（ADR-0002）を、そのエラーのファイルに出す。エラーがなくなったら空にする |
| `textDocument/hover` | 識別子の型（`name : type`）。参照なら、その場所でインスタンス化した型 |
| `textDocument/definition` | 識別子が指す宣言の位置（トップレベルの関数、構築子、引数、`let`、パターン変数）。組み込み関数は位置なし |
| `textDocument/formatting` | `fmt`（ADR-0016）の結果で、文書全体を置き換える編集 |

* 解析は、開いている文書の内容（メモリの中）をエントリとし、import した module はディスクから読む。
* ホバーと定義は、最後に解析した結果を使う。保存していない変更があると位置がずれることがある。
* 位置: LSP の `character` は UTF-16 の単位だが、Cero は列をコードポイントで数える（ADR-0002）。基本多言語面の外の文字を除いて一致するので、そのまま使う。
* ワークスペースの外のファイル（WASI の preopen の外）は扱えない。標準ライブラリは、ワークスペースの中の `std/`（`--std` で変えられる）から読む。

### メモリ: 回収せず、尽きたら再起動する

LSP サーバでもメモリを回収しない（ADR-0010 を続ける）。

* 理由: 回収の方式（参照カウント、トレース型 GC）は、IR とコード生成の全体に及ぶ大きな変更で、Go 製と Cero 製の両方に同じ変更を入れる必要がある（ADR-0014）。
  解析を開いたときと保存したときだけに絞れば、通常の大きさのプログラムでは、2 GiB に達するまでに十分な回数の解析ができる。
* メモリが尽きると、サーバは trap で終わる。VS Code 拡張は、サーバが終わったら再起動し、開いている文書を送り直す（1 分間に 5 回まで）。
* Cero 製 ceroc 自身（約 1.5 万行）のような大きなプログラムを編集するときは、保存の回数に応じて再起動が起きる。回収の方式は、この運用で問題になった時点で別の ADR で決める。

### VS Code 拡張

`editors/vscode/` に置く。

* TextMate の文法による構文ハイライト（キーワード、型名、文字列・文字リテラル、コメント、数値）と、言語の設定（コメント記号、括弧の対応）。
* LSP クライアントは、npm のパッケージ（`vscode-languageclient` など）を使わず、Node.js の標準ライブラリと VS Code の API だけで書く。JSON-RPC のメッセージの組み立てと解釈は、VS Code に依存しない module に分け、Node.js のテスト（`node --test`）で、実際のサーバを相手に確かめる。
  * 理由: 外部依存を増やさない（AGENTS.md）。使う機能（診断、ホバー、定義、書式設定）が少なく、クライアントは数百行で書ける。
* 設定: `cero.wasmtimePath`（既定 `wasmtime`）、`cero.compilerWasm`（既定はワークスペースの `bin/ceroc-cero.wasm`）、`cero.stdPath`（既定 `std`）。
* 拡張の公開（Marketplace）とパッケージ化（`.vsix`）は行わない。リポジトリから開発用の拡張として読み込む。

## Consequences

* VS Code で Cero のファイルを開くと、ハイライト、エラー、ホバーの型、定義へのジャンプ、書式設定が使える。
* 大きなプログラムでは、保存の回数に応じてサーバの再起動が起きる。
* `readStdinChunk` の追加で、Go 製と Cero 製の両方の型検査・IR 生成・WebAssembly の出力に、組み込み関数が 1 つ増える。
* CI に Node.js のテスト（拡張のクライアント）が増える。Node.js は GitHub Actions の Ubuntu に入っているものを使う。
