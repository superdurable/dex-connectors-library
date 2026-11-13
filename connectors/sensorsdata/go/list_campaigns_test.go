// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sensorsdata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "sensorsdata", Name: "sensorsdata"}
var testAPIKey = strings.Repeat("k", 35)

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "sensorsdata-provider-test" }
func (queryContext) StepExecutionID() string     { return "campaign-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type redirectTransport struct{ destination *url.URL }

func (transport redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.URL.Scheme = transport.destination.Scheme
	copy.URL.Host = transport.destination.Host
	return http.DefaultTransport.RoundTrip(copy)
}
func newTestClient(t *testing.T, serverURL string, maxResponseBytes int64) *Client {
	t.Helper()
	destination, err := url.Parse(serverURL)
	require.NoError(t, err)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	client, err := New(Config{BaseURL: "https://family.demo.sensorsdata.cn", ProjectName: "production", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{destination: destination}}))
	require.NoError(t, err)
	return client
}
func TestListCampaignsRequestAndPrivacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, campaignsPath, request.URL.Path)
		require.Equal(t, testAPIKey, request.Header.Get("api-key"))
		require.Equal(t, "production", request.Header.Get("sensorsdata-project"))
		require.NotContains(t, request.URL.String(), testAPIKey)
		var body struct {
			PageNum  int `json:"page_num"`
			PageSize int `json:"page_size"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, 0, body.PageNum)
		require.Equal(t, 20, body.PageSize)
		_, err := writer.Write([]byte(`{"code":"SUCCESS","request_id":"private","data":{"campaign_details":[{"campaign_name":"测试活动","branch_num":1,"latest_use_time":"2025-01-01 12:00:00","email":"private"}],"total_rows":"6","total_page":1,"page_num":0,"page_size":20}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListCampaigns(), testConnection, ListCampaignsInput{})
	require.NoError(t, err)
	require.Equal(t, ListCampaignsBranchListed, result.Branch)
	require.Equal(t, ListCampaignsOutput{Campaigns: []Campaign{{Name: "测试活动", LinkCount: 1, LatestUseTime: "2025-01-01 12:00:00"}}, TotalRows: 6, TotalPages: 1, PageNum: 0, PageSize: 20}, result.Value)
}
func TestListCampaignsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"unauthorized", 401, `private`, ListCampaignsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, `private`, ListCampaignsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"business failure", 200, `{"code":"ERROR","message":"private"}`, ListCampaignsBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limited", 429, `private`, "", sdkgo.FailureRateLimit, true},
		{"unavailable", 503, `private`, "", sdkgo.FailureAvailability, true},
		{"malformed", 200, `{`, ListCampaignsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing totals", 200, `{"code":"SUCCESS","data":{"campaign_details":[],"page_num":0,"page_size":20}}`, ListCampaignsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"bad campaign", 200, `{"code":"SUCCESS","data":{"campaign_details":[{"campaign_name":"x","branch_num":-1}],"total_rows":1,"total_page":1,"page_num":0,"page_size":20}}`, ListCampaignsBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListCampaigns(), testConnection, ListCampaignsInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAPIKey)
			}
		})
	}
}
func TestListCampaignsValidationBoundAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(strings.Repeat("x", 100)))
		require.NoError(t, err)
	}))
	client := newTestClient(t, server.URL, 8)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListCampaigns(), testConnection, ListCampaignsInput{})
	require.NoError(t, err)
	require.Equal(t, ListCampaignsBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListCampaigns(), testConnection, ListCampaignsInput{PageSize: 1001})
	require.NoError(t, err)
	require.Equal(t, ListCampaignsBranchDefect, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, client.ListCampaigns(), testConnection, ListCampaignsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
func TestSensorsDataBaseURLValidation(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{}
	for _, origin := range []string{"http://family.demo.sensorsdata.cn", "https://family.demo.sensorsdata.cn/api"} {
		_, err := New(Config{BaseURL: origin, ProjectName: "production"}, credentials)
		require.Error(t, err, origin)
	}
}
