//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package blogpostcheck

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/web-feed/go"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/web-feed/go/webfeedmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestBlogPostCheckReadsTheAdvertisedFeedWithRealDex(t *testing.T) {
	mock := webfeedmock.New(t, ConnectionName)
	harness := newBlogPostCheckHarness(t, mock.Connection())
	report := harness.requireCompletedReport(t, CheckRequest{SiteURL: "https://blog.example.com/", PublishedAfter: "2026-11-05T00:00:00Z"})
	require.Equal(t, SourceFeed, report.Source)
	require.Equal(t, "https://blog.example.com/feed.xml", report.SourceURL)
	require.Equal(t, `"6f1e-5c2a"`, report.ETag)
	require.Equal(t, []string{"https://blog.example.com/posts/faster-builds"}, postURLs(report), "only the post after publishedAfter is reported")
	require.Equal(t, "https://blog.example.com/", mock.DiscoverFeeds().Calls()[0].Input.URL)
	require.Equal(t, "https://blog.example.com/feed.xml", mock.ReadFeed().Calls()[0].Input.URL, "the first advertised feed is read")
	require.Empty(t, mock.ReadSitemap().Calls())
}

func TestBlogPostCheckReportsAnUnchangedFeedWithRealDex(t *testing.T) {
	mock := webfeedmock.New(t, ConnectionName)
	mock.ReadFeed().Respond(webfeedmock.ReadFeedUnchanged())
	harness := newBlogPostCheckHarness(t, mock.Connection())
	report := harness.requireCompletedReport(t, CheckRequest{FeedURL: "https://blog.example.com/feed.xml", ETag: `"6f1e-5c2a"`, LastModified: "Tue, 10 Nov 2026 09:30:00 GMT"})
	require.True(t, report.IsUnchanged)
	require.Empty(t, report.Posts)
	require.Equal(t, `"6f1e-5c2a"`, report.ETag)
	calls := mock.ReadFeed().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, `"6f1e-5c2a"`, calls[0].Input.ETag)
	require.Equal(t, "Tue, 10 Nov 2026 09:30:00 GMT", calls[0].Input.LastModified)
	require.Equal(t, webfeed.ReadFeedBranchNotModified, calls[0].Branch)
	require.Empty(t, mock.DiscoverFeeds().Calls(), "a feedUrl skips discovery")
}

// The sitemap path reads an index, its child urlsets, and the newest post pages through real Dex Steps.
func TestBlogPostCheckFallsBackToTheSitemapWithRealDex(t *testing.T) {
	mock := webfeedmock.New(t, ConnectionName)
	mock.DiscoverFeeds().Respond(webfeedmock.DiscoverFeedsDiscovered(webfeed.DiscoveredFeeds{FinalURL: "https://www.example.com/blog/", Feeds: []webfeed.DiscoveredFeed{}}, nil))
	newest, older := time.Date(2026, 11, 9, 18, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 8, 15, 0, 0, time.UTC)
	mock.ReadSitemap().Respond(
		webfeedmock.ReadSitemapRead(webfeed.Sitemap{Kind: webfeed.SitemapKindIndex, FinalURL: "https://www.example.com/sitemap.xml", Entries: []webfeed.SitemapEntry{}, Sitemaps: []webfeed.ChildSitemap{
			{URL: "https://www.example.com/sitemap-posts.xml"}, {URL: "https://www.example.com/sitemap-pages.xml"},
		}}, nil),
		webfeedmock.ReadSitemapRead(webfeed.Sitemap{Kind: webfeed.SitemapKindURLSet, FinalURL: "https://www.example.com/sitemap-posts.xml", Sitemaps: []webfeed.ChildSitemap{}, Entries: []webfeed.SitemapEntry{
			{URL: "https://www.example.com/blog/older", LastModifiedAt: &older},
		}}, nil),
		webfeedmock.ReadSitemapRead(webfeed.Sitemap{Kind: webfeed.SitemapKindURLSet, FinalURL: "https://www.example.com/sitemap-pages.xml", Sitemaps: []webfeed.ChildSitemap{}, Entries: []webfeed.SitemapEntry{
			{URL: "https://www.example.com/blog/newest", LastModifiedAt: &newest},
		}}, nil),
	)
	mock.ReadPageMetadata().Respond(
		webfeedmock.ReadPageMetadataRead(webfeed.PageMetadata{FinalURL: "https://www.example.com/blog/newest", Title: "Newest post", PublishedAt: &newest}, nil),
		webfeedmock.ReadPageMetadataUnpublished(),
	)
	harness := newBlogPostCheckHarness(t, mock.Connection())
	report := harness.requireCompletedReport(t, CheckRequest{SiteURL: "https://www.example.com/blog/", PathPrefix: "/blog/", PublishedAfter: "2026-11-01T00:00:00Z", MaxPosts: 2})
	require.Equal(t, SourceSitemap, report.Source)
	require.Equal(t, "https://www.example.com/sitemap.xml", report.SourceURL)
	require.Equal(t, []Post{{URL: "https://www.example.com/blog/newest", Title: "Newest post", PublishedAt: &newest}}, report.Posts, "the removed older post is skipped")
	sitemapCalls := mock.ReadSitemap().Calls()
	require.Len(t, sitemapCalls, 3)
	require.Equal(t, "https://www.example.com/sitemap.xml", sitemapCalls[0].Input.URL, "the default sitemap is at the page origin")
	require.Equal(t, "/blog/", sitemapCalls[0].Input.PathPrefix)
	require.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), *sitemapCalls[0].Input.LastModifiedAfter)
	require.Equal(t, 2, sitemapCalls[0].Input.Limit)
	pageCalls := mock.ReadPageMetadata().Calls()
	require.Equal(t, []string{"https://www.example.com/blog/newest", "https://www.example.com/blog/older"}, []string{pageCalls[0].Input.URL, pageCalls[1].Input.URL})
	require.Equal(t, webfeed.ReadPageMetadataBranchNotFound, pageCalls[1].Branch)
}

