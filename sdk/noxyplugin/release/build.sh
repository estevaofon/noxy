#!/usr/bin/env sh
set -eu
NAME="${1:?usage: build.sh <extension-name>}"
mkdir -p dist
for os in linux darwin windows; do for arch in amd64 arm64; do
  ext=""; [ "$os" = windows ] && ext=".exe"
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags=-s \
    -o "dist/noxy-plugin-$NAME-$os-$arch$ext" .
done; done
(cd dist && sha256sum -- * > checksums.txt)
echo "dist/ is ready: gh release create <tag> dist/*"
