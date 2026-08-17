package adb

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Downloading rootAVD reaches the network and writes ~11 MB into the user's
// cache, so it is opt-in:
//
//	ADBQ_PROBE_ROOTAVD=1 go test ./internal/adb/ -run TestHostRootAVDDownload -v
//
// Nothing here touches a system image; it only proves the pinned commit still
// resolves and still hashes to what this package expects.
func TestHostRootAVDDownload(t *testing.T) {
	if os.Getenv("ADBQ_PROBE_ROOTAVD") == "" {
		t.Skip("set ADBQ_PROBE_ROOTAVD=1 to download and verify rootAVD")
	}
	info, err := InstallRootAVD(context.Background(), func(s string) { t.Log(s) })
	if err != nil {
		t.Fatalf("InstallRootAVD: %v", err)
	}
	if !info.Installed {
		t.Fatal("install reported success but the status says not installed")
	}
	t.Logf("dir=%s", info.Dir)
	t.Logf("script=%s", info.Script)

	if got := sumFile(filepath.Join(info.Dir, "rootAVD.sh")); got != rootAVDScriptSHA {
		t.Errorf("rootAVD.sh sha256 = %s, pinned %s", got, rootAVDScriptSHA)
	}
	if got := sumFile(filepath.Join(info.Dir, "Magisk.zip")); got != rootAVDMagiskSHA {
		t.Errorf("Magisk.zip sha256 = %s, pinned %s", got, rootAVDMagiskSHA)
	}
	fi, err := os.Stat(filepath.Join(info.Dir, "rootAVD.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Error("rootAVD.sh must be executable after install")
	}
	// The archive must not be left behind in the cache.
	if _, err := os.Stat(filepath.Join(info.Dir, "rootAVD.tar.gz")); err == nil {
		t.Error("the downloaded archive should be removed after extraction")
	}
}
