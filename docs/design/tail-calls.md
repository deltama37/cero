# 設計: 末尾呼び出し（`return_call` / `return_call_indirect`）

## 目的

ADR-0005 の定義で末尾位置にある関数呼び出しを、WebAssembly の `return_call` と `return_call_indirect` で出力する。
これにより、末尾再帰と相互再帰が深さ 1,000 万でもスタックを使い切らずに実行できるようにする。

対象外にすること:

* 構築子の補助関数（`new`）の呼び出しを `return_call` にすること（ADR-0005 で保証の対象外）
* 末尾位置にない再帰を末尾再帰に書き換える変換（累積引数の導入など）
* 末尾呼び出しをループ（`loop` / `br`）に変換する最適化
* 字句解析・構文解析・型検査の変更（末尾位置は IR 生成で判定する）
* `internal/cli`、`internal/driver/driver.go`、`README.md`、既存の設計書（`docs/design/v0.*.md`）の変更

## 関連する ADR

* ADR-0001: 繰り返しは再帰で表す
* ADR-0002: 関数値の表現、`call` / `call_indirect`、lambda lifting
* ADR-0003: `match` の IR（`SwitchTag`）
* ADR-0005: 末尾呼び出しの保証と末尾位置の定義

## 前提

`docs/design/v0.1-*.md` と `docs/design/v0.2-*.md` の内容が実装済みであること。本書は差分だけを書く。

## 変更するパッケージとファイル

| パッケージ | ファイル | 内容 |
| --- | --- | --- |
| `internal/ir` | `ir.go`, `print.go`, `print_test.go` | `Call` と `CallIndirect` に `Tail` を追加し、表示を変える |
| `internal/lower` | `lower.go`, `lower_test.go` | 関数本体の末尾位置の呼び出しに `Tail` を付ける |
| `internal/wasm` | `encode.go`, `encode_test.go` | `Tail` の呼び出しを `return_call` / `return_call_indirect` で出力する |
| `internal/driver` | `e2e_test.go` | 深い末尾再帰の E2E を追加する |
| `examples` | `tail_sum.cero`（新規） | サンプル |

これ以外のファイルは変更しない。

## `internal/ir`

### 型の変更

`Call` と `CallIndirect` の末尾に `Tail` フィールドを追加する。ほかのフィールドと既存のコメントは変えない。
`Type()` の結果は `Tail` によらない。

```go
// Call calls a function directly. Tail reports that the call is in tail
// position of the enclosing function (ADR-0005); it is then encoded as
// return_call, and T equals the enclosing function's result type.
type Call struct {
	Func FuncID
	Args []Expr
	T    ValType
	Tail bool
}

// CallIndirect calls the FuncRef value Callee, whose signature is Sig.
// Tail reports that the call is in tail position of the enclosing function
// (ADR-0005); it is then encoded as return_call_indirect, and Sig.Result
// equals the enclosing function's result type.
type CallIndirect struct {
	Callee Expr
	Sig    Sig
	Args   []Expr
	Tail   bool
}
```

### テキスト表現（`FormatExpr`）

`Tail` が `false` のときは、これまでどおり `(call ...)` と `(call.indirect ...)` を出力する。
`Tail` が `true` のときは、先頭の名前だけを次のように変える。引数の並びは変えない。

| ノード | `Tail == false` | `Tail == true` |
| --- | --- | --- |
| `Call` | `(call F A...)` | `(return.call F A...)` |
| `CallIndirect` | `(call.indirect SIG C A...)` | `(return.call.indirect SIG C A...)` |

## `internal/lower`

### 追加する関数

```go
// markTailCalls sets Tail on every Call and CallIndirect in tail position
// of e (ADR-0005). e must be in tail position itself.
func markTailCalls(e ir.Expr)
```

公開する関数は増やさない。`Lower` のシグネチャも変えない。

### 処理の流れ

