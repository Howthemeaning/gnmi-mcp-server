package server

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/gnmi"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/selfupdate"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/session"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/natefinch/lumberjack.v2"
)

func setupLogger(cfg *config.AppConfig) {
	logDir := filepath.Join(cfg.DataDir, "logs")
	_ = os.MkdirAll(logDir, 0o755)
	w := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "gnmi-mcp-server.log"),
		MaxSize:    int(cfg.LogMaxSize / (1024 * 1024)),
		MaxBackups: cfg.LogBackupCount,
	}
	if w.MaxSize < 1 {
		w.MaxSize = 10
	}
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})))
}

// buildInstructions produces the server-level MCP instructions shown to the
// client/LLM at connect time: how to use the tools, vendor path hints, and the
// live list of configured devices (so the model knows valid target names).
func buildInstructions(cfg *config.AppConfig) string {
	var b strings.Builder
	b.WriteString("This server runs gNMI operations against network devices.\n\n")
	b.WriteString("Workflow:\n")
	b.WriteString("- Call gnmi_targets to list configured devices. Pass a target NAME (not a raw address) to every tool.\n")
	b.WriteString("- gnmi_capabilities(target): discover a device's supported YANG models/encodings when unsure which paths exist.\n")
	b.WriteString("- gnmi_get(target, path): read config/state data. Paths are vendor/model-specific.\n")
	b.WriteString("- gnmi_set(target, operations): two-phase. The first call returns a dry-run preview plus a confirm_token; call again with the same operations and confirm=<token> to apply.\n")
	b.WriteString("- gnmi_subscribe(target, path, mode): ONCE returns a snapshot; STREAM starts a background session managed via gnmi_session_list / gnmi_session_stop / gnmi_session_tail.\n\n")
	b.WriteString("Example paths (always verify per device with gnmi_capabilities):\n")
	b.WriteString("- OpenConfig (Arista and most vendors): /interfaces/interface/state/counters, /system/state, /network-instances/network-instance/protocols/protocol/bgp/neighbors/neighbor/state\n")
	b.WriteString("- Nokia SR OS state tree: /state/port/ethernet/statistics, /state/system/up-time, /state/router/interface/statistics\n\n")

	if len(cfg.Devices) == 0 {
		b.WriteString("No devices are configured yet.\n")
		return b.String()
	}
	names := make([]string, 0, len(cfg.Devices))
	for n := range cfg.Devices {
		names = append(names, n)
	}
	sort.Strings(names)
	b.WriteString("Configured targets:\n")
	for _, n := range names {
		b.WriteString("  - " + n + " (" + cfg.Devices[n].Address + ")\n")
	}
	return b.String()
}

// BuildServer creates the MCP server and conditionally registers tools.
func BuildServer(cfg *config.AppConfig, client gnmi.GnmiClient, mgr *session.Manager) *mcp.Server {
	srv := mcp.NewServer(
		&mcp.Implementation{Name: "gnmi-mcp-server", Version: "2.0.0"},
		&mcp.ServerOptions{Instructions: buildInstructions(cfg)},
	)
	tools.RegisterTargets(srv, cfg)
	tools.RegisterPrompts(srv)
	tools.RegisterCapabilities(srv, client, cfg)
	tools.RegisterGet(srv, client, cfg)
	if !cfg.ReadOnly {
		tools.RegisterSet(srv, client, cfg)
	}
	tools.RegisterSubscribe(srv, client, cfg, mgr)
	tools.RegisterSessionTools(srv, mgr)
	tools.RegisterPath(srv, cfg)
	return srv
}

func Run(ctx context.Context, cfg *config.AppConfig, version string) error {
	setupLogger(cfg)
	selfupdate.CheckInBackground(version, cfg.DataDir)
	client := gnmi.New()
	mgr := session.NewManager(filepath.Join(cfg.DataDir, "sessions"))
	mgr.SetRotation(cfg.LogMaxSize, cfg.LogBackupCount)
	mgr.RecoverOnStartup()

	srv := BuildServer(cfg, client, mgr)

	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-sigCtx.Done()
		slog.Info("shutting down, stopping sessions")
		mgr.Shutdown()
	}()

	slog.Info("gnmi-mcp-server starting", "devices", len(cfg.Devices), "read_only", cfg.ReadOnly)
	return srv.Run(sigCtx, &mcp.StdioTransport{})
}
