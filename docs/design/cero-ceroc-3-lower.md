# 設計: Cero 製 ceroc 段階 3（IR と IR 生成）

## 目的

ADR-0014 の段階 3 として、Go 製の `internal/ir` と `internal/lower` を Cero に移植し、`ir` コマンドと差分テストを追加する。

対象外にすること:

* WebAssembly の出力（段階 4）
* IR の最適化（Go にないもの）

## 関連する ADR

* ADR-0004（特殊化）、ADR-0005（末尾呼び出し）、ADR-0006（一般化した `let`）、ADR-0008（クロージャ）、ADR-0009（入れ子のパターン）、ADR-0010（String）、ADR-0011（module）、ADR-0012（IO）、ADR-0013
* ADR-0014: 移植の方針

## 基準となる Go のコード

| Go | Cero |
| --- | --- |
| `internal/ir/ir.go` | `compiler/ir/ir.cero` |
| `internal/ir/print.go` | `compiler/ir/print.cero` |
| `internal/lower/captures.go` | `compiler/lower/captures.cero` |
| `internal/lower/lower.go` | `compiler/lower/state.cero`、`compiler/lower/lower.cero` |

**IR の形（関数の番号と順序、局所変数の番号と型、テーブルの順序、名前、`Tail` の印）と表示は Go と同じにする。** 本書に書いていない細部は Go のコードに従う。
段階 2 の `Info`（`docs/design/cero-ceroc-2-typecheck.md`）だけを入力に使う。

## 名前の規則

| module | 構築子・関数の接頭辞 |
| --- | --- |
| `ir/*` | 構築子 `Ir...`（値型は `Vt...`）、関数 `ir...` |
| `lower/*` | 構築子 `Lw...`、関数 `lw...`（公開する `lowerProgram` を除く） |

## `compiler/ir/ir.cero`

```text
pub type ValType = | VtInt | VtBool | VtFuncRef | VtPtr
pub type Sig = | IrSig(List[ValType], ValType)
pub type UnOp = | IrNeg | IrNot
pub type BinOp = | IrAdd | IrSub | IrMul | IrDiv | IrRem | IrEq | IrNe | IrLt | IrLe | IrGt | IrGe
pub type PrimOp = ...（Go の ir.PrimOp の定数と同じ順の構築子。IrStrLength、…、IrIOExit、IrBitAnd、…）

pub type IrExpr =
    | IrIntConst(Int)
    | IrBoolConst(Bool)
    | IrStrConst(String)
    | IrLocalGet(Int, ValType)
    | IrFuncValue(Int, Option[IrExpr])
    | IrUnary(UnOp, IrExpr)
    | IrBinary(BinOp, IrExpr, IrExpr)
    | IrIf(IrExpr, IrExpr, IrExpr, ValType)
    | IrBlock(List[IrLet], IrExpr)
    | IrCall(Int, List[IrExpr], ValType, Bool)            // 関数、引数、型、Tail
    | IrCallIndirect(IrExpr, Sig, List[IrExpr], Bool)     // 呼ばれる値、利用者の型、引数、Tail
    | IrConstruct(Int, List[IrExpr])
    | IrField(Int, Int, ValType)                          // 局所変数、フィールドの番号、型
    | IrSwitchTag(Int, List[IrTagCase], Option[IrExpr], ValType)
    | IrPrim(PrimOp, List[IrExpr])

pub type IrLet = | IrLet(Int, IrExpr)
pub type IrTagCase = | IrTagCase(Int, IrExpr)
pub type IrFunc = | IrFunc(String, Sig, List[ValType], IrExpr)        // 名前、型、局所変数（引数を含む）、本体
pub type IrModule = | IrModule(List[IrFunc], List[Int], Int, Bool)   // 関数（番号順）、テーブル、main、Command

pub fn irExprType(e: IrExpr) -> ValType     // Go の Expr.Type()
pub fn primResultType(op: PrimOp) -> ValType
```

Go の `Unary.Type()` などの規則をそのまま移す。

## `compiler/ir/print.cero`

```text
pub fn irFormat(m: IrModule) -> String      // Go の ir.Format と同じ文字列
```

`Doc` で組み立てて最後に `docToString` する。文字列の定数の表示は段階 1 の `quoteString` を使う。

## `compiler/lower/`

```text
pub fn lowerProgram(mods: List[Module], info: Info) -> IrModule   // Go の LowerProgram
```

* Go の `lowerer` の状態（作った関数の表、テーブル、特殊化の表とキュー、`$ref` のラッパーの表、組み込み関数・IO の動作の関数の表、匿名関数の数、型パラメータの値型の表 `env`、一般化した `let` の表）を、状態モナド（段階 2 と同じ形）で引き回す。
* Go は `ir.Func` を作って番号を決めてから本体を変換し、変換の途中で入れ子の匿名関数を `module.Funcs` に追加する。Cero では、関数を「番号 → 関数」の表に置き、本体を後から書き込む。最後に番号順のリストにする。
* Go の `lowerFunc` は、変換中の関数の `Locals` に局所変数を追加していく。Cero では、変換中の関数ごとに「局所変数の型の列」と「シンボルの番号 → 局所変数の番号」の表を持つ。入れ子の匿名関数を変換するときは、外側の関数の分を保存し、終わったら戻す。
* `markTailCalls` は、Go が `Tail` を書き換える代わりに、印を付けた新しい式を返す関数にする。
* 互いに再帰する変換の関数（式、ブロック、`match`、匿名関数、一般化した `let`、特殊化）は `lower/lower.cero` にまとめる。捕捉の解析（`captures.cero`）は再帰が閉じているので別にする。
* `repKey` と、関数の名前（`identity[Int]`、`lambda$3`、`f$ref`、`std/list.map[Ptr,Int]`、`io.bind[Int,Bool]` など）は Go と同じ文字列にする。

## `compiler/main.cero` の変更

* `ir FILE`: `loadProgram` → `checkProgram` → `lowerProgram` の後、`irFormat` の結果を標準出力に書く（末尾に改行を足すかは Go 側の期待値の組み立てと合わせる）。エラーは段階 2 と同じ。

## 差分テスト（`internal/selfhost`）

`TestIR`:

* 対象: 段階 2 の `TestCheck` で成功するファイル（`examples/`、`compiler/main.cero`）、`testdata/check_ok/` のすべてのケース、および新しい `testdata/programs/`。
* `testdata/programs/`（新規）: `internal/driver/e2e_test.go` の `TestE2E`、`TestE2ERuntimeTrap`、`TestE2EIO` のソースをファイルにしたもの（スクリプトで抜き出してよい。ファイルを読む `examples/` のケースは除く）。段階 4 のバイト列の比較でも使う。
* 期待値: Go 側で `driver.Load` → `typecheck.CheckProgram` → `lower.LowerProgram` → `ir.Format` した文字列。
* Cero 製の標準出力と一致し、終了コードが 0 であること。

## 完了条件

* `make check` が通る（wasmtime がある状態で、`internal/selfhost` を skip しない）。
* Cero 製 ceroc の `ir compiler/main.cero` が Go と一致する。
* 本書の差分テストがすべて通る。
