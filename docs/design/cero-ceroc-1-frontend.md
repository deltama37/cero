# 設計: Cero 製 ceroc 段階 1（字句解析・構文解析・module の読み込み）

## 目的

ADR-0014 の段階 1 として、`compiler/` に Cero 製 ceroc の土台（汎用の module、字句解析、構文解析、AST の表示、module の読み込み、`parse` コマンド）を作り、Go 製 ceroc との差分テストを追加する。

対象外にすること:

* 型検査、IR 生成、WebAssembly の出力（段階 2〜4）
* `check` / `ir` / `build` コマンド（段階 2〜4。段階 1 では「not yet implemented」で終了コード 3）
* `strconv.Quote` の、ASCII 以外の表示できない文字の扱い（下記）

## 関連する ADR

* ADR-0011: module の読み込み、module パス、エラー
* ADR-0013: `let!`、`let` のパターン、文字リテラル
* ADR-0014: 本書の方針

## 基準となる Go のコード

段階 1 で移植するのは次のファイルである。**アルゴリズム、エラーメッセージ、エラーの位置、表示は、すべて Go のコードと同じにする。** 本書に書いていない細部は Go のコードに従う。

| Go | Cero |
| --- | --- |
| `internal/diag/diag.go` | `compiler/syntax/diag.cero` |
| `internal/token/token.go` | `compiler/syntax/token.cero` |
| `internal/lexer/lexer.go` | `compiler/syntax/lexer.cero` |
| `internal/ast/ast.go` | `compiler/syntax/ast.cero` |
| `internal/parser/parser.go` | `compiler/syntax/parser.cero` |
| `internal/ast/print.go` | `compiler/syntax/print.cero` |
| `internal/driver/driver.go` の `load` | `compiler/driver/load.cero` |
| `internal/cli/cli.go`（コマンドの振り分け） | `compiler/main.cero` |

## 汎用の module（`compiler/util/`）

```text
// util/intmap.cero: Int をキーにした AVL 木
pub type IntMap[V] = | IMEmpty | IMNode(IntMap[V], Int, V, IntMap[V], Int)   // 左、キー、値、右、高さ
pub fn imEmpty[V]() -> IntMap[V]
pub fn imInsert[V](m: IntMap[V], k: Int, v: V) -> IntMap[V]     // 既にあれば値を置き換える
pub fn imLookup[V](m: IntMap[V], k: Int) -> Option[V]
pub fn imMember[V](m: IntMap[V], k: Int) -> Bool
pub fn imSize[V](m: IntMap[V]) -> Int
pub fn imToList[V](m: IntMap[V]) -> List[Pair[Int, V]]          // キーの昇順

// util/strmap.cero: String をキーにした AVL 木（stringCompare の順）
pub type StrMap[V] = ...（同じ形）
pub fn smEmpty / smInsert / smLookup / smMember / smSize / smToList   // 上と同じ意味

// util/doc.cero: 出力用の文字列の木
pub type Doc = | DEmpty | DText(String) | DCat(Doc, Doc)
pub fn docText(s: String) -> Doc
pub fn docCat(a: Doc, b: Doc) -> Doc
pub fn docConcat(ds: List[Doc]) -> Doc
pub fn docJoin(ds: List[Doc], sep: String) -> Doc
pub fn docToString(d: Doc) -> String
```

* `docToString` は、木の葉を左から順にリストに集め（末尾再帰）、隣り合う 2 つずつを `++` でつなぐ操作を 1 つになるまで繰り返す（コピーの量が全体の長さ × log(葉の数) に収まる）。
* 引数のない関数（`imEmpty()`）にするのは、`IMEmpty` を値として使うと型引数が決まらない場面があるため（ADR-0004）。構築子を直接使ってもよい。
* `std/list`、`std/option`、`std/pair`、`std/string` は、そのまま import して使う。

## `compiler/syntax/`

### `diag.cero`

