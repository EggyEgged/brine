#!/bin/sh
# Install the RPM in clean Fedora containers and check the package behaves.
#   packaging/rpm/test-rpm.sh [rpm-file] [image...]      (default images: fedora:latest fedora:43)
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
rpm=${1:-$(ls "$root"/dist/brine-*.x86_64.rpm | head -1)}
[ $# -gt 0 ] && shift
images=${*:-fedora:latest fedora:43}
engine=$(command -v docker || command -v podman) || { echo "need docker or podman" >&2; exit 1; }
rpm=$(cd "$(dirname "$rpm")" && pwd)/$(basename "$rpm")

for image in $images; do
    echo "--> $image: install, run, remove $(basename "$rpm")"
    "$engine" run --rm -v "$rpm:/pkg/brine.rpm:ro" "$image" sh -ec '
        cat /etc/fedora-release
        dnf install -y -q --setopt=tsflags= /pkg/brine.rpm >/dev/null   # container images set tsflags=nodocs, which would skip the man page
        rpm -q brine
        rpm -V brine                                    # files match the package
        test -x /usr/bin/brine
        for f in /usr/share/applications/brine.desktop /usr/share/icons/hicolor/scalable/apps/brine.svg \
                 /usr/share/mime/packages/brine-studio.xml /usr/share/bash-completion/completions/brine \
                 /usr/share/zsh/site-functions/_brine /usr/share/fish/vendor_completions.d/brine.fish \
                 /usr/share/man/man1/brine.1.gz /usr/share/licenses/brine/LICENSE; do
            test -s "$f" || { echo "missing $f"; exit 1; }
        done
        # Text-only commands work as root (package builds need that); anything that
        # touches Wine or files must refuse root.
        brine version
        brine doctor --quick 2>&1 | grep -q "refusing to run as root" || { echo "doctor ran as root"; exit 1; }
        dnf install -y -q util-linux >/dev/null   # test-only: provides runuser
        useradd -m tester
        runuser -u tester -- sh -ec "
            brine version
            brine completion bash | bash -n
            brine desktop install >/dev/null
            grep -q \"^Exec=/usr/bin/brine run %u\" ~/.local/share/applications/brine.desktop
            brine desktop remove >/dev/null
            brine doctor --quick > /tmp/doctor.txt 2>&1 || true   # fails on a bare container (no Wine/prefix); must not crash
            grep -q \"wine runtime\" /tmp/doctor.txt
        "
        dnf remove -y -q brine >/dev/null
        test ! -e /usr/bin/brine
        echo "ok"
    '
done
echo PASS
