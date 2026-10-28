#!/usr/bin/env bash
# Jalankan binary bServer (tanpa build).
set -euo pipefail
cd "$(dirname "$0")"
exec ./dist/bServer-linux-amd64 "$@"
