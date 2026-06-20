package adb

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Frida CodeShare (https://codeshare.frida.re) integration.
//
// Two surfaces, by design:
//   - Discovery (search/browse) scrapes the site's HTML. There is no JSON search
//     API, so this is best-effort: if the markup changes, parsing yields zero
//     results rather than an error, and the UI still offers import-by-slug.
//   - Source fetch uses the documented JSON endpoint
//     /api/project/<owner>/<slug>/ — authoritative for the script body. We never
//     trust scraped HTML for the source that will eventually run.
//
// All requests are pinned to the codeshare.frida.re host. The fetched source is
// untrusted: it is shown to the user in the editor and only marked trusted on an
// explicit, separate action — it is never executed at fetch time.

const codeshareHost = "https://codeshare.frida.re"

// CodeshareProject is one discovery result (no source body).
type CodeshareProject struct {
	Owner string `json:"owner"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Likes string `json:"likes"`
	Views string `json:"views"`
}

// CodeshareScript is a fetched project including its source body.
type CodeshareScript struct {
	Owner        string `json:"owner"`
	Slug         string `json:"slug"`
	ProjectName  string `json:"projectName"`
	Description  string `json:"description"`
	FridaVersion string `json:"fridaVersion"`
	Likes        int    `json:"likes"`
	Source       string `json:"source"`
	SourceSha    string `json:"sourceSha"`
}

type codeshareProjectJSON struct {
	ProjectName  string `json:"project_name"`
	Description  string `json:"description"`
	Owner        string `json:"owner"`
	Slug         string `json:"slug"`
	FridaVersion string `json:"frida_version"`
	Likes        int    `json:"likes"`
	Source       string `json:"source"`
	Success      *bool  `json:"success"` // present (false) only on the error shape
}

// codeshareGet performs a host-pinned GET with a sane UA and timeout.
func codeshareGet(ctx context.Context, rawURL string) ([]byte, int, error) {
	if !strings.HasPrefix(rawURL, codeshareHost+"/") {
		return nil, 0, fmt.Errorf("refusing to fetch from non-codeshare host: %s", rawURL)
	}
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, "GET", rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "adbq/codeshare")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("reach CodeShare: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// CodeshareGetProject fetches a project's metadata and source via the JSON API.
// owner/slug may include a leading '@' (stripped). A 404 returns a clear error.
func CodeshareGetProject(ctx context.Context, owner, slug string) (*CodeshareScript, error) {
	owner = strings.TrimPrefix(strings.TrimSpace(owner), "@")
	slug = strings.TrimSpace(slug)
	if owner == "" || slug == "" {
		return nil, fmt.Errorf("CodeShare project needs both an owner and a slug (e.g. owner/script-name)")
	}
	u := codeshareHost + "/api/project/" + url.PathEscape(owner) + "/" + url.PathEscape(slug) + "/"
	body, status, err := codeshareGet(ctx, u)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("CodeShare project @%s/%s not found", owner, slug)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("CodeShare returned HTTP %d for @%s/%s", status, owner, slug)
	}
	var p codeshareProjectJSON
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("parse CodeShare response: %w", err)
	}
	if p.Success != nil && !*p.Success {
		return nil, fmt.Errorf("CodeShare project @%s/%s not found", owner, slug)
	}
	if p.Source == "" {
		return nil, fmt.Errorf("CodeShare returned no source for @%s/%s", owner, slug)
	}
	return &CodeshareScript{
		Owner: owner, Slug: slug,
		ProjectName: p.ProjectName, Description: p.Description,
		FridaVersion: p.FridaVersion, Likes: p.Likes,
		Source: p.Source, SourceSha: sha256Hex(p.Source),
	}, nil
}

// CodeshareSearch returns discovery results for a query (HTML scrape).
func CodeshareSearch(ctx context.Context, query string) ([]CodeshareProject, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return CodeshareBrowse(ctx, 1)
	}
	u := codeshareHost + "/search/?query=" + url.QueryEscape(q)
	body, status, err := codeshareGet(ctx, u)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("CodeShare search returned HTTP %d", status)
	}
	return parseCodeshareList(string(body)), nil
}

// CodeshareBrowse returns one page of the popular/browse listing (HTML scrape).
func CodeshareBrowse(ctx context.Context, page int) ([]CodeshareProject, error) {
	if page < 1 {
		page = 1
	}
	u := fmt.Sprintf("%s/browse?page=%d", codeshareHost, page)
	body, status, err := codeshareGet(ctx, u)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("CodeShare browse returned HTTP %d", status)
	}
	return parseCodeshareList(string(body)), nil
}

// Each result is a titled link to a project page; likes/views follow it in the
// markup up to the next result. RE2 has no lookahead, so we match the anchors
// and slice the text between consecutive ones for the stats (rather than letting
// a trailing delimiter consume the next item's opening tag).
var (
	reCodeshareItem  = regexp.MustCompile(`(?s)<h2><a href="https://codeshare\.frida\.re/@([^/"]+)/([^/"]+)/">(.*?)</a></h2>`)
	reCodeshareLikes = regexp.MustCompile(`fa-thumbs-o-up[^>]*></i>\s*([0-9.,KMkm]+)`)
	reCodeshareViews = regexp.MustCompile(`fa-eye[^>]*></i>\s*([0-9.,KMkm]+)`)
)

// parseCodeshareList extracts results from a search/browse page. It degrades to
// an empty slice (never an error) if the markup changes, so discovery breaking
// never blocks import-by-slug.
func parseCodeshareList(htmlStr string) []CodeshareProject {
	idx := reCodeshareItem.FindAllStringSubmatchIndex(htmlStr, -1)
	var out []CodeshareProject
	seen := map[string]bool{}
	for i, m := range idx {
		owner := htmlStr[m[2]:m[3]]
		slug := htmlStr[m[4]:m[5]]
		title := strings.TrimSpace(html.UnescapeString(htmlStr[m[6]:m[7]]))
		key := owner + "/" + slug
		if owner == "" || slug == "" || seen[key] {
			continue
		}
		seen[key] = true
		// The stats for this item live between the end of this anchor and the
		// start of the next item's anchor (or end of document for the last).
		tailEnd := len(htmlStr)
		if i+1 < len(idx) {
			tailEnd = idx[i+1][0]
		}
		tail := htmlStr[m[1]:tailEnd]
		p := CodeshareProject{Owner: owner, Slug: slug, Title: title}
		if lm := reCodeshareLikes.FindStringSubmatch(tail); lm != nil {
			p.Likes = lm[1]
		}
		if vm := reCodeshareViews.FindStringSubmatch(tail); vm != nil {
			p.Views = vm[1]
		}
		out = append(out, p)
	}
	return out
}
