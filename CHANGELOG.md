# Changelog

All notable changes to gnmi-mcp-server are documented in this file.

## [1.0.2]

### Added
- Chinese README (`README.zh-CN.md`).
- CI workflow (`ci.yml`) — `go vet` + `go test -race` on push/PR.
- Dockerfile (multi-stage, statically linked, ~8 MB image).
- Docker push to `ghcr.io` in release workflow.
- Badges (CI, Go version, license) in README.

### Changed
- Enhanced MCP server instructions with gNMI concepts (path, data type, encoding,
  subscribe/stream modes, set operations).
- `mcp.Implementation` version now uses the linker-injected `main.version` instead
  of a hardcoded `"2.0.0"`.

### Fixed
- Token store now runs a background goroutine to remove expired confirm tokens,
  preventing unbounded memory growth.
- Dockerfile `FROM scratch` now copies CA certificates for TLS support.

### Improved
- Better `.gitignore` coverage (IDE dirs, Reasonix local files, `.mcp.json`).
- Added Reasonix MCP client setup instructions.
- Documentation comments for `SetRotation` call-before-Create invariant, capabilities
  cache key assumptions, and ad-hoc address credential behavior.
- Enhanced config error message for missing `${ENV}` variables with macOS GUI / MCP
  client troubleshooting guidance.
- Server instructions now cover `json_ietf` namespace prefixes (`openconfig-interfaces:…`),
  Nokia port-id bracket notation, and `max_notifications` usage for broad paths.
- Added Troubleshooting section in both READMEs for env var issues with GUI-launched MCP clients.
- Added `max_notifications` parameter to `gnmi_get` to limit notifications before
  byte-level truncation, with test coverage.

## [1.0.0] — 2026-06-26

### Added
- Initial public release.
- 9 gNMI MCP tools: `gnmi_targets`, `gnmi_capabilities`, `gnmi_get`, `gnmi_set`,
  `gnmi_subscribe`, `gnmi_session_list`, `gnmi_session_stop`, `gnmi_session_tail`,
  `gnmi_path`.
- Two-phase commit for `gnmi_set` (dry-run → confirm token, 10-minute expiry).
- Subscribe session manager with on-disk recovery, log rotation, and status tracking.
- Self-update via `gnmi-mcp-server update` with SHA256 checksum verification.
- Multi-client auto-install (`gnmi-mcp-server install`) for Claude Code, Codex,
  OpenCode.
- YAML config with `${ENV}` / `${ENV:-default}` interpolation, TLS path confinement,
  and per-device credentials.
- MCP prompts: `device_health`, `interface_errors`, `bgp_status`.
- Server instructions with gNMI concept reference for LLM clients.
- Cross-platform one-line installer (`install.sh`) for macOS and Linux (amd64/arm64).
- goreleaser-based release pipeline with CI tests on tag push.

### Supported
- Arista EOS (gNMI 6030 — OpenConfig + eos_native paths).
- Nokia SR OS (gNMI 57400 — state tree paths).
