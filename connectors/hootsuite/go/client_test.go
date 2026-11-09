// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hootsuite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessToken = "synthetic-hootsuite-token"

var testConnection = sdkgo.ConnectionRef{Provider: "hootsuite", Name: "hootsuite"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "hootsuite-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	credentials := Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token")}
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestListSocialProfilesProjectsProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/socialProfiles", r.URL.Path)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.RawQuery)
		_, err := w.Write([]byte(`{"data":[{"id":"115185509","type":"FACEBOOK","socialNetworkUsername":"Example Page","socialNetworkId":"1261768027866378","ownerId":"private"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListSocialProfiles(), testConnection, ListSocialProfilesInput{})
	require.NoError(t, err)
	require.Equal(t, ListSocialProfilesBranchListed, result.Branch)
	require.Equal(t, "115185509", result.Value.Profiles[0].ID)
	require.Equal(t, "Example Page", result.Value.Profiles[0].Username)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestListSocialProfilesClassifiesFailures(t *testing.T) {
	var calls atomic.Int32
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(status.Load()))
		_, err := w.Write([]byte(`{"errors":[{"message":"unavailable"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunQuery(queryContext{}, client.ListSocialProfiles(), testConnection, ListSocialProfilesInput{})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
	status.Store(http.StatusForbidden)
	rejected, err := sdkgo.RunQuery(queryContext{}, client.ListSocialProfiles(), testConnection, ListSocialProfilesInput{})
	require.NoError(t, err)
	require.Equal(t, ListSocialProfilesBranchProviderRejected, rejected.Branch)
	require.Equal(t, int32(3), calls.Load())
}

func TestListSocialProfilesRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{"data":[{"id":"115185509","type":"FACEBOOK"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListSocialProfiles(), testConnection, ListSocialProfilesInput{})
	require.NoError(t, err)
	require.Equal(t, ListSocialProfilesBranchInvalidResponse, result.Branch)
}
