// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taosdata

import (
	"context"
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

var testConnection = sdkgo.ConnectionRef{Provider: "taosdata", Name: "taosdata"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "taosdata-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-databases" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, cloudURL string, limit int64) *Client {
	t.Helper()
	credentials := Credentials{CloudURL: sdkgo.NewSecretString(cloudURL), CloudToken: sdkgo.NewSecretString("secret-cloud-token")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}

const databasesJSON = `{"code":0,"column_meta":[["name","VARCHAR",64],["ntables","BIGINT",8],["status","VARCHAR",10]],"data":[["information_schema",16,"ready"],["performance_schema",9,"ready"]],"rows":2}`

func TestListDatabasesRequestAndResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "https://gw.us-central-1.gcp.cloud.tdengine.com/rest/sql?token=secret-cloud-token", request.URL.String())
		require.Empty(t, request.Header.Get("Authorization"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.Equal(t, databaseMetadataSQL, string(body))
		return reply(200, databasesJSON), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "https://gw.us-central-1.gcp.cloud.tdengine.com", 0).ListDatabases(), testConnection, ListDatabasesInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListDatabasesBranchListed, result.Branch)
	require.Equal(t, []DatabaseMetadata{{Name: "information_schema", TableCount: 16, Status: "ready"}, {Name: "performance_schema", TableCount: 9, Status: "ready"}}, result.Value.Databases)
}
func TestListDatabasesClassifiesProviderOutcomes(t *testing.T) {
	scenarios := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"bad query", 400, `{}`, ListDatabasesBranchProviderRejected, false},
		{"unauthorized", 401, `{}`, ListDatabasesBranchProviderRejected, false},
		{"forbidden", 403, `{}`, ListDatabasesBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"provider code", 200, `{"code":42,"desc":"secret-cloud-token"}`, ListDatabasesBranchProviderRejected, false},
		{"malformed", 200, `{}`, ListDatabasesBranchInvalidResponse, false},
		{"wrong columns", 200, `{"code":0,"column_meta":[["evil"]],"data":[],"rows":0}`, ListDatabasesBranchInvalidResponse, false},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "https://gw.us-central-1.gcp.cloud.tdengine.com", 0).ListDatabases(), testConnection, ListDatabasesInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "secret-cloud-token")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "secret-cloud-token")
			}
		})
	}
}
func TestListDatabasesRejectsUnsafeURL(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, databasesJSON), nil })
	for _, cloudURL := range []string{"http://gw.us-central-1.gcp.cloud.tdengine.com", "https://gw.us-central-1.gcp.cloud.tdengine.com.evil", "https://gw.us-central-1.gcp.cloud.tdengine.com:443", "https://gw.us-central-1.gcp.cloud.tdengine.com/path", "https://gw.us-central-1.gcp.cloud.tdengine.com/?token=leak", "https://user@gw.us-central-1.gcp.cloud.tdengine.com"} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, cloudURL, 0).ListDatabases(), testConnection, ListDatabasesInput{})
		require.NoError(t, err)
		require.Equal(t, ListDatabasesBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
}
func TestListDatabasesBoundsRedirectAndTransport(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "https://gw.us-central-1.gcp.cloud.tdengine.com", 16).ListDatabases(), testConnection, ListDatabasesInput{})
	require.NoError(t, err)
	require.Equal(t, ListDatabasesBranchInvalidResponse, result.Branch)
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "https://gw.us-central-1.gcp.cloud.tdengine.com", 0).ListDatabases(), testConnection, ListDatabasesInput{})
	require.NoError(t, err)
	require.Equal(t, ListDatabasesBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-cloud-token in transport") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "https://gw.us-central-1.gcp.cloud.tdengine.com", 0).ListDatabases(), testConnection, ListDatabasesInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "secret-cloud-token")
}
