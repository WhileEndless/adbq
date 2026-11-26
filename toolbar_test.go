package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"adbq/internal/adb"
)

func TestToolbarRecentAppsAndDeviceSelection(t *testing.T) {
	history := make([]adb.FridaHistoryEntry, 12)
	for i := range history {
		history[i] = adb.FridaHistoryEntry{Package: fmt.Sprintf("dev.app%d", i), Mode: "spawn"}
	}
	history[0].Mode = "attach"
	first := adb.Device{ID: "serial-a", Model: "Phone", Online: true}
	second := adb.Device{ID: "serial-b", Model: "Tablet", Online: true}
	devices := []adb.Device{first, second}
	toolbar := quickToolbar{
		ctx: context.Background(), devices: func() []adb.Device { return devices },
		history: func() []adb.FridaHistoryEntry { return history },
		actions: (&App{client: adb.NewClient()}).toolbarActions(),
	}
	rows := toolbar.menu()
	if rows[0].Title != "Device: Choose a device" || !rows[3].Children[0].Disabled {
		t.Fatal("multi-device menu should wait for a selection")
	}
	for i := 0; i < recentAppLimit; i++ {
		if got := rows[3+i].Title; got != history[i].Package {
			t.Fatalf("row %d title = %q, want %s", i, got, history[i].Package)
		}
	}
	if !rows[3+recentAppLimit].Separator {
		t.Fatal("menu exceeded the recent app limit")
	}

	rows[0].Children[0].Run()
	rows = toolbar.menu()
	if rows[0].Title != "Device: Phone (serial-a)" || !rows[0].Children[0].Checked || rows[3].Children[0].Disabled {
		t.Fatal("menu did not enable the selected device")
	}
	if rows[3].Children[0].Title != "Start with Frida" || rows[4].Children[0].Title != "Start with Frida" {
		t.Fatal("start with Frida must be the first action for every package")
	}
	if rows[3].Children[1].Title != "Attach with Frida" || rows[4].Children[1].Title != "Launch" {
		t.Fatal("attach must remain available for the saved attach mode")
	}

	devices = []adb.Device{second}
	rows = toolbar.menu()
	if rows[0].Title != "Device: serial-a (disconnected)" || !rows[3].Children[1].Disabled {
		t.Fatal("disconnect silently changed the action target")
	}
}

func TestToolbarAutomaticallySelectsOnlyDevice(t *testing.T) {
	devices := []adb.Device{{ID: "offline", Online: false}}
	toolbar := quickToolbar{
		ctx: context.Background(), devices: func() []adb.Device { return devices },
		history: func() []adb.FridaHistoryEntry { return nil },
	}
	rows := toolbar.menu()
	if rows[0].Title != "Device: No online devices" || rows[3].Title != "No Frida history yet" {
		t.Fatal("expected empty device and history states")
	}
	devices = append(devices, adb.Device{ID: "online", Online: true})
	if got := toolbar.menu()[0].Title; got != "Device: online" {
		t.Fatalf("device title = %q, want online device", got)
	}
}

func TestToolbarScrcpyTargetsSelectedDeviceWithoutAppHistory(t *testing.T) {
	devices := []adb.Device{{ID: "test-device", Online: true}}
	var active bool
	var ranOn string
	toolbar := quickToolbar{
		ctx: context.Background(), devices: func() []adb.Device { return devices },
		history: func() []adb.FridaHistoryEntry { return nil },
		busy:    make(map[[2]string]bool),
		deviceActions: func(_ string) []toolbarAction {
			title := "Start scrcpy"
			if active {
				title = "Stop scrcpy"
			}
			return []toolbarAction{{title: title, run: func(_ context.Context, target toolbarTarget) ([]string, error) {
				ranOn = target.device.ID
				active = !active
				return nil, nil
			}}}
		},
	}
	start := toolbar.menu()[1]
	if start.Title != "Start scrcpy" || start.Disabled {
		t.Fatal("expected scrcpy to be available with no app history")
	}
	start.Run()
	if ranOn != "test-device" || toolbar.last.title != "Start scrcpy completed" {
		t.Fatalf("start ran on %q with result %v", ranOn, toolbar.last)
	}
	stop := toolbar.menu()[1]
	if stop.Title != "Stop scrcpy" || stop.Disabled {
		t.Fatal("expected an action to stop the active mirror")
	}
	devices = nil
	if !toolbar.menu()[1].Disabled {
		t.Fatal("scrcpy must be disabled without an online target")
	}
	stop.Run()
	if !active || toolbar.last.title != "Stop scrcpy failed" {
		t.Fatal("a stale action ran after the device disconnected")
	}
}

