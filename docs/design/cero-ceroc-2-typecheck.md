# 設計: Cero 製 ceroc 段階 2（型検査）

## 目的

ADR-0014 の段階 2 として、Go 製の `internal/types` と `internal/typecheck` を Cero に移植し、`check` コマンドと差分テストを追加する。

対象外にすること:

* IR 生成と WebAssembly の出力（段階 3・4）
* 型検査の高速化（Go と同じアルゴリズムのまま）

## 関連する ADR

* ADR-0004〜0013: 型検査の仕様（Go の実装が基準）
* ADR-0014: 移植の方針

## 基準となる Go のコード

| Go | Cero |
| --- | --- |
| `internal/types/types.go` | `compiler/types/types.cero` |
| `internal/typecheck/unify.go` | `compiler/check/unify.cero` |
| `internal/typecheck/deps.go` | `compiler/check/deps.cero` |
| `internal/typecheck/exhaust.go` | `compiler/check/exhaust.cero` |
| `internal/typecheck/typecheck.go` | `compiler/check/state.cero`、`compiler/check/expr.cero`、`compiler/check/program.cero` |

**アルゴリズム、検査の順序、エラーメッセージと位置、型の表示（`?T`、`t1` などの名前を含む）は Go と同じにする。** 本書に書いていない細部は Go のコードに従う。
Go の `map` を反復している箇所で結果が順序に依存するものはない（`bindImports` は本 PR で名前の昇順に直した）。Cero では、反復の順序に意味がない箇所は任意の順でよい。

## 名前の衝突を避ける規則

段階 1 で、`token` の構築子（`Ident`、`If` など）が AST の構築子と衝突した。段階 2 以降は、公開する型の構築子と関数に、module の略称を付ける。

| module | 構築子・関数の接頭辞 |
| --- | --- |
| `types/types` | 構築子 `Ty...`（`TyInt`、`TyFunc` など）、関数 `ty...` |
| `check/*` | 構築子 `Ck...`、関数 `ck...`（公開する `checkProgram` などを除く） |

## `compiler/types/types.cero`

Go のポインタの同一性は番号に置き換える。

```text
pub type Type =
    | TyInt
    | TyBool
    | TyString
    | TyUnit
    | TyFunc(List[Type], Type)
    | TyNamed(Int, List[Type])        // データ型の番号、型引数
    | TyParam(Int, String)            // 型パラメータの番号、名前
    | TyMeta(Int)                     // 単一化変数の番号

pub type CtorInfo = | CtorInfo(String, Int, List[Type], Int)      // 名前、宣言順の番号（タグ）、フィールドの型、データ型の番号
pub type DataInfo = | DataInfo(String, List[Type], List[CtorInfo]) // 名前、型パラメータ（TyParam の列）、構築子

// TyEnv は、型の表示と解決に必要な表: データ型、単一化変数の名前と解。
pub type TyEnv = | TyEnv(IntMap[DataInfo], IntMap[String], IntMap[Type])

pub fn tyIOData() -> Int                      // 組み込みの IO のデータ型の番号（0）。型パラメータ T の番号も 0
pub fn tyPrune(env: TyEnv, t: Type) -> Type
pub fn tyResolve(env: TyEnv, t: Type) -> Type
pub fn tySubst(t: Type, params: List[Type], args: List[Type]) -> Type    // params は TyParam の列
pub fn tyMetas(env: TyEnv, t: Type) -> List[Int]
pub fn tyMentions(env: TyEnv, t: Type, v: Type) -> Bool
pub fn tyEqual(env: TyEnv, a: Type, b: Type) -> Bool
pub fn tyString(env: TyEnv, t: Type) -> String           // Go の Type.String() と同じ
pub fn tyIsIO(env: TyEnv, t: Type) -> Bool
```

* 番号（データ型・型パラメータ・単一化変数・シンボル）は、型検査の状態の 1 つのカウンタからプログラム全体で一意に振る。データ型の番号 0 と型パラメータの番号 0 は組み込みの `IO` に予約する。
* Go の `Meta.Solution` への代入は、`TyEnv` の解の表への追加になる。

## 型検査の結果（段階 3 が使う）

`compiler/check/state.cero` に置く。段階 3 の IR 生成は、これだけを使う。

```text
pub type SymKind = | CkSymFunc | CkSymParam | CkSymLocal | CkSymCtor | CkSymBuiltin

// シンボル: 種類、名前、型（解決済み）、位置、関数の宣言のノード番号（なければ -1）、
//          構築子（CkSymCtor のときだけ Some((データ型の番号, タグ)))、型パラメータ（TyParam の列）、組み込み関数の番号（なければ -1）
pub type Symbol = | Symbol(SymKind, String, Type, Pos, Int, Option[Pair[Int, Int]], List[Type], Int)

pub type Info = | Info(
    IntMap[Type],            // 式のノード番号 -> 型（Go の Info.Types）
    IntMap[Int],             // Ident の式のノード番号 -> シンボルの番号（Uses）
    IntMap[Int],             // let のノード番号 -> シンボルの番号（Defs）
    IntMap[Int],             // 引数のノード番号 -> シンボルの番号（Params）
    IntMap[Int],             // 関数の宣言のノード番号 -> シンボルの番号（Funcs）
    IntMap[Type],            // 匿名関数の式のノード番号 -> TyFunc（FuncLits）
    IntMap[Int],             // 型の宣言のノード番号 -> データ型の番号（Datas）
    IntMap[Pair[Int, Int]],  // 構築子のパターンのノード番号 -> (データ型の番号, タグ)（CtorPats）
    IntMap[Int],             // 変数のパターンのノード番号 -> シンボルの番号（PatVars）
    IntMap[List[Type]],      // Ident の式のノード番号 -> 型引数（TypeArgs）
    IntMap[Symbol],          // シンボルの番号 -> シンボル
    IntMap[DataInfo],        // データ型の番号 -> データ型
    Int,                     // main のシンボルの番号
    Bool,                    // MainIO
    IntMap[Int]              // 関数の宣言のノード番号 -> module の位置（loadProgram の結果のリストでの 0 始まりの位置。FuncModule）
)

// 各フィールドを取り出す関数（infoTypes、infoUses、… infoFuncModule）を、上の順に公開する。
pub fn checkProgram(mods: List[Module]) -> Result[Info]   // Go の CheckProgram。エラーは File 付き
```

