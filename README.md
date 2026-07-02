# gnmi-mcp-server

[![CI](https://github.com/Howthemeaning/gnmi-mcp-server/actions/workflows/ci.yml/badge.svg)](https://github.com/Howthemeaning/gnmi-mcp-server/actions/workflows/ci.yml)
[![Go 1.25+](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev)
[![License MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

[English](README.md) | [简体中文](README.zh-CN.md)

> Go MCP server for gNMI network device management — built on [gnmic](https://github.com/openconfig/gnmic)'s Go API.

`gnmi-mcp-server` exposes gNMI device operations to AI assistants (Claude Code, Codex, OpenCode) via the Model Context Protocol. It is a single statically linked binary with no runtime dependencies.

> **Tested devices:** validated against **Arista EOS** (gNMI; OpenConfig + eos_native) and **Nokia SR OS** (state tree). Other gNMI / OpenConfig platforms should work but are **untested** — paths and behavior vary by vendor; use `gnmi_capabilities` to confirm what a device supports.

## Install

**One-line install (macOS / Linux)** — downloads the right binary into `/usr/local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/Howthemeaning/gnmi-mcp-server/main/install.sh | sh
```

Install without sudo: `INSTALL_DIR="$HOME/.local/bin" curl -fsSL .../install.sh | sh` (make sure that directory is on your `PATH`).

**Manual:** grab your platform's archive from the [Releases](https://github.com/Howthemeaning/gnmi-mcp-server/releases) page, extract it, and put `gnmi-mcp-server` on your `PATH`:

| Platform | Asset |
|----------|-------|
| Linux x86_64 | `gnmi-mcp-server_linux_amd64.tar.gz` |
| Linux ARM64 | `gnmi-mcp-server_linux_arm64.tar.gz` |
| macOS Intel | `gnmi-mcp-server_darwin_amd64.tar.gz` |
| macOS Apple Silicon | `gnmi-mcp-server_darwin_arm64.tar.gz` |

> **macOS Gatekeeper:** a binary downloaded through the browser from the Releases page is quarantined and may be blocked on first run ("cannot verify the developer"). Clear it once before use: `xattr -d com.apple.quarantine ./gnmi-mcp-server`. (The `curl … | sh` installer is not affected — `curl` does not set the quarantine flag.)

**From source** (needs Go 1.25+):

```bash
go install github.com/Howthemeaning/gnmi-mcp-server@latest
```

**Docker:**

```bash
docker build -t gnmi-mcp-server .
docker run -i --rm \
  -v $HOME/.gnmi-mcp-server/config.yaml:/root/.gnmi-mcp-server/config.yaml:ro \
  gnmi-mcp-server
```

> MCP uses stdin/stdout for stdio transport, so `-i` (interactive) is required. Mount your config at the default path or pass `--config <path>`.

### For AI agents (one-shot)

An agent can install, seed a config, and register the server in one block (macOS / Linux):

```bash
# 1. install the binary onto your PATH
curl -fsSL https://raw.githubusercontent.com/Howthemeaning/gnmi-mcp-server/main/install.sh | sh
# 2. seed a config, then edit devices + credentials
mkdir -p ~/.gnmi-mcp-server
curl -fsSL https://raw.githubusercontent.com/Howthemeaning/gnmi-mcp-server/main/gnmi-mcp.example.yaml \
  -o ~/.gnmi-mcp-server/config.yaml
# 3. auto-register with every detected client (Claude Code / Codex / OpenCode)
gnmi-mcp-server install
```

`gnmi-mcp-server install` detects each client and wires it up — Claude Code and Codex via their `mcp add` CLIs, OpenCode by merging `opencode.json` (idempotent; skips clients it can't find). Pass `--config /abs/path.yaml` to bake a specific config path into the registration.

## Updating

```bash
gnmi-mcp-server update
```

Downloads the latest release, verifies its SHA256 checksum, and atomically replaces the binary in place — then restart your MCP client. (Re-running `curl … install.sh | sh` or `go install …@latest` also updates.) On startup the server checks at most once per day and logs a note to its log file when a newer version is available; it never updates silently.

> If the binary lives in a root-owned directory (e.g. `/usr/local/bin`), run `sudo gnmi-mcp-server update`. Installing to `~/.local/bin` avoids sudo.

## Configuration

Copy the template and edit it for your devices:

```bash
cp gnmi-mcp.example.yaml ~/.gnmi-mcp-server/config.yaml
chmod 600 ~/.gnmi-mcp-server/config.yaml   # if you keep credentials inline
```

`gnmi-mcp.example.yaml` (in this repo) documents every field. A minimal config looks like this. Passwords may be literals or `${ENV_VAR}` / `${ENV_VAR:-default}` references — the server interpolates them at startup so credentials never appear in the MCP tool arguments.

```yaml
devices:
  core-switch:
    address: 192.168.1.1:57400
    username: admin
    password: ${GNMI_PASS_CORE_SWITCH}   # env-var interpolation
    skip-verify: true                    # skip TLS cert verification

  leaf-01:
    address: 10.0.0.1:57400
    username: operator
    password: ${GNMI_PASS_LEAF_01}
    timeout: 30s                         # default: 30s

# Optional global settings
read-only: false          # set true to disable gnmi_set
# allow-arbitrary: false  # set true to allow ad-hoc host:port targets
# yang-dir: ~/yang        # enables gnmi_path tool
# data-dir: ~/.gnmi-mcp-server/data
# log-level: info         # debug / info / warn / error
```

### Where to put the config

The server looks for its config in this order (first match wins):

1. `--config <path>` flag
2. `GNMI_CONFIG=<path>` environment variable
3. `./gnmi-mcp.yaml` in the current working directory
4. `~/.gnmi-mcp-server/config.yaml` (home default)

Two recommended setups:

- **Zero-arg (simplest):** put your config at `~/.gnmi-mcp-server/config.yaml`, then launch with just `gnmi-mcp-server` — no `--config` needed.
- **Explicit (portable):** put the config anywhere and pass `--config /abs/path/gnmi-mcp.yaml`.

> When the server is launched by an MCP client (opencode / Claude Code), the working directory is unpredictable — do **not** rely on `./gnmi-mcp.yaml`. Use the home default or an absolute `--config` path.

Start the server manually to test:

```bash
gnmi-mcp-server --config /path/to/gnmi-mcp.yaml
# or, with ~/.gnmi-mcp-server/config.yaml in place:
gnmi-mcp-server
```

### TLS

To use mutual TLS, add `tls-ca`, `tls-cert`, and `tls-key` under a device. Set `tls-dir` at the top level to restrict certificate paths to a safe directory:

```yaml
tls-dir: /etc/gnmi-certs
devices:
  secure-router:
    address: 10.1.0.1:57400
    username: admin
    password: ${ROUTER_PASS}
    tls-ca: ca.pem        # relative to tls-dir
    tls-cert: client.pem
    tls-key: client.key
```

### Troubleshooting: "environment variable referenced in config is not set"

This error usually means you launched the server from a macOS GUI app (Reasonix desktop, Claude Code.app, etc.). macOS GUI apps inherit their environment from `launchd`, **not** from `~/.zshrc` or `~/.bash_profile` — they never see shell-exported variables.

Three fixes, in order of simplicity:

**Option A — set `env` in your MCP client config:**

- **Reasonix** (`reasonix.toml`):
  ```toml
  [[plugins]]
  name    = "gnmi"
  command = "gnmi-mcp-server"
  env     = { GNMI_TELEMETRY_USER = "...", GNMI_TELEMETRY_PASS = "..." }
  ```
- **Claude Code** (`claude.json`):
  ```json
  { "mcpServers": { "gnmi": { "command": "gnmi-mcp-server", "env": { "GNMI_TELEMETRY_USER": "...", "GNMI_TELEMETRY_PASS": "..." } } } }
  ```
- **Codex** (`config.toml`):
  ```toml
  [mcp_servers.gnmi]
  command = "gnmi-mcp-server"
  env = { GNMI_TELEMETRY_USER = "...", GNMI_TELEMETRY_PASS = "..." }
  ```

**Option B — replace `${VAR}` with plaintext in `config.yaml`.**

**Option C — add `export` lines to `~/.reasonix/.env`** (Reasonix only, for CLI mode).

## MCP Client Setup

It's a standard stdio MCP server, so any MCP client works. With the binary on your `PATH` and a config at `~/.gnmi-mcp-server/config.yaml`, the launch command is just `gnmi-mcp-server` — no args. Add `--config /abs/path.yaml` only if the config lives elsewhere.

**Auto-wire everything:** `gnmi-mcp-server install` registers the server with every detected client. Or configure one manually:

### Claude Code

```bash
claude mcp add gnmi -s user -- gnmi-mcp-server
```

Or edit `~/.claude.json`:

```json
{ "mcpServers": { "gnmi": { "command": "gnmi-mcp-server" } } }
```

### Codex

```bash
codex mcp add gnmi -- gnmi-mcp-server
```

Or edit `~/.codex/config.toml`:

```toml
[mcp_servers.gnmi]
command = "gnmi-mcp-server"
# args = ["--config", "/abs/path/gnmi-mcp.yaml"]
```

### OpenCode (`opencode.json`)

```json
{ "mcp": { "gnmi": { "type": "local", "command": ["gnmi-mcp-server"], "enabled": true } } }
```

### Reasonix

Reasonix supports standard MCP client configuration. Choose one of two methods:

**`.mcp.json` (recommended, shared with Claude Code):** create `.mcp.json` at the project root:

```json
{ "mcpServers": { "gnmi": { "command": "gnmi-mcp-server" } } }
```

**`reasonix.toml`:**

```toml
[[plugins]]
name    = "gnmi"
command = "gnmi-mcp-server"
# args = ["--config", "/abs/path/gnmi-mcp.yaml"]   # optional
```

## Tools

| Tool | gNMI RPC | Description |
|------|----------|-------------|
| `gnmi_targets` | — | List the devices configured on this server (target names + addresses). |
| `gnmi_capabilities` | Capabilities | Query supported gNMI version, YANG models, and encodings. Results are cached for 5 minutes. |
| `gnmi_get` | Get | Read configuration or state data from a device. Supports path, type (CONFIG/STATE/OPERATIONAL/ALL), encoding, and output truncation via `max_bytes`. |
| `gnmi_set` | Set | Two-phase config write: first call returns a dry-run preview and a `confirm_token`; call again with `confirm=<token>` to apply. Token expires in 10 minutes. Disabled when `read-only: true`. |
| `gnmi_subscribe` | Subscribe | ONCE returns a telemetry snapshot synchronously; STREAM starts a background session (manage via `gnmi_session_*`). POLL is not supported — use STREAM or ONCE. |
| `gnmi_session_list` | — | List all subscribe sessions and their current status. |
| `gnmi_session_stop` | — | Stop a running subscribe session. |
| `gnmi_session_tail` | — | Read the most recent telemetry lines from a session's output. |
| `gnmi_path` | — | List available YANG modules under the configured `yang-dir` (only registered when `yang-dir` is set). |

All tools accept `target` (a device name from the config) or, when `allow-arbitrary` is enabled, a raw `address` (host:port).

### Prompts

Guided templates (MCP prompts) that expand into ready-to-run requests; each takes a `target`:

- `device_health` — uptime + interface errors + BGP state summary
- `interface_errors` — interfaces with non-zero errors/discards
- `bgp_status` — BGP neighbors and session state

## Example Interaction

```
> What YANG models does core-switch support?
AI calls gnmi_capabilities(target="core-switch")

> Read the system uptime from core-switch
AI calls gnmi_get(target="core-switch", path="/state/system/uptime")

> Rename core-switch hostname to dc1-core
AI calls gnmi_set(target="core-switch", operations=[{"op":"update","path":"/system/name","value":"\"dc1-core\""}])
→ returns dry-run preview + confirm_token
AI calls gnmi_set(target="core-switch", operations=[...], confirm="<token>")
→ applied

> Stream interface counters from core-switch, sampled every 10s
AI calls gnmi_subscribe(target="core-switch", path="/interfaces/interface/state/counters",
                        mode="STREAM", stream_mode="SAMPLE", sample_interval="10s",
                        session_name="counters-stream")

> Show latest telemetry
AI calls gnmi_session_tail(session_name="counters-stream")
```

## License

MIT License — use, modify, and distribute freely.

---

Built with [gnmic](https://github.com/openconfig/gnmic) · [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