`lowerFunc` の最後（本体の型を検査する `if` の後）で、`markTailCalls(fn.Body)` を呼ぶ。
`lowerFunc` はトップレベル関数と、持ち上げた匿名関数の両方で呼ばれるので、どちらの本体にも印が付く。

```text
markTailCalls(e):
    switch e:
    case *ir.Call:          e.Tail = true            // 引数には降りない
    case *ir.CallIndirect:  e.Tail = true            // 呼び出される式と引数には降りない
    case *ir.If:            markTailCalls(e.Then); markTailCalls(e.Else)   // Cond には降りない
    case *ir.Block:         markTailCalls(e.Result)  // Let の値には降りない
    case *ir.SwitchTag:
        for c in e.Cases: markTailCalls(c.Body)
        if e.Default != nil: markTailCalls(e.Default)
    default:                何もしない
        // IntConst, BoolConst, LocalGet, FuncValue, Unary, Binary, Construct, Field
```

AST の末尾位置（ADR-0005）と IR の末尾位置は、既存の変換を通して次のように対応する。

| AST | IR | 末尾位置になる部分 |
| --- | --- | --- |
| 関数本体 | `Func.Body` | 本体 |
| ブロック | `Block` | `Result` |
| `if` | `If` | `Then`, `Else` |
| `x && y` | `If{Cond: x, Then: y, Else: false}` | `Then`（= `y`） |
| `x \|\| y` | `If{Cond: x, Then: true, Else: y}` | `Else`（= `y`） |
| データ型の `match` | `Block{Lets: [s], Result: SwitchTag}` | 各 `TagCase.Body`、`Default` |
| `Int` / `Bool` の `match` | `Block{Lets: [s], Result: If の連鎖}` | 各 `If` の `Then`、`Else` |
| パターン変数を束縛するアーム | `Block{Lets: [fields], Result: body}` | `Result` |

`Unary` と `Binary` の被演算子、`Construct` のフィールドは末尾位置ではないので降りない。
`FuncValue` は匿名関数の本体を含まない（本体は別の `ir.Func` で、その `lowerFunc` で印が付く）。

IR のノードは変換中に共有されない（各 AST ノードから新しい IR ノードを作る）ので、その場で書き換えてよい。

### 型の整合性

末尾位置にある式の型は、`Func.Sig.Result` と等しい。
`lowerFunc` は本体の型が `Sig.Result` と等しいことを検査し、`lowerExpr` は各式の IR の型が型検査の結果と等しいことを検査している。
`If`・`Block`・`SwitchTag` の型は分岐と `Result` の型に等しいので、印を付けた呼び出しの型も `Sig.Result` に等しくなる。
`lower` では追加の検査をしない（`wasm` 側で検査する）。

## `internal/wasm`

### 命令

```go
opReturnCall         byte = 0x12
opReturnCallIndirect byte = 0x13
```

既存の `opCallIndirect` の直後に追加する。

| 命令 | エンコード |
| --- | --- |
| `return_call f` | `0x12` の後に関数インデックス（ULEB128） |
| `return_call_indirect t 0` | `0x13` の後に型インデックス（ULEB128）とテーブルインデックス `0x00` |

### `encoder` の変更

`encoder` に、いま出力している関数の戻り値の型を持たせる。

```go
type encoder struct {
	table    []ir.FuncID
	types    map[string]int
	buf      []byte
	allocIdx int
	newIdx   map[string]int
	result   ir.ValType // result type of the function being encoded
}
```

`encodeBody(fn)` の先頭で `e.result = fn.Sig.Result` を設定する。
`alloc` と `new` の本体は `encodeBody` を通らず、`Tail` の呼び出しも含まないので、`result` は使わない。

### 追加するメソッド

```go
// checkTail panics unless a tail call returning t can replace the frame of
// the function being encoded.
func (e *encoder) checkTail(t ir.ValType)
```

`t != e.result` のとき、`fmt.Sprintf("wasm: tail call returns %s, function returns %s", t, e.result)` で panic する。
IR の型で比べる（`Bool` と `Ptr` のように wasm では同じ `i32` になる組み合わせでも、IR の型が違えば panic する）。
これは lower の不具合を検出するためのもので、正しい IR では起きない。

