package main

import (
	"testing"

	"adbq/internal/adb"
)

// The push path publishes twice off one slice: the bare list the instant a
// transport appears, then the same slice enriched in place. If the watcher
// keeps the caller's slice, the second publish compares the list against itself
// and the UI never learns the device is rooted.
func TestPublishEmitsEnrichmentOfAnAlreadyPublishedSlice(t *testing.T) {
	var emitted [][]adb.Device
	w := &deviceWatcher{emitFn: func(l []adb.Device) { emitted = append(emitted, l) }}

	list := []adb.Device{{ID: "serial-1", State: "device", Online: true}}
	w.publish(list) // bare, as the tracker delivers it

	// Enrichment writes through the same backing array.
	list[0].Root = true
	list[0].RootMethod = "su"
	list[0].AndroidVersion = "14"
	w.publish(list)

	if len(emitted) != 2 {
		t.Fatalf("expected the bare list and the enriched one to be emitted, got %d event(s)", len(emitted))
	}
	if emitted[0][0].Root {
		t.Errorf("first event should be the bare device")
	}
	if !emitted[1][0].Root || emitted[1][0].AndroidVersion != "14" {
		t.Errorf("second event should carry the enrichment, got %+v", emitted[1][0])
	}
}

// An unchanged list must still be silent — that is what keeps an idle app from
// re-rendering the device tabs at the poll rate.
func TestPublishStaysSilentWhenNothingChanged(t *testing.T) {
	var emitted int
	w := &deviceWatcher{emitFn: func([]adb.Device) { emitted++ }}

	list := []adb.Device{{ID: "serial-1", State: "device", Online: true, Root: true}}
	w.publish(list)
	w.publish([]adb.Device{{ID: "serial-1", State: "device", Online: true, Root: true}})

	if emitted != 1 {
		t.Fatalf("expected one event for one distinct list, got %d", emitted)
	}
}