```text
pub type Pos = | Pos(Int, Int)                      // 行、列（どちらも 1 始まり。列はコードポイントで数える）
pub type Error = | Error(String, Pos, String)       // ファイル（空なら付けない）、位置、メッセージ
pub fn errorf(pos: Pos, msg: String) -> Error       // File は ""
pub fn errorWithFile(e: Error, file: String) -> Error
pub fn formatError(e: Error) -> String              // Go の (*diag.Error).Error() と同じ
```

### `token.cero`

Go の `token.Kind` の各値を、同じ名前の構築子にする（`EOF`、`Ident`、`Int`、`String`、`Char`、`Fn`、…）。
`pub fn kindString(k: Kind) -> String` は Go の `Kind.String()` と同じ文字列を返す。

```text
pub type Token = | Token(Kind, String, Pos)          // 種類、テキスト（文字列・文字リテラルはデコード後のバイト列）、位置
```

### `lexer.cero`

```text
pub fn lex(src: String) -> Result[List[Token]]       // 最後は EOF のトークン
// Result は diag.cero に置く: pub type Result[T] = | Ok(T) | Err(Error)
```

* Go の `lexer.go` と同じ規則・同じエラー（メッセージと位置）で、トークンの列を作る。位置の列は UTF-8 の継続バイト（`0x80`〜`0xBF`）を数えない。
* 走査は添字による末尾再帰で行い、トークンは逆順に集めてから `reverse` する。
* 整数リテラルの範囲外（`integer literal out of range`）は Go と同じ位置とメッセージにする。値の計算でオーバーフローを検出する（`9223372036854775807` は通り、`9223372036854775808` はエラー）。

### `ast.cero`

式・パターン・`let`・引数・宣言には通し番号（**ノード番号**、`Int`）を持たせる。番号は構文解析が振り、プログラム全体（すべての module）で一意にする。表示には現れない。

```text
pub type TypeExpr =
    | NamedType(Pos, String, List[TypeExpr])
    | FuncType(Pos, List[TypeExpr], TypeExpr)

pub type Expr =
    | IntLit(Int, Pos, Int)                           // 番号、位置、値
    | StringLit(Int, Pos, String)
    | BoolLit(Int, Pos, Bool)
    | UnitLit(Int, Pos)
    | Ident(Int, Pos, String)
    | Unary(Int, Pos, Kind, Expr)                     // 演算子は token の Kind
    | Binary(Int, Pos, Pos, Kind, Expr, Expr)         // 位置、演算子の位置
    | Call(Int, Pos, Expr, Pos, List[Expr])           // 位置、関数、'(' の位置、引数
    | If(Int, Pos, Expr, Block, Expr)                 // else は BlockE か If
    | BlockE(Block)
    | FuncLit(Int, Pos, List[Param], Option[TypeExpr], Block)
    | Match(Int, Pos, Expr, List[Arm], Bool)          // 最後は Go の MatchExpr.Let

pub type Block = | Block(Int, Pos, List[LetStmt], Expr)      // 番号は式としての番号
pub type LetStmt = | LetStmt(Int, Pos, String, Pos, Option[TypeExpr], Expr)
pub type Param = | Param(Int, Pos, String, Option[TypeExpr])
pub type Arm = | Arm(Pattern, Expr)

pub type Pattern =
    | WildP(Int, Pos)
    | VarP(Int, Pos, String)
    | CtorP(Int, Pos, String, List[Pattern])
    | IntP(Int, Pos, Int)
    | StrP(Int, Pos, String)
    | BoolP(Int, Pos, Bool)

pub type TypeParam = | TypeParam(Pos, String)
pub type CtorDecl = | CtorDecl(Pos, String, List[TypeExpr])
pub type TypeDecl = | TypeDecl(Int, Pos, Bool, String, Pos, List[TypeParam], List[CtorDecl])   // 番号、位置、pub、名前、名前の位置、型パラメータ、構築子
pub type FuncDecl = | FuncDecl(Int, Pos, Bool, String, Pos, List[TypeParam], List[Param], Option[TypeExpr], Block)
pub type Import = | Import(Pos, String, Pos)
pub type File = | File(List[Import], List[TypeDecl], List[FuncDecl])

pub fn exprId(e: Expr) -> Int
pub fn exprPos(e: Expr) -> Pos            // Go の Position() と同じ
pub fn patternId(p: Pattern) -> Int
pub fn patternPos(p: Pattern) -> Pos
pub fn typeExprPos(t: TypeExpr) -> Pos
```

