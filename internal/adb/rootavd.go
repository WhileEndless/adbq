package adb

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// rootAVD is a third-party GPL-3.0 shell script that installs Magisk into an
// emulator system image's ramdisk. adbq never vendors it and never links it:
// it is fetched, with the user's explicit consent, from a pinned commit, and
// lives in the disposable cache alongside the frida runtimes.
//
// Pinning is what makes running someone else's shell script defensible:
//
//   - the URL names one immutable commit, not a branch,
//   - rootAVD.sh — the only file adbq executes — is checked against a hash
//     recorded here, and a mismatch aborts and deletes the download,
//   - Magisk.zip, the only binary that can reach the device from this tree, is
//     checked the same way,
//   - the download host is allowlisted to gitlab.com.
//
// The archive's own digest is deliberately not enforced: GitLab regenerates
// tarballs on demand and the gzip framing is not byte-stable, so pinning it
// would fail for reasons that have nothing to do with integrity. The file
// hashes inside are stable and are what actually matter.
//
// What adbq cannot verify, and therefore states plainly in the consent dialog:
// while running, rootAVD downloads a Magisk build from
// raw.githubusercontent.com/topjohnwu/magisk-files. That fetch is the script's,
// not adbq's.
const (
	rootAVDCommit    = "613caa44371f85e1a461bc030e07ddc2d71afe32"
	rootAVDArchive   = "https://gitlab.com/newbit/rootAVD/-/archive/" + rootAVDCommit + "/rootAVD-" + rootAVDCommit + ".tar.gz"
	rootAVDProject   = "https://gitlab.com/newbit/rootAVD"
	rootAVDLicense   = "GPL-3.0"
	rootAVDScriptSHA = "f69e5524b3fab04abd9ad8a1a6a9053e3b4244228b4194379069ed0b7c9df036"
	rootAVDMagiskSHA = "543a96fe26c012d99baf3a3aa5a97b80508d67cc641af7c12ce9f7b226b2b889"

	// rootAVDMaxArchive bounds the extraction, so a malicious or corrupt
	// archive cannot fill the disk.
	rootAVDMaxArchive = 64 << 20
	rootAVDMaxEntry   = 32 << 20

	rootAVDRunTimeout = 20 * time.Minute
)

// RootAVDInfo is what the UI needs to describe the tool and its provenance
// before asking the user whether to download it.
type RootAVDInfo struct {
	Installed bool   `json:"installed"`
	Dir       string `json:"dir"`
	Script    string `json:"script"`
	Commit    string `json:"commit"`
	Source    string `json:"source"`
	Archive   string `json:"archive"`
	License   string `json:"license"`
	ScriptSHA string `json:"scriptSHA"`
	MagiskSHA string `json:"magiskSHA"`
	// Runner is the bash adbq would execute the script with, and RunnerNote says
	// why there isn't one. rootAVD is a bash script: without a shell to run it
	// the download is wasted effort, so the UI needs to know before it offers.
	Runner     string `json:"runner"`
	RunnerNote string `json:"runnerNote"`
	// Disclosures are the facts the user must see before consenting. They are
	// produced here so the dialog cannot quietly drop one.
	Disclosures []string `json:"disclosures"`
}

// rootAVDDir is the cache location for the extracted tool. Disposable — the
// user can delete it at any time and adbq re-fetches on demand.
func rootAVDDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, "adbq", "rootavd", rootAVDCommit)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

func rootAVDScriptPath() (string, error) {
	d, err := rootAVDDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "rootAVD.sh"), nil
}

