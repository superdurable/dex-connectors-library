// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package microsoftadvertising

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestCreateAdGroupDoesNotClaimExistingNameAfterLostResponse(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, adGroupsPath, request.URL.Path)
		requestCount++
		if requestCount == 1 {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, err := writer.Write([]byte(`{"AdGroupIds":["34"]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	callContext := singledispatchtest.NewContext("advertising-create", "create-ad-group")
	input := CreateAdGroupInput{AccountID: "12345", CampaignID: "98765", Name: "Existing name"}
	_, err := sdkgo.RunMutation(callContext, client.CreateAdGroup(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	callContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(callContext, client.CreateAdGroup(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateAdGroupBranchCreated, result.Branch)
	require.Equal(t, "34", result.Value.AdGroupID)
	require.Equal(t, 2, requestCount)
}

func TestCreateKeywordDoesNotClaimExistingTextAfterLostResponse(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, keywordsPath, request.URL.Path)
		requestCount++
		if requestCount == 1 {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, err := writer.Write([]byte(`{"KeywordIds":["45"]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	callContext := singledispatchtest.NewContext("advertising-create", "create-keyword")
	input := CreateKeywordInput{AccountID: "12345", AdGroupID: "33", Text: "existing text", MatchType: "Exact"}
	_, err := sdkgo.RunMutation(callContext, client.CreateKeyword(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	callContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(callContext, client.CreateKeyword(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateKeywordBranchCreated, result.Branch)
	require.Equal(t, "45", result.Value.KeywordID)
	require.Equal(t, 2, requestCount)
}

func TestLimitedCreatesClassifyProviderRejectionAndIncompleteResponse(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		response  string
		wantRetry bool
	}{
		{name: "partial rejection", response: `{"PartialErrors":[{"Code":123}],"AdIds":[null],"KeywordIds":[null],"AdGroupIds":[null]}`},
		{name: "malformed response", response: `{`, wantRetry: true},
		{name: "missing ID", response: `{}`, wantRetry: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, err := writer.Write([]byte(testCase.response))
				require.NoError(t, err)
			}))
			defer server.Close()
			client := testClient(t, server.URL)
			check := func(branch sdkgo.BranchID, err error, rejected sdkgo.BranchID) {
				if testCase.wantRetry {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, err, &retry)
					return
				}
				require.NoError(t, err)
				require.Equal(t, rejected, branch)
			}
			ad, err := sdkgo.RunMutation(singledispatchtest.NewContext("advertising-create", "ad"), client.CreateResponsiveSearchAd(), testConnection, CreateResponsiveSearchAdInput{AccountID: "12345", AdGroupID: "33", FinalURL: "https://example.com", Headlines: []string{"One", "Two", "Three"}, Descriptions: []string{"First", "Second"}})
			check(ad.Branch, err, CreateResponsiveSearchAdBranchProviderRejected)
			keyword, err := sdkgo.RunMutation(singledispatchtest.NewContext("advertising-create", "keyword"), client.CreateKeyword(), testConnection, CreateKeywordInput{AccountID: "12345", AdGroupID: "33", Text: "shoes", MatchType: "Exact"})
			check(keyword.Branch, err, CreateKeywordBranchProviderRejected)
			group, err := sdkgo.RunMutation(singledispatchtest.NewContext("advertising-create", "group"), client.CreateAdGroup(), testConnection, CreateAdGroupInput{AccountID: "12345", CampaignID: "98765", Name: "New group"})
			check(group.Branch, err, CreateAdGroupBranchProviderRejected)
			campaign, err := sdkgo.RunMutation(singledispatchtest.NewContext("advertising-create", "campaign"), client.CreateCampaign(), testConnection, CreateCampaignInput{AccountID: "12345", Name: "New campaign", DailyBudget: 10, TimeZone: "PacificTimeUSCanadaTijuana"})
			check(campaign.Branch, err, CreateCampaignBranchProviderRejected)
		})
	}
}
