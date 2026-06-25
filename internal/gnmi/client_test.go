package gnmi

import (
	"testing"
	"time"

	"github.com/Howthemeaning/gnmi-mcp-server/internal/config"
	"github.com/stretchr/testify/require"
)

func dev() config.DeviceConfig {
	return config.DeviceConfig{
		Name: "sw1", Address: "10.0.0.1:57400",
		Username: "admin", Password: "secret", Timeout: "30s",
	}
}

func TestBuildTargetNoError(t *testing.T) {
	tg, err := buildTarget(dev())
	require.NoError(t, err)
	require.NotNil(t, tg)
}

func TestParseDuration(t *testing.T) {
	require.Equal(t, 30*time.Second, mustDuration("30s"))
	require.Equal(t, time.Minute, mustDuration("1m"))
	require.Equal(t, 30*time.Second, mustDuration("bad")) // fallback
}

func TestMapErrorAuth(t *testing.T) {
	msg := mapError(errString("rpc error: code = Unauthenticated desc = nope"))
	require.Contains(t, msg, "Authentication failed")
}

func TestMapErrorTimeout(t *testing.T) {
	msg := mapError(errString("context deadline exceeded"))
	require.Contains(t, msg, "timed out")
}

func TestMapErrorRefused(t *testing.T) {
	msg := mapError(errString("connection refused"))
	require.Contains(t, msg, "Connection refused")
}

func TestMapErrorEOF(t *testing.T) {
	msg := mapError(errString("rpc error: code = Unavailable desc = error reading server preface: EOF"))
	require.Contains(t, msg, "TLS")
}

func TestMapErrorUnreachable(t *testing.T) {
	msg := mapError(errString("dial tcp 10.0.0.1:57400: connect: no route to host"))
	require.Contains(t, msg, "unreachable")
}

type errString string

func (e errString) Error() string { return string(e) }
