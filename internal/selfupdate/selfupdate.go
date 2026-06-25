// Package selfupdate implements `gnmi-mcp-server update` (download the latest
// release and atomically replace this binary) and a cached startup check that
// logs when a newer version is available.
package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const repo = "Howthemeaning/gnmi-mcp-server"
const binName = "gnmi-mcp-server"

// Overridable for tests; default to the public GitHub endpoints.
var (
	httpClient   = &http.Client{Timeout: 60 * time.Second}
	apiLatestURL = "https://api.github.com/repos/" + repo + "/releases/latest"
	downloadBase = "https://github.com/" + repo + "/releases/download"
	userAgent    = binName // GitHub API rejects requests without a User-Agent
)

func parseVer(s string) [3]int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	var out [3]int
	for i, p := range strings.Split(s, ".") {
		if i >= 3 {
			break
		}
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

// isNewer reports whether latest is a strictly higher version than current.
func isNewer(latest, current string) bool {
	l, c := parseVer(latest), parseVer(current)
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// assetName returns the goreleaser archive name for a platform, or an error for
// platforms we don't publish.
func assetName(goos, goarch string) (string, error) {
	if goos != "linux" && goos != "darwin" {
		return "", fmt.Errorf("unsupported OS %q (only linux and darwin are released)", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return "", fmt.Errorf("unsupported architecture %q", goarch)
	}
	return fmt.Sprintf("%s_%s_%s.tar.gz", binName, goos, goarch), nil
}

func findChecksum(sums, asset string) string {
	for _, line := range strings.Split(sums, "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[1] == asset {
			return f[0]
		}
	}
	return ""
}

func shouldCheck(last, now time.Time) bool {
	return now.Sub(last) >= 24*time.Hour
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		return nil, fmt.Errorf("GET %s: %s (GitHub API rate limit reached; try again later)", url, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// fetchVerifiedBinary downloads the asset for tag, REQUIRES a matching SHA256 in
// checksums.txt (fail closed), and returns the extracted binary bytes.
func fetchVerifiedBinary(ctx context.Context, tag, asset string) ([]byte, error) {
	base := downloadBase + "/" + tag
	data, err := httpGet(ctx, base+"/"+asset)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", asset, err)
	}
	sums, err := httpGet(ctx, base+"/checksums.txt")
	if err != nil {
		return nil, fmt.Errorf("cannot fetch checksums.txt (required for verification): %w", err)
	}
	want := findChecksum(string(sums), asset)
	if want == "" {
		return nil, fmt.Errorf("no checksum listed for %s — refusing to install unverified binary", asset)
	}
	got := fmt.Sprintf("%x", sha256.Sum256(data))
	if !strings.EqualFold(got, want) {
		return nil, fmt.Errorf("checksum mismatch for %s (expected %s, got %s)", asset, want, got)
	}
	return extractBinary(data)
}

func latestTag(ctx context.Context) (string, error) {
	body, err := httpGet(ctx, apiLatestURL)
	if err != nil {
		return "", err
	}
	var r struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", err
	}
	if r.TagName == "" {
		return "", fmt.Errorf("no published release found")
	}
	return r.TagName, nil
}

func extractBinary(targz []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(targz))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(h.Name) == binName {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in release archive", binName)
}

// replaceSelf atomically swaps the running binary's file with new contents.
// It writes a temp file in the same directory (so rename is atomic on one FS),
// then renames over the target. Replacing a running binary is safe on Unix.
func replaceSelf(exe string, b []byte) error {
	dir := filepath.Dir(exe)
	f, err := os.CreateTemp(dir, "."+binName+".new-*")
	if err != nil {
		return fmt.Errorf("cannot write into %s (need write permission — try sudo, or reinstall): %v", dir, err)
	}
	tmp := f.Name()
	_, werr := f.Write(b)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("cannot write update into %s: %v", dir, errors.Join(werr, cerr))
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("cannot set exec bit on update: %v", err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("cannot replace %s (try sudo): %v", exe, err)
	}
	return nil
}

// Update downloads the latest release and replaces this binary if it is newer.
func Update(ctx context.Context, current string) error {
	if current == "dev" || current == "" {
		return fmt.Errorf("this is a dev build (version %q) — install a release binary before using update", current)
	}
	tag, err := latestTag(ctx)
	if err != nil {
		return err
	}
	if !isNewer(tag, current) {
		fmt.Printf("Already up to date (current %s, latest %s).\n", current, tag)
		return nil
	}
	asset, err := assetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	fmt.Printf("Updating %s: %s -> %s ...\n", binName, current, tag)
	bin, err := fetchVerifiedBinary(ctx, tag, asset)
	if err != nil {
		return err
	}
	if err := replaceSelf(exe, bin); err != nil {
		return err
	}
	fmt.Printf("Updated to %s at %s. Restart your MCP client to use it.\n", tag, exe)
	return nil
}

// CheckInBackground does a best-effort, once-per-day check and logs (never to
// stdout) when a newer version exists. Safe to call at startup; returns at once.
func CheckInBackground(current, dataDir string) {
	if current == "dev" || current == "" {
		return
	}
	stamp := filepath.Join(dataDir, ".last-update-check")
	if info, err := os.Stat(stamp); err == nil && !shouldCheck(info.ModTime(), time.Now()) {
		return
	}
	go func() {
		_ = os.MkdirAll(dataDir, 0o755)
		_ = os.WriteFile(stamp, []byte(time.Now().Format(time.RFC3339)), 0o644)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tag, err := latestTag(ctx)
		if err != nil {
			return
		}
		if isNewer(tag, current) {
			slog.Warn("a newer version is available",
				"current", current, "latest", tag, "action", "run: gnmi-mcp-server update")
		}
	}()
}
