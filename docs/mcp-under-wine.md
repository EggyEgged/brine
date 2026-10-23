# Studio's built-in MCP server under Wine

Findings from running Roblox Studio 0.744.0.7441077 (`version-7aaa6838d36c4448`)
in a plain Wine prefix (Wine 11.20, the "kombucha" build) on Arch-based
CachyOS, kernel 7.2, on 2026-10-23. All of it was observed directly. Where a
claim is inferred rather than observed, it says so.

**Result: the built-in server works under Wine, unmodified.** The deprecated
standalone server is not needed.

## 1. What `mcp.bat` runs

Studio writes `%LOCALAPPDATA%\Roblox\mcp.bat` when "Enable Studio as MCP
server" is switched on. In Brine's prefix that is
`~/.local/share/brine/prefix/drive_c/users/$USER/AppData/Local/Roblox/mcp.bat`:

```bat
@echo off
if exist "Z:\home\USER\.local\share\brine\versions\version-7aaa6838d36c4448\StudioMCP.exe" ( "Z:\home\USER\.local\share\brine\versions\version-7aaa6838d36c4448\StudioMCP.exe" %* ) else (for /f "tokens=2*" %%A in ('reg query HKEY_CURRENT_USER\Software\Roblox\RobloxStudio /v ContentFolder') do (
"%%B/..\StudioMCP.exe" %*
))
```

* The real helper is **`StudioMCP.exe` in the Studio build directory**. The
  fallback branch finds the build through `HKCU\Software\Roblox\RobloxStudio\ContentFolder`.
* It takes no required arguments (`--help`: `-v/--verbose`, `-s/--stdio` which
  is a no-op, `--print-flags`, `-V`). Studio passes none.
* The file is written with LF line endings, despite being a `.bat`.
* `StudioMCP.exe` is a 64-bit Rust console program (`rmcp` 3.4, `axum`,
  `tokio-tungstenite`), crate name `studio_mcp_proxy`, version 1.0.0 (f500a21).

Brine runs `StudioMCP.exe` directly from the active build, so a stale
`mcp.bat` (for example after `brine rollback`) does not matter, and `cmd.exe`
is never involved.

## 2. How the helper talks to Studio: localhost WebSocket

There are no named pipes, shared memory or files in the data path. Everything
goes over TCP on the loopback interface:

| Endpoint | Who connects | Purpose |
|---|---|---|
| `ws://127.0.0.1:13469/studio` | Studio (client) | Studio registers, pushes its tool list, answers tool calls, pings every ~5 s |
| `ws://127.0.0.1:13469/proxy` | every `StudioMCP.exe` (client) | each MCP client's helper joins the shared host |

* The **first** `StudioMCP.exe` to start binds `127.0.0.1:13469` and becomes
  the "WS host" (`ws_server::host: Bound port, starting WS host server`). Later
  helpers, one per MCP client, find the port taken and connect to `/proxy`
  as clients. The host routes tool calls between proxies and Studio
  instances. If the host exits, the remaining helpers reconnect and one of them
  takes over.
* **Studio is a WebSocket client.** It never listens. Its log shows
  `ws: N url: http://localhost:13469/studio` attempts. With the setting on,
  it retries every ~3 s whenever the connection is down, and connects about 4 s
  after launch when a helper is already listening.
* After connecting, Studio sends an MCP `initialize` result, a `ping`, and
  its tool list. With a place open that was 27 tools: `execute_luau`,
  `search_game_tree`, `script_read`, `multi_edit`, `get_console_output`,
  `screen_capture` and others. The helper caches the list in
  `%LOCALAPPDATA%\Roblox\StudioMCP\tools-cache.json`, so later `tools/list`
  calls answer instantly even before Studio connects.
* `ss -ltnp` while connected shows the listening socket held by the helper
  (`main`) **and** by `wineserver`. Wine keeps a reference to every socket in
  the server, which is expected.

### Authentication

* Token file: `%LOCALAPPDATA%\Roblox\StudioMCP\auth.token` (64 bytes). The
  helper's host creates it if missing (`token mode: ReadOrCreate`). Proxies send
  it as an `Authorization` header, and so does Studio.
