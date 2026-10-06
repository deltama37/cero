# 設計: LSP サーバと VS Code 拡張

## 目的

ADR-0017 のとおり、`readStdinChunk` を両方のコンパイラに追加し、Cero 製 ceroc に `lsp` コマンドを実装し、`editors/vscode/` に VS Code 拡張を置く。

対象外にすること: 補完、リネーム、参照の検索、シンボルの一覧、インクリメンタルな同期、ワークスペースの外のファイル、拡張のパッケージ化と公開、メモリの回収。

## 関連する ADR

ADR-0001、ADR-0010、ADR-0012、ADR-0014、ADR-0015、ADR-0016、ADR-0017

## 前提

実装は 3 つの単位に分け、この順に行う。各単位の終わりに `make check` と `make selfhost` と `make fmt-cero-check` が通ること。

1. `readStdinChunk`（Go 製と Cero 製の両方）
2. LSP サーバ（Cero 製 ceroc）と Go のテスト
3. VS Code 拡張と Node.js のテスト、CI

## 単位 1: `readStdinChunk`

番号の並びを崩さないよう、すべて既存の列の**末尾**に足す。

| 場所 | 追加するもの |
| --- | --- |
| Go `internal/typecheck` | `BuiltinReadStdinChunk`（`BuiltinShiftRightUnsigned` の後）、名前 `readStdinChunk`、型 `(Int) -> IO[String]` |
| Go `internal/ir` | `IOReadStdinChunk`（`PrimOp` の最後）、`(Int) -> Ptr`、表示 `io.read_stdin_chunk` |
| Go `internal/lower` | IO の動作（`readFile` と同じ形で、引数は `Int`）、`$ref` |
| Go `internal/wasm` | 補助関数 `io_read_chunk`（`(i64 max) -> i32`）。IO の補助関数の列の最後に置く。`max <= 0` なら `unreachable`。`max` バイトのバッファを確保して `fd_read(0, ...)` を 1 回呼び、読めたバイト数を長さにした String を返す（`fd_read` が失敗したら `unreachable`） |
| Cero `compiler/` | 上と同じもの（型検査、IR、IR 生成、WebAssembly の出力）。差分テストで Go と一致させる |
| `README.md` | 組み込み関数の表に追加 |

テスト: Go の `TestE2EIO` に、`readStdinChunk(4)` を繰り返して標準入力を写すケース（4 バイトずつ、終わりで `""`）と、`readStdinChunk(0)` の実行時エラー。`internal/selfhost/testdata/programs/` に同じソースを追加する（段階 3・4 の差分テストの対象になる）。

## 単位 2: LSP サーバ

### module

| module | 内容 |
| --- | --- |
| `compiler/lsp/json.cero` | JSON の値、解析、出力 |
| `compiler/lsp/rpc.cero` | メッセージの読み書き（`Content-Length` の枠） |
| `compiler/lsp/analysis.cero` | 解析（読み込み・型検査）、位置から識別子を探す、ホバーと定義の答え |
| `compiler/lsp/server.cero` | メッセージの振り分けとサーバの状態 |
| `compiler/driver/load.cero` | `loadProgramWithSource(entryFile, src, stdDir)` を追加（エントリの内容をディスクから読まない以外は `loadProgram` と同じ） |
| `compiler/main.cero` | `lsp [--std DIR]` |

公開する名前には接頭辞 `ls`（構築子は `Ls...`、JSON は `Js...`）を付ける。

### JSON（`json.cero`）

```text
pub type Json = | JsNull | JsBool(Bool) | JsInt(Int) | JsString(String) | JsArray(List[Json]) | JsObject(List[Pair[String, Json]])
pub fn jsParse(s: String) -> Option[Json]
pub fn jsEncode(v: Json) -> String
pub fn jsField(v: Json, name: String) -> Option[Json]
```

