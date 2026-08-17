package adb

import "fmt"

// RootAVD action codes. The UI branches on these rather than on the reason
// text, which is prose meant for the user.
const (
	// RootNotNeeded — `adb root` already gives full root on this image, so
	// patching its ramdisk would be pure risk for no gain.
	RootNotNeeded = "not-needed"
	// RootAlready — this system image already carries a rootAVD backup, i.e.
	// it has been patched before.
	RootAlready = "already"
	// RootUnsupported — rootAVD is known not to work on this API level.
	RootUnsupported = "unsupported"
	// RootRisky — outside the range upstream tests, but not known to fail.
	RootRisky = "risky"
	// RootEligible — the ordinary case rootAVD exists for.
	RootEligible = "eligible"
)

// rootAVD upstream support window, from its README and CompatibilityChart.
//
// API 28 is excluded by the tool itself: Pie moved to a system-as-root layout
// that the ramdisk patch does not apply to. Below 25 there is no ramdisk to
// patch in the form rootAVD expects.
const (
	rootAVDMinAPI     = 25
	rootAVDMaxTestAPI = 34
	rootAVDBrokenAPI  = 28
)

// RootAVDAdvice decides whether rootAVD should be offered for an AVD, and says
// why in a sentence the UI can show verbatim.
//
// The order matters: "you already have root" and "this image is already
// patched" both outrank compatibility, because in those cases the user should
// not be running rootAVD regardless of what the level chart says.
//
// root is the AVD's observed root state ("adb-root", "su", "no", or "" when the
// AVD is not running); patched reports a ramdisk backup next to the image.
func RootAVDAdvice(api int, playStore bool, root string, patched bool) (action, reason string) {
	switch {
	case root == "adb-root":
		return RootNotNeeded, "This AVD already runs adb as root — patching the system image would add risk without adding access."
	case root == "su":
		return RootAlready, "This AVD already has a working su binary."
	case patched:
		return RootAlready, "This system image has already been patched (a ramdisk backup sits next to it). Use Restore to undo it."
	case !playStore && root == "":
		// Not running, so we have no observed root. A non-Play-Store image is
		// debuggable and `adb root` will almost certainly succeed, so say so
		// rather than sending the user down the patching path first.
		return RootNotNeeded, "This is not a Play Store image, so `adb root` should work once it boots. Start it and check before patching anything."
	case !playStore:
		return RootEligible, "`adb root` was refused on this non-Play-Store image, which is unusual — rooting with Magisk will work."
	case api == rootAVDBrokenAPI:
		return RootUnsupported, fmt.Sprintf("rootAVD does not support API %d (Android 9 changed to a system-as-root layout the ramdisk patch cannot handle).", api)
	case api > 0 && api < rootAVDMinAPI:
		return RootUnsupported, fmt.Sprintf("rootAVD supports API %d and newer; this image is API %d.", rootAVDMinAPI, api)
	case api > rootAVDMaxTestAPI:
		return RootRisky, fmt.Sprintf("rootAVD is only tested up to API %d and this image is API %d. It may still work — a ramdisk backup is taken first and Restore undoes it.", rootAVDMaxTestAPI, api)
	case api == 0:
		return RootRisky, "This is a preview system image, which rootAVD has not been tested against. A ramdisk backup is taken first and Restore undoes it."
	default:
		return RootEligible, fmt.Sprintf("This is a Play Store image, so `adb root` is refused by design. rootAVD patches API %d images to install Magisk.", api)
	}
}

// RootAVDOffered reports whether the UI should show the "Root this AVD" action.
func RootAVDOffered(action string) bool {
	return action == RootEligible || action == RootRisky
}
