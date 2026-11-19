// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const urlSetFixture = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:news="http://www.google.com/schemas/sitemap-news/0.9" xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
  <url><loc>https://www.example.com/</loc><lastmod>2026-11-10</lastmod></url>
  <url><loc>https://www.example.com/blog/older-post</loc><lastmod>2026-10-01T08:00:00+00:00</lastmod>
    <image:image><image:loc>https://www.example.com/images/older.png</image:loc></image:image></url>
  <url><loc>https://www.example.com/blog/newest-post</loc><lastmod>2026-11-09T18:00:00Z</lastmod></url>
  <url><loc>/blog/relative-post</loc><lastmod>2026-11-05</lastmod></url>
  <url><loc>https://www.example.com/blog/undated-post</loc></url>
  <url><loc>https://www.example.com/blog/news-only</loc>
    <news:news><news:publication><news:name>Example</news:name><news:language>en</news:language></news:publication>
      <news:publication_date>2026-11-08T12:00:00Z</news:publication_date><news:title>News &amp; notes</news:title></news:news></url>
  <url><loc>https://www.example.com/Blog/case-differs</loc><lastmod>2026-11-09</lastmod></url>
  <url><loc>javascript:alert(1)</loc></url>
  <url><lastmod>2026-11-09</lastmod></url>
</urlset>`

func TestReadSitemapFiltersAndOrdersURLSetEntries(t *testing.T) {
	site := newTestSite(t)
	site.handle("https://www.example.com/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Header().Set("ETag", `W/"a41"`)
		_, _ = w.Write([]byte(urlSetFixture))
	})
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadSitemap(), ReadSitemapInput{URL: "https://www.example.com/sitemap.xml", PathPrefix: "/blog/"})
	requireBranch(t, result, err, ReadSitemapBranchRead, "")
	sitemap := result.Value
	require.Equal(t, SitemapKindURLSet, sitemap.Kind)
	require.Equal(t, 7, sitemap.TotalEntryCount, "entries without a usable http or https loc are not counted")
	require.Equal(t, 5, sitemap.MatchedEntryCount)
	require.False(t, sitemap.HasMore)
	require.Equal(t, `W/"a41"`, sitemap.ETag)
	require.Empty(t, sitemap.Sitemaps)
	require.Equal(t, []SitemapEntry{
		{URL: "https://www.example.com/blog/newest-post", LastModifiedAt: timestamp(t, "2026-11-09T18:00:00Z")},
		{URL: "https://www.example.com/blog/news-only", NewsTitle: "News & notes", NewsPublishedAt: timestamp(t, "2026-11-08T12:00:00Z")},
		{URL: "https://www.example.com/blog/relative-post", LastModifiedAt: timestamp(t, "2026-11-05T00:00:00Z")},
		{URL: "https://www.example.com/blog/older-post", LastModifiedAt: timestamp(t, "2026-10-01T08:00:00Z")},
		{URL: "https://www.example.com/blog/undated-post"},
	}, sitemap.Entries)

	after := timestamp(t, "2026-11-05T00:00:00Z")
	result, err = runQuery(t, client.ReadSitemap(), ReadSitemapInput{URL: "https://www.example.com/sitemap.xml", PathPrefix: "/blog/", LastModifiedAfter: after, Limit: 1})
	requireBranch(t, result, err, ReadSitemapBranchRead, "")
	require.Equal(t, 2, result.Value.MatchedEntryCount, "only entries strictly after the time with a date match")
	require.True(t, result.Value.HasMore)
	require.Equal(t, []SitemapEntry{{URL: "https://www.example.com/blog/newest-post", LastModifiedAt: timestamp(t, "2026-11-09T18:00:00Z")}}, result.Value.Entries)
}

func TestReadSitemapListsChildSitemapsOfAnIndex(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://www.example.com/sitemap_index.xml", "text/xml", `<?xml version="1.0"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>https://www.example.com/sitemap-pages.xml</loc><lastmod>2026-09-01</lastmod></sitemap>
  <sitemap><loc>https://www.example.com/sitemap-posts.xml.gz</loc><lastmod>2026-11-09T10:00:00Z</lastmod></sitemap>
  <sitemap><loc>https://www.example.com/sitemap-authors.xml</loc></sitemap>
