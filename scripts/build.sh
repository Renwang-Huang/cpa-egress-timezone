#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
target_os="${GOOS:-$(go env GOOS)}"
target_arch="${GOARCH:-$(go env GOARCH)}"
if [[ "$target_os" != linux || ( "$target_arch" != amd64 && "$target_arch" != arm64 ) ]]; then
  printf '%s\n' 'Supported release targets: linux/amd64 and linux/arm64.' >&2
  exit 1
fi
mkdir -p "build/$target_os/$target_arch"
CGO_ENABLED=1 GOOS="$target_os" GOARCH="$target_arch" go build \
  -buildvcs=false -trimpath -buildmode=c-shared -ldflags='-s -w' \
  -o "build/$target_os/$target_arch/egress-timezone.so" .
printf 'Built build/%s/%s/egress-timezone.so\n' "$target_os" "$target_arch"
