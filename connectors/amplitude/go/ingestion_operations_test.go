// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package amplitude

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestTrackEventSendsStableInsertID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/2/httpapi", request.URL.Path)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var payload struct {
			APIKey string `json:"api_key"`
			Events []struct {
				UserID     string            `json:"user_id"`
				EventType  string            `json:"event_type"`
				InsertID   string            `json:"insert_id"`
				Properties map[string]string `json:"event_properties"`
			} `json:"events"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.Equal(t, "sample-amplitude-api-key", payload.APIKey)
		require.Len(t, payload.Events, 1)
		require.Equal(t, "example-user", payload.Events[0].UserID)
		require.Equal(t, "purchase", payload.Events[0].EventType)
		require.NotEmpty(t, payload.Events[0].InsertID)
		require.Equal(t, "pro", payload.Events[0].Properties["plan"])
		_, err := response.Write([]byte(`{"code":200,"events_ingested":1,"payload_size_bytes":100,"server_upload_time":1396381378123}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).TrackEvent(), testConnection, TrackEventInput{UserID: "example-user", EventType: "purchase", EventProperties: map[string]string{"plan": "pro"}})
	require.NoError(t, err)
	require.Equal(t, TrackEventBranchTracked, result.Branch)
	require.NotEmpty(t, result.Value.InsertID)
	require.Equal(t, string(result.Receipt.IdempotencyKey), result.Value.InsertID)
}

func TestIdentifyUserAbsolutelySetsOneProperty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/identify", request.URL.Path)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		form, err := url.ParseQuery(string(body))
		require.NoError(t, err)
		require.Equal(t, "sample-amplitude-api-key", form.Get("api_key"))
		require.JSONEq(t, `{"user_id":"example-user","user_properties":{"$set":{"plan":"pro"}}}`, form.Get("identification"))
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).IdentifyUser(), testConnection, IdentifyUserInput{UserID: "example-user", PropertyName: "plan", PropertyValue: "pro"})
	require.NoError(t, err)
	require.Equal(t, IdentifyUserBranchIdentified, result.Branch)
}

func TestIngestionWritesClassifyProviderOutcomes(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(status) }))
		client := newTestClient(t, server.URL, 0)
		for _, invoke := range []func() (sdkgo.BranchID, error){
			func() (sdkgo.BranchID, error) {
				result, err := sdkgo.RunMutation(queryContext{}, client.TrackEvent(), testConnection, TrackEventInput{UserID: "example-user", EventType: "purchase"})
				return result.Branch, err
			},
			func() (sdkgo.BranchID, error) {
				result, err := sdkgo.RunMutation(queryContext{}, client.IdentifyUser(), testConnection, IdentifyUserInput{UserID: "example-user", PropertyName: "plan", PropertyValue: "pro"})
				return result.Branch, err
			},
		} {
			branch, err := invoke()
			if status == http.StatusForbidden {
				require.NoError(t, err)
				require.Equal(t, sdkgo.BranchID("providerRejected"), branch)
			} else {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.False(t, strings.Contains(retry.Error(), "sample-amplitude-secret-key"))
			}
		}
		server.Close()
	}
}

func TestTrackEventRetriesMalformedSuccessWithoutChangingInsertID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"code":200,"events_ingested":0}`))
	}))
	defer server.Close()
	_, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).TrackEvent(), testConnection, TrackEventInput{UserID: "example-user", EventType: "purchase"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
