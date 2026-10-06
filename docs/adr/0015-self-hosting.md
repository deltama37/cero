# ADR-0015: self-hosting の達成と検査

* Status: Accepted
* Date: 2026-10-06

## Context

ADR-0001 は「Go 製 ceroc で Cero 製 ceroc をコンパイルし、Cero 製 ceroc で自身をコンパイルする」状態を自己ホストの達成とした。
ADR-0014 で、Cero 製 ceroc（`compiler/`）を Go 製の忠実な移植とし、同じ入力に同じ WebAssembly を出すと決めた。
段階 4 で、Cero 製 ceroc が `compiler/main.cero` を Go 製と同じバイト列にコンパイルできるようになった（約 1 秒、ピーク RSS 約 954 MB）。

自己ホストが壊れていないことを、継続的に検査する仕組みが要る。

## Decision

### 自己ホストの定義

次の 3 段のビルドを行う。

```text
stage1.wasm = Go 製 ceroc で compiler/main.cero をビルドしたもの
stage2.wasm = stage1.wasm で compiler/main.cero をビルドしたもの
stage3.wasm = stage2.wasm で compiler/main.cero をビルドしたもの
```

* **自己ホストの達成**: `stage2.wasm` と `stage3.wasm` がバイト単位で一致する（不動点）。Cero 製 ceroc が、自分自身を正しくコンパイルできていることを表す。
* **移植の一致**（ADR-0014）: さらに `stage1.wasm` と `stage2.wasm` も一致する。
* どちらかが一致しなければ失敗とする。

### 検査の仕組み

* `scripts/selfhost.sh` が上の 3 段のビルドを行い、各段の大きさと時間を表示して、一致しなければ 0 以外で終わる。
* `make selfhost` はこのスクリプトを実行する。CI（`.github/workflows/ci.yml`）の `check` ジョブで、テストの後に実行する。
* `make selfhost-build` は `bin/ceroc-cero.wasm`（stage1）を作る。`scripts/ceroc-cero` は、リポジトリのルートで `wasmtime run --dir=. bin/ceroc-cero.wasm "$@"` を実行する薄いラッパーとする。

### 2 つのコンパイラの役割

* Go 製 ceroc は、ブートストラップのコンパイラとして残す（ADR-0014）。バイナリの種（コミットした WebAssembly）は置かない。Go と wasmtime があれば、ソースだけから自己ホストを再現できる。
* `ceroc fmt`、LSP、x86-64 backend は Cero 製 ceroc にだけ実装する（ADR-0001）。

### メモリ

* 自己ビルドのピークは約 954 MB で、上限（2 GiB、ADR-0003）の半分に近い。ADR-0010 の「回収しない」方針はこのまま続ける。
* `scripts/selfhost.sh` はピークのメモリも表示する（測れる環境では）。自己ビルドが 3 GiB（ADR-0018）を超えたら、回収の方式の ADR を書く。

## Consequences

* ADR-0001 の成長過程の「Cero 製 ceroc」と「self-hosting」の段階が完了する。
* CI の時間が、3 段のビルドの分（数秒）増える。
* Cero 製 ceroc を壊す変更（Go 製と出力が食い違う変更、Cero 製 ceroc のソースが使う機能を Go 製から取り除く変更）は、CI で検出される。