### 処理の流れ（`expr`）

```text
case *ir.Call:
    for arg in x.Args: e.expr(arg)
    if x.Tail:
        e.checkTail(x.T)
        e.buf += opReturnCall
    else:
        e.buf += opCall
    e.buf += uleb(x.Func)

case *ir.CallIndirect:
    for arg in x.Args: e.expr(arg)
    e.expr(x.Callee)
    if x.Tail:
        e.checkTail(x.Sig.Result)
        e.buf += opReturnCallIndirect
    else:
        e.buf += opCallIndirect
    e.buf += uleb(e.typeIndex(x.Sig)), 0x00
```

`return_call` の後はスタックが多相になるので、`if` の分岐や関数本体の最後にそのまま置いてよい（後ろに `else` / `end` が続いても検証に通る）。
型セクション（`collectTypes`）と `walk` は変えない。`CallIndirect` の `Sig` は `Tail` によらず型セクションに登録される。

## エラーケースと扱い

| 状況 | 扱い |
| --- | --- |
| `Tail` の呼び出しの戻り値の型が、出力中の関数の戻り値の型と違う | `wasm` が panic（コンパイラの不具合。利用者向けの `diag` エラーにはしない） |
| 実行時のランタイムが末尾呼び出しに対応していない | `ceroc` では検出しない。`ceroc run` はランタイムのエラーをそのまま伝える（既存の扱い） |

利用者向けの新しいコンパイルエラーはない。

## テスト観点

既存のテストはすべてテーブル駆動なので、既存のテーブルにケースを追加する。新しいテスト関数には `t.Parallel()` を呼び、サブテストでも呼ぶ。

### `internal/ir/print_test.go`

`TestFormatExpr` に追加する。

| name | expr | want |
| --- | --- | --- |
| `tail call` | `&Call{Func: 1, Args: []Expr{&IntConst{Value: 2}}, T: Int, Tail: true}` | `(return.call 1 2)` |
| `tail call without arguments` | `&Call{Func: 2, T: Int, Tail: true}` | `(return.call 2)` |
| `tail call indirect` | `&CallIndirect{Callee: &LocalGet{Local: 0, T: FuncRef}, Sig: Sig{Params: []ValType{Int}, Result: Int}, Args: []Expr{&IntConst{Value: 3}}, Tail: true}` | `(return.call.indirect (sig (Int) Int) (local 0) 3)` |

`TestType` に追加する。

| name | expr | want |
| --- | --- | --- |
| `tail call` | `&Call{T: Bool, Tail: true}` | `Bool` |
| `tail call indirect` | `&CallIndirect{Sig: Sig{Result: FuncRef}, Tail: true}` | `FuncRef` |

### `internal/lower/lower_test.go`

#### 既存のケースの更新

既存の `TestLower` のケースのうち、末尾位置に `call` / `call.indirect` があるものは期待値が変わる（例: `fib recursion and main not first` の `main` は `(return.call 0 10)` になる）。
更新してよいのは、末尾位置にある `(call ` を `(return.call ` に、`(call.indirect ` を `(return.call.indirect ` に変えることだけとする。
それ以外の差分（ローカルの番号、テーブル、引数の中の呼び出しなど）が出たら、期待値を直さずに報告する。

#### 追加するケース

各ケースの `want` は `lines(...)` で、次の行をこの順に並べる。

1. `self tail call in else branch`

   ```text
   fn go(n: Int, acc: Int) -> Int {
       if n == 0 {
           acc
       } else {
           go(n - 1, acc + n)
       }
   }

   fn main() -> Int {
       go(10, 0)
   }
   ```

   ```text
   (func 0 go (sig (Int Int) Int) (locals Int Int) (if (eq (local 0) 0) (local 1) (return.call 0 (sub (local 0) 1) (add (local 1) (local 0)))))
   (func 1 main (sig () Int) (locals) (return.call 0 10 0))
   (table)
   (main 1)
   ```

