#!/bin/sh
# hlm installer: curl -fsSL https://raw.githubusercontent.com/GujaLomsadze/helm/main/install.sh | sh
set -eu
REPO="GujaLomsadze/helm"
DIR="${HLM_INSTALL_DIR:-$HOME/.local/bin}"

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  A=$(printf '\033[1;38;2;255;90;74m'); OK=$(printf '\033[38;2;255;122;107m')
  W=$(printf '\033[38;2;255;178;122m'); B=$(printf '\033[1m'); D=$(printf '\033[2m'); R=$(printf '\033[0m')
else
  A=; OK=; W=; B=; D=; R=
fi
step() { printf '  %s✓%s %s\n' "$OK" "$R" "$1"; }
die() { printf '  %s✗%s %s\n' "$W" "$R" "$1" >&2; exit 1; }

printf '\n  %sHELM.EXE%s  %sinstaller%s\n\n' "$A" "$R" "$D" "$R"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  *) die "unsupported OS '$os' (on Windows, get the .zip from github.com/$REPO/releases)" ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture '$arch'" ;;
esac

asset="hlm_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/latest/download"
[ -n "${HLM_VERSION:-}" ] && base="https://github.com/$REPO/releases/download/$HLM_VERSION"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$base/$asset" -o "$tmp/$asset" || die "download failed: $base/$asset"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" || die "download failed: checksums.txt"
step "Downloaded ${B}${os}/${arch}${R}"

want=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
else
  got=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)
fi
[ -n "$want" ] && [ "$want" = "$got" ] || die "checksum mismatch for $asset"
step "Checksum verified"

tar -xzf "$tmp/$asset" -C "$tmp" hlm
mkdir -p "$DIR"
install -m 0755 "$tmp/hlm" "$DIR/hlm"
version=$("$DIR/hlm" version 2>/dev/null | cut -d' ' -f2)
step "Installed ${B}hlm ${version}${R} ${D}→ $DIR/hlm${R}"

# Helm was called TRK: an old trk next to it becomes a link to hlm, so hooks that still
# call trk run the new version until `hlm init` points them at hlm.
if [ -e "$DIR/trk" ] || [ -L "$DIR/trk" ]; then
  ln -sf hlm "$DIR/trk"
  step "${B}trk${R} now runs hlm ${D}(TRK is now Helm; run hlm init to switch your hooks)${R}"
fi

case ":$PATH:" in
  *":$DIR:"*) ;;
  *) printf '\n  %s!%s %s is not on your PATH. Add this to your shell profile:\n      %sexport PATH="%s:$PATH"%s\n' "$W" "$R" "$DIR" "$B" "$DIR" "$R" ;;
esac

printf '\n  %sNext%s\n' "$B" "$R"
printf '    %s1%s  %shlm init%s   %sconnect Claude Code (backs up your settings first)%s\n' "$A" "$R" "$A" "$R" "$D" "$R"
printf '    %s2%s  %shlm open%s   %sopen the dashboard%s\n\n' "$A" "$R" "$A" "$R" "$D" "$R"
