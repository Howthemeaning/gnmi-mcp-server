// Package install wires gnmi-mcp-server into local MCP clients (Claude Code,
// Codex, OpenCode). Claude Code and Codex are configured via their native
// `mcp add` CLIs; OpenCode is configured by merging its JSON config.
package install

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Result reports what happened for one client.
type Result struct {
	Client string
	Action string // registered | already | skipped | manual | error
	Detail string
}

const serverName = "gnmi"

// ServerCommand returns the command to register: the bare name if it resolves on
// PATH, otherwise this binary's absolute path (so registration works regardless).
func ServerCommand() string {
	if p, err := exec.LookPath("gnmi-mcp-server"); err == nil && p != "" {
		return "gnmi-mcp-server"
	}
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "gnmi-mcp-server"
}

func claudeArgs(command string, extra []string) []string {
	return append([]string{"mcp", "add", serverName, "-s", "user", "--", command}, extra...)
}

func codexArgs(command string, extra []string) []string {
	return append([]string{"mcp", "add", serverName, "--", command}, extra...)
}

// mergeOpenCodeConfig adds an mcp.gnmi entry to an OpenCode config if absent.
// Returns the new bytes, whether anything changed, and any parse error. It is a
// no-op (changed=false) when an entry already exists, and errors on non-JSON
// input (so the caller can fall back to printing manual instructions).
func mergeOpenCodeConfig(raw []byte, command string, extra []string) (out []byte, changed bool, err error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, false, err
	}
	if root == nil {
		root = map[string]any{}
	}
	if existing, ok := root["mcp"]; ok {
		if _, isObj := existing.(map[string]any); !isObj {
			return nil, false, fmt.Errorf("opencode.json has a non-object %q value; not editing", "mcp")
		}
	}
	mcp, _ := root["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}
	if _, exists := mcp[serverName]; exists {
		return raw, false, nil
	}
	cmd := append([]string{command}, extra...)
	mcp[serverName] = map[string]any{
		"type":    "local",
		"command": cmd,
		"enabled": true,
	}
	root["mcp"] = mcp
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return append(b, '\n'), true, nil
}

// Wire registers the server with every detected client. configPath, if set, is
// baked into the registered command as `--config <configPath>`.
func Wire(configPath string) []Result {
	command := ServerCommand()
	var extra []string
	if configPath != "" {
		extra = []string{"--config", configPath}
	}
	return []Result{
		wireViaCLI("Claude Code", "claude", claudeArgs(command, extra)),
		wireViaCLI("Codex", "codex", codexArgs(command, extra)),
		wireOpenCode(command, extra),
	}
}

func wireViaCLI(client, bin string, args []string) Result {
	if _, err := exec.LookPath(bin); err != nil {
		return Result{client, "skipped", bin + " CLI not found on PATH"}
	}
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		s := strings.ToLower(string(out))
		if strings.Contains(s, "already") {
			return Result{client, "already", "gnmi already registered"}
		}
		return Result{client, "error", strings.TrimSpace(string(out))}
	}
	return Result{client, "registered", bin + " mcp add gnmi"}
}

func wireOpenCode(command string, extra []string) Result {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{"OpenCode", "skipped", "cannot resolve home dir"}
	}
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{"OpenCode", "skipped", "no opencode.json at " + path}
		}
		return Result{"OpenCode", "error", "cannot read " + path + ": " + err.Error()}
	}
	merged, changed, err := mergeOpenCodeConfig(raw, command, extra)
	if err != nil {
		return Result{"OpenCode", "manual", err.Error() + "; add the mcp.gnmi entry by hand"}
	}
	if !changed {
		return Result{"OpenCode", "already", "gnmi already in opencode.json"}
	}
	// Atomic write: temp file in the same dir, then rename, so a crash can't
	// leave a half-written opencode.json.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, merged, 0o644); err != nil {
		return Result{"OpenCode", "error", err.Error()}
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return Result{"OpenCode", "error", err.Error()}
	}
	return Result{"OpenCode", "registered", path}
}