Go で同じ AST ノードを 2 か所から指している箇所（あれば）は、同じ番号を持つ値にする。`let!` の書き換え（ADR-0013）で作るノードにも新しい番号を振る。

### `parser.cero`

```text
pub fn parseFile(src: String, firstId: Int) -> Result[Pair[File, Int]]   // 構文木と、次に使う番号
```

* Go の `parser.go` の関数を 1 つずつ移す（`parseFuncDecl`、`parseBlockRest`、`parseLeft`、`parsePattern` など）。同じ入力に同じ構文木（同じ形・同じ位置）と同じエラーを返す。
* 状態（残りのトークンのリストと次の番号）を引き回すモナドを `parser.cero` の中に定義し、`let!` で書く。

```text
type P[A] = | P(PState -> PResult[A])
type PState = | PState(List[Token], Int)
type PResult[A] = | POk(A, PState) | PErr(Error)
fn bind[A, B](m: P[A], f: A -> P[B]) -> P[B]       // 非公開（parser.cero の中だけで使う）
fn pure[A](x: A) -> P[A]                             // 組み込みの pure を隠す（ADR-0013）
```

### `print.cero`

```text
pub fn astFormatFile(f: File) -> String     // Go の ast.FormatFile と同じ文字列
pub fn astFormatExpr(e: Expr) -> String
```

* Go の `print.go` を移す。内部では `Doc` を組み立てて最後に `docToString` する。
* 文字列リテラルとパターンの表示は Go の `strconv.Quote` と同じにする。ASCII は Go と完全に同じにする（`\a` `\b` `\f` `\n` `\r` `\t` `\v` `\\` `\"`、それ以外の制御文字と `0x7F` は `\x` 2 桁の小文字の 16 進）。
  正しい UTF-8 の 2〜4 バイトの並びはそのまま出力する（Go は表示できる文字ならそのまま出す）。正しくない UTF-8 のバイトは `\x` 2 桁にする。
  Go が `\u` で出す「表示できない ASCII 以外の文字」は対象外とし、差分テストの入力に含めない。

## `compiler/driver/load.cero`

```text
pub type Module = | Module(String, String, File, List[String])   // module パス、ファイル名、構文木、import した module のファイル名（Imports の順）
pub fn loadProgram(entryFile: String, stdDir: String) -> IO[LoadResult]
pub type LoadResult = | Loaded(List[Module], Int) | LoadFailed(Error)       // 依存順の module（最後がエントリ）、次のノード番号
```

* Go の `driver.load` と同じ順序（後順）、同じエラー（`invalid module path`、`cannot find module`、`import cycle: ...`、各 module の構文エラーの `File`）にする。
* `std/X` は `<stdDir>/X.cero` を読む。エラーメッセージとエラーの `File` に使うファイル名は Go と同じ `std/X.cero` とする（`stdDir` に関係なく）。
* それ以外の module は、エントリのファイルのディレクトリからの相対パス `p.cero`（Go の `filepath.Join` と同じく、`dir/p.cero`。エントリがカレントディレクトリのファイルなら `p.cero`）。
* ファイルが存在するかは `fileExists` で確かめ、なければ `cannot find module`。ノード番号は module を読む順に続けて振る。
* Go 製は module の同一性をファイル名で判定する（`filepath.Clean`）。Cero では、エントリのファイル名の先頭の `./` を取り除いたものと、import から作ったファイル名を文字列で比べる。

## `compiler/main.cero`

```text
fn main() -> IO[Unit]
```

