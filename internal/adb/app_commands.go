package adb

import (
	"context"
	"fmt"
)

// The remote commands behind the Apps screen's per-app actions.
//
// Each one is a function rather than an inline string so the code that runs it
// and the preview the user reads come from the same place; a preview that has
// drifted from the action is worse than no preview at all (CLAUDE.md §4.1).

func appLaunchRemote(pkg string) string {
	return "monkey -p " + pkg + " -c android.intent.category.LAUNCHER 1"
}

func appForceStopRemote(pkg string) string { return "am force-stop " + pkg }

func appClearRemote(pkg string) string { return "pm clear " + pkg }

// appDataArchive is where ExportAppData stages the archive before pulling it:
// /sdcard is world-readable, so the pull needs no root of its own.
func appDataArchive(pkg string) string { return "/sdcard/adbq-appdata-" + pkg + ".tar.gz" }

// appDataTarRemote archives the app's private data. The fallback form exists
// because `tar -C` is missing from some toybox builds.
func appDataTarRemote(pkg string) string {
	remote := appDataArchive(pkg)
	return fmt.Sprintf("tar -czf %s -C /data/data %s 2>&1 || tar -czf %s /data/data/%s 2>&1",
		remote, pkg, remote, pkg)
}

// AppCommands is what each action in the Apps detail panel will run for one
// package. Every field is a complete, paste-ready step list.
type AppCommands struct {
	Pkg        string   `json:"pkg"`
	Launch     []string `json:"launch"`
	ForceStop  []string `json:"forceStop"`
	Clear      []string `json:"clear"`
	Uninstall  []string `json:"uninstall"`
	ExportData []string `json:"exportData"`
}

// AppCommandsFor builds the panel's command previews. baseName is the file name
// an export would suggest (package plus version), so the pull line is the line
// the user can actually paste; render supplies the device's `su` form.
func AppCommandsFor(serial, pkg, baseName string, render CommandRenderer) AppCommands {
	if baseName == "" {
		baseName = pkg
	}
	archive := appDataArchive(pkg)
	return AppCommands{
		Pkg:       pkg,
		Launch:    []string{render(appLaunchRemote(pkg), false)},
		ForceStop: []string{render(appForceStopRemote(pkg), false)},
		Clear:     []string{render(appClearRemote(pkg), false)},
		Uninstall: []string{DeviceCommandText(serial, "uninstall", pkg)},
		ExportData: []string{
			render(appDataTarRemote(pkg), true),
			DeviceCommandText(serial, "pull", archive, baseName+".tar.gz"),
			render("rm "+archive, true),
		},
	}
}

// AppCommandsFor is the device-aware entry point: same builder, but the root
// steps are rendered with the `su` form this device accepted.
func (c *Client) AppCommandsFor(ctx context.Context, serial, pkg string) AppCommands {
	return AppCommandsFor(serial, pkg, c.ExportBaseNameFor(ctx, serial, pkg), c.Renderer(ctx, serial))
}