* `Info` のすべての型は、Go の `resolveInfo` と同じく単一化変数を含まない（解決済み）。一般化した単一化変数は `TyParam` になっている。
* `Builtin` の番号は Go の `typecheck.Builtin` の定数の値と同じにする（`stringLength` が 0、…）。

## 状態とモナド

```text
// check/state.cero
pub type Check[A] = | Check(CkState -> CkResult[A])
pub type CkResult[A] = | CkOk(A, CkState) | CkErr(Error)
pub fn bind[A, B](m: Check[A], f: A -> Check[B]) -> Check[B]
pub fn pure[A](x: A) -> Check[A]
pub fn ckFail[A](e: Error) -> Check[A]
```

* `CkState` は、Go の `checker` と `Info` の状態（表、`TyEnv`、カウンタ、検査中の関数の単一化変数の一覧、成分の情報、現在の関数、import した名前、自分の宣言の表、組み込み関数）を持つ。フィールドの分け方は自由。
* 局所的なスコープ（Go の `funcCtx` と `scope`）は、状態に持っても、関数の引数で渡してもよい。Go と同じ規則で名前を引ければよい。
* 型検査の関数は互いに再帰する（式、ブロック、匿名関数、`match`、パターン）。module の間では相互再帰できないので、これらは `check/expr.cero` にまとめる。`unify`、`exhaust`、`deps` は再帰が閉じているので別の module にできる。
* スタック: `bind` の続きは末尾呼び出しになるので、リストを順に処理するモナドの再帰はスタックを使わない（段階 1 の構文解析と同じ）。

## `compiler/main.cero` の変更

* `check FILE`: `loadProgram` の後に `checkProgram`。成功なら何も出力せず終了コード 0。失敗なら段階 1 と同じくエラーを書いて `exit(1)`。
* `check --types FILE`: 成功したとき、各 module について `"== " ++ ファイル名 ++ "\n"` の後、その module のトップレベルの関数ごとに宣言順で `名前 ++ " : " ++ 型 ++ "\n"` を書く。型はシンボルの型を `tyString` で表示したもので、型パラメータがあれば先頭に `"forall " ++ 名前を ", " でつないだもの ++ ". "` を付ける（差分テスト用）。

## Go 側の変更

* `internal/typecheck`: `bindImports` を名前の昇順で反復する（本 PR の最初の commit で済み）。

## 差分テスト（`internal/selfhost`）

`TestCheck`:

* 対象: 段階 1 の `TestParse` と同じファイル（`examples/`、`std/`、`compiler/main.cero`）。
* `check --types` の標準出力が、Go 側で `driver.Load` → `typecheck.CheckProgram` した結果から同じ形式で組み立てた文字列と一致し、終了コードが 0 であること。

`TestCheckErrors`:

* 対象: `internal/selfhost/testdata/check/*.cero`（新規）と、複数ファイルのケース（`testdata/check/modules/`）。
  `internal/typecheck/typecheck_test.go` のエラーのテーブル（`TestCheckError`、`TestCheckDataError`、`TestCheckGenericError`、`TestCheckInferError`、`TestCheckTopLevelInferError`、`TestCheckNestedPatternError`、`TestCheckStringError`、`TestCheckIOError`、`TestCheckConveniencesError`、`TestCheckProgram` のエラー）のソースから、プログラム全体として成り立つものをすべてファイルにする（スクリプトで抜き出してよい。抜き出しのスクリプトはコミットしなくてよい）。目安 200 ファイル以上。
* 期待値: Go 側で `driver.Load` → `typecheck.CheckProgram` のエラーの `Error()` に `"\n"` を付けたもの。
* Cero 製の標準エラー出力と一致し、終了コードが 1 であること。

`TestCheckSuccessCorpus`:

* 対象: 同じく typecheck のテストの成功のテーブル（`TestCheckSuccess` など）のうち、プログラム全体として成り立つものを `testdata/check_ok/*.cero` に置いたもの。
* `check --types` の出力が Go と一致すること。

`TestCheckCorpusCoversErrors`: `typecheck.go`、`exhaust.go` などの `diag.Errorf` の書式が、`testdata/check` のどれかの期待値に現れることを確かめる（段階 1 と同じ方式）。

## 完了条件

* `make check` が通る（wasmtime がある状態で、`internal/selfhost` を skip しない）。
* Cero 製 ceroc の `check --types compiler/main.cero` が Go と一致する（Cero 製 ceroc が自分自身を型検査できる）。
* 本書の差分テストがすべて通る。
