package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func userPrompt(text string) *mcp.GetPromptResult {
	return &mcp.GetPromptResult{
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: text}},
		},
	}
}

// RegisterPrompts adds guided, network-ops query templates. Each takes a
// `target` argument (a device name from gnmi_targets) and expands into a
// concrete instruction that drives the gnmi_* tools.
func RegisterPrompts(server *mcp.Server) {
	targetArg := []*mcp.PromptArgument{
		{Name: "target", Description: "device name (see gnmi_targets)", Required: true},
	}

	server.AddPrompt(&mcp.Prompt{
		Name:        "device_health",
		Title:       "Device health check",
		Description: "Summarize a device's health: uptime, interface errors, BGP state.",
		Arguments:   targetArg,
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		t := req.Params.Arguments["target"]
		if t == "" {
			return nil, fmt.Errorf("target is required (call gnmi_targets to list devices)")
		}
		return userPrompt(fmt.Sprintf("Summarize the health of gNMI device %q. Steps: 1) read its uptime (OpenConfig /system/state or Nokia /state/system/up-time); 2) read interface counters and flag any non-zero in/out errors or discards; 3) read BGP neighbor session-state and list any not ESTABLISHED. If unsure which paths the device supports, call gnmi_capabilities(target=%q) first.", t, t)), nil
	})

	server.AddPrompt(&mcp.Prompt{
		Name:        "interface_errors",
		Title:       "Interface errors",
		Description: "Find interfaces with errors or discards on a device.",
		Arguments:   targetArg,
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		t := req.Params.Arguments["target"]
		if t == "" {
			return nil, fmt.Errorf("target is required (call gnmi_targets to list devices)")
		}
		return userPrompt(fmt.Sprintf("On gNMI device %q, read interface counters (OpenConfig /interfaces/interface/state/counters, or Nokia /state/port/ethernet/statistics) and report only interfaces with non-zero in-errors, out-errors, in-discards or out-discards.", t)), nil
	})

	server.AddPrompt(&mcp.Prompt{
		Name:        "bgp_status",
		Title:       "BGP neighbor status",
		Description: "List BGP neighbors and their session state on a device.",
		Arguments:   targetArg,
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		t := req.Params.Arguments["target"]
		if t == "" {
			return nil, fmt.Errorf("target is required (call gnmi_targets to list devices)")
		}
		return userPrompt(fmt.Sprintf("On gNMI device %q, read BGP neighbor state (OpenConfig /network-instances/network-instance/protocols/protocol/bgp/neighbors/neighbor/state, or Nokia /state/service/vprn/bgp/neighbor) and list each neighbor with its session-state, highlighting any not ESTABLISHED.", t)), nil
	})
}
