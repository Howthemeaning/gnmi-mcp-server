package server

import (
	"context"
	"testing"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/gnmi"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func listToolNames(t *testing.T, cfg *config.AppConfig) map[string]bool {
	t.Helper()
	mgr := session.NewManager(t.TempDir())
	srv := BuildServer(cfg, gnmi.New(), mgr)

	clientT, serverT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := srv.Connect(ctx, serverT, nil)
	require.NoError(t, err)
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	require.NoError(t, err)
	defer cs.Close()

	res, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, tl := range res.Tools {
		names[tl.Name] = true
	}
	return names
}

func baseCfg() *config.AppConfig {
	return &config.AppConfig{
		Devices: map[string]config.DeviceConfig{
			"sw1": {Name: "sw1", Address: "10.0.0.1:57400", Username: "u", Password: "p", Timeout: "30s"},
		},
	}
}

func TestRegistersAlwaysOnTools(t *testing.T) {
	names := listToolNames(t, baseCfg())
	for _, want := range []string{
		"gnmi_targets", "gnmi_capabilities", "gnmi_get", "gnmi_set", "gnmi_subscribe",
		"gnmi_session_list", "gnmi_session_stop", "gnmi_session_tail",
	} {
		require.True(t, names[want], "expected tool %q registered", want)
	}
	require.False(t, names["gnmi_path"], "gnmi_path should be off without yang-dir")
}

func TestReadOnlyHidesSet(t *testing.T) {
	cfg := baseCfg()
	cfg.ReadOnly = true
	names := listToolNames(t, cfg)
	require.False(t, names["gnmi_set"], "gnmi_set must be hidden in read-only mode")
	require.True(t, names["gnmi_get"])
}

func TestYangDirEnablesPath(t *testing.T) {
	cfg := baseCfg()
	cfg.YangDir = t.TempDir()
	names := listToolNames(t, cfg)
	require.True(t, names["gnmi_path"], "gnmi_path should be on when yang-dir set")
}

func TestBuildInstructionsListsDevicesAndGuidance(t *testing.T) {
	s := buildInstructions(baseCfg())
	require.Contains(t, s, "sw1")               // live device name
	require.Contains(t, s, "10.0.0.1:57400")    // its address
	require.Contains(t, s, "gnmi_targets")      // points the LLM at the list tool
	require.Contains(t, s, "gnmi_capabilities") // workflow guidance
}

func TestPromptsRegistered(t *testing.T) {
	mgr := session.NewManager(t.TempDir())
	srv := BuildServer(baseCfg(), gnmi.New(), mgr)

	clientT, serverT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := srv.Connect(ctx, serverT, nil)
	require.NoError(t, err)
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	require.NoError(t, err)
	defer cs.Close()

	lp, err := cs.ListPrompts(ctx, nil)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, p := range lp.Prompts {
		names[p.Name] = true
	}
	require.True(t, names["device_health"], "device_health prompt should be registered")
	require.True(t, names["bgp_status"], "bgp_status prompt should be registered")

	gp, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "bgp_status", Arguments: map[string]string{"target": "sw1"}})
	require.NoError(t, err)
	require.NotEmpty(t, gp.Messages)
	tc, ok := gp.Messages[0].Content.(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, tc.Text, "sw1") // target argument interpolated
}

func TestPromptsRejectEmptyTarget(t *testing.T) {
	mgr := session.NewManager(t.TempDir())
	srv := BuildServer(baseCfg(), gnmi.New(), mgr)
	clientT, serverT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := srv.Connect(ctx, serverT, nil)
	require.NoError(t, err)
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	require.NoError(t, err)
	defer cs.Close()

	_, err = cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "bgp_status"}) // no target arg
	require.Error(t, err)
}
