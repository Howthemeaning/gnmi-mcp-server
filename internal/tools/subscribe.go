package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/gnmi"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SubscribeInput struct {
	ConnParams
	Path              string `json:"path"`
	Mode              string `json:"mode" jsonschema:"ONCE or STREAM"`
	StreamMode        string `json:"stream_mode,omitempty" jsonschema:"SAMPLE/ON_CHANGE/TARGET_DEFINED"`
	SampleInterval    string `json:"sample_interval,omitempty" jsonschema:"e.g. 10s"`
	HeartbeatInterval string `json:"heartbeat_interval,omitempty"`
	SessionName       string `json:"session_name,omitempty"`
	Timeout           int    `json:"timeout,omitempty" jsonschema:"ONCE timeout seconds, default 30"`
}

func parseDur(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d
}

func doSubscribe(ctx context.Context, client gnmi.GnmiClient, cfg *config.AppConfig, mgr *session.Manager, in SubscribeInput) (string, bool) {
	if err := validatePath(in.Path); err != nil {
		return err.Error(), true
	}
	dev, err := in.apply(cfg)
	if err != nil {
		return err.Error(), true
	}
	mode := strings.ToUpper(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "ONCE"
	}
	if mode == "POLL" {
		return "POLL mode is not supported; use STREAM for continuous telemetry or ONCE for a one-time snapshot", true
	}
	if mode != "ONCE" && mode != "STREAM" {
		return fmt.Sprintf("unknown mode %q; use ONCE or STREAM", in.Mode), true
	}
	p := gnmi.SubParams{
		Path: in.Path, Mode: mode, StreamMode: in.StreamMode,
		SampleInterval: parseDur(in.SampleInterval), HeartbeatInterval: parseDur(in.HeartbeatInterval),
	}

	if mode == "ONCE" {
		to := in.Timeout
		if to <= 0 {
			to = 30
		}
		octx, cancel := context.WithTimeout(ctx, time.Duration(to)*time.Second)
		defer cancel()
		resp, err := client.SubscribeOnce(octx, dev, p)
		if err != nil {
			return err.Error(), true
		}
		return string(resp), false
	}

	// STREAM: create background session.
	info, err := mgr.Create(ctx, client, dev, p, in.SessionName)
	if err != nil {
		return err.Error(), true
	}
	b, _ := json.Marshal(info)
	return string(b), false
}

func RegisterSubscribe(server *mcp.Server, client gnmi.GnmiClient, cfg *config.AppConfig, mgr *session.Manager) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "gnmi_subscribe",
		Description: "Subscribe to telemetry. ONCE returns a snapshot; STREAM starts a background session managed via gnmi_session_* (POLL is not supported).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SubscribeInput) (*mcp.CallToolResult, any, error) {
		text, isErr := doSubscribe(ctx, client, cfg, mgr, in)
		return textResult(text, isErr), nil, nil
	})
}