2. `arguments of a tail call are not tail calls`

   ```text
   fn f(n: Int) -> Int {
       n
   }

   fn main() -> Int {
       f(f(1))
   }
   ```

   ```text
   (func 0 f (sig (Int) Int) (locals Int) (local 0))
   (func 1 main (sig () Int) (locals) (return.call 0 (call 0 1)))
   (table)
   (main 1)
   ```

3. `let value is not a tail position`

   ```text
   fn f(n: Int) -> Int {
       n
   }

   fn main() -> Int {
       let x = f(1)
       f(x)
   }
   ```

   ```text
   (func 0 f (sig (Int) Int) (locals Int) (local 0))
   (func 1 main (sig () Int) (locals Int) (block (let 0 (call 0 1)) (return.call 0 (local 0))))
   (table)
   (main 1)
   ```

4. `if condition is not a tail position`

   ```text
   fn p(n: Int) -> Bool {
       n > 0
   }

   fn main() -> Int {
       if p(1) {
           1
       } else {
           0
       }
   }
   ```

   ```text
   (func 0 p (sig (Int) Bool) (locals Int) (gt (local 0) 0))
   (func 1 main (sig () Int) (locals) (if (call 0 1) 1 0))
   (table)
   (main 1)
   ```

5. `right operand of && and || is a tail position`

   ```text
   fn p(n: Int) -> Bool {
       n > 0
   }

   fn both(n: Int) -> Bool {
       p(n) && p(n - 1)
   }

   fn either(n: Int) -> Bool {
       p(n) || p(n - 1)
   }

   fn main() -> Int {
       0
   }
   ```

   ```text
   (func 0 p (sig (Int) Bool) (locals Int) (gt (local 0) 0))
   (func 1 both (sig (Int) Bool) (locals Int) (if (call 0 (local 0)) (return.call 0 (sub (local 0) 1)) false))
   (func 2 either (sig (Int) Bool) (locals Int) (if (call 0 (local 0)) true (return.call 0 (sub (local 0) 1))))
   (func 3 main (sig () Int) (locals) 0)
   (table)
   (main 3)
   ```

6. `constructor arm of a match is a tail position`

   ```text
   type IntList =
       | Nil
       | Cons(Int, IntList)

   fn sum(xs: IntList, acc: Int) -> Int {
       match xs {
           Nil => acc,
           Cons(x, rest) => sum(rest, acc + x),
       }
   }

   fn main() -> Int {
       sum(Nil, 0)
   }
   ```

   ```text
   (func 0 sum (sig (Ptr Int) Int) (locals Ptr Int Ptr Int Ptr) (block (let 2 (local 0)) (switch.tag 2 (case 0 (local 1)) (case 1 (block (let 3 (field 2 0)) (let 4 (field 2 1)) (return.call 0 (local 4) (add (local 1) (local 3))))))))
   (func 1 main (sig () Int) (locals) (return.call 0 (construct 0) 0))
   (table)
   (main 1)
   ```

7. `default arm of a data match is a tail position`

   ```text
   type IntList =
       | Nil
       | Cons(Int, IntList)

   fn g(xs: IntList) -> Int {
       match xs {
           Nil => 0,
           _ => g(Nil),
       }
   }

   fn main() -> Int {
       0
   }
   ```

   ```text
   (func 0 g (sig (Ptr) Int) (locals Ptr Ptr) (block (let 1 (local 0)) (switch.tag 1 (case 0 0) (default (return.call 0 (construct 0))))))
   (func 1 main (sig () Int) (locals) 0)
   (table)
   (main 1)
   ```