* 数値は整数だけを扱う（小数・指数を含む数は `JsInt(0)` にしてよい。LSP の使う範囲には現れない）。
* 文字列の `\uXXXX` は UTF-8 にする（サロゲートの組も）。出力では `"`、`\`、制御文字（`\n` などと `\u00XX`）をエスケープし、それ以外のバイトはそのまま出す。
* オブジェクトのキーの順は、出力では組み立てた順にする。

### メッセージ（`rpc.cero`）

* 読み込み: バッファ（`String`）に `readStdinChunk(65536)` で足していき、`\r\n\r\n` までをヘッダとして `Content-Length: N` を読み（大文字小文字を区別しない。他のヘッダは無視）、続く `N` バイトを本体とする。入力が終われば終了の扱い。
* 書き込み: `Content-Length: <本体のバイト数>\r\n\r\n<本体>` を 1 回の `print` で書く。

### サーバ（`server.cero`）

状態: ルートのパス、文書の表（URI → 内容）、最後の解析の結果（URI → 読み込んだ module と `Info`。解析に失敗していれば構文木まで）、診断を出した URI の集合、`shutdown` を受けたか。

| メッセージ | 処理 |
| --- | --- |
| `initialize` | `rootUri`（なければ `rootPath`）を覚え、`{"capabilities": {"textDocumentSync": 1, "hoverProvider": true, "definitionProvider": true, "documentFormattingProvider": true}, "serverInfo": {"name": "ceroc-lsp"}}` を返す |
| `initialized` | 何もしない |
| `textDocument/didOpen` | 内容を覚えて解析し、診断を出す |
| `textDocument/didChange` | 最後の変更（全文）で内容を置き換える。解析しない |
| `textDocument/didSave` | 解析し、診断を出す |
| `textDocument/didClose` | 文書と解析の結果を忘れ、その文書の診断を空にする |
| `textDocument/hover` | 下記。答えがなければ `null` |
| `textDocument/definition` | 下記。答えがなければ `null` |
| `textDocument/formatting` | `fmFormat` が成功すれば、全体（`(0,0)` から `(行数, 0)`）を置き換える `TextEdit` 1 つの配列。失敗すれば `[]` |
| `shutdown` | `null` を返し、受けたことを覚える |
| `exit` | `shutdown` を受けていれば `exit(0)`、そうでなければ `exit(1)` |
| それ以外の要求 | エラー `{"code": -32601, "message": "method not found"}` |
| それ以外の通知 | 無視 |

* URI とパス: `file://` の後の部分を `%XX` を戻して絶対パスにし、ルートの絶対パスとその後の `/` を取り除いた相対パスを、ceroc のファイル名として使う。ルートの外なら、その文書は解析しない（診断も出さない）。出力する URI は `file://` + ルート + `/` + 相対パス。
* 解析: `loadProgramWithSource(相対パス, 内容, stdDir)` → `checkProgram`。失敗すれば、エラーのファイルの URI に、エラーの位置から 1 文字の範囲（`line - 1`、`col - 1` から `col`）、`severity: 1`、`source: "ceroc"`、`message` の診断を 1 つ出す。前回診断を出した URI のうち今回出さないものには、空の診断を出す。
* ホバー・定義の位置: カーソルの `(line, character)` を `(line + 1, character + 1)` として、その文書の構文木から、次の名前のうち位置が `[col, col + 名前の長さ)` に入るものを探す（後で見つかったものを優先しない。最初に見つかったもの）。
  * 式の `Ident`、変数のパターン、構築子のパターンの名前、引数、`let` の名前、トップレベルの関数の名前。
* ホバー: `{"contents": {"kind": "markdown", "value": "```cero\n" ++ 名前 ++ " : " ++ 型 ++ "\n```"}}`。型は、`Ident` ならその式の型（`Info` の式の型）、それ以外はシンボルの型（型パラメータがあれば `check --types` と同じ `forall ...` 付き）。
* 定義: 名前が指すシンボルの宣言の位置（`Location`）。トップレベルの関数と構築子は、宣言のある module のファイル。それ以外はその文書。組み込み関数は `null`。

### テスト（`internal/selfhost/lsp_test.go`）

`buildCompiler` でできた wasm を `wasmtime run --dir=. <wasm> lsp` として、リポジトリのルートで起動し、JSON-RPC を送って応答を確かめる。テスト用のファイルは `internal/selfhost/testdata/lsp/` に置く（`ok.cero`、`bad.cero`、`lib.cero` を import する `uses_lib.cero` など）。

| ケース | 期待 |
| --- | --- |
| `initialize` | 上の capabilities |
| `didOpen` で型エラーのある文書 | その文書の URI に診断 1 件（メッセージと範囲が Go 製のエラーと一致） |
| `didOpen` で import 先に型エラーがある文書 | import 先の URI に診断 |
| エラーのない文書 | 空の診断 |
| `didChange` で直した内容 → `didSave` | 空の診断 |
| ホバー（局所変数、多相な関数の参照、トップレベルの関数の名前） | `name : type` |
| 定義（局所変数、別の module の関数、構築子） | 正しい URI と位置 |
| 定義（組み込み関数） | `null` |
| `formatting` | 整形の結果全体を置き換える編集 |
| 知らない要求 | `-32601` |
| `shutdown` → `exit` | 終了コード 0 |
| `shutdown` なしの `exit` | 終了コード 1 |
| 1 回の読み込みに複数のメッセージ、メッセージが複数の読み込みに分かれる | 正しく処理される（書き込みを分けて送る） |

