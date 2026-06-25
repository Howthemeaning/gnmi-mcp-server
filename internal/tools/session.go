package tools

import (
	"context"
	"encoding/json"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type emptyInput struct{}

type sessionNameInput struct {
	SessionName string `json:"session_name"`
}

type tailInput struct {
	SessionName string `json:"session_name"`
	Lines       int    `json:"lines,omitempty" jsonschema:"recent lines, default 20, max 500"`
}

func RegisterSessionTools(server *mcp.Server, mgr *session.Manager) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "gnmi_session_list",
		Description: "List all subscribe sessions and their status.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
		b, _ := json.Marshal(mgr.List())
		return textResult(string(b), false), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "gnmi_session_stop",
		Description: "Stop a running subscribe session.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in sessionNameInput) (*mcp.CallToolResult, any, error) {
		if err := mgr.Stop(in.SessionName); err != nil {
			return textResult(err.Error(), true), nil, nil
		}
		return textResult(`{"stopped":"`+in.SessionName+`"}`, false), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "gnmi_session_tail",
		Description: "Read recent telemetry from a subscribe session.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in tailInput) (*mcp.CallToolResult, any, error) {
		out, err := mgr.Tail(in.SessionName, in.Lines)
		if err != nil {
			return textResult(err.Error(), true), nil, nil
		}
		if out == "" {
			out = "(no telemetry data for this session yet)"
		}
		return textResult(out, false), nil, nil
	})
}
