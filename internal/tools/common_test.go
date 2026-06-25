package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/stretchr/testify/require"
)

func appCfg() *config.AppConfig {
	return &config.AppConfig{
		Devices: map[string]config.DeviceConfig{
			"sw1": {Name: "sw1", Address: "10.0.0.1:57400", Username: "u", Password: "p", Timeout: "30s"},
		},
	}
}

func TestResolveByTarget(t *testing.T) {
	d, err := resolveDevice(appCfg(), "sw1", "")
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1:57400", d.Address)
}

func TestResolveUnknownTarget(t *testing.T) {
	_, err := resolveDevice(appCfg(), "nope", "")
	require.Error(t, err)
}

func TestResolveAddressDeniedByDefault(t *testing.T) {
	_, err := resolveDevice(appCfg(), "", "1.2.3.4:57400")
	require.Error(t, err)
	require.Contains(t, err.Error(), "allow-arbitrary")
}

func TestResolveAddressAllowed(t *testing.T) {
	c := appCfg()
	c.AllowArbitrary = true
	c.Devices["1.2.3.4:57400"] = config.DeviceConfig{} // ensure not required
	d, err := resolveDevice(c, "", "1.2.3.4:57400")
	require.NoError(t, err)
	require.Equal(t, "1.2.3.4:57400", d.Address)
}

func TestValidatePath(t *testing.T) {
	require.NoError(t, validatePath("/state/system"))
	require.Error(t, validatePath("state/system")) // no leading /
	require.Error(t, validatePath("/a/../b"))       // traversal
}

func TestApplyTLSOverrideRejectedWithoutDir(t *testing.T) {
	c := appCfg() // no tls-dir configured
	_, err := ConnParams{Target: "sw1", TLSCA: "/etc/passwd"}.apply(c)
	require.Error(t, err)
	require.Contains(t, err.Error(), "tls_ca")
}

func TestApplyTLSOverrideRejectedOutsideDir(t *testing.T) {
	c := appCfg()
	c.TLSDir = t.TempDir()
	_, err := ConnParams{Target: "sw1", TLSCA: "/etc/passwd"}.apply(c)
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside tls-dir")
}

func TestApplyTLSOverrideAllowedInsideDir(t *testing.T) {
	c := appCfg()
	dir := t.TempDir()
	c.TLSDir = dir
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ca.pem"), []byte("x"), 0o600))
	d, err := ConnParams{Target: "sw1", TLSCA: "ca.pem"}.apply(c)
	require.NoError(t, err)
	require.Equal(t, "ca.pem", d.TLSCA)
}
