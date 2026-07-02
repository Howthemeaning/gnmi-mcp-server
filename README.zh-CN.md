# gnmi-mcp-server

[![CI](https://github.com/Howthemeaning/gnmi-mcp-server/actions/workflows/ci.yml/badge.svg)](https://github.com/Howthemeaning/gnmi-mcp-server/actions/workflows/ci.yml)
[![Go 1.25+](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev)
[![License MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

[English](README.md) | [简体中文](README.zh-CN.md)

> 基于 [gnmic](https://github.com/openconfig/gnmic) Go API 构建的 gNMI 网络设备管理 MCP Server。

`gnmi-mcp-server` 通过 Model Context Protocol (MCP) 将 gNMI 设备操作暴露给 AI 助手（Claude Code、Codex、OpenCode、Reasonix）。它是一个无运行时依赖的单一静态链接二进制文件。

> **已验证设备：** 在 **Arista EOS**（gNMI；OpenConfig + eos_native）和 **Nokia SR OS**（state tree）上验证通过。其他 gNMI / OpenConfig 平台理论上可用但**未经测试** — 路径和行为因厂商而异；使用 `gnmi_capabilities` 确认设备支持的范围。

## 安装

**一行安装（macOS / Linux）** — 下载对应平台的二进制到 `/usr/local/bin`：

```bash
curl -fsSL https://raw.githubusercontent.com/Howthemeaning/gnmi-mcp-server/main/install.sh | sh
```

无需 sudo 的安装方式：`INSTALL_DIR="$HOME/.local/bin" curl -fsSL .../install.sh | sh`（需要该目录在 `PATH` 中）。

**手动安装：** 从 [Releases](https://github.com/Howthemeaning/gnmi-mcp-server/releases) 页面下载对应平台的压缩包，解压后放入 `PATH`：

| 平台 | 文件名 |
|------|--------|
| Linux x86_64 | `gnmi-mcp-server_linux_amd64.tar.gz` |
| Linux ARM64 | `gnmi-mcp-server_linux_arm64.tar.gz` |
| macOS Intel | `gnmi-mcp-server_darwin_amd64.tar.gz` |
| macOS Apple Silicon | `gnmi-mcp-server_darwin_arm64.tar.gz` |

> **macOS Gatekeeper：** 从浏览器下载的二进制文件可能被隔离，首次运行时可能被阻止（"无法验证开发者"）。使用前清除一次：`xattr -d com.apple.quarantine ./gnmi-mcp-server`。（`curl … | sh` 安装方式不受影响 — `curl` 不会设置隔离标记。）

**从源码安装**（需要 Go 1.25+）：

```bash
go install github.com/Howthemeaning/gnmi-mcp-server@latest
```

**Docker：**

```bash
docker build -t gnmi-mcp-server .
docker run -i --rm \
  -v $HOME/.gnmi-mcp-server/config.yaml:/root/.gnmi-mcp-server/config.yaml:ro \
  gnmi-mcp-server
```

> MCP 使用 stdin/stdout 作为传输通道，因此需要 `-i`（交互模式）。将配置文件挂载到默认路径，或通过 `--config <path>` 指定。

### 面向 AI Agent 的一键安装

Agent 可以一次性完成安装、配置和注册（macOS / Linux）：

```bash
# 1. 安装二进制到 PATH
curl -fsSL https://raw.githubusercontent.com/Howthemeaning/gnmi-mcp-server/main/install.sh | sh
# 2. 生成配置模板，然后编辑设备和凭证
mkdir -p ~/.gnmi-mcp-server
curl -fsSL https://raw.githubusercontent.com/Howthemeaning/gnmi-mcp-server/main/gnmi-mcp.example.yaml \
  -o ~/.gnmi-mcp-server/config.yaml
# 3. 自动注册到所有检测到的客户端（Claude Code / Codex / OpenCode）
gnmi-mcp-server install
```

`gnmi-mcp-server install` 会自动检测每个客户端并完成注册 — Claude Code 和 Codex 通过 `mcp add` CLI，OpenCode 通过合并 `opencode.json`（幂等操作；找不到的客户端会跳过）。可传 `--config /abs/path.yaml` 将特定配置路径写入注册命令。

## 更新

```bash
gnmi-mcp-server update
```

下载最新 release，验证 SHA256 校验和，然后原子替换当前二进制 — 之后重启 MCP 客户端即可。（重新运行 `curl … install.sh | sh` 或 `go install …@latest` 也能更新。）服务启动后每天最多检查一次新版本并通过日志提示；不会静默更新。

> 如果二进制文件安装在 root 拥有的目录（如 `/usr/local/bin`），请运行 `sudo gnmi-mcp-server update`。安装到 `~/.local/bin` 可避免 sudo。

## 配置

复制模板并根据你的设备进行编辑：

```bash
cp gnmi-mcp.example.yaml ~/.gnmi-mcp-server/config.yaml
chmod 600 ~/.gnmi-mcp-server/config.yaml   # 如果凭证直接写在配置中的话
```

`gnmi-mcp.example.yaml`（在本仓库中）记录了所有配置项。最小配置示例见下。密码可以是明文或 `${ENV_VAR}` / `${ENV_VAR:-default}` 引用 — 服务启动时自动插值，凭证永远不会出现在 MCP 工具参数中。

```yaml
devices:
  core-switch:
    address: 192.168.1.1:57400
    username: admin
    password: ${GNMI_PASS_CORE_SWITCH}   # 环境变量插值
    skip-verify: true                    # 跳过 TLS 证书验证

  leaf-01:
    address: 10.0.0.1:57400
    username: operator
    password: ${GNMI_PASS_LEAF_01}
    timeout: 30s                         # 默认: 30s

# 可选的全局设置
read-only: false          # 设为 true 禁用 gnmi_set
# allow-arbitrary: false  # 设为 true 允许临时指定 host:port 目标
# yang-dir: ~/yang        # 启用 gnmi_path 工具
# data-dir: ~/.gnmi-mcp-server/data
# log-level: info         # debug / info / warn / error
```

### 配置文件的存放位置

服务按以下顺序查找配置文件（第一个匹配的生效）：

1. `--config <path>` 命令行参数
2. `GNMI_CONFIG=<path>` 环境变量
3. 当前工作目录下的 `./gnmi-mcp.yaml`
4. `~/.gnmi-mcp-server/config.yaml`（home 目录默认值）

两种推荐方案：

- **零参数（最简单）：** 将配置放在 `~/.gnmi-mcp-server/config.yaml`，然后直接 `gnmi-mcp-server` — 无需 `--config`。
- **显式路径（可移植）：** 将配置放在任意位置，启动时传 `--config /abs/path/gnmi-mcp.yaml`。

> MCP 客户端（opencode / Claude Code）启动 server 时，工作目录是不可预测的 — **不要**依赖 `./gnmi-mcp.yaml`。使用 home 默认位置或绝对 `--config` 路径。

手动启动测试：

```bash
gnmi-mcp-server --config /path/to/gnmi-mcp.yaml
# 或者，配置在 ~/.gnmi-mcp-server/config.yaml 时：
gnmi-mcp-server
```

### TLS

要使用双向 TLS，在设备下添加 `tls-ca`、`tls-cert` 和 `tls-key`。在顶层设置 `tls-dir` 限定证书路径到安全目录：

```yaml
tls-dir: /etc/gnmi-certs
devices:
  secure-router:
    address: 10.1.0.1:57400
    username: admin
    password: ${ROUTER_PASS}
    tls-ca: ca.pem        # 相对于 tls-dir
    tls-cert: client.pem
    tls-key: client.key
```

### 故障排查："environment variable referenced in config is not set"

这个错误通常意味着你是从 macOS 桌面应用启动 server 的（Reasonix 桌面版、Claude Code.app 等）。macOS GUI 应用的环境继承自 `launchd`，**不会**加载 `~/.zshrc` 或 `~/.bash_profile`——它们看不到 shell 导出的变量。

三种修复方式，按简单程度排序：

**方案 A —— 在 MCP 客户端配置中设置 `env`：**

- **Reasonix** (`reasonix.toml`)：
  ```toml
  [[plugins]]
  name    = "gnmi"
  command = "gnmi-mcp-server"
  env     = { GNMI_TELEMETRY_USER = "..." , GNMI_TELEMETRY_PASS = "..." }
  ```
- **Claude Code** (`claude.json`)：
  ```json
  { "mcpServers": { "gnmi": { "command": "gnmi-mcp-server", "env": { "GNMI_TELEMETRY_USER": "...", "GNMI_TELEMETRY_PASS": "..." } } } }
  ```
- **Codex** (`config.toml`)：
  ```toml
  [mcp_servers.gnmi]
  command = "gnmi-mcp-server"
  env = { GNMI_TELEMETRY_USER = "...", GNMI_TELEMETRY_PASS = "..." }
  ```

**方案 B —— 将 `${VAR}` 替换为明文值写入 `config.yaml`。**

**方案 C —— 在 `~/.reasonix/.env` 中添加 `export` 行**（仅限 Reasonix CLI 模式）。

## MCP 客户端配置

这是一个标准的 stdio MCP server，任何 MCP 客户端都可以使用。二进制在 `PATH` 上、配置在 `~/.gnmi-mcp-server/config.yaml` 时，启动命令就是 `gnmi-mcp-server` — 无需参数。仅当配置放在别处时才需要 `--config /abs/path.yaml`。

**自动注册所有客户端：** `gnmi-mcp-server install` 会注册到每个检测到的客户端。也可以手动配置：

### Claude Code

```bash
claude mcp add gnmi -s user -- gnmi-mcp-server
```

或者编辑 `~/.claude.json`：

```json
{ "mcpServers": { "gnmi": { "command": "gnmi-mcp-server" } } }
```

### Codex

```bash
codex mcp add gnmi -- gnmi-mcp-server
```

或者编辑 `~/.codex/config.toml`：

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

Reasonix 支持标准 MCP 客户端配置，两种方式任选其一：

**`.mcp.json`（推荐，与 Claude Code 共用）：** 在项目根目录创建 `.mcp.json`：

```json
{ "mcpServers": { "gnmi": { "command": "gnmi-mcp-server" } } }
```

**`reasonix.toml`：**

```toml
[[plugins]]
name    = "gnmi"
command = "gnmi-mcp-server"
# args = ["--config", "/abs/path/gnmi-mcp.yaml"]   # 可选
```

启动后工具会以 `mcp__gnmi__gnmi_get` 等形式出现。也可用 `/mcp` 命令查看连接状态。

## 工具列表

| 工具 | gNMI RPC | 说明 |
|------|----------|------|
| `gnmi_targets` | — | 列出服务器上配置的所有设备（名称 + 地址）。 |
| `gnmi_capabilities` | Capabilities | 查询设备支持的 gNMI 版本、YANG 模型和编码格式。结果缓存 5 分钟。 |
| `gnmi_get` | Get | 读取设备配置或状态数据。支持路径、类型（CONFIG/STATE/OPERATIONAL/ALL）、编码格式，以及通过 `max_bytes` 截断输出。 |
| `gnmi_set` | Set | 两阶段配置写入：首次调用返回 dry-run 预览和 `confirm_token`；再次调用带 `confirm=<token>` 才真正执行。Token 10 分钟过期。`read-only: true` 时禁用。 |
| `gnmi_subscribe` | Subscribe | ONCE 模式同步返回遥测快照；STREAM 模式启动后台会话（通过 `gnmi_session_*` 管理）。不支持 POLL — 使用 STREAM 或 ONCE。 |
| `gnmi_session_list` | — | 列出所有订阅会话及当前状态。 |
| `gnmi_session_stop` | — | 停止一个运行中的订阅会话。 |
| `gnmi_session_tail` | — | 读取某个会话最新的遥测输出行。 |
| `gnmi_path` | — | 列出 `yang-dir` 下可用的 YANG 模块（仅在配置了 `yang-dir` 时注册）。 |

所有工具接受 `target`（配置中的设备名称），或当 `allow-arbitrary` 启用时接受原始 `address`（host:port）。

### Prompts

引导式模板（MCP prompts），展开为可直接执行的请求；每个需要 `target` 参数：

- `device_health` — 运行时间 + 接口错误 + BGP 状态汇总
- `interface_errors` — 有非零 errors/discards 的接口
- `bgp_status` — BGP 邻居及会话状态

## 示例交互

```
> core-switch 支持哪些 YANG 模型？
AI 调用 gnmi_capabilities(target="core-switch")

> 读取 core-switch 的系统运行时间
AI 调用 gnmi_get(target="core-switch", path="/state/system/uptime")

> 将 core-switch 主机名改为 dc1-core
AI 调用 gnmi_set(target="core-switch", operations=[{"op":"update","path":"/system/name","value":"\"dc1-core\""}])
→ 返回 dry-run 预览 + confirm_token
AI 再次调用 gnmi_set(target="core-switch", operations=[...], confirm="<token>")
→ 已应用

> 每 10 秒采集一次 core-switch 的接口计数器
AI 调用 gnmi_subscribe(target="core-switch", path="/interfaces/interface/state/counters",
                        mode="STREAM", stream_mode="SAMPLE", sample_interval="10s",
                        session_name="counters-stream")

> 查看最新遥测数据
AI 调用 gnmi_session_tail(session_name="counters-stream")
```

## 许可证

MIT License — 自由使用、修改和分发。

---

基于 [gnmic](https://github.com/openconfig/gnmic) · [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