## 単位 3: VS Code 拡張

### ファイル

```text
editors/vscode/
  package.json                 名前 cero、main ./src/extension.js、engines.vscode ^1.80.0、activationEvents onLanguage:cero、
                               contributes.languages / grammars / configuration（ADR-0017 の 3 つの設定）
  language-configuration.json  コメント //、括弧 () [] {}、自動で閉じる組（" と ' を含む）
  syntaxes/cero.tmLanguage.json
  src/client.js                JSON-RPC のクライアント（vscode に依存しない）
  src/extension.js             VS Code の API との接続
  test/client.test.js          node --test
  README.md                    開発用の拡張としての読み込み方（code --extensionDevelopmentPath=editors/vscode）、設定
```

`package.json` に `dependencies` と `devDependencies` を書かない。

### `src/client.js`

```js
// LspClient speaks JSON-RPC 2.0 over a child process's stdio with
// Content-Length framing (ADR-0017).
class LspClient extends EventEmitter {
  constructor(command, args, options) // child_process.spawn の引数
  start()                             // 起動する。'exit'（code）を発行する
  request(method, params)             // Promise。応答の result で解決、error で reject。サーバが終わったら reject
  notify(method, params)
  stop()                              // shutdown → exit を送り、終了を待つ
}
// サーバからの通知は 'notification'（method, params）として発行する。
module.exports = { LspClient, encodeMessage, MessageReader };
```

`MessageReader` は、バイト列（`Buffer`）を受け取ってメッセージを取り出す（分割や連結に対応する）。

### `src/extension.js`

* 起動: 設定から `wasmtime` のパス、`compilerWasm`（`${workspaceFolder}` を置き換える）、`stdPath` を読み、`wasmtime run --dir=. <compilerWasm> lsp --std <stdPath>` を、最初のワークスペースフォルダを `cwd` として起動する。`initialize`（`rootUri` はそのフォルダ）→ `initialized`。
* 開いている `cero` の文書について `didOpen`、変更で `didChange`（全文）、保存で `didSave`、閉じたら `didClose`。
* `publishDiagnostics` を `DiagnosticCollection` に反映する。
* `HoverProvider`、`DefinitionProvider`、`DocumentFormattingEditProvider` を登録し、要求を転送する。
* サーバが予期せず終わったら再起動し、開いている文書の `didOpen` を送り直す。1 分間に 5 回を超えたら再起動をやめ、エラーメッセージを表示する。
* 拡張の停止で `stop()`。

### 構文ハイライト

`source.cero`。キーワード（`fn` `let` `if` `else` `match` `type` `import` `pub` `true` `false`）、`let!`、大文字で始まる名前（型と構築子）、関数の宣言の名前、文字列（エスケープを含む）、文字リテラル、整数、`//` コメント、演算子。

### テスト（`editors/vscode/test/client.test.js`）

* `MessageReader`: 1 つ、2 つ連結、ヘッダの途中で分割、本体の途中で分割、余分なヘッダ。
* `encodeMessage`: 非 ASCII の本体の `Content-Length` がバイト数。
* 実際のサーバ（`bin/ceroc-cero.wasm` と wasmtime があるときだけ。なければ skip）: リポジトリのルートで起動し、`initialize`、エラーのある文書の `didOpen` → 診断の通知、ホバー、`stop()` で終了コード 0。
* `extension.js` と `client.js` が構文として正しいこと（`node --check`）。

### `Makefile` と CI

```make
# vscode-test runs the VS Code extension's client tests against the Cero-written LSP server (ADR-0017).
vscode-test: selfhost-build
	node --check editors/vscode/src/extension.js
	node --test editors/vscode/test/
```

CI の `check` ジョブで、`make fmt-cero-check` の後に `make vscode-test` を実行する（Node.js は GitHub Actions の Ubuntu に入っているもの。`actions/setup-node` は使わない）。

### `README.md` と `AGENTS.md`

README に LSP と VS Code 拡張の節（`scripts/ceroc-cero lsp`、拡張の読み込み方、提供する機能、メモリの制限と再起動）。AGENTS.md の構成に `editors/vscode/`、検証コマンドに `make vscode-test` を足す。

## 完了条件

* `make check`、`make selfhost`、`make fmt-cero-check`、`make vscode-test` が通る。CI にそれぞれのステップが入る。
* 本書のテストがすべて通る。
