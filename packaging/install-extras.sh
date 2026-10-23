#!/bin/sh
# Install the man page and shell completions for the current user.
# Inputs (environment): PREFIX, MAN (path to brine.1), COMP (directory with
# brine.bash, _brine and brine.fish). Used by install.sh and `make install`.
set -eu
: "${PREFIX:?}" "${MAN:?}" "${COMP:?}"
data=${XDG_DATA_HOME:-$HOME/.local/share}
conf=${XDG_CONFIG_HOME:-$HOME/.config}
install -Dm644 "$MAN" "$PREFIX/share/man/man1/brine.1"
install -Dm644 "$COMP/brine.bash" "$data/bash-completion/completions/brine"
install -Dm644 "$COMP/_brine" "$PREFIX/share/zsh/site-functions/_brine"
install -Dm644 "$COMP/brine.fish" "$conf/fish/completions/brine.fish"
