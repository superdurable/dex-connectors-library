//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package storysearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hackernews/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStorySearchWithRealDex(t *testing.T) {
	provider := newFakeSearchAPI(t)
	client, err := hackernews.New(hackernews.Config{SearchBaseURL: provider.server.URL}, nil)
	require.NoError(t, err)
	connection, err := hackernews.NewConnection(client, sdkgo.ConnectionRef{Provider: "hacker-news", Name: ConnectionName})
	require.NoError(t, err)
	flow := NewStorySearchFlow(connection)
	worker := startTestWorker(t, flow)

	t.Run("real retry, window filter, show tag, and repeated story across pages", func(t *testing.T) {
		flowID := fmt.Sprintf("hn-search-%d", time.Now().UnixNano())
		request := Request{Query: "postgres", WithinHours: 48, Tags: []hackernews.StoryTag{hackernews.StoryTagShowHN}, MinPoints: 2}
		_, err := worker.client.StartFlow(context.Background(), flow, flowID, request, dex.StartFlowOptions{})
		require.NoError(t, err)
		result := waitForFlow(t, worker.client, flowID)
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var report Report
		require.NoError(t, result.DecodeSingleOutput(&report))
		require.Equal(t, []int64{503, 502, 501}, storyIDs(report.Stories))
		require.Equal(t, 2, report.PagesRead)
		require.Equal(t, 3, report.TotalHits)
		require.Equal(t, "https://news.ycombinator.com/item?id=502", report.Stories[1].DiscussionURL)
		require.Equal(t, "First paragraph & more.\n\nSecond paragraph.", report.Stories[1].StoryText)

		requests := provider.recordedRequests()
		// The first request returned 429, so Dex retried it before reading the second page.
		require.Len(t, requests, 3)
		for index, page := range []string{"0", "0", "1"} {
			require.Equal(t, "/search_by_date", requests[index].Path)
			require.Equal(t, url.Values{
				"query": {"postgres"}, "tags": {"story,show_hn"}, "page": {page}, "hitsPerPage": {"50"},
				"numericFilters": {"created_at_i>" + strconv.FormatInt(report.CreatedAfter.Unix(), 10) + ",points>=2"},
			}, requests[index].Query)
		}
		require.WithinDuration(t, time.Now().Add(-48*time.Hour), report.CreatedAfter, time.Minute)

		var display map[string]any
		require.NoError(t, worker.client.InvokeRPC(context.Background(), flowID, flow.GetDexDisplay, nil, &display))
		encoded, err := json.Marshal(display["hacker-news-story-search-report"])
		require.NoError(t, err)
		var displayed Report
		require.NoError(t, json.Unmarshal(encoded, &displayed))
		require.Equal(t, report, displayed)
	})

	t.Run("invalid request fails before any provider request", func(t *testing.T) {
		before := len(provider.recordedRequests())
		for _, request := range []Request{{Query: " "}, {Query: "postgres", MaxPages: 21}} {
			flowID := fmt.Sprintf("hn-search-invalid-%d", time.Now().UnixNano())
			_, err := worker.client.StartFlow(context.Background(), flow, flowID, request, dex.StartFlowOptions{})
			require.NoError(t, err)
			require.Equal(t, dex.FlowFailed, waitForFlow(t, worker.client, flowID).Status)
		}
		require.Len(t, provider.recordedRequests(), before)
	})

	t.Run("unwired connector defect fails the Flow without a provider request", func(t *testing.T) {
		before := len(provider.recordedRequests())
		flowID := fmt.Sprintf("hn-search-defect-%d", time.Now().UnixNano())
		_, err := worker.client.StartFlow(context.Background(), flow, flowID, Request{Query: "postgres", MinPoints: -1}, dex.StartFlowOptions{})
		require.NoError(t, err)
		require.Equal(t, dex.FlowFailed, waitForFlow(t, worker.client, flowID).Status)
		require.Len(t, provider.recordedRequests(), before)
	})
}

type recordedSearchRequest struct {
	Path  string
	Query url.Values
}

// fakeSearchAPI serves two newest-first pages and throttles the first request once.
type fakeSearchAPI struct {
	server       *httptest.Server
	mutex        sync.Mutex
	requests     []recordedSearchRequest
	isThrottling atomic.Bool
}

func newFakeSearchAPI(t *testing.T) *fakeSearchAPI {
	t.Helper()
	provider := &fakeSearchAPI{}
	provider.isThrottling.Store(true)
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serveSearch))
	t.Cleanup(provider.server.Close)
	return provider
}

func (provider *fakeSearchAPI) serveSearch(w http.ResponseWriter, r *http.Request) {
	provider.mutex.Lock()
	provider.requests = append(provider.requests, recordedSearchRequest{Path: r.URL.Path, Query: r.URL.Query()})
	provider.mutex.Unlock()
	if provider.isThrottling.CompareAndSwap(true, false) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	now := time.Now().Unix()
	hits := []map[string]any{
		{"objectID": "503", "title": "Show HN: A Postgres extension", "url": "https://example.com/extension", "author": "builder", "points": 9, "num_comments": 2, "created_at_i": now - 600, "_tags": []string{"story", "show_hn"}},
		{"objectID": "502", "title": "Show HN: Postgres notes", "author": "writer", "points": 4, "num_comments": 1, "created_at_i": now - 3600, "story_text": "First paragraph &amp; more.<p>Second paragraph.", "_tags": []string{"story", "show_hn"}},
	}
	page, pages := 0, 2
	if r.URL.Query().Get("page") == "1" {
		page = 1
		// A newer story moved story 502 onto the second page.
		hits = []map[string]any{hits[1], {"objectID": "501", "title": "Show HN: A Postgres client", "url": "https://example.com/client", "author": "maker", "points": 3, "created_at_i": now - 7200, "_tags": []string{"story", "show_hn"}}}
	}
	if err := json.NewEncoder(w).Encode(map[string]any{"hits": hits, "page": page, "nbPages": pages, "nbHits": 3, "hitsPerPage": 50}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (provider *fakeSearchAPI) recordedRequests() []recordedSearchRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]recordedSearchRequest(nil), provider.requests...)
}

type testWorker struct {
	client *dex.Client
	worker *dex.Worker
	cache  *blobcache.Cache
	result chan error
}

func startTestWorker(t *testing.T, flow dex.Flow) *testWorker {
	t.Helper()
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: address})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address})
	require.NoError(t, err)
	suite := &testWorker{client: client, worker: worker, cache: cache, result: make(chan error, 1)}
	go func() { suite.result <- worker.Start() }()
	registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
	registrationError := worker.WaitPullRegistered(registrationContext)
	cancelRegistration()
	require.NoError(t, registrationError)
	t.Cleanup(func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, suite.worker.Stop(stopContext))
		require.NoError(t, <-suite.result)
		require.NoError(t, errors.Join(client.Close(), cache.Close()))
	})
	return suite
}

func waitForFlow(t *testing.T, client *dex.Client, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for {
		result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var timeout *dex.LongPollTimeoutError
		if errors.As(err, &timeout) {
			continue
		}
		require.NoError(t, err)
		return result
	}
}
