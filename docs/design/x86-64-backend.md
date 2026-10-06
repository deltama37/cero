# 設計: x86-64 backend

## 目的

ADR-0019 のとおり、Cero 製 ceroc に `build --target x86_64-linux` を追加し、IR から Linux の x86-64 の静的な ELF64 実行ファイルを書き出す。WebAssembly 版と同じ振る舞いを差分テストで確かめる。

対象外: 最適化、x86-64 以外と Linux 以外、Go 製 ceroc への実装、デバッグ情報。

## 関連する ADR

ADR-0001、ADR-0003、ADR-0005、ADR-0008、ADR-0010、ADR-0012、ADR-0014、ADR-0015、ADR-0018、ADR-0019

## 前提

実装は 2 つの単位に分け、この順に行う。各単位の終わりに `make check`、`make selfhost`、`make fmt-cero-check`、`make vscode-test` が通ること。

1. **骨組み**: アセンブラ、ELF、コード生成（文字列と IO の `Prim` を除くすべて）、起動と `alloc` と trap と整数の出力
2. **補助関数と仕上げ**: 文字列と IO の補助関数、`Command`（`main: () -> IO[Unit]`）、自分自身のネイティブのビルド、CI

## module

| module | 内容 |
| --- | --- |
| `compiler/x86/asm.cero` | 命令の列とラベル、機械語への符号化 |
| `compiler/x86/elf.cero` | ELF64 のヘッダとプログラムヘッダ |
| `compiler/x86/codegen.cero` | IR の関数 → 命令の列 |
| `compiler/x86/runtime.cero` | `_start`、`alloc`、trap、整数の出力（単位 1）、文字列と IO（単位 2） |
| `compiler/x86/link.cero` | 配置（アドレスの決定）、ラベルの解決、ファイルの組み立て |
| `compiler/main.cero` | `build` の `--target x86_64-linux`（`--target wasm32` も受け付ける。既定は wasm32） |

公開する名前には接頭辞 `x6`（構築子は `X6...`）を付ける。

## アセンブラ（`asm.cero`）

```text
pub type X6Reg = | Rax | Rcx | Rdx | Rbx | Rsp | Rbp | Rsi | Rdi | R8 | R9 | R10 | R11
pub type X6Cond = | CEq | CNe | CLt | CLe | CGt | CGe | CB | CBe | CA | CAe     // 符号つき・符号なし
pub type X6Ins =
    | X6Bytes(List[Int])                 // そのままのバイト列
    | X6Label(Int)                       // ラベルの位置
    | X6Jmp(Int) | X6Jcc(X6Cond, Int) | X6Call(Int)     // rel32 で符号化（大きさが固定）
    | X6MovLabel(X6Reg, Int)             // movabs reg, ラベルの絶対アドレス（10 バイト）
pub fn x6Size(i: X6Ins) -> Int
```

* 命令を組み立てる関数（`x6MovRR`、`x6MovRI64`、`x6Load64(dst, base, disp32)`、`x6Store64`、`x6Load32`、`x6Store32`、`x6Load8u`、`x6Store8`、`x6Push`、`x6Pop`、`x6AddRR`、`x6SubRR`、`x6ImulRR`、`x6Cqo`、`x6IdivR`、`x6CmpRR`、`x6CmpRI32`、`x6Setcc`、`x6MovzxB`、`x6AndRR`、`x6OrRR`、`x6XorRR`、`x6ShlCl`、`x6SarCl`、`x6ShrCl`、`x6Neg`、`x6RetN(n)`、`x6Syscall`、`x6CallR`、`x6JmpR`、`x6Lea` など）は `X6Bytes` を返す。必要なものを足してよい。
* 変位は常に `disp32`、即値の移動は必要に応じて `imm32`（符号拡張）か `imm64`。大きさを固定にして、ラベルの解決を 2 回の走査で済ませる。
* 各命令の符号化は、単体のテスト（下記）で既知のバイト列と比べる。

## ELF と配置（`elf.cero`、`link.cero`）

* 2 つの `PT_LOAD`:
  * テキスト: ファイルの先頭（ヘッダを含む）から、仮想アドレス `0x400000`、`R+X`。
  * データ: ファイルのオフセットを 4096 の倍数にそろえ、仮想アドレス `0x400000 + オフセット`、`R+W`。中身は、文字列リテラル（v0.8 と同じレイアウト、8 の倍数にそろえる）、関数のテーブル（8 バイトのアドレスの配列）、大域変数（ヒープの次の位置、ヒープの終わり、`argc`、`argv`）。
* ELF ヘッダの `e_entry` は `_start` のラベル。`e_type = ET_EXEC`、`e_machine = 62`。セクションヘッダは持たない。

## コード生成（`codegen.cero`）

### フレームと呼び出し規約

```text
引数を左から順に push → call f
f の入口:  push rbp; mov rbp, rsp; sub rsp, 8 * len(Locals)
           引数 i（n 個中）を [rbp + 16 + 8 * (n - 1 - i)] から局所変数 i の [rbp - 8 * (i + 1)] に写す
局所変数 j: [rbp - 8 * (j + 1)]
戻り:      結果を rax に置き、mov rsp, rbp; pop rbp; ret 8 * n
```