// RootAVDStatus describes the tool and whether it is present locally.
func RootAVDStatus() RootAVDInfo {
	info := RootAVDInfo{
		Commit:    rootAVDCommit,
		Source:    rootAVDProject,
		Archive:   rootAVDArchive,
		License:   rootAVDLicense,
		ScriptSHA: rootAVDScriptSHA,
		MagiskSHA: rootAVDMagiskSHA,
		Disclosures: []string{
			"rootAVD is a third-party tool (" + rootAVDLicense + "), not part of adbq. It is downloaded from " + rootAVDProject + " at commit " + rootAVDCommit[:12] + " and verified by SHA-256 before anything runs.",
			"It patches the system image's ramdisk on this computer, not the AVD. Every AVD using the same system image is affected.",
			"A ramdisk backup is written next to the image, and Restore puts the original back.",
			"While running, rootAVD downloads a Magisk build from GitHub. adbq cannot verify that download — it is made by the script itself.",
			"The AVD is shut down and cold-booted at the end; unsaved state in the running emulator is lost.",
		},
	}
	info.Runner, info.RunnerNote = rootAVDRunner()
	if d, err := rootAVDDir(); err == nil {
		info.Dir = d
		script := filepath.Join(d, "rootAVD.sh")
		if fi, err := os.Stat(script); err == nil && fi.Size() > 0 {
			info.Installed = sumFile(script) == rootAVDScriptSHA
			info.Script = script
		}
	}
	return info
}

// rootAVDRunner resolves the bash that will run the script, or explains its
// absence. Every Unix has one; Windows does not, and "exec: bash: not found"
// two minutes into a download is not an answer anybody can act on.
func rootAVDRunner() (bin, note string) {
	if p, ok := lookTool("bash"); ok {
		return p, ""
	}
	if runtime.GOOS == "windows" {
		return "", "rootAVD is a bash script and Windows has no bash. Install Git for Windows (which ships one) or run adbq from WSL, then restart adbq."
	}
	return "", "rootAVD is a bash script and no bash was found on this computer."
}

// InstallRootAVD downloads and verifies the pinned rootAVD tree. The caller is
// responsible for having obtained the user's consent first — this function does
// the fetching, not the asking.
func InstallRootAVD(ctx context.Context, onStage func(string)) (RootAVDInfo, error) {
	if onStage == nil {
		onStage = func(string) {}
	}
	if s := RootAVDStatus(); s.Installed {
		return s, nil
	}
	dir, err := rootAVDDir()
	if err != nil {
		return RootAVDInfo{}, err
	}

	onStage("downloading rootAVD " + rootAVDCommit[:12])
	tarPath := filepath.Join(dir, "rootAVD.tar.gz")
	// No digest is passed: see the note on rootAVDArchive. The files extracted
	// from it are verified individually below, which is the check that counts.
	if err := downloadVerifiedAsset(ctx, rootAVDArchive, "", tarPath, []string{"https://gitlab.com/"}); err != nil {
		return RootAVDInfo{}, fmt.Errorf("could not download rootAVD: %w", err)
	}
	defer os.Remove(tarPath)

	onStage("extracting")
	if err := extractRootAVD(tarPath, dir); err != nil {
		_ = os.RemoveAll(dir)
		return RootAVDInfo{}, err
	}

	onStage("verifying")
	if err := verifyRootAVDTree(dir); err != nil {
		// A tree we cannot vouch for must not be left behind where a later run
		// might pick it up.
		_ = os.RemoveAll(dir)
		return RootAVDInfo{}, err
	}
	if err := os.Chmod(filepath.Join(dir, "rootAVD.sh"), 0o755); err != nil {
		return RootAVDInfo{}, err
	}
	return RootAVDStatus(), nil
}

// RemoveRootAVD deletes the downloaded tool.
func RemoveRootAVD() error {
	base, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(base, "adbq", "rootavd"))
}

// verifyRootAVDTree checks the two files that can act: the script adbq
// executes, and the Magisk archive that can reach the device.
func verifyRootAVDTree(dir string) error {
	for _, f := range []struct{ name, want string }{
		{"rootAVD.sh", rootAVDScriptSHA},
		{"Magisk.zip", rootAVDMagiskSHA},
	} {
		p := filepath.Join(dir, f.name)
		got := sumFile(p)
		if got == "" {
			return fmt.Errorf("rootAVD download is incomplete: %s is missing", f.name)
		}
		if got != f.want {
			return fmt.Errorf("rootAVD verification failed: %s has SHA-256 %s but %s was expected — the download was not the pinned commit and has been discarded",
				f.name, got, f.want)
		}
	}
	return nil
}

