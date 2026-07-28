#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd -P)
version="${OMG_RELEASE_VERSION:-}"
output="${OMG_RELEASE_OUTPUT:-$root/dist/release}"
targets="${OMG_RELEASE_TARGETS:-darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64}"

fail() {
  printf 'OMG release package: %s\n' "$1" >&2
  exit 1
}

[ -n "$version" ] || fail "OMG_RELEASE_VERSION is required"
case "$version" in
  *[!A-Za-z0-9._-]*) fail "OMG_RELEASE_VERSION contains unsupported characters" ;;
esac

if [ -e "$output" ]; then
  [ -d "$output" ] || fail "output exists and is not a directory"
  [ -z "$(find "$output" -mindepth 1 -maxdepth 1 -print -quit)" ] || fail "output directory must be empty"
else
  mkdir -p "$output"
fi

work="$output/.work"
mkdir -p "$work" "$output/.cache"
cleanup() {
  rm -rf "$work" "$output/.cache"
}
trap cleanup EXIT HUP INT TERM

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  fail "sha256sum or shasum is required"
}

package_target() {
  target="$1"
  os_name=${target%/*}
  arch_name=${target#*/}
  case "$os_name/$arch_name" in
    darwin/amd64|darwin/arm64|linux/amd64|linux/arm64|windows/amd64|windows/arm64) ;;
    *) fail "unsupported target: $target" ;;
  esac

  directory="$work/${os_name}_${arch_name}"
  mkdir -p "$directory"
  binary="$directory/omg"
  [ "$os_name" != windows ] || binary="$directory/omg.exe"

  (
    cd "$root"
    CGO_ENABLED=0 \
    GOOS="$os_name" \
    GOARCH="$arch_name" \
    GOCACHE="$output/.cache" \
    go build -mod=readonly -trimpath -ldflags "-s -w -X main.version=$version" -o "$binary" ./cmd/omg
  )

  if [ "$os_name" = windows ]; then
    asset="omg_${os_name}_${arch_name}.zip"
    python3 - "$directory" "$output/$asset" <<'PY'
import pathlib, sys, zipfile
source = pathlib.Path(sys.argv[1]) / "omg.exe"
destination = pathlib.Path(sys.argv[2])
with zipfile.ZipFile(destination, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
    info = zipfile.ZipInfo("omg.exe", date_time=(1980, 1, 1, 0, 0, 0))
    info.external_attr = 0o755 << 16
    archive.writestr(info, source.read_bytes(), compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
PY
  else
    asset="omg_${os_name}_${arch_name}.tar.gz"
    COPYFILE_DISABLE=1 tar -C "$directory" -czf "$output/$asset" omg
  fi
}

for target in $targets; do
  package_target "$target"
done

checksums="$output/checksums.txt"
: > "$checksums"
for asset in "$output"/omg_*.tar.gz "$output"/omg_*.zip; do
  [ -f "$asset" ] || continue
  printf '%s  %s\n' "$(sha256_file "$asset")" "$(basename "$asset")" >> "$checksums"
done
LC_ALL=C sort -o "$checksums" "$checksums"
[ -s "$checksums" ] || fail "no release assets were produced"

printf 'OMG release assets packaged.\n'
printf '  version  %s\n' "$version"
printf '  output   %s\n' "$output"
printf '  assets   %s\n' "$(wc -l < "$checksums" | tr -d ' ')"