* 関数値の呼び出し（`CallIndirect`）: 引数を push した後、呼ばれる値を評価して `rax` に置き、`rdx = rax >> 32`（環境）を push、`ecx = eax`（テーブルの番号）、`rax = [テーブル + rcx * 8]`、`call rax`。テーブルの関数は引数の最後に環境を受け取る（ADR-0008）。
* 末尾呼び出し（`Tail`）: 引数（関数値なら環境まで）を push した後、
  1. 戻り先 `[rbp + 8]` と、保存した `rbp` の値 `[rbp]` を、使わないレジスタ（`r10`、`r11`）に読む。
  2. 新しい引数 m 個を、`rbp + 16 + 8 * n`（今の関数の引数の領域の上端）の下に、上から順に写す。写す元（今の `rsp` から）と先が重なる場合に壊さないよう、両者のアドレスの差（コンパイル時に分かる。積んだ数を数えておく）から写す向きを決める。
  3. `rsp` を新しい引数の下端 − 8 にし、そこに戻り先を書き、`rbp` を保存した値に戻し、`jmp`（関数値なら `jmp rax`）。
  呼ばれた側が `ret 8 * m` で取り除くので、元の呼び出し元から見たスタックの高さは変わらない。

### 式（結果は `rax`）

| IR | 符号化の方針 |
| --- | --- |
| `IntConst` / `BoolConst` | `mov rax, imm` |
| `StrConst` | `mov rax, リテラルのアドレス` |
| `LocalGet` | `mov rax, [rbp - 8(j+1)]` |
| `Unary` | `Neg`: `neg rax`。`Not`: `xor rax, 1` |
| `Binary` | X を評価して push、Y を評価して `rcx` に、`pop rax`、演算。比較は `cmp` と `setcc` と `movzx`。`Div` / `Rem` は下記 |
| `If` | 条件が 0 なら else へ |
| `Block` | 各 `let` の値を局所変数に書き、結果を評価 |
| `Call` | 上の規約 |
| `FuncValue` | 環境なし: `mov rax, index`。あり: 環境を評価、`shl rax, 32`、`or rax, index` |
| `Construct` | フィールドを順に push、`push 8 + 8k`、`call alloc`、タグを `[rax]`（4 バイト）に、0 を `[rax + 4]` に、フィールドを逆順に pop して `[rax + 8 + 8i]`（8 バイト）に |
| `Field` | `mov rax, [局所変数]; mov rax, [rax + 8 + 8i]` |
| `SwitchTag` | `mov rax, [局所変数]; mov eax, [rax]` の後、タグの比較の連鎖。`Default` がなければ最後の場合は比較しない |
| `Prim` | 単位 1: `StrLength`（`mov eax, [rax]`）とビット演算（`and`・`or`・`xor`・`shl cl`・`sar cl`・`shr cl`）をその場で。それ以外は単位 2 の補助関数を `call`（引数を push、呼ばれた側が取り除く） |

* `Div` / `Rem`: 除数が 0 なら trap。除数が -1 なら、`Div` は被除数が最小値なら trap、そうでなければ `neg`。`Rem` は 0。それ以外は `cqo; idiv rcx`（商は `rax`、余りは `rdx`）。
* データ型のフィールドは、値型にかかわらず 8 バイトで読み書きする（`Ptr` と `Bool` は 0 拡張済みの 64 bit の値）。文字列のブロック（長さ 4 バイト + 未使用 4 バイト + バイト列）のレイアウトは v0.8 のまま。

### 起動（`runtime.cero`）

```text
_start:
    [argc] = [rsp]; [argv] = rsp + 8
    mmap(0x40000000, 0xBFFF0000, PROT_READ|PROT_WRITE, MAP_PRIVATE|MAP_ANONYMOUS|MAP_NORESERVE|MAP_FIXED_NOREPLACE, -1, 0)
    失敗（rax が -4095..-1）なら trap
    [heap] = 0x40000000; [heapEnd] = 0xFFFF0000
    call main
    Command でなければ: print_int(rax); write(1, "\n"); exit_group(0)
    Command なら: 関数値として引数なしで呼ぶ（環境を push、テーブルから呼ぶ）; exit_group(0)

alloc(size):            // 引数はスタック。8 の倍数でなくても 8 の倍数に切り上げる
    p = [heap]; q = p + ((size + 7) & ~7)
    q > [heapEnd]（符号なし）なら trap
    [heap] = q; return p

trap:  write(2, "error: runtime trap\n"); exit_group(134)
print_int(n): v0.8 の intToString と同じ 10 進の表記を、スタック上のバッファに作って write(1, ...)
```

システムコールの番号: `read` 0、`write` 1、`close` 3、`mmap` 9、`openat` 257、`exit_group` 231。

## 単位 1 のテスト

