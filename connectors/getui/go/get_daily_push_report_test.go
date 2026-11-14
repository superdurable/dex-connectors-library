// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package getui

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

var testConnection = sdkgo.ConnectionRef{Provider: "getui", Name: "getui"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "getui-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-daily-push-report" }
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
	credentials := Credentials{AppID: "test-app", AppKey: sdkgo.NewSecretString("test-appkey"), MasterSecret: sdkgo.NewSecretString("test-mastersecret")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func authResponse() string {
	return `{"code":0,"data":{"token":"test-token","expire_time":"` + strconv.FormatInt(time.Now().Add(24*time.Hour).UnixMilli(), 10) + `"}}`
}
func reportResponse() string {
	return `{"code":0,"msg":"secret-raw","data":{"2021-01-01":{"total":{"request_num":10,"msg_num":4,"target_num":4,"receive_num":4,"display_num":2,"click_num":2},"hw":{"device":"private"}}}}`
}

func TestAuthSignatureFixedVector(t *testing.T) {
	require.Equal(t, "b4048b7b7e89b93a02e1259ab0b788a3f0641748a671e43137bf99f540a28b87", authSign("appkey", "1700000000000", "mastersecret"))
}
func TestGetDailyPushReportAuthCacheAndFilteredResponse(t *testing.T) {
	authCalls, reportCalls := 0, 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "restapi.getui.com", request.URL.Host)
		switch request.URL.Path {
		case "/v2/test-app/auth":
			authCalls++
			require.Equal(t, http.MethodPost, request.Method)
			var payload map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
			require.Equal(t, "test-appkey", payload["appkey"])
			require.Len(t, payload["timestamp"], 13)
			require.Equal(t, authSign("test-appkey", payload["timestamp"], "test-mastersecret"), payload["sign"])
			return reply(200, authResponse()), nil
		case "/v2/test-app/report/push/date/2021-01-01":
			reportCalls++
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "test-token", request.Header.Get("token"))
			return reply(200, reportResponse()), nil
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
			return nil, nil
		}
	})
	client := newTestClient(t, transport)
	for range 2 {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetDailyPushReport(), testConnection, GetDailyPushReportInput{Date: "2021-01-01"})
		require.NoError(t, err)
		require.Equal(t, GetDailyPushReportBranchFound, result.Branch)
		require.Equal(t, GetDailyPushReportOutput{Date: "2021-01-01", RequestCount: 10, EligibleMessageCount: 4, SentCount: 4, ReceivedCount: 4, DisplayedCount: 2, ClickedCount: 2}, result.Value)
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private")
		require.NotContains(t, string(encoded), "test-token")
		require.NotContains(t, string(encoded), "test-mastersecret")
	}
	require.Equal(t, 1, authCalls)
	require.Equal(t, 2, reportCalls)
}
func TestGetDailyPushReportClassifiesFailuresWithoutSecrets(t *testing.T) {
	cases := []struct {
		name         string
		authStatus   int
		authBody     string
		reportStatus int
		reportBody   string
		branch       sdkgo.BranchID
		retry        bool
	}{
		{name: "auth rejected", authBody: `{"code":10002,"msg":"test-mastersecret"}`, branch: GetDailyPushReportBranchProviderRejected},
		{name: "auth malformed", authBody: `{"code":0,"data":{"token":"test-token","expire_time":"bad"}}`, branch: GetDailyPushReportBranchInvalidResponse},
		{name: "auth rate limit", authStatus: 429, retry: true},
		{name: "report rejected", authBody: authResponse(), reportBody: `{"code":10003,"msg":"test-token"}`, branch: GetDailyPushReportBranchProviderRejected},
		{name: "token expired", authBody: authResponse(), reportBody: `{"code":10001,"msg":"test-token"}`, retry: true},
		{name: "report malformed", authBody: authResponse(), reportBody: `{"code":0,"data":{}}`, branch: GetDailyPushReportBranchInvalidResponse},
		{name: "report unavailable", authBody: authResponse(), reportStatus: 503, retry: true},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if strings.HasSuffix(request.URL.Path, "/auth") {
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).GetDailyPushReport(), testConnection, GetDailyPushReportInput{Date: "2021-01-01"})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-mastersecret")
				require.NotContains(t, retry.Error(), "test-token")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "test-mastersecret")
			require.NotContains(t, result.Failure.Message, "test-token")
		})
	}
}
func TestGetDailyPushReportRejectsCurrentDateAndRedirect(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).GetDailyPushReport(), testConnection, GetDailyPushReportInput{Date: time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")})
	require.NoError(t, err)
	require.Equal(t, GetDailyPushReportBranchDefect, result.Branch)
	require.Zero(t, calls)
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).GetDailyPushReport(), testConnection, GetDailyPushReportInput{Date: "2021-01-01"})
	require.NoError(t, err)
	require.Equal(t, GetDailyPushReportBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-mastersecret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).GetDailyPushReport(), testConnection, GetDailyPushReportInput{Date: "2021-01-01"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-mastersecret")
}

func TestGetDailyPushReportRejectsUnsafeAppIDWithoutRequest(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, authResponse()), nil })
	credentials := Credentials{AppID: "../another-app", AppKey: sdkgo.NewSecretString("test-appkey"), MasterSecret: sdkgo.NewSecretString("test-mastersecret")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetDailyPushReport(), testConnection, GetDailyPushReportInput{Date: "2021-01-01"})
	require.NoError(t, err)
	require.Equal(t, GetDailyPushReportBranchDefect, result.Branch)
	require.Zero(t, calls)
}
