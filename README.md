# Brine

Brine installs, updates and runs **Roblox Studio on Linux under Wine**,
directly on your system as your own user. There is no Flatpak, Snap or AppImage sandbox.

Its main feature: Studio's **built-in MCP server works out of the box** with
Linux MCP clients such as Claude Code and Claude Desktop. One command connects
them, and `brine mcp` keeps the connection clean and robust.

* Single static binary (`brine`) plus a `.desktop` file. Nothing needs root.
* Uses your system Wine, a custom Wine/Proton/UMU build, or a Studio-tuned Wine
  build that Brine downloads for you.
* DXVK or Vulkan renderer, with defaults for NVIDIA, AMD, Intel and hybrid laptops.
* Keeps the previous Studio build, so `brine rollback` undoes a bad update.
* Opens `.rbxl`/`.rbxlx` files and handles `roblox-studio:` and
  `roblox-studio-auth:` links, so "Edit in Studio" and browser sign-in work.

Brine is an independent project and is not affiliated with Roblox Corporation.
"Roblox" and "Roblox Studio" are trademarks of Roblox Corporation.

## Install

From a release tarball, as your normal user:

```sh
tar xf brine-*-linux-amd64.tar.gz
cd brine-*-linux-amd64
./install.sh            # installs ~/.local/bin/brine and the desktop entry
```

From source (Go 1.23+):

```sh
make install            # same as above, from build/brine
```

Distribution packages: `packaging/aur/PKGBUILD` (Arch) and `make deb` (Debian/Ubuntu).

## First run

```sh
brine setup
```

The wizard:

1. Picks a Wine runtime. It uses **system Wine** if you have it, otherwise it
   offers a **managed** Wine build (~30 MB, saved to `~/.local/share/brine/runtimes`).
   You can also give a **path** to any Wine or Proton build directory, a `wine` binary, or `umu-run`.
