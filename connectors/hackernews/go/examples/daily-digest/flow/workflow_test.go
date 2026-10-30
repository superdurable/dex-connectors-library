// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dailydigest

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hackernews"
)

func TestDigestLinksFollowSummaryAndCitationsStayWithinEvidence(t *testing.T) {
	current := state{Request: Request{MaxItems: 8}, Candidates: map[int64]candidate{
		42: {Story: hackernews.Item{ID: 42, Title: "Release <script>", URL: "https://example.com/a_(b)", DiscussionURL: "https://news.ycombinator.com/item?id=42"}, Comments: []hackernews.Item{{ID: 43, DiscussionURL: "https://news.ycombinator.com/item?id=43"}}},
	}}
	selection := Selection{ID: 42, Summary: "A release was posted.", Why: "Useful to developers.", Discussion: "One commenter reported a limitation.", CommentIDs: []int64{43}}
	digest, err := assembleDigest(current, []Selection{selection})
	require.NoError(t, err)
	require.Less(t, strings.Index(digest.Markdown, selection.Summary), strings.Index(digest.Markdown, "[Original]"))
	require.Contains(t, digest.Markdown, "https://example.com/a_%28b%29")
	require.Contains(t, digest.Markdown, "https://news.ycombinator.com/item?id=43")
	require.NotContains(t, digest.Markdown, "<script>")
	selection.CommentIDs = []int64{99}
	_, err = assembleDigest(current, []Selection{selection})
	require.ErrorContains(t, err, "outside the story evidence")
	selection.CommentIDs = []int64{43}
	_, err = assembleDigest(current, []Selection{selection, selection})
	require.ErrorContains(t, err, "repeated")
	selection.ID = 99
	_, err = assembleDigest(current, []Selection{selection})
	require.ErrorContains(t, err, "unknown story")
}

func TestCandidateBoundRetainsRecentDiscoveries(t *testing.T) {
	candidates := map[int64]candidate{}
	for id := int64(1); id <= 60; id++ {
		candidates[id] = candidate{Story: hackernews.Item{ID: id, Score: int(61 - id), Time: time.Now().Unix() + id}}
	}
	retained := retainCandidates(candidates)
	require.Len(t, retained, 40)
	require.Contains(t, retained, int64(1))
	require.Contains(t, retained, int64(60))
}

func TestOriginalURLIdentityAndRendering(t *testing.T) {
	require.Equal(t, storyKey(hackernews.Item{URL: "https://EXAMPLE.com/release#section"}), storyKey(hackernews.Item{URL: "https://example.com/release"}))
	require.NotEqual(t, storyKey(hackernews.Item{ID: 1}), storyKey(hackernews.Item{ID: 2}))
	require.Empty(t, safeLink("javascript:alert(1)"))
}
