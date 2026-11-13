// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mingdao

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

var testConnection = sdkgo.ConnectionRef{Provider: "mingdao", Name: "mingdao"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "mingdao-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-worksheets" }
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
	credentials := Credentials{AppKey: sdkgo.NewSecretString("app-secret"), Sign: sdkgo.NewSecretString("sign-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestListWorksheetsRequestAndSafeResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, worksheetsEndpoint, request.URL.String())
		require.Equal(t, "app-secret", request.Header.Get("HAP-Appkey"))
		require.Equal(t, "sign-secret", request.Header.Get("HAP-Sign"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"responseFormat":"json"}`, string(body))
		return reply(200, `{"success":true,"error_code":1,"data":[{"id":"sheet-123","name":"客户表","remark":"private remark","privateRow":"private customer"}]}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListWorksheets(), testConnection, ListWorksheetsInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListWorksheetsBranchListed, result.Branch)
	require.Equal(t, []Worksheet{{ID: "sheet-123", Name: "客户表"}}, result.Value.Worksheets)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
}
func TestListWorksheetsClassifiesProviderOutcomes(t *testing.T) {
	scenarios := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"success":true,"data":[]}`, ListWorksheetsBranchListed, false},
		{"permission", 200, `{"success":false,"error_code":10005,"error_msg":"app-secret"}`, ListWorksheetsBranchProviderRejected, false},
		{"bad signature", 200, `{"success":false,"error_code":10102,"error_msg":"sign-secret"}`, ListWorksheetsBranchProviderRejected, false},
		{"rate code", 200, `{"success":false,"error_code":51}`, "", true},
		{"quota code", 200, `{"success":false,"error_code":90000}`, "", true},
		{"service code", 200, `{"success":false,"error_code":99999}`, "", true},
		{"invalid shape", 200, `{"success":true}`, ListWorksheetsBranchInvalidResponse, false},
		{"incomplete item", 200, `{"success":true,"data":[{"id":"sheet-123"}]}`, ListWorksheetsBranchInvalidResponse, false},
		{"http forbidden", 403, `{"message":"sign-secret"}`, ListWorksheetsBranchProviderRejected, false},
		{"http rate", 429, `{}`, "", true},
		{"http unavailable", 503, `{}`, "", true},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListWorksheets(), testConnection, ListWorksheetsInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "secret")
			}
		})
	}
}
func TestListWorksheetsBoundsResponseAndPreventsRedirect(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).ListWorksheets(), testConnection, ListWorksheetsInput{})
	require.NoError(t, err)
	require.Equal(t, ListWorksheetsBranchInvalidResponse, result.Branch)
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListWorksheets(), testConnection, ListWorksheetsInput{})
	require.NoError(t, err)
	require.Equal(t, ListWorksheetsBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("sign-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListWorksheets(), testConnection, ListWorksheetsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "sign-secret")
}
