package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetDryRunReturnsToken(t *testing.T) {
	mc := &mockClient{setResp: json.RawMessage(`{"ok":true}`)}
	store := newTokenStore()
	out, isErr := doSet(context.Background(), mc, cfgWithSW1(), store, SetInput{
		ConnParams: ConnParams{Target: "sw1"},
		Operations: []SetOpInput{{Op: "update", Path: "/system/name", Value: "\"x\""}},
	})
	require.False(t, isErr)
	require.Contains(t, out, "confirm_token")
	require.Contains(t, out, "dry_run")
}

func TestSetConfirmExecutes(t *testing.T) {
	mc := &mockClient{setResp: json.RawMessage(`{"ok":true}`)}
	store := newTokenStore()
	ops := []SetOpInput{{Op: "update", Path: "/system/name", Value: "\"x\""}}
	out1, _ := doSet(context.Background(), mc, cfgWithSW1(), store, SetInput{
		ConnParams: ConnParams{Target: "sw1"}, Operations: ops,
	})
	var p struct {
		ConfirmToken string `json:"confirm_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(out1), &p))

	out2, isErr := doSet(context.Background(), mc, cfgWithSW1(), store, SetInput{
		ConnParams: ConnParams{Target: "sw1"}, Operations: ops, Confirm: p.ConfirmToken,
	})
	require.False(t, isErr)
	require.Contains(t, out2, `"ok":true`)
	require.Len(t, mc.setOps, 1)
}

func TestSetConfirmRejectsChangedOps(t *testing.T) {
	mc := &mockClient{setResp: json.RawMessage(`{}`)}
	store := newTokenStore()
	out1, _ := doSet(context.Background(), mc, cfgWithSW1(), store, SetInput{
		ConnParams: ConnParams{Target: "sw1"},
		Operations: []SetOpInput{{Op: "update", Path: "/a", Value: "1"}},
	})
	var p struct {
		ConfirmToken string `json:"confirm_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(out1), &p))

	_, isErr := doSet(context.Background(), mc, cfgWithSW1(), store, SetInput{
		ConnParams: ConnParams{Target: "sw1"},
		Operations: []SetOpInput{{Op: "update", Path: "/DIFFERENT", Value: "1"}},
		Confirm:    p.ConfirmToken,
	})
	require.True(t, isErr)
}

func TestSetBadToken(t *testing.T) {
	mc := &mockClient{}
	store := newTokenStore()
	_, isErr := doSet(context.Background(), mc, cfgWithSW1(), store, SetInput{
		ConnParams: ConnParams{Target: "sw1"},
		Operations: []SetOpInput{{Op: "update", Path: "/a", Value: "1"}},
		Confirm:    "garbage",
	})
	require.True(t, isErr)
}
