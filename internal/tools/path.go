package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PathInput struct {
	Search string `json:"search,omitempty" jsonschema:"filter YANG modules by keyword"`
}

func RegisterPath(server *mcp.Server, cfg *config.AppConfig) {
	if cfg.YangDir == "" {
		return // 未配置则不注册
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "gnmi_path",
		Description: "List available YANG modules under the configured yang-dir (optionally filtered).",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in PathInput) (*mcp.CallToolResult, any, error) {
		var modules []string
		_ = filepath.WalkDir(cfg.YangDir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if strings.HasSuffix(d.Name(), ".yang") {
				if in.Search == "" || strings.Contains(strings.ToLower(d.Name()), strings.ToLower(in.Search)) {
					modules = append(modules, d.Name())
				}
			}
			return nil
		})
		b, _ := json.Marshal(map[string]any{"yang_dir": cfg.YangDir, "modules": modules})
		return textResult(string(b), false), nil, nil
	})
}
