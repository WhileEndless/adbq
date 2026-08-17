package adb

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseJavaVersion(t *testing.T) {
	cases := []struct {
		out   string
		want  string
		major int
	}{
		{"openjdk version \"17.0.9\" 2023-10-17\nOpenJDK Runtime Environment\n", "17.0.9", 17},
		{"java version \"1.8.0_301\"\nJava(TM) SE Runtime Environment\n", "1.8.0_301", 8},
		{"openjdk version \"21\" 2023-09-19\n", "21", 21},
		{"openjdk version \"11.0.20.1\" 2023-08-24\n", "11.0.20.1", 11},
		{"", "", 0},
		{"command not found\n", "", 0},
	}
	for _, c := range cases {
		v, major := parseJavaVersion(c.out)
		if v != c.want || major != c.major {
			t.Errorf("parseJavaVersion(%q) = (%q, %d), want (%q, %d)", c.out, v, major, c.want, c.major)
		}
	}
}

// Java 8 reports itself as 1.8, and reading only the leading number would call
// it version 1 — older than nothing, and so silently accepted.
func TestParseJavaVersionRejectsJava8(t *testing.T) {
	if _, major := parseJavaVersion("java version \"1.8.0_301\"\n"); major >= jadxMinJava {
		t.Errorf("Java 8 parsed as %d, which passes the minimum of %d", major, jadxMinJava)
	}
}

func TestStudioJavaCandidatesFollowThePlatformLayout(t *testing.T) {
	if got := studioJavaCandidates(""); got != nil {
		t.Errorf("no Studio means no candidates, got %v", got)
	}
	var studio, want string
	switch runtime.GOOS {
	case "darwin":
		studio = "/Applications/Android Studio.app"
		want = "/Applications/Android Studio.app/Contents/jbr/Contents/Home/bin/java"
	case "windows":
		studio = `C:\Program Files\Android\Android Studio\bin\studio64.exe`
		want = `C:\Program Files\Android\Android Studio\jbr\bin\java.exe`
	default:
		studio = "/opt/android-studio/bin/studio.sh"
		want = "/opt/android-studio/jbr/bin/java"
	}
	got := studioJavaCandidates(studio)
	if len(got) == 0 || got[0] != want {
		t.Errorf("studioJavaCandidates(%q) = %v, want it to start with %q", studio, got, want)
	}
}

func TestJadxCommandQuotesEveryPath(t *testing.T) {
	cmd := JadxCommand("", "/opt/jadx/bin/jadx-gui", []string{"/tmp/a b/base.apk", "/tmp/a b/split_x.apk"})
	if !strings.Contains(cmd, "'/tmp/a b/base.apk'") || !strings.Contains(cmd, "'/tmp/a b/split_x.apk'") {
		t.Errorf("paths with spaces must be quoted so the line can be pasted: %s", cmd)
	}
	if strings.Contains(cmd, "JAVA_HOME") {
		t.Errorf("without a resolved Java home the command must not claim one: %s", cmd)
	}
	withJava := JadxCommand("/opt/jdk-17/bin/java", "/opt/jadx/bin/jadx-gui", []string{"/tmp/base.apk"})
	if !strings.HasPrefix(withJava, "JAVA_HOME=/opt/jdk-17 ") {
		t.Errorf("a resolved Java must appear as the JAVA_HOME the launch actually uses: %s", withJava)
	}
	// The order matters: jadx merges inputs, and the base APK carries the
	// manifest and the resource table the splits refer back to.
	if i, j := strings.Index(cmd, "base.apk"), strings.Index(cmd, "split_x.apk"); i > j {
		t.Errorf("base must precede the splits: %s", cmd)
	}
}

func TestJadxStatusDisclosesTheProvenance(t *testing.T) {
	info := JadxStatus(HostSettings{}, "")
	if len(info.Disclosures) == 0 {
		t.Fatal("consent needs disclosures")
	}
	all := strings.Join(info.Disclosures, "\n")
	for _, must := range []string{jadxLicense, jadxVersion, "SHA-256", "Java", "cache"} {
		if !strings.Contains(all, must) {
			t.Errorf("disclosures must mention %q:\n%s", must, all)
		}
	}
	if info.SHA256 == "" || info.Asset == "" || info.Source == "" {
		t.Error("the dialog cannot describe the download without asset, source and digest")
	}
}

// A user-set path is the escape hatch for a manually updated jadx, so it has to
// win over anything adbq found by itself.
func TestJadxStatusPrefersTheUserPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("launcher name differs on windows; covered by resolveJadxUserPath")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin", "jadx-gui")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{bin, dir, filepath.Dir(bin)} {
		info := JadxStatus(HostSettings{JadxPath: in}, "")
		if !info.Installed || info.Bin != bin {
			t.Errorf("JadxStatus with JadxPath=%q resolved to %q (installed=%v), want %q", in, info.Bin, info.Installed, bin)
		}
		if info.Kind != "external" {
			t.Errorf("a user-managed install must be reported as external, got %q", info.Kind)
		}
	}
	if info := JadxStatus(HostSettings{JadxPath: filepath.Join(dir, "nope")}, ""); info.Bin == filepath.Join(dir, "nope") {
		t.Error("a path that does not exist must not be reported as the resolved binary")
	}
}

