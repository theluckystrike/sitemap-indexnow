// Package sitemapindexnow is the plumbing between a generated sitemap and an
// IndexNow submission: parse a sitemap, diff it against the previous build,
// and turn the URLs that actually changed into validated, batched IndexNow
// request bodies.
//
// The point of the diff is that a static site generator rewrites every file
// on every run, so "what is in the sitemap" is not the same question as "what
// changed". Submitting the whole sitemap after a one-page edit is noise.
// DiffURLSets answers the second question, and it treats a changed lastmod as
// a change even when the URL set is identical.
//
// The package builds request bodies and validates them. It does not send
// them: the HTTP call, the retry policy and the key file are the caller's,
// which keeps the package testable without a network and keeps your key out
// of it.
//
//	old, _ := sitemapindexnow.ParseURLSet(previousXML)
//	new, _ := sitemapindexnow.ParseURLSet(currentXML)
//	diff := sitemapindexnow.DiffURLSets(old, new)
//	subs, err := sitemapindexnow.BuildSubmissions(
//		"example.com", key, "https://example.com/"+key+".txt", diff.Submittable())
//	for _, s := range subs {
//		body, _ := s.JSON() // POST to your IndexNow endpoint
//		_ = body
//	}
//
// Standard library only. Written for the publishing step of the agentic site
// pipeline at https://aiwebsitepipeline.com/ and kept as its own module so the
// sitemap-to-IndexNow hop can be tested without the rest of it.
package sitemapindexnow

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// MaxURLsPerSubmission is the number of URLs this package puts in a single
// IndexNow request body. The IndexNow protocol documents a ceiling of 10000
// URLs per submission; BuildSubmissions splits larger sets into several
// bodies rather than producing one that would be rejected.
const MaxURLsPerSubmission = 10000

// URLEntry is one <url> element of a sitemap, or one <sitemap> element of a
// sitemap index. Fields absent from the XML stay empty; Priority is a pointer
// so that an absent priority is distinguishable from a priority of 0.
type URLEntry struct {
	Loc        string
	LastMod    string
	ChangeFreq string
	Priority   *float64
}

type xmlURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	URLs    []xmlURLNode `xml:"url"`
}

type xmlSitemapIndex struct {
	XMLName  xml.Name     `xml:"sitemapindex"`
	Sitemaps []xmlURLNode `xml:"sitemap"`
}

type xmlURLNode struct {
	Loc        string   `xml:"loc"`
	LastMod    string   `xml:"lastmod"`
	ChangeFreq string   `xml:"changefreq"`
	Priority   *float64 `xml:"priority"`
}

func toEntries(nodes []xmlURLNode) ([]URLEntry, error) {
	out := make([]URLEntry, 0, len(nodes))
	for i, n := range nodes {
		loc := strings.TrimSpace(n.Loc)
		if loc == "" {
			return nil, fmt.Errorf("sitemapindexnow: entry %d has an empty <loc>", i)
		}
		out = append(out, URLEntry{
			Loc:        loc,
			LastMod:    strings.TrimSpace(n.LastMod),
			ChangeFreq: strings.TrimSpace(n.ChangeFreq),
			Priority:   n.Priority,
		})
	}
	return out, nil
}

// ParseURLSet parses a sitemap <urlset> document. It rejects a document whose
// root is a <sitemapindex>, because an index carries sitemap locations rather
// than page locations and silently treating one as the other is how a
// pipeline ends up submitting its own sitemaps as pages.
func ParseURLSet(doc []byte) ([]URLEntry, error) {
	var set xmlURLSet
	if err := xml.Unmarshal(doc, &set); err != nil {
		if strings.Contains(err.Error(), "sitemapindex") {
			return nil, errors.New("sitemapindexnow: document is a sitemap index, use ParseSitemapIndex")
		}
		return nil, fmt.Errorf("sitemapindexnow: parsing urlset: %w", err)
	}
	if set.XMLName.Local != "urlset" {
		return nil, fmt.Errorf("sitemapindexnow: root element is <%s>, want <urlset>", set.XMLName.Local)
	}
	return toEntries(set.URLs)
}

// ParseSitemapIndex parses a <sitemapindex> document and returns one entry per
// child sitemap.
func ParseSitemapIndex(doc []byte) ([]URLEntry, error) {
	var idx xmlSitemapIndex
	if err := xml.Unmarshal(doc, &idx); err != nil {
		return nil, fmt.Errorf("sitemapindexnow: parsing sitemapindex: %w", err)
	}
	if idx.XMLName.Local != "sitemapindex" {
		return nil, fmt.Errorf("sitemapindexnow: root element is <%s>, want <sitemapindex>", idx.XMLName.Local)
	}
	return toEntries(idx.Sitemaps)
}

// Change is a URL present in both sitemaps whose lastmod differs.
type Change struct {
	Loc        string
	OldLastMod string
	NewLastMod string
}

// Diff is the result of comparing two sitemaps. Every slice is sorted by URL
// so that two runs over the same input produce byte-identical output.
type Diff struct {
	Added     []string
	Removed   []string
	Changed   []Change
	Unchanged []string
}

// Empty reports whether nothing was added, removed or changed.
func (d Diff) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Changed) == 0
}

