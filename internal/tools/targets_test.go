package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDoTargets(t *testing.T) {
	out := doTargets(cfgWithSW1())
	require.Contains(t, out, "sw1")
	require.Contains(t, out, "10.0.0.1:57400")

	var parsed struct {
		Count   int `json:"count"`
		Targets []struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"targets"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	require.Equal(t, 1, parsed.Count)
	require.Equal(t, "sw1", parsed.Targets[0].Name)
	require.Equal(t, "10.0.0.1:57400", parsed.Targets[0].Address)
}
