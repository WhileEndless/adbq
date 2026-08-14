package adb

import (
	"strings"
	"testing"
)

func TestFridaAdvice(t *testing.T) {
	cases := []struct {
		name    string
		sdk     int
		version string
		want    string
	}{
		// Measured: on an API 21 emulator, frida-server 17.5.1 starts and reports
		// its version, then its agent fails with "Unable to find fields in
		// java/lang/Thread" and every enumerate/spawn call times out.
		{"lollipop with a modern frida", 21, "17.5.1", FridaAdviceBroken},
		{"lollipop MR1 with a modern frida", 22, "16.6.0", FridaAdviceBroken},
		{"lollipop with an older frida", 21, "16.5.9", FridaAdviceOK},
		{"lollipop with a much older frida", 21, "14.2.2", FridaAdviceOK},

		{"marshmallow is unaffected", 23, "17.5.1", FridaAdviceOK},
		{"pie is unaffected", 28, "17.5.1", FridaAdviceOK},
		{"android 14 is unaffected", 34, "17.5.1", FridaAdviceOK},

		{"android 15 warns about page size", 35, "17.5.1", FridaAdviceWarn},
		{"android 16 warns about page size", 36, "17.9.9", FridaAdviceWarn},

		// A guess is worse than silence when we do not know the device.
		{"unknown sdk", 0, "17.5.1", FridaAdviceUnknown},
		{"unknown version", 34, "", FridaAdviceUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			level, note := FridaAdvice(tc.sdk, tc.version)
			if level != tc.want {
				t.Errorf("FridaAdvice(%d, %q) = %q, want %q", tc.sdk, tc.version, level, tc.want)
			}
			// Anything not "ok" has to say why, or the badge is just noise.
			if level != FridaAdviceOK && level != FridaAdviceUnknown && strings.TrimSpace(note) == "" {
				t.Errorf("FridaAdvice(%d, %q) returned %q with no explanation", tc.sdk, tc.version, level)
			}
			if level == FridaAdviceOK && note != "" {
				t.Errorf("FridaAdvice(%d, %q) is ok but carries a note %q", tc.sdk, tc.version, note)
			}
		})
	}
}

// The Android 5 cut-off has to sit exactly at the release that changed how the
// Java bridge finds ART, or advice is wrong on one side of it.
func TestFridaAdviceBoundaryOnLollipop(t *testing.T) {
	if lvl, _ := FridaAdvice(21, "16.5.9"); lvl != FridaAdviceOK {
		t.Errorf("16.5.9 on API 21 rated %q, want ok", lvl)
	}
	if lvl, _ := FridaAdvice(21, fridaJavaBridgeART); lvl != FridaAdviceBroken {
		t.Errorf("%s on API 21 rated %q, want broken", fridaJavaBridgeART, lvl)
	}
	// Marshmallow (23) is the first level the scan works on.
	if lvl, _ := FridaAdvice(23, "17.5.1"); lvl != FridaAdviceOK {
		t.Errorf("17.5.1 on API 23 rated %q, want ok", lvl)
	}
}
