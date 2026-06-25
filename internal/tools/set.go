package tools

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/gnmi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SetOpInput struct {
	Op    string `json:"op" jsonschema:"update, replace or delete"`
	Path  string `json:"path"`
	Value string `json:"value,omitempty" jsonschema:"JSON value for update/replace; omit for delete"`
}

type SetInput struct {
	ConnParams
	Operations []SetOpInput `json:"operations"`
	DryRun     *bool        `json:"dry_run,omitempty" jsonschema:"preview only, default true"`
	Confirm    string       `json:"confirm,omitempty" jsonschema:"confirm_token from a prior dry-run"`
}

type pendingSet struct {
	hash   string
	expiry time.Time
}

type tokenStore struct {
	mu sync.Mutex
	m  map[string]pendingSet
}

func newTokenStore() *tokenStore { return &tokenStore{m: map[string]pendingSet{}} }

func opsHash(ops []SetOpInput) string {
	b, _ := json.Marshal(ops)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *tokenStore) issue(ops []SetOpInput) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate confirm token: %v", err)
	}
	tok := hex.EncodeToString(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[tok] = pendingSet{hash: opsHash(ops), expiry: time.Now().Add(10 * time.Minute)}
	return tok, nil
}

// consume validates the token matches the ops and has not expired, then deletes it (single-use).
func (s *tokenStore) consume(tok string, ops []SetOpInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[tok]
	if !ok {
		return fmt.Errorf("invalid or expired confirm token; run a dry-run first")
	}
	delete(s.m, tok)
	if time.Now().After(p.expiry) {
		return fmt.Errorf("confirm token expired (10 min TTL); run a dry-run again")
	}
	if p.hash != opsHash(ops) {
		return fmt.Errorf("operations changed since dry-run; confirm rejected, run a new dry-run")
	}
	return nil
}

func toClientOps(in []SetOpInput) []gnmi.SetOp {
	out := make([]gnmi.SetOp, 0, len(in))
	for _, o := range in {
		out = append(out, gnmi.SetOp{Op: o.Op, Path: o.Path, Value: o.Value})
	}
	return out
}

func doSet(ctx context.Context, client gnmi.GnmiClient, cfg *config.AppConfig, store *tokenStore, in SetInput) (string, bool) {
	if len(in.Operations) == 0 {
		return "operations is required and must be non-empty", true
	}
	dev, err := in.apply(cfg)
	if err != nil {
		return err.Error(), true
	}

	// Second call: with confirm token.
	if in.Confirm != "" {
		if err := store.consume(in.Confirm, in.Operations); err != nil {
			return err.Error(), true
		}
		resp, err := client.Set(ctx, dev, toClientOps(in.Operations))
		if err != nil {
			return err.Error(), true
		}
		return string(resp), false
	}

	// First call: dry-run preview + token.
	tok, err := store.issue(in.Operations)
	if err != nil {
		return err.Error(), true
	}
	preview := map[string]any{
		"dry_run":       true,
		"target":        dev.Name,
		"operations":    in.Operations,
		"confirm_token": tok,
		"note":          "Review the operations above. Call gnmi_set again with the same operations plus confirm=<token> to execute. Token expires in 10 minutes.",
	}
	b, _ := json.Marshal(preview)
	return string(b), false
}

func RegisterSet(server *mcp.Server, client gnmi.GnmiClient, cfg *config.AppConfig) {
	store := newTokenStore()
	mcp.AddTool(server, &mcp.Tool{
		Name:        "gnmi_set",
		Description: "Modify gNMI device config. Two-phase: first call returns a dry-run preview + confirm_token; call again with confirm=<token> to apply.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SetInput) (*mcp.CallToolResult, any, error) {
		text, isErr := doSet(ctx, client, cfg, store, in)
		return textResult(text, isErr), nil, nil
	})
}