* 引数: `parse FILE`、`check FILE`、`ir FILE`、`build FILE [-o OUT]`、および任意の位置の `--std DIR`（既定は `std`）。
* `parse FILE`: `loadProgram` の結果の各 module について、`"== " ++ ファイル名 ++ "\n" ++ astFormatFile(...) ++ "\n"` を順に標準出力に書く。
* エラー: `formatError(e) ++ "\n"` を標準エラー出力に書き、`exit(1)`。
* エントリのファイルが存在しない: `ceroc: cannot read FILE\n` を標準エラー出力に書き `exit(1)`（Go とは文言が違ってよい。差分テストの対象外）。
* 使い方の誤り: `ceroc: usage: ...` を標準エラー出力に書き `exit(2)`。
* `check` / `ir` / `build`（段階 1 では未実装）: `ceroc: COMMAND: not yet implemented\n`、`exit(3)`。

## Go 側の変更

* `internal/driver`: 差分テストのために、`load` を `Load(filename string, src []byte, read func(string) ([]byte, error)) ([]*typecheck.Module, error)` として公開する（中身は変えない）。
* `Makefile`: `selfhost-build` ターゲットを追加する。`./bin/ceroc build compiler/main.cero -o bin/ceroc-cero.wasm`。

## 差分テスト（`internal/selfhost`、新規）

```go
// selfhost_test.go
// buildCompiler は、リポジトリのルートで compiler/main.cero を driver.CompileProgram でビルドし、
// 一時ディレクトリに ceroc.wasm を書いて、そのパスを返す。テストの中で 1 回だけ行う（sync.Once）。
// runCero は、リポジトリのルートを作業ディレクトリにして
// `wasmtime run --dir=. ceroc.wasm ARGS...` を実行し、標準出力・標準エラー出力・終了コードを返す。
```

* wasmtime が `PATH` になければ skip する（既存の E2E と同じ）。
* 起動を速くするため、`wasmtime compile` で事前にコンパイルし、`wasmtime run --allow-precompiled` で実行してもよい。
* テストはテーブル駆動で、`t.Parallel()` を呼ぶ。

`TestParse`:

* 対象: `examples/` の各エントリ（`examples/*.cero`、`examples/io/*.cero`、`examples/modules/main.cero`）と、`std/*.cero` の各ファイルをエントリとしたもの、`compiler/main.cero`（Cero 製 ceroc 自身）。
* 期待値: Go 側で `driver.Load` した各 module について、同じ形式（`== ファイル名`、`ast.FormatFile`）で組み立てた文字列。
* Cero 製の標準出力と一致し、終了コードが 0 であること。

`TestParseErrors`:

* 対象: `internal/selfhost/testdata/parse/*.cero`（新規）。Go の `lexer` と `parser` のすべてのエラーメッセージを 1 回以上起こすファイルを置く（字句エラー、各 `expected ...`、`let!` のエラー、import のエラー、`integer literal out of range` など。目安 40 ファイル以上）。module の読み込みのエラー（`invalid module path`、`cannot find module`、`import cycle`、import される側の構文エラー）は `testdata/parse/modules/` に、ファイルを組み合わせて置く。
* 期待値: Go 側で `driver.Load` が返すエラーの `Error()` に `"\n"` を付けたもの。
* Cero 製の標準エラー出力と一致し、終了コードが 1 であること。

`TestParseCorpusCoversErrors`（任意）: `parser.go` と `lexer.go` の `diag.Errorf` の書式文字列を正規表現で集め、それぞれが `testdata/parse` のどれかの期待値に現れることを確かめる。

## 完了条件

* `make check` が通る（wasmtime がある状態で、`internal/selfhost` のテストを skip しない）。
* `./bin/ceroc build compiler/main.cero -o /tmp/c.wasm && wasmtime run --dir=. /tmp/c.wasm parse examples/fib.cero` が Go 製と同じ表示を出す。
* 本書の差分テストがすべて通る。
