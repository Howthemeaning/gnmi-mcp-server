package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigError 携带可操作的英文错误消息。
type ConfigError struct{ msg string }

func (e *ConfigError) Error() string { return e.msg }
func errf(format string, a ...any) error { return &ConfigError{msg: fmt.Sprintf(format, a...)} }

type DeviceConfig struct {
	Name       string `yaml:"-"`
	Address    string `yaml:"address"`
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
	Insecure   bool   `yaml:"insecure"`
	SkipVerify bool   `yaml:"skip-verify"`
	TLSCA      string `yaml:"tls-ca"`
	TLSCert    string `yaml:"tls-cert"`
	TLSKey     string `yaml:"tls-key"`
	Timeout    string `yaml:"timeout"`
}

type AppConfig struct {
	Devices        map[string]DeviceConfig `yaml:"devices"`
	ReadOnly       bool                    `yaml:"read-only"`
	AllowArbitrary bool                    `yaml:"allow-arbitrary"`
	DataDir        string                  `yaml:"data-dir"`
	LogLevel       string                  `yaml:"log-level"`
	LogMaxSize     int64                   `yaml:"log-max-size"`
	LogBackupCount int                     `yaml:"log-backup-count"`
	YangDir        string                  `yaml:"yang-dir"`
	TLSDir         string                  `yaml:"tls-dir"`
}

var (
	addrRe    = regexp.MustCompile(`^[\w.\-]+:\d+$`)
	timeoutRe = regexp.MustCompile(`^\d+(s|m)$`)
	envRe     = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)
)

func expandEnv(s string) (string, error) {
	var missing string
	out := envRe.ReplaceAllStringFunc(s, func(m string) string {
		groups := envRe.FindStringSubmatch(m)
		name, hasDefault, def := groups[1], groups[2] != "", groups[3]
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		if hasDefault {
			return def
		}
		if missing == "" {
			missing = name
		}
		return ""
	})
	if missing != "" {
		return "", errf("environment variable %q referenced in config is not set", missing)
	}
	return out, nil
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// expandNode applies ${ENV} interpolation to string scalar *values* only —
// never to keys, and never to comments (comments are not part of node values).
// This prevents a ${...} written inside a comment from being treated as a
// required variable.
func expandNode(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!str" {
			v, err := expandEnv(n.Value)
			if err != nil {
				return err
			}
			n.Value = v
		}
	case yaml.MappingNode:
		for i := 1; i < len(n.Content); i += 2 { // values are at odd indices
			if err := expandNode(n.Content[i]); err != nil {
				return err
			}
		}
	default: // document / sequence / alias
		for _, c := range n.Content {
			if err := expandNode(c); err != nil {
				return err
			}
		}
	}
	return nil
}

// Load 读取并校验 YAML 配置。
func Load(path string) (*AppConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errf("cannot read config %q: %v", path, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, errf("config is not valid YAML: %v", err)
	}
	if err := expandNode(&root); err != nil {
		return nil, err
	}
	var cfg AppConfig
	if root.Kind != 0 {
		if err := root.Decode(&cfg); err != nil {
			return nil, errf("config is not valid YAML: %v", err)
		}
	}

	// 默认值
	if cfg.DataDir == "" {
		home, _ := os.UserHomeDir()
		cfg.DataDir = filepath.Join(home, ".gnmi-mcp-server", "data")
	}
	cfg.DataDir = expandHome(cfg.DataDir)
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.LogMaxSize == 0 {
		cfg.LogMaxSize = 10 * 1024 * 1024
	}
	if cfg.LogBackupCount == 0 {
		cfg.LogBackupCount = 3
	}
	if cfg.TLSDir != "" {
		cfg.TLSDir = expandHome(cfg.TLSDir)
	}
	if cfg.YangDir != "" {
		cfg.YangDir = expandHome(cfg.YangDir)
	}

	// 设备校验
	for name := range cfg.Devices {
		d := cfg.Devices[name]
		d.Name = name
		if d.Timeout == "" {
			d.Timeout = "30s"
		}
		if !addrRe.MatchString(d.Address) {
			return nil, errf("device %q: invalid address %q, expected host:port", name, d.Address)
		}
		if !timeoutRe.MatchString(d.Timeout) {
			return nil, errf("device %q: invalid timeout %q, expected like 30s or 1m", name, d.Timeout)
		}
		if d.Username == "" {
			return nil, errf("device %q: username is required", name)
		}
		if d.Password == "" {
			return nil, errf("device %q: password is required", name)
		}
		if err := validateTLSPaths(name, d, cfg.TLSDir); err != nil {
			return nil, err
		}
		cfg.Devices[name] = d
	}
	return &cfg, nil
}

// TLSPathAllowed reports whether a TLS file path resolves inside tlsDir.
// Relative paths are resolved against tlsDir; symlinks are resolved to prevent
// escape. Callers must ensure tlsDir is non-empty. Exported so tool-argument
// TLS overrides can be validated with the same logic as config-defined paths.
func TLSPathAllowed(tlsDir, path string) bool {
	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(tlsDir, full)
	}
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		resolved = filepath.Clean(full)
	}
	base, err := filepath.EvalSymlinks(tlsDir)
	if err != nil {
		base = filepath.Clean(tlsDir)
	}
	return resolved == base || strings.HasPrefix(resolved, base+string(os.PathSeparator))
}

func validateTLSPaths(name string, d DeviceConfig, tlsDir string) error {
	if tlsDir == "" {
		return nil
	}
	for _, f := range []struct{ field, val string }{
		{"tls-ca", d.TLSCA}, {"tls-cert", d.TLSCert}, {"tls-key", d.TLSKey},
	} {
		if f.val != "" && !TLSPathAllowed(tlsDir, f.val) {
			return errf("device %q: %s=%q is outside tls-dir %q", name, f.field, f.val, tlsDir)
		}
	}
	return nil
}
