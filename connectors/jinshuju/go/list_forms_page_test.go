// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jinshuju

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

var testConnection = sdkgo.ConnectionRef{Provider: "jinshuju", Name: "jinshuju"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "jinshuju-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-forms-page" }
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
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("access-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestListFormsPageRequestAndSafeResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://jinshuju.net/api/v1/forms?next=cursor%2B2%3D", request.URL.String())
		require.Equal(t, "Bearer access-secret", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		return reply(200, `{"total":828,"count":1,"data":[{"name":"需求调研表","token":"a1B2C3","creator":{"name":"private-person"},"tags":["private-tag"]}],"next":"cursor-3"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListFormsPage(), testConnection, ListFormsPageInput{Next: "cursor+2="})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListFormsPageBranchListed, result.Branch)
	require.Equal(t, ListFormsPageOutput{Total: 828, Count: 1, Forms: []FormMetadata{{Name: "需求调研表", Token: "a1B2C3"}}, Next: "cursor-3"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-person")
	require.NotContains(t, string(encoded), "private-tag")
}
func TestListFormsPageClassifiesOutcomes(t *testing.T) {
	scenarios := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"total":0,"count":0,"data":[],"next":null}`, ListFormsPageBranchListed, false},
		{"unauthorized", 401, `{"error":"access-secret"}`, ListFormsPageBranchProviderRejected, false},
		{"billing", 402, `{}`, ListFormsPageBranchProviderRejected, false},
		{"not found", 404, `{}`, ListFormsPageBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing data", 200, `{"total":0,"count":0}`, ListFormsPageBranchInvalidResponse, false},
		{"wrong count", 200, `{"total":2,"count":2,"data":[{"name":"表","token":"a1"}]}`, ListFormsPageBranchInvalidResponse, false},
		{"missing token", 200, `{"total":1,"count":1,"data":[{"name":"表"}]}`, ListFormsPageBranchInvalidResponse, false},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListFormsPage(), testConnection, ListFormsPageInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "access-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "access-secret")
			}
		})
	}
}
func TestListFormsPageRejectsInvalidCursorAndBoundsResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, strings.Repeat("x", 100)), nil })
	client := newTestClient(t, transport, 16)
	for _, cursor := range []string{" bad", "bad\nheader", strings.Repeat("x", 257)} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListFormsPage(), testConnection, ListFormsPageInput{Next: cursor})
		require.NoError(t, err)
		require.Equal(t, ListFormsPageBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListFormsPage(), testConnection, ListFormsPageInput{})
	require.NoError(t, err)
	require.Equal(t, ListFormsPageBranchInvalidResponse, result.Branch)
}
func TestListFormsPageTransportAndRedirectAreSecretSafe(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("access-secret") })
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListFormsPage(), testConnection, ListFormsPageInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "access-secret")
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListFormsPage(), testConnection, ListFormsPageInput{})
	require.NoError(t, err)
	require.Equal(t, ListFormsPageBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}