* Current mode is **`Observe`**: mismatched tokens are logged and allowed.
  Proof: a helper started from a *different* prefix and wineserver, with
  its own token, still served Studio:
  `[AUTH] request: method=GET, path=/studio, … token_match=false, mode=Observe`
  then `Allowed unauthenticated WebSocket request` then `Studio connected`.
* The binaries contain the fast flags `FFlagStudioMCPProxyTokenEnforce` and
  `FFlagStudioMCPProxyTokenRotate`. Once Roblox enforces the token, only a
  helper that reads Studio's own `auth.token` (that is, one running in the
  same prefix) will be accepted. **This is the real reason to run the helper
  in Studio's prefix.** Sharing the wineserver is a consequence of that,
  not a transport requirement.

### The on/off setting

"Enable Studio as MCP server" is stored per user in
`%LOCALAPPDATA%\Roblox\AssistantSettings\<userId>.json`:

```json
{ "mcp-server": { "enabled": true }, ... }
```

Observed behaviour: toggling it on while **no helper was listening** made
Studio try `/studio` exactly once, fail with `ConnectFail`, and then stop. The
file still read `"enabled": false` after Studio exited. The toggle apparently
does not stick if the first connection fails. (The exact rule is inferred
from one observation.) Writing `"enabled": true` while Studio was closed
(`brine mcp enable`), then launching Studio, made it connect on startup and
retry every 3 s after any disconnect. Brine's advice is therefore: enable
it while a client (and so a helper) is running, or use `brine mcp enable`.

## 3. Running the helper from a host terminal

Same `WINEPREFIX`, same Wine binary, same `WINESERVER` as Studio. This is
what `brine mcp` does, using the same environment builder as `brine run`:

```
WINEPREFIX=~/.local/share/brine/prefix WINEDEBUG=-all \
WINESERVER=~/.local/share/brine/runtimes/current/bin/wineserver \
~/.local/share/brine/runtimes/current/bin/wine \
~/.local/share/brine/versions/version-7aaa6838d36c4448/StudioMCP.exe
```

Round trip (newline-delimited JSON-RPC on stdin/stdout):

```
>> {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18",...}}
<< {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{"listChanged":true}},"serverInfo":{"name":"RobloxStudio","version":"1.0.0"},"instructions":"Studio MCP Proxy - bridges MCP clients with Roblox Studio"}}
>> {"jsonrpc":"2.0","method":"notifications/initialized"}
>> {"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_roblox_studios","arguments":{}}}
<< {"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"{\"studios\":[{\"id\":\"85d12890-…\",\"name\":\"Place1\"}]}"}],"isError":false}}
```

Then `execute_luau` with
`{"studio_id": "<id>", "datamodel_type": "Edit", "code": "print(\"hi\") return \"brine-ok\""}`
returned `brine-ok`, and Studio logged `[FLog::CreatorOutput] hi`, which is
the Output window. `datamodel_type` is required (`Edit`, `Client` or `Server`).

Behaviour without Studio:

