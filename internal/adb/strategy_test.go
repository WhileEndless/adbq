package adb

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const testSerial = "test-device"

// fakeStrategy is a Strategy that records how often it ran, so tests can assert
// on selection and caching rather than on device output.
type fakeStrategy struct {
	name string
	req  Requirements
	val  string
	err  error
	runs *int
}

func (f fakeStrategy) Name() string           { return f.name }
func (f fakeStrategy) Requires() Requirements { return f.req }
func (f fakeStrategy) Run(context.Context, *Client, string) (string, error) {
	*f.runs++
	return f.val, f.err
}

// testClient returns a Client with pre-seeded capabilities so resolving needs
// no device.
func testClient(caps *Capabilities) *Client {
	if caps.Has == nil {
		caps.Has = map[string]bool{}
	}
	return &Client{caps: map[string]*Capabilities{testSerial: caps}}
}

func TestResolverGatesOnCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		caps     *Capabilities
		modern   Requirements
		wantVal  string
		wantRuns [2]int // modern, legacy
	}{
		{
			name:     "modern path taken when SDK is high enough",
			caps:     &Capabilities{SDK: 30, Has: map[string]bool{"cmd": true}},
			modern:   Requirements{MinSDK: 30, Bins: []string{"cmd"}},
			wantVal:  "modern",
			wantRuns: [2]int{1, 0},
		},
		{
			name:     "older SDK falls back to the legacy path",
			caps:     &Capabilities{SDK: 28, Has: map[string]bool{"cmd": true}},
			modern:   Requirements{MinSDK: 30, Bins: []string{"cmd"}},
			wantVal:  "legacy",
			wantRuns: [2]int{0, 1},
		},
		{
			name:     "unknown SDK is treated as old",
			caps:     &Capabilities{},
			modern:   Requirements{MinSDK: 30},
			wantVal:  "legacy",
			wantRuns: [2]int{0, 1},
		},
		{
			name:     "missing binary gates the strategy out",
			caps:     &Capabilities{SDK: 33},
			modern:   Requirements{MinSDK: 30, Bins: []string{"cmd"}},
			wantVal:  "legacy",
			wantRuns: [2]int{0, 1},
		},
		{
			name:     "MaxSDK excludes newer releases",
			caps:     &Capabilities{SDK: 33},
			modern:   Requirements{MaxSDK: 30},
			wantVal:  "legacy",
			wantRuns: [2]int{0, 1},
		},
		{
			name:     "MaxSDK includes its own level",
			caps:     &Capabilities{SDK: 30},
			modern:   Requirements{MaxSDK: 30},
			wantVal:  "modern",
			wantRuns: [2]int{1, 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var mRuns, lRuns int
			r := NewResolver("test.fact",
				fakeStrategy{name: "modern", req: tc.modern, val: "modern", runs: &mRuns},
				fakeStrategy{name: "legacy", val: "legacy", runs: &lRuns},
			)
			got, err := r.Resolve(context.Background(), testClient(tc.caps), testSerial, "")
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got != tc.wantVal {
				t.Errorf("Resolve() = %q, want %q", got, tc.wantVal)
			}
			if mRuns != tc.wantRuns[0] || lRuns != tc.wantRuns[1] {
				t.Errorf("runs = modern:%d legacy:%d, want modern:%d legacy:%d",
					mRuns, lRuns, tc.wantRuns[0], tc.wantRuns[1])
			}
		})
	}
}

func TestResolverRootGate(t *testing.T) {
	var rootRuns, plainRuns int
	c := testClient(&Capabilities{SDK: 33})
	// Seed the su-style cache so the gate resolves without probing a device.
	c.suStyles = map[string]suStyle{testSerial: suSimple}
	r := NewResolver("test.fact",
		fakeStrategy{name: "needs-root", req: Requirements{Root: true}, val: "root", runs: &rootRuns},
		fakeStrategy{name: "plain", val: "plain", runs: &plainRuns},
	)
	got, err := r.Resolve(context.Background(), c, testSerial, "")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "root" {
		t.Errorf("Resolve() = %q, want root — the gate should pass on a rooted device", got)
	}
	if rootRuns != 1 || plainRuns != 0 {
		t.Errorf("runs = root:%d plain:%d, want root:1 plain:0", rootRuns, plainRuns)
	}
}

