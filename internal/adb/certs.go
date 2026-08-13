package adb

import (
	"context"
	"crypto/md5"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// CertInstallResult describes how a CA certificate install landed on the device.
type CertInstallResult struct {
	Subject     string `json:"subject"`    // human-readable cert subject
	FileName    string `json:"fileName"`   // on-device filename, e.g. 9a5ba575.0
	Path        string `json:"path"`       // full on-device path (system store) or /sdcard path (user fallback)
	Strategy    string `json:"strategy"`   // direct / magisk-remount / remount-root / remount-system / tmpfs-overlay / user-store
	Persistent  bool   `json:"persistent"` // false for tmpfs-overlay (reset on reboot) and user-store
	Rooted      bool   `json:"rooted"`
	SDK         int    `json:"sdk"`
	Note        string `json:"note"`        // guidance shown to the user
	Diagnostics string `json:"diagnostics"` // transcript for troubleshooting
}

const systemCacertsDir = "/system/etc/security/cacerts"

// CACert is one certificate found in the device trust store.
type CACert struct {
	FileName   string `json:"fileName"`   // e.g. eb1bf87a.0
	Store      string `json:"store"`      // "system" or "apex"
	Subject    string `json:"subject"`    // CN or full subject
	Issuer     string `json:"issuer"`     // issuer CN
	NotAfter   string `json:"notAfter"`   // expiry (YYYY-MM-DD)
	Expired    bool   `json:"expired"`    // already past NotAfter
	SelfSigned bool   `json:"selfSigned"` // subject == issuer (typical for interception CAs)
}

// ListCACerts enumerates the certificates in the device's CA trust store
// (system store, plus the Conscrypt APEX store on Android 14+). Each file is
// read once via a single shell call and parsed host-side, so it works even on
// ROMs missing coreutils. Reading is best-effort and needs no root for the
// world-readable system store; the APEX store is included when present.
func (c *Client) ListCACerts(ctx context.Context, serial string) ([]CACert, error) {
	const marker = "@@@ADBQCERT "
	// `for f in dir/*` + `${f##*/}` are POSIX/mksh; cat is universally present.
	script := `for d in ` + systemCacertsDir + ` /apex/com.android.conscrypt/cacerts; do
[ -d "$d" ] || continue
for f in "$d"/*; do
[ -f "$f" ] || continue
echo "` + marker + `$d ${f##*/}"
cat "$f"
done
done`
	out, err := c.Shell(ctx, serial, script)
	if err != nil && out == "" {
		// Fall back to root in case the store isn't world-readable on this ROM.
		out, _, _ = c.ShellSU(ctx, serial, script)
	}

	var certs []CACert
	var curFile, curStore string
	var buf strings.Builder
	flush := func() {
		if curFile == "" {
			return
		}
		if ca, ok := parseCACertPEM(buf.String()); ok {
			ca.FileName = curFile
			ca.Store = curStore
			certs = append(certs, ca)
		}
		buf.Reset()
	}
	for _, ln := range strings.Split(out, "\n") {
		line := strings.TrimRight(ln, "\r")
		if strings.HasPrefix(line, marker) {
			flush()
			rest := strings.TrimPrefix(line, marker)
			dir, name, _ := strings.Cut(rest, " ")
			curStore = "system"
			if strings.HasPrefix(dir, "/apex") {
				curStore = "apex"
			}
			curFile = strings.TrimSpace(name)
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	flush()
	return certs, nil
}

// parseCACertPEM parses the first CERTIFICATE block from an Android cacerts
// file (PEM, optionally followed by an openssl text dump) into a CACert.
func parseCACertPEM(s string) (CACert, bool) {
	block, _ := pem.Decode([]byte(s))
	if block == nil || block.Type != "CERTIFICATE" {
		return CACert{}, false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return CACert{}, false
	}
	ca := CACert{
		Subject:    subjectString(cert),
		Issuer:     issuerString(cert),
		NotAfter:   cert.NotAfter.Format("2006-01-02"),
		Expired:    nowAfter(cert),
		SelfSigned: cert.Subject.String() == cert.Issuer.String(),
	}
	return ca, true
}

func issuerString(cert *x509.Certificate) string {
	if cert.Issuer.CommonName != "" {
		return cert.Issuer.CommonName
	}
	return cert.Issuer.String()
}

func nowAfter(cert *x509.Certificate) bool {
	return time.Now().After(cert.NotAfter)
}

// InstallSystemCert installs a CA certificate (Burp/ZAP/mitmproxy/Charles export)
// into the device's trust store. On rooted devices it lands the cert in the
// system CA store (so apps that trust system CAs intercept cleanly), trying
// persistent strategies first and falling back to an Android-10+ tmpfs overlay.
// On non-rooted devices it stages the cert to /sdcard/Download and returns the
// manual steps, since the OS requires a user tap to trust a user CA.
//
// localCertPath may be DER (.der/.cer) or PEM (.pem/.crt). The Android trust
// store keys files by the OpenSSL subject_hash_old, so we compute that here and
// name the file <hash>.0 (incrementing the suffix on the rare hash collision).
func (c *Client) InstallSystemCert(ctx context.Context, serial, localCertPath string) (*CertInstallResult, error) {
	cert, der, err := loadCertificate(localCertPath)
	if err != nil {
		return nil, err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	hash := androidSubjectHash(cert)

	res := &CertInstallResult{Subject: subjectString(cert), SDK: c.Capabilities(ctx, serial).SDK}

	// Stage the PEM on the device via `adb push` (works without root, and unlike
	// shell `printf`/`echo` heredocs it doesn't depend on which utilities the
	// ROM ships — some stripped images lack printf entirely).
	const stage = "/data/local/tmp/adbq-cacert.pem"
	tmp, err := os.CreateTemp("", "adbq-cacert-*.pem")
	if err != nil {
		return res, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(pemBytes); err != nil {
		tmp.Close()
		return res, err
	}
	tmp.Close()
	if _, err := c.PushFile(ctx, serial, tmpPath, stage); err != nil {
		return res, fmt.Errorf("stage cert on device: %w", err)
	}
	_, _ = c.Shell(ctx, serial, "chmod 644 "+stage)

	style, _ := c.suStyleFor(ctx, serial)
	// Every concrete root style is usable here — ShellSU/rootWrap already
	// handle suBareRoot (adb-rooted emulators/userdebug, no `su` binary) and the
	// suZero* forms the same as suSimple/suShWrap. Only suUnknown means "no
	// root at all". Excluding suBareRoot here made adb-rooted emulators wrongly
	// fall back to the user-store path despite being rooted.
	res.Rooted = styleGrantsRoot(style)
	if !res.Rooted {
		return c.installUserCertFallback(ctx, serial, stage, hash, res)
	}

	fname := c.pickCacertFileName(ctx, serial, hash, pemBytes)
	res.FileName = fname
	res.Path = systemCacertsDir + "/" + fname
	want := normalizePEM(string(pemBytes))

	var diag strings.Builder
	// Persistent strategies first — a reboot keeps the cert. Mirrors the hosts
	// writer (hosts.go) since the underlying problem (writable /system) is the
	// same.
	install := cacertInstallCmd(stage, res.Path)
	persistent := []struct{ name, cmd string }{
		{"direct", install},
		{"magisk-remount", "magisk --remount-system 2>&1; " + install},
		{"remount-root", "mount -o rw,remount / 2>&1; " + install + " ; mount -o ro,remount / 2>/dev/null; true"},
		{"remount-system", "mount -o rw,remount /system 2>&1; " + install + " ; mount -o ro,remount /system 2>/dev/null; true"},
	}
	for _, s := range persistent {
		out, _, _ := c.ShellSU(ctx, serial, s.cmd)
		diag.WriteString("\n[" + s.name + "]\n" + strings.TrimSpace(out) + "\n")
		if c.cacertMatches(ctx, serial, res.Path, want) {
			res.Strategy = s.name
			res.Persistent = true
			break
		}
	}

	if res.Strategy == "" {
		// Android 10+ keeps /system read-only even to root. Overlay a tmpfs on
		// the cacerts dir(s), repopulate with the existing system CAs plus ours,
		// and fix up perms/SELinux. Also covers the Android 14+ Conscrypt APEX
		// store. Lost on reboot — re-run after a reboot.
		out, _, _ := c.ShellSU(ctx, serial, cacertOverlayCmd(stage, fname))
		diag.WriteString("\n[tmpfs-overlay]\n" + strings.TrimSpace(out) + "\n")
		if c.cacertMatches(ctx, serial, res.Path, want) {
			res.Strategy = "tmpfs-overlay"
			res.Persistent = false
		}
	}

	res.Diagnostics = diag.String()
	if res.Strategy == "" {
		return res, fmt.Errorf("could not install system CA — see diagnostics. The device may not be rooted with a usable `su`, or it blocks both /system writes and tmpfs overlays")
	}

	_, _ = c.Shell(ctx, serial, "rm -f "+stage)
	if res.Persistent {
		res.Note = "Installed as a system CA (" + res.Strategy + "). Force-stop and reopen the target app so it reloads the trust store."
	} else {
		res.Note = "Installed via tmpfs overlay — active now but reset on reboot. Re-run after rebooting. Force-stop and reopen the target app to pick it up."
	}
	return res, nil
}

// styleGrantsRoot reports whether a probed suStyle is a real root grant, i.e.
// anything but suUnknown. All concrete styles — including suBareRoot (adb-rooted
// emulators/userdebug, no `su` binary) and the suZero* positional forms — are
// usable for system-store cert writes because ShellSU/rootWrap already adapt the
// command to each style. suUnknown means no root at all → user-store fallback.
func styleGrantsRoot(style suStyle) bool {
	return style == suBareRoot || style == suSimple || style == suShWrap ||
		style == suZeroSimple || style == suZeroShWrap
}

// installUserCertFallback stages the cert where the user can import it via
// Settings, since a non-rooted device requires an explicit user tap to trust a
// CA (and Android 7+ apps ignore user CAs unless they opt in).
func (c *Client) installUserCertFallback(ctx context.Context, serial, stage, hash string, res *CertInstallResult) (*CertInstallResult, error) {
	dst := "/sdcard/Download/adbq-ca-" + hash + ".crt"
	if _, err := c.Shell(ctx, serial, "mkdir -p /sdcard/Download && cp "+stage+" "+dst); err != nil {
		return res, fmt.Errorf("copy cert to /sdcard: %w", err)
	}
	_, _ = c.Shell(ctx, serial, "rm -f "+stage)
	// Best-effort: open the security settings so the user is one step from the
	// CA import screen. Harmless if it fails.
	_, _ = c.Shell(ctx, serial, "am start -a android.settings.SECURITY_SETTINGS >/dev/null 2>&1 || true")
	res.Strategy = "user-store"
	res.Persistent = false
	res.FileName = "adbq-ca-" + hash + ".crt"
	res.Path = dst
	res.Note = "Device is not rooted. The cert was saved to " + dst + ". Install it via Settings → Security → Encryption & credentials → Install a certificate → CA certificate, then pick the file. Note: most Android 7+ apps ignore user-store CAs unless their network-security-config opts in — system-store (root) is needed for those."
	return res, nil
}

// cacertInstallCmd drops a single staged cert into the system store with the
// right perms/owner/SELinux label.
func cacertInstallCmd(stage, destPath string) string {
	return fmt.Sprintf(
		"cp %s %s && chmod 644 %s && chown root:root %s 2>/dev/null; chcon u:object_r:system_security_cacerts_file:s0 %s 2>/dev/null; sync; echo OK",
		stage, destPath, destPath, destPath, destPath)
}

// cacertOverlayCmd builds an idempotent tmpfs overlay for the CA store, used
// when /system is read-only (Android 10+, emulators without -writable-system).
//
// It applies a straight-line overlay (no shell functions — those proved fragile
// through the su/adb quoting layers) to the legacy store and, when present, the
// Android 14+ Conscrypt APEX store. Each dir is first unmounted a few times to
// tear down any overlays we stacked on a previous run — without this, re-running
// piles tmpfs layers and an empty top layer silently drops the cert. `mkdir -m`
// is avoided (old toybox misparses it); the snapshot copies the *real* store
// after the unmounts.
func cacertOverlayCmd(stage, fname string) string {
	apex := "/apex/com.android.conscrypt/cacerts"
	return overlayOneDir(systemCacertsDir, stage, fname) +
		"\nif [ -d " + apex + " ]; then\n" + overlayOneDir(apex, stage, fname) + "\nfi\n" +
		"sync; echo OK"
}

func overlayOneDir(dir, stage, fname string) string {
	const work = "/data/local/tmp/adbq-cacerts"
	unmounts := strings.Repeat("umount "+dir+" 2>/dev/null; ", 8)
	return fmt.Sprintf(`%[5]srm -rf %[2]s; mkdir -p %[2]s && chmod 700 %[2]s
cp -f %[1]s/* %[2]s/ 2>/dev/null
cp -f %[3]s %[2]s/%[4]s
mount -t tmpfs tmpfs %[1]s
cp -f %[2]s/* %[1]s/
chmod 644 %[1]s/* 2>/dev/null
chown root:root %[1]s/* 2>/dev/null
chcon u:object_r:system_security_cacerts_file:s0 %[1]s/* 2>/dev/null`,
		dir, work, stage, fname, unmounts)
}

// cacertMatches verifies the on-device file equals the cert we meant to install,
// reading it back through a fresh root shell and comparing normalized PEM. We
// compare content (not md5sum/awk) because minimal toybox builds on old devices
// lack those tools.
func (c *Client) cacertMatches(ctx context.Context, serial, path, wantNormalized string) bool {
	out, _, err := c.ShellSU(ctx, serial, "cat "+path+" 2>/dev/null")
	if err != nil {
		return false
	}
	return normalizePEM(out) == wantNormalized
}

// pickCacertFileName returns "<hash>.N" choosing the lowest free suffix, reusing
// .0 when it's absent or already holds this exact cert (idempotent re-install).
func (c *Client) pickCacertFileName(ctx context.Context, serial, hash string, pemBytes []byte) string {
	want := normalizePEM(string(pemBytes))
	for i := 0; i < 10; i++ {
		name := hash + "." + strconv.Itoa(i)
		out, _, _ := c.ShellSU(ctx, serial, "cat "+systemCacertsDir+"/"+name+" 2>/dev/null")
		got := strings.TrimSpace(out)
		if got == "" || normalizePEM(out) == want {
			return name // free slot, or already ours
		}
	}
	return hash + ".0"
}

// loadCertificate reads a DER or PEM certificate file and returns the parsed
// cert plus its DER bytes.
func loadCertificate(path string) (*x509.Certificate, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	der := raw
	if block, _ := pem.Decode(raw); block != nil {
		if block.Type != "CERTIFICATE" {
			return nil, nil, fmt.Errorf("PEM file holds a %q block, not a CERTIFICATE", block.Type)
		}
		der = block.Bytes
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("not a valid X.509 certificate: %w", err)
	}
	return cert, der, nil
}

// androidSubjectHash computes OpenSSL's subject_hash_old (the value Android uses
// to name files in its trust store): the first 4 bytes of MD5(DER subject),
// little-endian, as 8 lowercase hex digits.
func androidSubjectHash(cert *x509.Certificate) string {
	sum := md5.Sum(cert.RawSubject)
	return fmt.Sprintf("%08x", binary.LittleEndian.Uint32(sum[:4]))
}

func subjectString(cert *x509.Certificate) string {
	s := cert.Subject.CommonName
	if s == "" {
		s = cert.Subject.String()
	}
	if s == "" {
		s = "(unnamed CA)"
	}
	return s
}

// normalizePEM strips CR and surrounding whitespace so content comparisons
// survive adb's line-ending handling.
func normalizePEM(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r", ""))
}
