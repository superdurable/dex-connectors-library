//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dailydigest

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

func TestDailyDigestExampleUsesHackerNewsMockWithRealDex(t *testing.T) {
	mock := hackernewsmock.New(t, ConnectionName)
	mock.GetItem().When(func(input hackernews.GetItemInput) bool { return input.ID == 42001 },
		hackernewsmock.GetItemFound(hackernews.Item{
			ID: 42001, Type: "story", By: "compilerdev", Time: time.Now().Unix(),
			Title: "A compiler release", URL: "https://example.com/compiler",
			DiscussionURL: "https://news.ycombinator.com/item?id=42001", Score: 100,
		}, nil))
	flow := NewFlow(mock.Connection(), func(_ context.Context, prompt string) ([]Selection, error) {
		require.Contains(t, prompt, "A compiler release")
		return []Selection{{ID: 42001, Summary: "The HN post announces a compiler release.", Why: "Developers can evaluate the compiler."}}, nil
	})
	worker := startTestWorker(t, flow)
	flowID := fmt.Sprintf("hn-mock-%d", time.Now().UnixNano())
	_, err := worker.client.StartFlow(context.Background(), flow, flowID, Request{Once: true, MaxItems: 1}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result := waitForDigest(t, worker.client, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var digest Digest
	require.NoError(t, result.DecodeSingleOutput(&digest))
	require.Equal(t, 1, digest.CandidateCount)
	require.Contains(t, digest.Markdown, "https://example.com/compiler")
	require.Contains(t, digest.Markdown, "https://news.ycombinator.com/item?id=42001")

	feedCalls := mock.ListStoryIDs().Calls()
	require.Len(t, feedCalls, 5)
	require.Equal(t, []hackernews.ListStoryIDsInput{
		{Feed: "top", Limit: 30}, {Feed: "best", Limit: 10}, {Feed: "show", Limit: 10},
		{Feed: "ask", Limit: 10}, {Feed: "new", Limit: 10},
	}, []hackernews.ListStoryIDsInput{feedCalls[0].Input, feedCalls[1].Input, feedCalls[2].Input, feedCalls[3].Input, feedCalls[4].Input})
	itemCalls := mock.GetItem().Calls()
	require.Len(t, itemCalls, 1)
	require.Equal(t, hackernews.GetItemInput{ID: 42001}, itemCalls[0].Input)
}
