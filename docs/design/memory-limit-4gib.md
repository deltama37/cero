# 設計: メモリの上限を 65535 ページにする

## 目的

ADR-0018 のとおり、Go 製と Cero 製の WebAssembly の出力で、メモリの最大を 65535 ページにし、`alloc` を桁あふれしない計算に直す。

対象外: 回収、確保の削減、memory64。

## 関連する ADR

ADR-0003、ADR-0010、ADR-0014、ADR-0015、ADR-0018

## 変更するファイル

| ファイル | 内容 |
| --- | --- |
| `internal/wasm/encode.go`, `encode_test.go` | `memoryMaxPages = 65535`（コメントを ADR-0018 に合わせる）、`encodeAllocBody` |
| `internal/wasm/strfunc.go`, `iofunc.go` | アドレスどうしの符号つきの比較があれば符号なしに |
| `compiler/wasm/encode.cero`, `strfunc.cero`, `iofunc.cero` | 上と同じ変更（バイト列を Go と一致させる） |
| `docs/adr/0015-self-hosting.md` | 「1.5 GiB」を「3 GiB（ADR-0018）」に直す（決定の更新なので ADR の本文を直す） |
| `README.md` | メモリの上限の記述があれば直す |

## `alloc` の本体

型は今までどおり `(i32 size) -> i32`。局所変数を `p: i32`（今まで）に加えて `end: i64` を持つ。

```text
p = heap
end = i64.extend_i32_u(heap) + i64.extend_i32_u(size)
if end > i64.extend_i32_u(memory.size) << 16:            // 符号なしの比較（i64.gt_u）
    if end > 65535 << 16: unreachable                   // 上限を超える
    pages = (end - (i64.extend_i32_u(memory.size) << 16) + 65535) >> 16    // i64
    if memory.grow(i32.wrap_i64(pages)) == -1: unreachable
heap = i32.wrap_i64(end)
return p
```

* `heap` が 0xFFFF0000 ちょうどになることはあっても、それを超えることはない（上限の検査）。
* 局所変数の宣言は `01 01 7F` から、`p`（i32）と `end`（i64）の 2 つのまとまりに変わる。

## 補助関数の監査

`strfunc.go` と `iofunc.go` で、アドレス（`alloc` の結果、`heap`、文字列のブロックのアドレス、それに 8 や添字を足したもの）どうしを `i32.lt_s` / `i32.gt_s` / `i32.le_s` / `i32.ge_s` で比べている箇所を探し、`_u` に直す。長さ（`i32.load` した長さ）と、`Int` から来た添字の比較はそのままでよい。直した箇所の一覧を最終報告に書く。見つからなければ「なし」と報告する。

## テスト

* `encode_test.go`: メモリのセクションの最大が 65535（LEB128 で `ff ff 03`）、`alloc` の本体のバイト列。既存のゴールデンの期待値を新しい規則で直す。
* E2E（`internal/driver`）に、2 GiB を超えて確保するケースを 1 つ足す（例: 1 MiB の文字列を `++` で 2,200 回作って、最後の長さを返す。実行に時間がかかりすぎる（10 秒以上）なら、大きな文字列を少ない回数作る形にする）。メモリの少ない CI でも動くように、同時に保持するのは小さくする（確保したまま回収されないので、確保の合計だけが 2 GiB を超えればよい）。
* `internal/selfhost` の差分テスト（バイト列の一致）と `make selfhost` がそのまま通る。

## 完了条件

* `make check`、`make selfhost`、`make fmt-cero-check`、`make vscode-test` が通る。
* 2 GiB を超えて確保する E2E が通る。
