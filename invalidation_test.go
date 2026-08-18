package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
)

// This test is the reason the caching in this app can be trusted.
//
// adbq keeps device state cached for minutes so it does not spawn an `adb`
// process several times a second. That is only safe while every action which
// changes the device declares what it changed. Before this existed the codebase
// had ~240 bindings and essentially no invalidation: install, uninstall, cert
// install, iptables apply and reboot all left every cache untouched. Nobody
// noticed because the TTLs were seconds — short enough to hide the bug, long
// enough to be useless.
//
// A documented convention would decay the same way. So the rule is checked by
// walking the AST: a binding whose name says it mutates device state must
// either call a.touch/a.touchAll, or appear below with a reason.
//
// If this test fails, the fix is almost always to add one line:
//
//	defer a.touch(serial, adb.DomApps)
//
// and only rarely to add an allowlist entry.

// mutatingVerbs are the name prefixes that mark a binding as changing state.
// Matching on the name is crude, but it is the property that survives: a method
// called UninstallApp will always be a mutation, whatever its body becomes.
// Over-matching is handled by the allowlist, which is cheap; under-matching
// would silently reopen the hole, which is not.
var mutatingVerbs = []string{
	"Add", "Append", "Apply", "Bind", "Chmod", "Chown", "Clear", "Connect",
	"Create", "Delete", "Disconnect", "Download", "Enable", "Ensure", "Export",
	"Flush", "Force", "Import", "Insert", "Install", "Kill", "Launch", "Mkdir",
	"Move", "Pick", "Power", "Pull", "Push", "Reboot", "Register", "Remove",
	"Restart", "Restore", "Root", "Save", "Set", "Start", "Stop", "Take",
	"Tcpip", "Undo", "Uninstall", "Update", "Write",
}

