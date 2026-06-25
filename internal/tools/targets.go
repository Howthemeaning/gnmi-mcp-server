package tools

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type targetEntry struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

func doTargets(cfg *config.AppConfig) string {
	entries := make([]targetEntry, 0, len(cfg.Devices))
	for name, d := range cfg.Devices {
		entries = append(entries, targetEntry{Name: name, Address: d.Address})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	out := map[string]any{
		"count":     len(entries),
		"read_only": cfg.ReadOnly,
		"targets":   entries,
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func RegisterTargets(server *mcp.Server, cfg *config.AppConfig) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "gnmi_targets",
		Description: "List the gNMI devices configured on this server. Pass a returned target name to the other gnmi_* tools.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
		return textResult(doTargets(cfg), false), nil, nil
	})
}
