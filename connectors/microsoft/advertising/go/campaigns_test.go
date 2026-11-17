// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package microsoftadvertising

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestGetCampaignByID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, campaignsPath+"/QueryByIds", request.URL.Path)
		var body map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, []any{"98765"}, body["CampaignIds"])
		_, err := writer.Write([]byte(`{"Campaigns":[{"Id":"98765","Name":"Launch","CampaignType":"Search","Status":"Paused"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).GetCampaign(), testConnection, GetCampaignInput{AccountID: "12345", CampaignID: "98765"})
	require.NoError(t, err)
	require.Equal(t, GetCampaignBranchFound, result.Branch)
	require.Equal(t, "Launch", result.Value.Name)
}

func TestCampaignSafeWrites(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		method string
		status int
		branch sdkgo.BranchID
	}{
		{"update", http.MethodPut, http.StatusOK, UpdateCampaignBranchUpdated},
		{"delete", http.MethodDelete, http.StatusNoContent, DeleteCampaignBranchDeleted},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, testCase.method, request.Method)
				require.Equal(t, campaignsPath, request.URL.Path)
				writer.WriteHeader(testCase.status)
				if testCase.status == http.StatusOK {
					_, err := writer.Write([]byte(`{"PartialErrors":[]}`))
					require.NoError(t, err)
				}
			}))
			defer server.Close()
			client := testClient(t, server.URL)
			if testCase.name == "update" {
				result, err := sdkgo.RunMutation(queryContext{}, client.UpdateCampaign(), testConnection, UpdateCampaignInput{AccountID: "12345", CampaignID: "98765", Status: "Paused"})
				require.NoError(t, err)
				require.Equal(t, testCase.branch, result.Branch)
			} else {
				result, err := sdkgo.RunMutation(queryContext{}, client.DeleteCampaign(), testConnection, DeleteCampaignInput{AccountID: "12345", CampaignID: "98765"})
				require.NoError(t, err)
				require.Equal(t, testCase.branch, result.Branch)
			}
		})
	}
}

func TestCreateCampaignChecksNameBeforeResend(t *testing.T) {
	creates := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == listCampaignsPath {
			_, err := writer.Write([]byte(`{"Campaigns":[{"Id":"98765","Name":"Launch","CampaignType":"Search","Status":"Paused"}]}`))
			require.NoError(t, err)
			return
		}
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, campaignsPath, request.URL.Path)
		creates++
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	callContext := singledispatchtest.NewContext("advertising-create", "create-campaign")
	input := CreateCampaignInput{AccountID: "12345", Name: "Launch", DailyBudget: 10, TimeZone: "PacificTimeUSCanadaTijuana"}
	_, err := sdkgo.RunMutation(callContext, client.CreateCampaign(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	callContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(callContext, client.CreateCampaign(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateCampaignBranchCreated, result.Branch)
	require.Equal(t, "98765", result.Value.CampaignID)
	require.Equal(t, 1, creates)
}
