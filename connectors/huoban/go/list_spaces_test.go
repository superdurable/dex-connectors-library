// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package huoban

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "huoban", Name: "huoban"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "huoban-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-spaces" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestListSpacesRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://api.huoban.com/openapi/v1/space/list", request.URL.String())
		require.Equal(t, "Bearer test-secret", request.Header.Get("Open-Authorization"))
		return reply(200, `{"code":0,"data":{"spaces":[{"space_id":"4000000000000001","name":"Operations","cover_image":{"source":"private-cover"}}]},"meta":{"trace_id":"private-trace"}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListSpaces(), testConnection, ListSpacesInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListSpacesBranchListed, result.Branch)
	require.Equal(t, []SpaceMetadata{{SpaceID: "4000000000000001", Name: "Operations"}}, result.Value.Spaces)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-cover")
	require.NotContains(t, string(encoded), "private-trace")
}
func TestListSpacesClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"business rejection", 200, `{"code":104,"message":"test-secret"}`, ListSpacesBranchProviderRejected, false},
		{"unauthorized", 401, `{"message":"test-secret"}`, ListSpacesBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing code", 200, `{"data":{"spaces":[]}}`, ListSpacesBranchInvalidResponse, false},
		{"missing spaces", 200, `{"code":0,"data":{}}`, ListSpacesBranchInvalidResponse, false},
		{"invalid space", 200, `{"code":0,"data":{"spaces":[{"space_id":"","name":"x"}]}}`, ListSpacesBranchInvalidResponse, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListSpaces(), testConnection, ListSpacesInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "test-secret")
			}
		})
	}
}
func TestListSpacesBoundTransportAndRedirect(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).ListSpaces(), testConnection, ListSpacesInput{})
	require.NoError(t, err)
	require.Equal(t, ListSpacesBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListSpaces(), testConnection, ListSpacesInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListSpaces(), testConnection, ListSpacesInput{})
	require.NoError(t, err)
	require.Equal(t, ListSpacesBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}
