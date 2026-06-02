#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
OUT="${OUT:-$ROOT/dist}"
mkdir -p "$OUT/client"

cd "$ROOT"
go mod tidy

build_one() {
  local goos="$1" goarch="$2" out="$3" pkg="$4"
  echo "==> $goos/$goarch -> $out (CGO_ENABLED=0, static)"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags="-s -w" -o "$out" "$pkg"
  if command -v file >/dev/null 2>&1 && [[ "$goos" == linux ]]; then
    file "$out" || true
  fi
}

# Server: Linux amd64, Windows amd64
build_one linux   amd64 "$OUT/server_linux_amd64" ./cmd/server
build_one windows amd64 "$OUT/server_windows_amd64.exe" ./cmd/server

# Client (agent): Linux + Windows, amd64 + 386
build_one linux   amd64 "$OUT/client/agent_linux_amd64" ./cmd/agent
build_one linux   386   "$OUT/client/agent_linux_386" ./cmd/agent
build_one windows amd64 "$OUT/client/agent_windows_amd64.exe" ./cmd/agent
build_one windows 386   "$OUT/client/agent_windows_386.exe" ./cmd/agent

echo "Done -> $OUT"
ls -la "$OUT" "$OUT/client" 2>/dev/null || true