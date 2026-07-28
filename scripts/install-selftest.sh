#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd -P)
fixture="$root/.tmp/install-selftest"
rm -rf "$fixture"
mkdir -p "$fixture/home" "$fixture/install-bin" "$fixture/tmp"
chmod 700 "$fixture/home" "$fixture/install-bin" "$fixture/tmp"
trap 'rm -rf "$fixture"' EXIT HUP INT TERM

candidate="$fixture/candidate-omg"
log="$fixture/agent-install.log"
cat > "$candidate" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "${OMG_INSTALL_TEST_LOG:?}"
case "$*" in
  "agent install") exit 0 ;;
  *) exit 9 ;;
esac
EOF
chmod 0755 "$candidate"

if command -v sha256sum >/dev/null 2>&1; then
  checksum=$(sha256sum "$candidate" | awk '{print $1}')
else
  checksum=$(shasum -a 256 "$candidate" | awk '{print $1}')
fi

run_install() {
  HOME="$fixture/home" \
  SHELL=/bin/zsh \
  PATH=/usr/bin:/bin \
  TMPDIR="$fixture/tmp" \
  OMG_INSTALL_DIR="$fixture/install-bin" \
  OMG_INSTALL_SOURCE="$candidate" \
  OMG_INSTALL_SHA256="$checksum" \
  OMG_INSTALL_TEST_LOG="$log" \
  sh "$root/install" >/dev/null
}

run_install
run_install

installed="$fixture/install-bin/omg"
test -x "$installed"
installed_checksum=$(
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$installed" | awk '{print $1}'
  else
    shasum -a 256 "$installed" | awk '{print $1}'
  fi
)
test "$installed_checksum" = "$checksum"
test "$(grep -c '^agent install$' "$log")" -eq 2
for profile in "$fixture/home/.profile" "$fixture/home/.zprofile"; do
  test -f "$profile"
  test "$(grep -c '^# >>> OMG PATH >>>$' "$profile")" -eq 1
  test "$(grep -c '^# <<< OMG PATH <<<$' "$profile")" -eq 1
  grep -F "export PATH=\"$fixture/install-bin:\$PATH\"" "$profile" >/dev/null
done

test ! -e "$fixture/install-bin/.omg-install-$$"
printf 'INSTALL_SELFTEST_PASS\n'
