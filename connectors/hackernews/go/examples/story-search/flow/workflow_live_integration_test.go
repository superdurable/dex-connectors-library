//go:build integration && live

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
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStorySearchLiveCommentsWithRealDex(t *testing.T) {
	provider, err := hackernews.New(hackernews.Config{}, nil)
	require.NoError(t, err)
	connection, err := hackernews.NewConnection(provider, sdkgo.ConnectionRef{Provider: "hacker-news", Name: ConnectionName})
	require.NoError(t, err)
	flow := NewStorySearchFlow(connection)
	worker := startTestWorker(t, flow)
	flowID := fmt.Sprintf("hn-comments-live-%d", time.Now().UnixNano())
	_, err = worker.client.StartFlow(context.Background(), flow, flowID, Request{Query: "the", ItemType: "comment", WithinHours: 720, MaxPages: 1}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result := waitForFlow(t, worker.client, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var report Report
	require.NoError(t, result.DecodeSingleOutput(&report))
	require.Equal(t, 1, report.PagesRead)
	require.NotEmpty(t, report.Items)
	for _, item := range report.Items {
		require.Equal(t, "comment", item.Type)
	}
}
