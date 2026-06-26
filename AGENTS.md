# gnmi-mcp-server

> Go MCP server for gNMI network device management — built on gnmic's Go API.

## Project

- **Stack:** Go (module `github.com/Howthemeaning/gnmi-mcp-server`, go 1.25.0), stdio MCP server via `github.com/modelcontextprotocol/go-sdk`.
- **Entry point:** `main.go` — CLI with subcommands `install` | `update` | default server mode.
- **Config:** YAML (env-var interpolation `${VAR}` / `${VAR:-default}`). Resolved in order: `--config`, `GNMI_CONFIG`, `./gnmi-mcp.yaml`, `~/.gnmi-mcp-server/config.yaml`.
- **Release:** tag `v*` → GitHub Actions + goreleaser → Linux/macOS amd64/arm64 archives.

## Commands

```bash
go build -o gnmi-mcp-server .        # build binary
go test ./...                         # run all tests
gnmi-mcp-server [--config <path>]     # start server (stdio MCP)
gnmi-mcp-server --version             # print version
gnmi-mcp-server install [--config]    # register with detected MCP clients
gnmi-mcp-server update                # self-update from GitHub releases
```

## Architecture

| Package | Role |
|---------|------|
| `main.go` | CLI dispatch, config path resolution, subcommands |
| `internal/config/` | YAML parsing + validation, `${ENV}` interpolation, TLS path confinement |
| `internal/gnmi/` | `GnmiClient` interface + gnmic-based impl (Capabilities, Get, Set, SubscribeOnce, SubscribeStream) |
| `internal/server/` | MCP server wiring: builds instructions, registers tools, sets up logging + signals |
| `internal/tools/` | Individual MCP tool handlers — capabilities, get, set, subscribe, session mgmt, path, prompts |
| `internal/session/` | Subscribe session lifecycle: create/stop/tail, on-disk persistence, log rotation |
| `internal/install/` | Auto-registration with Claude Code (`~/.claude.json`), Codex (`~/.codex/config.toml`), OpenCode (`opencode.json`) |
| `internal/selfupdate/` | Download latest GitHub release, SHA256 verify, atomic binary replacement |

## Conventions

- **Tests:** `testing` stdlib + `github.com/stretchr/testify/require`. Helper functions use `t.Helper()`. Temp files via `t.TempDir()`.
- **Errors:** custom error types (e.g. `config.ConfigError`, `config.errf` helper). Tools return `textResult(err.Error(), true)` for user-facing errors.
- **YAML:** tags in kebab-case (`yaml:"skip-verify"`). JSON tags in snake_case (`json:"skip_verify"`). MCP tool inputs annotated with `jsonschema:` tags.
- **Interfaces:** `GnmiClient` abstracts RPC calls so tools can be tested with mocks.
- **Logging:** `log/slog` TextHandler to rotating files via `gopkg.in/natefinch/lumberjack.v2`. No stdout logging in server mode (MCP transport uses stdout).
- **Secrets:** never log credentials. Passwords use `${ENV}` interpolation so they never appear in MCP tool arguments.
- **Naming:** Go CamelCase. Device config fields match gnmic conventions. MCP tool names are `gnmi_`-prefixed snake_case.

## Notes

