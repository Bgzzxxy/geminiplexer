#!/usr/bin/env bash
set -e
export PATH="/home/bagus/.local/go/bin:$PATH"
mkdir -p bin
go build -ldflags="-s -w" -o bin/gemini-to-api cmd/server/main.go
echo "Built binary at bin/gemini-to-api"
