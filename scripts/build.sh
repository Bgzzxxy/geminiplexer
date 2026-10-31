#!/usr/bin/env bash
set -e

# Ensure Go is on PATH
if command -v go >/dev/null 2>&1; then
    GO_BIN="go"
elif [ -x "/home/bagus/.local/go/bin/go" ]; then
    GO_BIN="/home/bagus/.local/go/bin/go"
else
    echo "Error: go compiler not found on PATH"
    exit 1
fi

mkdir -p bin

build_target() {
    local os=$1
    local arch=$2
    local output=$3
    echo "[BUILD] Building for ${os}/${arch} -> ${output} ..."
    GOOS=${os} GOARCH=${arch} ${GO_BIN} build -ldflags="-s -w" -o "${output}" ./cmd/server
}

if [ "$1" = "all" ]; then
    echo "=== Building Gemini Relay for all platforms ==="
    build_target "linux" "amd64" "bin/gemini-relay-linux-amd64"
    build_target "linux" "arm64" "bin/gemini-relay-linux-arm64"
    build_target "darwin" "amd64" "bin/gemini-relay-darwin-amd64"
    build_target "darwin" "arm64" "bin/gemini-relay-darwin-arm64"
    build_target "windows" "amd64" "bin/gemini-relay-windows-amd64.exe"
    build_target "windows" "arm64" "bin/gemini-relay-windows-arm64.exe"
    echo "=== All platform builds completed successfully in bin/ ==="
else
    build_target "linux" "amd64" "bin/gemini-to-api"
    echo "Built local binary at bin/gemini-to-api"
fi
