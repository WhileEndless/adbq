package adb

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// IconCache returns base64 PNG data URIs for app icons, cached on disk under
// ~/.adbq/icons/.  Two extraction strategies are tried in order:
//  1. aapt2 dump badging — accurate, requires Android SDK build-tools.
//  2. Plain ZIP scan — pulls APK, opens with archive/zip, picks the first
//     png from res/mipmap-xxxhdpi/ → ...hdpi/ → ...mdpi/ → res/drawable*/.
//     Fast, no SDK dependency, works for ~90% of apps; misses vector-only icons.
type IconCache struct {
	mu       sync.Mutex
	memCache map[string]string // pkg → data uri
	aapt2    string            // empty when not probed; "-" when missing
}

func NewIconCache() *IconCache {
	return &IconCache{memCache: map[string]string{}}
}

func (ic *IconCache) iconDir() (string, error) {
	d, err := configDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(d, "icons")
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", err
	}
	return p, nil
}

func (ic *IconCache) probeAapt2() string {
	ic.mu.Lock()
	cached := ic.aapt2
	ic.mu.Unlock()
	if cached != "" {
		if cached == "-" {
			return ""
		}
		return cached
	}
	candidates := []string{"aapt2", "/opt/homebrew/bin/aapt2", "/usr/local/bin/aapt2"}
	if home, err := os.UserHomeDir(); err == nil {
		// Android SDK default location on macOS
		matches, _ := filepath.Glob(filepath.Join(home, "Library", "Android", "sdk", "build-tools", "*", "aapt2"))
		candidates = append(candidates, matches...)
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			ic.mu.Lock()
			ic.aapt2 = p
			ic.mu.Unlock()
			return p
		}
		if _, err := os.Stat(c); err == nil {
			ic.mu.Lock()
			ic.aapt2 = c
			ic.mu.Unlock()
			return c
		}
	}
	ic.mu.Lock()
	ic.aapt2 = "-"
	ic.mu.Unlock()
	return ""
}

// IconFor returns a `data:image/png;base64,...` data URI for the given package
// on the device. Empty string means "no icon found, use letter tile".
func (c *Client) IconFor(ctx context.Context, ic *IconCache, serial, pkg string) (string, error) {
	if pkg == "" {
		return "", nil
	}
	key := serial + ":" + pkg
	ic.mu.Lock()
	if v, ok := ic.memCache[key]; ok {
		ic.mu.Unlock()
		return v, nil
	}
	ic.mu.Unlock()

	// Disk cache
	dir, err := ic.iconDir()
	if err != nil {
		return "", err
	}
	cachePath := filepath.Join(dir, sanitize(serial)+"-"+sanitize(pkg)+".png")
	if b, err := os.ReadFile(cachePath); err == nil && len(b) > 0 {
		uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
		ic.mu.Lock()
		ic.memCache[key] = uri
		ic.mu.Unlock()
		return uri, nil
	}

	// Pull APK to a temp host file.
	remoteAPK, err := c.PathOfApp(ctx, serial, pkg)
	if err != nil || remoteAPK == "" {
		return "", err
	}
	tmp, err := os.CreateTemp("", "adbq-apk-*.apk")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)
	if _, err := c.PullFile(ctx, serial, remoteAPK, tmpPath); err != nil {
		return "", err
	}

	var iconBytes []byte
	// Strategy 1: aapt2
	if aapt2 := ic.probeAapt2(); aapt2 != "" {
		iconBytes, _ = extractIconAapt2(aapt2, tmpPath)
	}
	// Strategy 2: ZIP scan fallback
	if len(iconBytes) == 0 {
		iconBytes, _ = extractIconZipScan(tmpPath)
	}
	if len(iconBytes) == 0 {
		return "", nil
	}
	// Persist
	_ = os.WriteFile(cachePath, iconBytes, 0o644)
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(iconBytes)
	ic.mu.Lock()
	ic.memCache[key] = uri
	ic.mu.Unlock()
	return uri, nil
}

// extractIconAapt2 asks aapt2 for the icon path inside the APK, then extracts
// it from the zip. Returns the raw icon bytes (always png for our purposes).
func extractIconAapt2(aapt2, apkPath string) ([]byte, error) {
	out, err := exec.Command(aapt2, "dump", "badging", apkPath).Output()
	if err != nil {
		return nil, err
	}
	// "application-icon-640:'res/mipmap-xxxhdpi-v4/ic_launcher.png'"
	bestRes := -1
	bestPath := ""
	for _, ln := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(ln, "application-icon-") {
			continue
		}
		rest := strings.TrimPrefix(ln, "application-icon-")
		colon := strings.Index(rest, ":'")
		if colon <= 0 {
			continue
		}
		var res int
		for _, r := range rest[:colon] {
			if r < '0' || r > '9' {
				break
			}
			res = res*10 + int(r-'0')
		}
		end := strings.LastIndex(rest, "'")
		if end <= colon+2 {
			continue
		}
		p := rest[colon+2 : end]
		// Skip XML/vector icons — we can't render those easily without a renderer.
		if strings.HasSuffix(strings.ToLower(p), ".xml") {
			continue
		}
		if res > bestRes {
			bestRes = res
			bestPath = p
		}
	}
	if bestPath == "" {
		return nil, fmt.Errorf("aapt2: no png icon path found")
	}
	return readZipFile(apkPath, bestPath)
}

// extractIconZipScan opens the APK as a zip and returns the first png in the
// most-likely-to-be-the-icon location, ordered by density.
func extractIconZipScan(apkPath string) ([]byte, error) {
	r, err := zip.OpenReader(apkPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	// Score directories so xxxhdpi wins over mdpi.
	scoreDir := func(name string) int {
		l := strings.ToLower(name)
		switch {
		case strings.Contains(l, "mipmap-xxxhdpi"):
			return 1000
		case strings.Contains(l, "mipmap-xxhdpi"):
			return 900
		case strings.Contains(l, "mipmap-xhdpi"):
			return 800
		case strings.Contains(l, "mipmap-hdpi"):
			return 700
		case strings.Contains(l, "mipmap-mdpi"):
			return 600
		case strings.Contains(l, "mipmap"):
			return 500
		case strings.Contains(l, "drawable-xxxhdpi"):
			return 400
		case strings.Contains(l, "drawable-xxhdpi"):
			return 350
		case strings.Contains(l, "drawable-xhdpi"):
			return 300
		case strings.Contains(l, "drawable-hdpi"):
			return 250
		case strings.Contains(l, "drawable-mdpi"):
			return 200
		case strings.Contains(l, "drawable"):
			return 100
		}
		return 0
	}
	type cand struct {
		score int
		entry *zip.File
	}
	var cands []cand
	for _, f := range r.File {
		n := strings.ToLower(f.Name)
		if !strings.HasSuffix(n, ".png") && !strings.HasSuffix(n, ".webp") {
			continue
		}
		// Only icons matter — heuristic: filename contains "ic_launcher" or just sits in mipmap-*
		base := filepath.Base(n)
		s := scoreDir(f.Name)
		if s == 0 {
			continue
		}
		if strings.Contains(base, "launcher") || strings.Contains(base, "icon") || s >= 500 {
			cands = append(cands, cand{score: s, entry: f})
		}
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("no usable icon in APK")
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	rc, err := cands[0].entry.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func readZipFile(apkPath, inside string) ([]byte, error) {
	r, err := zip.OpenReader(apkPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == inside {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("not in apk: %s", inside)
}
