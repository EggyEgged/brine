# Security policy

## Supported versions

Only the latest release receives fixes.

## Reporting a vulnerability

Please report security problems privately to the maintainers (see the project page for the contact
address) instead of opening a public issue. Include the Brine version (`brine version`), what you did and
what you saw. We aim to acknowledge reports within a week.

## What Brine does with your data

- Brine has no telemetry and makes no network requests except to download Studio and its components
  (Roblox's CDN, `github.com` for the Wine build and DXVK, Microsoft's WebView2 update service) and to
  check for a newer Studio build.
- Your Roblox sign-in stays inside the Wine prefix (`~/.local/share/brine/prefix`), exactly as on
  Windows. Brine never reads, copies or transmits it. Logs redact cookies, tokens and sign-in codes.
- Downloads are verified against published checksums where one exists. Studio's own package manifest
  is served by the same CDN as the packages, so it protects against corruption, not a compromised CDN.
- Brine runs entirely as your user and refuses to run as root. Anything Studio does, it does with your
  user's permissions: there is no sandbox. Only open places you trust.
- `StudioMCP.exe` accepts connections on `127.0.0.1:13469` only. Any local process of any user can
  connect to that port, as with the Windows version.
