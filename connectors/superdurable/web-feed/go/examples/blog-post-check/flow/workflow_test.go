// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package blogpostcheck

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/web-feed/go"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/web-feed/go/webfeedmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestBlogPostCheckIdentityAndRegistration(t *testing.T) {
	require.Equal(t, "BlogPostCheck", dex.GetFinalFlowType(NewBlogPostCheck(webfeed.Connection{})))
	require.Equal(t, "StartBlogPostCheck", dex.GetFinalStepType[CheckRequest](StartBlogPostCheck{}))
	require.Equal(t, "web-feed-public", ConnectionName)
	mock := webfeedmock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewBlogPostCheck(mock.Connection())})
	require.NoError(t, err)
	input := webfeed.ReadFeedInput{URL: "https://blog.example.com/feed.xml", ETag: `"v1"`}
	require.Equal(t, input, mapReadFeedInput(input))
	pageInput := webfeed.ReadPageInput{URL: "https://blog.example.com/posts/faster-builds", MaxTextCharacters: firstPostTextCharacters}
	require.Equal(t, pageInput, mapReadPageInput(pageInput))
}

func TestNewCheckStateValidatesTheRequest(t *testing.T) {
	state, err := newCheckState(CheckRequest{SiteURL: "https://blog.example.com/", PublishedAfter: "2026-11-01T01:00:00+01:00"})
	require.NoError(t, err)
	require.Equal(t, defaultMaxPosts, state.Request.MaxPosts)
	require.Equal(t, time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC), *state.PublishedAfter)
	for request, text := range map[CheckRequest]string{
		{}: "siteUrl, feedUrl, or sitemapUrl is required",
		{SiteURL: "https://blog.example.com/", MaxPosts: 11}:                "maxPosts",
		{SiteURL: "https://blog.example.com/", PublishedAfter: "yesterday"}: "RFC 3339",
	} {
		_, err := newCheckState(request)
		require.ErrorContains(t, err, text)
	}
}

func TestSelectFeedPostsKeepsNewerItemsInFeedOrder(t *testing.T) {
	newer, older := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cutoff := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	items := []webfeed.FeedItem{
		{Link: "https://blog.example.com/a", Title: "A", PublishedAt: &newer},
		{Link: "https://blog.example.com/b", Title: "B", PublishedAt: &older},
		{Link: "https://blog.example.com/c", Title: "C", UpdatedAt: &newer},
		{Link: "https://blog.example.com/d", Title: "D"},
	}
	require.Equal(t, []Post{
		{URL: "https://blog.example.com/a", Title: "A", PublishedAt: &newer},
		{URL: "https://blog.example.com/c", Title: "C", PublishedAt: &newer},
	}, selectFeedPosts(items, &cutoff, 5))
	require.Len(t, selectFeedPosts(items, nil, 3), 3)
	require.Equal(t, []Post{}, selectFeedPosts(nil, nil, 3))
}

func TestSitemapHelpersMergeNewestFirstAndBoundReads(t *testing.T) {
	first, second := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	entries := newestEntries([]webfeed.SitemapEntry{
		{URL: "https://www.example.com/blog/undated"},
		{URL: "https://www.example.com/blog/second", LastModifiedAt: &second},
		{URL: "https://www.example.com/blog/first", NewsPublishedAt: &first},
		{URL: "https://www.example.com/blog/second", LastModifiedAt: &second},
	}, 3)
	require.Equal(t, []string{"https://www.example.com/blog/first", "https://www.example.com/blog/second", "https://www.example.com/blog/undated"},
		[]string{entries[0].URL, entries[1].URL, entries[2].URL})

	state := checkState{SitemapReadCount: 1}
	state.recordSitemap(webfeed.Sitemap{Kind: webfeed.SitemapKindIndex, Sitemaps: []webfeed.ChildSitemap{
		{URL: "https://www.example.com/a.xml"}, {URL: "https://www.example.com/b.xml"}, {URL: "https://www.example.com/c.xml"}, {URL: "https://www.example.com/d.xml"},
	}})
	require.Equal(t, []string{"https://www.example.com/a.xml", "https://www.example.com/b.xml", "https://www.example.com/c.xml"}, state.PendingSitemaps)
	require.Equal(t, "https://www.example.com/sitemap.xml", defaultSitemapURL("https://www.example.com/blog/?page=2"))

	page := webfeed.PageMetadata{Title: "Page title", OpenGraph: webfeed.OpenGraphMetadata{Description: "Shared description"}}
	require.Equal(t, Post{URL: "https://www.example.com/blog/first", Title: "Page title", Summary: "Shared description", PublishedAt: &first},
		postFromPage(webfeed.SitemapEntry{URL: "https://www.example.com/blog/first", NewsPublishedAt: &first}, page))
}
