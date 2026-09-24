# ADR-0001: Cero 言語の初期方針

* Status: Accepted
* Date: 2026-09-24

## Context

新しいプログラミング言語 **Cero** を自作する。

目的は既存言語との競争ではなく、以下を実際に構築することでプログラミング言語処理系、とくに型システムを理解すること。

* 字句解析・構文解析
* 型検査・型推論
* 多相
* 代数的データ型
* 中間表現
* コード生成
* ランタイム
* 副作用の表現
* ブートストラップ
* 自己ホスト

最初から多機能な言語を作るのではなく、最小限の機能から開始し、実装上・学習上の必要性に応じて段階的に成長させる。

## Decision

### 言語名

言語名を **Cero** とする。

`cero` はスペイン語で「0」を意味する。

「0から言語を作る」「最小の状態から徐々に成長させる」という方針を表す。

### CLI

コンパイラおよびCLIの基本コマンドを以下とする。

```bash
ceroc
```

`cero` は既存コマンドとの衝突があるため使用しない。

将来的には以下のようなインターフェースを想定する。

```bash
ceroc build
ceroc run
ceroc fmt
```

`ceroc fmt` は self-hosting の後に追加する（「Development Policy」の成長過程を参照）。

## 初期コンパイラ

初期の `ceroc` は **Goで実装する**。

最終的にはCero自身で `ceroc` を再実装する。

```text
Go製 ceroc
    ↓
Ceroプログラムをコンパイル
    ↓
Cero製 ceroc を実装
    ↓
Go製 ceroc で Cero製 ceroc をコンパイル
    ↓
Cero製 ceroc で自身をコンパイル
```

最後の状態を自己ホスト達成とする。

## プログラミングパラダイム

Ceroは **純粋関数型言語** とする。

ML系言語の型システムやデータ表現を参考にしつつ、Cero独自の仕様として段階的に設計する。

基本原則は以下。

* 値は原則として不変
* 関数を第一級の値として扱う
* 通常の関数は副作用を持たない
* 制御構造も可能な限り式として扱う
* 繰り返しは再帰を基本とする
* 代数的データ型を採用する
* パターンマッチを採用する
* パラメトリック多相を採用する
* 型推論を段階的に導入する
* 副作用は型として明示する
* クラス継承を中心としたオブジェクト指向は採用しない

特定の既存言語を再実装することは目的としない。

### 副作用

ファイル操作、標準入出力、時刻取得などの副作用を通常の純粋関数から直接実行できない設計とする。

概念的には以下のように、副作用を型で区別する。

```text
fn parse(source: String) -> Ast

fn typecheck(ast: Ast) -> Result[TypedAst, Error]

fn readFile(path: String) -> IO[String]
```

`IO` の具体的な仕組みは初期段階では固定せず、後続バージョンで設計する。

将来的には以下のような方式を検討対象とする。

* `IO` 型
* Effect system
* Algebraic Effects

どの方式を採用する場合でも、

> 副作用が通常の純粋関数に暗黙的に入り込まない

ことを基本原則とする。

## コンパイル先

最初のコード生成先は **WebAssembly** とする。

```text
Cero source
    ↓
AST
    ↓
Type checking
    ↓
IR
    ↓
WebAssembly
```

将来的には同じIRからx86-64など別のバックエンドを追加できる構造とする。

```text
             ┌─ WebAssembly
Cero → IR ───┤
             └─ x86-64 (future)
```

Ceroの言語仕様自体はWebAssembly固有の仕様へ強く依存させない。

## 実行環境

WebAssemblyはブラウザ専用技術として扱わない。

Ceroでは **ローカル環境での実行を基準** とする。

```text
main.cero
   ↓
ceroc
   ↓
main.wasm
   ↓
WebAssembly Runtime
```

初期の実行環境として `wasmtime` などのWebAssemblyランタイムを想定する。

```bash
ceroc build main.cero
wasmtime main.wasm
```

将来的には、

```bash
ceroc run main.cero
```

によってコンパイルと実行をまとめて行えるようにする。

### WASI

WebAssembly自体にはOSのファイルシステムや標準入出力へ直接アクセスする標準機能がない。

