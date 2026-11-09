// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braze

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "braze", Name: "braze"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "braze-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-user-profile" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{RESTAPIKey: sdkgo.NewSecretString("sample-rest-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetUserProfileRequestsOnlySelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/users/export/ids", request.URL.Path)
		require.Equal(t, "Bearer sample-rest-key", request.Header.Get("Authorization"))
		var payload struct {
			ExternalIDs    []string `json:"external_ids"`
			FieldsToExport []string `json:"fields_to_export"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.Equal(t, []string{"sample-user-1"}, payload.ExternalIDs)
		require.Equal(t, []string{"external_id", "braze_id", "first_name", "last_name", "email"}, payload.FieldsToExport)
		_, err := writer.Write([]byte(`{"message":"success","users":[{"external_id":"sample-user-1","braze_id":"braze-123","first_name":"Avery","email":"reader@example.com","custom_attributes":{"secret":"must not surface"}}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetUserProfile(), testConnection, GetUserProfileInput{ExternalID: "sample-user-1"})
	require.NoError(t, err)
	require.Equal(t, GetUserProfileBranchFound, result.Branch)
	require.Equal(t, UserProfile{ExternalID: "sample-user-1", BrazeID: "braze-123", FirstName: "Avery", Email: "reader@example.com"}, result.Value)
}

func TestGetUserProfileClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 200, body: `{"message":"success","users":[]}`, branch: GetUserProfileBranchNotFound},
		{name: "forbidden", status: 403, branch: GetUserProfileBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"message":"success","users":[{"external_id":"other"}]}`, branch: GetUserProfileBranchInvalidResponse},
		{name: "unexpected users", status: 200, body: `{"message":"success","users":[{},{}]}`, branch: GetUserProfileBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetUserProfile(), testConnection, GetUserProfileInput{ExternalID: "sample-user-1"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestGetUserProfileRejectsInvalidInputAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://rest.iad-01.braze.com", 0)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetUserProfile(), testConnection, GetUserProfileInput{})
	require.NoError(t, err)
	require.Equal(t, GetUserProfileBranchDefect, result.Branch)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"message":"success","users":[{"external_id":"sample-user-1"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).GetUserProfile(), testConnection, GetUserProfileInput{ExternalID: "sample-user-1"})
	require.NoError(t, err)
	require.Equal(t, GetUserProfileBranchInvalidResponse, result.Branch)
}
