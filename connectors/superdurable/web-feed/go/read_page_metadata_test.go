// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const articlePageFixture = `<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8">
<title>Spring release notes &#8211; Example</title>
<meta name="description" content="Everything   the team shipped.">
<meta name="Description" content="A second description that must not win">
<meta property="og:title" content="Spring release notes">
<meta property="og:description" content="Everything released this spring.">
<meta property="og:image" content="/images/spring.png">
<meta property="og:type" content="article">
<meta property="og:site_name" content="Example">
<meta property="article:published_time" content="2026-11-09T17:00:00+00:00">
<link rel="canonical" href="/blog/spring-release-notes">
<script type="application/ld+json">{ not json </script>
<script type="application/ld+json">
{"@context": "https://schema.org", "@graph": [
  {"@type": "WebSite", "name": "Example"},
  {"@type": ["BlogPosting"], "headline": "Spring release notes (JSON-LD)",
   "datePublished": "2026-11-09T18:00:00+01:00", "dateModified": {"@value": "2026-11-09T19:00:00Z"}}
]}
</script>
<script>var title = "<title>Not this</title>";</script>
</head><body><svg><title>Icon</title></svg><h1>Spring release notes</h1></body></html>`

func TestReadPageMetadataReadsTheHead(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://www.example.com/blog/spring-release-notes", "text/html; charset=utf-8", articlePageFixture)
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadPageMetadata(), ReadPageMetadataInput{URL: "https://www.example.com/blog/spring-release-notes?utm_source=feed"})
	requireBranch(t, result, err, ReadPageMetadataBranchRead, "")
	require.Equal(t, PageMetadata{
		FinalURL:     "https://www.example.com/blog/spring-release-notes?utm_source=feed",
		Title:        "Spring release notes – Example",
		Description:  "Everything the team shipped.",
		CanonicalURL: "https://www.example.com/blog/spring-release-notes",
		OpenGraph: OpenGraphMetadata{
			Title: "Spring release notes", Description: "Everything released this spring.",
			ImageURL: "https://www.example.com/images/spring.png", Type: "article", SiteName: "Example",
		},
		ArticlePublishedAt: timestamp(t, "2026-11-09T17:00:00Z"),
		StructuredData: StructuredDataMetadata{
			Type: "BlogPosting", Headline: "Spring release notes (JSON-LD)",
			DatePublished: timestamp(t, "2026-11-09T17:00:00Z"), DateModified: timestamp(t, "2026-11-09T19:00:00Z"),
		},
		PublishedAt: timestamp(t, "2026-11-09T17:00:00Z"),
		ModifiedAt:  timestamp(t, "2026-11-09T19:00:00Z"),
		FetchedAt:   testClockTime,
	}, result.Value)
}

func TestReadPageMetadataFallsBackAcrossSources(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://www.example.com/post", "text/html", `<html><head>
<meta property="og:updated_time" content="2026-11-02T10:00:00Z">
<script type="application/ld+json">[{"@type": "Organization", "name": "Example"}, {"@type": "Thing", "headline": "Fallback node", "datePublished": "2026-11-01"}]</script>
</head><body><svg><title>Icon</title></svg></body></html>`)
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadPageMetadata(), ReadPageMetadataInput{URL: "https://www.example.com/post"})
	requireBranch(t, result, err, ReadPageMetadataBranchRead, "")
	require.Empty(t, result.Value.Title, "an svg title is not the page title")
	require.Equal(t, StructuredDataMetadata{Type: "Thing", Headline: "Fallback node", DatePublished: timestamp(t, "2026-11-01T00:00:00Z")}, result.Value.StructuredData)
	require.Equal(t, timestamp(t, "2026-11-01T00:00:00Z"), result.Value.PublishedAt)
	require.Equal(t, timestamp(t, "2026-11-02T10:00:00Z"), result.Value.ModifiedAt)
	require.Empty(t, result.Value.CanonicalURL)
}

func TestReadPageMetadataReadsOnlyThePagePrefix(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://www.example.com/long", "text/html", `<html><head><title>Long page</title></head><body>`+strings.Repeat("<p>text</p>", 1000)+`<meta name="description" content="too late"></body></html>`)
	site.serve("https://www.example.com/image.png", "image/png", "\x89PNG")
	client := site.newClient(t, Config{MaxPageBytes: 2048})
	result, err := runQuery(t, client.ReadPageMetadata(), ReadPageMetadataInput{URL: "https://www.example.com/long"})
	requireBranch(t, result, err, ReadPageMetadataBranchRead, "")
	require.Equal(t, "Long page", result.Value.Title)
	require.Empty(t, result.Value.Description)
	require.True(t, result.Value.IsTruncated)
	result, err = runQuery(t, client.ReadPageMetadata(), ReadPageMetadataInput{URL: "https://www.example.com/image.png"})
	requireBranch(t, result, err, ReadPageMetadataBranchInvalidResponse, sdkgo.FailureProtocol)
	require.Equal(t, "readPageMetadata", result.Failure.Operation)
	result, err = runQuery(t, client.ReadPageMetadata(), ReadPageMetadataInput{URL: "https://metadata.example.com/"})
	requireBranch(t, result, err, ReadPageMetadataBranchUnreachable, sdkgo.FailureValidation)
	require.Contains(t, result.Failure.Message, "link-local")
}