func TestToolbarClearDataConfirmsTheCapturedTarget(t *testing.T) {
	for _, scenario := range []string{"cancel", "disconnect", "replace transport", "accept"} {
		t.Run(scenario, func(t *testing.T) {
			device := adb.Device{ID: "test-device", Online: true, Transport: "transport_id:1"}
			target := toolbarTarget{device: device, history: adb.FridaHistoryEntry{Package: "dev.test.app"}}
			calls := 0
			toolbar := quickToolbar{
				ctx: context.Background(), devices: func() []adb.Device { return []adb.Device{device} },
				busy: make(map[[2]string]bool),
				confirm: func(got toolbarTarget, commands []string) (bool, error) {
					if got.key() != target.key() || len(commands) != 1 || commands[0] != "adb -s test-device shell 'pm clear dev.test.app'" {
						t.Fatalf("confirmation used wrong target or commands: %v, %q", got.key(), commands)
					}
					switch scenario {
					case "cancel":
						return false, nil
					case "disconnect":
						device.Online = false
					case "replace transport":
						device.Transport = "transport_id:2"
					}
					return true, nil
				},
			}
			clear := (&App{client: adb.NewClient()}).toolbarActions()[4]
			clear.run = func(_ context.Context, got toolbarTarget) ([]string, error) {
				if got.key() != target.key() {
					t.Fatalf("clear used wrong target: %v", got.key())
				}
				calls++
				return nil, nil
			}
			toolbar.run(clear, target)
			if scenario == "accept" {
				if calls != 1 || toolbar.last.title != "Clear data completed" {
					t.Fatalf("clear calls = %d, result = %v", calls, toolbar.last)
				}
			} else if calls != 0 || toolbar.last.title == "Clear data completed" {
				t.Fatalf("clear ran after %s: %d calls, result = %v", scenario, calls, toolbar.last)
			}
		})
	}
}

func TestToolbarBlocksDuplicateActionsDuringConfirmation(t *testing.T) {
	device := adb.Device{ID: "test-device", Online: true}
	target := toolbarTarget{device: device, history: adb.FridaHistoryEntry{Package: "dev.test.app"}}
	confirming, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	toolbar := quickToolbar{
		ctx: context.Background(), devices: func() []adb.Device { return []adb.Device{device} },
		busy: make(map[[2]string]bool),
		confirm: func(toolbarTarget, []string) (bool, error) {
			close(confirming)
			<-release
			return false, nil
		},
	}
	go func() {
		defer close(done)
		toolbar.run(toolbarAction{title: "Clear data", confirm: true}, target)
	}()
	<-confirming
	run := func(context.Context, toolbarTarget) ([]string, error) {
		t.Error("action ran with a confirmation pending")
		return nil, nil
	}
	toolbar.run(toolbarAction{title: "Launch", run: run}, target)
	invalid := target
	invalid.history.Package = "dev.test.app;echo invalid"
	toolbar.run(toolbarAction{title: "Launch", run: run}, invalid)
	close(release)
	<-done
	if !strings.HasSuffix(toolbar.last.title, "canceled") {
		t.Fatalf("confirmation result = %s", toolbar.last.title)
	}
}