8. `wildcard arm of an integer match is a tail position`

   ```text
   fn down(n: Int) -> Int {
       match n {
           0 => 0,
           _ => down(n - 1),
       }
   }

   fn main() -> Int {
       0
   }
   ```

   ```text
   (func 0 down (sig (Int) Int) (locals Int Int) (block (let 1 (local 0)) (if (eq (local 1) 0) 0 (return.call 0 (sub (local 0) 1)))))
   (func 1 main (sig () Int) (locals) 0)
   (table)
   (main 1)
   ```

9. `anonymous function body and indirect tail call`

   ```text
   fn f(n: Int) -> Int {
       n
   }

   fn main() -> Int {
       let g = fn(x: Int) -> Int { f(x) }
       g(1)
   }
   ```

   ```text
   (func 0 f (sig (Int) Int) (locals Int) (local 0))
   (func 1 main (sig () Int) (locals FuncRef) (block (let 0 (func.ref 2)) (return.call.indirect (sig (Int) Int) (local 0) 1)))
   (func 2 lambda$0 (sig (Int) Int) (locals Int) (return.call 0 (local 0)))
   (table 2)
   (main 1)
   ```

10. `constructor in tail position is not a call`

    ```text
    type IntList =
        | Nil
        | Cons(Int, IntList)

    fn f(n: Int) -> Int {
        n
    }

    fn one(n: Int) -> IntList {
        Cons(f(n), Nil)
    }

    fn main() -> Int {
        0
    }
    ```

    ```text
    (func 0 f (sig (Int) Int) (locals Int) (local 0))
    (func 1 one (sig (Int) Ptr) (locals Int) (construct 1 (call 0 (local 0)) (construct 0)))
    (func 2 main (sig () Int) (locals) 0)
    (table)
    (main 2)
    ```

### `internal/wasm/encode_test.go`

#### `TestEncodeTailCall`（新規）

IR を手で組み立てて `Encode` し、`functionBodies(t, got)[1]`（`main` の本体。ローカル宣言を含む）を比べる。
どのケースも `Funcs` は次の 2 つで、`Main: 1` とする。

* 0: `{Name: "f", Sig: ir.Sig{Params: []ir.ValType{ir.Int}, Result: ir.Int}, Locals: []ir.ValType{ir.Int}, Body: &ir.LocalGet{Local: 0, T: ir.Int}}`
* 1: `{Name: "main", Sig: ir.Sig{Result: ir.Int}, Body: <ケースの body>}`

型インデックスは、0 が `(i64) -> i64`、1 が `() -> i64` になる。

| name | body | Table | want（`main` の本体） |
| --- | --- | --- | --- |
| `direct tail call` | `&ir.Call{Func: 0, Args: []ir.Expr{&ir.IntConst{Value: 7}}, T: ir.Int, Tail: true}` | なし | `00 42 07 12 00 0b` |
| `direct call not in tail position` | `&ir.Call{Func: 0, Args: []ir.Expr{&ir.IntConst{Value: 7}}, T: ir.Int}` | なし | `00 42 07 10 00 0b` |
| `indirect tail call` | `&ir.CallIndirect{Callee: &ir.FuncValue{Func: 0}, Sig: ir.Sig{Params: []ir.ValType{ir.Int}, Result: ir.Int}, Args: []ir.Expr{&ir.IntConst{Value: 7}}, Tail: true}` | `[]ir.FuncID{0}` | `00 42 07 41 00 13 00 00 0b` |
| `indirect call not in tail position` | 上と同じで `Tail` なし | `[]ir.FuncID{0}` | `00 42 07 41 00 11 00 00 0b` |
| `tail call inside if` | `&ir.If{Cond: &ir.BoolConst{Value: true}, Then: &ir.Call{Func: 0, Args: []ir.Expr{&ir.IntConst{Value: 1}}, T: ir.Int, Tail: true}, Else: &ir.IntConst{Value: 0}, T: ir.Int}` | なし | `00 41 01 04 7e 42 01 12 00 05 42 00 0b 0b` |

#### `TestEncodePanics`（既存）に追加

既存のテーブルの `main` は `Sig: ir.Sig{Result: ir.Bool}` で、引数とローカルを持たない。

