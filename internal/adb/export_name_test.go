package adb

import (
	"strings"
	"testing"
)

func TestExportBaseNameCarriesTheVersion(t *testing.T) {
	cases := []struct {
		v    AppVersion
		want string
	}{
		{AppVersion{Name: "1.2.3", Code: "10203"}, "com.example.app-1.2.3-10203"},
		{AppVersion{Name: "1.2.3"}, "com.example.app-1.2.3"},
		{AppVersion{Code: "10203"}, "com.example.app-10203"},
		{AppVersion{}, "com.example.app"},
		// Some builds set the name to the code; repeating it says nothing.
		{AppVersion{Name: "10203", Code: "10203"}, "com.example.app-10203"},
	}
	for _, c := range cases {
		if got := ExportBaseName("com.example.app", c.v); got != c.want {
			t.Errorf("ExportBaseName(%+v) = %q, want %q", c.v, got, c.want)
		}
	}
}

// A version name is free text set by whoever built the app, so it reaches the
// file system only after being made safe for it.
func TestExportBaseNameSanitisesTheVersionName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"1.0 (beta)", "com.example.app-1.0-beta"},
		{"2.3-debug/nightly", "com.example.app-2.3-debug-nightly"},
		{`a\b:c*d?`, "com.example.app-a-b-c-d"},
		{"  1.0  ", "com.example.app-1.0"},
		{"../../etc/passwd", "com.example.app-etc-passwd"},
		{"///", "com.example.app"},
	}
	for _, c := range cases {
		got := ExportBaseName("com.example.app", AppVersion{Name: c.in})
		if got != c.want {
			t.Errorf("ExportBaseName(name=%q) = %q, want %q", c.in, got, c.want)
		}
		if strings.ContainsAny(got, `/\:*?"<>|`) {
			t.Errorf("ExportBaseName(name=%q) = %q still holds a character no file system wants", c.in, got)
		}
	}
}

func TestExportBaseNameBoundsAbsurdVersionNames(t *testing.T) {
	got := ExportBaseName("com.example.app", AppVersion{Name: strings.Repeat("9", 500)})
	if len(got) > len("com.example.app")+1+maxVersionInName {
		t.Errorf("a pathological version name must be trimmed, got %d chars", len(got))
	}
}

func TestParseAppVersionTakesTheFirstOfEach(t *testing.T) {
	out := `    versionCode=10203 minSdk=21 targetSdk=34
      versionName=1.2.3
    User 0: ceDataInode=1
    versionCode=99999 minSdk=21
`
	v := parseAppVersion(out)
	if v.Code != "10203" || v.Name != "1.2.3" {
		t.Errorf("parseAppVersion = %+v, want code 10203 and name 1.2.3", v)
	}
	if got := parseAppVersion("nothing useful here\n"); got != (AppVersion{}) {
		t.Errorf("unparsable output must yield an empty version, got %+v", got)
	}
}
