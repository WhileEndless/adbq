package adb

import (
	"context"
	"os"
	"testing"
)

// Downloading jadx reaches the network and writes ~70 MB into the user's cache,
// so it is opt-in:
//
//	ADBQ_PROBE_JADX=1 go test ./internal/adb/ -run TestHostJadx -v
//
// It proves the pinned release still resolves, still hashes to what this package
// expects, and still contains a launcher where every later lookup expects one.
func TestHostJadxDownload(t *testing.T) {
	if os.Getenv("ADBQ_PROBE_JADX") == "" {
		t.Skip("set ADBQ_PROBE_JADX=1 to download and verify jadx")
	}
	if err := InstallJadx(context.Background(), func(s string) { t.Log(s) }); err != nil {
		t.Fatalf("InstallJadx: %v", err)
	}
	bin, ver, dir := managedJadx()
	if bin == "" {
		t.Fatal("install reported success but no launcher was found")
	}
	t.Logf("version=%s dir=%s bin=%s", ver, dir, bin)
	if fi, err := os.Stat(bin); err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("the launcher must exist and be executable: %v", err)
	}
	// The archive must not be left behind in the cache.
	root, err := jadxRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root + "/jadx-" + jadxVersion + ".zip"); err == nil {
		t.Error("the downloaded archive should be removed after extraction")
	}
}

// The pin can go stale silently — the release stays downloadable while a newer
// one exists. This reports the drift without changing anything.
func TestHostJadxPinIsCurrent(t *testing.T) {
	if os.Getenv("ADBQ_PROBE_JADX") == "" {
		t.Skip("set ADBQ_PROBE_JADX=1 to query the jadx release feed")
	}
	rel, err := LatestJadxRelease(context.Background())
	if err != nil {
		t.Fatalf("LatestJadxRelease: %v", err)
	}
	t.Logf("latest=%s pinned=%s digest=%s", rel.Version, jadxVersion, rel.SHA256)
	if rel.SHA256 == "" {
		t.Error("the release feed published no checksum, so the update path cannot verify a download")
	}
	if rel.Newer {
		t.Logf("the pin is behind: %s is available", rel.Version)
	}
}
