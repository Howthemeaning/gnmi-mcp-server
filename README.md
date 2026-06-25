# gnmi-mcp-server

> MCP server for gNMI network device management — powered by [gnmic](https://github.com/openconfig/gnmic)

`gnmi-mcp-server` 将 gNMI 协议的网络设备管理能力暴露给 AI 助手（OpenCode、Claude Code），通过 MCP (Model Context Protocol) 实现自然语言驱动的网络运维。

## 功能

| MCP 工具 | 对应 gNMI RPC | 说明 |
|----------|---------------|------|
| `gnmi_capabilities` | Capabilities | 查询设备支持的 gNMI 版本、YANG 模型、编码格式 |
| `gnmi_get` | Get | 读取设备配置/状态数据（支持路径截断） |
| `gnmi_set` | Set | 修改设备配置（两阶段确认：dry-run 预览 → token 确认） |
| `gnmi_subscribe` | Subscribe | 遥测订阅（ONCE 一次性快照 / STREAM 流式 / POLL 轮询） |
| `gnmi_session_list` | — | 列出所有活跃的订阅会话 |
| `gnmi_session_stop` | — | 停止指定订阅会话 |
| `gnmi_session_tail` | — | 读取会话最近的遥测数据 |
| `gnmi_path` | — | 从 YANG 模型浏览可用的 gNMI 路径（可选） |

## 安装

### 前置条件

- Python >= 3.11
- [uv](https://docs.astral.sh/uv/) 或 pip

### 通过 uv 安装（推荐）

```bash
git clone https://github.com/your-org/gnmi-mcp-server.git
cd gnmi-mcp
uv run gnmi-mcp-server --help
```

### 通过 pip 安装

```bash
pip install -e /path/to/gnmi-mcp
```

启动时如未找到 `gnmic` 二进制，会**自动从 GitHub Releases 下载**对应平台的静态二进制（SHA256 校验）。

## 配置

全部通过环境变量配置，无需配置文件。

### 1. 在 Shell Profile 中设置设备账密

```bash
# ~/.bash_profile 或 ~/.zshrc
export GNMI_USER_CORE_SWITCH="admin"
export GNMI_PASS_CORE_SWITCH="s3cret!"

export GNMI_USER_LEAF_01="operator"
export GNMI_PASS_LEAF_01="0p3r@t0r!"
```

### 2. 在 AI 客户端中配置 MCP Server

**OpenCode** (`opencode.json`)：

```json
{
  "mcpServers": {
    "gnmi-mcp-server": {
      "command": "uv",
      "args": ["run", "--directory", "/path/to/gnmi-mcp", "gnmi-mcp-server"],
      "env": {
        "GNMI_DEVICES": "[{\"name\":\"core-switch\",\"address\":\"192.168.1.1:57400\"},{\"name\":\"leaf-01\",\"address\":\"10.0.0.1:57400\"}]"
      }
    }
  }
}
```

**Claude Code** (`claude_desktop_config.json`)：

```json
{
  "mcpServers": {
    "gnmi-mcp-server": {
      "command": "uv",
      "args": ["run", "--directory", "/path/to/gnmi-mcp", "gnmi-mcp-server"],
      "env": {
        "GNMI_DEVICES": "[{\"name\":\"core-switch\",\"address\":\"192.168.1.1:57400\"}]",
        "GNMI_USER_CORE_SWITCH": "admin",
        "GNMI_PASS_CORE_SWITCH": "s3cret!"
      }
    }
  }
}
```

### 完整环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `GNMI_DEVICES` | `[]` | JSON 数组，定义设备名称、地址、TLS 配置 |
| `GNMI_USER_<NAME>` | (必填) | 设备 `<NAME>` 的用户名（大写，`-` → `_`） |
| `GNMI_PASS_<NAME>` | (必填) | 设备 `<NAME>` 的密码 |
| `GNMI_READ_ONLY` | `false` | `true` 时禁用 `gnmi_set` 工具 |
| `GNMI_ALLOW_ARBITRARY` | `false` | `true` 时允许直接指定未预定义的 address |
| `GNMI_TLS_DIR` | — | TLS 证书文件的允许目录 |
| `GNMI_YANG_DIR` | — | YANG 模型目录（设置后启用 `gnmi_path` 工具） |
| `GNMI_BINARY_PATH` | — | 手动指定 gnmic 二进制路径 |
| `GNMI_VERSION` | `latest` | 自动下载的 gnmic 版本 |
| `GNMI_DATA_DIR` | `~/.gnmi-mcp-server/data` | 会话输出和日志存储目录 |
| `GNMI_LOG_LEVEL` | `INFO` | 日志级别 |

`GNMI_DEVICES` 格式：

```json
[
  {
    "name": "core-switch",
    "address": "192.168.1.1:57400",
    "insecure": false,
    "tls_ca": "ca.pem",
    "tls_cert": "client.pem",
    "tls_key": "client-key.pem",
    "timeout": "30s"
  }
]
```

### 安全特性

- 凭证**只通过环境变量**传递，绝不出现于 MCP 工具参数或进程命令行（防 `ps aux` 泄露）
- `gnmi_set` 默认 **dry-run 预览**，需二次确认 token 才真正下发
- 路径参数（TLS 证书、YANG 文件）通过 `os.path.realpath()` 前缀校验防路径遍历
- 会话名白名单字符 `[A-Za-z0-9_-]`，防注入

## 使用示例

在 AI 助手中直接对话：

```
> 查看 core-switch 支持哪些 YANG 模型
AI 调用 gnmi_capabilities(target="core-switch")

> 读取 core-switch 的系统平台信息
AI 调用 gnmi_get(target="core-switch", path="/state/system/platform")

> 把 core-switch 的主机名改成 new-name
AI 调用 gnmi_set(operations=[{"op":"update","path":"/system/name","value":"new-name"}])
→ 返回 dry_run 预览 + confirm_token
AI 调用 gnmi_set(operations=[...], confirm="<token>")
→ 执行成功

> 监控 core-switch 的端口统计，每 10 秒采样
AI 调用 gnmi_subscribe(target="core-switch", path="/state/port/statistics", mode="STREAM", stream_mode="SAMPLE", sample_interval="10s")
→ 返回 session 信息

> 查看最新数据
AI 调用 gnmi_session_tail(session_name="...")
```

## 项目结构

```
gnmi-mcp-server/
├── pyproject.toml
├── src/gnmi_mcp_server/
│   ├── server.py                      # MCP server 主入口
│   ├── lib/
│   │   ├── config.py                  # 环境变量配置加载
│   │   ├── gnmic.py                   # gnmic 子进程管理（GnmicClient）
│   │   ├── installer.py               # gnmic 二进制自动下载 + SHA256 校验
│   │   └── session.py                 # subscribe 会话生命周期（SessionManager）
│   └── tools/
│       ├── _common.py                 # 共享辅助函数
│       ├── capabilities.py            # gnmi_capabilities
│       ├── get.py                     # gnmi_get
│       ├── set.py                     # gnmi_set（dry-run + confirm）
│       ├── subscribe.py               # gnmi_subscribe
│       ├── session_list.py            # gnmi_session_list
│       ├── session_stop.py            # gnmi_session_stop
│       ├── session_tail.py            # gnmi_session_tail
│       └── path.py                    # gnmi_path（可选）
└── tests/
```

## 许可证

MIT License — 随意使用、修改和分发。

---

Built with [gnmic](https://github.com/openconfig/gnmic) · [MCP Python SDK](https://github.com/modelcontextprotocol/python-sdk)
