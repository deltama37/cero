#!/usr/bin/env bash
# Checks the self-hosting fixed point (ADR-0015).
#
# stage1.wasm is compiler/main.cero built by the Go ceroc.
# stage2.wasm is compiler/main.cero built by stage1.
# stage3.wasm is compiler/main.cero built by stage2.
# stage2 must match stage3, and stage1 must match stage2.
#
# Work files go to $SELFHOST_OUT when that is set. Otherwise they go to a
# temporary directory, which is removed on exit. Stage 2 and stage 3 write
# WebAssembly to stdout (-o -). When /usr/bin/time -v works, the peak RSS of
# the stage 2 build is printed too.
set -euo pipefail

cd "$(dirname "$0")/.."

if ! command -v wasmtime >/dev/null 2>&1; then
	echo "self-hosting: wasmtime is not in PATH" >&2
	exit 1
fi

if [ -n "${SELFHOST_OUT:-}" ]; then
	out=$SELFHOST_OUT
	mkdir -p "$out"
else
	out=$(mktemp -d)
	trap 'rm -rf "$out"' EXIT
fi

# Seconds between two `date +%s%N` readings, with millisecond precision.
elapsed_s() {
	local ns=$(($2 - $1))
	printf '%d.%03d\n' $((ns / 1000000000)) $(((ns / 1000000) % 1000))
}

bytes_of() {
	wc -c <"$1" | tr -d '[:space:]'
}

report_stage() {
	local name=$1
	local file=$2
	local seconds=$3
	local rss=${4:-}
	local bytes
	bytes=$(bytes_of "$file")
	if [ -n "$rss" ]; then
		echo "${name}: ${bytes} bytes, ${seconds} s, peak RSS ${rss} kB"
	else
		echo "${name}: ${bytes} bytes, ${seconds} s"
	fi
}

# True when GNU time can record a verbose report. Otherwise peak RSS is omitted.
have_time_verbose() {
	[ -x /usr/bin/time ] && /usr/bin/time -v -o /dev/null true >/dev/null 2>&1
}

go build -o "$out/ceroc" ./cmd/ceroc

t0=$(date +%s%N)
"$out/ceroc" build compiler/main.cero -o "$out/stage1.wasm"
t1=$(date +%s%N)
stage1_s=$(elapsed_s "$t0" "$t1")

stage2_rss=""
t0=$(date +%s%N)
if have_time_verbose; then
	/usr/bin/time -v -o "$out/stage2.time" \
		wasmtime run --dir=. "$out/stage1.wasm" build compiler/main.cero -o - \
		>"$out/stage2.wasm"
	stage2_rss=$(
		sed -n 's/.*Maximum resident set size (kbytes):[[:space:]]*\([0-9][0-9]*\).*/\1/p' \
			"$out/stage2.time" | head -n 1
	)
else
	wasmtime run --dir=. "$out/stage1.wasm" build compiler/main.cero -o - \
		>"$out/stage2.wasm"
fi
t1=$(date +%s%N)
stage2_s=$(elapsed_s "$t0" "$t1")

t0=$(date +%s%N)
wasmtime run --dir=. "$out/stage2.wasm" build compiler/main.cero -o - \
	>"$out/stage3.wasm"
t1=$(date +%s%N)
stage3_s=$(elapsed_s "$t0" "$t1")

report_stage stage1 "$out/stage1.wasm" "$stage1_s"
report_stage stage2 "$out/stage2.wasm" "$stage2_s" "$stage2_rss"
report_stage stage3 "$out/stage3.wasm" "$stage3_s"

if ! cmp -s "$out/stage2.wasm" "$out/stage3.wasm"; then
	echo "self-hosting: stage2 and stage3 differ" >&2
	exit 1
fi
if ! cmp -s "$out/stage1.wasm" "$out/stage2.wasm"; then
	echo "self-hosting: stage1 (Go) and stage2 (Cero) differ" >&2
	exit 1
fi

echo "self-hosting: OK (stage1 == stage2 == stage3)"
