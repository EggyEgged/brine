# Changelog

All notable changes to Brine are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.1] - 2026-10-25

### Added
- Fedora package: `brine-0.1.1-1.x86_64.rpm` and a source RPM (`packaging/rpm/brine.spec`), built and
  install-tested in clean Fedora containers by CI and attached to every release, with a `SHA256SUMS` file.
- `brine run` accepts `roblox.com/games/<id>` and `create.roblox.com/dashboard/creations/experiences/<id>`
  addresses and opens them as "Edit in Studio" links.
- `brine doctor` verifies the registered link handler points at a real binary.
- `scripts/link-test.sh` (`make link-test`): live test of links, files and hostile input.

### Changed
- Links and files launched from a browser, file manager or `xdg-open` now hand off to a background process
  and return immediately instead of blocking until Studio exits.
- `file://` links are decoded properly (spaces, `#`, `%`, unicode, `localhost`); links to other hosts are refused.

### Security
- `roblox-studio:` and `roblox-studio-auth:` links are validated: whitespace, quotes, backslashes and control
  characters are refused, so a crafted link cannot add arguments to Studio's Wine command line.

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

[Unreleased]: https://github.com/EggyEgged/brine/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/EggyEgged/brine/releases/tag/v0.1.1
[0.1.0]: https://github.com/EggyEgged/brine/releases/tag/v0.1.0
