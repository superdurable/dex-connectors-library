// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pinterest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListBoardsProjectsPageAndClassifiesProviderResponses(t *testing.T) {
	status := http.StatusOK
	body := `{"items":[{"id":"987654321","name":"Example Board","description":"discard-me"}],"bookmark":"next-page"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v5/boards", r.URL.Path)
		require.Equal(t, "2", r.URL.Query().Get("page_size"))
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		w.WriteHeader(status)
		_, err := w.Write([]byte(body))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	input := ListBoardsInput{PageSize: 2}
	result, err := sdkgo.RunQuery(queryContext{}, client.ListBoards(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, ListBoardsBranchListed, result.Branch)
	require.Equal(t, BoardsPage{Boards: []Board{{ID: "987654321", Name: "Example Board"}}, NextBookmark: "next-page"}, result.Value)
	for _, retryStatus := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status = retryStatus
		_, err = sdkgo.RunQuery(queryContext{}, client.ListBoards(), testConnection, input)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
	status = http.StatusForbidden
	result, err = sdkgo.RunQuery(queryContext{}, client.ListBoards(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, ListBoardsBranchProviderRejected, result.Branch)
	status = http.StatusOK
	body = `{"items":{}}`
	result, err = sdkgo.RunQuery(queryContext{}, client.ListBoards(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, ListBoardsBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListBoards(), testConnection, ListBoardsInput{PageSize: 251})
	require.NoError(t, err)
	require.Equal(t, ListBoardsBranchDefect, result.Branch)
}