2. Creates the Wine prefix at `~/.local/share/brine/prefix`.
3. Downloads the current Studio build from Roblox's CDN, checking each package's MD5.
4. Installs the WebView2 runtime (for Studio's sign-in window) and DXVK.
5. Installs the desktop entry, MIME types and URI handlers in `~/.local/share`.

Run it again at any time to repair things. Non-interactive:
`brine setup --runtime managed --yes`.

Then:

```sh
brine run               # start Studio and sign in
```

## Connect an MCP client

1. Turn on Studio's MCP server. In Studio: **Assistant → … → Manage MCP Servers →
   "Enable Studio as MCP server"**. Alternatively, close Studio and run:

   ```sh
   brine mcp enable
   ```

   Studio only keeps this setting if its first connection succeeds. If the
   toggle won't stay on, connect a client first (step 2) or use `brine mcp enable`.

2. Register Brine with your client:

   ```sh
   brine mcp connect claude-code       # runs: claude mcp add --transport stdio --scope user Roblox_Studio -- /path/to/brine mcp
   brine mcp connect claude-desktop    # merges into ~/.config/Claude/claude_desktop_config.json (with a backup)
   brine mcp --print-config            # or copy the configuration by hand
   ```

3. Check it:

   ```sh
   claude mcp list                     # Roblox_Studio: … ✔ Connected
   brine mcp test --luau 'print("hi")' # list_roblox_studios + execute_luau; "hi" appears in Studio's Output
   ```

Studio's own "Quick connect" cannot see Linux clients. Use the commands above instead.

### How `brine mcp` works

Studio ships an MCP helper, `StudioMCP.exe`. The first copy that starts listens on
`127.0.0.1:13469`, and Studio connects to it as a WebSocket client.
`brine mcp` runs that helper from your active Studio build inside Brine's
prefix, with the same Wine binary, wineserver and environment as Studio. That
way it shares Studio's auth token. On top of that, it:

* **Keeps stdout clean.** Only valid JSON-RPC reaches the client. `\r` is
  stripped, Wine noise is dropped, and everything else goes to
  `~/.local/state/brine/mcp.log`.
* **Fails fast.** Tool calls get a clear error right away when Studio isn't running.
* **Autostarts Studio** when you set `[mcp] autostart_studio = true`.
* **Survives restarts.** If the helper dies (for example because you ran
  `brine kill`), it is restarted and your client's session keeps working.
* **Cleans up.** It exits when the client closes stdin and leaves no Wine
  processes behind.

The full investigation is in [docs/mcp-under-wine.md](docs/mcp-under-wine.md).

## Everyday commands

| Command | What it does |
|---|---|
| `brine run [FILE\|URI] [-v]` | Start Studio. Opens a place file or a `roblox-studio:` / `roblox-studio-auth:` link. `-v` echoes Wine output |
| `brine update` | Install the newest Studio build. The previous one is kept |
| `brine rollback` | Switch back to the previous build and turn off auto-update |
| `brine kill` | Stop Studio and the prefix's wineserver |
| `brine doctor` | Check everything, including a live MCP round trip, and explain fixes |
| `brine runtime update` | Update the managed Wine build |
| `brine winecfg`, `brine regedit`, `brine wine PROG` | Wine tools inside Brine's prefix |
| `brine desktop install\|remove` | Re-register or remove the desktop entry and handlers |
| `brine config` | Show the effective configuration |

## Configuration

`~/.config/brine/config.toml`. Every key is optional:

```toml
[wine]
runtime = "auto"          # auto | system | managed | /path/to/wine-or-proton | /path/to/umu-run
proton_path = ""          # for umu-run: a Proton directory, or "GE-Proton"
debug = "-all"            # WINEDEBUG

[graphics]
renderer = "auto"         # auto (DXVK if Vulkan works, else wined3d) | dxvk | vulkan | d3d11 | opengl
gpu = "auto"              # auto/discrete: prefer the dedicated GPU on hybrid laptops | integrated
dxvk_version = "3.1.1"

[studio]
webview2 = "144.0.3719.92" # WebView2 runtime version, "latest", or "" to skip
auto_update = true         # update Studio on `brine run`
keep_versions = 2
desktop = ""               # e.g. "1920x1080" runs Studio in a Wine virtual desktop
launcher = ""              # e.g. "gamemoderun"

[mcp]
autostart_studio = false   # start Studio when a client calls a tool and it isn't running
start_timeout = 120        # seconds to wait for an autostarted Studio to connect
list_timeout = 5           # seconds before answering tools/list with "no tools yet"

[env]                      # extra environment for Studio and the MCP helper
# DXVK_HUD = "fps"

[fflags]                   # extra Studio fast flags (ClientAppSettings.json)
# FFlagDebugGraphicsPreferVulkan = true
```

Renderer defaults: `dxvk` runs Studio's Direct3D 11 renderer through DXVK
(the most reliable choice on NVIDIA, AMD and Intel). `vulkan` uses Studio's
native Vulkan renderer through winevulkan. `d3d11` and `opengl` are fallbacks
for machines without Vulkan.

On hybrid laptops, `gpu = "auto"` sets the PRIME offload variables for
NVIDIA's proprietary driver, or `DRI_PRIME=1` for Mesa.

## Files

| Path | Contents |
|---|---|
| `~/.config/brine/config.toml` | configuration |
| `~/.local/share/brine/prefix/` | the Wine prefix. Your Studio sign-in stays here |
| `~/.local/share/brine/versions/` | Studio builds (current and previous) |
| `~/.local/share/brine/runtimes/` | the managed Wine build |
| `~/.local/state/brine/` | `studio.log`, `mcp.log`, `brine.log`. Tokens and auth codes are redacted |
| `~/.cache/brine/` | downloads |

Brine never needs root, never touches system Wine installs, and keeps Roblox
credentials inside its prefix.

## Troubleshooting

Start with `brine doctor`. It prints a fix for every failed check.

* **`claude mcp list` shows Roblox_Studio failing.** Run `brine doctor`. If
  Studio isn't installed or the prefix is missing, `brine mcp` still answers,
  but every tool call explains what's wrong. See `~/.local/state/brine/mcp.log`.
* **`list_roblox_studios` returns `[]` while Studio is open.** Make sure "Enable Studio as MCP
  server" is on: close Studio, then `brine mcp enable`, then `brine run`.
  Studio retries every few seconds, so allow up to ~5 s.
* **`execute_luau` says "Place is not open".** Open a place. Studio's tools work on an open place.
* **Port 13469 belongs to something else.** `brine doctor` names the process. Another launcher's MCP
  helper (or a Windows VM forwarding the port) must be stopped.
* **Sign-in window is blank.** Run `brine setup` to (re)install WebView2. Or set
  `[studio] webview2 = ""` and use "Log in with browser". The browser
  hands the code back through the `roblox-studio-auth:` handler.
* **Black or flickering viewport.** Try `renderer = "vulkan"` or
  `renderer = "d3d11"`. On hybrid laptops try `gpu = "integrated"`.
* **An update broke Studio.** Run `brine rollback`.
* **Start from scratch.** `brine kill`, then delete `~/.local/share/brine/prefix`, then `brine setup`.
  This signs you out.
* **Leftover Vinegar entry.** If `claude mcp list` still shows an old
  `flatpak run … org.vinegarhq.Vinegar` server, remove it with
  `claude mcp remove <name>`.

## Development

```sh
make build          # build/brine
make test           # unit tests (stdout filter, config merge, bridge behaviour, ...)
make integration    # scripts/integration-test.sh: real Studio + MCP round trip
make dist           # release tarball in dist/
make deb            # .deb in dist/
```

The integration test needs a signed-in Studio with MCP enabled. It opens a
temporary place, runs `print(...)` through `execute_luau`, and checks that the
print reached Studio's Output.

## License

MIT. See [LICENSE](LICENSE). Brine is a fresh codebase. It downloads, but does not
include, Wine builds (LGPL-2.1), DXVK (zlib), WebView2 and Studio.
