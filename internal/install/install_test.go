package install

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeArgs(t *testing.T) {
	require.Equal(t,
		[]string{"mcp", "add", "gnmi", "-s", "user", "--", "/usr/local/bin/gnmi-mcp-server"},
		claudeArgs("/usr/local/bin/gnmi-mcp-server", nil))
	require.Equal(t,
		[]string{"mcp", "add", "gnmi", "-s", "user", "--", "gnmi-mcp-server", "--config", "/c.yaml"},
		claudeArgs("gnmi-mcp-server", []string{"--config", "/c.yaml"}))
}

func TestCodexArgs(t *testing.T) {
	require.Equal(t,
		[]string{"mcp", "add", "gnmi", "--", "gnmi-mcp-server"},
		codexArgs("gnmi-mcp-server", nil))
}

func TestMergeOpenCodeAddsEntry(t *testing.T) {
	raw := []byte(`{"mcp":{"other":{"type":"local","command":["x"]}}}`)
	out, changed, err := mergeOpenCodeConfig(raw, "gnmi-mcp-server", nil)
	require.NoError(t, err)
	require.True(t, changed)
	var root map[string]any
	require.NoError(t, json.Unmarshal(out, &root))
	mcp := root["mcp"].(map[string]any)
	require.Contains(t, mcp, "gnmi")  // added
	require.Contains(t, mcp, "other") // preserved
	cmd := mcp["gnmi"].(map[string]any)["command"].([]any)
	require.Equal(t, "gnmi-mcp-server", cmd[0])
}

func TestMergeOpenCodeIdempotent(t *testing.T) {
	raw := []byte(`{"mcp":{"gnmi":{"type":"local","command":["gnmi-mcp-server"]}}}`)
	_, changed, err := mergeOpenCodeConfig(raw, "gnmi-mcp-server", nil)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestMergeOpenCodeNoMcpKey(t *testing.T) {
	out, changed, err := mergeOpenCodeConfig([]byte(`{"$schema":"x"}`), "gnmi-mcp-server", nil)
	require.NoError(t, err)
	require.True(t, changed)
	var root map[string]any
	require.NoError(t, json.Unmarshal(out, &root))
	require.Contains(t, root["mcp"].(map[string]any), "gnmi")
}

func TestMergeOpenCodeInvalidJSON(t *testing.T) {
	_, _, err := mergeOpenCodeConfig([]byte(`{not json`), "x", nil)
	require.Error(t, err)
}

func TestMergeOpenCodeNonObjectMcp(t *testing.T) {
	// A non-object "mcp" value must error (don't silently overwrite user data).
	_, _, err := mergeOpenCodeConfig([]byte(`{"mcp":[1,2]}`), "x", nil)
	require.Error(t, err)
}
