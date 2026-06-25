package gnmi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/target"
	"google.golang.org/protobuf/encoding/protojson"
)

// GetParams / SetOp / SubParams / Update 是 tools 层与 client 层之间的数据契约。
type GetParams struct {
	Path     string
	Prefix   string
	Type     string // ALL/CONFIG/STATE/OPERATIONAL
	Encoding string // json / json_ietf
}

type SetOp struct {
	Op    string // update / replace / delete
	Path  string
	Value string // update/replace 的 JSON 值；delete 为空
}

type SubParams struct {
	Path              string
	Mode              string // once / stream / poll
	StreamMode        string // sample / on_change / target_defined
	SampleInterval    time.Duration
	HeartbeatInterval time.Duration
	Encoding          string
}

type Update struct {
	JSON json.RawMessage
	Err  error
}

// GnmiClient 抽象出 RPC 能力，便于 tools 层 mock。
type GnmiClient interface {
	Capabilities(ctx context.Context, dev config.DeviceConfig) (json.RawMessage, error)
	Get(ctx context.Context, dev config.DeviceConfig, p GetParams) (json.RawMessage, error)
	Set(ctx context.Context, dev config.DeviceConfig, ops []SetOp) (json.RawMessage, error)
	SubscribeOnce(ctx context.Context, dev config.DeviceConfig, p SubParams) (json.RawMessage, error)
	// SubscribeStream 返回的 channel 在 ctx 取消时关闭，target 同时关闭。
	SubscribeStream(ctx context.Context, dev config.DeviceConfig, p SubParams) (<-chan Update, error)
}

type gnmicClient struct{}

func New() GnmiClient { return &gnmicClient{} }

func mustDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

func buildTarget(dev config.DeviceConfig) (*target.Target, error) {
	opts := []api.TargetOption{
		api.Name(dev.Name),
		api.Address(dev.Address),
		api.Username(dev.Username),
		api.Password(dev.Password),
		api.Timeout(mustDuration(dev.Timeout)),
	}
	if dev.Insecure {
		opts = append(opts, api.Insecure(true))
	}
	if dev.SkipVerify {
		opts = append(opts, api.SkipVerify(true))
	}
	if dev.TLSCA != "" {
		opts = append(opts, api.TLSCA(dev.TLSCA))
	}
	if dev.TLSCert != "" {
		opts = append(opts, api.TLSCert(dev.TLSCert))
	}
	if dev.TLSKey != "" {
		opts = append(opts, api.TLSKey(dev.TLSKey))
	}
	return api.NewTarget(opts...)
}

func mapError(err error) string {
	if err == nil {
		return ""
	}
	m := strings.ToLower(err.Error())
	switch {
	case strings.Contains(m, "unauthenticated"), strings.Contains(m, "permission denied"):
		return "Authentication failed. Check the device username/password in your config. Details: " + err.Error()
	case strings.Contains(m, "deadline exceeded"), strings.Contains(m, "timeout"):
		return "Request timed out. The device may be slow or unreachable. Details: " + err.Error()
	case strings.Contains(m, "connection refused"):
		return "Connection refused. Verify the target address and that gNMI is enabled. Details: " + err.Error()
	case strings.Contains(m, "not found"), strings.Contains(m, "no such"):
		return "Path or resource not found. Check the gNMI path. Details: " + err.Error()
	case strings.Contains(m, "no route to host"), strings.Contains(m, "network is unreachable"), strings.Contains(m, "host is down"):
		return "Host unreachable (routing/firewall/down). Verify connectivity to the target. Details: " + err.Error()
	case strings.Contains(m, "eof"), strings.Contains(m, "server preface"), strings.Contains(m, "malformed"):
		return "Connected but got no valid gNMI/gRPC response (EOF). Likely the wrong port, or a TLS mismatch — the device may expect TLS while insecure:true was used (or vice-versa; try skip-verify). Details: " + err.Error()
	default:
		return "gnmi error: " + err.Error()
	}
}

