package main

import (
	"embed"
	"fmt"
	"os"

	"adbq/internal/version"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

// appIcon is the same artwork the platform packagers read: macOS turns it into
// the bundle's .icns and Windows into the .exe's .ico at build time, but Linux
// has no packaging step to do that, and macOS's About panel is drawn by us. Both
// need the bytes at run time, so they are embedded rather than duplicated.
//
//go:embed build/appicon.png
var appIcon []byte

func main() {
	// Allow checking the build version without launching the GUI, so packaging
	// scripts and users can verify what they have from a terminal.
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" || arg == "version" {
			fmt.Println("adbq", version.Version)
			return
		}
	}

	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:     "adbq — ADB Manager",
		Width:     1320,
		Height:    840,
		MinWidth:  1000,
		MinHeight: 660,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 12, G: 12, B: 16, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		// Per-platform blocks stay separate (CLAUDE.md §3): what each OS needs
		// here genuinely differs.
		//
		// Windows is absent on purpose: its icon is compiled into the executable
		// from build/windows/icon.ico, so there is nothing to set at run time.
		Mac: &mac.Options{
			About: &mac.AboutInfo{
				Title:   "adbq " + version.Version,
				Message: "ADB Manager — every action shows the command it runs.",
				Icon:    appIcon,
			},
		},
		Linux: &linux.Options{
			// Without this the window and the taskbar fall back to a generic
			// icon: there is no bundle or resource section to read one from.
			Icon: appIcon,
			// The name desktop environments group windows under, which is what
			// makes a .desktop entry's icon apply to the running window too.
			ProgramName: "adbq",
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
