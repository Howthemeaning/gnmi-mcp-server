package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/gnmi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CapInput struct {
	ConnParams
}

type capCacheEntry struct {
	data   json.RawMessage
	expiry time.Time
}

type capCache struct {
	mu sync.Mutex
	m  map[string]capCacheEntry
}

func newCapCache() *capCache { return &capCache{m: map[string]capCacheEntry{}} }

func (c *capCache) get(key string) (json.RawMessage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.expiry) {
		return nil, false
	}
	return e.data, true
}

func (c *capCache) set(key string, data json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = capCacheEntry{data: data, expiry: time.Now().Add(5 * time.Minute)}
}

func RegisterCapabilities(server *mcp.Server, client gnmi.GnmiClient, cfg *config.AppConfig) {
	cache := newCapCache()
	mcp.AddTool(server, &mcp.Tool{
		Name:        "gnmi_capabilities",
		Description: "Query a gNMI device's version, supported YANG models and encodings.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in CapInput) (*mcp.CallToolResult, any, error) {
		dev, err := in.apply(cfg)
		if err != nil {
			return textResult(err.Error(), true), nil, nil
		}
		key := fmt.Sprintf("%s|insecure=%t|skipverify=%t", dev.Address, dev.Insecure, dev.SkipVerify)
		if cached, ok := cache.get(key); ok {
			return textResult(string(cached), false), nil, nil
		}
		resp, err := client.Capabilities(ctx, dev)
		if err != nil {
			return textResult(err.Error(), true), nil, nil
		}
		cache.set(key, resp)
		return textResult(string(resp), false), nil, nil
	})
}
