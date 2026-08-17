package adb

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The advice gate decides whether the user is even offered a tool that rewrites
// a shared system image, so every branch is pinned.
func TestRootAVDAdvice(t *testing.T) {
	cases := []struct {
		name      string
		api       int
		playStore bool
		root      string
		patched   bool
		want      string
	}{
		// Root that already works outranks everything else.
		{"adb root already works", 34, false, "adb-root", false, RootNotNeeded},
		{"adb root on a play image", 33, true, "adb-root", false, RootNotNeeded},
		{"su already present", 33, true, "su", false, RootAlready},
		{"already patched", 33, true, "no", true, RootAlready},

		// Non-Play-Store images are debuggable; don't send users patching.
		{"google_apis not running", 34, false, "", false, RootNotNeeded},
		{"google_apis that refused adb root", 34, false, "no", false, RootEligible},

		// Play Store images are the case rootAVD exists for.
		{"play store api 33", 33, true, "no", false, RootEligible},
		{"play store api 25 lower bound", 25, true, "no", false, RootEligible},
		{"play store api 34 upper tested bound", 34, true, "no", false, RootEligible},

		// Known-broken and out-of-range levels.
		{"api 28 is unsupported by the tool", 28, true, "no", false, RootUnsupported},
		{"api 24 below the floor", 24, true, "no", false, RootUnsupported},
		{"api 21 below the floor", 21, true, "no", false, RootUnsupported},

		// Above the tested range it is risky, not forbidden: an API 36.1 image
		// patched by rootAVD was observed working.
		{"api 35 above the tested range", 35, true, "no", false, RootRisky},
		{"api 36 above the tested range", 36, true, "no", false, RootRisky},
		{"preview image with no numeric level", 0, true, "no", false, RootRisky},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			action, reason := RootAVDAdvice(tc.api, tc.playStore, tc.root, tc.patched)
			if action != tc.want {
				t.Errorf("RootAVDAdvice(%d, play=%v, root=%q, patched=%v) = %q, want %q",
					tc.api, tc.playStore, tc.root, tc.patched, action, tc.want)
			}
			if strings.TrimSpace(reason) == "" {
				t.Error("every verdict must carry an explanation the UI can show")
			}
		})
	}
}

func TestRootAVDOffered(t *testing.T) {
	offered := map[string]bool{
		RootEligible: true, RootRisky: true,
		RootNotNeeded: false, RootAlready: false, RootUnsupported: false,
	}
	for action, want := range offered {
		if got := RootAVDOffered(action); got != want {
			t.Errorf("RootAVDOffered(%q) = %v, want %v", action, got, want)
		}
	}
}

// The consent dialog is the only place the user learns that a shared system
// image is about to be rewritten, so the disclosures must never go missing.
func TestRootAVDStatusDisclosesTheRisks(t *testing.T) {
	s := RootAVDStatus()
	if s.Commit != rootAVDCommit || !strings.Contains(s.Archive, rootAVDCommit) {
		t.Error("the download URL must name the pinned commit, not a branch")
	}
	if !strings.HasPrefix(s.Archive, "https://gitlab.com/") {
		t.Errorf("archive host is not the allowlisted one: %s", s.Archive)
	}
	if s.License != "GPL-3.0" || s.ScriptSHA == "" || s.MagiskSHA == "" {
		t.Errorf("provenance fields incomplete: %+v", s)
	}
	joined := strings.ToLower(strings.Join(s.Disclosures, "\n"))
	for _, must := range []string{"gpl-3.0", "every avd using the same system image", "backup", "github", "cold-boot"} {
		if !strings.Contains(joined, must) {
			t.Errorf("disclosures must mention %q:\n%s", must, joined)
		}
	}
}

func TestRootAVDCommand(t *testing.T) {
	got := RootAVDCommand("/cache/adbq/rootavd/abc", "system-images/android-33/google_apis_playstore/x86_64/ramdisk.img", false)
	if !strings.HasPrefix(got, "bash /cache/adbq/rootavd/abc/rootAVD.sh ") {
		t.Errorf("unexpected command: %s", got)
	}
	if strings.HasSuffix(got, "restore") {
		t.Error("the patch command must not carry the restore argument")
	}
	if r := RootAVDCommand("/x", "y/ramdisk.img", true); !strings.HasSuffix(r, " restore") {
		t.Errorf("restore command = %s", r)
	}
	// A path with a space must survive being pasted into a terminal.
	q := RootAVDCommand("/cache/with space", "sys/ramdisk.img", false)
	if !strings.Contains(q, "'/cache/with space/rootAVD.sh'") {
		t.Errorf("script path must be quoted: %s", q)
	}
}

