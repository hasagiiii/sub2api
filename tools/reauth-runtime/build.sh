#!/usr/bin/env bash
set -euo pipefail
: "${VERSION:?set VERSION}"
: "${ARCH:?set ARCH}"
if [[ ! $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$ ]]; then
  echo "error: VERSION '$VERSION' is not semver (expected X.Y.Z or X.Y.Z-suffix, e.g. 0.2.15-custom.1)" >&2
  exit 1
fi
if [[ $ARCH != amd64 && $ARCH != arm64 ]]; then
  echo "error: ARCH '$ARCH' is not supported (expected amd64 or arm64)" >&2
  exit 1
fi
runtime_output=$(mktemp -d)
trap 'rm -rf "$runtime_output"' EXIT
docker buildx build --platform "linux/$ARCH" -f tools/reauth-runtime/Dockerfile \
  --output "type=local,dest=$runtime_output" .
mkdir -p reauth-output
tar --dereference -czf "reauth-output/sub2api-reauth_${VERSION}_linux_${ARCH}.tar.gz" -C "$runtime_output" .
sha256sum "reauth-output/sub2api-reauth_${VERSION}_linux_${ARCH}.tar.gz"
