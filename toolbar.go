package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"adbq/internal/adb"
	"adbq/internal/menubar"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const recentAppLimit = 10

// Frida history is persisted on disk. Only Android package identifiers may
// become arguments to the existing package lifecycle shell commands.
var toolbarPackage = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)*$`)

type toolbarTarget struct {
	device  adb.Device
	history adb.FridaHistoryEntry
}

type toolbarAction struct {
	title   string
	icon    string
	visible func(adb.FridaHistoryEntry) bool
	preview func(adb.AppCommands) []string
	confirm bool
	timeout time.Duration
	run     func(context.Context, toolbarTarget) ([]string, error)
}

type toolbarResult struct {
	title    string
	icon     string
	target   toolbarTarget
	detail   string
	commands []string
}

type quickToolbar struct {
	ctx           context.Context
	devices       func() []adb.Device
	history       func() []adb.FridaHistoryEntry
	actions       []toolbarAction
	deviceActions func(string) []toolbarAction
	confirm       func(toolbarTarget, []string) (bool, error)
	showResult    func(toolbarResult)
	showWindow    func()
	quit          func()

	mu         sync.Mutex
	selected   string
	seenDevice bool
	busy       map[[2]string]bool
	last       *toolbarResult
}

func (a *App) mountToolbar(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	t := &quickToolbar{
		ctx: ctx, devices: a.devices.Devices, history: a.ListFridaHistory,
		actions: a.toolbarActions(), deviceActions: a.toolbarDeviceActions, busy: make(map[[2]string]bool),
		confirm: func(target toolbarTarget, commands []string) (bool, error) {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			response, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
				Type:  runtime.WarningDialog,
				Title: fmt.Sprintf("Clear data for %s?", target.history.Package),
				Message: fmt.Sprintf("Device: %s\n\nThis permanently deletes the app’s settings, cache, and local data.\n\n%s",
					toolbarDeviceName(target.device), strings.Join(commands, "\n")),
				Buttons: []string{"Cancel", "Clear data"}, DefaultButton: "Cancel", CancelButton: "Cancel",
			})
			return response == "Clear data", err
		},
		showResult: func(result toolbarResult) {
			if ctx.Err() == nil {
				showToolbarResult(ctx, result)
			}
		},
		showWindow: func() { runtime.Show(ctx); runtime.WindowShow(ctx) },
		quit:       func() { runtime.Quit(ctx) },
	}
	unmount := menubar.Mount(t.menu)
	return func() {
		cancel()
		unmount()
	}
}

func showToolbarResult(ctx context.Context, result toolbarResult) {
	message := "Device: " + toolbarDeviceName(result.target.device)
	if pkg := result.target.history.Package; pkg != "" {
		message = pkg + "\n" + message
	}
	if result.detail != "" {
		message += "\n\n" + result.detail
	}
	commands := strings.Join(result.commands, "\n")
	buttons := []string{"Close"}
	if commands != "" {
		message += "\n\nCommands\n" + commands
		buttons = append(buttons, "Copy commands")
	}
	style := runtime.InfoDialog
	if result.icon == "exclamationmark.triangle" {
		style = runtime.ErrorDialog
	}
	response, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type: style, Title: result.title, Message: message, Buttons: buttons,
		DefaultButton: "Close", CancelButton: "Close",
	})
	if err == nil && response == "Copy commands" {
		_ = runtime.ClipboardSetText(ctx, commands)
	}
}

func (a *App) toolbarDeviceActions(serial string) []toolbarAction {
	if a.ScrcpyActive(serial) {
		return []toolbarAction{{
			title: "Stop scrcpy", icon: "stop",
			run: func(_ context.Context, target toolbarTarget) ([]string, error) {
				return nil, a.StopScrcpy(target.device.ID)
			},
		}}
	}
	return []toolbarAction{{
		title: "Start scrcpy", icon: "display",
		run: func(ctx context.Context, target toolbarTarget) ([]string, error) {
			err := a.startScrcpy(ctx, target.device.ID)
			if command := a.ScrcpyCommand(target.device.ID); command != "" {
				return []string{command}, err
			}
			return nil, err
		},
	}}
}

func (a *App) toolbarActions() []toolbarAction {
	return []toolbarAction{
		a.toolbarFridaAction("Start with Frida", "bolt", "spawn", nil),
		a.toolbarFridaAction("Attach with Frida", "link", "attach", func(entry adb.FridaHistoryEntry) bool {
			return entry.Mode == "attach"
		}),
		{
			title: "Launch", icon: "play", preview: func(c adb.AppCommands) []string { return c.Launch },
			run: a.toolbarADBAction(a.client.LaunchApp, adb.DomApps),
		},
		{
			title: "Restart", icon: "arrow.clockwise",
			preview: func(c adb.AppCommands) []string { return append(c.ForceStop, c.Launch...) },
			run:     a.toolbarADBAction(a.client.RestartApp, adb.DomApps),
		},
		{
			title: "Clear data", icon: "trash", confirm: true,
			preview: func(c adb.AppCommands) []string { return c.Clear },
			run:     a.toolbarADBAction(a.client.ClearApp, adb.DomApps, adb.DomStorage),
		},
	}
}

func (a *App) toolbarFridaAction(title, icon, mode string, visible func(adb.FridaHistoryEntry) bool) toolbarAction {
	return toolbarAction{
		title: title, icon: icon, visible: visible, timeout: 90 * time.Second,
		run: func(ctx context.Context, target toolbarTarget) ([]string, error) {
			target.history.Mode = mode
			session, err := a.repeatFridaSession(ctx, target.device.ID, target.history)
			if err != nil {
				return nil, err
			}
			info := session.Info()
			runtime.EventsEmit(a.ctx, "frida:session-started", info)
			err = session.WaitReady(ctx)
			if err != nil {
				session.Stop()
			}
			return info.Commands.Runner, err
		},
	}
}

func (a *App) toolbarADBAction(
	run func(context.Context, string, string) (string, error), domains ...adb.Domain,
) func(context.Context, toolbarTarget) ([]string, error) {
	return func(ctx context.Context, target toolbarTarget) ([]string, error) {
		defer a.touch(target.device.ID, domains...)
		_, err := run(ctx, target.device.ID, target.history.Package)
		return nil, err
	}
}

func toolbarDeviceName(device adb.Device) string {
	name := device.Label
	if name == "" {
		name = device.Model
	}
	if name == "" || name == device.ID {
		return device.ID
	}
	return name + " (" + device.ID + ")"
}

func onlineToolbarDevice(devices []adb.Device, serial string) adb.Device {
	for _, device := range devices {
		if device.ID == serial && device.Online {
			return device
		}
	}
	return adb.Device{}
}

func (t *quickToolbar) menu() []menubar.Item {
	devices, history := t.devices(), t.history()

	t.mu.Lock()
	defer t.mu.Unlock()
	var online []adb.Device
	for _, device := range devices {
		if device.Online {
			online = append(online, device)
		}
	}
	if !t.seenDevice && len(online) > 0 {
		t.seenDevice = true
		if len(online) == 1 {
			t.selected = online[0].ID
		}
	}
	choices := make([]menubar.Item, 0, len(online))
	for _, device := range online {
		choices = append(choices, menubar.Item{
			Title: toolbarDeviceName(device), Checked: device.ID == t.selected,
			Run: func() { t.selectDevice(device.ID) },
		})
	}
	selected := onlineToolbarDevice(online, t.selected)
	deviceTitle := "Choose a device"
	switch {
	case selected.ID != "":
		deviceTitle = toolbarDeviceName(selected)
	case t.selected != "":
		deviceTitle = t.selected + " (disconnected)"
	case len(choices) == 0:
		deviceTitle = "No online devices"
	}
	rows := []menubar.Item{
		{Title: "Device: " + deviceTitle, Icon: "iphone", Disabled: len(choices) == 0, Children: choices},
	}
	if t.deviceActions != nil {
		target := toolbarTarget{device: selected}
		for _, action := range t.deviceActions(selected.ID) {
			rows = append(rows, menubar.Item{
				Title: action.title, Icon: action.icon, Disabled: selected.ID == "" || t.busy[target.key()],
				Run: func() { t.run(action, target) },
			})
		}
	}
	rows = append(rows, menubar.Item{Separator: true}, menubar.Item{Title: "Recent Apps", Disabled: true})
	if len(history) == 0 {
		rows = append(rows, menubar.Item{Title: "No Frida history yet", Disabled: true})
	}
	for _, entry := range history[:min(len(history), recentAppLimit)] {
		target := toolbarTarget{device: selected, history: entry}
		pending := t.busy[target.key()]
		children := make([]menubar.Item, 0, len(t.actions)+1)
		if pending {
			children = append(children, menubar.Item{Title: "Action in progress…", Disabled: true})
		}
		for _, action := range t.actions {
			if action.visible != nil && !action.visible(entry) {
				continue
			}
			label := action.title
			if action.confirm {
				label += "…"
			}
			children = append(children, menubar.Item{
				Title: label, Icon: action.icon, Disabled: selected.ID == "" || pending || !toolbarPackage.MatchString(entry.Package),
				Run: func() { t.run(action, target) },
			})
		}
		rows = append(rows, menubar.Item{Title: entry.Package, Icon: "app", Children: children})
	}
	if t.last != nil {
		result := *t.last
		rows = append(rows, menubar.Item{Separator: true}, menubar.Item{
			Title: result.title + "…", Icon: result.icon,
			Run: func() { t.showResult(result) },
		})
	}
	return append(rows,
		menubar.Item{Separator: true},
		menubar.Item{Title: "Open adbq", Icon: "macwindow", Run: t.showWindow},
		menubar.Item{Title: "Quit adbq", Icon: "power", Run: t.quit},
	)
}

func (t *quickToolbar) selectDevice(serial string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if onlineToolbarDevice(t.devices(), serial).ID != "" {
		t.selected = serial
		t.seenDevice = true
	}
}

func (target toolbarTarget) key() [2]string {
	return [2]string{target.device.ID, target.history.Package}
}

func (t *quickToolbar) checkTarget(target toolbarTarget) error {
	current := onlineToolbarDevice(t.devices(), target.device.ID)
	if current.ID == "" || (target.device.Transport != "" && current.Transport != target.device.Transport) {
		return fmt.Errorf("device %s disconnected or changed connection. Reopen the menu and try again", target.device.ID)
	}
	return t.ctx.Err()
}

func (t *quickToolbar) run(action toolbarAction, target toolbarTarget) {
	pkg := target.history.Package
	if target.device.ID == "" || (pkg != "" && !toolbarPackage.MatchString(pkg)) || t.ctx.Err() != nil {
		return
	}
	key := target.key()
	result := toolbarResult{title: action.title + " in progress", icon: "hourglass", target: target}
	if action.preview != nil {
		serial, pkg := target.device.ID, target.history.Package
		result.commands = action.preview(adb.AppCommandsFor(serial, pkg, pkg, adb.PlainRenderer(serial)))
	}
	t.mu.Lock()
	if t.busy[key] {
		t.mu.Unlock()
		return
	}
	t.busy[key], t.last = true, &result
	t.mu.Unlock()

	detail := result
	defer func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		delete(t.busy, key)
		if t.last == &result {
			t.last = &detail
		}
	}()

	err := t.checkTarget(target)
	if err == nil && action.confirm {
		var ok bool
		ok, err = t.confirm(target, result.commands)
		if err == nil && !ok {
			detail.title, detail.icon = action.title+" canceled", "xmark.circle"
			return
		}
		if err == nil {
			err = t.checkTarget(target)
		}
	}
	if err == nil {
		timeout := action.timeout
		if timeout == 0 {
			timeout = 20 * time.Second
		}
		ctx, cancel := context.WithTimeout(t.ctx, timeout)
		defer cancel()
		commands, runErr := action.run(ctx, target)
		if len(commands) > 0 {
			detail.commands = commands
		}
		err = runErr
	}
	if err != nil {
		detail.title, detail.icon, detail.detail = action.title+" failed", "exclamationmark.triangle", err.Error()
		return
	}
	detail.title, detail.icon = action.title+" completed", "checkmark.circle"
}