// Submittable returns the URLs worth telling a search engine about: the added
// ones and the changed ones, sorted, with no duplicates. Removed URLs are not
// included, because IndexNow submits URLs whose content changed and a URL you
// deleted is better handled by the status code it now returns.
func (d Diff) Submittable() []string {
	seen := make(map[string]bool, len(d.Added)+len(d.Changed))
	out := make([]string, 0, len(d.Added)+len(d.Changed))
	for _, u := range d.Added {
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	for _, c := range d.Changed {
		if !seen[c.Loc] {
			seen[c.Loc] = true
			out = append(out, c.Loc)
		}
	}
	sort.Strings(out)
	return out
}

// DiffURLSets compares two parsed sitemaps by URL. A URL in both whose
// lastmod differs is reported as Changed; one whose lastmod is equal, or
// absent from both, is Unchanged. When the same URL appears more than once in
// one input, the last occurrence wins.
func DiffURLSets(old, current []URLEntry) Diff {
	oldIdx := make(map[string]URLEntry, len(old))
	for _, e := range old {
		oldIdx[e.Loc] = e
	}
	newIdx := make(map[string]URLEntry, len(current))
	for _, e := range current {
		newIdx[e.Loc] = e
	}

	var d Diff
	for loc, ne := range newIdx {
		oe, ok := oldIdx[loc]
		switch {
		case !ok:
			d.Added = append(d.Added, loc)
		case oe.LastMod != ne.LastMod:
			d.Changed = append(d.Changed, Change{Loc: loc, OldLastMod: oe.LastMod, NewLastMod: ne.LastMod})
		default:
			d.Unchanged = append(d.Unchanged, loc)
		}
	}
	for loc := range oldIdx {
		if _, ok := newIdx[loc]; !ok {
			d.Removed = append(d.Removed, loc)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Removed)
	sort.Strings(d.Unchanged)
	sort.Slice(d.Changed, func(i, j int) bool { return d.Changed[i].Loc < d.Changed[j].Loc })
	return d
}

// Submission is one IndexNow request body.
type Submission struct {
	Host        string   `json:"host"`
	Key         string   `json:"key"`
	KeyLocation string   `json:"keyLocation,omitempty"`
	URLList     []string `json:"urlList"`
}

// JSON marshals the submission as the request body.
func (s Submission) JSON() ([]byte, error) { return json.Marshal(s) }

// ValidateKey checks the key against the character rules the IndexNow
// protocol states: 8 to 128 characters, each of them a letter, a digit or a
// dash.
func ValidateKey(key string) error {
	if n := len(key); n < 8 || n > 128 {
		return fmt.Errorf("sitemapindexnow: key length %d is outside 8..128", n)
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return fmt.Errorf("sitemapindexnow: key contains %q, only letters, digits and dashes are allowed", r)
		}
	}
	return nil
}

// ValidateHost checks that host is a bare hostname: no scheme, no path, no
// port, and at least one dot.
func ValidateHost(host string) error {
	if host == "" {
		return errors.New("sitemapindexnow: host is empty")
	}
	if strings.Contains(host, "/") || strings.Contains(host, ":") {
		return fmt.Errorf("sitemapindexnow: host %q must be a bare hostname, without scheme, port or path", host)
	}
	if !strings.Contains(host, ".") {
		return fmt.Errorf("sitemapindexnow: host %q has no dot", host)
	}
	return nil
}

// URLHost returns the hostname of an absolute http or https URL.
func URLHost(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("sitemapindexnow: parsing %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("sitemapindexnow: %q is not an http or https URL", raw)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("sitemapindexnow: %q has no host", raw)
	}
	return u.Hostname(), nil
}

// BuildSubmissions validates host, key and every URL, then splits the URLs
// into request bodies of at most MaxURLsPerSubmission each. Every URL must
// belong to host, which is the rule that stops a mixed-host sitemap from
// producing a submission a search engine will reject wholesale. Duplicate
// URLs are dropped and the order of the input is preserved.
//
// An empty url list returns no submissions and no error: nothing changed is a
// normal outcome of a build, not a failure.
func BuildSubmissions(host, key, keyLocation string, urls []string) ([]Submission, error) {
	if err := ValidateHost(host); err != nil {
		return nil, err
	}
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	if keyLocation != "" {
		if _, err := URLHost(keyLocation); err != nil {
			return nil, fmt.Errorf("sitemapindexnow: keyLocation: %w", err)
		}
	}

	seen := make(map[string]bool, len(urls))
	clean := make([]string, 0, len(urls))
	for _, raw := range urls {
		h, err := URLHost(raw)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(h, host) {
			return nil, fmt.Errorf("sitemapindexnow: %q is on host %q, not %q", raw, h, host)
		}
		if seen[raw] {
			continue
		}
		seen[raw] = true
		clean = append(clean, raw)
	}
	if len(clean) == 0 {
		return nil, nil
	}

	var subs []Submission
	for start := 0; start < len(clean); start += MaxURLsPerSubmission {
		end := start + MaxURLsPerSubmission
		if end > len(clean) {
			end = len(clean)
		}
		batch := make([]string, end-start)
		copy(batch, clean[start:end])
		subs = append(subs, Submission{
			Host:        host,
			Key:         key,
			KeyLocation: keyLocation,
			URLList:     batch,
		})
	}
	return subs, nil
}
