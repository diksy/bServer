#!/usr/bin/env bash
# Cross-compile bServer ke semua platform target (pure-Go, tanpa cgo).
# Prasyarat: Go 1.24+ (toolchain lintas-platform), frontend sudah di-build.
set -euo pipefail
cd "$(dirname "$0")/.."

OUT=dist
mkdir -p "$OUT"

targets=(
  "linux amd64"
  "linux arm64"
  "windows amd64"
  "darwin amd64"
  "darwin arm64"
)

for t in "${targets[@]}"; do
  set -- $t
  os=$1; arch=$2
  ext=""
  [ "$os" = "windows" ] && ext=".exe"
  out="$OUT/bserver-$os-$arch$ext"
  echo "==> build $os/$arch -> $out"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -o "$out" ./cmd/bserver
done

echo "Selesai. Hasil:"
ls -la "$OUT"/bserver-*
