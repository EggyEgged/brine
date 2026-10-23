#!/bin/sh
# Build a .deb from build/brine with dpkg-deb. Run via `make deb`.
set -eu
VERSION=${VERSION:-0.1.0}
VERSION=$(printf %s "$VERSION" | sed 's/^v//; s/-/~/g')
root=$(cd "$(dirname "$0")/../.." && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
install -Dm755 "$root/build/brine" "$stage/usr/bin/brine"
install -Dm644 "$root/assets/brine.desktop" "$stage/usr/share/applications/brine.desktop"
install -Dm644 "$root/assets/brine.svg" "$stage/usr/share/icons/hicolor/scalable/apps/brine.svg"
install -Dm644 "$root/assets/brine-studio.xml" "$stage/usr/share/mime/packages/brine-studio.xml"
install -Dm644 "$root/README.md" "$stage/usr/share/doc/brine/README.md"
install -Dm644 "$root/LICENSE" "$stage/usr/share/doc/brine/copyright"
mkdir -p "$stage/DEBIAN"
cat > "$stage/DEBIAN/control" <<CTL
Package: brine
Version: $VERSION
Section: games
Priority: optional
Architecture: amd64
Depends: tar, xz-utils
Recommends: xdg-utils, desktop-file-utils, shared-mime-info, libvulkan1
Suggests: wine
Maintainer: Brine contributors <brine@example.invalid>
Description: Unsandboxed Roblox Studio launcher for Wine with MCP bridge
 Installs, updates and runs Roblox Studio under Wine without Flatpak,
 and connects Studio's built-in MCP server to Linux MCP clients such as
 Claude Code and Claude Desktop.
CTL
mkdir -p "$root/dist"
out="$root/dist/brine_${VERSION}_amd64.deb"
if command -v dpkg-deb >/dev/null 2>&1; then
    dpkg-deb --root-owner-group --build "$stage" "$out"
else
    # A .deb is an ar archive of debian-binary, control.tar.gz and data.tar.gz.
    work=$(mktemp -d)
    printf '2.0\n' > "$work/debian-binary"
    tar --owner=0 --group=0 --numeric-owner -C "$stage/DEBIAN" -czf "$work/control.tar.gz" .
    tar --owner=0 --group=0 --numeric-owner --exclude=./DEBIAN -C "$stage" -czf "$work/data.tar.gz" .
    rm -f "$out"
    (cd "$work" && ar rc "$out" debian-binary control.tar.gz data.tar.gz)
    rm -rf "$work"
fi
echo "built $out"
