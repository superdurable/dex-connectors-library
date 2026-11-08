// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tagmanager

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

const testAccessToken = "synthetic-tag-manager-token"

var testConnection = sdkgo.ConnectionRef{Provider: "google-tag-manager", Name: "google-tag-manager"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "tag-manager-provider-test" }
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

func TestListOperationsProjectPublicFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/tagmanager/v2/accounts":
			require.Equal(t, "pageToken=account-cursor", r.URL.RawQuery)
			_, err := w.Write([]byte(`{"account":[{"path":"accounts/123456","name":"Example Account","private":"discard"}],"nextPageToken":"account-next"}`))
			require.NoError(t, err)
		case "/tagmanager/v2/accounts/123456/containers":
			require.Equal(t, "pageToken=container-cursor", r.URL.RawQuery)
			_, err := w.Write([]byte(`{"container":[{"path":"accounts/123456/containers/987654","name":"Example Container","publicId":"GTM-EXAMPLE","private":"discard"}],"nextPageToken":"container-next"}`))
			require.NoError(t, err)
		case "/tagmanager/v2/accounts/123456/containers/987654/workspaces/1/tags":
			require.Equal(t, "pageToken=tag-cursor", r.URL.RawQuery)
			_, err := w.Write([]byte(`{"tag":[{"path":"accounts/123456/containers/987654/workspaces/1/tags/42","name":"Analytics Tag","type":"gaawe","private":"discard"}],"nextPageToken":"tag-next"}`))
			require.NoError(t, err)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	accounts, err := sdkgo.RunQuery(queryContext{}, client.ListAccounts(), testConnection, ListAccountsInput{PageToken: "account-cursor"})
	require.NoError(t, err)
	require.Equal(t, ListAccountsBranchListed, accounts.Branch)
	require.Equal(t, "accounts/123456", accounts.Value.Accounts[0].Path)
	containers, err := sdkgo.RunQuery(queryContext{}, client.ListContainers(), testConnection, ListContainersInput{AccountID: "123456", PageToken: "container-cursor"})
	require.NoError(t, err)
	require.Equal(t, ListContainersBranchListed, containers.Branch)
	require.Equal(t, "GTM-EXAMPLE", containers.Value.Containers[0].PublicID)
	tags, err := sdkgo.RunQuery(queryContext{}, client.ListTags(), testConnection, ListTagsInput{AccountID: "123456", ContainerID: "987654", WorkspaceID: "1", PageToken: "tag-cursor"})
	require.NoError(t, err)
	require.Equal(t, ListTagsBranchListed, tags.Branch)
	require.Equal(t, "gaawe", tags.Value.Tags[0].Type)
	for _, value := range []any{accounts.Value, containers.Value, tags.Value} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private")
		require.NotContains(t, string(encoded), testAccessToken)
	}
}

func TestInvalidInputsAndRetries(t *testing.T) {
	var status atomic.Int32
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(int(status.Load())) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	containers, err := sdkgo.RunQuery(queryContext{}, client.ListContainers(), testConnection, ListContainersInput{AccountID: "../123"})
	require.NoError(t, err)
	require.Equal(t, ListContainersBranchDefect, containers.Branch)
	tags, err := sdkgo.RunQuery(queryContext{}, client.ListTags(), testConnection, ListTagsInput{AccountID: "123456", ContainerID: "987654", WorkspaceID: "1", PageToken: "bad\nheader"})
	require.NoError(t, err)
	require.Equal(t, ListTagsBranchDefect, tags.Branch)
	require.Zero(t, calls.Load())
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunQuery(queryContext{}, client.ListAccounts(), testConnection, ListAccountsInput{})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
}
