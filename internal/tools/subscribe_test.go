package tools

import (
	"context"
	"testing"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/session"
	"github.com/stretchr/testify/require"
)

func TestDoSubscribeOnceBadPath(t *testing.T) {
	mc := &mockClient{}
	mgr := session.NewManager(t.TempDir())
	out, isErr := doSubscribe(context.Background(), mc, cfgWithSW1(), mgr, SubscribeInput{
		ConnParams: ConnParams{Target: "sw1"}, Path: "bad", Mode: "ONCE",
	})
	require.True(t, isErr)
	require.Contains(t, out, "must start with '/'")
}

func TestDoSubscribeStreamCreatesSession(t *testing.T) {
	mc := &mockClient{}
	mgr := session.NewManager(t.TempDir())
	out, isErr := doSubscribe(context.Background(), mc, cfgWithSW1(), mgr, SubscribeInput{
		ConnParams: ConnParams{Target: "sw1"}, Path: "/x", Mode: "STREAM", SessionName: "s1",
	})
	require.False(t, isErr)
	require.Contains(t, out, "s1")
	require.Len(t, mgr.List(), 1)
}

func TestDoSubscribePollRejected(t *testing.T) {
	mc := &mockClient{}
	mgr := session.NewManager(t.TempDir())
	out, isErr := doSubscribe(context.Background(), mc, cfgWithSW1(), mgr, SubscribeInput{
		ConnParams: ConnParams{Target: "sw1"}, Path: "/x", Mode: "POLL",
	})
	require.True(t, isErr)
	require.Contains(t, out, "POLL")
	require.Len(t, mgr.List(), 0) // no dead session created
}
