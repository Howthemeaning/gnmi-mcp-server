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

	b.WriteString("== gNMI concepts ==\n\n")

	b.WriteString("Path: a gNMI path references a data node in the device's YANG schema.\n")
	b.WriteString("Always start with '/' (e.g. /interfaces/interface/state/counters).\n")
	b.WriteString("Use gnmi_capabilities to discover which YANG models a device supports.\n")
	b.WriteString("Prefix: optional base path prepended to every path in the request (used for\n")
	b.WriteString("origin routing; rarely needed in practice — leave empty unless the device\n")
	b.WriteString("requires a specific origin like 'openconfig:' or 'eos_native:').\n\n")

	b.WriteString("Data type (gnmi_get): controls which datastore to query.\n")
	b.WriteString("  CONFIG      — intended configuration (what you wrote)\n")
	b.WriteString("  STATE       — read-only operational state (counters, uptime, BGP status)\n")
	b.WriteString("  OPERATIONAL — applied/running config + derived state (device's ground truth)\n")
	b.WriteString("  ALL         — everything (default when omitted)\n\n")

	b.WriteString("Encoding: controls the JSON format of the response.\n")
	b.WriteString("  json_ietf — compact, structured JSON_IETF (default; recommended).\n")
	b.WriteString("    Field names include YANG namespace prefixes: 'openconfig-interfaces:in-octets'\n")
	b.WriteString("    instead of just 'in-octets'. Strip the prefix before the colon when searching.\n")
	b.WriteString("  json      — legacy JSON-serialized bytes (more verbose, includes empty fields)\n\n")

	b.WriteString("Subscribe mode (gnmi_subscribe):\n")
	b.WriteString("  ONCE   — one-time telemetry snapshot, returns immediately\n")
	b.WriteString("  STREAM — continuous background telemetry; managed via gnmi_session_*\n\n")

	b.WriteString("Stream mode (when mode=STREAM):\n")
	b.WriteString("  SAMPLE           — periodic polling at sample_interval (e.g. 10s)\n")
	b.WriteString("  ON_CHANGE        — push only when data changes (heartbeat_interval sends\n")
	b.WriteString("                      an empty update if nothing changed)\n")
	b.WriteString("  TARGET_DEFINED   — device chooses the mode (use after checking capabilities)\n\n")

	b.WriteString("Set operations (gnmi_set):\n")
	b.WriteString("  update  — merge value into an existing path (non-destructive)\n")
	b.WriteString("  replace — overwrite the path's content (destructive; use update when possible)\n")
	b.WriteString("  delete  — remove a path (no value needed)\n")
	b.WriteString("gnmi_set is TWO-PHASE for safety: the first call always does a dry-run and\n")
	b.WriteString("returns a confirm_token. Only on the second call with confirm=<token> does\n")
	b.WriteString("the change actually apply. Token expires in 10 minutes.\n\n")

	b.WriteString("== Workflow ==\n\n")
	b.WriteString("1. gnmi_targets — list all configured devices and their addresses.\n")
	b.WriteString("   Pass target NAMES (not raw addresses) to every other tool.\n")
	b.WriteString("2. gnmi_capabilities(target) — discover a device's gNMI version, supported\n")
	b.WriteString("   YANG models, and encodings. Always call this first when unsure which\n")
	b.WriteString("   paths exist on a device.\n")
	b.WriteString("3. gnmi_get(target, path) — read config or state data.\n")
	b.WriteString("   Use max_notifications (e.g. 3) with broad paths like /interfaces/.../state\n")
	b.WriteString("   to limit output and avoid hitting max_bytes truncation.\n")
	b.WriteString("4. gnmi_set(target, operations) — two-phase config write (see above).\n")
	b.WriteString("5. gnmi_subscribe(target, path, mode) — telemetry snapshot (ONCE) or\n")
	b.WriteString("   continuous stream (STREAM → gnmi_session_tail/stop).\n\n")

	b.WriteString("== Vendor path hints (always verify with gnmi_capabilities first) ==\n\n")
	b.WriteString("OpenConfig (Arista, Cisco, Juniper, Nokia):\n")
	b.WriteString("  /interfaces/interface/state/counters\n")
	b.WriteString("  /interfaces/interface/subinterfaces/subinterface/state/counters\n")
	b.WriteString("  /system/state\n")
	b.WriteString("  /network-instances/network-instance/protocols/protocol/bgp/neighbors/neighbor/state\n")
	b.WriteString("  /components/component/state\n\n")
	b.WriteString("Nokia SR OS state tree (non-OpenConfig):\n")
	b.WriteString("  /state/port/ethernet/statistics\n")
	b.WriteString("  /state/system/up-time\n")
	b.WriteString("  /state/router/interface/statistics\n")
	b.WriteString("  /state/service/vprn/bgp/neighbor\n")
	b.WriteString("  Nokia port IDs use bracket notation: e.g. [port-id=1/1/c2/1]\n")
	b.WriteString("  The '/' inside brackets is part of the port identifier, not path separators.\n\n")

	if len(cfg.Devices) == 0 {
		b.WriteString("No devices are configured yet.\n")
		return b.String()
	}
	names := make([]string, 0, len(cfg.Devices))
	for n := range cfg.Devices {
		names = append(names, n)
	}
	sort.Strings(names)
	b.WriteString("== Configured targets ==\n\n")
	for _, n := range names {
		b.WriteString("  - " + n + " (" + cfg.Devices[n].Address + ")\n")
	}
	return b.String()
}

// BuildServer creates the MCP server and conditionally registers tools.
func BuildServer(cfg *config.AppConfig, client gnmi.GnmiClient, mgr *session.Manager, version string) *mcp.Server {
	srv := mcp.NewServer(
		&mcp.Implementation{Name: "gnmi-mcp-server", Version: version},
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

	srv := BuildServer(cfg, client, mgr, version)

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
