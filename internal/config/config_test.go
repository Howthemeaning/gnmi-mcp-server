package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestLoadMinimal(t *testing.T) {
	p := writeCfg(t, `
devices:
  sw1:
    address: 10.0.0.1:57400
    username: admin
    password: secret
`)
	cfg, err := Load(p)
	require.NoError(t, err)
	d, ok := cfg.Devices["sw1"]
	require.True(t, ok)
	require.Equal(t, "sw1", d.Name)
	require.Equal(t, "10.0.0.1:57400", d.Address)
	require.Equal(t, "admin", d.Username)
	require.Equal(t, "secret", d.Password)
	require.Equal(t, "30s", d.Timeout) // default
	require.False(t, cfg.ReadOnly)
}

func TestEnvInterpolationIgnoresComments(t *testing.T) {
	// ${...} inside comments must NOT be treated as a required variable.
	p := writeCfg(t, `
# mentions ${UNDEFINED_IN_COMMENT} which is not set
devices:
  sw1:
    address: 10.0.0.1:57400
    username: admin
    password: secret  # inline ${ALSO_UNDEFINED}
`)
	cfg, err := Load(p)
	require.NoError(t, err)
	require.Equal(t, "secret", cfg.Devices["sw1"].Password)
}

func TestEnvInterpolation(t *testing.T) {
	t.Setenv("MY_PASS", "fromenv")
	p := writeCfg(t, `
devices:
  sw1:
    address: 10.0.0.1:57400
    username: admin
    password: ${MY_PASS}
`)
	cfg, err := Load(p)
	require.NoError(t, err)
	require.Equal(t, "fromenv", cfg.Devices["sw1"].Password)
}

func TestEnvInterpolationDefault(t *testing.T) {
	p := writeCfg(t, `
devices:
  sw1:
    address: 10.0.0.1:57400
    username: admin
    password: ${ABSENT_VAR:-fallback}
`)
	cfg, err := Load(p)
	require.NoError(t, err)
	require.Equal(t, "fallback", cfg.Devices["sw1"].Password)
}

func TestEnvInterpolationMissing(t *testing.T) {
	p := writeCfg(t, `
devices:
  sw1:
    address: 10.0.0.1:57400
    username: admin
    password: ${DEFINITELY_ABSENT_VAR}
`)
	_, err := Load(p)
	require.Error(t, err)
	require.Contains(t, err.Error(), "DEFINITELY_ABSENT_VAR")
}

func TestMissingCredentials(t *testing.T) {
	p := writeCfg(t, `
devices:
  sw1:
    address: 10.0.0.1:57400
    username: admin
`)
	_, err := Load(p)
	require.Error(t, err)
	require.Contains(t, err.Error(), "password")
}

func TestBadAddress(t *testing.T) {
	p := writeCfg(t, `
devices:
  sw1:
    address: not-valid
    username: admin
    password: x
`)
	_, err := Load(p)
	require.Error(t, err)
	require.Contains(t, err.Error(), "address")
}

func TestTLSDirRejectsOutside(t *testing.T) {
	dir := t.TempDir()
	certDir := filepath.Join(dir, "certs")
	require.NoError(t, os.MkdirAll(certDir, 0o755))
	p := writeCfg(t, `
tls-dir: `+certDir+`
devices:
  sw1:
    address: 10.0.0.1:57400
    username: admin
    password: x
    tls-ca: /etc/passwd
`)
	_, err := Load(p)
	require.Error(t, err)
	require.Contains(t, err.Error(), "tls-ca")
}
