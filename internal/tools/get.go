package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/gnmi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetInput struct {
	ConnParams
	Path             string `json:"path" jsonschema:"gNMI path, must start with /"`
	Type             string `json:"type,omitempty" jsonschema:"CONFIG/STATE/OPERATIONAL/ALL, default ALL"`
	Prefix           string `json:"prefix,omitempty"`
	Encoding         string `json:"encoding,omitempty" jsonschema:"json or json_ietf, default json_ietf"`
	MaxBytes         int    `json:"max_bytes,omitempty" jsonschema:"truncation threshold in bytes, default 131072"`
	MaxNotifications int    `json:"max_notifications,omitempty" jsonschema:"keep only the first N notifications; use with broad paths like /interfaces/interface/state to avoid huge output"`
}

func doGet(ctx context.Context, client gnmi.GnmiClient, cfg *config.AppConfig, in GetInput) (string, bool) {
	if err := validatePath(in.Path); err != nil {
		return err.Error(), true
	}
	dev, err := in.apply(cfg)
	if err != nil {
		return err.Error(), true
	}
	max := in.MaxBytes
	if max <= 0 {
		max = 131072
	}
	var resp json.RawMessage
	// Get is idempotent: retry once, but only on transient/network errors.
	for attempt := 0; attempt < 2; attempt++ {
		resp, err = client.Get(ctx, dev, gnmi.GetParams{
			Path: in.Path, Prefix: in.Prefix, Type: in.Type, Encoding: in.Encoding,
		})
		if err == nil {
			break
		}
		m := strings.ToLower(err.Error())
		if !strings.Contains(m, "timed out") && !strings.Contains(m, "refused") && !strings.Contains(m, "unavailable") {
			break // non-transient (auth, bad path, not found): don't retry
		}
	}
	if err != nil {
		return err.Error(), true
	}
	return string(truncateGetJSON(resp, max, in.MaxNotifications)), false
}

// truncateGetJSON shrinks an oversized gNMI GetResponse to fit max bytes and
// ALWAYS returns valid JSON. It keeps whole notifications while they fit; for
// the first notification that doesn't, it trims that notification's update[]
// (the common single-notification case). Metadata records what was omitted.
func truncateGetJSON(raw json.RawMessage, max, maxNotifs int) json.RawMessage {
	// Parse early so we can apply max_notifications even when the response
	// already fits within max_bytes.
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		if len(raw) <= max { return raw }
		return truncNotice(len(raw), max, "could not be structurally truncated")
	}
	var notifs []json.RawMessage
	if json.Unmarshal(obj["notification"], &notifs) != nil || len(notifs) == 0 {
		if len(raw) <= max { return raw }
		return truncNotice(len(raw), max, "could not be structurally truncated")
	}

	// Apply max_notifications cap before byte-level truncation.
	originalNotifs := len(notifs)
	if maxNotifs > 0 && len(notifs) > maxNotifs {
		notifs = notifs[:maxNotifs]
	}

	// Re-serialize and check if the result fits within max_bytes.
	capped := raw
	if len(notifs) != originalNotifs {
		capped = rewrapNotifications(obj, notifs)
	}
	if len(capped) <= max {
		return capped
	}
	kept := make([]json.RawMessage, 0, len(notifs))
	size := len(`{"notification":[],"truncated":true,...}`) // rough envelope reserve
	omittedNotifs, omittedUpdates := 0, 0
	for i, n := range notifs {
		if size+len(n) <= max {
			kept = append(kept, n)
			size += len(n) + 1
			continue
		}
		// Doesn't fit whole: trim this notification's updates to the remainder.
		tn, dropped := truncateNotificationUpdates(n, max-size)
		if dropped > 0 || len(kept) == 0 {
			kept = append(kept, tn)
			omittedUpdates += dropped
		} else {
			omittedNotifs++
		}
		omittedNotifs += len(notifs) - i - 1 // everything after this is dropped
		break
	}

	wrapper := map[string]any{
		"notification":        kept,
		"truncated":           true,
		"original_bytes":      len(raw),
		"max_bytes":           max,
		"kept_notifications":  len(kept),
		"total_notifications": originalNotifs,
		"note":                "Truncated; some data omitted. Narrow the path, raise max_bytes, or lower max_notifications for the full result.",
	}
	if omittedUpdates > 0 {
		wrapper["omitted_updates"] = omittedUpdates
	}
	if omittedNotifs > 0 {
		wrapper["omitted_notifications"] = omittedNotifs
	}
	b, _ := json.Marshal(wrapper)
	return b
}

// rewrapNotifications produces a minimal valid GetResponse JSON object containing
// only the given notifications. Used when max_notifications culled entries without
// byte-level truncation.
func rewrapNotifications(obj map[string]json.RawMessage, notifs []json.RawMessage) json.RawMessage {
	out := make(map[string]json.RawMessage, len(obj))
	for k, v := range obj {
		if k == "notification" {
			b, _ := json.Marshal(notifs)
			out[k] = b
		} else {
			out[k] = v
		}
	}
	b, _ := json.Marshal(out)
	return b
}

// truncateNotificationUpdates keeps whole updates of a notification within budget.
// Returns the (possibly shortened) notification and how many updates were dropped.
func truncateNotificationUpdates(n json.RawMessage, budget int) (json.RawMessage, int) {
	var m map[string]json.RawMessage
	if json.Unmarshal(n, &m) != nil {
		return n, 0
	}
	var updates []json.RawMessage
	if json.Unmarshal(m["update"], &updates) != nil || len(updates) == 0 {
		return n, 0
	}
	kept := make([]json.RawMessage, 0, len(updates))
	size := len(n) - len(m["update"]) // notification envelope minus the updates blob
	for _, u := range updates {
		if size+len(u) > budget && len(kept) > 0 {
			break
		}
		kept = append(kept, u)
		size += len(u) + 1
	}
	if len(kept) == len(updates) {
		return n, 0
	}
	m["update"], _ = json.Marshal(kept)
	out, _ := json.Marshal(m)
	return out, len(updates) - len(kept)
}

func truncNotice(orig, max int, why string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"truncated":      true,
		"original_bytes": orig,
		"max_bytes":      max,
		"note":           "Response exceeds max_bytes and " + why + ". Narrow the gNMI path or raise max_bytes.",
	})
	return b
}

func RegisterGet(server *mcp.Server, client gnmi.GnmiClient, cfg *config.AppConfig) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "gnmi_get",
		Description: "Read configuration/state data from a gNMI device.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GetInput) (*mcp.CallToolResult, any, error) {
		text, isErr := doGet(ctx, client, cfg, in)
		return textResult(text, isErr), nil, nil
	})
}

func textResult(text string, isErr bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: isErr,
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}
