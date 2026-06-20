package adb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadVerifiedAssetHostAllowlist(t *testing.T) {
	// A URL whose host isn't allowlisted must be rejected before any network I/O,
	// so a poisoned release index can never redirect us to an attacker host.
	err := downloadVerifiedAsset(context.Background(),
		"https://evil.example.com/frida.whl", "", filepath.Join(t.TempDir(), "x"),
		[]string{"https://files.pythonhosted.org/"})
	if err == nil {
		t.Fatal("expected rejection for non-allowlisted host")
	}
}

func TestDownloadVerifiedAsset(t *testing.T) {
	body := []byte("hello frida wheel")
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	allowed := []string{srv.URL}

	// Happy path: downloads, verifies the sha256, writes the file atomically.
	dst := filepath.Join(t.TempDir(), "wheel.whl")
	if err := downloadVerifiedAsset(context.Background(), srv.URL+"/wheel.whl", hexSum, dst, allowed); err != nil {
		t.Fatalf("download: %v", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != string(body) {
		t.Fatalf("content mismatch: %q", got)
	}
	if hits != 1 {
		t.Fatalf("want 1 server hit, got %d", hits)
	}

	// Cached: a verified copy already on disk is reused without re-fetching.
	if err := downloadVerifiedAsset(context.Background(), srv.URL+"/wheel.whl", hexSum, dst, allowed); err != nil {
		t.Fatalf("cached download: %v", err)
	}
	if hits != 1 {
		t.Fatalf("expected cache reuse (still 1 hit), got %d", hits)
	}

	// Wrong checksum: rejected, and the bad file is not left at dst.
	dst2 := filepath.Join(t.TempDir(), "wheel2.whl")
	if err := downloadVerifiedAsset(context.Background(), srv.URL+"/wheel.whl", "deadbeef", dst2, allowed); err == nil {
		t.Fatal("expected sha256 mismatch error")
	}
	if _, err := os.Stat(dst2); !os.IsNotExist(err) {
		t.Fatal("bad download should not be left on disk")
	}
}
