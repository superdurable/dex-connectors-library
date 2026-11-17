//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threadreply

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail/go"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSearchMailboxListsMessagesWithRealDex(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/users/me/messages", request.URL.Path)
		require.Equal(t, "from:ada@example.com", request.URL.Query().Get("q"))
		require.Equal(t, "2", request.URL.Query().Get("maxResults"))
		require.Equal(t, "Bearer gmail-token", request.Header.Get("Authorization"))
		response.Header().Set("Content-Type", "application/json")
		_, err := response.Write([]byte(`{"messages":[{"id":"message-1","threadId":"thread-1"}],"nextPageToken":"next","resultSizeEstimate":1}`))
		require.NoError(t, err)
	}))
	defer provider.Close()
	threadFlow, harness := newGmailIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	flow := NewSearchMailboxFlow(threadFlow.connection)
	flowID := "gmail-search-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, flow, flowID, SearchMailboxInput{Query: "from:ada@example.com", MaxResults: 2}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var page gmail.MessagesPage
	require.NoError(t, result.DecodeSingleOutput(&page))
	require.Equal(t, "message-1", page.Messages[0].ID)
	require.Equal(t, "next", page.NextPageToken)
}
