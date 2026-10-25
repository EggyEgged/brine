#!/bin/sh
# Build brine-<version>-1.x86_64.rpm and the matching .src.rpm into dist/.
#
#   packaging/rpm/build-rpm.sh              package the current HEAD
#   REF=v0.1.1 packaging/rpm/build-rpm.sh   package a tag or commit
#
# Uses the host's rpmbuild and Go on a Fedora/RHEL machine; anywhere else it builds inside a
# Fedora container (docker or podman), which works on any distro and in CI.
# The source package carries the vendored Go modules, so it builds offline (COPR, mock).
set -eu

root=$(cd "$(dirname "$0")/../.." && pwd)
spec=$root/packaging/rpm/brine.spec
version=$(sed -n 's/^Version:[[:space:]]*//p' "$spec")
out=$root/dist

if [ "${1:-}" = "--inside" ]; then
    # Runs where rpmbuild and Go exist. Expects $WORK/SOURCES/brine-$version.tar.gz.
    work=$2
    src=$work/SOURCES/brine-$version.tar.gz
    tmp=$(mktemp -d)
    tar -xzf "$src" -C "$tmp"
    (cd "$tmp/brine-$version" && go mod vendor)
    tar -C "$tmp/brine-$version" -cJf "$work/SOURCES/brine-$version-vendor.tar.xz" vendor
    rm -rf "$tmp"
    rpmbuild -ba --define "_topdir $work" --define "dist %{nil}" "$spec"
    mkdir -p "$out"
    cp "$work"/RPMS/x86_64/*.rpm "$work"/SRPMS/*.rpm "$out"/
    exit 0
fi

if [ -n "${VERSION:-}" ] && [ "$VERSION" != "$version" ]; then
    echo "build-rpm: the spec says Version: $version but VERSION=$VERSION; update packaging/rpm/brine.spec" >&2
    exit 1
fi

work=$root/dist/rpmbuild
rm -rf "$work"
mkdir -p "$work/SOURCES" "$work/BUILD" "$work/RPMS" "$work/SRPMS" "$work/SPECS"
git -C "$root" archive --format=tar.gz --prefix="brine-$version/" -o "$work/SOURCES/brine-$version.tar.gz" "${REF:-HEAD}"
[ -z "$(git -C "$root" status --porcelain)" ] || echo "build-rpm: note: uncommitted changes are not included (packaging ${REF:-HEAD})" >&2

# Build on the host only if it is an RPM system that actually has the BuildRequires
# (a Fedora/RHEL machine). Debian-style hosts that merely have rpmbuild installed
# (CI runners) don't, so they use the Fedora container.
if command -v rpmbuild >/dev/null 2>&1 && command -v go >/dev/null 2>&1 \
   && rpm -q --quiet golang desktop-file-utils 2>/dev/null; then
    "$0" --inside "$work"
else
    engine=$(command -v docker || command -v podman || true)
    [ -n "$engine" ] || { echo "build-rpm: need rpmbuild+go, or docker/podman" >&2; exit 1; }
    image=${FEDORA_IMAGE:-fedora:latest}
    mkdir -p "$out"
    # The container runs as root; whatever happens (even a failed build), hand
    # the files back to the invoking user so the next run can clean them up.
    "$engine" run --rm -e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" \
        -v "$root:/src:ro" -v "$work:/work" -v "$out:/dist" -w /src "$image" sh -ec '
        trap "chown -R $HOST_UID:$HOST_GID /work /dist" EXIT
        dnf install -y -q rpm-build golang desktop-file-utils tar xz gzip >/dev/null
        # /src is read-only: build from a copy so the spec path is stable.
        cp -r /src/packaging /tmp/pkg
        sed "s|^spec=.*|spec=/tmp/pkg/rpm/brine.spec|; s|^root=.*|root=/src|; s|^out=.*|out=/dist|" /src/packaging/rpm/build-rpm.sh > /tmp/build-rpm.sh
        sh /tmp/build-rpm.sh --inside /work
    '
fi
ls -1 "$out"/brine-"$version"-*.rpm