* `list_roblox_studios` answers immediately with `{"studios":[]}`.
* `tools/list` blocks ("No tools available, waiting for ws_server to push
  tools...") until Studio connects, unless a tool cache exists. It answered
  after ~2–4 s in testing. Brine answers it with an empty list after
  `[mcp] list_timeout` seconds. The helper announces
  `notifications/tools/list_changed` when Studio connects, and clients
  re-fetch.
* Closing stdin makes the helper exit (0.02–4 s observed). If it was the only
  process in the prefix, the wineserver and Wine's system processes exit on
  their own a few seconds later. No orphans were left after any test.

## 4. stdout pollution

| Source | Where it goes | Observed |
|---|---|---|
| Wine/kombucha banners (`kombucha: ntsync is unavailable…`) | stderr | yes |
| `WINEDEBUG` channels (tried `warn+all`: 167 lines) | stderr | 0 lines on stdout |
| `fixme:` from `cmd.exe` | stderr | yes |
| `cmd.exe` banner | n/a with `/c` and `@echo off` | not seen |
| Line endings from `StudioMCP.exe` | `\n` only | no `\r\n` seen, even via `cmd.exe /c mcp.bat` |
| Helper logs (`tracing`) | stderr | yes |

So with this Wine build stdout is already clean. Other Wine, Proton and UMU
builds are less predictable: Proton and pressure-vessel wrappers have printed to
stdout historically, and a console `cmd.exe` without `/c` prints a banner with
`\r\n`. `brine mcp` therefore passes helper stdout through a strict filter
(`internal/mcp/filter.go`):

1. Strip `\r`, the BOM, and surrounding whitespace.
2. Drop any non-JSON prefix before the first `{`.
3. Forward only lines that parse as a JSON object with `"jsonrpc":"2.0"`.

Everything else goes to `~/.local/state/brine/mcp.log`. `WINEDEBUG` defaults
to `-all`.

## 5. Why the Vinegar Flatpak setup failed here

* The existing Claude Code entry ran `flatpak run --command=env
  org.vinegarhq.Vinegar … StudioMCP.exe`. On this machine that fails at once
  with `app/org.vinegarhq.Vinegar/x86_64/master not installed`. The Flatpak
  had been removed, leaving only its data. Claude Code reported that as
  `CONNECTION_CLOSED`.
* Vinegar's Studio logs from when it *was* installed show hundreds of
  `ConnectFail` attempts on `localhost:13469/studio`. Studio was retrying, but
  no helper ever came up in a place Studio could reach, plus two
  `Connection reset by peer` upgrades. Vinegar also moves Local AppData out of
  the prefix (`…/vinegar/appdata`), so a helper started any other way would
  not see Studio's `auth.token`. That breaks as soon as the token is enforced.
* Flatpak's sandbox normally shares the network namespace, so loopback TCP
  is not what breaks. What breaks is starting the helper from outside: a
  second sandbox instance, with a matching runtime, the same prefix and the
  same `auth.token`. That is fiddly to set up from outside. Brine avoids all
  of it by running everything directly on the host.

## 6. Design consequences for `brine mcp`

* Run `StudioMCP.exe` from the active build, in Brine's prefix, using the
  runtime and environment `brine run` uses. It attaches to Studio's
  wineserver if Studio is running, or starts one if not.
* **Start the helper immediately**, even when Studio is closed. Studio can only
  connect to a helper that is already listening.
* Filter stdout strictly. Send helper stderr to the log.
* When Studio is not running, answer `tools/call` at once with a clear error
  rather than waiting.
* If the helper dies (for example `brine kill` stops the wineserver), restart it and
  replay the client's `initialize`, so a long-lived client session survives a
  Studio restart.
* On stdin EOF: close the helper's stdin, wait up to 3 s, then SIGTERM and
  SIGKILL its process group. The wineserver is never killed from `brine mcp`,
  because Studio may still be using it.

## 7. Verified end to end

Run on the same machine, with Studio 0.744.0.7441077:

* `claude -p` (a real Claude Code session) listed two connected Studios, chose the one with a
  place open, ran `execute_luau`, and `print` appeared in Studio's log as `[FLog::CreatorOutput]`.
  The tool result carries the return value only; print output is read with `get_console_output`.
* One `brine mcp` session survived `brine kill` and a Studio relaunch: the helper was restarted,
  the client got `notifications/tools/list_changed`, and calls worked again without a new `initialize`.
  (`scripts/restart-test.py`.)
* **Pitfall found by that test:** the helper starts a wineserver when none is running, and that
  wineserver keeps inheriting the helper's stdout/stderr for as long as Studio uses it. Waiting
  for EOF on those pipes (Go's `StdoutPipe`) therefore hangs `brine mcp` after stdin closes.
  Brine hands the helper plain pipe files and tracks the process exit separately.
* With `[mcp] autostart_studio = true`, a tool call with Studio closed launched Studio and was
  answered 6 s later.
* `roblox-studio-auth:` links reach Studio's own handler (it parses the URI and validates `state`).
  A real sign-in needs credentials and was not exercised.
* Local playtesting under Wine works: `start_stop_play` switched Studio to Play mode,
  `get_studio_state` then listed the `Client` and `Server` datamodels, `execute_luau` with
  `datamodel_type: "Server"` ran on the server, and the `Edit` datamodel correctly refused calls while playing.
* Several MCP clients at once work (`scripts/multi-client-test.py`): each `brine mcp` starts its own
  helper; the first hosts port 13469 and the others join it. Concurrent calls from two clients did not
  cross. When the host's client exits, another helper takes the port and Studio reconnects within
  about 5 s (Studio retries every 3 s).
