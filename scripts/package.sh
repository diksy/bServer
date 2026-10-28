#!/usr/bin/env bash
# Kemas artefak distribusi: tarball (linux/darwin) & zip (windows).
# Prasyarat: jalankan `make build-all` lebih dulu (binary sudah di dist/).
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${1:-0.1.0}"
OUT=dist
RELEASE="$OUT/release"
mkdir -p "$RELEASE"

for f in "$OUT"/bserver-*; do
  [ -e "$f" ] || continue
  name=$(basename "$f")
  # sertakan README + LICENSE (bila ada) di tiap paket
  case "$name" in
    *.exe)
      zip -j "$RELEASE/${name%.exe}-$VERSION.zip" "$f" README.bserver.md >/dev/null
      ;;
    *)
      # tarball berisi binary + README
      tmp=$(mktemp -d)
      cp "$f" "$tmp/bserver"
      cp README.bserver.md "$tmp/README.md"
      tar -C "$tmp" -czf "$RELEASE/$name-$VERSION.tar.gz" bserver README.md
      rm -rf "$tmp"
      ;;
  esac
done

echo "Paket rilis:"
ls -la "$RELEASE"
