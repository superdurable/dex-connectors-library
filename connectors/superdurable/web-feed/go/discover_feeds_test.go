// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestDiscoverFeedsListsAdvertisedFeedsInPageOrder(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://www.example.com/blog/", "text/html; charset=utf-8", `<!DOCTYPE html>
<html><head>
<meta charset="utf-8">
<title>Example &amp; Co Blog</title>
<base href="https://www.example.com/blog/assets/">
<link rel="stylesheet alternate" type="application/rss+xml" href="/not-a-feed.css">
<link rel="alternate" type="application/json" href="/wp-json/wp/v2/pages/7">
<link rel="Alternate" type="application/rss+xml; charset=utf-8" title="Posts" href="../feed.xml">
<link rel="alternate" type="application/atom+xml" title="Atom" href="https://www.example.com/blog/atom.xml">
<link rel="alternate" type="application/rss+xml" title="Duplicate" href="https://www.example.com/blog/feed.xml#latest">
<link rel="alternate" type="application/rss+xml" href="javascript:alert(1)">
<link rel="icon" href="/favicon.ico">
</head><body>
<svg><title>Logo</title></svg>
<link rel="alternate" type="application/feed+json" title="JSON" href="/blog/feed.json">
</body></html>`)
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.DiscoverFeeds(), DiscoverFeedsInput{URL: "http://www.example.com/blog/"})
	requireBranch(t, result, err, DiscoverFeedsBranchDiscovered, "")
	require.Equal(t, DiscoveredFeeds{
		FinalURL:  "https://www.example.com/blog/",
		PageTitle: "Example & Co Blog",
		Feeds: []DiscoveredFeed{
			{URL: "https://www.example.com/blog/feed.xml", Title: "Posts", MediaType: "application/rss+xml"},
			{URL: "https://www.example.com/blog/atom.xml", Title: "Atom", MediaType: "application/atom+xml"},
			{URL: "https://www.example.com/blog/feed.json", Title: "JSON", MediaType: "application/feed+json"},
		},
		FetchedAt: testClockTime,
	}, result.Value)
	require.Contains(t, site.recordedRequests()[0].header.Get("Accept"), "text/html")
}

func TestDiscoverFeedsReportsAnEmptyListAndATruncatedPage(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://www.example.com/", "text/html", `<html><head><title>No feeds</title></head><body>`+strings.Repeat("<p>filler</p>", 400)+`<link rel="alternate" type="application/rss+xml" href="/late.xml"></body></html>`)
	client := site.newClient(t, Config{MaxPageBytes: 1024})
	result, err := runQuery(t, client.DiscoverFeeds(), DiscoverFeedsInput{URL: "https://www.example.com/"})
	requireBranch(t, result, err, DiscoverFeedsBranchDiscovered, "")
	require.Equal(t, "No feeds", result.Value.PageTitle)
	require.Empty(t, result.Value.Feeds)
	require.NotNil(t, result.Value.Feeds)
	require.True(t, result.Value.IsTruncated, "the late link lies beyond maxPageBytes")
}

func TestDiscoverFeedsDecodesTheMetaCharset(t *testing.T) {
	site := newTestSite(t)
	page := append([]byte(`<html><head><meta charset="windows-1252"><title>Caf`), 0xe9)
	page = append(page, []byte(`</title><link rel="alternate" type="application/atom+xml" title="Caf`)...)
	page = append(page, 0xe9)
	page = append(page, []byte(`" href="/feed"></head></html>`)...)
	site.serve("https://www.example.com/", "text/html", string(page))
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.DiscoverFeeds(), DiscoverFeedsInput{URL: "https://www.example.com/"})
	requireBranch(t, result, err, DiscoverFeedsBranchDiscovered, "")
	require.Equal(t, "Café", result.Value.PageTitle)
	require.Equal(t, "Café", result.Value.Feeds[0].Title)
}

func TestDiscoverFeedsRejectsNonHTMLAndClassifiesFailures(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://www.example.com/feed.xml", "application/rss+xml", `<rss/>`)
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.DiscoverFeeds(), DiscoverFeedsInput{URL: "https://www.example.com/feed.xml"})
	requireBranch(t, result, err, DiscoverFeedsBranchInvalidResponse, sdkgo.FailureProtocol)
	require.Contains(t, result.Failure.Message, "serves application/rss+xml, not an HTML page")
	result, err = runQuery(t, client.DiscoverFeeds(), DiscoverFeedsInput{URL: "https://www.example.com/missing"})
	requireBranch(t, result, err, DiscoverFeedsBranchNotFound, sdkgo.FailureNotFound)
	result, err = runQuery(t, client.DiscoverFeeds(), DiscoverFeedsInput{URL: "https://internal.example.com/"})
	requireBranch(t, result, err, DiscoverFeedsBranchUnreachable, sdkgo.FailureValidation)
	result, err = runQuery(t, client.DiscoverFeeds(), DiscoverFeedsInput{URL: "file:///etc/passwd"})
	requireBranch(t, result, err, DiscoverFeedsBranchDefect, sdkgo.FailureValidation)
	require.Equal(t, "discoverFeeds", result.Failure.Operation)
}
