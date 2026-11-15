// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package storysearch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hackernews/go"
)

func TestRequestDefaultsAndBounds(t *testing.T) {
	resolved, err := resolveRequest(Request{Query: "postgres"})
	require.NoError(t, err)
	require.Equal(t, Request{Query: "postgres", WithinHours: 24, MaxPages: 4, SortBy: hackernews.StorySearchSortDate}, resolved)

	resolved, err = resolveRequest(Request{Query: "postgres", WithinHours: 720, MaxPages: 20, SortBy: hackernews.StorySearchSortRelevance})
	require.NoError(t, err)
	require.Equal(t, hackernews.StorySearchSortRelevance, resolved.SortBy)

	for _, request := range []Request{
		{}, {Query: "  "}, {Query: "postgres", WithinHours: -1}, {Query: "postgres", WithinHours: 721},
		{Query: "postgres", MaxPages: -1}, {Query: "postgres", MaxPages: 21},
	} {
		_, err := resolveRequest(request)
		require.Error(t, err, "%+v", request)
	}
}

func TestSearchInputKeepsTheStartWindowForEveryPage(t *testing.T) {
	createdAfter := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	report := Report{
		Request:      Request{Query: "rust compiler", Tags: []hackernews.StoryTag{hackernews.StoryTagShowHN}, MinPoints: 5, SortBy: hackernews.StorySearchSortDate},
		CreatedAfter: createdAfter,
	}
	require.Equal(t, hackernews.SearchStoriesInput{
		Query: "rust compiler", SortBy: hackernews.StorySearchSortDate, Tags: []hackernews.StoryTag{hackernews.StoryTagShowHN},
		CreatedAfter: createdAfter, MinPoints: 5, Page: 3, HitsPerPage: 50,
	}, searchInput(report, 3))
}

func TestAppendNewStoriesKeepsPageOrderAndSkipsRepeats(t *testing.T) {
	kept := appendNewStories(nil, []hackernews.StorySearchHit{{ID: 3}, {ID: 2}})
	kept = appendNewStories(kept, []hackernews.StorySearchHit{{ID: 2}, {ID: 1}})
	require.Equal(t, []hackernews.StorySearchHit{{ID: 3}, {ID: 2}, {ID: 1}}, kept)
}
