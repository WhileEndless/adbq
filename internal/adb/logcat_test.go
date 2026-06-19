package adb

import "testing"

func TestParseThreadtimeShapes(t *testing.T) {
	cases := []struct {
		name string
		line string
		want LogEntry
	}{
		{
			name: "classic no-year",
			line: "05-22 12:18:04.123  1582 1612 I ActivityManager: Start proc",
			want: LogEntry{Time: "05-22 12:18:04.123", PID: 1582, TID: 1612, Level: "I", Tag: "ActivityManager", Msg: "Start proc"},
		},
		{
			name: "year-prefixed (API 24+)",
			line: "2026-05-22 12:18:04.123  1582 1612 D OkHttp: --> GET /x",
			want: LogEntry{Time: "2026-05-22 12:18:04.123", PID: 1582, TID: 1612, Level: "D", Tag: "OkHttp", Msg: "--> GET /x"},
		},
		{
			name: "UTC offset token",
			line: "05-22 12:18:04.123 +0000  1582 1612 W ART: slow",
			want: LogEntry{Time: "05-22 12:18:04.123 +0000", PID: 1582, TID: 1612, Level: "W", Tag: "ART", Msg: "slow"},
		},
		{
			name: "year and UTC offset",
			line: "2026-05-22 12:18:04.123 +0000 1582 1612 E Conscrypt: boom",
			want: LogEntry{Time: "2026-05-22 12:18:04.123 +0000", PID: 1582, TID: 1612, Level: "E", Tag: "Conscrypt", Msg: "boom"},
		},
		{
			name: "monotonic timestamp",
			line: "1234.567 1582 1612 I Tag: hi",
			want: LogEntry{Time: "1234.567", PID: 1582, TID: 1612, Level: "I", Tag: "Tag", Msg: "hi"},
		},
		{
			name: "message preserves internal spacing and colons",
			line: "05-22 12:18:04.123  1582 1612 I Foo: a:  b   c",
			want: LogEntry{Time: "05-22 12:18:04.123", PID: 1582, TID: 1612, Level: "I", Tag: "Foo", Msg: "a:  b   c"},
		},
		{
			name: "tag with no message",
			line: "05-22 12:18:04.123  1582 1612 I JustTag",
			want: LogEntry{Time: "05-22 12:18:04.123", PID: 1582, TID: 1612, Level: "I", Tag: "JustTag", Msg: ""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseThreadtime(tc.line)
			if !ok {
				t.Fatalf("parse failed for %q", tc.line)
			}
			if got != tc.want {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestParseThreadtimeRejectsNonLog(t *testing.T) {
	for _, line := range []string{
		"",
		"--------- beginning of main",
		"java.lang.NullPointerException",
		"    at com.foo.Bar.baz(Bar.java:42)",
		"short line",
	} {
		if e, ok := parseThreadtime(line); ok {
			t.Errorf("parseThreadtime(%q) unexpectedly parsed as %+v", line, e)
		}
	}
}
