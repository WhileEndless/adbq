package adb

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The driver is Python, so these exercise it the way it actually runs: write the
// embedded source out, import it, and assert on what it composes. No device and
// no frida install are involved — build_agent is pure string work.
func driverHarness(t *testing.T, body string) string {
	t.Helper()
	py, ok := lookTool("python3")
	if !ok {
		t.Skip("python3 not available")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "frida_driver.py"), fridaDriverPy, 0o600); err != nil {
		t.Fatal(err)
	}
	// A stand-in bridge: build_agent only reads and inlines the file, so a marker
	// is enough to count how many copies end up in the agent.
	bridges := filepath.Join(dir, "bridges")
	if err := os.MkdirAll(bridges, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"java.js", "objc.js", "swift.js"} {
		marker := "var bridge = 'BRIDGE_MARKER_" + strings.TrimSuffix(f, ".js") + "';"
		if err := os.WriteFile(filepath.Join(bridges, f), []byte(marker), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	script := "import sys, json\nsys.path.insert(0, " + strconv.Quote(dir) + ")\n" +
		"import frida_driver as d\nBRIDGES = " + strconv.Quote(bridges) + "\n" + body
	cmd := exec.Command(py, "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driver harness failed: %v\n%s", err, out)
	}
	return string(out)
}

// The crash this guards against: frida gives each script its own JS realm, so a
// per-script bridge prologue put a second copy of frida-java-bridge in the
// target process. Two bridges patching ART at once killed the app on startup —
// measured on an API 34 emulator, where one Java-using script worked and two
// terminated the process before any hook ran. One agent, one bridge.
func TestDriverEmitsOneBridgeForManyScripts(t *testing.T) {
	out := driverHarness(t, `
scripts = [
    {"name": "a", "source": "Java.perform(function(){ console.log('a'); });"},
    {"name": "b", "source": "Java.perform(function(){ console.log('b'); });"},
    {"name": "c", "source": "Java.use('x');"},
]
src, owners = d.build_agent(scripts, BRIDGES)
print(json.dumps({"java": src.count("BRIDGE_MARKER_java"),
                  "objc": src.count("BRIDGE_MARKER_objc"),
                  "swift": src.count("BRIDGE_MARKER_swift")}))
`)
	var got map[string]int
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("parse harness output %q: %v", out, err)
	}
	if got["java"] != 1 {
		t.Errorf("java bridge appears %d times in the agent, want exactly 1", got["java"])
	}
	// Bridges nobody references must not be shipped at all.
	if got["objc"] != 0 || got["swift"] != 0 {
		t.Errorf("unreferenced bridges included: objc=%d swift=%d", got["objc"], got["swift"])
	}
}

func TestDriverOmitsBridgeWhenUnreferenced(t *testing.T) {
	out := driverHarness(t, `
scripts = [{"name": "a", "source": "console.log('no bridges here');"}]
src, _ = d.build_agent(scripts, BRIDGES)
print(json.dumps({"java": src.count("BRIDGE_MARKER_java")}))
`)
	var got map[string]int
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("parse harness output %q: %v", out, err)
	}
	if got["java"] != 0 {
		t.Errorf("java bridge shipped for a script that never mentions Java (%d copies)", got["java"])
	}
}

// Merging the scripts costs the per-script identity frida used to stamp on each
// message. Errors are re-attributed by line number, so the line index must point
// at the body that owns it.
func TestDriverAttributesLinesToOwningScript(t *testing.T) {
	out := driverHarness(t, `
scripts = [
    {"name": "first", "source": "line1();\nline2();"},
    {"name": "second", "source": "lineA();\nlineB();"},
]
src, owners = d.build_agent(scripts, BRIDGES)
lines = src.split("\n")
probe = {}
for start, name in owners:
    probe[name] = [lines[start - 1], lines[start]]
print(json.dumps({
    "owners": owners,
    "probe": probe,
    "before": d.owner_of_line(owners, 1),
    "first_body": d.owner_of_line(owners, owners[0][0]),
    "second_body": d.owner_of_line(owners, owners[1][0] + 1),
}))
`)
	var got struct {
		Probe      map[string][]string `json:"probe"`
		Before     string              `json:"before"`
		FirstBody  string              `json:"first_body"`
		SecondBody string              `json:"second_body"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("parse harness output %q: %v", out, err)
	}
	if got.Before != "" {
		t.Errorf("a line before any body attributed to %q, want unattributed", got.Before)
	}
	if got.FirstBody != "first" || got.SecondBody != "second" {
		t.Errorf("line attribution: first=%q second=%q", got.FirstBody, got.SecondBody)
	}
	// The recorded start line must really be that script's first source line,
	// or every reported error would be off by the wrapper.
	if l := got.Probe["first"]; len(l) < 1 || !strings.Contains(l[0], "line1()") {
		t.Errorf("first script's start line is %q, want its first source line", l)
	}
	if l := got.Probe["second"]; len(l) < 1 || !strings.Contains(l[0], "lineA()") {
		t.Errorf("second script's start line is %q, want its first source line", l)
	}
}

// Each body runs in its own scope with shadowed console/send, so one script's
// declarations can't collide with another's and every message stays attributable.
func TestDriverScopesEachScriptSeparately(t *testing.T) {
	out := driverHarness(t, `
scripts = [
    {"name": "a", "source": "var shared = 1;"},
    {"name": "b", "source": "var shared = 2;"},
]
src, _ = d.build_agent(scripts, BRIDGES)
print(json.dumps({"ctx": src.count(d._TAG + "_ctx(")}))
`)
	var got map[string]int
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("parse harness output %q: %v", out, err)
	}
	// One factory definition plus one call per script.
	if got["ctx"] < 2 {
		t.Errorf("expected a per-script context for each body, saw %d call sites", got["ctx"])
	}
}
