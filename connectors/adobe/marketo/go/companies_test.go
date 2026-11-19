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

func TestCompanyReadAndWritesUseExternalKey(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		client := testClient(t, func(request *http.Request) (*http.Response, error) {
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "/rest/v1/companies.json", request.URL.Path)
			require.Equal(t, "externalCompanyId", request.URL.Query().Get("filterType"))
			require.Equal(t, "source-123", request.URL.Query().Get("filterValues"))
			require.Equal(t, "id,company", request.URL.Query().Get("fields"))
			return response(200, `{"success":true,"result":[{"id":3433,"company":"Sample Company","externalCompanyId":"source-123","private":"secret"}]}`), nil
		})
		result, err := sdkgo.RunQuery(queryContext{}, client.GetCompanyByExternalID(), testConnection, GetCompanyByExternalIDInput{ExternalCompanyID: "source-123"})
		require.NoError(t, err)
		require.Equal(t, GetCompanyByExternalIDBranchFound, result.Branch)
		require.Equal(t, CompanyProfile{ID: 3433, Company: "Sample Company"}, result.Value)
	})
	for _, testCase := range []struct {
		name   string
		action string
		invoke func(*Client) (CompanyWriteResult, sdkgo.BranchID, error)
	}{
		{name: "create", action: "createOnly", invoke: func(client *Client) (CompanyWriteResult, sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(queryContext{}, client.CreateCompany(), testConnection, CreateCompanyInput{ExternalCompanyID: "source-123", Company: "Sample Company"})
			return result.Value, result.Branch, err
		}},
		{name: "upsert", action: "createOrUpdate", invoke: func(client *Client) (CompanyWriteResult, sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(queryContext{}, client.UpsertCompany(), testConnection, UpsertCompanyInput{ExternalCompanyID: "source-123", Company: "Sample Company"})
			return result.Value, result.Branch, err
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := testClient(t, func(request *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodPost, request.Method)
				require.Equal(t, "/rest/v1/companies.json", request.URL.Path)
				var body struct {
					Action   string `json:"action"`
					DedupeBy string `json:"dedupeBy"`
					Input    []struct {
						ExternalCompanyID string `json:"externalCompanyId"`
						Company           string `json:"company"`
					} `json:"input"`
				}
				require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				require.Equal(t, testCase.action, body.Action)
				require.Equal(t, "dedupeFields", body.DedupeBy)
				require.Len(t, body.Input, 1)
				require.Equal(t, "source-123", body.Input[0].ExternalCompanyID)
				return response(200, `{"success":true,"result":[{"seq":0,"id":3433,"status":"created"}]}`), nil
			})
			value, branch, err := testCase.invoke(client)
			require.NoError(t, err)
			require.Equal(t, int64(3433), value.ID)
			if testCase.name == "create" {
				require.Equal(t, CreateCompanyBranchCreated, branch)
			} else {
				require.Equal(t, UpsertCompanyBranchSynced, branch)
			}
		})
	}
	t.Run("delete missing", func(t *testing.T) {
		client := testClient(t, func(request *http.Request) (*http.Response, error) {
			require.Equal(t, "/rest/v1/companies/delete.json", request.URL.Path)
			return response(200, `{"success":true,"result":[{"seq":0,"status":"skipped","reasons":[{"code":"1013","message":"secret"}]}]}`), nil
		})
		result, err := sdkgo.RunMutation(queryContext{}, client.DeleteCompany(), testConnection, DeleteCompanyInput{ExternalCompanyID: "source-123"})
		require.NoError(t, err)
		require.Equal(t, DeleteCompanyBranchDeleted, result.Branch)
		require.True(t, result.Value.Deleted)
	})
}

func TestCompanyOperationsClassifyFailure(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 200, body: `{"success":true,"result":[]}`, branch: GetCompanyByExternalIDBranchNotFound},
		{name: "rejected", status: 200, body: `{"success":false,"errors":[{"code":"603","message":"secret"}]}`, branch: GetCompanyByExternalIDBranchProviderRejected},
		{name: "invalid", status: 200, body: `{`, branch: GetCompanyByExternalIDBranchInvalidResponse},
		{name: "429", status: 429, body: `{}`, retry: true},
		{name: "503", status: 503, body: `{}`, retry: true},
		{name: "500", status: 500, body: `{}`, retry: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := testClient(t, func(*http.Request) (*http.Response, error) { return response(testCase.status, testCase.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, client.GetCompanyByExternalID(), testConnection, GetCompanyByExternalIDInput{ExternalCompanyID: "source-123"})
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
	_, err := sdkgo.RunQuery(queryContext{}, client.GetCompanyByExternalID(), testConnection, GetCompanyByExternalIDInput{ExternalCompanyID: "source-123"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
