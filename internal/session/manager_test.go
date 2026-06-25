package session

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/gnmi"
	"github.com/stretchr/testify/require"
)

// fakeClient 实现 gnmi.GnmiClient，仅用于 session 测试。
type fakeClient struct {
	updates    []gnmi.Update
	closeAfter bool // close the channel after emitting updates (simulates stream end/error)
}

func (f *fakeClient) Capabilities(context.Context, config.DeviceConfig) (json.RawMessage, error) {
	return nil, nil
}
func (f *fakeClient) Get(context.Context, config.DeviceConfig, gnmi.GetParams) (json.RawMessage, error) {
	return nil, nil
}
func (f *fakeClient) Set(context.Context, config.DeviceConfig, []gnmi.SetOp) (json.RawMessage, error) {
	return nil, nil
}
func (f *fakeClient) SubscribeOnce(context.Context, config.DeviceConfig, gnmi.SubParams) (json.RawMessage, error) {
	return nil, nil
}
func (f *fakeClient) SubscribeStream(ctx context.Context, _ config.DeviceConfig, _ gnmi.SubParams) (<-chan gnmi.Update, error) {
	ch := make(chan gnmi.Update)
	go func() {
		defer close(ch)
		for _, u := range f.updates {
			select {
			case ch <- u:
			case <-ctx.Done():
				return
			}
		}
		if f.closeAfter {
			return
		}
		<-ctx.Done() // keep open until stop
	}()
	return ch, nil
}

func dev() config.DeviceConfig {
	return config.DeviceConfig{Name: "sw1", Address: "10.0.0.1:57400", Username: "u", Password: "p", Timeout: "30s"}
}

func TestStreamErrorMarksError(t *testing.T) {
	mgr := NewManager(t.TempDir())
	fc := &fakeClient{updates: []gnmi.Update{{Err: fmt.Errorf("rpc connection dropped")}}, closeAfter: true}
	_, err := mgr.Create(context.Background(), fc, dev(), gnmi.SubParams{Path: "/x", Mode: "STREAM"}, "errsess")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		for _, s := range mgr.List() {
			if s.Name == "errsess" {
				return s.Status == StatusError && s.LastError != ""
			}
		}
		return false
	}, 2*time.Second, 20*time.Millisecond)
}

func TestValidateName(t *testing.T) {
	require.NoError(t, validateName("ok-name_1"))
	require.Error(t, validateName("bad/name"))
	require.Error(t, validateName(""))
}

func TestCreateAndTail(t *testing.T) {
	mgr := NewManager(t.TempDir())
	fc := &fakeClient{updates: []gnmi.Update{
		{JSON: json.RawMessage(`{"i":1}`)},
		{JSON: json.RawMessage(`{"i":2}`)},
	}}
	s, err := mgr.Create(context.Background(), fc, dev(), gnmi.SubParams{Path: "/x", Mode: "stream"}, "sess1")
	require.NoError(t, err)
	require.Equal(t, "sess1", s.Name)

	require.Eventually(t, func() bool {
		out, _ := mgr.Tail("sess1", 10)
		return len(out) > 0
	}, 2*time.Second, 20*time.Millisecond)

	out, err := mgr.Tail("sess1", 10)
	require.NoError(t, err)
	require.Contains(t, out, `"i":2`)
}

func TestDuplicateRejected(t *testing.T) {
	mgr := NewManager(t.TempDir())
	fc := &fakeClient{}
	_, err := mgr.Create(context.Background(), fc, dev(), gnmi.SubParams{Path: "/x", Mode: "stream"}, "dup")
	require.NoError(t, err)
	_, err = mgr.Create(context.Background(), fc, dev(), gnmi.SubParams{Path: "/x", Mode: "stream"}, "dup")
	require.Error(t, err)
}

func TestStopAndList(t *testing.T) {
	mgr := NewManager(t.TempDir())
	fc := &fakeClient{}
	_, err := mgr.Create(context.Background(), fc, dev(), gnmi.SubParams{Path: "/x", Mode: "stream"}, "s1")
	require.NoError(t, err)
	require.NoError(t, mgr.Stop("s1"))
	list := mgr.List()
	require.Len(t, list, 1)
	require.Equal(t, StatusStopped, list[0].Status)
}

func TestRecoverMarksEnded(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	fc := &fakeClient{}
	_, err := mgr.Create(context.Background(), fc, dev(), gnmi.SubParams{Path: "/x", Mode: "stream"}, "old")
	require.NoError(t, err)
	// 模拟进程重启：丢弃内存注册表，仅磁盘元数据留存（status=running）
	require.NoError(t, writeMeta(filepath.Join(dir, "old"), meta{Name: "old", Status: StatusRunning, Path: "/x", Mode: "stream"}))
	mgr2 := NewManager(dir)
	mgr2.RecoverOnStartup()
	list := mgr2.List()
	require.Len(t, list, 1)
	require.Equal(t, StatusEnded, list[0].Status)
}
