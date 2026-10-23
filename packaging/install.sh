#!/bin/sh
# Install Brine for the current user from a release tarball. No root needed.
# Installs the binary, man page, shell completions and desktop integration.
#   ./install.sh            installs to ~/.local
#   PREFIX=/opt/x ./install.sh
set -eu
if [ "$(id -u)" = 0 ]; then
    echo "install.sh: run this as your normal user, not root" >&2
    exit 1
fi
here=$(cd "$(dirname "$0")" && pwd)
PREFIX=${PREFIX:-$HOME/.local}
install -Dm755 "$here/brine" "$PREFIX/bin/brine"
MAN="$here/brine.1" COMP="$here/completions" PREFIX="$PREFIX" sh "$here/install-extras.sh"
"$PREFIX/bin/brine" desktop install
case ":$PATH:" in
    *":$PREFIX/bin:"*) ;;
    *) echo "Note: $PREFIX/bin is not in your PATH; add it to your shell profile." ;;
esac
echo "Installed brine to $PREFIX/bin/brine. Next: brine setup"
