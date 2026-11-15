// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// Binding the hand-written client fails if its Definitions drift from the generated agent metadata.
func TestAgentToolsReadAndClassifyEveryOutcome(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://blog.example.com/feed.xml", "application/rss+xml", `<rss><channel><title>Agent feed</title><item><title>One</title><guid>1</guid></item></channel></rss>`)
	site.serve("https://blog.example.com/", "text/html", `<html><head><link rel="alternate" type="application/atom+xml" href="/atom.xml"></head></html>`)
	site.serve("https://blog.example.com/sitemap.xml", "application/xml", `<urlset><url><loc>https://blog.example.com/blog/a</loc><lastmod>2026-11-09</lastmod></url></urlset>`)
	site.serve("https://blog.example.com/blog/a", "text/html", `<html><head><title>Post A</title></head></html>`)
	client := site.newClient(t, Config{})
	connection, err := NewConnection(client, testConnection)
	require.NoError(t, err)
	tools, err := connection.AgentTools()
	require.NoError(t, err)
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Metadata().ToolName)
		require.Equal(t, sdkgo.AgentToolEffectRead, tool.Metadata().Effect())
		require.Equal(t, connection.Reference(), tool.Connection())
		require.False(t, tool.Metadata().IsApprovalRecommended, "a public read needs no approval")
		var schema struct {
			Required []string `json:"required"`
		}
		require.NoError(t, json.Unmarshal(tool.Metadata().InputSchema, &schema))
		require.Equal(t, []string{"url"}, schema.Required, "only the URL is required")
	}
	require.Equal(t, []string{"web-feed_read_feed", "web-feed_discover_feeds", "web-feed_read_sitemap", "web-feed_read_page_metadata"}, names)
	readFeed, discoverFeeds, readSitemap, readPageMetadata := tools[0], tools[1], tools[2], tools[3]

	for _, test := range []struct {
		name      string
		tool      sdkgo.AgentTool
		arguments string
		outcome   sdkgo.AgentToolAttemptOutcome
		branch    sdkgo.BranchID
	}{
		{"readFeed read", readFeed, `{"url":"https://blog.example.com/feed.xml"}`, sdkgo.AgentToolAttemptSucceeded, ReadFeedBranchRead},
		{"readFeed notFound", readFeed, `{"url":"https://blog.example.com/missing.xml"}`, sdkgo.AgentToolAttemptRejected, ReadFeedBranchNotFound},
		{"readFeed invalidResponse", readFeed, `{"url":"https://blog.example.com/"}`, sdkgo.AgentToolAttemptRejected, ReadFeedBranchInvalidResponse},
		{"readFeed unreachable", readFeed, `{"url":"https://internal.example.com/feed.xml"}`, sdkgo.AgentToolAttemptRejected, ReadFeedBranchUnreachable},
		{"readFeed defect", readFeed, `{"url":"feed.xml"}`, sdkgo.AgentToolAttemptRejected, ReadFeedBranchDefect},
		{"discoverFeeds discovered", discoverFeeds, `{"url":"https://blog.example.com/"}`, sdkgo.AgentToolAttemptSucceeded, DiscoverFeedsBranchDiscovered},
		{"readSitemap read", readSitemap, `{"url":"https://blog.example.com/sitemap.xml","pathPrefix":"/blog/","lastModifiedAfter":"2026-11-01T00:00:00Z"}`, sdkgo.AgentToolAttemptSucceeded, ReadSitemapBranchRead},
		{"readPageMetadata read", readPageMetadata, `{"url":"https://blog.example.com/blog/a"}`, sdkgo.AgentToolAttemptSucceeded, ReadPageMetadataBranchRead},
		{"readPageMetadata unreachable", readPageMetadata, `{"url":"https://metadata.example.com/"}`, sdkgo.AgentToolAttemptRejected, ReadPageMetadataBranchUnreachable},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempt, err := test.tool.RunAttempt(singledispatchtest.NewContext("agent-host", test.name), json.RawMessage(test.arguments))
			require.NoError(t, err)
			require.Equal(t, test.outcome, attempt.Outcome)
			require.Equal(t, test.branch, attempt.Branch)
			require.True(t, attempt.HasInvokedOperation)
		})
	}

	attempt, err := readSitemap.RunAttempt(singledispatchtest.NewContext("agent-host", "sitemap-value"), json.RawMessage(`{"url":"https://blog.example.com/sitemap.xml"}`))
	require.NoError(t, err)
	var sitemap Sitemap
	require.NoError(t, json.Unmarshal(attempt.Value, &sitemap))
	require.Equal(t, "https://blog.example.com/blog/a", sitemap.Entries[0].URL)

	attempt, err = readFeed.RunAttempt(singledispatchtest.NewContext("agent-host", "unknown-field"), json.RawMessage(`{"url":"https://blog.example.com/feed.xml","feedUrl":"x"}`))
	require.Error(t, err, "the input schema rejects an unknown field before the operation runs")
	require.False(t, attempt.HasInvokedOperation)
}