func TestResolverNoEligibleStrategy(t *testing.T) {
	var runs int
	r := NewResolver("test.fact",
		fakeStrategy{name: "modern", req: Requirements{MinSDK: 30}, val: "modern", runs: &runs},
	)
	_, err := r.Resolve(context.Background(), testClient(&Capabilities{SDK: 21}), testSerial, "")
	if !errors.Is(err, ErrNoStrategy) {
		t.Fatalf("error = %v, want ErrNoStrategy", err)
	}
	if runs != 0 {
		t.Errorf("gated-out strategy ran %d times", runs)
	}
}

func TestResolverRulesOutUnsupportedOnce(t *testing.T) {
	var mRuns, lRuns int
	c := testClient(&Capabilities{SDK: 33})
	r := NewResolver("test.fact",
		fakeStrategy{name: "modern", err: ErrUnsupported, runs: &mRuns},
		fakeStrategy{name: "legacy", val: "legacy", runs: &lRuns},
	)
	for i := range 3 {
		got, err := r.Resolve(context.Background(), c, testSerial, "")
		if err != nil {
			t.Fatalf("call %d: Resolve() error = %v", i, err)
		}
		if got != "legacy" {
			t.Fatalf("call %d: Resolve() = %q, want legacy", i, got)
		}
	}
	// The unsupported path is permanent for the device: probed once, then never
	// paid for again.
	if mRuns != 1 {
		t.Errorf("unsupported strategy ran %d times, want 1", mRuns)
	}
	if lRuns != 3 {
		t.Errorf("legacy strategy ran %d times, want 3", lRuns)
	}
}

func TestResolverTransientErrorKeepsStrategyEligible(t *testing.T) {
	var mRuns, lRuns int
	c := testClient(&Capabilities{SDK: 33})
	r := NewResolver("test.fact",
		fakeStrategy{name: "flaky", err: errors.New("device offline"), runs: &mRuns},
		fakeStrategy{name: "legacy", val: "legacy", runs: &lRuns},
	)
	for i := range 2 {
		if _, err := r.Resolve(context.Background(), c, testSerial, ""); err != nil {
			t.Fatalf("call %d: Resolve() error = %v", i, err)
		}
	}
	// A transient failure must not permanently demote the better path.
	if mRuns != 2 {
		t.Errorf("flaky strategy ran %d times, want 2", mRuns)
	}
}

func TestResolverAllStrategiesFail(t *testing.T) {
	var runs int
	sentinel := errors.New("boom")
	r := NewResolver("test.fact",
		fakeStrategy{name: "only", err: sentinel, runs: &runs},
	)
	_, err := r.Resolve(context.Background(), testClient(&Capabilities{SDK: 33}), testSerial, "")
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want it to wrap %v", err, sentinel)
	}
}

func TestResolverCostlyStrategyCachesOnFreshKey(t *testing.T) {
	var runs int
	c := testClient(&Capabilities{})
	r := NewResolver("test.fact",
		fakeStrategy{name: "costly", req: Requirements{Costly: true}, val: "value", runs: &runs},
	)
	for range 5 {
		if _, err := r.Resolve(context.Background(), c, testSerial, "key-1"); err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
	}
	if runs != 1 {
		t.Fatalf("costly strategy ran %d times on an unchanged key, want 1", runs)
	}

	// A moved key means the underlying state changed: the value must be re-read.
	if _, err := r.Resolve(context.Background(), c, testSerial, "key-2"); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if runs != 2 {
		t.Errorf("costly strategy ran %d times after the key moved, want 2", runs)
	}
}

func TestResolverCostlyStrategyCachesEmptyResult(t *testing.T) {
	var runs int
	c := testClient(&Capabilities{})
	r := NewResolver("test.fact",
		fakeStrategy{name: "costly", req: Requirements{Costly: true}, val: "", runs: &runs},
	)
	// An empty key with an empty value is the "state is absent" case, and must
	// not re-probe on every poll.
	for range 3 {
		if _, err := r.Resolve(context.Background(), c, testSerial, ""); err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
	}
	if runs != 1 {
		t.Errorf("costly strategy ran %d times, want 1", runs)
	}
}

