// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package douyin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "douyin", Name: "douyin"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "douyin-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-behavior-summary" }
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
func newTestClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()
	credentials := Credentials{ClientKey: "tt-test-app", ClientSecret: sdkgo.NewSecretString("test-client-secret")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func reportDate() string { return time.Now().In(chinaTime).AddDate(0, 0, -2).Format("2006-01-02") }
func authResponse() string {
	return `{"data":{"access_token":"test-token","error_code":0,"expires_in":7200},"message":"success"}`
}
func reportResponse() string {
	return `{"err_no":0,"err_msg":"secret-raw","data":{"sum":{"active_user_num":113,"new_user_num":74,"total_user_num":285227,"open_time":190,"share_time":8},"behaviors":[{"private":"private-cover"}]}}`
}

func TestGetBehaviorSummaryAuthCacheAndFilteredResponse(t *testing.T) {
	authCalls, reportCalls := 0, 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "open.douyin.com", request.URL.Host)
		switch request.URL.Path {
		case "/oauth/client_token/":
			authCalls++
			require.Equal(t, http.MethodPost, request.Method)
			var payload map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
			require.Equal(t, "tt-test-app", payload["client_key"])
			require.Equal(t, "test-client-secret", payload["client_secret"])
			require.Equal(t, "client_credential", payload["grant_type"])
			return reply(200, authResponse()), nil
		case "/api/platform/v2/data_analysis/query_behavior_data/":
			reportCalls++
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "test-token", request.Header.Get("access-token"))
			start, err := time.ParseInLocation("2006-01-02", reportDate(), chinaTime)
			require.NoError(t, err)
			require.Equal(t, strconv.FormatInt(start.Unix(), 10), request.URL.Query().Get("start_time"))
			require.Equal(t, strconv.FormatInt(start.Add(24*time.Hour-time.Second).Unix(), 10), request.URL.Query().Get("end_time"))
			return reply(200, reportResponse()), nil
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
			return nil, nil
		}
	})
	client := newTestClient(t, transport)
	for range 2 {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetBehaviorSummary(), testConnection, GetBehaviorSummaryInput{Date: reportDate()})
		require.NoError(t, err)
		require.Equal(t, GetBehaviorSummaryBranchFound, result.Branch)
		require.Equal(t, GetBehaviorSummaryOutput{ActiveUserCount: 113, NewUserCount: 74, TotalUserCount: 285227, OpenCount: 190, ShareCount: 8}, result.Value)
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private")
		require.NotContains(t, string(encoded), "test-token")
		require.NotContains(t, string(encoded), "test-client-secret")
	}
	require.Equal(t, 1, authCalls)
	require.Equal(t, 2, reportCalls)
}
func TestGetBehaviorSummaryClassifiesFailuresWithoutSecrets(t *testing.T) {
	cases := []struct {
		name         string
		authStatus   int
		authBody     string
		reportStatus int
		reportBody   string
		branch       sdkgo.BranchID
		retry        bool
	}{
		{name: "auth rejected", authBody: `{"data":{"error_code":10013,"description":"test-client-secret"}}`, branch: GetBehaviorSummaryBranchProviderRejected},
		{name: "auth malformed", authBody: `{"data":{"error_code":0,"access_token":"test-token","expires_in":0}}`, branch: GetBehaviorSummaryBranchInvalidResponse},
		{name: "auth rate limit", authStatus: 429, retry: true},
		{name: "report rejected", authBody: authResponse(), reportBody: `{"err_no":11009,"err_msg":"test-token"}`, branch: GetBehaviorSummaryBranchProviderRejected},
		{name: "token expired", authBody: authResponse(), reportBody: `{"err_no":28001008,"err_msg":"test-token"}`, retry: true},
		{name: "report malformed", authBody: authResponse(), reportBody: `{"err_no":0,"data":{}}`, branch: GetBehaviorSummaryBranchInvalidResponse},
		{name: "report unavailable", authBody: authResponse(), reportStatus: 503, retry: true},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/oauth/client_token/" {
					status := scenario.authStatus
					if status == 0 {
						status = 200
					}
					return reply(status, scenario.authBody), nil
				}
				status := scenario.reportStatus
				if status == 0 {
					status = 200
				}
				return reply(status, scenario.reportBody), nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).GetBehaviorSummary(), testConnection, GetBehaviorSummaryInput{Date: reportDate()})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-client-secret")
				require.NotContains(t, retry.Error(), "test-token")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "test-client-secret")
			require.NotContains(t, result.Failure.Message, "test-token")
		})
	}
}
func TestGetBehaviorSummaryRejectsCurrentDateAndRedirect(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).GetBehaviorSummary(), testConnection, GetBehaviorSummaryInput{Date: time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")})
	require.NoError(t, err)
	require.Equal(t, GetBehaviorSummaryBranchDefect, result.Branch)
	require.Zero(t, calls)
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).GetBehaviorSummary(), testConnection, GetBehaviorSummaryInput{Date: reportDate()})
	require.NoError(t, err)
	require.Equal(t, GetBehaviorSummaryBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-client-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).GetBehaviorSummary(), testConnection, GetBehaviorSummaryInput{Date: reportDate()})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-client-secret")
}

func TestLatestAvailableBehaviorDateUsesChinaTenAMCutoff(t *testing.T) {
	before := time.Date(2026, 11, 13, 9, 59, 0, 0, chinaTime)
	after := time.Date(2026, 11, 13, 10, 0, 0, 0, chinaTime)
	require.Equal(t, "2026-11-11", latestAvailableBehaviorDate(before).Format("2006-01-02"))
	require.Equal(t, "2026-11-12", latestAvailableBehaviorDate(after).Format("2006-01-02"))
}
