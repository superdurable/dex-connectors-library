// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tiktok

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "tiktok-business", Name: "tiktok-business"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "tiktok-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-campaigns" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("sample-tiktok-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestListCampaignsReadsSelectedFieldsAndPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/campaign/get/", request.URL.Path)
		require.Equal(t, "1845123456789012345", request.URL.Query().Get("advertiser_id"))
		require.Equal(t, "2", request.URL.Query().Get("page"))
		require.Equal(t, "2", request.URL.Query().Get("page_size"))
		require.Equal(t, "sample-tiktok-token", request.Header.Get("Access-Token"))
		_, err := writer.Write([]byte(`{"code":0,"message":"OK","data":{"list":[{"campaign_id":"1845123456789012346","campaign_name":"Example Campaign","budget":"private"}],"page_info":{"page":2,"total_page":3}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	input := ListCampaignsInput{AdvertiserID: "1845123456789012345", Page: 2, PageSize: 2}
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListCampaigns(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, ListCampaignsBranchListed, result.Branch)
	require.Equal(t, CampaignsPage{Campaigns: []CampaignMetadata{{ID: "1845123456789012346", Name: "Example Campaign"}}, Page: 2, TotalPages: 3}, result.Value)
}

func TestListCampaignsClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "forbidden", status: 403, branch: ListCampaignsBranchProviderRejected},
		{name: "business rejection", status: 200, body: `{"code":40100,"message":"denied"}`, branch: ListCampaignsBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "missing campaign name", status: 200, body: `{"code":0,"data":{"list":[{"campaign_id":"1845123456789012346"}],"page_info":{"page":1,"total_page":1}}}`, branch: ListCampaignsBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			input := ListCampaignsInput{AdvertiserID: "1845123456789012345"}
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListCampaigns(), testConnection, input)
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestListCampaignsRejectsInvalidInputAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://business-api.tiktok.com/open_api/v1.3", 0)
	for _, input := range []ListCampaignsInput{{AdvertiserID: "bad"}, {AdvertiserID: "1845123456789012345", PageSize: 1001}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListCampaigns(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListCampaignsBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":0,"data":{"list":[],"page_info":{"page":1,"total_page":0}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	input := ListCampaignsInput{AdvertiserID: "1845123456789012345"}
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).ListCampaigns(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, ListCampaignsBranchInvalidResponse, result.Branch)
}
