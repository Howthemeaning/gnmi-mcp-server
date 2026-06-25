package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/install"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/selfupdate"
	"github.com/Howthemeaning/gnmi-mcp-server/internal/server"
)

func resolveConfigPath(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	if env := os.Getenv("GNMI_CONFIG"); env != "" {
		return env
	}
	for _, c := range []string{"./gnmi-mcp.yaml"} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := home + "/.gnmi-mcp-server/config.yaml"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// version is set at release time via -ldflags "-X main.version=<tag>".
var version = "dev"

// runInstall handles `gnmi-mcp-server install`: wires the server into any
// detected MCP clients (Claude Code, Codex, OpenCode).
func runInstall(argv []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	var cfgPath string
	fs.StringVar(&cfgPath, "config", "", "bake --config <path> into the registered command (optional)")
	_ = fs.Parse(argv)

	fmt.Println("Wiring gnmi-mcp-server into local MCP clients...")
	for _, r := range install.Wire(cfgPath) {
		fmt.Printf("  %-12s %-11s %s\n", r.Client, r.Action, r.Detail)
	}
	fmt.Println("\nDone. Restart your MCP client so it picks up the server.")
}

// runUpdate handles `gnmi-mcp-server update`: download the latest release and
// replace this binary if it is newer.
func runUpdate() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := selfupdate.Update(ctx, version); err != nil {
		fmt.Fprintln(os.Stderr, "update failed:", err)
		os.Exit(1)
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "install" {
		runInstall(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "update" {
		runUpdate()
		return
	}

	var cfgPath string
	var showVersion bool
	flag.StringVar(&cfgPath, "config", "", "path to YAML config (or set GNMI_CONFIG)")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()
	if showVersion {
		fmt.Println("gnmi-mcp-server", version)
		return
	}

	path := resolveConfigPath(cfgPath)
	if path == "" {
		fmt.Fprintln(os.Stderr, `error: no config file found. Provide one (checked in this order):
  1. --config <path>                  (flag)
  2. GNMI_CONFIG=<path>               (environment variable)
  3. ./gnmi-mcp.yaml                  (current working directory)
  4. ~/.gnmi-mcp-server/config.yaml   (home default)`)
		os.Exit(1)
	}
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	if err := server.Run(context.Background(), cfg, version); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
