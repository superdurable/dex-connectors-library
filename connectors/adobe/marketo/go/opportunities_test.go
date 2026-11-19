// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestOpportunityReadAndWritesUseExternalKey(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		client := testClient(t, func(request *http.Request) (*http.Response, error) {
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "/rest/v1/opportunities.json", request.URL.Path)
			require.Equal(t, "externalOpportunityId", request.URL.Query().Get("filterType"))
			require.Equal(t, "source-123", request.URL.Query().Get("filterValues"))
			require.Equal(t, "marketoGUID,name", request.URL.Query().Get("fields"))
			return response(200, `{"success":true,"result":[{"marketoGUID":"dff23271-f996-47d7-984f-f2676861b5fa","name":"Sample Opportunity","externalOpportunityId":"source-123","private":"secret"}]}`), nil
		})
		result, err := sdkgo.RunQuery(queryContext{}, client.GetOpportunityByExternalID(), testConnection, GetOpportunityByExternalIDInput{ExternalOpportunityID: "source-123"})
		require.NoError(t, err)
		require.Equal(t, GetOpportunityByExternalIDBranchFound, result.Branch)
		require.Equal(t, OpportunityProfile{MarketoGUID: "dff23271-f996-47d7-984f-f2676861b5fa", Name: "Sample Opportunity"}, result.Value)
	})
	for _, testCase := range []struct {
		name   string
		action string
		invoke func(*Client) (OpportunityWriteResult, sdkgo.BranchID, error)
	}{
		{name: "create", action: "createOnly", invoke: func(client *Client) (OpportunityWriteResult, sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(queryContext{}, client.CreateOpportunity(), testConnection, CreateOpportunityInput{ExternalOpportunityID: "source-123", Name: "Sample Opportunity"})
			return result.Value, result.Branch, err
		}},
		{name: "upsert", action: "createOrUpdate", invoke: func(client *Client) (OpportunityWriteResult, sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(queryContext{}, client.UpsertOpportunity(), testConnection, UpsertOpportunityInput{ExternalOpportunityID: "source-123", Name: "Sample Opportunity"})
			return result.Value, result.Branch, err
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := testClient(t, func(request *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodPost, request.Method)
				require.Equal(t, "/rest/v1/opportunities.json", request.URL.Path)
				var body struct {
					Action   string `json:"action"`
					DedupeBy string `json:"dedupeBy"`
					Input    []struct {
						ExternalOpportunityID string `json:"externalOpportunityId"`
						Name                  string `json:"name"`
					} `json:"input"`
				}
				require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				require.Equal(t, testCase.action, body.Action)
				require.Equal(t, "dedupeFields", body.DedupeBy)
				require.Len(t, body.Input, 1)
				require.Equal(t, "source-123", body.Input[0].ExternalOpportunityID)
				require.Equal(t, "Sample Opportunity", body.Input[0].Name)
				return response(200, `{"success":true,"result":[{"seq":0,"marketoGUID":"dff23271-f996-47d7-984f-f2676861b5fa","status":"created"}]}`), nil
			})
			value, branch, err := testCase.invoke(client)
			require.NoError(t, err)
			require.Equal(t, "dff23271-f996-47d7-984f-f2676861b5fa", value.MarketoGUID)
			if testCase.name == "create" {
				require.Equal(t, CreateOpportunityBranchCreated, branch)
			} else {
				require.Equal(t, UpsertOpportunityBranchSynced, branch)
			}
		})
	}
	t.Run("delete missing", func(t *testing.T) {
		client := testClient(t, func(request *http.Request) (*http.Response, error) {
			require.Equal(t, "/rest/v1/opportunities/delete.json", request.URL.Path)
			return response(200, `{"success":true,"result":[{"seq":0,"status":"skipped","reasons":[{"code":"1013","message":"secret"}]}]}`), nil
		})
		result, err := sdkgo.RunMutation(queryContext{}, client.DeleteOpportunity(), testConnection, DeleteOpportunityInput{ExternalOpportunityID: "source-123"})
		require.NoError(t, err)
		require.Equal(t, DeleteOpportunityBranchDeleted, result.Branch)
		require.True(t, result.Value.Deleted)
	})
}

func TestOpportunityOperationsClassifyFailure(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 200, body: `{"success":true,"result":[]}`, branch: GetOpportunityByExternalIDBranchNotFound},
		{name: "rejected", status: 200, body: `{"success":false,"errors":[{"code":"603","message":"secret"}]}`, branch: GetOpportunityByExternalIDBranchProviderRejected},
		{name: "invalid", status: 200, body: `{`, branch: GetOpportunityByExternalIDBranchInvalidResponse},
		{name: "429", status: 429, body: `{}`, retry: true},
		{name: "503", status: 503, body: `{}`, retry: true},
		{name: "500", status: 500, body: `{}`, retry: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := testClient(t, func(*http.Request) (*http.Response, error) { return response(testCase.status, testCase.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, client.GetOpportunityByExternalID(), testConnection, GetOpportunityByExternalIDInput{ExternalOpportunityID: "source-123"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
	client := testClient(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("secret network error") })
	_, err := sdkgo.RunQuery(queryContext{}, client.GetOpportunityByExternalID(), testConnection, GetOpportunityByExternalIDInput{ExternalOpportunityID: "source-123"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
