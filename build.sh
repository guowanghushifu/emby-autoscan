#!/usr/bin/env sh
set -eu

APP_NAME="emby-autoscan"
CMD_PATH="./cmd/emby-autoscan"
OUTPUT_DIR="${OUTPUT_DIR:-dist}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-}"
CGO_ENABLED="${CGO_ENABLED:-0}"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || printf 'dev')}"
TARGET_ARCHES="${TARGET_ARCHES:-amd64 arm64}"

build_target() {
  target_goarch="$1"
  output="${2:-${OUTPUT_DIR}/${APP_NAME}-${GOOS}-${target_goarch}}"

  mkdir -p "$(dirname "$output")"

  printf 'Building %s\n' "$APP_NAME"
  printf '  version: %s\n' "$VERSION"
  printf '  target:  %s/%s\n' "$GOOS" "$target_goarch"
  printf '  output:  %s\n' "$output"

  CGO_ENABLED="$CGO_ENABLED" GOOS="$GOOS" GOARCH="$target_goarch" \
    go build \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o "$output" \
      "$CMD_PATH"

  printf 'Build complete: %s\n' "$output"
}

if [ -n "${OUTPUT:-}" ] && [ -z "$GOARCH" ]; then
  printf 'GOARCH must be set when OUTPUT is set\n' >&2
  exit 1
fi

if [ -n "$GOARCH" ]; then
  build_target "$GOARCH" "${OUTPUT:-}"
else
  for target_goarch in $TARGET_ARCHES; do
    build_target "$target_goarch"
  done
fi
