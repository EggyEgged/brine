#!/bin/sh
# End-to-end test: start Studio (unless it is already running), run
# `brine mcp` the way an MCP client does, round-trip list_roblox_studios and
# execute_luau, and confirm the print reached Studio's Output.
#
# Prerequisites: `brine setup` done, signed in to Studio, and
# "Enable Studio as MCP server" on (`brine mcp enable` while Studio is closed).
# A place must be open for execute_luau; this script opens a temporary one
# when it starts Studio itself.
#
#   scripts/integration-test.sh            uses `brine` from PATH (or build/brine)
#   BRINE=/path/to/brine scripts/integration-test.sh
#   KEEP_STUDIO=1 ...                      leave a Studio the script started running
set -eu

here=$(cd "$(dirname "$0")/.." && pwd)
BRINE=${BRINE:-}
if [ -z "$BRINE" ]; then
    if [ -x "$here/build/brine" ]; then BRINE=$here/build/brine; else BRINE=$(command -v brine); fi
fi
prefix=${XDG_DATA_HOME:-$HOME/.local/share}/brine/prefix
roblox="$prefix/drive_c/users/$USER/AppData/Local/Roblox"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
step() { echo "--> $*"; }

studio_running() { pgrep -u "$(id -u)" -f 'RobloxStudioBeta\.exe' >/dev/null 2>&1; }

step "doctor (quick)"
"$BRINE" doctor --quick || fail "brine doctor reported failures"

started=0
if ! studio_running; then
    [ -f "$here/scripts/fixtures/Integration.rbxlx" ] || fail "missing scripts/fixtures/Integration.rbxlx"
    cp "$here/scripts/fixtures/Integration.rbxlx" "$work/Integration.rbxlx"
    step "starting Studio with a temporary place"
    "$BRINE" run "$work/Integration.rbxlx" >"$work/run.log" 2>&1 &
    started=1
    i=0
    until studio_running; do
        i=$((i + 1)); [ $i -gt 60 ] && fail "Studio did not start (see $work/run.log)"
        sleep 1
    done
fi

marker="brine-integration-$(date +%s)-$$"
step "MCP round trip (initialize → list_roblox_studios → execute_luau)"
"$BRINE" mcp test --wait 180s --luau "print(\"$marker\") return \"$marker\"" >"$work/test.out" 2>&1 \
    || { cat "$work/test.out"; fail "brine mcp test failed"; }
cat "$work/test.out"
grep -q "$marker" "$work/test.out" || fail "execute_luau did not return the marker"

step "checking Studio's Output log for the print"
i=0
until grep -qs "$marker" "$(ls -t "$roblox"/logs/*.log | head -1)"; do
    i=$((i + 1)); [ $i -gt 20 ] && fail "\"$marker\" not found in Studio's log"
    sleep 1
done
echo "found \"$marker\" in Studio's Output"

step "checking that no MCP helper is left behind"
sleep 4
if pgrep -u "$(id -u)" -f 'StudioMCP\.exe' >/dev/null 2>&1 && ! pgrep -u "$(id -u)" -f 'brine mcp' >/dev/null 2>&1; then
    fail "orphaned StudioMCP.exe: $(pgrep -a -f 'StudioMCP\.exe')"
fi

if [ "$started" = 1 ] && [ "${KEEP_STUDIO:-0}" != 1 ]; then
    step "stopping the Studio this script started"
    "$BRINE" kill
fi
echo "PASS"