// exemptBindings are methods whose names match a mutating verb but which do not
// change any cached device state. Every entry needs a reason — an allowlist
// without reasons is just a list of things nobody re-examined.
var exemptBindings = map[string]string{
	// ── Pure host-side UI/session state; nothing on the device moves. ──
	"SetLogcatSystem":         "flips a host-side line filter on a running feed; no device call",
	"ClearLogcat":             "drops buffered lines between device and UI; device log untouched",
	"StopLogcat":              "tears down a host-side subscription",
	"EnsureLogcat":            "starts/keeps a read-only log stream",
	"ClearShellHistory":       "deletes host-side scrollback files",
	"ClearFridaHistory":       "deletes a host-side history list",
	"RemoveFridaHistory":      "deletes a host-side history entry",
	"ClearEmulatorLog":        "clears a host-side log buffer",
	"ClearStagedApks":         "deletes host-side staged files",
	"RemoveTask":              "drops a completed task from the host-side task list",
	"CancelTask":              "cancels a host-side context; the underlying op does its own touching",
	"StartProcStream":         "read-only /proc sampling",
	"StopProcStream":          "stops read-only /proc sampling",
	"StartDNSSniffer":         "read-only packet observation",
	"StopDNSSniffer":          "stops read-only packet observation",
	"StartLiveCapture":        "read-only packet observation",
	"StopLiveCapture":         "stops read-only packet observation",
	"AdoptExternalCapture":    "records an already-running capture in the session list",
	"KillExternalCapture":     "kills a foreign tcpdump; captures no state adbq caches",
	"ResizeShell":             "sends a window-size change to a PTY",
	"WriteShell":              "writes user keystrokes to a PTY; what they do is unknowable here",
	"CloseShell":              "closes a host-side PTY",
	"StartScrcpy":             "host-side mirror process; no device state",
	"StopScrcpy":              "host-side mirror process; no device state",
	"SaveScreenshotAs":        "writes a host-side file",
	"SaveLivePcap":            "writes a host-side file",
	"TakeScreenshot":          "reads the framebuffer",
	"PullCapture":             "copies a device file to the host",
	"PullFileWithPicker":      "copies a device file to the host",
	"ExportAPK":               "copies an apk to the host",
	"ExportApks":              "copies apks to the host",
	"ExportAppBinaries":       "copies native libs to the host",
	"ExportAppDataWithPicker": "copies app data to the host",
	"ExportIptables":          "renders the current ruleset as text",
	"StartScreenRecord":       "writes a device-side mp4 in a scratch path adbq does not cache",
	"StopScreenRecord":        "finalises and pulls that mp4",
	"ResetADBStats":           "resets the host-side spawn counter",

	// ── Host settings and tool management. The ones that back a cached read
	//    (SDK, jadx, AVDs) declare DomSDK/DomJadx/DomAVD themselves and are not
	//    listed here; these are the remainder, which nothing caches. ──
	"SetJavaPath":                  "host setting; the JRE is not device state",
	"PickJavaPath":                 "file dialog; delegates to SetJavaPath",
	"PickApkFile":                  "file dialog only",
	"PickAndInstallAPK":            "file dialog; delegates to the install binding, which touches",
	"PickExternalFridaInterpreter": "file dialog; registers a host interpreter",
	"DownloadRootAVD":              "downloads a host-side script; no adbq cache describes it",
	"RemoveRootAVD":                "deletes that host-side script",
	"SetFridaManagedEnabled":       "host policy flag",
	"RemoveFridaRuntime":           "host-side venv removal",
	"EnsureFridaVenv":              "host-side venv creation",
	"RegisterExternalFrida":        "registers a host interpreter",
	"SaveFridaScript":              "host-side script library",
	"DeleteFridaScript":            "host-side script library",
	"ImportCodeshareScript":        "host-side script library",
	"StartFridaSession":            "host-side driver process; DomFrida covers the server, not a session",
	"StopFridaSession":             "host-side driver process",
	"RemoveFridaSession":           "host-side session bookkeeping",
	"StartAppWithFrida":            "orchestrates bindings that touch on their own",
	"OpenAndroidStudio":            "launches a host application",
	"OpenInJadx":                   "launches a host application",
	"OpenPath":                     "opens a host path in the file manager",
	"RevealPath":                   "reveals a host path in the file manager",
	"OpenShell":                    "opens a PTY",

	// ── Profiles and device records: host-side stores, not device state. ──
	"SaveProfile":              "host-side profile store",
	"DeleteProfile":            "host-side profile store",
	"BindDeviceProfile":        "host-side binding record",
	"BindDeviceProfileByKey":   "host-side binding record",
	"RegisterDevice":           "host-side device record",
	"CaptureProfileFromDevice": "reads the device into a host-side profile",

	// ── Command-preview and planning bindings: they render text, run nothing. ──
	"RootAVDCommand":            "renders a command string",
	"RootAVDAdvice":             "renders advice text",
	"CreateAVDCommand":          "renders a command string",
	"DeleteAVDCommand":          "renders a command string",
	"EmulatorLaunchCommand":     "renders a command string",
	"ScrcpyCommand":             "renders a command string",
	"ConnectCommands":           "renders command strings",
	"TcpdumpInstallCommands":    "renders command strings",
	"PlanApkInstall":            "renders a plan; installs nothing",
	"PlanAppBinaries":           "renders a plan",
	"PlanCertInstall":           "renders a plan",
	"PlanHostsApply":            "renders a plan",
	"PlanJadxOpen":              "renders a plan",
	"PlanTcpdumpAutoInstall":    "renders a plan",
	"PreviewProfile":            "renders a plan",
	"DefaultAVDSpec":            "returns a default struct",
	"ProxyCommand":              "renders a command string",
	"SuggestProxyHost":          "computes a suggestion from host interfaces",
	"StagedApkDir":              "returns a host path",
	"DetectRunningFridaVersion": "reads the running server's version",
	"RootSignals":               "reads root indicators",
	"RootAVDInfo":               "reports the rootAVD tool's status; downloads nothing",
	"SetAppFridaScripts":        "host-side per-app script binding; nothing on the device moves",
}