</sitemapindex>`)
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadSitemap(), ReadSitemapInput{URL: "https://www.example.com/sitemap_index.xml", PathPrefix: "/blog/", Limit: 2})
	requireBranch(t, result, err, ReadSitemapBranchRead, "")
	require.Equal(t, SitemapKindIndex, result.Value.Kind)
	require.Equal(t, 3, result.Value.TotalEntryCount)
	require.Equal(t, 3, result.Value.MatchedEntryCount, "pathPrefix does not filter child sitemaps")
	require.True(t, result.Value.HasMore)
	require.Empty(t, result.Value.Entries)
	require.Equal(t, []ChildSitemap{
		{URL: "https://www.example.com/sitemap-posts.xml.gz", LastModifiedAt: timestamp(t, "2026-11-09T10:00:00Z")},
		{URL: "https://www.example.com/sitemap-pages.xml", LastModifiedAt: timestamp(t, "2026-09-01T00:00:00Z")},
	}, result.Value.Sitemaps)
}

func TestReadSitemapReadsGzipAndConditionalGet(t *testing.T) {
	site := newTestSite(t)
	site.handle("https://www.example.com/sitemap-posts.xml.gz", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"posts-1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/x-gzip")
		w.Header().Set("ETag", `"posts-1"`)
		w.Header().Set("Last-Modified", "Mon, 09 Nov 2026 10:00:00 GMT")
		_, _ = w.Write(gzipBytes(t, urlSetFixture))
	})
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadSitemap(), ReadSitemapInput{URL: "https://www.example.com/sitemap-posts.xml.gz"})
	requireBranch(t, result, err, ReadSitemapBranchRead, "")
	require.Equal(t, 7, result.Value.MatchedEntryCount)
	require.Equal(t, "https://www.example.com/", result.Value.Entries[0].URL)
	result, err = runQuery(t, client.ReadSitemap(), ReadSitemapInput{URL: "https://www.example.com/sitemap-posts.xml.gz", ETag: result.Value.ETag, LastModified: result.Value.LastModified})
	requireBranch(t, result, err, ReadSitemapBranchNotModified, "")
	require.Equal(t, Sitemap{
		FinalURL: "https://www.example.com/sitemap-posts.xml.gz", Entries: []SitemapEntry{}, Sitemaps: []ChildSitemap{},
		ETag: `"posts-1"`, LastModified: "Mon, 09 Nov 2026 10:00:00 GMT", FetchedAt: testClockTime,
	}, result.Value)
	require.Equal(t, "Mon, 09 Nov 2026 10:00:00 GMT", site.recordedRequests()[1].header.Get("If-Modified-Since"))
}

func TestReadSitemapRejectsOtherDocumentsAndInput(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://www.example.com/page", "text/html", `<html><body>Not found</body></html>`)
	site.serve("https://www.example.com/feed", "application/rss+xml", `<rss><channel/></rss>`)
	site.serve("https://www.example.com/broken", "application/xml", `<urlset><url><loc>https://www.example.com/</loc></url>`+"\x00")
	site.serve("https://www.example.com/bad.gz", "application/gzip", "\x1f\x8bnot gzip")
	site.serve("https://www.example.com/image", "image/svg+xml", `<svg/>`)
	client := site.newClient(t, Config{})
	for path, text := range map[string]string{
		"page": "HTML page", "feed": "neither urlset nor sitemapindex", "broken": "malformed",
		"bad.gz": "gzip-compressed body is malformed", "image": "serves image/svg+xml, not a sitemap",
	} {
		result, err := runQuery(t, client.ReadSitemap(), ReadSitemapInput{URL: "https://www.example.com/" + path})
		requireBranch(t, result, err, ReadSitemapBranchInvalidResponse, sdkgo.FailureProtocol)
		require.Contains(t, result.Failure.Message, text, path)
	}
	requestCount := len(site.recordedRequests())
	for _, input := range []ReadSitemapInput{
		{URL: "https://www.example.com/sitemap.xml", PathPrefix: "blog/"},
		{URL: "https://www.example.com/sitemap.xml", PathPrefix: "/blog/?draft"},
		{URL: "https://www.example.com/sitemap.xml", PathPrefix: "/" + strings.Repeat("a", 512)},
		{URL: "https://www.example.com/sitemap.xml", Limit: 2001},
		{URL: "https://www.example.com/sitemap.xml", ETag: "line\nbreak"},
	} {
		result, err := runQuery(t, client.ReadSitemap(), input)
		requireBranch(t, result, err, ReadSitemapBranchDefect, sdkgo.FailureValidation)
	}
	require.Len(t, site.recordedRequests(), requestCount)
}

func TestReadSitemapBoundsTheEntryCount(t *testing.T) {
	site := newTestSite(t)
	var document strings.Builder
	document.WriteString("<urlset>")
	for index := range maxSitemapScanEntries + 1 {
		fmt.Fprintf(&document, "<url><loc>https://www.example.com/p/%d</loc></url>", index)
	}
	document.WriteString("</urlset>")
	site.serve("https://www.example.com/huge.xml", "application/xml", document.String())
	client := site.newClient(t, Config{MaxDocumentBytes: 16 << 20})
	result, err := runQuery(t, client.ReadSitemap(), ReadSitemapInput{URL: "https://www.example.com/huge.xml"})
	requireBranch(t, result, err, ReadSitemapBranchInvalidResponse, sdkgo.FailureProtocol)
	require.Contains(t, result.Failure.Message, "more than 100000 entries")
}
