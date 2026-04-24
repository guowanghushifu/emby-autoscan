#!/usr/bin/env sh
set -eu

APP_NAME="fuse-mount-emby-notify"
CMD_PATH="./cmd/fuse-mount-emby-notify"
OUTPUT_DIR="${OUTPUT_DIR:-dist}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
CGO_ENABLED="${CGO_ENABLED:-0}"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || printf 'dev')}"
OUTPUT="${OUTPUT:-${OUTPUT_DIR}/${APP_NAME}-${GOOS}-${GOARCH}}"

mkdir -p "$(dirname "$OUTPUT")"

printf 'Building %s\n' "$APP_NAME"
printf '  version: %s\n' "$VERSION"
printf '  target:  %s/%s\n' "$GOOS" "$GOARCH"
printf '  output:  %s\n' "$OUTPUT"

CGO_ENABLED="$CGO_ENABLED" GOOS="$GOOS" GOARCH="$GOARCH" \
  go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o "$OUTPUT" \
    "$CMD_PATH"

printf 'Build complete: %s\n' "$OUTPUT"