func TestJadxReleaseFromFeedPicksTheVerifiableArchive(t *testing.T) {
	assets := []jadxFeedAsset{
		{Name: "jadx-gui-9.9.9-win.zip", Digest: "sha256:" + strings.Repeat("b", 64), URL: "https://github.com/x/gui.zip"},
		{Name: "jadx-9.9.9.zip", Size: 42, Digest: "sha256:" + strings.Repeat("a", 64), URL: "https://github.com/x/jadx-9.9.9.zip"},
	}
	rel, err := jadxReleaseFromFeed("v9.9.9", "2026-01-01T00:00:00Z", assets)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "9.9.9" || rel.Asset != "https://github.com/x/jadx-9.9.9.zip" {
		t.Errorf("wrong asset chosen: %+v", rel)
	}
	if rel.SHA256 != strings.Repeat("a", 64) {
		t.Errorf("the sha256: prefix must be stripped, got %q", rel.SHA256)
	}
	if !rel.Newer {
		t.Error("a higher version than the pin must be reported as newer")
	}
	if _, err := jadxReleaseFromFeed("v9.9.9", "", assets[:1]); err == nil {
		t.Error("a feed without the cross-platform archive must be an error, not a silent miss")
	}
	if _, err := jadxReleaseFromFeed("", "", assets); err == nil {
		t.Error("a feed without a version must be an error")
	}
}

// An asset whose digest adbq cannot check must not be installable: the whole
// point of the manual-update path is that it stays verifiable.
func TestInstallJadxReleaseRefusesAnUnverifiableAsset(t *testing.T) {
	err := InstallJadxRelease(context.Background(), JadxRelease{Version: "9.9.9", Asset: jadxAsset}, nil)
	if err == nil || !strings.Contains(err.Error(), "verify") {
		t.Errorf("want a refusal mentioning verification, got %v", err)
	}
	err = InstallJadxRelease(context.Background(), JadxRelease{Version: "../evil", Asset: jadxAsset, SHA256: strings.Repeat("a", 64)}, nil)
	if err == nil {
		t.Error("a version that is not a plain path segment must be refused")
	}
}

func TestExtractJadxStripsTheWrapperDirectory(t *testing.T) {
	src := filepath.Join(t.TempDir(), "jadx.zip")
	writeTestZip(t, src, map[string]string{
		"jadx-1.2.3/bin/jadx-gui": "#!/bin/sh\n",
		"jadx-1.2.3/lib/jadx.jar": "jar",
	})
	dst := t.TempDir()
	if err := extractJadx(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "bin", "jadx-gui")); err != nil {
		t.Fatalf("the wrapper directory should have been stripped: %v", err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(dst, "bin", "jadx-gui"))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode()&0o111 == 0 {
			t.Error("the launcher must stay executable")
		}
	}
}

func TestExtractJadxRejectsPathTraversal(t *testing.T) {
	src := filepath.Join(t.TempDir(), "evil.zip")
	writeTestZip(t, src, map[string]string{
		"jadx-1.2.3/bin/jadx-gui":  "#!/bin/sh\n",
		"jadx-1.2.3/../../escaped": "x",
	})
	dst := t.TempDir()
	if err := extractJadx(src, dst); err == nil {
		t.Fatal("an entry escaping the destination must reject the whole archive")
	}
}

func TestZipCommonPrefixOnlyStripsASingleRoot(t *testing.T) {
	flat := filepath.Join(t.TempDir(), "flat.zip")
	writeTestZip(t, flat, map[string]string{"bin/jadx-gui": "x", "lib/a.jar": "y"})
	zr, err := zip.OpenReader(flat)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if got := zipCommonPrefix(zr); got != "" {
		t.Errorf("an archive with two roots has no wrapper to strip, got %q", got)
	}
}

func writeTestZip(t *testing.T, dst string, files map[string]string) {
	t.Helper()
	f, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o755)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// The macOS stub at /usr/bin/java answers `-version` whenever some JDK is
// registered, so probing alone accepts it — and then it hangs as a child of a
// GUI app with no console instead of failing, which is how a launch reported
// success while no window ever appeared. Only a real Java home is accepted.
func TestIsJavaHomeRejectsASystemPrefix(t *testing.T) {
	if isJavaHome("/usr") {
		t.Error("/usr is a system prefix, not a Java home")
	}
	if isJavaHome("") || isJavaHome(string(filepath.Separator)) {
		t.Error("an empty path and the filesystem root are never Java homes")
	}

	// A directory that looks like a real runtime: bin/java plus the release
	// file every JDK since 9 ships.
	home := t.TempDir()
	bin := filepath.Join(home, "bin", javaExe())
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if isJavaHome(home) {
		t.Error("bin/java alone does not make a Java home — that is exactly the stub's shape")
	}
	if err := os.WriteFile(filepath.Join(home, "release"), []byte("JAVA_VERSION=\"17\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isJavaHome(home) {
		t.Error("a directory with bin/java and release is a Java home")
	}
}

func TestResolveJavaRejectsAJavalessSetting(t *testing.T) {
	// A user-set path that is not a runtime must not be accepted just because
	// the user typed it.
	dir := t.TempDir()
	if bin, _, _, err := resolveJava(filepath.Join(dir, "nope"), ""); err == nil && bin == filepath.Join(dir, "nope") {
		t.Error("a path that does not exist must not be used as the runtime")
	}
}

func TestInstalledJavaCandidatesArePlausible(t *testing.T) {
	// The list is generic — it must not be empty of shape, and every entry must
	// end in the platform's java executable.
	for _, p := range installedJavaCandidates() {
		if filepath.Base(p) != javaExe() {
			t.Errorf("candidate %q does not end in %q", p, javaExe())
		}
	}
}