func (c *gnmicClient) Capabilities(ctx context.Context, dev config.DeviceConfig) (json.RawMessage, error) {
	tg, err := buildTarget(dev)
	if err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	if err := tg.CreateGNMIClient(ctx); err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	defer tg.Close()
	resp, err := tg.Capabilities(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	return protojson.Marshal(resp)
}

func (c *gnmicClient) Get(ctx context.Context, dev config.DeviceConfig, p GetParams) (json.RawMessage, error) {
	tg, err := buildTarget(dev)
	if err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	if err := tg.CreateGNMIClient(ctx); err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	defer tg.Close()

	enc := p.Encoding
	if enc == "" {
		enc = "json_ietf"
	}
	gopts := []api.GNMIOption{api.Path(p.Path), api.Encoding(enc)}
	if p.Prefix != "" {
		gopts = append(gopts, api.Prefix(p.Prefix))
	}
	if p.Type != "" && strings.ToUpper(p.Type) != "ALL" {
		gopts = append(gopts, api.DataType(strings.ToLower(p.Type)))
	}
	req, err := api.NewGetRequest(gopts...)
	if err != nil {
		return nil, fmt.Errorf("failed to build get request: %v", err)
	}
	resp, err := tg.Get(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	return protojson.Marshal(resp)
}

func (c *gnmicClient) Set(ctx context.Context, dev config.DeviceConfig, ops []SetOp) (json.RawMessage, error) {
	tg, err := buildTarget(dev)
	if err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	if err := tg.CreateGNMIClient(ctx); err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	defer tg.Close()

	var sopts []api.GNMIOption
	for _, op := range ops {
		switch strings.ToLower(op.Op) {
		case "update":
			sopts = append(sopts, api.Update(api.Path(op.Path), api.Value(op.Value, "json_ietf")))
		case "replace":
			sopts = append(sopts, api.Replace(api.Path(op.Path), api.Value(op.Value, "json_ietf")))
		case "delete":
			sopts = append(sopts, api.Delete(op.Path))
		default:
			return nil, fmt.Errorf("unknown set op %q (want update/replace/delete)", op.Op)
		}
	}
	req, err := api.NewSetRequest(sopts...)
	if err != nil {
		return nil, fmt.Errorf("failed to build set request: %v", err)
	}
	resp, err := tg.Set(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	return protojson.Marshal(resp)
}

func buildSubscribeRequest(p SubParams) (*gnmipb.SubscribeRequest, error) {
	enc := p.Encoding
	if enc == "" {
		enc = "json_ietf"
	}
	subOpts := []api.GNMIOption{api.Path(p.Path)}
	if p.StreamMode != "" {
		subOpts = append(subOpts, api.SubscriptionMode(strings.ToLower(p.StreamMode)))
	}
	if p.SampleInterval > 0 {
		subOpts = append(subOpts, api.SampleInterval(p.SampleInterval))
	}
	if p.HeartbeatInterval > 0 {
		subOpts = append(subOpts, api.HeartbeatInterval(p.HeartbeatInterval))
	}
	mode := p.Mode
	if mode == "" {
		mode = "stream"
	}
	return api.NewSubscribeRequest(
		api.Encoding(enc),
		api.SubscriptionListMode(strings.ToLower(mode)),
		api.Subscription(subOpts...),
	)
}

func (c *gnmicClient) SubscribeOnce(ctx context.Context, dev config.DeviceConfig, p SubParams) (json.RawMessage, error) {
	p.Mode = "once"
	tg, err := buildTarget(dev)
	if err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	if err := tg.CreateGNMIClient(ctx); err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	defer tg.Close()

	req, err := buildSubscribeRequest(p)
	if err != nil {
		return nil, fmt.Errorf("failed to build subscribe request: %v", err)
	}
	// target 提供同步 ONCE 辅助方法，返回全部 notification 后结束。
	resps, err := tg.SubscribeOnce(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	var notifs []json.RawMessage
	for _, r := range resps {
		if r.GetSyncResponse() {
			continue
		}
		b, err := protojson.Marshal(r)
		if err == nil {
			notifs = append(notifs, b)
		}
	}
	return marshalArray(notifs), nil
}

func (c *gnmicClient) SubscribeStream(ctx context.Context, dev config.DeviceConfig, p SubParams) (<-chan Update, error) {
	if p.Mode == "" {
		p.Mode = "stream"
	}
	tg, err := buildTarget(dev)
	if err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	if err := tg.CreateGNMIClient(ctx); err != nil {
		return nil, fmt.Errorf("%s", mapError(err))
	}
	req, err := buildSubscribeRequest(p)
	if err != nil {
		tg.Close()
		return nil, fmt.Errorf("failed to build subscribe request: %v", err)
	}
	subName := "mcp-" + dev.Name
	// SubscribeStreamChan 返回该订阅专属的响应/错误 channel；ctx 取消时停止。
	rspCh, errCh := tg.SubscribeStreamChan(ctx, req, subName)

	out := make(chan Update, 64)
	go func() {
		defer close(out)
		defer tg.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case r, ok := <-rspCh:
				if !ok {
					return
				}
				if r.GetSyncResponse() {
					continue
				}
				b, err := protojson.Marshal(r)
				select {
				case out <- Update{JSON: b, Err: err}:
				case <-ctx.Done():
					return
				}
			case e, ok := <-errCh:
				if !ok {
					return
				}
				if e != nil {
					out <- Update{Err: fmt.Errorf("%s", mapError(e))}
				}
				return
			}
		}
	}()
	return out, nil
}

func marshalArray(items []json.RawMessage) json.RawMessage {
	b, _ := json.Marshal(items)
	return b
}
