package tools

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
)

// ConnParams 是所有工具共享的连接参数（嵌入到各工具 Input 中）。
type ConnParams struct {
	Target     string `json:"target,omitempty" jsonschema:"device name defined in config"`
	Address    string `json:"address,omitempty" jsonschema:"host:port, only when allow-arbitrary is enabled"`
	Insecure   *bool  `json:"insecure,omitempty" jsonschema:"override plaintext gRPC"`
	SkipVerify *bool  `json:"skip_verify,omitempty" jsonschema:"override TLS cert verification skip"`
	TLSCA      string `json:"tls_ca,omitempty"`
	TLSCert    string `json:"tls_cert,omitempty"`
	TLSKey     string `json:"tls_key,omitempty"`
}

// resolveDevice 把连接参数解析为 DeviceConfig（含 allow-arbitrary / override）。
func resolveDevice(cfg *config.AppConfig, target, address string) (config.DeviceConfig, error) {
	if target != "" {
		d, ok := cfg.Devices[target]
		if !ok {
			return config.DeviceConfig{}, fmt.Errorf("unknown target %q; defined devices: %s", target, deviceNames(cfg))
		}
		return d, nil
	}
	if address != "" {
		if !cfg.AllowArbitrary {
			return config.DeviceConfig{}, fmt.Errorf("ad-hoc address is rejected unless allow-arbitrary is true in config")
		}
		return config.DeviceConfig{Name: address, Address: address, Timeout: "30s"}, nil
	}
	return config.DeviceConfig{}, fmt.Errorf("either target or address is required")
}

func (p ConnParams) apply(cfg *config.AppConfig) (config.DeviceConfig, error) {
	d, err := resolveDevice(cfg, p.Target, p.Address)
	if err != nil {
		return d, err
	}
	if p.Insecure != nil {
		d.Insecure = *p.Insecure
	}
	if p.SkipVerify != nil {
		d.SkipVerify = *p.SkipVerify
	}
	// Tool-supplied TLS paths are restricted to tls-dir (spec §4). Without a
	// configured tls-dir, ad-hoc TLS paths from tool arguments are refused.
	for _, f := range []struct{ field, val string }{
		{"tls_ca", p.TLSCA}, {"tls_cert", p.TLSCert}, {"tls_key", p.TLSKey},
	} {
		if f.val == "" {
			continue
		}
		if cfg.TLSDir == "" {
			return d, fmt.Errorf("%s override requires tls-dir to be set in config", f.field)
		}
		if !config.TLSPathAllowed(cfg.TLSDir, f.val) {
			return d, fmt.Errorf("%s=%q is outside tls-dir %q", f.field, f.val, cfg.TLSDir)
		}
	}
	if p.TLSCA != "" {
		d.TLSCA = p.TLSCA
	}
	if p.TLSCert != "" {
		d.TLSCert = p.TLSCert
	}
	if p.TLSKey != "" {
		d.TLSKey = p.TLSKey
	}
	return d, nil
}

func deviceNames(cfg *config.AppConfig) string {
	names := make([]string, 0, len(cfg.Devices))
	for n := range cfg.Devices {
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

func validatePath(p string) error {
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("path %q must start with '/'", p)
	}
	if strings.Contains(p, "..") {
		return fmt.Errorf("path %q must not contain '..'", p)
	}
	for _, r := range p {
		if r == 0 || (!unicode.IsPrint(r)) {
			return fmt.Errorf("path contains invalid characters")
		}
	}
	return nil
}
