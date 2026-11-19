//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package generatesummary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini/go"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStoreInventoryListsFileSearchStoresWithRealDex(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1beta/fileSearchStores", request.URL.Path)
		require.Equal(t, "2", request.URL.Query().Get("pageSize"))
		require.Equal(t, integrationAPIKey, request.Header.Get("x-goog-api-key"))
		response.Header().Set("Content-Type", "application/json")
		_, err := response.Write([]byte(`{"fileSearchStores":[{"name":"fileSearchStores/team-docs-abc123def456","displayName":"Team docs"}],"nextPageToken":"next"}`))
		require.NoError(t, err)
	}))
	defer provider.Close()
	summaryFlow, harness := newSummaryIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	flow := NewStoreInventoryFlow(summaryFlow.connection)
	flowID := "gemini-inventory-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, flow, flowID, StoreInventoryRequest{PageSize: 2}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var page gemini.FileSearchStoresPage
	require.NoError(t, result.DecodeSingleOutput(&page))
	require.Equal(t, "fileSearchStores/team-docs-abc123def456", page.FileSearchStores[0].Name)
	require.Equal(t, "next", page.NextPageToken)
}
