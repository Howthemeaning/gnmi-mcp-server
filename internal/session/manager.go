package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/gnmi"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	StatusRunning = "running"
	StatusStopped = "stopped"
	StatusEnded   = "ended"
	StatusError   = "error"
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func validateName(n string) error {
	if !nameRe.MatchString(n) {
		return fmt.Errorf("invalid session name %q: must be 1-64 chars of [A-Za-z0-9_-]", n)
	}
	return nil
}

type meta struct {
	Name       string `json:"name"`
	Target     string `json:"target"`
	Address    string `json:"address"`
	Path       string `json:"path"`
	Mode       string `json:"mode"`
	StreamMode string `json:"stream_mode"`
	CreatedAt  string `json:"created_at"`
	Status     string `json:"status"`
	LastError  string `json:"last_error,omitempty"`
}

type SessionInfo struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	Path        string `json:"path"`
	Mode        string `json:"mode"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	OutputBytes int64  `json:"output_bytes"`
	LastError   string `json:"last_error,omitempty"`
}

type session struct {
	meta    meta
	cancel  context.CancelFunc
	outPath string
}

type Manager struct {
	dir      string
	mu       sync.Mutex
	sessions map[string]*session
	maxSize  int64
	backups  int
}

func NewManager(sessionsDir string) *Manager {
	_ = os.MkdirAll(sessionsDir, 0o755)
	return &Manager{
		dir:      sessionsDir,
		sessions: map[string]*session{},
		maxSize:  10 * 1024 * 1024,
		backups:  3,
	}
}

// SetRotation 由 server 用配置覆盖默认轮转参数。
func (m *Manager) SetRotation(maxSize int64, backups int) {
	if maxSize > 0 {
		m.maxSize = maxSize
	}
	if backups > 0 {
		m.backups = backups
	}
}

func (m *Manager) sessionDir(name string) string { return filepath.Join(m.dir, name) }

func writeMeta(dir string, md meta) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(md, "", "  ")
	return os.WriteFile(filepath.Join(dir, "metadata.json"), b, 0o644)
}

func readMeta(dir string) (meta, error) {
	var md meta
	b, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return md, err
	}
	return md, json.Unmarshal(b, &md)
}

func (m *Manager) Create(ctx context.Context, client gnmi.GnmiClient, dev config.DeviceConfig, p gnmi.SubParams, name string) (*SessionInfo, error) {
	if name == "" {
		name = fmt.Sprintf("sub-%d", time.Now().UnixNano())
	}
	if err := validateName(name); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[name]; ok && s.meta.Status == StatusRunning {
		return nil, fmt.Errorf("session %q already running; stop it or use a different name", name)
	}

	dir := m.sessionDir(name)
	outPath := filepath.Join(dir, "output.jsonl")
	md := meta{
		Name: name, Target: dev.Name, Address: dev.Address,
		Path: p.Path, Mode: strings.ToLower(p.Mode), StreamMode: strings.ToLower(p.StreamMode),
		CreatedAt: time.Now().UTC().Format(time.RFC3339), Status: StatusRunning,
	}
	if err := writeMeta(dir, md); err != nil {
		return nil, fmt.Errorf("cannot create session dir: %v", err)
	}

	sctx, cancel := context.WithCancel(context.Background())
	ch, err := client.SubscribeStream(sctx, dev, p)
	if err != nil {
		cancel()
		md.Status = StatusError
		_ = writeMeta(dir, md)
		return nil, err
	}

	s := &session{meta: md, cancel: cancel, outPath: outPath}
	m.sessions[name] = s

	// Fresh start on name reuse: don't append to a prior session's output.
	_ = os.Remove(outPath)
	writer := &lumberjack.Logger{Filename: outPath, MaxSize: int(m.maxSize / (1024 * 1024)), MaxBackups: m.backups}
	if m.maxSize < 1024*1024 {
		writer.MaxSize = 1 // lumberjack 以 MB 计，最小 1
	}
	errLog := filepath.Join(dir, "err.log")

	go func() {
		defer cancel()
		defer writer.Close()
		var lastErr string
		for u := range ch {
			if u.Err != nil {
				lastErr = u.Err.Error()
				_ = os.WriteFile(errLog, []byte(lastErr+"\n"), 0o644)
				continue
			}
			_, _ = writer.Write(append(u.JSON, '\n'))
		}
		m.mu.Lock()
		if cur, ok := m.sessions[name]; ok && cur.meta.Status == StatusRunning {
			if lastErr != "" {
				cur.meta.Status = StatusError
				cur.meta.LastError = lastErr
			} else {
				cur.meta.Status = StatusEnded
			}
			_ = writeMeta(m.sessionDir(name), cur.meta)
		}
		m.mu.Unlock()
	}()

	info := toInfo(md, outPath)
	return &info, nil
}

func (m *Manager) Stop(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[name]
	if !ok {
		return fmt.Errorf("session %q not found", name)
	}
	if s.meta.Status != StatusRunning {
		return nil
	}
	s.cancel()
	s.meta.Status = StatusStopped
	return writeMeta(m.sessionDir(name), s.meta)
}

func (m *Manager) Tail(name string, lines int) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	if lines < 1 {
		lines = 20
	}
	if lines > 500 {
		lines = 500
	}
	b, err := os.ReadFile(filepath.Join(m.sessionDir(name), "output.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			// No output file yet — empty (not an error). Callers may show a
			// friendly "no data yet" message.
			return "", nil
		}
		return "", fmt.Errorf("cannot read session %q output: %v", name, err)
	}
	all := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n"), nil
}

func (m *Manager) List() []SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []SessionInfo{}
	entries, _ := os.ReadDir(m.dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if s, ok := m.sessions[name]; ok {
			out = append(out, toInfo(s.meta, s.outPath))
			continue
		}
		if md, err := readMeta(m.sessionDir(name)); err == nil {
			out = append(out, toInfo(md, filepath.Join(m.sessionDir(name), "output.jsonl")))
		}
	}
	return out
}

// RecoverOnStartup 把上轮进程残留的 running 会话标记为 ended（goroutine 已消失）。
func (m *Manager) RecoverOnStartup() {
	entries, _ := os.ReadDir(m.dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := m.sessionDir(e.Name())
		md, err := readMeta(dir)
		if err != nil {
			continue
		}
		if md.Status == StatusRunning {
			md.Status = StatusEnded
			_ = writeMeta(dir, md)
		}
	}
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, s := range m.sessions {
		if s.meta.Status == StatusRunning {
			s.cancel()
			s.meta.Status = StatusStopped
			_ = writeMeta(m.sessionDir(name), s.meta)
		}
	}
}

func toInfo(md meta, outPath string) SessionInfo {
	var bytes int64
	if fi, err := os.Stat(outPath); err == nil {
		bytes = fi.Size()
	}
	return SessionInfo{
		Name: md.Name, Target: md.Target, Path: md.Path, Mode: md.Mode,
		Status: md.Status, CreatedAt: md.CreatedAt, OutputBytes: bytes, LastError: md.LastError,
	}
}
