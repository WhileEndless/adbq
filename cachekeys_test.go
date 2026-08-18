package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"adbq/internal/adb"
)

// The frontend cache is only reachable by the backend's invalidation because of
// a naming contract: every key is `<domain>:<serial>[:...]`, built through
// deviceKey()/hostKey() in cache.tsx. A hand-written key still caches and still
// reads back correctly — it just can never be invalidated, so it goes stale
// silently and only in the situations that matter (right after the user changed
// something). That is the worst possible failure mode to leave to code review,
// so it is checked here.
//
// These live in Go because Go is where this repo has a test runner; the
// frontend has no vitest today.

// cacheEntryPoints are the cache.tsx functions whose first argument is a key.
var cacheEntryPoints = []string{
	"useDeviceData", "prefetchData", "getOrFetch", "getCached", "mutateData", "invalidateData",
}

// rawTemplateKey matches a call like `useDeviceData(\`apps:${id}\“ — a template
// literal handed straight to a cache function instead of a built key.
var rawTemplateKey = regexp.MustCompile(
	`(?:` + strings.Join(cacheEntryPoints, "|") + `)\s*(?:<[^>]*>)?\s*\(\s*` + "`")

func frontendSources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	root := filepath.Join("frontend", "src")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".ts" && ext != ".tsx" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(out) == 0 {
		t.Fatalf("no frontend sources found under %s — this test is not checking anything", root)
	}
	return out
}

func TestCacheKeysAreBuiltNotHandWritten(t *testing.T) {
	var bad []string
	for path, src := range frontendSources(t) {
		if strings.HasSuffix(path, "cache.tsx") {
			continue // defines the helpers; its own doc comments show the shape
		}
		for i, line := range strings.Split(src, "\n") {
			if rawTemplateKey.MatchString(line) {
				bad = append(bad, path+":"+strconv.Itoa(i+1)+"  "+strings.TrimSpace(line))
			}
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%d cache call(s) pass a hand-written key:\n  %s\n\n"+
			"Build keys with deviceKey(domain, serial, ...) or hostKey(domain, ...) from cache.tsx.\n"+
			"A hand-written key caches and reads back fine — it just cannot be invalidated,\n"+
			"so it goes stale exactly when the user has changed something.",
			len(bad), strings.Join(bad, "\n  "))
	}
}

// cache.tsx declares the Domain union by hand because TypeScript cannot import
// Go constants. That is a duplicate, and duplicates drift — so it is compared
// against the real list rather than trusted.
func TestFrontendDomainUnionMatchesBackend(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("frontend", "src", "cache.tsx"))
	if err != nil {
		t.Fatalf("read cache.tsx: %v", err)
	}
	_, after, ok := strings.Cut(string(src), "export type Domain =")
	if !ok {
		t.Fatal("cache.tsx no longer declares `export type Domain =` — this test cannot check the union")
	}
	decl, _, ok := strings.Cut(after, ";")
	if !ok {
		t.Fatal("Domain union is not terminated by ';'")
	}

	inTS := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([a-z]+)'`).FindAllStringSubmatch(decl, -1) {
		inTS[m[1]] = true
	}

	var missing, extra []string
	inGo := map[string]bool{}
	for _, d := range adb.AllDomains {
		inGo[string(d)] = true
		if !inTS[string(d)] {
			missing = append(missing, string(d))
		}
	}
	for name := range inTS {
		if !inGo[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("cache.tsx's Domain union is missing %v — the backend can emit these and "+
			"the frontend would silently ignore the invalidation", missing)
	}
	if len(extra) > 0 {
		t.Errorf("cache.tsx's Domain union has %v, which no longer exist in adb.AllDomains — "+
			"keys under them can never be invalidated", extra)
	}
}
