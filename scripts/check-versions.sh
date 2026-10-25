#!/bin/sh
# Release hygiene: every place that names the version must agree.
#   scripts/check-versions.sh 0.1.1
set -eu
v=${1:?usage: check-versions.sh VERSION}
root=$(cd "$(dirname "$0")/.." && pwd)
bad=0
chk() { # name actual
    if [ "$2" != "$v" ]; then echo "check-versions: $1 is '$2', expected '$v'" >&2; bad=1; fi
}
chk "packaging/rpm/brine.spec Version" "$(sed -n 's/^Version:[[:space:]]*//p' "$root/packaging/rpm/brine.spec")"
chk "packaging/aur/PKGBUILD pkgver" "$(sed -n 's/^pkgver=//p' "$root/packaging/aur/PKGBUILD")"
grep -q "^## \[$v\] - " "$root/CHANGELOG.md" || { echo "check-versions: CHANGELOG.md has no '## [$v] - date' section" >&2; bad=1; }
grep -q "^%changelog" "$root/packaging/rpm/brine.spec" && grep -q " - $v-1\$" "$root/packaging/rpm/brine.spec" || { echo "check-versions: brine.spec %changelog has no entry for $v-1" >&2; bad=1; }
exit $bad