// extractRootAVD unpacks the archive, stripping the single top-level directory
// GitLab wraps around it. Entry names are flattened against traversal and the
// total size is bounded.
func extractRootAVD(tarPath, dst string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(io.LimitReader(f, rootAVDMaxArchive))
	if err != nil {
		return fmt.Errorf("rootAVD archive is not valid gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("rootAVD archive is corrupt: %w", err)
		}
		rel := stripTopDir(hdr.Name)
		if rel == "" {
			continue
		}
		target, ok := safeJoin(dst, rel)
		if !ok {
			// A path escaping the destination is the classic zip-slip; refuse
			// the whole archive rather than skipping the entry.
			return fmt.Errorf("rootAVD archive contains an unsafe path: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if hdr.Size > rootAVDMaxEntry {
				return fmt.Errorf("rootAVD archive entry %s is implausibly large", hdr.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, io.LimitReader(tr, rootAVDMaxEntry)); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			// Symlinks and device nodes have no business in this tree.
			continue
		}
	}
	return nil
}

// stripTopDir removes the "rootAVD-<commit>/" wrapper GitLab adds.
func stripTopDir(name string) string {
	name = filepath.ToSlash(strings.TrimPrefix(name, "./"))
	if i := strings.Index(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return ""
}

// safeJoin resolves rel under base, refusing anything that escapes it.
func safeJoin(base, rel string) (string, bool) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", false
	}
	target := filepath.Join(base, clean)
	if !strings.HasPrefix(target, filepath.Clean(base)+string(os.PathSeparator)) {
		return "", false
	}
	return target, true
}

// ─── running ───────────────────────────────────────────────────────────────

// RootAVDCommand renders the command adbq will run, for the confirm dialog.
func RootAVDCommand(scriptDir, ramdiskRel string, restore bool) string {
	script := filepath.Join(scriptDir, "rootAVD.sh")
	cmd := "bash " + quoteArg(script) + " " + quoteArg(ramdiskRel)
	if restore {
		cmd += " restore"
	}
	return cmd
}

// RootAVD patches an AVD's system image to install Magisk, then cold-boots it
// and confirms root actually works.
//
// The AVD must be running: rootAVD pushes to and pulls from the live device.
func (m *EmulatorManager) RootAVD(ctx context.Context, name string, onStage func(string)) error {
	return m.runRootAVD(ctx, name, false, onStage)
}

// RestoreAVDRamdisk reverts a rootAVD patch using the backup it left behind.
func (m *EmulatorManager) RestoreAVDRamdisk(ctx context.Context, name string, onStage func(string)) error {
	return m.runRootAVD(ctx, name, true, onStage)
}

