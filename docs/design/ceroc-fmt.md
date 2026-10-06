# 設計: `ceroc fmt`

## 目的

ADR-0016 のとおり、Cero 製 ceroc に `fmt` コマンドを追加し、リポジトリの Cero のソースを整形して、CI で整形済みであることを検査する。

対象外にすること: 長い行の折り返し、設定項目、Go 製 ceroc への実装、ブロックコメント（言語にない）。

## 関連する ADR

ADR-0001、ADR-0013（書き換える構文）、ADR-0014、ADR-0015、ADR-0016

## 変更・追加するファイル

| ファイル | 内容 |
| --- | --- |
| `compiler/syntax/lexer.cero` | コメントとトークンの元の文字列を返す字句解析 `lexWithTrivia` を追加（既存の `lex` の結果は変えない） |
| `compiler/fmt/trivia.cero`（新規） | コメントの分類と、項目への割り当て |
| `compiler/fmt/fmt.cero`（新規） | 構文木の印刷 |
| `compiler/main.cero` | `fmt` コマンド |
| `internal/cli/cli.go`, `cli_test.go` | Go 製の `ceroc fmt` の案内メッセージ |
| `Makefile` | `fmt-cero`、`fmt-cero-check` |
| `.github/workflows/ci.yml` | `make fmt-cero-check` |
| `internal/selfhost/selfhost_test.go`, `testdata/fmt/` | テスト |
| `examples/`、`std/`、`compiler/` の `.cero` | 整形した結果に置き換える |
| `README.md` | `fmt` の使い方 |

## 字句解析の拡張

```text
pub type Comment = | Comment(Pos, String, Bool)          // 位置、"//" を含む本文（行末の改行を除く）、行コメントか（その行でコメントより前が空白だけ）
pub type RawToken = | RawToken(Pos, Kind, String)        // 位置、種類、ソースの元の文字列（文字列・文字リテラルは引用符を含む）
pub fn lexWithTrivia(src: String) -> Result[Pair[List[RawToken], List[Comment]]]
```

`lex` と同じ規則で走査し、エラーも同じにする。差分テスト（段階 1〜4）に影響しないよう、`lex` の動作は変えない（内部で共通の処理を使ってよい）。

## 整形の流れ

```text
fmtSource(src):
    file = parseFile(src, 0)                   // 構文エラーならそのエラー
    raws, comments = lexWithTrivia(src)
    tokAt = 位置 -> RawToken の表
    項目の列を作る（下記）、コメントを割り当てる、印刷する
    return 文字列
```

### 項目と位置

各項目の「開始行」と「終了行」を、構文木の位置とトークンの列から求める。

* トップレベルの宣言: 開始は `pub` か `fn` / `type` のトークンの行。終了は、宣言の最後のトークン（`}` または最後の構築子の最後のトークン）の行。
* 構築子: 開始は `|` の行（なければ構築子の名前の行）、終了はその構築子の最後のトークンの行。
* `let`（`let!` とパターンの `let` を含む）: 開始は `let` の行、終了は右辺の最後のトークンの行。
* ブロックの結果の式、`match` のアーム: 開始は最初のトークン、終了は最後のトークンの行。

最後のトークンの位置は、項目の開始位置から括弧の対応をたどって求めてよい（トークンの列の中で、開始位置以降、次の項目の開始位置より前の最後のトークン）。

### 書き換えた構文の復元

* `let! x = e`: 構文木の `Call` の位置のトークンが `let` なら、`bind(e, fn(x) { 残り })` を `let! x = e` と、`残り` のブロックの中身として印刷する（`残り` の `let` と結果は、元のブロックの続きとして同じインデントで並べる）。引数の型注釈があれば `let! x: T = e`。
* `let P = e`: `Match` の `Let` が真なら、`let P = e` と、アームの本体（ブロックならその中身）を続きとして印刷する。
* 整数リテラル: その位置のトークンが文字リテラルなら、元の文字列（`'a'`）を出す。それ以外は元の文字列。
* 文字列リテラルとパターン: 元の文字列。

### 式の印刷

* 優先順位（低い順）: `||`、`&&`、比較（`==` `!=` `<` `<=` `>` `>=`）、加法（`+` `-` `++`）、乗法（`*` `/` `%`）、単項（`-` `!`）、呼び出しと一次式。`parser.cero` の規則と同じにする（結合性も含む）。
* 二項演算の子は、子の優先順位が親より低いとき、または同じ優先順位で右側にあるとき（左結合）に括弧で囲む。比較が結合しない規則なら、比較の子の比較は括弧で囲む。
* 単項演算の被演算子は、二項演算なら括弧で囲む。
* 呼び出しの関数の部分は、`Ident`・`Call`・括弧の要らない一次式以外（匿名関数など）なら括弧で囲む。
* `if`・`match`・複数行の匿名関数は、引数や二項演算の中にあっても、その場で複数行として印刷する（1 行目は現在の行に続け、2 行目以降は現在の項目のインデントを基準にする）。
* 引数の列は `, ` で区切り、末尾の `,` は付けない。

