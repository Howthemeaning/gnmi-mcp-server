package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/gnmi"
	"github.com/stretchr/testify/require"
)

type mockClient struct {
	getResp     json.RawMessage
	getErr      error
	capResp     json.RawMessage
	lastDev     config.DeviceConfig
	lastGet     gnmi.GetParams
	setResp     json.RawMessage
	setOps      []gnmi.SetOp
	subOnceResp json.RawMessage
	streamCh    chan gnmi.Update
}

func (m *mockClient) Capabilities(_ context.Context, d config.DeviceConfig) (json.RawMessage, error) {
	m.lastDev = d
	return m.capResp, nil
}
func (m *mockClient) Get(_ context.Context, d config.DeviceConfig, p gnmi.GetParams) (json.RawMessage, error) {
	m.lastDev, m.lastGet = d, p
	return m.getResp, m.getErr
}
func (m *mockClient) Set(_ context.Context, d config.DeviceConfig, ops []gnmi.SetOp) (json.RawMessage, error) {
	m.lastDev, m.setOps = d, ops
	return m.setResp, nil
}
func (m *mockClient) SubscribeOnce(_ context.Context, d config.DeviceConfig, _ gnmi.SubParams) (json.RawMessage, error) {
	m.lastDev = d
	if m.subOnceResp == nil {
		return json.RawMessage(`[]`), nil
	}
	return m.subOnceResp, nil
}
func (m *mockClient) SubscribeStream(ctx context.Context, d config.DeviceConfig, _ gnmi.SubParams) (<-chan gnmi.Update, error) {
	m.lastDev = d
	ch := m.streamCh
	if ch == nil {
		c := make(chan gnmi.Update)
		go func() { <-ctx.Done(); close(c) }()
		ch = c
	}
	return ch, nil
}

func cfgWithSW1() *config.AppConfig {
	return &config.AppConfig{Devices: map[string]config.DeviceConfig{
		"sw1": {Name: "sw1", Address: "10.0.0.1:57400", Username: "u", Password: "p", Timeout: "30s"},
	}}
}

func TestDoGet(t *testing.T) {
	mc := &mockClient{getResp: json.RawMessage(`{"ok":true}`)}
	out, isErr := doGet(context.Background(), mc, cfgWithSW1(), GetInput{
		ConnParams: ConnParams{Target: "sw1"}, Path: "/state/system",
	})
	require.False(t, isErr)
	require.Contains(t, out, `"ok":true`)
	require.Equal(t, "/state/system", mc.lastGet.Path)
}

func TestDoGetBadPath(t *testing.T) {
	mc := &mockClient{}
	out, isErr := doGet(context.Background(), mc, cfgWithSW1(), GetInput{
		ConnParams: ConnParams{Target: "sw1"}, Path: "bad",
	})
	require.True(t, isErr)
	require.Contains(t, out, "must start with '/'")
}

func TestDoGetStructuralTruncation(t *testing.T) {
	notif := `{"timestamp":"1","update":[{"path":"/x","val":{"stringVal":"yyyyyyyyyy"}}]}`
	big := `{"notification":[`
	for i := 0; i < 50; i++ {
		if i > 0 {
			big += ","
		}
		big += notif
	}
	big += `]}`
	mc := &mockClient{getResp: json.RawMessage(big)}
	out, isErr := doGet(context.Background(), mc, cfgWithSW1(), GetInput{
		ConnParams: ConnParams{Target: "sw1"}, Path: "/x", MaxBytes: 200,
	})
	require.False(t, isErr)
	require.Contains(t, out, "truncated")
	// Output must be VALID JSON with a parseable, shortened notification array.
	var parsed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	var kept []json.RawMessage
	require.NoError(t, json.Unmarshal(parsed["notification"], &kept))
	require.Greater(t, len(kept), 0)
	require.Less(t, len(kept), 50)
}

func TestDoGetTruncatesWithinNotification(t *testing.T) {
	// The common real-device shape: ONE notification holding many updates.
	var updates []string
	for i := 0; i < 100; i++ {
		updates = append(updates, `{"path":"/x","val":{"stringVal":"aaaaaaaaaa"}}`)
	}
	raw := `{"notification":[{"timestamp":"1","update":[` + strings.Join(updates, ",") + `]}]}`
	mc := &mockClient{getResp: json.RawMessage(raw)}
	out, isErr := doGet(context.Background(), mc, cfgWithSW1(), GetInput{
		ConnParams: ConnParams{Target: "sw1"}, Path: "/x", MaxBytes: 300,
	})
	require.False(t, isErr)
	require.Contains(t, out, "truncated")
	require.Less(t, len(out), len(raw)) // actually shrank, not returned whole
	// valid JSON whose single notification has FEWER updates than the original
	var parsed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	var notifs []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(parsed["notification"], &notifs))
	require.Len(t, notifs, 1)
	var kept []json.RawMessage
	require.NoError(t, json.Unmarshal(notifs[0]["update"], &kept))
	require.Greater(t, len(kept), 0)
	require.Less(t, len(kept), 100)
}

func TestDoGetNotTruncated(t *testing.T) {
	mc := &mockClient{getResp: json.RawMessage(`{"notification":[{"a":1}]}`)}
	out, isErr := doGet(context.Background(), mc, cfgWithSW1(), GetInput{
		ConnParams: ConnParams{Target: "sw1"}, Path: "/x", MaxBytes: 65536,
	})
	require.False(t, isErr)
	require.NotContains(t, out, "truncated")
}

func TestMaxNotificationsWithoutByteTruncation(t *testing.T) {
	// max_notifications should be applied even when the response fits in max_bytes.
	// 5 notifications, all small, default max_bytes (131072).
	var notifs []string
	for i := 0; i < 5; i++ {
		notifs = append(notifs, `{"timestamp":"1","update":[{"path":"/x","val":{"uintVal":1}}]}`)
	}
	raw := `{"notification":[` + strings.Join(notifs, ",") + `]}`
	mc := &mockClient{getResp: json.RawMessage(raw)}
	out, isErr := doGet(context.Background(), mc, cfgWithSW1(), GetInput{
		ConnParams: ConnParams{Target: "sw1"}, Path: "/x", MaxNotifications: 2,
	})
	require.False(t, isErr)
	var parsed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	var kept []json.RawMessage
	require.NoError(t, json.Unmarshal(parsed["notification"], &kept))
	require.Len(t, kept, 2)
}
