package adb

// Pinned tcpdump binaries for auto-install on the device. Per CLAUDE.md §1.2
// we never accept an unverified blob: each entry below is matched on the
// device ABI, the URL is pinned to a specific commit on a public GitHub
// repository, and the streamed bytes must hash to the SHA256 listed here.
//
// Source: github.com/vanyasem/magisk-tcpdump — a Magisk module that ships
// statically-linked tcpdump 4.99.5 builds for Android ABIs. We pull the raw
// `system/bin/tcpdump` file from a pinned commit (not the floating main
// branch) so the bytes can never change underneath us.
//
// File integrity was verified locally:
//   $ curl -L -o aarch64 'https://raw.githubusercontent.com/.../<pin>/aarch64/system/bin/tcpdump'
//   $ shasum -a 256 aarch64
//   351635d4...  → matches manifest below
//   $ file aarch64
//   → ELF 64-bit LSB executable, ARM aarch64, statically linked, stripped
//
// To bump:
//   1. Pick a new commit on vanyasem/magisk-tcpdump (or a different reputable
//      static-build repo).
//   2. curl the per-arch raw binaries, shasum -a 256 each, file(1) sanity.
//   3. Update the URL + SHA256 + Size below. Bump TcpdumpManifestSource.
//   4. Document the bump in the commit message.

// TcpdumpBuild describes one pinned binary in the manifest.
type TcpdumpBuild struct {
	Abi    string `json:"abi"`    // ro.product.cpu.abi value
	URL    string `json:"url"`    // pinned raw.githubusercontent.com URL
	SHA256 string `json:"sha256"` // hex, lower-case
	Source string `json:"source"` // human label for the UI (repo + version)
	Size   int64  `json:"size"`   // expected byte length; 0 = unknown
}

// TcpdumpManifestSource is shown in the install confirmation dialog so the
// user can see at a glance what upstream we're pulling from.
const TcpdumpManifestSource = "github.com/vanyasem/magisk-tcpdump (tcpdump 4.99.5, commit 858c81b)"

// tcpdumpManifest is the immutable allowlist of download targets. Anything
// not in this list is rejected before any network call. URLs are pinned to a
// specific commit hash so the bytes are stable forever.
const tcpdumpPin = "858c81b381130b38ba17b745465ae9ef35197fcc"

var tcpdumpManifest = []TcpdumpBuild{
	{
		Abi:    "arm64-v8a",
		URL:    "https://raw.githubusercontent.com/vanyasem/magisk-tcpdump/" + tcpdumpPin + "/aarch64/system/bin/tcpdump",
		SHA256: "351635d45adbeec477e783b1d7336a8db7890e95f1f82854e610d4e0ea93cd14",
		Source: TcpdumpManifestSource + " · aarch64",
		Size:   2303768,
	},
	{
		Abi:    "armeabi-v7a",
		URL:    "https://raw.githubusercontent.com/vanyasem/magisk-tcpdump/" + tcpdumpPin + "/armv7h/system/bin/tcpdump",
		SHA256: "cead1f9f570b1dd1add31320edc0bba1cbc25046d3324b295cdbf6ef5df6e637",
		Source: TcpdumpManifestSource + " · armv7h",
		Size:   2074440,
	},
	// No pinned x86/x86_64 in the upstream module. Most physical Android
	// devices are arm64-v8a; emulator users can still install via the file
	// picker (InstallTcpdumpWithPicker) using e.g. NDK-built tcpdumps.
}

// tcpdumpBuildFor returns the manifest entry matching the device ABI, or nil
// when the ABI isn't covered. Caller should surface a clear "unsupported
// architecture" error to the user in that case.
func tcpdumpBuildFor(abi string) *TcpdumpBuild {
	for i := range tcpdumpManifest {
		if tcpdumpManifest[i].Abi == abi {
			return &tcpdumpManifest[i]
		}
	}
	return nil
}
