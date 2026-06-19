package adb

import (
	"bytes"
	"testing"
)

func TestIsPNG(t *testing.T) {
	valid := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte{0x00, 0x01, 0x02}...)
	if !isPNG(valid) {
		t.Error("valid PNG not recognized")
	}
	for _, bad := range [][]byte{
		nil,
		{},
		[]byte("not a png"),
		{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, // header only, no data
	} {
		if isPNG(bad) {
			t.Errorf("isPNG(%v) = true, want false", bad)
		}
	}
}

func TestScreencapCRLFUnmangle(t *testing.T) {
	// Simulate the shell transport mangling every LF into CRLF.
	original := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte{'d', '\n', 'a', '\n', 't', 'a'}...)
	mangled := bytes.ReplaceAll(original, []byte("\n"), []byte("\r\n"))
	fixed := bytes.ReplaceAll(mangled, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(fixed, original) {
		t.Errorf("un-mangle did not restore original: %v vs %v", fixed, original)
	}
	if !isPNG(fixed) {
		t.Error("un-mangled bytes should be a valid PNG")
	}
}
