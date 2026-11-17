//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package storysearch

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hackernews/go"
	"github.com/superdurable/dex-connectors-library/connectors/hackernews/go/hackernewsmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStorySearchExampleUsesHackerNewsMockWithRealDex(t *testing.T) {
	mock := hackernewsmock.New(t, ConnectionName)
	flow := NewStorySearchFlow(mock.Connection())
	worker := startTestWorker(t, flow)

	t.Run("reads every page and keeps each story once", func(t *testing.T) {
		flowID := fmt.Sprintf("hn-search-mock-%d", time.Now().UnixNano())
		startedAt := time.Now().UTC()
		_, err := worker.client.StartFlow(context.Background(), flow, flowID, Request{Query: "compiler", LookupFirstAuthor: true, ReadFirstThread: true}, dex.StartFlowOptions{})
		require.NoError(t, err)
		result := waitForFlow(t, worker.client, flowID)
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var report Report
		require.NoError(t, result.DecodeSingleOutput(&report))
		// The default mock repeats story 42001 across a page boundary.
		require.Equal(t, []int64{42003, 42001, 41990, 41980}, storyIDs(report.Stories))
		require.Equal(t, 3, report.PagesRead)
		require.False(t, report.HasUnreadPages)
		require.Empty(t, report.Stories[2].URL, "the Ask HN text post has no source URL")
		require.NotNil(t, report.FirstAuthor)
		require.Equal(t, "langtools", report.FirstAuthor.ID)
		require.Equal(t, "langtools", mock.GetUser().Calls()[0].Input.ID)
		require.NotNil(t, report.FirstThread)
		require.Equal(t, int64(42003), report.FirstThread.ID)
		require.Equal(t, int64(42003), mock.GetArticleThread().Calls()[0].Input.ID)

		calls := mock.SearchStories().Calls()
		require.Len(t, calls, 3)
		for page, call := range calls {
			require.Equal(t, hackernews.SearchStoriesInput{
				Query: "compiler", SortBy: hackernews.StorySearchSortDate, CreatedAfter: report.CreatedAfter, Page: page, HitsPerPage: 50,
			}, call.Input)
		}
		require.WithinDuration(t, startedAt.Add(-24*time.Hour), report.CreatedAfter, time.Minute)
	})

	t.Run("stops at the page budget", func(t *testing.T) {
		flowID := fmt.Sprintf("hn-search-mock-budget-%d", time.Now().UnixNano())
		_, err := worker.client.StartFlow(context.Background(), flow, flowID, Request{Query: "compiler", MaxPages: 2}, dex.StartFlowOptions{})
		require.NoError(t, err)
		result := waitForFlow(t, worker.client, flowID)
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var report Report
		require.NoError(t, result.DecodeSingleOutput(&report))
		require.Equal(t, []int64{42003, 42001, 41990}, storyIDs(report.Stories))
		require.Equal(t, 2, report.PagesRead)
		require.True(t, report.HasUnreadPages)
	})

	t.Run("searches public comments", func(t *testing.T) {
		mock.SearchItems().Respond(hackernewsmock.SearchItemsSearched(hackernews.ItemSearchPage{
			Items: []hackernews.ItemSearchHit{{ID: 42002, Type: "comment", Text: "A useful compiler note", DiscussionURL: "https://news.ycombinator.com/item?id=42002"}},
			Page:  0, Pages: 1, TotalHits: 1, HitsPerPage: 50,
		}, nil))
		flowID := fmt.Sprintf("hn-comment-search-mock-%d", time.Now().UnixNano())
		_, err := worker.client.StartFlow(context.Background(), flow, flowID, Request{Query: "compiler", ItemType: "comment"}, dex.StartFlowOptions{})
		require.NoError(t, err)
		result := waitForFlow(t, worker.client, flowID)
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var report Report
		require.NoError(t, result.DecodeSingleOutput(&report))
		require.Equal(t, "comment", report.Items[0].Type)
		require.Equal(t, "comment", mock.SearchItems().Calls()[0].Input.Type)
	})
}

func storyIDs(stories []hackernews.StorySearchHit) []int64 {
	ids := make([]int64, 0, len(stories))
	for _, story := range stories {
		ids = append(ids, story.ID)
	}
	return ids
}
