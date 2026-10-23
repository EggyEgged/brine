# Contributing

```sh
make check          # gofmt, go vet, go test -race — what CI runs
make build          # build/brine
make integration    # real Studio: MCP round trip (needs a signed-in Studio, MCP enabled)
make restart-test   # one MCP session survives `brine kill` and a Studio restart
make multi-test     # several MCP clients at once
```

- Keep changes small and tested. Behaviour that touches Wine or Studio should get a unit test where it
  can (see `internal/mcp/bridge_test.go` for the fake-helper pattern) and a note in
  `docs/mcp-under-wine.md` if it teaches something about how Studio behaves.
- Brine must stay a fresh codebase: do not copy code from Vinegar or other GPL projects.
- No Roblox names or logos in the program name, icon or desktop entry.
- Never log tokens, cookies or sign-in codes; route new log output through `internal/logx`.
- Update `CHANGELOG.md`.
