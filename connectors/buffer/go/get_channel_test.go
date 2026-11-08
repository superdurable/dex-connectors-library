// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package buffer

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "buffer", Name: "buffer"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "buffer-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-channel" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{APIKey: sdkgo.NewSecretString("sample-api-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}
func TestGetChannelReadsChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/", request.URL.Path)
		require.Equal(t, "Bearer sample-api-key", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var fields struct {
			Query     string `json:"query"`
			Variables struct {
				ID string `json:"id"`
			} `json:"variables"`
		}
		require.NoError(t, json.Unmarshal(body, &fields))
		require.Equal(t, getChannelGraphQL, fields.Query)
		require.Equal(t, "channel-1", fields.Variables.ID)
		_, err = io.WriteString(writer, `{"data":{"channel":{"id":"channel-1","name":"Example Channel","service":"instagram"}}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetChannel(), testConnection, GetChannelInput{ChannelID: "channel-1"})
	require.NoError(t, err)
	require.Equal(t, GetChannelBranchFound, result.Branch)
	require.Equal(t, ChannelProfile{ID: "channel-1", Name: "Example Channel", Service: "instagram"}, result.Value)
}
func TestGetChannelClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"not found", 200, `{"data":{"channel":null}}`, GetChannelBranchNotFound, false},
		{"missing channel name", 200, `{"data":{"channel":{"id":"channel-1","service":"instagram"}}}`, GetChannelBranchInvalidResponse, false},
		{"GraphQL error", 200, `{"errors":[{"message":"denied"}],"data":null}`, GetChannelBranchProviderRejected, false},
		{"forbidden", 403, "", GetChannelBranchProviderRejected, false},
		{"rate limit", 429, "", "", true},
		{"unavailable", 503, "", "", true},
		{"malformed", 200, `{"data":{"channel":{"id":"wrong","name":"Example Channel","service":"instagram"}}}`, GetChannelBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetChannel(), testConnection, GetChannelInput{ChannelID: "channel-1"})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestGetChannelRejectsInputAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.buffer.com", 0)
	for _, input := range []GetChannelInput{{}, {ChannelID: " channel-1"}, {ChannelID: "bad\nID"}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetChannel(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, GetChannelBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"data":{"channel":{"id":"channel-1","name":"Example Channel","service":"instagram"}}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).GetChannel(), testConnection, GetChannelInput{ChannelID: "channel-1"})
	require.NoError(t, err)
	require.Equal(t, GetChannelBranchInvalidResponse, result.Branch)
}
