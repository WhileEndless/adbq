package main

import (
	"adbq/internal/adb"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Caching device state for minutes instead of seconds is what keeps adbq from
// spawning an `adb` process several times a second. That is only honest if
// every action which changes the device says what it changed — otherwise the
// user uninstalls an app and watches it sit in the list for ten minutes.
//
// adbq performs those changes itself, so this is bookkeeping rather than
// guesswork: a mutating binding declares the domains it dirties (see
// internal/adb/cachedomain.go) and both caches drop them.
//
// The rule is enforced, not documented: invalidation_test.go walks the AST of
// every exported *App method and fails the build when one that mutates device
// state has no touch call and no allowlist entry. A convention nobody checks is
// how this codebase came to cache without invalidating at all.

// cacheInvalidateEvent is the Wails event the frontend cache listens on. The
// backend is the only side that knows an install finished, so it is the side
// that gets to say the app list is stale.
const cacheInvalidateEvent = "cache:invalidate"

// cacheInvalidation is the event payload. Serial is empty for host-scoped
// domains (SDK, jadx, scrcpy, AVDs), which are not keyed by device.
type cacheInvalidation struct {
	Serial  string       `json:"serial"`
	Domains []adb.Domain `json:"domains"`
}

// touch marks the given domains stale for serial: it drops the backend's cached
// facts and tells the frontend to drop its own.
//
// Call it deferred, at the top of a mutating binding:
//
//	func (a *App) UninstallApp(serial, pkg string) (string, error) {
//	    defer a.touch(serial, adb.DomApps, adb.DomStorage)
//	    ...
//	}
//
// Deferred and unconditional, including on the error path, on purpose: a failed
// install can still have changed the device (a partially written file, a
// session that half-committed), and re-reading costs one round trip while
// trusting a stale value costs the user's trust in the screen.
//
// Pass an empty serial for host-scoped domains.
func (a *App) touch(serial string, domains ...adb.Domain) {
	if len(domains) == 0 {
		return
	}
	a.client.InvalidateDomains(serial, domains...)
	// a.ctx is nil until startup runs; tests construct an App without it.
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, cacheInvalidateEvent, cacheInvalidation{
		Serial:  serial,
		Domains: domains,
	})
}

// touchAll invalidates everything for a device. Used where the device's whole
// identity may have moved under us — a reboot can bring back a different build,
// a newly granted root, another IP — so even the properties treated as fixed
// for a connected lifetime have to go.
func (a *App) touchAll(serial string) {
	a.touch(serial, adb.DomainReboot...)
}

// CacheDomains lets the frontend enumerate the domains it may be told about,
// so its key→domain mapping can be checked against the backend's list rather
// than duplicating it as a literal that quietly drifts.
func (a *App) CacheDomains() []adb.Domain { return adb.AllDomains }