| name | body |
| --- | --- |
| `tail call result differs from function result` | `&ir.Call{Func: 0, T: ir.Int, Tail: true}` |
| `tail call indirect result differs from function result` | `&ir.CallIndirect{Callee: &ir.LocalGet{Local: 0, T: ir.FuncRef}, Sig: ir.Sig{Result: ir.Int}, Tail: true}` |

### `internal/driver/e2e_test.go`

`TestE2E` のテーブルに追加する。すべて wasmtime で実行し、`call stack exhausted` にならずに `want` を出力することを確かめる。
深さ 1,000 万は、末尾呼び出しがない場合の上限（約 5 万）の 200 倍にあたる。

| name | src / file | want |
| --- | --- | --- |
| `tail_sum example` | `file: "examples/tail_sum.cero"` | `50000005000000` |
| `deep mutual tail recursion` | 下記 A | `1` |
| `deep indirect tail call` | 下記 B | `50000005000000` |
| `deep tail call in match arm` | 下記 C | `500000500000` |
| `deep tail call in integer match` | 下記 D | `20000000` |
| `deep tail call through && and \|\|` | 下記 E | `1` |
| `deep tail call after let` | 下記 F | `50000005000000` |

A:

```text
fn isEven(n: Int) -> Bool {
    if n == 0 {
        true
    } else {
        isOdd(n - 1)
    }
}

fn isOdd(n: Int) -> Bool {
    if n == 0 {
        false
    } else {
        isEven(n - 1)
    }
}

fn main() -> Int {
    if isEven(10000000) {
        1
    } else {
        0
    }
}
```

B（関数値の呼び出しと、匿名関数からトップレベル関数への呼び出しが交互に続く）:

```text
fn count(n: Int, acc: Int) -> Int {
    let next = fn(m: Int, a: Int) -> Int { count(m, a) }
    if n == 0 {
        acc
    } else {
        next(n - 1, acc + n)
    }
}

fn main() -> Int {
    count(10000000, 0)
}
```

C（リストを 100 万要素にする。1 要素 24 バイトで約 24 MB を確保する）:

```text
type IntList =
    | Nil
    | Cons(Int, IntList)

fn build(n: Int, acc: IntList) -> IntList {
    if n == 0 {
        acc
    } else {
        build(n - 1, Cons(n, acc))
    }
}

fn sum(xs: IntList, acc: Int) -> Int {
    match xs {
        Nil => acc,
        Cons(x, rest) => sum(rest, acc + x),
    }
}

fn main() -> Int {
    sum(build(1000000, Nil), 0)
}
```

D:

```text
fn down(n: Int, acc: Int) -> Int {
    match n {
        0 => acc,
        _ => down(n - 1, acc + 2),
    }
}

fn main() -> Int {
    down(10000000, 0)
}
```

E:

```text
fn allPositive(n: Int) -> Bool {
    n == 0 || (n > 0 && allPositive(n - 1))
}

fn main() -> Int {
    if allPositive(10000000) {
        1
    } else {
        0
    }
}
```

F:

```text
fn go(n: Int, acc: Int) -> Int {
    if n == 0 {
        acc
    } else {
        let m = n - 1
        go(m, acc + n)
    }
}

fn main() -> Int {
    go(10000000, 0)
}
```

### `examples/tail_sum.cero`（新規）

```text
// expected output: 50000005000000

fn sumTo(n: Int, acc: Int) -> Int {
    if n == 0 {
        acc
    } else {
        sumTo(n - 1, acc + n)
    }
}

fn main() -> Int {
    sumTo(10000000, 0)
}
```

## 完了条件

* `make check`（gofmt + go vet + go test）が通る。
* wasmtime がある環境で、`internal/driver` の E2E が skip されずに通る（追加した 7 ケースを含む）。
* `make build` の後、`./bin/ceroc run examples/tail_sum.cero` が `50000005000000` を出力する。
* `./bin/ceroc run examples/fib.cero` が引き続き `55` を出力する。
