#!/usr/bin/env bash
# Builds the release files into dist/. The release workflow runs this on a tag, and you can run it
# yourself to see exactly what a release would contain:
#
#   scripts/package.sh v1.2.3
#
# For each kind of Raspberry Pi OS it makes a tarball holding the listener and its installer, and a
# bare binary; checksums.txt covers them all.
#   arm64  64-bit Raspberry Pi OS (Pi 3, 4, 5, Zero 2 W)
#   armv6  32-bit Raspberry Pi OS (any Pi, including Zero and 1; it also runs on every newer 32-bit Pi)
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${1:-dev}"
NAME=now-playing-listener
rm -rf dist
mkdir -p dist

# build LABEL GOARCH [GOARM]
build() {
  local label=$1 goarch=$2 goarm=${3:-}
  local base="${NAME}_${VERSION}_linux_${label}"
  mkdir -p "dist/$base"
  GOOS=linux GOARCH="$goarch" GOARM="$goarm" CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "dist/$base/$NAME" ./cmd/listener
  cp deploy/setup.sh deploy/now-playing-listener.service deploy/listener.env.example README.md "dist/$base/"
  chmod 755 "dist/$base/$NAME" "dist/$base/setup.sh"
  tar -C dist -czf "dist/$base.tar.gz" --owner=0 --group=0 --numeric-owner "$base"
  cp "dist/$base/$NAME" "dist/${NAME}-linux-${label}"
  rm -rf "dist/$base"
}

build arm64 arm64
build armv6 arm 6

(cd dist && sha256sum "${NAME}"* > checksums.txt)
echo "Built ${VERSION}:"
(cd dist && ls -l)
