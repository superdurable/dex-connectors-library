// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package topicposts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reddit/go"
	"github.com/superdurable/dex-connectors-library/connectors/reddit/go/redditmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTopicPostsFlowIdentityAndMapping(t *testing.T) {
	listing := reddit.ListSubredditPostsInput{Subreddit: "golang", Sort: reddit.SubredditPostSortNew}
	require.Equal(t, listing, MapToListSubredditPostsInput(listing))
	search := reddit.SearchPostsInput{Query: "generics", Subreddit: "golang", Sort: reddit.SearchPostSortNew, TimeWindow: SearchTimeWindow}
	require.Equal(t, search, MapToSearchPostsInput(search))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(reddit.Connection{})))
	require.Equal(t, "StartRedditTopicPosts", dex.GetFinalStepType[TopicPostsRequest](startTopicPosts{}))
	wait, err := startTopicPosts{}.WaitFor(nil, TopicPostsRequest{Subreddit: "golang", Topic: "generics"})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := redditmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}

func TestSelectTopicMentionsIgnoresCaseAndPinnedPosts(t *testing.T) {
	posts := []reddit.ListedPost{
		{ID: "t3_pinned", Title: "Generics questions thread", IsStickied: true},
		{ID: "t3_title", Title: "Why GENERICS?"},
		{ID: "t3_body", Title: "Type sets", SelfText: "How do generics constraints work?"},
		{ID: "t3_other", Title: "Profiling tips"},
	}
	mentions := SelectTopicMentions(posts, "Generics")
	require.Equal(t, []string{"t3_title", "t3_body"}, postIDs(mentions))
	require.NotNil(t, SelectTopicMentions(nil, "generics"))
}

func TestMergeTopicPostsDeduplicatesAndOrdersNewestFirst(t *testing.T) {
	older := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	merged := MergeTopicPosts(
		[]reddit.ListedPost{{ID: "t3_b", CreatedAt: older}, {ID: "t3_c", CreatedAt: newer}},
		[]reddit.ListedPost{{ID: "t3_c", CreatedAt: newer}, {ID: "t3_pin", CreatedAt: newer, IsStickied: true}, {ID: "t3_a", CreatedAt: older}},
	)
	require.Equal(t, []string{"t3_c", "t3_a", "t3_b"}, postIDs(merged))
	require.NotNil(t, MergeTopicPosts(nil, nil))
}

func postIDs(posts []reddit.ListedPost) []string {
	ids := make([]string, 0, len(posts))
	for _, post := range posts {
		ids = append(ids, post.ID)
	}
	return ids
}
