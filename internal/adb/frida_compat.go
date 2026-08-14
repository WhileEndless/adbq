package adb

// Which frida-server versions are worth running on which Android.
//
// The install list offers every published release for the device's architecture,
// and nothing told the user that some of them cannot work on their device. The
// failures are not obvious from the outside: the server starts, reports its
// version, and only the agent inside the target process fails — so the symptom
// is a host-side "timeout was reached" with no hint that the version was the
// problem. This is the missing hint.
//
// Everything here is a heuristic over API level and version, evaluated purely so
// it can be tested and shown before anything is downloaded. It advises; it never
// blocks. Advice is deliberately conservative: only patterns we have evidence for.

// Advice levels for a (device, frida version) pair.
const (
	FridaAdviceOK      = "ok"
	FridaAdviceWarn    = "warn"
	FridaAdviceBroken  = "broken"
	FridaAdviceUnknown = ""
)

// fridaJavaBridgeART is the first frida line whose Java bridge locates ART's
// internals by scanning for field offsets rather than by table. The scan does not
// resolve on Android 5.x, so the server runs but nothing Java-related works.
// Measured on an API 21 emulator with 17.5.1: frida-server reports its version,
// then the agent fails with "Unable to find fields in java/lang/Thread" and every
// enumerate/spawn call times out.
const fridaJavaBridgeART = "16.6.0"

// androidLollipopMax is the last API level of Android 5.x.
const androidLollipopMax = 22

// android16KBPageMin is the first API level whose devices and emulator images
// may use a 16 KB memory page size, which frida's prebuilt binaries are not
// universally aligned for (frida/frida#3389).
const android16KBPageMin = 35

// FridaAdvice rates a frida version against a device's API level and explains
// why. An unknown SDK (0) or version yields no advice rather than a guess.
func FridaAdvice(sdk int, version string) (level, note string) {
	if sdk <= 0 || version == "" {
		return FridaAdviceUnknown, ""
	}
	switch {
	case sdk <= androidLollipopMax && compareVersions(version, fridaJavaBridgeART) >= 0:
		return FridaAdviceBroken, "This frida's Java bridge cannot map ART on Android 5.x — the server starts, but Java hooks never run and calls time out. Try a 16.5.x or older build."
	case sdk >= android16KBPageMin:
		return FridaAdviceWarn, "Android 15+ images may use a 16 KB memory page size, which frida's prebuilt server does not always support. If it crashes on launch, check the server log and try another build."
	}
	return FridaAdviceOK, ""
}
