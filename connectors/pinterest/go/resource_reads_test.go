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

func TestGetBoardProjectsIdentityAndClassifiesResponses(t *testing.T) {
	status := http.StatusOK
	body := `{"id":"987654321","name":"Example Board","description":"discard-me"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v5/boards/987654321", r.URL.Path)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		w.WriteHeader(status)
		_, err := w.Write([]byte(body))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	input := GetBoardInput{BoardID: "987654321"}
	result, err := sdkgo.RunQuery(queryContext{}, client.GetBoard(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetBoardBranchFound, result.Branch)
	require.Equal(t, Board{ID: "987654321", Name: "Example Board"}, result.Value)
	status = http.StatusNotFound
	result, err = sdkgo.RunQuery(queryContext{}, client.GetBoard(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetBoardBranchNotFound, result.Branch)
	status = http.StatusForbidden
	result, err = sdkgo.RunQuery(queryContext{}, client.GetBoard(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetBoardBranchProviderRejected, result.Branch)
	status = http.StatusOK
	body = `{"id":"bad"}`
	result, err = sdkgo.RunQuery(queryContext{}, client.GetBoard(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetBoardBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetBoard(), testConnection, GetBoardInput{BoardID: "bad/id"})
	require.NoError(t, err)
	require.Equal(t, GetBoardBranchDefect, result.Branch)
	for _, retryStatus := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status = retryStatus
		_, err = sdkgo.RunQuery(queryContext{}, client.GetBoard(), testConnection, input)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
}

func TestGetPinProjectsIdentityAndClassifiesResponses(t *testing.T) {
	status := http.StatusOK
	body := `{"id":"123456789","board_id":"987654321","title":"Example Pin","media":{"images":"discard-me"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v5/pins/123456789", r.URL.Path)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		w.WriteHeader(status)
		_, err := w.Write([]byte(body))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	input := GetPinInput{PinID: "123456789"}
	result, err := sdkgo.RunQuery(queryContext{}, client.GetPin(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetPinBranchFound, result.Branch)
	require.Equal(t, Pin{ID: "123456789", BoardID: "987654321", Title: "Example Pin"}, result.Value)
	status = http.StatusNotFound
	result, err = sdkgo.RunQuery(queryContext{}, client.GetPin(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetPinBranchNotFound, result.Branch)
	status = http.StatusForbidden
	result, err = sdkgo.RunQuery(queryContext{}, client.GetPin(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetPinBranchProviderRejected, result.Branch)
	status = http.StatusOK
	body = `{"id":"bad"}`
	result, err = sdkgo.RunQuery(queryContext{}, client.GetPin(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetPinBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetPin(), testConnection, GetPinInput{PinID: "bad/id"})
	require.NoError(t, err)
	require.Equal(t, GetPinBranchDefect, result.Branch)
	for _, retryStatus := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status = retryStatus
		_, err = sdkgo.RunQuery(queryContext{}, client.GetPin(), testConnection, input)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
}
