package adb

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// An install carries more than code: the native libraries a cross-platform
// toolchain compiles the whole app into, and the data blobs those libraries load
// at startup. Pulling the APK gets you the container; getting at these means
// unpacking every part of a split install by hand.
//
// Which entries count is decided by looking at them rather than by trusting
// their names. Shipped executables routinely arrive as `.so` (the only extension
// the packaging tools let through), as `.bin`, or with no extension at all, so a
// suffix list would miss exactly the files worth having.

// Magic numbers of the two executable formats worth collecting: ELF for
// anything the device runs, PE for managed assemblies that ship alongside.
var (
	elfMagic = []byte{0x7f, 'E', 'L', 'F'}
	peMagic  = []byte{'M', 'Z'}
)

// knownBlobs are data files that are not executables but are the payload of the
// runtime that loads them — without these the collected libraries are inert.
var knownBlobs = map[string]bool{
	"kernel_blob.bin":        true, // Dart kernel, debug/JIT builds
	"isolate_snapshot_data":  true,
	"vm_snapshot_data":       true,
	"isolate_snapshot_instr": true,
	"vm_snapshot_instr":      true,
	"global-metadata.dat":    true, // IL2CPP metadata
}

// binaryHeadBytes is how much of an entry is read to identify it. Every magic
// number of interest is in the first few bytes.
const binaryHeadBytes = 8

// apkSource pairs a staged APK's path with the name it is filed under in the
// archive and the manifest.
type apkSource struct{ path, source string }

