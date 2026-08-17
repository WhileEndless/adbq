package adb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// JadxOpenPlan is what opening the package in jadx would do, in full, before
// anything happens: which APKs are copied here, and the exact command line the
// decompiler is launched with (CLAUDE.md §4.1).
type JadxOpenPlan struct {
	Bin      string   `json:"bin"`
	Java     string   `json:"java"`
	Names    []string `json:"names"` // APK file names, base first
	Split    bool     `json:"split"`
	Staged   bool     `json:"staged"` // already on this computer; nothing will be pulled
	Ready    bool     `json:"ready"`  // jadx and a usable Java are both available
	Reason   string   `json:"reason"` // why not, when Ready is false
	Commands []string `json:"commands"`
}

// JadxCommand renders the launch line, for the preview panel and for the log.
//
// Java is passed as JAVA_HOME rather than spliced into the argument list because
// the launcher is a shell script that resolves its own interpreter: naming the
// home is how you tell it which one, and it keeps the rendered line something
// the user can paste verbatim.
func JadxCommand(java, bin string, files []string) string {
	var b strings.Builder
	if home := javaHomeOf(java); home != "" {
		b.WriteString("JAVA_HOME=" + shellQuoteLocal(home) + " ")
	}
	b.WriteString(shellQuoteLocal(bin))
	for _, f := range files {
		b.WriteString(" " + shellQuoteLocal(f))
	}
	return b.String()
}

// PlanJadxOpen describes the open without performing it. It reads the package's
// APK layout from the device and touches the network never.
func (c *Client) PlanJadxOpen(ctx context.Context, serial, pkg string, info JadxInfo) (*JadxOpenPlan, error) {
	set, err := c.ApkSetOf(ctx, serial, pkg)
	if err != nil {
		return nil, err
	}
	remote := append([]string{set.Base}, set.Splits...)

	plan := &JadxOpenPlan{Bin: info.Bin, Java: info.Java, Split: set.Split, Ready: info.Ready}
	switch {
	case !info.Installed:
		plan.Reason = "jadx is not installed yet"
	case info.Java == "":
		plan.Reason = info.JavaError
	}

	// The staging directory is where the files will be, so the preview names the
	// paths the launch will really use.
	var hostPaths []string
	dir := plannedStageDir(pkg, c.stagedVersion(ctx, serial, pkg))
	for _, p := range remote {
		name := uniqueEntryName(path.Base(p), plan.Names)
		plan.Names = append(plan.Names, name)
		hostPaths = append(hostPaths, filepath.Join(dir, name))
	}
	plan.Staged = dir != "" && allFilesPresent(hostPaths)

	plan.Commands = append(plan.Commands, "adb -s "+serial+" shell pm path "+pkg)
	for i, p := range remote {
		plan.Commands = append(plan.Commands, "adb -s "+serial+" pull "+p+" "+shellQuoteLocal(hostPaths[i]))
	}
	bin := info.Bin
	if bin == "" {
		bin = "jadx-gui"
	}
	plan.Commands = append(plan.Commands, JadxCommand(info.Java, bin, hostPaths))
	return plan, nil
}

// stagedVersion reads the version code staging keys on, tolerating failure: a
// preview is worth showing even when the version cannot be read.
func (c *Client) stagedVersion(ctx context.Context, serial, pkg string) string {
	if d, err := c.DescribeApp(ctx, serial, pkg); err == nil && d != nil {
		return d.VersionCode
	}
	return ""
}

func plannedStageDir(pkg, version string) string {
	root, err := stageRoot()
	if err != nil {
		return ""
	}
	return filepath.Join(root, stageKey(pkg, version))
}

func allFilesPresent(paths []string) bool {
	for _, p := range paths {
		if fi, err := os.Stat(p); err != nil || fi.Size() == 0 {
			return false
		}
	}
	return len(paths) > 0
}

// OpenInJadx stages the package's APKs and hands all of them to jadx at once.
//
// Splits are passed as separate inputs rather than as the `.apks` bundle adbq
// exports: jadx takes several input files and merges them, which is the only way
// a bundled app decompiles as one program instead of a handful of fragments —
// and the bundle format itself is not among the inputs it reads.
//
// The process is deliberately not tracked. Unlike a screen mirror, whose window
// adbq owns, the decompiler is the user's session: it must outlive the task that
// started it.
func (c *Client) OpenInJadx(ctx context.Context, serial, pkg string, info JadxInfo, progress func(string)) (string, error) {
	if !info.Installed {
		return "", fmt.Errorf("jadx is not installed yet — download it from Settings first")
	}
	if info.Java == "" {
		if info.JavaError != "" {
			return "", fmt.Errorf("%s", info.JavaError)
		}
		return "", fmt.Errorf("no Java runtime found for jadx")
	}
	staged, err := c.StageApks(ctx, serial, pkg, progress)
	if err != nil {
		return "", err
	}
	if progress != nil {
		progress("opening " + filepath.Base(info.Bin))
	}
	cmd := exec.Command(info.Bin, staged.Files...)
	cmd.Dir = staged.Dir
	cmd.Env = jadxEnv(info.Java)

	// The launcher's output is read, not discarded. A GUI process that dies on
	// startup says why on stderr, and throwing that away is how a launch that
	// produced no window still reported success.
	pr, pw, err := os.Pipe()
	if err != nil {
		return "", err
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return "", fmt.Errorf("could not launch jadx: %w", err)
	}
	// Only the child holds the write end now, so the reader sees EOF when it
	// exits.
	pw.Close()

	var mu sync.Mutex
	var head []string
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		sc := LineScanner(pr)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			mu.Lock()
			if line != "" && len(head) < jadxLaunchLogLines {
				head = append(head, line)
			}
			mu.Unlock()
		}
		pr.Close()
	}()

	// Draining continues for the process's whole life: a full pipe would
	// otherwise block the child once its log outgrew the buffer.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	select {
	case werr := <-exited:
		<-drained
		mu.Lock()
		why := strings.Join(head, "; ")
		mu.Unlock()
		if why == "" {
			why = "no output"
		}
		return "", fmt.Errorf("jadx exited immediately instead of opening a window: %s (%v)", why, werr)
	case <-time.After(jadxLaunchGrace):
		// Still running, which for a GUI process means it is up.
	}
	return staged.Dir, nil
}

const (
	// jadxLaunchGrace is how long a launch is watched before it counts as up.
	// Long enough for a broken invocation to die and be reported, short enough
	// not to make the button feel stuck.
	jadxLaunchGrace = 2 * time.Second
	// jadxLaunchLogLines bounds what is kept from the launcher's output for an
	// error message.
	jadxLaunchLogLines = 6
)

// jadxEnv points the launcher at the Java adbq probed, so the runtime that was
// checked is the runtime that runs. Launched from Finder or the Dock the app
// inherits a minimal PATH, which is exactly when the launcher's own lookup
// fails.
func jadxEnv(java string) []string {
	home := javaHomeOf(java)
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if strings.HasPrefix(e, "JAVA_HOME=") {
			continue
		}
		out = append(out, e)
	}
	if home != "" {
		out = append(out, "JAVA_HOME="+home)
	}
	return out
}
