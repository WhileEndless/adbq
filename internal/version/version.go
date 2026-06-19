// Package version is the single source of truth for the build's semver tag, so
// the UI (App.Version binding), the `adbq --version` flag, and the released
// git tag all agree.
//
// To cut a release, bump the literal below and create a matching git tag, e.g.
//
//	# edit Version to "v0.2.0"
//	git commit -am "release: v0.2.0"
//	git tag -a v0.2.0 -m "v0.2.0"
//
// The release CI fails if the pushed tag does not match this value, so this
// file stays the one place a version is ever edited. Version is a var (not a
// const) only so a build may optionally stamp it via
// -ldflags "-X adbq/internal/version.Version=...".
package version

// Version is bumped with each user-facing release. This literal is canonical.
var Version = "v0.1.5-beta"