CeroからOS機能を利用する場合は **WASI** をホスト環境との境界として利用する。

```text
Cero
 ↓
WebAssembly
 ↓
WASI
 ↓
WebAssembly Runtime
 ↓
OS
```

対象には以下を想定する。

* 標準入力
* 標準出力
* ファイル操作
* 環境変数
* その他のホスト機能

ただし、

**WASIを利用できることと、Ceroの純粋関数から直接WASIを呼び出せることは別とする。**

WASIへのアクセスは将来導入する `IO` やEffectなど、副作用を表現する仕組みを経由する。

ブラウザ実行への対応も将来的に可能とするが、v0.1の必須要件にはしない。

## 中間表現

ASTから直接WebAssemblyを生成せず、独自の中間表現を挟む。

目的は以下。

* 言語仕様と実行先の分離
* 複数バックエンドへの対応
* コード生成処理の単純化
* 将来的な最適化

## Cero v0.1 Language Specification

v0.1の目的は、

> 小さな純粋関数型プログラムを静的型検査し、WebAssemblyへコンパイルしてローカルで実行できる状態を作ること

とする。

型システムは最初から完成形を実装せず、単純な型検査から段階的に発展させる。

### 基本型

v0.1では以下を扱う。

```text
Int
Bool
```

`Int` は64bit符号付き整数とする。

### 値

値は不変とする。

```text
let x = 10
let y = x + 20
```

再代入はできない。

```text
let x = 10
x = 20 // compile error
```

`var` のような可変変数は導入しない。

### 関数

名前付き関数を定義できる。

```text
fn add(a: Int, b: Int) -> Int {
    a + b
}
```

関数本体は式として評価され、その値が関数の結果となる。

`return` 文は導入しない。

```text
fn abs(x: Int) -> Int {
    if x < 0 {
        -x
    } else {
        x
    }
}
```

### 再帰

名前付き関数の再帰呼び出しをサポートする。

```text
fn fib(n: Int) -> Int {
    if n <= 1 {
        n
    } else {
        fib(n - 1) + fib(n - 2)
    }
}
```

v0.1では `while` や `for` を導入せず、繰り返しは再帰によって表現する。

末尾再帰最適化はv0.1の必須要件とはしない。

### 第一級関数

関数を値として扱える設計とする。

匿名関数をサポートする。

```text
let double = fn(x: Int) -> Int {
    x * 2
}
```

関数を引数として渡せる。

```text
fn apply(f: Int -> Int, x: Int) -> Int {
    f(x)
}
```

v0.1ではクロージャが外側の変数を捕捉する機能は必須としない。

### 式

最低限以下をサポートする。

```text
1
true
false

a + b
a - b
a * b
a / b

a == b
a != b
a < b
a <= b
a > b
a >= b

a && b
a || b
!a
```

### 条件分岐

`if` は文ではなく式とする。

```text
let value =
    if condition {
        10
    } else {
        20
    }
```

両方の分岐は互換性のある型を返す必要がある。

```text
if condition {
    10
} else {
    false
}
```

上記は型エラーとする。

### ローカル束縛

関数内で `let` により値を束縛できる。

```text
fn calc(x: Int) -> Int {
    let doubled = x * 2
    let added = doubled + 10

    added
}
```

最後の式が関数またはブロックの値となる。

### スコープ

波括弧によるレキシカルスコープを持つ。

シャドーイングを許可する。

```text
let x = 1

let x = 2
```

既存の値を書き換えるのではなく、新しい束縛を作る。

### 型注釈

関数の引数と戻り値については、v0.1では明示的な型注釈を要求する。

```text
fn add(a: Int, b: Int) -> Int {
    a + b
}
```

ローカル変数については右辺から型を推論する。

```text
let x = 10
```

v0.1では完全なHindley–Milner型推論は実装しない。

### 型検査

v0.1から静的型検査を行う。

```text
let x: Int = true
```

```text
1 + false
```

などはコンパイルエラーとする。

暗黙的な型変換は原則として導入しない。

## v0.1では導入しないが早期に追加する型機能

### 代数的データ型

早期に以下のような型を導入する。

```text
type Option[T] =
    | Some(T)
    | None
```

