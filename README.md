# sitemap-indexnow

Go package for the step between a generated sitemap and an IndexNow submission:
parse the sitemap, diff it against the previous build, and turn the URLs that
actually changed into validated, batched request bodies.

```
go get github.com/theluckystrike/sitemap-indexnow
```

## Why the diff

A static site generator rewrites every file on every run, so "what is in the
sitemap" is not the same question as "what changed". `DiffURLSets` answers the
second one, and it treats a changed `lastmod` as a change even when the URL set is
identical.

`Diff.Submittable()` returns added plus changed URLs, sorted and de-duplicated.
Removed URLs are left out: a URL you deleted is better handled by the status code
it now returns.

## Why it does not send anything

The package builds and validates request bodies. The HTTP call, the retry policy
and the key file stay with the caller, which keeps the package testable without a
network and keeps your key out of it.

## Example

```go
oldSet, err := sitemapindexnow.ParseURLSet(previousXML)
newSet, err := sitemapindexnow.ParseURLSet(currentXML)

diff := sitemapindexnow.DiffURLSets(oldSet, newSet)
if diff.Empty() {
    return // nothing to tell anyone about
}

subs, err := sitemapindexnow.BuildSubmissions(
    "example.com", key, "https://example.com/"+key+".txt", diff.Submittable())

for _, s := range subs {
    body, err := s.JSON()
    // POST body to your IndexNow endpoint
}
```

## What is validated

* `ValidateKey` applies the protocol's character rules: 8 to 128 characters, letters,
  digits and dashes only.
* `ValidateHost` insists on a bare hostname, so a scheme, a port or a path is an
  error rather than a request that gets rejected later.
* Every URL must be an absolute http or https URL on that host. A mixed-host list is
  an error, not a partially valid submission.
* Batches are capped at `MaxURLsPerSubmission` (10000, the protocol's documented
  ceiling); larger sets are split into several bodies.
* `ParseURLSet` refuses a `<sitemapindex>` document, because treating an index as a
  urlset is how a pipeline ends up submitting its own sitemaps as pages.

Standard library only. Written for the publishing step of the pipeline at
<https://aiwebsitepipeline.com/> and kept as its own module.

MIT licensed.