### 型の印刷

`Int`、`List[Int]`、`(A, B) -> C`、`A -> B -> C`（右結合）、`(A -> B) -> C`（左が関数なら括弧）。引数が 1 つの関数型は `A -> B`、0 個は `() -> B`。

### コメントの割り当て

ADR-0016 の規則のとおり。実装の目安:

```text
コメントを位置の順に並べる
各ブロック（ファイル、型の構築子の列、ブロックの中身、match のアームの列）について、項目の開始行の列を持つ
行コメント c: c の行より後に始まる、最も内側のブロックの最初の項目の前。なければそのブロックの最後（`}` の前）
行末コメント c: c と同じ行で始まる項目があれば、その項目の最初の行の末尾。なければ行コメントと同じ扱い
```

どの項目にも割り当てられないコメントが残らないこと（残ったらファイルの末尾に出す）をテストで確かめる。

## コマンド

* `fmt FILE`: 整形した結果を標準出力に書く。
* `fmt -w FILE...`: 各ファイルを整形した結果で上書きする（変わらなければ書かない）。
* `fmt --check FILE...`: 整形済みでないファイルの名前を 1 行ずつ標準出力に書き、1 つでもあれば終了コード 1。
* 構文エラーは、他のコマンドと同じ形式で標準エラー出力に書き、終了コード 1。
* `fmt` は import をたどらない（ファイルごとに独立に整形する）。

## Go 製の `ceroc fmt`

`ceroc fmt` は、標準エラー出力に `ceroc fmt: the formatter is part of the Cero-written compiler; run scripts/ceroc-cero fmt FILE (ADR-0016)` を書いて、終了コード 3（今の「未実装」と同じ）で終わる。`cli_test.go` の期待値を直す。

## `Makefile` と CI

```make
CERO_SOURCES = $(shell find examples std compiler -name '*.cero' | sort)

# fmt-cero formats every Cero source in the repository with the Cero-written compiler (ADR-0016).
fmt-cero: selfhost-build
	./scripts/ceroc-cero fmt -w $(CERO_SOURCES)

fmt-cero-check: selfhost-build
	./scripts/ceroc-cero fmt --check $(CERO_SOURCES)
```

CI の `check` ジョブで、`make selfhost` の後に `make fmt-cero-check` を実行する。

## リポジトリの整形

`make fmt-cero` で `examples/`、`std/`、`compiler/` を整形し、その結果をコミットする（整形だけの commit にする）。その後で `make check` と `make selfhost` が通ることを確かめる。

## テスト（`internal/selfhost`）

* `TestFmtGolden`: `testdata/fmt/*.in.cero` を整形した結果が、`*.out.cero` と一致する。ケース: 型宣言、関数の注釈の有無、`if` / `else if` / `else`、`match` とアームのブロック、1 行と複数行の匿名関数、優先順位と括弧（`(a + b) * c`、`a - (b - c)`、`-(a + b)`、`!(a && b)`、`(fn(x) { x })(1)`）、`let!`、`let` のパターン、文字リテラルとエスケープを含む文字列、空行の保持と圧縮、行コメントと行末コメント（宣言の前、構築子の間、ブロックの最後、ファイルの末尾、式の途中）、`import` と `pub`。目安 25 ケース。
* `TestFmtProperties`: リポジトリのすべての `.cero`（`examples/`、`std/`、`compiler/`、`testdata/fmt/*.in.cero`、`testdata/check_ok/`、`testdata/programs/`）について、
  1. 整形の前と後で、Go 製の `parser.ParseFile` → `ast.FormatFile` が一致する。
  2. 整形した結果をもう一度整形しても変わらない。
  3. 前と後で、コメントの本文の列（`//` の後の文字列、ソースの順）が一致する（Go 側で字句的に集める）。
* `TestFmtCheck`: `--check` が整形済みのファイルで 0、そうでないファイルで 1 と名前を返す。`-w` が書き換える（一時ディレクトリはリポジトリの下の `testdata/.out/` を使う）。

## 完了条件

* `make check`、`make selfhost`、`make fmt-cero-check` が通る。CI に `fmt-cero-check` のステップが入る。
* 本書のテストがすべて通る。