// ─── archive handling ──────────────────────────────────────────────────────

func makeTarGz(t *testing.T, entries map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractRootAVDStripsTheWrapperDirectory(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"rootAVD-abc123/rootAVD.sh": "#!/bin/bash\n",
		"rootAVD-abc123/Magisk.zip": "PK\x03\x04",
		"rootAVD-abc123/Apps/keep":  "",
		"rootAVD-abc123/CHANGES.md": "notes",
	})
	dst := t.TempDir()
	if err := extractRootAVD(tgz, dst); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"rootAVD.sh", "Magisk.zip", filepath.Join("Apps", "keep")} {
		if _, err := os.Stat(filepath.Join(dst, f)); err != nil {
			t.Errorf("%s was not extracted: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "rootAVD-abc123")); err == nil {
		t.Error("the GitLab wrapper directory must be stripped")
	}
}

// An archive entry that escapes the destination must abort the whole extract,
// not merely be skipped: a tree we cannot trust must not be left half-written.
func TestExtractRootAVDRejectsPathTraversal(t *testing.T) {
	for _, evil := range []string{"rootAVD-abc/../../../etc/passwd", "rootAVD-abc/../outside"} {
		tgz := makeTarGz(t, map[string]string{evil: "x"})
		dst := t.TempDir()
		if err := extractRootAVD(tgz, dst); err == nil {
			t.Errorf("%q must be refused", evil)
		}
	}
}

func TestSafeJoin(t *testing.T) {
	base := filepath.Join(string(os.PathSeparator), "tmp", "base")
	if _, ok := safeJoin(base, "a/b.txt"); !ok {
		t.Error("an ordinary relative path must be accepted")
	}
	for _, bad := range []string{"../escape", "a/../../escape", "..", "/absolute"} {
		if _, ok := safeJoin(base, bad); ok {
			t.Errorf("safeJoin must refuse %q", bad)
		}
	}
}

func TestStripTopDir(t *testing.T) {
	cases := map[string]string{
		"rootAVD-abc/rootAVD.sh": "rootAVD.sh",
		"./rootAVD-abc/a/b":      "a/b",
		"rootAVD-abc/":           "",
		"toplevelfile":           "",
	}
	for in, want := range cases {
		if got := stripTopDir(in); got != want {
			t.Errorf("stripTopDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// A tree whose script does not match the pinned hash is not the pinned commit,
// and must never be left on disk where a later run could execute it.
func TestVerifyRootAVDTreeRejectsUnpinnedContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rootAVD.sh"), []byte("#!/bin/bash\necho tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Magisk.zip"), []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := verifyRootAVDTree(dir)
	if err == nil {
		t.Fatal("a tampered script must fail verification")
	}
	if !strings.Contains(err.Error(), "rootAVD.sh") || !strings.Contains(err.Error(), "SHA-256") {
		t.Errorf("the failure must name the file and the check: %v", err)
	}

	// A missing file is a different failure and must say so.
	empty := t.TempDir()
	if err := verifyRootAVDTree(empty); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("a missing script must be reported as incomplete, got %v", err)
	}
}

func TestRootAVDErrorMapping(t *testing.T) {
	cases := []struct{ last, want string }{
		{"[!] No AVD is online", "fully booted"},
		{"[!] Ramdisk.img uses UNKNOWN compression", "compression"},
		{"curl: (6) Could not resolve host: raw.githubusercontent.com", "download Magisk"},
		{"cp: /sdk/system-images/.../ramdisk.img: Permission denied", "permissions"},
	}
	for _, tc := range cases {
		got := rootAVDError(tc.last, errors.New("exit status 1"))
		if !strings.Contains(got, tc.want) {
			t.Errorf("rootAVDError(%q) = %q, want it to mention %q", tc.last, got, tc.want)
		}
	}
	if got := rootAVDError("", errors.New("boom")); !strings.Contains(got, "boom") {
		t.Errorf("with no output, the exec error must survive: %q", got)
	}
}