func TestResolverCheapStrategyIsNotCached(t *testing.T) {
	var runs int
	c := testClient(&Capabilities{})
	r := NewResolver("test.fact",
		fakeStrategy{name: "cheap", val: "value", runs: &runs},
	)
	for range 3 {
		if _, err := r.Resolve(context.Background(), c, testSerial, "key-1"); err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
	}
	if runs != 3 {
		t.Errorf("cheap strategy ran %d times, want 3 (no caching)", runs)
	}
}

func TestResolverInvalidateForcesReread(t *testing.T) {
	var runs int
	c := testClient(&Capabilities{})
	r := NewResolver("test.fact",
		fakeStrategy{name: "costly", req: Requirements{Costly: true}, val: "value", runs: &runs},
	)
	if _, err := r.Resolve(context.Background(), c, testSerial, "key-1"); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	r.Invalidate(c, testSerial)
	if _, err := r.Resolve(context.Background(), c, testSerial, "key-1"); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if runs != 2 {
		t.Errorf("costly strategy ran %d times across an Invalidate, want 2", runs)
	}
}

func TestInvalidateFactsDropsSelection(t *testing.T) {
	var mRuns, lRuns int
	c := testClient(&Capabilities{SDK: 33})
	r := NewResolver("test.fact",
		fakeStrategy{name: "modern", err: ErrUnsupported, runs: &mRuns},
		fakeStrategy{name: "legacy", val: "legacy", runs: &lRuns},
	)
	if _, err := r.Resolve(context.Background(), c, testSerial, ""); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	// A device that comes back may be a different build, so the ruled-out set
	// must not survive a disconnect.
	c.InvalidateFacts(testSerial)
	if _, err := r.Resolve(context.Background(), c, testSerial, ""); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if mRuns != 2 {
		t.Errorf("modern strategy ran %d times, want 2 (re-probed after invalidation)", mRuns)
	}
}

func TestInvalidateFactsIsPerSerial(t *testing.T) {
	var runs int
	c := testClient(&Capabilities{})
	c.caps["other-device"] = &Capabilities{Has: map[string]bool{}}
	r := NewResolver("test.fact",
		fakeStrategy{name: "costly", req: Requirements{Costly: true}, val: "value", runs: &runs},
	)
	for _, s := range []string{testSerial, "other-device"} {
		if _, err := r.Resolve(context.Background(), c, s, "key-1"); err != nil {
			t.Fatalf("Resolve(%s) error = %v", s, err)
		}
	}
	if runs != 2 {
		t.Fatalf("runs = %d, want 2 (one per device)", runs)
	}
	c.InvalidateFacts(testSerial)
	for _, s := range []string{testSerial, "other-device"} {
		if _, err := r.Resolve(context.Background(), c, s, "key-1"); err != nil {
			t.Fatalf("Resolve(%s) error = %v", s, err)
		}
	}
	if runs != 3 {
		t.Errorf("runs = %d, want 3 — only the invalidated device re-reads", runs)
	}
}

func TestResolverNilClient(t *testing.T) {
	var runs int
	r := NewResolver("test.fact", fakeStrategy{name: "only", val: "v", runs: &runs})
	if _, err := r.Resolve(context.Background(), nil, testSerial, ""); !errors.Is(err, ErrNoStrategy) {
		t.Fatalf("error = %v, want ErrNoStrategy", err)
	}
}

func TestResolverErrorNamesTheFact(t *testing.T) {
	r := NewResolver("net.ssid",
		fakeStrategy{name: "modern", req: Requirements{MinSDK: 30}, runs: new(int)},
	)
	_, err := r.Resolve(context.Background(), testClient(&Capabilities{SDK: 21}), testSerial, "")
	if err == nil || !strings.Contains(err.Error(), "net.ssid") {
		t.Errorf("error = %v, want it to name the fact", err)
	}
}