// BinaryEntry is one collected file.
type BinaryEntry struct {
	Source string `json:"source"` // the APK it came from, or "device-lib"
	Path   string `json:"path"`   // path inside that source
	Kind   string `json:"kind"`   // "so" | "elf" | "pe" | "blob"
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// BinaryPlan is what a collection would do, before it does it.
type BinaryPlan struct {
	Pkg       string   `json:"pkg"`
	Suggested string   `json:"suggested"`
	Sources   int      `json:"sources"` // APKs that will be inspected
	Commands  []string `json:"commands"`
}

// PlanAppBinaries renders the collection's plan. Pure: it describes the work
// from the APK layout alone, so the preview costs no extra device round trip.
func PlanAppBinaries(serial string, set *ApkSet) *BinaryPlan {
	plan := &BinaryPlan{
		Pkg:       set.Pkg,
		Suggested: ExportBaseName(set.Pkg, set.Version) + "-binaries.zip",
		Sources:   1 + len(set.Splits),
	}
	plan.Commands = append(plan.Commands, "adb -s "+serial+" shell pm path "+set.Pkg)
	for _, p := range append([]string{set.Base}, set.Splits...) {
		plan.Commands = append(plan.Commands, "adb -s "+serial+" pull "+p+" ./"+path.Base(p))
	}
	plan.Commands = append(plan.Commands,
		"adb -s "+serial+" pull "+path.Join(path.Dir(set.Base), "lib")+" ./device-lib",
		"# adbq collects lib/** plus every ELF/PE entry and known runtime blob from the "+
			strconv.Itoa(plan.Sources)+" APK(s) into "+plan.Suggested)
	return plan
}

// classifyBinaryEntry decides what an archive entry is, from its name and the
// first bytes of its content. "" means "not a binary worth collecting".
func classifyBinaryEntry(name string, head []byte) string {
	slash := strings.ReplaceAll(name, `\`, "/")
	base := path.Base(slash)
	switch {
	case strings.HasPrefix(slash, "lib/") && strings.HasSuffix(strings.ToLower(base), ".so"):
		return "so"
	case hasPrefixBytes(head, elfMagic):
		return "elf"
	case hasPrefixBytes(head, peMagic):
		return "pe"
	case knownBlobs[base]:
		return "blob"
	}
	return ""
}

func hasPrefixBytes(b, prefix []byte) bool {
	if len(b) < len(prefix) {
		return false
	}
	for i := range prefix {
		if b[i] != prefix[i] {
			return false
		}
	}
	return true
}

// skipBinaryScan reports entries there is no point reading: they are either
// already covered by the APK export or known not to be binaries. Skipping them
// keeps the scan from opening every drawable in the app.
func skipBinaryScan(name string) bool {
	slash := strings.ReplaceAll(name, `\`, "/")
	lower := strings.ToLower(slash)
	switch {
	case strings.HasPrefix(lower, "res/"), strings.HasPrefix(lower, "meta-inf/"):
		return true
	case lower == "androidmanifest.xml", lower == "resources.arsc":
		return true
	case strings.HasPrefix(lower, "classes") && strings.HasSuffix(lower, ".dex"):
		return true
	}
	return false
}

// scanApkBinaries lists the binaries inside one APK. Entries are read only far
// enough to identify them.
func scanApkBinaries(apkPath, source string) ([]BinaryEntry, error) {
	zr, err := zip.OpenReader(apkPath)
	if err != nil {
		return nil, fmt.Errorf("%s is not a readable APK: %w", source, err)
	}
	defer zr.Close()

	var out []BinaryEntry
	for _, e := range zr.File {
		if e.FileInfo().IsDir() || skipBinaryScan(e.Name) {
			continue
		}
		head, err := readZipHead(e, binaryHeadBytes)
		if err != nil {
			// One unreadable entry is not worth abandoning the whole app for.
			continue
		}
		kind := classifyBinaryEntry(e.Name, head)
		if kind == "" {
			continue
		}
		out = append(out, BinaryEntry{
			Source: source,
			Path:   filepath.ToSlash(e.Name),
			Kind:   kind,
			Size:   int64(e.UncompressedSize64),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func readZipHead(e *zip.File, n int) ([]byte, error) {
	rc, err := e.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	buf := make([]byte, n)
	read, err := io.ReadFull(rc, buf)
	if err != nil && read == 0 {
		return nil, err
	}
	return buf[:read], nil
}

// binariesManifest is written into the archive so the collection is
// self-describing: where each file came from, how large it is and what it
// hashes to.
type binariesManifest struct {
	Package     string        `json:"package"`
	VersionName string        `json:"versionName,omitempty"`
	VersionCode string        `json:"versionCode,omitempty"`
	Entries     []BinaryEntry `json:"entries"`
	Notes       []string      `json:"notes"`
}

// ExportAppBinaries collects the app's native and managed binaries into one
// zip.
//
// Both places they can live are covered. Inside the APKs is where they are when
// the app opts out of extraction — which is the common case for cross-platform
// toolchains, and precisely when unpacking by hand is most tedious. The
// directory the platform extracts them to is covered as well, because that is
// where they are for everything else.
func (c *Client) ExportAppBinaries(ctx context.Context, serial, pkg, dst string, progress func(string)) (string, error) {
	note := func(s string) {
		if progress != nil {
			progress(s)
		}
	}
	staged, err := c.StageApks(ctx, serial, pkg, progress)
	if err != nil {
		return "", err
	}

	manifest := binariesManifest{Package: pkg, Entries: []BinaryEntry{}, Notes: []string{}}
	if set, err := c.ApkSetOf(ctx, serial, pkg); err == nil {
		manifest.VersionName, manifest.VersionCode = set.Version.Name, set.Version.Code
	}

	var files []apkSource
	var entries []BinaryEntry
	for i, p := range staged.Files {
		source := staged.Names[i]
		note("scanning " + source)
		found, err := scanApkBinaries(p, source)
		if err != nil {
			return "", err
		}
		if len(found) == 0 {
			continue
		}
		entries = append(entries, found...)
		files = append(files, apkSource{p, source})
	}

	// The extracted directory, when the platform made one.
	deviceLib, libNote := c.pullDeviceLibs(ctx, serial, pkg, staged.Dir, note)
	if libNote != "" {
		manifest.Notes = append(manifest.Notes, libNote)
	}

	note("packing " + filepath.Base(dst))
	written, err := writeBinariesArchive(dst, files, deviceLib, entries, &manifest)
	if err != nil {
		return "", err
	}
	if written == 0 {
		note("no binaries found in this app")
	} else {
		note(strconv.Itoa(written) + " file(s) collected")
	}
	return dst, nil
}

// pullDeviceLibs copies the directory the platform extracts native libraries
// into, when it exists. Its absence is normal, not a failure: an app that keeps
// its libraries compressed inside the APK never gets one.
func (c *Client) pullDeviceLibs(ctx context.Context, serial, pkg, into string, note func(string)) (string, string) {
	base, err := c.PathOfApp(ctx, serial, pkg)
	if err != nil || base == "" {
		return "", ""
	}
	remote := path.Join(path.Dir(base), "lib")
	local := filepath.Join(into, "device-lib")
	if err := os.RemoveAll(local); err != nil {
		return "", ""
	}
	note("pulling the extracted library directory")
	if out, err := c.pullOne(ctx, serial, remote, local); err != nil {
		// Typically "does not exist" (libraries are inside the APK) or a
		// permission error. Either way the APK scan already covered the code.
		reason := firstLine(out)
		if reason == "" {
			reason = "not present"
		}
		return "", "the extracted library directory (" + remote + ") could not be read: " + reason + " — the libraries inside the APKs were collected instead"
	}
	return local, ""
}

// writeBinariesArchive copies the selected entries into dst, grouped by the APK
// they came from, and records what it wrote in the manifest. Returns how many
// files landed in the archive.
//
// Like the APK export, a failure removes dst: a half-written archive is worse
// than none, because it looks complete.
func writeBinariesArchive(dst string, sources []apkSource, deviceLib string, entries []BinaryEntry, manifest *binariesManifest) (int, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	f, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	zw := zip.NewWriter(f)
	count := 0

	err = func() error {
		wanted := map[string]map[string]*BinaryEntry{}
		for i := range entries {
			e := &entries[i]
			if wanted[e.Source] == nil {
				wanted[e.Source] = map[string]*BinaryEntry{}
			}
			wanted[e.Source][e.Path] = e
		}
		for _, src := range sources {
			zr, err := zip.OpenReader(src.path)
			if err != nil {
				return err
			}
			err = func() error {
				for _, e := range zr.File {
					want := wanted[src.source][filepath.ToSlash(e.Name)]
					if want == nil {
						continue
					}
					rc, err := e.Open()
					if err != nil {
						return err
					}
					sum, err := copyIntoZip(zw, src.source+"/"+filepath.ToSlash(e.Name), rc)
					rc.Close()
					if err != nil {
						return err
					}
					want.SHA256 = sum
					count++
				}
				return nil
			}()
			zr.Close()
			if err != nil {
				return err
			}
		}
		if deviceLib != "" {
			added, err := addDeviceLibs(zw, deviceLib, manifest)
			if err != nil {
				return err
			}
			count += added
		}
		manifest.Entries = append(manifest.Entries, entries...)
		if manifest.Entries == nil {
			manifest.Entries = []BinaryEntry{}
		}
		if count == 0 {
			manifest.Notes = append(manifest.Notes, "this app ships no native libraries, executables or known runtime blobs")
		}
		b, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		_, err = copyIntoZip(zw, "manifest.json", strings.NewReader(string(b)))
		return err
	}()

	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
		return 0, err
	}
	return count, nil
}

// addDeviceLibs walks the pulled directory and adds every file in it. Whatever
// the platform extracted is by definition what the app runs, so nothing here is
// filtered.
func addDeviceLibs(zw *zip.Writer, dir string, manifest *binariesManifest) (int, error) {
	count := 0
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		name := "device-lib/" + filepath.ToSlash(rel)
		sum, err := copyIntoZip(zw, name, src)
		if err != nil {
			return err
		}
		size := int64(0)
		if fi, err := d.Info(); err == nil {
			size = fi.Size()
		}
		manifest.Entries = append(manifest.Entries, BinaryEntry{
			Source: "device-lib",
			Path:   filepath.ToSlash(rel),
			Kind:   "so",
			Size:   size,
			SHA256: sum,
		})
		count++
		return nil
	})
	if err != nil {
		return count, err
	}
	return count, nil
}

// copyIntoZip writes one entry and returns its SHA-256, so the manifest can
// name what is actually in the archive without a second read.
func copyIntoZip(zw *zip.Writer, name string, body io.Reader) (string, error) {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
