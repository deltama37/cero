# 設計: self-hosting の検査

## 目的

ADR-0015 のとおり、3 段のビルドで自己ホストの不動点を確かめるスクリプトと、`make` のターゲット、CI のステップ、ラッパー、文書を追加する。

対象外: Cero 製 ceroc の機能の追加、メモリの削減。

## 関連する ADR

ADR-0001、ADR-0014、ADR-0015

## 変更・追加するファイル

| ファイル | 内容 |
| --- | --- |
| `scripts/selfhost.sh`（新規） | 3 段のビルドと比較 |
| `scripts/ceroc-cero`（新規、実行可能） | `bin/ceroc-cero.wasm` のラッパー |
| `Makefile` | `selfhost` ターゲット |
| `.github/workflows/ci.yml` | `make selfhost` のステップ |
| `README.md` | self-hosting の段落 |
| `AGENTS.md` | リポジトリ構成に `compiler/`、`std/`、`internal/selfhost` を追加。検証コマンドに `make selfhost` を追加 |

## `scripts/selfhost.sh`

```text
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."             # リポジトリのルート
out=${SELFHOST_OUT:-$(mktemp -d)}   # 作業ディレクトリ（リポジトリの外でよい。stage2 以降の出力は標準出力で受け取る）
go build -o "$out/ceroc" ./cmd/ceroc
"$out/ceroc" build compiler/main.cero -o "$out/stage1.wasm"
wasmtime run --dir=. "$out/stage1.wasm" build compiler/main.cero -o - > "$out/stage2.wasm"
wasmtime run --dir=. "$out/stage2.wasm" build compiler/main.cero -o - > "$out/stage3.wasm"
各段の大きさ（バイト）と時間（秒）を表示する
/usr/bin/time -v が使えれば stage2 のビルドのピーク RSS も表示する（使えなければ省略）
cmp stage2 stage3 が失敗したら "self-hosting: stage2 and stage3 differ" を表示して exit 1
cmp stage1 stage2 が失敗したら "self-hosting: stage1 (Go) and stage2 (Cero) differ" を表示して exit 1
"self-hosting: OK (stage1 == stage2 == stage3)" を表示する
SELFHOST_OUT を指定しなかったときは作業ディレクトリを消す（trap）
```

* wasmtime が `PATH` になければ、メッセージを出して exit 1（CI では必ず入っている）。

## `scripts/ceroc-cero`

```text
#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
exec wasmtime run --dir=. "$root/bin/ceroc-cero.wasm" "$@"
```

カレントディレクトリが preopen されるので、ファイルはカレントディレクトリからの相対パスで渡す（ADR-0014）。

## `Makefile`

```make
# selfhost checks that the Cero-written compiler reproduces itself (ADR-0015).
selfhost:
	./scripts/selfhost.sh
```

`.PHONY` に `selfhost` と `selfhost-build` を加える。`check` には含めない（CI では別のステップとして実行する）。

## CI

`check` ジョブの `go test` の後に、次のステップを追加する。

```yaml
      - name: self-hosting
        run: make selfhost
```

## README.md

「Self-hosting」の節を追加する。内容: `compiler/` が Cero で書いた ceroc であること、`make selfhost` の 3 段のビルドと不動点の意味、`make selfhost-build` と `scripts/ceroc-cero build examples/fib.cero` の使い方、Go 製 ceroc がブートストラップとして残ること、ADR-0014 と ADR-0015 へのリンク。

## テスト観点

* `make selfhost` がローカルで成功し、OK のメッセージと各段の大きさを表示する。
* `compiler/` のソースを一時的に壊した場合（たとえば `encode.cero` の定数を 1 つ変える）に、`make selfhost` が失敗することを手で確かめる（変更は元に戻し、コミットしない）。
* `scripts/ceroc-cero build examples/fib.cero -o /tmp/...` はリポジトリの外に書けないので、`-o -` で標準出力に書いた結果を `wasmtime run --invoke main` で実行して `55` になることを確かめる。

## 完了条件

* `make check` と `make selfhost` が通る。
* CI に self-hosting のステップが入り、green になる。