func TestBlogPostCheckReadsTheFirstPostTextWithRealDex(t *testing.T) {
	mock := webfeedmock.New(t, ConnectionName)
	harness := newBlogPostCheckHarness(t, mock.Connection())
	report := harness.requireCompletedReport(t, CheckRequest{FeedURL: "https://blog.example.com/feed.xml", IncludeFirstPostText: true})
	require.Len(t, report.Posts, 2)
	require.True(t, strings.HasPrefix(report.FirstPostText, "Spring release notes\n\n"), report.FirstPostText)
	calls := mock.ReadPage().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "https://blog.example.com/posts/faster-builds", calls[0].Input.URL, "the first reported post is read")
	require.Equal(t, 2000, calls[0].Input.MaxTextCharacters)

	withoutText := harness.requireCompletedReport(t, CheckRequest{FeedURL: "https://blog.example.com/feed.xml"})
	require.Empty(t, withoutText.FirstPostText)
	require.Len(t, mock.ReadPage().Calls(), 1, "the text is read only on request")
}

func TestBlogPostCheckRetriesATransientFeedFailureWithRealDex(t *testing.T) {
	mock := webfeedmock.New(t, ConnectionName)
	mock.ReadFeed().Respond(
		webfeedmock.ReadFeedRetry(sdkgo.Failure{Kind: sdkgo.FailureAvailability, Message: "the site answered HTTP 503"}, time.Second),
		webfeedmock.ReadFeedEngineeringBlog(),
	)
	harness := newBlogPostCheckHarness(t, mock.Connection())
	report := harness.requireCompletedReport(t, CheckRequest{FeedURL: "https://blog.example.com/feed.xml"})
	require.Len(t, report.Posts, 2)
	require.Len(t, mock.ReadFeed().Calls(), 2, "Dex retried the Step once")
}

func TestBlogPostCheckFailsOnAnUnwiredBranchWithRealDex(t *testing.T) {
	mock := webfeedmock.New(t, ConnectionName)
	mock.ReadFeed().Respond(webfeedmock.ReadFeedRemoved())
	harness := newBlogPostCheckHarness(t, mock.Connection())
	result := harness.waitForFlow(t, harness.startFlow(t, CheckRequest{FeedURL: "https://blog.example.com/feed.xml"}))
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "notFound")
}

func TestBlogPostCheckRejectsAnInvalidRequestWithRealDex(t *testing.T) {
	mock := webfeedmock.New(t, ConnectionName)
	harness := newBlogPostCheckHarness(t, mock.Connection())
	result := harness.waitForFlow(t, harness.startFlow(t, CheckRequest{SiteURL: "https://blog.example.com/", MaxPosts: 50}))
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "maxPosts")
	require.Empty(t, mock.DiscoverFeeds().Calls())
}

func postURLs(report CheckReport) []string {
	urls := []string{}
	for _, post := range report.Posts {
		urls = append(urls, post.URL)
	}
	return urls
}

// blogPostCheckHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type blogPostCheckHarness struct {
	flow   *BlogPostCheck
	client *dex.Client
}

func newBlogPostCheckHarness(t *testing.T, connection webfeed.Connection) *blogPostCheckHarness {
	t.Helper()
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	flow := NewBlogPostCheck(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), <-workerResult, client.Close(), cache.Close()))
	})
	registrationContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(registrationContext))
	return &blogPostCheckHarness{flow: flow, client: client}
}

func (harness *blogPostCheckHarness) requireCompletedReport(t *testing.T, request CheckRequest) CheckReport {
	t.Helper()
	flowID := harness.startFlow(t, request)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var report CheckReport
	require.NoError(t, result.DecodeSingleOutput(&report))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var display map[string]any
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, harness.flow.GetDexDisplay, nil, &display))
	encoded, err := json.Marshal(display["blog-post-check-report"])
	require.NoError(t, err)
	var displayed CheckReport
	require.NoError(t, json.Unmarshal(encoded, &displayed))
	require.Equal(t, report, displayed, "the display view reads the stored report")
	return report
}

func (harness *blogPostCheckHarness) startFlow(t *testing.T, request CheckRequest) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "blog-post-check-" + t.Name() + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, request, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *blogPostCheckHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for {
		result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err, "Flow %s did not close", flowID)
		return result
	}
}
