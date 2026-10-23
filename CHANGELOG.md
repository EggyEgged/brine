# Changelog

All notable changes to Brine are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-10-23

First release.

### Added
- `brine setup`, `install`, `update`, `rollback`, `run`, `kill`: install and run Roblox Studio under
  Wine with no Flatpak or other sandbox. System Wine, a managed Studio-tuned Wine build, a custom
  Wine/Proton build, or `umu-run` can be used.
- DXVK and Vulkan renderers with NVIDIA, AMD, Intel and hybrid-GPU defaults; WebView2 for Studio's
  sign-in window; `roblox-studio:` / `roblox-studio-auth:` handlers and `.rbxl` / `.rbxlx` file types.
- `brine mcp`: Studio's built-in MCP server for Linux MCP clients. Runs `StudioMCP.exe` in Studio's
  prefix and wineserver; stdout carries only JSON-RPC; clear errors when Studio is closed; the helper
  restarts after `brine kill`; optional `autostart_studio`.
- `brine mcp connect|disconnect claude-code|claude-desktop`, `mcp enable`, `mcp test`, `--print-config`.
- `brine doctor` with a full MCP round trip; `brine uninstall [--purge]`; shell completions
  (bash, zsh, fish); man page.
- Downloads are verified (Studio package MD5s, SHA-256 for Wine, DXVK and WebView2) and time out
  instead of hanging; install/update/rollback are serialised with a lock; credentials, cookies and auth
  codes are redacted from logs; logs rotate at 5 MiB.
- Packaging: release tarball with a user-level installer, AUR `PKGBUILD`, `.deb`.
