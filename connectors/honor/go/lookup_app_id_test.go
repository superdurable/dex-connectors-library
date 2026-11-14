// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package honor

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

var testConnection = sdkgo.ConnectionRef{Provider: "honor", Name: "honor"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "honor-provider-test" }
func (queryContext) StepExecutionID() string     { return "lookup-app-id" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(token any) any         { return context.Background().Value(token) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{ClientID: sdkgo.NewSecretString("test-client"), ClientSecret: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestLookupAppIDRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, tokenEndpoint, request.URL.String())
			require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
			require.NoError(t, request.ParseForm())
			require.Equal(t, "client_credentials", request.PostForm.Get("grant_type"))
			require.Equal(t, "test-client", request.PostForm.Get("client_id"))
			require.Equal(t, "test-secret", request.PostForm.Get("client_secret"))
			return reply(200, `{"access_token":"test-token","expires_in":3600,"token_type":"Bearer"}`), nil
		case 2:
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, lookupEndpoint+"?pkgName=com.example.app", request.URL.String())
			require.Equal(t, "Bearer test-token", request.Header.Get("Authorization"))
			return reply(200, `{"code":0,"msg":"ok","data":[{"appId":123456,"packageName":"com.example.app","private":"test-secret"}]}`), nil
		default:
			t.Fatal("unexpected request")
			return nil, nil
		}
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupAppID(), testConnection, LookupAppIDInput{PackageName: "com.example.app"})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, LookupAppIDBranchFound, result.Branch)
	require.Equal(t, LookupAppIDOutput{PackageName: "com.example.app", AppID: 123456}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "test-secret")
	require.NotContains(t, string(encoded), "test-token")
}
func TestLookupAppIDOutcomes(t *testing.T) {
	cases := []struct {
		name        string
		tokenStatus int
		tokenBody   string
		appStatus   int
		appBody     string
		branch      sdkgo.BranchID
		retry       bool
		requests    int
	}{
		{"not found", 200, `{"access_token":"test-token","expires_in":3600,"token_type":"Bearer"}`, 200, `{"code":0,"data":[]}`, LookupAppIDBranchNotFound, false, 2},
		{"bad credentials", 401, `{"message":"test-secret"}`, 0, "", LookupAppIDBranchProviderRejected, false, 1},
		{"rate limit", 429, `{"message":"test-secret"}`, 0, "", "", true, 1},
		{"bad token", 200, `{"access_token":"test-secret"}`, 0, "", LookupAppIDBranchInvalidResponse, false, 1},
		{"app forbidden", 200, `{"access_token":"test-token","expires_in":3600,"token_type":"Bearer"}`, 403, `{"message":"test-secret"}`, LookupAppIDBranchProviderRejected, false, 2},
		{"app code", 200, `{"access_token":"test-token","expires_in":3600,"token_type":"Bearer"}`, 200, `{"code":10004,"msg":"test-secret"}`, LookupAppIDBranchProviderRejected, false, 2},
		{"mismatch", 200, `{"access_token":"test-token","expires_in":3600,"token_type":"Bearer"}`, 200, `{"code":0,"data":[{"appId":1,"packageName":"com.other.app"}]}`, LookupAppIDBranchInvalidResponse, false, 2},
		{"server", 200, `{"access_token":"test-token","expires_in":3600,"token_type":"Bearer"}`, 503, `{}`, "", true, 2},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return reply(scenario.tokenStatus, scenario.tokenBody), nil
				}
				return reply(scenario.appStatus, scenario.appBody), nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupAppID(), testConnection, LookupAppIDInput{PackageName: "com.example.app"})
			require.Equal(t, scenario.requests, calls)
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
func TestLookupAppIDValidationAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("test-secret") })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupAppID(), testConnection, LookupAppIDInput{PackageName: "bad name"})
	require.NoError(t, err)
	require.Equal(t, LookupAppIDBranchDefect, result.Branch)
	require.Zero(t, calls)
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupAppID(), testConnection, LookupAppIDInput{PackageName: "com.example.app"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