### パターンマッチ

```text
match value {
    Some(x) => x
    None => 0
}
```

コンパイラは将来的にパターンの網羅性を検査する。

### パラメトリック多相

```text
fn identity[T](x: T) -> T {
    x
}
```

のような多相関数を導入する。

### 型推論

最終的には、

```text
fn identity(x) {
    x
}
```

のようなコードから概念的に、

```text
identity : a -> a
```

を導出できる型推論を目指す。

Hindley–Milner型システムを重要な参考対象とする。

ただし、Ceroが最終的に厳密なHM型システムを採用するかは、後続ADRで決定する。

## 副作用とv0.1

v0.1のCeroプログラムは基本的に純粋な計算のみを扱う。

そのため、v0.1の言語仕様には以下を含めない。

* ファイルI/O
* 標準入力
* 標準出力
* 可変状態
* 時刻
* 乱数
* ネットワーク

コンパイル時のファイル読み込みなどは、Goで実装された `ceroc` が担当する。

Cero自身で `ceroc` を実装する段階までに、純粋性を維持しながら副作用を扱う仕組みを導入する。

## エントリポイント

v0.1では実行可能プログラムに `main` 関数を要求する。

```text
fn main() -> Int {
    fib(10)
}
```

`main` 自体もv0.1では純粋関数とする。

返された整数値をWebAssemblyランタイムまたは `ceroc run` が取得できればよい。

## コメント

単一行コメントをサポートする。

```text
// comment
```

## コンパイルエラー

最低限以下を表示する。

* ファイル名
* 行番号
* 列番号
* エラー内容

```text
main.cero:4:12: expected Int, found Bool
```

## v0.1に含めない機能

* String
* Float
* List / Array
* Tuple
* record / struct
* 代数的データ型
* pattern matching
* generics / polymorphism
* 完全な型推論
* type class / trait
* module
* package manager
* mutable variable
* while / for
* class
* async / await
* IO
* Effect system
* GC
* 例外
* Result / Option
* マクロ
* FFI
* x86-64コード生成

## Development Policy

言語機能は以下の観点を優先して追加する。

1. 型システムの理解を一段ずつ深められること
2. 純粋関数型言語として一貫性があること
3. Cero自身でコンパイラを書くために必要であること
4. 標準ライブラリを構築するために必要であること
5. 一般用途で有用であること

「他の言語に存在するから」という理由だけでは追加しない。

想定する成長過程は以下。

```text
v0.1
Int / Bool
純粋関数
第一級関数
再帰
if expression
単相型検査

↓

代数的データ型
pattern matching
String / List

↓

型変数
パラメトリック多相
単一化

↓

let多相
型推論

↓

module

↓

IO / Effect

↓

Cero製 ceroc

↓

self-hosting

↓

ceroc fmt

↓

x86-64 backend
```

`ceroc fmt` は self-hosting の達成後に、Cero製 `ceroc` の機能として追加する。

* self-hosting までは構文が段階的に増えるため、それ以前にフォーマッタを作ると構文の追加ごとに作り直しが必要になる。
* Go製 `ceroc` はいずれ置き換えるので、Go でフォーマッタを実装する労力を避ける。
* スタイル規則やコメントの扱いは、追加する時点で別の ADR として決める。

この順序は固定されたロードマップではなく、実装・学習上の要求に応じて変更する。

## Initial Goal

最初の具体的な完成条件として、以下をコンパイル・実行できることを目標とする。

```text
fn fib(n: Int) -> Int {
    if n <= 1 {
        n
    } else {
        fib(n - 1) + fib(n - 2)
    }
}

fn main() -> Int {
    fib(10)
}
```

実行経路は以下。

```text
Cero source
    ↓
Go製 ceroc
    ↓
WebAssembly
    ↓
WebAssembly Runtime
    ↓
ローカル実行
```

副作用が必要になった段階では、

```text
Cero
    ↓
IO / Effect
    ↓
WebAssembly
    ↓
WASI
    ↓
WebAssembly Runtime
    ↓
OS
```

という境界を設ける。

最終的にはCero自身で `ceroc` を実装し、自己ホストを達成する。