func TestEveryMutatingBindingDeclaresItsCacheDomains(t *testing.T) {
	fset := token.NewFileSet()
	files := []string{"app.go", "profiles_app.go", "app_invalidate.go"}

	touches := map[string]bool{} // method name → calls a.touch/a.touchAll
	methods := map[string]bool{} // exported methods on *App

	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name == nil || !fn.Name.IsExported() {
				continue
			}
			if !receiverIsApp(fn.Recv) {
				continue
			}
			methods[fn.Name.Name] = true
			if bodyCallsTouch(fn) {
				touches[fn.Name.Name] = true
			}
		}
	}

	if len(methods) == 0 {
		t.Fatal("found no exported *App methods — the AST walk is broken, not the code")
	}

	var missing []string
	for name := range methods {
		if !isMutatingName(name) || touches[name] {
			continue
		}
		if _, exempt := exemptBindings[name]; exempt {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("%d binding(s) look like they change device state but declare no cache domains:\n  %s\n\n"+
			"Add one line at the top of each:\n"+
			"    defer a.touch(serial, adb.DomApps)   // whichever domains it dirties\n\n"+
			"…or, if it genuinely changes nothing adbq caches, add it to exemptBindings\n"+
			"in this file WITH a reason. See internal/adb/cachedomain.go for the domain list.",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// An allowlist rots by accumulating entries for methods that no longer exist,
// and each dead entry is a place a real mutation could later be added under an
// old name and silently skipped.
func TestExemptBindingsAreAllRealMethods(t *testing.T) {
	fset := token.NewFileSet()
	methods := map[string]bool{}
	for _, name := range []string{"app.go", "profiles_app.go", "app_invalidate.go"} {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv != nil && fn.Name.IsExported() && receiverIsApp(fn.Recv) {
				methods[fn.Name.Name] = true
			}
		}
	}
	var stale []string
	for name := range exemptBindings {
		if !methods[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("exemptBindings names %d method(s) that no longer exist — remove them:\n  %s",
			len(stale), strings.Join(stale, "\n  "))
	}
	for name, reason := range exemptBindings {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("exemptBindings[%q] has no reason", name)
		}
	}
}

// The two tests above are only worth having if the machinery under them
// actually detects things. A silent break in receiverIsApp or bodyCallsTouch
// would turn both into unconditional passes — the most expensive kind of green.
func TestDetectionMachineryWorks(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "app.go", nil, 0)
	if err != nil {
		t.Fatalf("parse app.go: %v", err)
	}
	var appMethods, withTouch int
	sawKnownMutator := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name == nil || !receiverIsApp(fn.Recv) {
			continue
		}
		appMethods++
		if bodyCallsTouch(fn) {
			withTouch++
			// UninstallApp is the canonical mutation: if the walker cannot see
			// its touch call, it cannot see anyone's.
			if fn.Name.Name == "UninstallApp" {
				sawKnownMutator = true
			}
		}
	}
	if appMethods < 100 {
		t.Errorf("found only %d *App methods in app.go — receiverIsApp is broken", appMethods)
	}
	if withTouch < 20 {
		t.Errorf("found only %d methods calling touch — bodyCallsTouch is broken", withTouch)
	}
	if !sawKnownMutator {
		t.Error("UninstallApp is not detected as declaring cache domains; either it lost its " +
			"touch call (a real bug) or bodyCallsTouch stopped working (a worse one)")
	}

	// And the name matcher must actually discriminate.
	if !isMutatingName("UninstallApp") {
		t.Error("isMutatingName missed UninstallApp")
	}
	if isMutatingName("ListApps") || isMutatingName("GetStats") || isMutatingName("DescribeApp") {
		t.Error("isMutatingName matches plain reads; the allowlist would fill with noise")
	}
}

func receiverIsApp(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) != 1 {
		return false
	}
	star, ok := recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	id, ok := star.X.(*ast.Ident)
	return ok && id.Name == "App"
}

func bodyCallsTouch(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "touch" || sel.Sel.Name == "touchAll" {
			found = true
			return false
		}
		return true
	})
	return found
}

func isMutatingName(name string) bool {
	for _, v := range mutatingVerbs {
		if strings.HasPrefix(name, v) {
			return true
		}
	}
	return false
}