func (m *EmulatorManager) runRootAVD(ctx context.Context, name string, restore bool, onStage func(string)) error {
	if onStage == nil {
		onStage = func(string) {}
	}
	status := RootAVDStatus()
	if !status.Installed {
		return fmt.Errorf("rootAVD is not downloaded yet — download it from the Root tab first")
	}
	if status.Runner == "" {
		return fmt.Errorf("%s", status.RunnerNote)
	}

	avd, err := m.AVDByName(ctx, name)
	if err != nil {
		return err
	}
	if avd.RamdiskRel == "" {
		return fmt.Errorf("cannot locate the system image for %s — reinstall it from the System images tab", name)
	}
	if _, err := os.Stat(filepath.Join(avd.SysImgDir, "ramdisk.img")); err != nil {
		return fmt.Errorf("no ramdisk.img in %s — the system image looks incomplete", avd.SysImgDir)
	}
	if restore && !avd.Patched {
		return fmt.Errorf("%s has no ramdisk backup to restore", name)
	}

	// rootAVD talks to a live device, so make sure there is one.
	serial := avd.Serial
	if avd.State != AVDRunning {
		onStage("starting " + name)
		serial, err = m.Start(ctx, name, EmulatorOpts{})
		if err != nil {
			return err
		}
		if err := m.WaitForBoot(ctx, serial, onStage); err != nil {
			return err
		}
	}

	log := m.logFor(name)
	cmdLine := RootAVDCommand(status.Dir, avd.RamdiskRel, restore)
	log.Append("$ "+cmdLine, false)
	onStage("running rootAVD")

	if err := m.execRootAVD(ctx, status.Dir, avd.RamdiskRel, restore, log); err != nil {
		return fmt.Errorf("%s", rootAVDError(log.LastMeaningful(), err))
	}

	// The patch only takes effect on a cold boot, and the script itself only
	// asks the user to do it. adbq does it.
	onStage("cold-booting " + name)
	_ = m.Stop(ctx, name)
	time.Sleep(5 * time.Second)
	serial, err = m.Start(ctx, name, EmulatorOpts{ColdBoot: true})
	if err != nil {
		return fmt.Errorf("patched, but the AVD did not restart: %w", err)
	}
	if err := m.WaitForBoot(ctx, serial, onStage); err != nil {
		return fmt.Errorf("patched, but the AVD did not finish booting: %w", err)
	}

	// Root state is cached per serial; without this the app would keep
	// reporting the pre-patch answer.
	m.client.ForgetRootProbe(serial)
	if restore {
		onStage("restored")
		return nil
	}

	onStage("verifying root")
	if kind := m.rootKind(ctx, serial); kind == "no" {
		return fmt.Errorf("rootAVD finished but %s still has no root — see the rootAVD log; some images need the FAKEBOOTIMG path, which requires patching from the Magisk app inside the emulator", name)
	}
	onStage("rooted")
	return nil
}

// execRootAVD runs the script with its prompts answered.
//
// rootAVD asks which Magisk build to install with `read -t 10`, defaulting to
// the first (stable) entry when the read times out. Feeding "1" answers it
// immediately instead of costing ten seconds of dead time, and keeps the choice
// explicit rather than depending on a timeout default.
func (m *EmulatorManager) execRootAVD(ctx context.Context, dir, ramdiskRel string, restore bool, log *hostLog) error {
	args := []string{"rootAVD.sh", ramdiskRel}
	if restore {
		args = append(args, "restore")
	}
	rctx, cancel := context.WithTimeout(ctx, rootAVDRunTimeout)
	defer cancel()

	runner, note := rootAVDRunner()
	if runner == "" {
		return fmt.Errorf("%s", note)
	}
	cmd := exec.CommandContext(rctx, runner, args...)
	cmd.Dir = dir
	// The script finds the SDK through ANDROID_HOME; launched from Finder we
	// may have inherited none, so it is set explicitly.
	cmd.Env = emulatorEnv(m.sdk.Info())
	cmd.Stdin = strings.NewReader("1\n")

	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return err
	}
	pw.Close()

	sc := LineScanner(pr)
	for sc.Scan() {
		log.Append(sc.Text(), false)
	}
	pr.Close()
	return cmd.Wait()
}

// rootAVDError turns the script's last line into an explanation.
func rootAVDError(lastLine string, err error) string {
	low := strings.ToLower(lastLine)
	switch {
	case strings.Contains(low, "no avd"), strings.Contains(low, "not online"), strings.Contains(low, "no device"):
		return "rootAVD could not reach the emulator — make sure it is fully booted and adb can see it"
	case strings.Contains(low, "unknown compression"):
		return "rootAVD does not understand this ramdisk's compression — this system image is not one it supports"
	case strings.Contains(low, "not found") && strings.Contains(low, "ramdisk"):
		return "the system image has no ramdisk.img where rootAVD expects it"
	case strings.Contains(low, "curl"), strings.Contains(low, "wget"), strings.Contains(low, "could not resolve"):
		return "rootAVD could not download Magisk — check the network and try again"
	case strings.Contains(low, "permission denied"):
		return "rootAVD could not write to the system image directory — check the permissions on the Android SDK folder"
	}
	if lastLine != "" {
		return "rootAVD failed: " + lastLine
	}
	return "rootAVD failed: " + err.Error()
}
