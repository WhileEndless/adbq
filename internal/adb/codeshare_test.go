package adb

import (
	"encoding/json"
	"testing"
)

// Mirrors the live search/browse markup captured from codeshare.frida.re.
const codeshareSearchHTML = `
<div class="posts">
<h2><a href="https://codeshare.frida.re/@pcipolloni/universal-android-ssl-pinning-bypass-with-frida/">Universal Android SSL Pinning Bypass with Frida</a></h2>
  <i class="fa fa-thumbs-o-up" aria-hidden="true"></i> 129 &nbsp; <i class="fa fa-eye" aria-hidden="true"></i> 561K
<h4>Uploaded by: <a href="/@pcipolloni/">@pcipolloni</a></h4>
<h2><a href="https://codeshare.frida.re/@akabe1/frida-multiple-unpinning/">frida-multiple-unpinning &amp; more</a></h2>
  <i class="fa fa-thumbs-o-up" aria-hidden="true"></i> 64 &nbsp; <i class="fa fa-eye" aria-hidden="true"></i> 268K
</div>`

func TestParseCodeshareList(t *testing.T) {
	got := parseCodeshareList(codeshareSearchHTML)
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %d: %+v", len(got), got)
	}
	first := got[0]
	if first.Owner != "pcipolloni" || first.Slug != "universal-android-ssl-pinning-bypass-with-frida" {
		t.Fatalf("owner/slug: %+v", first)
	}
	if first.Title != "Universal Android SSL Pinning Bypass with Frida" {
		t.Fatalf("title: %q", first.Title)
	}
	if first.Likes != "129" || first.Views != "561K" {
		t.Fatalf("likes/views: %q / %q", first.Likes, first.Views)
	}
	// HTML entities in the title must be unescaped.
	if got[1].Title != "frida-multiple-unpinning & more" {
		t.Fatalf("entity unescape: %q", got[1].Title)
	}
}

func TestParseCodeshareListGracefulOnMarkupChange(t *testing.T) {
	// If CodeShare restructures its HTML, discovery must yield zero results, not
	// panic or error — import-by-slug still works.
	for _, in := range []string{
		"",
		"<html><body>totally different markup, no results</body></html>",
		`<div class="card"><span>@owner/slug</span></div>`, // no <h2><a> blocks
	} {
		if got := parseCodeshareList(in); len(got) != 0 {
			t.Fatalf("expected 0 results for changed markup, got %d", len(got))
		}
	}
}

func TestCodeshareProjectJSONShapes(t *testing.T) {
	// The success shape we parse for the source body.
	ok := `{"project_name":"X","description":"d","owner":"o","slug":"s","frida_version":"16.0.0","likes":7,"source":"console.log(1)"}`
	var p codeshareProjectJSON
	if err := json.Unmarshal([]byte(ok), &p); err != nil {
		t.Fatalf("unmarshal ok: %v", err)
	}
	if p.Success != nil {
		t.Fatal("success should be absent on the ok shape")
	}
	if p.Source != "console.log(1)" || p.FridaVersion != "16.0.0" {
		t.Fatalf("fields: %+v", p)
	}

	// The 404 error shape: {"success": false, ...}
	var e codeshareProjectJSON
	if err := json.Unmarshal([]byte(`{"success": false, "error": "Not found!"}`), &e); err != nil {
		t.Fatalf("unmarshal err shape: %v", err)
	}
	if e.Success == nil || *e.Success {
		t.Fatalf("expected success=false, got %+v", e.Success)
	}
}

func TestSha256Hex(t *testing.T) {
	// Stable fingerprint used for CodeShare update detection.
	if got := sha256Hex(""); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("sha256 of empty string: %s", got)
	}
	if sha256Hex("a") == sha256Hex("b") {
		t.Fatal("distinct inputs hashed equal")
	}
}
