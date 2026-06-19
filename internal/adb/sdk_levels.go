package adb

import "strconv"

// androidNames maps API level → "Android <ver> (Codename)" label.
// Source: https://developer.android.com/tools/releases/platforms
var androidNames = map[int]string{
	21: "Android 5.0 (Lollipop)",
	22: "Android 5.1 (Lollipop MR1)",
	23: "Android 6.0 (Marshmallow)",
	24: "Android 7.0 (Nougat)",
	25: "Android 7.1 (Nougat MR1)",
	26: "Android 8.0 (Oreo)",
	27: "Android 8.1 (Oreo MR1)",
	28: "Android 9 (Pie)",
	29: "Android 10 (Q)",
	30: "Android 11 (R)",
	31: "Android 12 (S)",
	32: "Android 12L (Sv2)",
	33: "Android 13 (Tiramisu)",
	34: "Android 14 (UpsideDownCake)",
	35: "Android 15 (VanillaIceCream)",
	36: "Android 16 (Baklava)",
}

// AndroidVersionForSdk returns "Android 13 (Tiramisu)" for "33". Returns the
// input verbatim when the level is unknown or unparseable.
func AndroidVersionForSdk(level string) string {
	n, err := strconv.Atoi(level)
	if err != nil {
		return level
	}
	if name, ok := androidNames[n]; ok {
		return name
	}
	return level
}

// AndroidVersionMap returns a copy of the SDK level → name table for shipping
// to the frontend once at startup (avoids per-app round-trips for labelling).
func AndroidVersionMap() map[int]string {
	out := make(map[int]string, len(androidNames))
	for k, v := range androidNames {
		out[k] = v
	}
	return out
}
