// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package heap

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

type providerContext struct{ dex.Context }

func (providerContext) FlowID() string              { return "heap-provider-test" }
func (providerContext) StepExecutionID() string     { return "provider-step" }
func (providerContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (providerContext) Done() <-chan struct{}       { return nil }
func (providerContext) Err() error                  { return nil }
func (providerContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "heap", Name: "heap"}

func newProviderClient(t *testing.T, address string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: address, AppID: "12345"}, nil)
	require.NoError(t, err)
	return client
}

func TestSetUserPropertiesSendsOnlySelectedValues(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/add_user_properties", request.URL.Path)
		require.Empty(t, request.Header.Get("Authorization"))
		var payload struct {
			AppID      string            `json:"app_id"`
			Identity   string            `json:"identity"`
			Properties map[string]string `json:"properties"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.Equal(t, "12345", payload.AppID)
		require.Equal(t, "member-1", payload.Identity)
		require.Equal(t, map[string]string{"Plan": "Pro"}, payload.Properties)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	input := SetUserPropertiesInput{Identity: "member-1", Properties: map[string]string{"Plan": "Pro"}}
	result, err := sdkgo.RunMutation(providerContext{}, newProviderClient(t, server.URL).SetUserProperties(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SetUserPropertiesBranchAccepted, result.Branch)
	require.True(t, result.Value.Accepted)
	require.Equal(t, 1, calls)
}

func TestSetUserPropertiesRejectsInvalidInputBeforeRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider request") }))
	defer server.Close()
	for _, input := range []SetUserPropertiesInput{
		{Identity: "", Properties: map[string]string{"Plan": "Pro"}},
		{Identity: "member-1", Properties: map[string]string{"ID": "1"}},
		{Identity: "member-1", Properties: nil},
	} {
		result, err := sdkgo.RunMutation(providerContext{}, newProviderClient(t, server.URL).SetUserProperties(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, SetUserPropertiesBranchDefect, result.Branch)
	}
}

func TestSetUserPropertiesClassifiesProviderStatuses(t *testing.T) {
	cases := []struct {
		status int
		branch sdkgo.BranchID
		retry  bool
	}{
		{status: 400, branch: SetUserPropertiesBranchProviderRejected},
		{status: 429, retry: true},
		{status: 503, retry: true},
	}
	for _, testCase := range cases {
		t.Run(http.StatusText(testCase.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(testCase.status) }))
			defer server.Close()
			result, err := sdkgo.RunMutation(providerContext{}, newProviderClient(t, server.URL).SetUserProperties(), testConnection, SetUserPropertiesInput{Identity: "member-1", Properties: map[string]string{"Plan": "Pro"}})
			if testCase.retry {
				require.Error(t, err)
				require.Empty(t, result.Branch)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.branch, result.Branch)
			}
		})
	}
}