* `internal/selfhost/x86_test.go`（新規）。Linux の x86-64 でないとき、または wasmtime がないときは skip する。
  * 対象: 差分テストのコーパス（`examples/`、`testdata/check_ok/`、`testdata/programs/`）のうち、`main: () -> Int` で、IR に文字列と IO の `Prim`（`StrLength` とビット演算を除く）を含まないもの（Go の `lower` の結果で判定する）。
  * 期待: Go 製の WebAssembly を `wasmtime run --invoke main` で実行した標準出力と、終了コードが 0 か 134 か。
  * 実際: Cero 製 ceroc（WebAssembly）の `build --target x86_64-linux FILE -o internal/selfhost/testdata/.out/x86/NAME` で書き出し、`chmod +x` して実行した標準出力と終了コード。
* アセンブラの単体のテスト（`TestX86Encoding`）: Cero で小さなプログラムを書き、`x6...` の命令を符号化したバイト列を標準出力に書いて、Go のテストで既知のバイト列（手で調べた値）と比べる。目安 40 命令。テスト用の Cero のエントリは `internal/selfhost/testdata/x86enc/main.cero` に置き、`compiler/` の module を import できるよう、`compiler/` に置く方が簡単ならそこでもよい（その場合は `compiler/x86/enc_test_main.cero` とし、self-hosting の対象の `compiler/main.cero` からは import しない）。
* 末尾呼び出しの深さ（`tail_sum` の 1000 万回、引数の数が違う関数への末尾呼び出し、関数値の末尾呼び出し）、0 除算と最小値 ÷ -1 の trap、最小値 % -1 = 0 を、対象に必ず含める。

## 単位 2: 補助関数と仕上げ

### 補助関数

WebAssembly 版（`internal/wasm/strfunc.go`、`iofunc.go`）と同じ動作のものを x86-64 で書く。引数はスタック（左から積む）、呼ばれた側が取り除く。

| 補助関数 | 動作の基準 |
| --- | --- |
| `str_alloc`、`str_byte_at`、`str_slice`、`str_from_byte`、`str_compare`、`int_to_string`、`str_concat`、`str_eq` | v0.8（範囲外は trap） |
| `io_write(fd, s)` | `write` を全部書けるまで繰り返す（失敗は trap） |
| `io_print`、`io_eprint` | fd 1、2 |
| `io_read_fd(fd)` | 終わりまで読む（バッファを 2 倍にしていく） |
| `io_read_stdin`、`io_read_chunk(max)` | v0.10、ADR-0017 |
| `io_read_file(path)`、`io_file_exists(path)`、`io_write_file(path, s)` | パスを NUL 終端に写し、`openat(AT_FDCWD, ...)`（読み: `O_RDONLY`、書き: `O_WRONLY\|O_CREAT\|O_TRUNC`、モード 0644）。失敗時のメッセージと終了コード 1 は v0.10 と同じ |
| `io_arg_count`、`io_arg_at(i)` | `argc - 1`、`argv[i + 1]`（範囲外は trap） |
| `io_exit(code)` | `exit_group(code)` |

使われた補助関数だけを出力する。

### `Command`

`main: () -> IO[Unit]` は、上の `_start` の Command の経路で実行する。

### ネイティブの ceroc

* `scripts/selfhost.sh` に次を足す（ADR-0015 の 3 段の後）。

```text
stage1-x86 = stage1.wasm で compiler/main.cero を --target x86_64-linux でビルドしたもの（chmod +x）
native が stage1-x86 で compiler/main.cero を wasm32 でビルドした結果 == stage1.wasm
stage2-x86 = stage1-x86 で compiler/main.cero を --target x86_64-linux でビルドしたもの
stage2-x86 == stage1-x86（x86-64 の不動点）
```

  x86-64 の Linux でない環境では、この部分を飛ばして、そのことを表示する。各段の時間とピークのメモリを表示する。
* `stage1-x86` の出力先は作業ディレクトリ（リポジトリの外）でよい。ネイティブの ceroc はカレントディレクトリからの相対パスと `openat` で読むので、preopen の制限はない。WebAssembly 版の ceroc が書き出すときは、出力先をリポジトリの中にする必要がある（`--dir=.`）。作業ディレクトリとの間でファイルを動かしてよい。

### 単位 2 のテスト

* `x86_test.go` の対象を、コーパスのすべてのプログラムに広げる（`Command` のものは、標準入力を空、引数なしで実行し、標準出力・標準エラー出力・終了コードを WebAssembly 版と比べる）。
* `examples/io/cat.cero`（ファイル 2 つ、存在しないファイル）、`examples/io/wc.cero`（標準入力）、`examples/io/hello.cero` を、同じ入力で WebAssembly 版と比べる。
* `make selfhost` の x86-64 の部分が通る。

### `README.md`

x86-64 の節（`scripts/ceroc-cero build --target x86_64-linux FILE -o OUT`、`chmod +x`、振る舞いの一致、制限）と、ネイティブの ceroc の作り方。

## 完了条件

* 単位 1・2 の各テストが通る。
* `make check`、`make selfhost`（x86-64 の不動点を含む）、`make fmt-cero-check`、`make vscode-test` が通る。CI green。
