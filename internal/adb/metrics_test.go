package adb

import (
	"testing"
	"time"
)

func TestSpawnKeyForCollapsesToCommandShape(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "device target is dropped so one call site is not split per device",
			args: []string{"/usr/bin/adb", "-s", "emulator-5554", "shell", "getprop ro.build.id"},
			want: "shell getprop",
		},
		{
			name: "a batched script reports as the shape of its first command",
			args: []string{"/usr/bin/adb", "-s", "R58M12", "shell", "cat /proc/stat; echo '@@@'; cat /proc/meminfo"},
			want: "shell cat",
		},
		{
			name: "untargeted subcommand",
			args: []string{"/usr/bin/adb", "devices", "-l"},
			want: "devices",
		},
		{
			name: "shell with no remote command",
			args: []string{"/usr/bin/adb", "-s", "abc", "shell"},
			want: "shell",
		},
		{
			name: "leading whitespace in the remote command is ignored",
			args: []string{"/usr/bin/adb", "-s", "abc", "shell", "  id"},
			want: "shell id",
		},
		{
			name: "empty argv degrades instead of panicking",
			args: nil,
			want: "adb",
		},
		{
			name: "binary path only",
			args: []string{"/usr/bin/adb"},
			want: "adb",
		},
		{
			name: "dangling -s target",
			args: []string{"/usr/bin/adb", "-s", "abc"},
			want: "adb",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := spawnKeyFor(tc.args); got != tc.want {
				t.Errorf("spawnKeyFor(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// A per-serial or per-package key would make the top-N list useless — it is
// meant to name the *caller* that spawns too much, and there is one caller
// behind all of these.
func TestSpawnKeyForIsLowCardinality(t *testing.T) {
	pkgs := []string{"com.example.one", "com.example.two", "com.other.app"}
	seen := map[string]bool{}
	for _, p := range pkgs {
		seen[spawnKeyFor([]string{"adb", "-s", "dev", "shell", "pidof " + p})] = true
	}
	if len(seen) != 1 {
		t.Errorf("one call site produced %d keys (%v), want 1", len(seen), seen)
	}
}

func TestBucketIndex(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want int
	}{
		{0, 0},
		{500 * time.Microsecond, 0},
		{time.Millisecond, 1},
		{4 * time.Millisecond, 1},
		{24 * time.Millisecond, 2},
		{99 * time.Millisecond, 3},
		{499 * time.Millisecond, 4},
		{1999 * time.Millisecond, 5},
		{2 * time.Second, len(durationBucketBounds)},
		{time.Minute, len(durationBucketBounds)},
	}
	for _, tc := range tests {
		if got := bucketIndex(tc.d); got != tc.want {
			t.Errorf("bucketIndex(%v) = %d, want %d", tc.d, got, tc.want)
		}
	}
}

func TestMetricsCountsAndRanks(t *testing.T) {
	m := &metrics{byCmd: map[string]int64{}, window: time.Now()}

	for range 5 {
		m.recordSpawn(spawnOneShot, []string{"adb", "-s", "d", "shell", "getprop ro.x"}, 2*time.Millisecond)
	}
	m.recordSpawn(spawnOneShot, []string{"adb", "devices", "-l"}, 30*time.Millisecond)
	m.recordSpawn(spawnStream, []string{"adb", "-s", "d", "logcat", "-v", "threadtime"}, 0)

	snap := m.snapshot(10)
	if snap.Spawns != 6 {
		t.Errorf("Spawns = %d, want 6 (streams must not be counted as one-shots)", snap.Spawns)
	}
	if snap.Streams != 1 {
		t.Errorf("Streams = %d, want 1", snap.Streams)
	}
	if snap.TotalMillis != 40 {
		t.Errorf("TotalMillis = %d, want 40", snap.TotalMillis)
	}
	if len(snap.TopCommands) == 0 || snap.TopCommands[0].Command != "shell getprop" || snap.TopCommands[0].Count != 5 {
		t.Errorf("TopCommands[0] = %+v, want {shell getprop 5}", snap.TopCommands)
	}
	// The stream is tagged so it cannot be mistaken for a repeated round trip.
	var sawStream bool
	for _, c := range snap.TopCommands {
		if c.Command == "(stream) logcat" {
			sawStream = true
		}
	}
	if !sawStream {
		t.Errorf("stream spawn missing or untagged in %+v", snap.TopCommands)
	}
	// 2ms → bucket 1, 30ms → bucket 3.
	if snap.Buckets[1].Count != 5 || snap.Buckets[3].Count != 1 {
		t.Errorf("buckets = %+v", snap.Buckets)
	}

	m.reset()
	if snap2 := m.snapshot(10); snap2.Spawns != 0 || snap2.Streams != 0 || len(snap2.TopCommands) != 0 {
		t.Errorf("after reset: %+v", snap2)
	}
}
