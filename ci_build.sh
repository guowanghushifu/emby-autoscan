#!/usr/bin/env sh
set -eu

APP_NAME="emby-autoscan"
CMD_PATH="./cmd/emby-autoscan"
OUTPUT_DIR="${OUTPUT_DIR:-dist}"
GOOS="${GOOS:-linux}"
CGO_ENABLED="${CGO_ENABLED:-0}"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || printf 'dev')}"
TARGET_ARCHES="${TARGET_ARCHES:-amd64 arm64}"

build_archive() {
  target_goarch="$1"
  package_name="${APP_NAME}-${GOOS}-${target_goarch}"
  package_dir="${OUTPUT_DIR}/${package_name}"
  binary_path="${package_dir}/${package_name}"
  archive_path="${OUTPUT_DIR}/${package_name}.tar.gz"

  rm -rf "$package_dir" "$archive_path"
  mkdir -p "$package_dir"

  printf 'Building %s\n' "$APP_NAME"
  printf '  version: %s\n' "$VERSION"
  printf '  target:  %s/%s\n' "$GOOS" "$target_goarch"
  printf '  output:  %s\n' "$archive_path"

  CGO_ENABLED="$CGO_ENABLED" GOOS="$GOOS" GOARCH="$target_goarch" \
    go build \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o "$binary_path" \
      "$CMD_PATH"

  cp config.example.yaml "${package_dir}/config.example.yaml"
  cp run-forever.sh "${package_dir}/run-forever.sh"
  chmod +x "${package_dir}/run-forever.sh"

  tar -C "$OUTPUT_DIR" -czf "$archive_path" "$package_name"
  rm -rf "$package_dir"

  printf 'Archive complete: %s\n' "$archive_path"
}

rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

for target_goarch in $TARGET_ARCHES; do
  build_archive "$target_goarch"
done

(
  cd "$OUTPUT_DIR"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum ./*.tar.gz > SHA256SUMS
  else
    shasum -a 256 ./*.tar.gz > SHA256SUMS
  fi
)

printf 'Checksums written: %s\n' "${OUTPUT_DIR}/SHA256SUMS"
