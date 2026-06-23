#!/usr/bin/env bash
set -euo pipefail

PKG="./cmd/kkd-leaf"
OUT="dist"
LDFLAGS="-s -w"

mkdir -p "$OUT"

build() {
  local goos="$1" goarch="$2" goarm="$3" name="$4"
  local env_extra=""
  if [[ -n "$goarm" ]]; then
    env_extra="GOARM=$goarm"
  fi
  echo "  building $name..."
  env CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" $env_extra \
    go build -ldflags "$LDFLAGS" -trimpath -o "$OUT/$name" "$PKG"
}

echo "knock-knock-deflock build-all"
echo "=============================="
build linux arm   6 kkd-leaf-linux-arm6
build linux arm   7 kkd-leaf-linux-arm7
build linux arm64 "" kkd-leaf-linux-arm64
build linux amd64 "" kkd-leaf-linux-amd64
echo ""
echo "artifacts:"
ls -lh "$OUT"/kkd-leaf-* 2>/dev/null || echo "  (none)"
