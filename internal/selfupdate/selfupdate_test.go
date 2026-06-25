package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsNewer(t *testing.T) {
	require.True(t, isNewer("v1.2.0", "1.1.9"))
	require.True(t, isNewer("0.2.0", "v0.1.5"))
	require.True(t, isNewer("v1.0.1", "1.0.0"))
	require.False(t, isNewer("v1.0.0", "1.0.0"))
	require.False(t, isNewer("v1.0.0", "1.0.1"))
	require.True(t, isNewer("v0.1.0", "dev")) // dev compares as 0.0.0
}

func TestAssetName(t *testing.T) {
	n, err := assetName("darwin", "arm64")
	require.NoError(t, err)
	require.Equal(t, "gnmi-mcp-server_darwin_arm64.tar.gz", n)

	n2, err := assetName("linux", "amd64")
	require.NoError(t, err)
	require.Equal(t, "gnmi-mcp-server_linux_amd64.tar.gz", n2)

	_, err = assetName("windows", "amd64")
	require.Error(t, err) // not released for windows
}

func TestFindChecksum(t *testing.T) {
	sums := "abc123  gnmi-mcp-server_darwin_arm64.tar.gz\ndef456  gnmi-mcp-server_linux_amd64.tar.gz\n"
	require.Equal(t, "abc123", findChecksum(sums, "gnmi-mcp-server_darwin_arm64.tar.gz"))
	require.Equal(t, "def456", findChecksum(sums, "gnmi-mcp-server_linux_amd64.tar.gz"))
	require.Equal(t, "", findChecksum(sums, "missing.tar.gz"))
}

func TestShouldCheck(t *testing.T) {
	now := time.Now()
	require.True(t, shouldCheck(now.Add(-25*time.Hour), now))
	require.False(t, shouldCheck(now.Add(-1*time.Hour), now))
}

func makeTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}))
	_, err := tw.Write(content)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// TestFetchVerifiedBinary locks in fail-CLOSED behavior: anything other than a
// matching checksum must error rather than install an unverified binary.
func TestFetchVerifiedBinary(t *testing.T) {
	bin := []byte("FAKE-BINARY-CONTENT")
	asset := "gnmi-mcp-server_darwin_arm64.tar.gz"
	targz := makeTarGz(t, "gnmi-mcp-server", bin)
	good := sha256hex(targz)

	cases := []struct {
		name    string
		sumBody string
		sum404  bool
		wantErr bool
	}{
		{"valid", good + "  " + asset + "\n", false, false},
		{"mismatch", "deadbeef  " + asset + "\n", false, true},
		{"asset not listed", good + "  other.tar.gz\n", false, true},
		{"checksums 404", "", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/vX/"+asset, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(targz) })
			mux.HandleFunc("/vX/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
				if tc.sum404 {
					http.Error(w, "not found", 404)
					return
				}
				_, _ = w.Write([]byte(tc.sumBody))
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			old := downloadBase
			downloadBase = srv.URL
			defer func() { downloadBase = old }()

			got, err := fetchVerifiedBinary(context.Background(), "vX", asset)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, bin, got)
		})
	}
}
