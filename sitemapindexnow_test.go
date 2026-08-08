package sitemapindexnow

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const urlsetXML = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://example.com/</loc>
    <lastmod>2026-08-01</lastmod>
    <changefreq>weekly</changefreq>
    <priority>1.0</priority>
  </url>
  <url>
    <loc>https://example.com/a/</loc>
    <lastmod>2026-08-01</lastmod>
  </url>
</urlset>`

const indexXML = `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>https://example.com/sitemap-1.xml</loc><lastmod>2026-08-02</lastmod></sitemap>
  <sitemap><loc>https://example.com/sitemap-2.xml</loc></sitemap>
</sitemapindex>`

func TestParseURLSet(t *testing.T) {
	entries, err := ParseURLSet([]byte(urlsetXML))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Loc != "https://example.com/" {
		t.Fatalf("loc = %q", entries[0].Loc)
	}
	if entries[0].ChangeFreq != "weekly" {
		t.Fatalf("changefreq = %q", entries[0].ChangeFreq)
	}
	if entries[0].Priority == nil || *entries[0].Priority != 1.0 {
		t.Fatalf("priority = %v", entries[0].Priority)
	}
	if entries[1].Priority != nil {
		t.Fatal("an absent priority should stay nil")
	}
}

func TestParseURLSetRejectsIndex(t *testing.T) {
	_, err := ParseURLSet([]byte(indexXML))
	if err == nil {
		t.Fatal("expected an error when a sitemap index is parsed as a urlset")
	}
	if !strings.Contains(err.Error(), "sitemap index") {
		t.Fatalf("unhelpful error: %v", err)
	}
}

func TestParseURLSetEmptyLoc(t *testing.T) {
	doc := `<urlset><url><loc>  </loc></url></urlset>`
	if _, err := ParseURLSet([]byte(doc)); err == nil {
		t.Fatal("expected an error for an empty <loc>")
	}
}

func TestParseSitemapIndex(t *testing.T) {
	entries, err := ParseSitemapIndex([]byte(indexXML))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].LastMod != "2026-08-02" || entries[1].LastMod != "" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	if _, err := ParseSitemapIndex([]byte(urlsetXML)); err == nil {
		t.Fatal("expected an error when a urlset is parsed as an index")
	}
}

func TestDiffURLSets(t *testing.T) {
	old := []URLEntry{
		{Loc: "https://example.com/", LastMod: "2026-08-01"},
		{Loc: "https://example.com/a/", LastMod: "2026-08-01"},
		{Loc: "https://example.com/gone/", LastMod: "2026-07-01"},
	}
	current := []URLEntry{
		{Loc: "https://example.com/", LastMod: "2026-08-05"},
		{Loc: "https://example.com/a/", LastMod: "2026-08-01"},
		{Loc: "https://example.com/new/", LastMod: "2026-08-05"},
	}
	d := DiffURLSets(old, current)
	if d.Empty() {
		t.Fatal("diff should not be empty")
	}
	if len(d.Added) != 1 || d.Added[0] != "https://example.com/new/" {
		t.Fatalf("added = %v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0] != "https://example.com/gone/" {
		t.Fatalf("removed = %v", d.Removed)
	}
	if len(d.Changed) != 1 || d.Changed[0].OldLastMod != "2026-08-01" || d.Changed[0].NewLastMod != "2026-08-05" {
		t.Fatalf("changed = %+v", d.Changed)
	}
	if len(d.Unchanged) != 1 || d.Unchanged[0] != "https://example.com/a/" {
		t.Fatalf("unchanged = %v", d.Unchanged)
	}
	sub := d.Submittable()
	want := []string{"https://example.com/", "https://example.com/new/"}
	if fmt.Sprint(sub) != fmt.Sprint(want) {
		t.Fatalf("submittable = %v, want %v", sub, want)
	}
}

func TestDiffIdenticalIsEmpty(t *testing.T) {
	set := []URLEntry{{Loc: "https://example.com/", LastMod: "2026-08-01"}}
	d := DiffURLSets(set, set)
	if !d.Empty() {
		t.Fatalf("identical sitemaps should diff empty, got %+v", d)
	}
	if len(d.Submittable()) != 0 {
		t.Fatal("nothing should be submittable")
	}
}

func TestDiffDetectsLastModOnlyChange(t *testing.T) {
	old := []URLEntry{{Loc: "https://example.com/", LastMod: "2026-08-01"}}
	current := []URLEntry{{Loc: "https://example.com/", LastMod: "2026-08-02"}}
	d := DiffURLSets(old, current)
	if len(d.Added) != 0 || len(d.Changed) != 1 {
		t.Fatalf("a lastmod-only change should be Changed, got %+v", d)
	}
}

func TestValidateKey(t *testing.T) {
	bad := []string{"", "short7c", strings.Repeat("a", 129), "has space", "has_underscore"}
	for _, k := range bad {
		if err := ValidateKey(k); err == nil {
			t.Fatalf("key %q should be rejected", k)
		}
	}
	good := []string{"abcdefgh", "11e40ecb5769464a860c281331ee9ecc", "a-b-c-d-e-f", strings.Repeat("a", 128)}
	for _, k := range good {
		if err := ValidateKey(k); err != nil {
			t.Fatalf("key %q should be accepted: %v", k, err)
		}
	}
}

func TestValidateHost(t *testing.T) {
	bad := []string{"", "https://example.com", "example.com/path", "example.com:8080", "localhost"}
	for _, h := range bad {
		if err := ValidateHost(h); err == nil {
			t.Fatalf("host %q should be rejected", h)
		}
	}
	if err := ValidateHost("example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestURLHost(t *testing.T) {
	h, err := URLHost("https://example.com:443/a/b?c=d")
	if err != nil {
		t.Fatal(err)
	}
	if h != "example.com" {
		t.Fatalf("host = %q", h)
	}
	for _, raw := range []string{"ftp://example.com/", "/relative", "https://"} {
		if _, err := URLHost(raw); err == nil {
			t.Fatalf("%q should be rejected", raw)
		}
	}
}

func TestBuildSubmissions(t *testing.T) {
	key := "11e40ecb5769464a860c281331ee9ecc"
	subs, err := BuildSubmissions("example.com", key, "https://example.com/"+key+".txt", []string{
		"https://example.com/",
		"https://example.com/a/",
		"https://example.com/", // duplicate
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 {
		t.Fatalf("got %d submissions, want 1", len(subs))
	}
	if len(subs[0].URLList) != 2 {
		t.Fatalf("urlList = %v, duplicates should be dropped", subs[0].URLList)
	}
	body, err := subs[0].JSON()
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"host", "key", "keyLocation", "urlList"} {
		if _, ok := back[field]; !ok {
			t.Fatalf("body is missing %q: %s", field, body)
		}
	}
}

func TestBuildSubmissionsRejectsForeignHost(t *testing.T) {
	key := "abcdefgh12345678"
	_, err := BuildSubmissions("example.com", key, "", []string{"https://other.example.org/x"})
	if err == nil {
		t.Fatal("a URL on another host should be rejected")
	}
}

func TestBuildSubmissionsBatches(t *testing.T) {
	key := "abcdefgh12345678"
	n := MaxURLsPerSubmission + 5
	urls := make([]string, 0, n)
	for i := 0; i < n; i++ {
		urls = append(urls, fmt.Sprintf("https://example.com/p/%d", i))
	}
	subs, err := BuildSubmissions("example.com", key, "", urls)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2 {
		t.Fatalf("got %d submissions, want 2", len(subs))
	}
	if len(subs[0].URLList) != MaxURLsPerSubmission || len(subs[1].URLList) != 5 {
		t.Fatalf("batch sizes = %d, %d", len(subs[0].URLList), len(subs[1].URLList))
	}
	if subs[1].KeyLocation != "" {
		t.Fatal("an empty keyLocation should stay empty")
	}
	body, err := subs[1].JSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "keyLocation") {
		t.Fatalf("an empty keyLocation should be omitted from the body: %s", body)
	}
}

func TestBuildSubmissionsEmptyInput(t *testing.T) {
	subs, err := BuildSubmissions("example.com", "abcdefgh12345678", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if subs != nil {
		t.Fatalf("nothing to submit should yield no submissions, got %v", subs)
	}
}

func TestEndToEnd(t *testing.T) {
	oldXML := `<urlset><url><loc>https://example.com/</loc><lastmod>2026-08-01</lastmod></url></urlset>`
	newXML := `<urlset>
	  <url><loc>https://example.com/</loc><lastmod>2026-08-01</lastmod></url>
	  <url><loc>https://example.com/fresh/</loc><lastmod>2026-08-08</lastmod></url>
	</urlset>`
	oldSet, err := ParseURLSet([]byte(oldXML))
	if err != nil {
		t.Fatal(err)
	}
	newSet, err := ParseURLSet([]byte(newXML))
	if err != nil {
		t.Fatal(err)
	}
	subs, err := BuildSubmissions("example.com", "abcdefgh12345678", "",
		DiffURLSets(oldSet, newSet).Submittable())
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || len(subs[0].URLList) != 1 || subs[0].URLList[0] != "https://example.com/fresh/" {
		t.Fatalf("only the new page should be submitted, got %+v", subs)
	}
}
