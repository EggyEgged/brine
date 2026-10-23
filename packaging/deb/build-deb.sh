#!/bin/sh
# Build a .deb from build/brine (and build/completions) with dpkg-deb, or
# with ar+tar where dpkg-deb is missing. Run via `make deb`.
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
install -Dm644 "$root/build/completions/brine.bash" "$stage/usr/share/bash-completion/completions/brine"
install -Dm644 "$root/build/completions/_brine" "$stage/usr/share/zsh/vendor-completions/_brine"
install -Dm644 "$root/build/completions/brine.fish" "$stage/usr/share/fish/vendor_completions.d/brine.fish"
install -d "$stage/usr/share/man/man1"
gzip -9n -c "$root/packaging/brine.1" > "$stage/usr/share/man/man1/brine.1.gz"
install -Dm644 "$root/README.md" "$stage/usr/share/doc/brine/README.md"
install -Dm644 "$root/CHANGELOG.md" "$stage/usr/share/doc/brine/changelog"
gzip -9n "$stage/usr/share/doc/brine/changelog"
install -Dm644 "$root/LICENSE" "$stage/usr/share/doc/brine/copyright"
mkdir -p "$stage/DEBIAN"
size=$(du -sk "$stage" | cut -f1)
cat > "$stage/DEBIAN/control" <<CTL
Package: brine
Version: $VERSION
Section: games
Priority: optional
Architecture: amd64
Installed-Size: $size
Depends: tar, xz-utils
Recommends: xdg-utils, desktop-file-utils, shared-mime-info, libvulkan1
Suggests: wine
Maintainer: Brine contributors <186115781+EggyEgged@users.noreply.github.com>
Homepage: https://github.com/EggyEgged/brine
Description: Unsandboxed Roblox Studio launcher for Wine with MCP bridge
 Installs, updates and runs Roblox Studio under Wine without Flatpak,
 and connects Studio's built-in MCP server to Linux MCP clients such as
 Claude Code and Claude Desktop. Not affiliated with Roblox Corporation.
CTL
cat > "$stage/DEBIAN/postinst" <<'SH'
#!/bin/sh
set -e
if [ "$1" = configure ]; then
    command -v update-mime-database >/dev/null 2>&1 && update-mime-database /usr/share/mime || true
    command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database -q /usr/share/applications || true
    command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q -t /usr/share/icons/hicolor || true
fi
exit 0
SH
cp "$stage/DEBIAN/postinst" "$stage/DEBIAN/postrm"
sed -i 's/\[ "\$1" = configure \]/[ "$1" = remove ] || [ "$1" = purge ]/' "$stage/DEBIAN/postrm"
chmod 755 "$stage/DEBIAN/postinst" "$stage/DEBIAN/postrm"
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
