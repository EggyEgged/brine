#!/bin/sh
# Live test: links and files reach Studio the way a browser or file manager sends them.
# Needs a set-up Brine (`brine setup`) and a graphical session. Opens (and closes) real
# Studio windows. Uses public place 1818 for link tests; permission errors inside Studio are fine.
#
#   BRINE=/path/to/brine scripts/link-test.sh
set -u
BRINE=${BRINE:-$(command -v brine)}
fail() { echo "FAIL: $*" >&2; "$BRINE" kill >/dev/null 2>&1; exit 1; }
step() { echo "--> $*"; }
studio() { ps -eo args | grep -E '[R]obloxStudioBeta\.exe' | grep -v msedge; }
wait_studio() { n=0; until studio | grep -q "$1"; do n=$((n+1)); [ $n -gt 60 ] && fail "Studio never started with: $1"; sleep 1; done; }
ms() { echo $(( ($(date +%s%N) - $1) / 1000000 )); }
# Don't run over a real session: these tests run `brine kill`.
if [ "${BRINE_TEST_FORCE:-0}" != 1 ]; then
    if pgrep -a -x brine | grep -qE ' mcp( |$)'; then
        echo "Refusing to run: another MCP client is connected (brine mcp). Close it, or set BRINE_TEST_FORCE=1." >&2; exit 2
    fi
    if ps -eo args | grep -E '[R]obloxStudioBeta\.exe' | grep -vq msedge; then
        echo "Refusing to run: Studio is already running. Close it, or set BRINE_TEST_FORCE=1." >&2; exit 2
    fi
fi

work=$(mktemp -d); trap 'rm -rf "$work"; "$BRINE" kill >/dev/null 2>&1' EXIT
"$BRINE" kill >/dev/null 2>&1; sleep 2

step "registered handlers point at brine"
for t in x-scheme-handler/roblox-studio x-scheme-handler/roblox-studio-auth application/x-roblox-place application/x-roblox-place+xml; do
    [ "$(xdg-mime query default $t)" = brine.desktop ] || fail "$t is handled by '$(xdg-mime query default $t)'"
done

step "roblox-studio: link via xdg-open returns at once and starts Studio"
t0=$(date +%s%N); timeout 20 xdg-open 'roblox-studio:1+launchmode:edit+task:EditPlace+placeId:1818+universeId:13058' || fail "xdg-open failed"
[ "$(ms $t0)" -lt 3000 ] || fail "xdg-open took $(ms $t0) ms (the launcher must hand off)"
wait_studio 'roblox-studio:1+launchmode:edit+task:EditPlace+placeId:1818'
"$BRINE" kill >/dev/null 2>&1; sleep 2

step "roblox.com and create.roblox.com addresses become Edit-in-Studio links"
"$BRINE" run 'https://www.roblox.com/games/1818/Crossroads' </dev/null >/dev/null 2>&1 || fail "web address rejected"
wait_studio 'placeId:1818'; "$BRINE" kill >/dev/null 2>&1; sleep 2
"$BRINE" run 'https://create.roblox.com/dashboard/creations/experiences/13058/overview' </dev/null >/dev/null 2>&1 || fail "dashboard address rejected"
wait_studio 'placeId:1818+universeId:13058'; "$BRINE" kill >/dev/null 2>&1; sleep 2

step "place file with spaces, #, % and unicode, sent as a file:// link"
cp "$(dirname "$0")/fixtures/Integration.rbxlx" "$work/My Place #1 100% café.rbxlx"
enc=$(python3 -c 'import sys,urllib.parse; print("file://"+urllib.parse.quote(sys.argv[1]))' "$work/My Place #1 100% café.rbxlx")
gio open "$enc" || fail "gio open failed"
wait_studio 'localPlaceFile'
"$BRINE" kill >/dev/null 2>&1; sleep 2

step "hostile and unknown links are refused and start nothing"
for bad in 'roblox-studio:1+x -task EditFile -localPlaceFile Z:\etc\passwd' 'roblox-studio:1+x" -localPlaceFile "Z:\tmp\a.rbxl' 'javascript:alert(1)' 'https://evil.example/games/1818'; do
    "$BRINE" run "$bad" </dev/null >/dev/null 2>&1 && fail "accepted: $bad"
done
sleep 2; [ -z "$(studio)" ] || fail "a refused link started Studio"

step "sign-in callback returns at once and its code is never logged"
t0=$(date +%s%N); "$BRINE" run 'roblox-studio-auth:/?code=LINKTESTCODE&state=LINKTESTSTATE' </dev/null >/dev/null 2>&1
[ "$(ms $t0)" -lt 3000 ] || fail "auth hand-off took $(ms $t0) ms"
sleep 8
grep -rqs 'LINKTESTCODE\|LINKTESTSTATE' "${XDG_STATE_HOME:-$HOME/.local/state}/brine/" && fail "sign-in code found in Brine's logs"
echo PASS
