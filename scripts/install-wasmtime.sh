#!/usr/bin/env bash
# Installs a pinned wasmtime into /usr/local/bin (used by `ceroc run` and the
# end-to-end tests). Safe to run repeatedly: it does nothing when the pinned
# version is already installed.
set -euo pipefail

WASMTIME_VERSION="${WASMTIME_VERSION:-25.0.3}"
INSTALL_DIR="${WASMTIME_INSTALL_DIR:-/usr/local/bin}"

if command -v wasmtime >/dev/null 2>&1 &&
	wasmtime --version | grep -q "^wasmtime ${WASMTIME_VERSION} "; then
	echo "wasmtime ${WASMTIME_VERSION} is already installed"
	exit 0
fi

case "$(uname -m)" in
x86_64 | amd64) arch="x86_64" ;;
aarch64 | arm64) arch="aarch64" ;;
*)
	echo "unsupported architecture: $(uname -m)" >&2
	exit 1
	;;
esac

name="wasmtime-v${WASMTIME_VERSION}-${arch}-linux"
url="https://github.com/bytecodealliance/wasmtime/releases/download/v${WASMTIME_VERSION}/${name}.tar.xz"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

curl -sSfL --retry 4 "$url" -o "$tmp/wasmtime.tar.xz"
tar -xJf "$tmp/wasmtime.tar.xz" -C "$tmp"

if [ -w "$INSTALL_DIR" ]; then
	install -m 0755 "$tmp/$name/wasmtime" "$INSTALL_DIR/wasmtime"
else
	sudo install -m 0755 "$tmp/$name/wasmtime" "$INSTALL_DIR/wasmtime"
fi

wasmtime --version
