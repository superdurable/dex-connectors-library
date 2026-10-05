// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func contactByEmailInput() crm.FindRecordsInput {
	return crm.FindRecordsInput{
		Module: crm.ModuleContacts, Fields: []string{"Email", "Account_Name", "Owner", "Modified_Time"},
		Conditions: []crm.RecordCondition{condition("Email", crm.ConditionEquals, crm.COQLText("jane@acme.example.com"))},
		Limit:      2,
	}
}

func TestFindRecordsPostsTheQueryAndReturnsRecordsWithoutDollarKeys(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{
			"data": []any{contactJSON(testContactID, "jane@acme.example.com", "2026-01-26T14:00:00+05:30"),
				contactJSON("4150868000000376011", "jane@acme.example.com", "2026-01-27T09:00:00+05:30")},
			"info": map[string]any{"count": 2, "more_records": true},
		})
	})
	result, err := sdkgo.RunQuery(newCRMDexContext("find"), newCRMClient(t, provider.URL).FindRecords(), crmConnection, contactByEmailInput())
	require.NoError(t, err)
	require.Equal(t, crm.FindRecordsBranchFound, result.Branch)
	require.Equal(t, crm.ModuleContacts, result.Value.Module)
	require.Len(t, result.Value.Records, 2)
	record := result.Value.Records[0]
	require.Equal(t, testContactID, record.ID)
	email, isFound := record.StringField("Email")
	require.True(t, isFound)
	require.Equal(t, "jane@acme.example.com", email)
	accountID, isFound := record.LookupID("Account_Name")
	require.True(t, isFound)
	require.Equal(t, testAccountID, accountID)
	modifiedAt, isFound := record.ModifiedAt()
	require.True(t, isFound)
	require.True(t, modifiedAt.Equal(time.Date(2026, 1, 26, 8, 30, 0, 0, time.UTC)))
	for name := range record.Fields {
		require.False(t, strings.HasPrefix(name, "$"), "Zoho's $ keys are dropped")
	}
	require.NotContains(t, record.Fields, "id")
	require.Equal(t, 2, result.Value.NextOffset)
	require.Equal(t, "48211", result.Receipt.Metadata["remainingCredits"])

	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/crm/v8/coql", request.path)
	require.Equal(t, "Zoho-oauthtoken "+testAccessToken, request.header.Get("Authorization"))
	require.Equal(t, "application/json", request.header.Get("Content-Type"))
	require.Equal(t, "select Email, Account_Name, Owner, Modified_Time from Contacts where Email = 'jane@acme.example.com' order by id asc limit 0, 2",
		provider.selectQuery(t, 0))
}

func TestFindRecordsSelectsNotFoundForNoContentAndForAnEmptyPage(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter){
		"204": func(response http.ResponseWriter) { response.WriteHeader(http.StatusNoContent) },
		"empty data": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"data":[],"info":{"count":0,"more_records":false}}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) { reply(response) })
			result, err := sdkgo.RunQuery(newCRMDexContext("find"), newCRMClient(t, provider.URL).FindRecords(), crmConnection, contactByEmailInput())
			require.NoError(t, err)
			require.Equal(t, crm.FindRecordsBranchNotFound, result.Branch)
			require.Empty(t, result.Value.Records)
			require.Zero(t, result.Value.NextOffset)
		})
	}
}

func TestFindRecordsMapsZohoErrorsWithoutMessageText(t *testing.T) {
	for name, scenario := range map[string]struct {
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		text   string
	}{
		"invalid query":  {http.StatusBadRequest, `{"code":"INVALID_QUERY","details":{"column_name":"Testing"},"message":"SENTINEL invalid column","status":"error"}`, crm.FindRecordsBranchProviderRejected, sdkgo.FailureValidation, "INVALID_QUERY"},
		"scope mismatch": {http.StatusUnauthorized, `{"code":"OAUTH_SCOPE_MISMATCH","details":{},"message":"SENTINEL","status":"error"}`, crm.FindRecordsBranchProviderRejected, sdkgo.FailureAuthorization, "lacks ZohoCRM.coql.READ"},
		"no permission":  {http.StatusForbidden, `{"code":"NO_PERMISSION","details":{},"message":"SENTINEL","status":"error"}`, crm.FindRecordsBranchProviderRejected, sdkgo.FailureAuthorization, "NO_PERMISSION"},
		"invalid module": {http.StatusBadRequest, `{"code":"INVALID_MODULE","details":{"resource_path_index":0},"message":"SENTINEL","status":"error"}`, crm.FindRecordsBranchProviderRejected, sdkgo.FailureValidation, "INVALID_MODULE"},
		"redirect":       {http.StatusFound, `{}`, crm.FindRecordsBranchProviderRejected, sdkgo.FailureProtocol, "data center"},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				response.Header().Set("Location", "https://www.zohoapis.eu/crm/v8/coql")
				writeJSON(t, response, scenario.status, scenario.body)
			})
			result, err := sdkgo.RunQuery(newCRMDexContext("find"), newCRMClient(t, provider.URL).FindRecords(), crmConnection, contactByEmailInput())
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			require.Equal(t, scenario.kind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, scenario.text)
			requireNoSentinel(t, result)
			require.Equal(t, 1, provider.requestCount(), "a conclusive rejection is not resent")
		})
	}
}

func TestFindRecordsRetriesRateLimitsAndOutages(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			response.Header().Set("Retry-After", "7")
			writeJSON(t, response, http.StatusTooManyRequests, `{"code":"TOO_MANY_REQUESTS","message":"SENTINEL","status":"error"}`)
			return
		}
		writeJSON(t, response, http.StatusInternalServerError, `{"code":"INTERNAL_ERROR","message":"SENTINEL","status":"error"}`)
	})
	client := newCRMClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newCRMDexContext("find"), client.FindRecords(), crmConnection, contactByEmailInput())
	retry := requireRetry(t, err, sdkgo.FailureRateLimit)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 7*time.Second, retryAfter.After)
	require.Contains(t, retry.Failure.Message, "TOO_MANY_REQUESTS")
	_, err = sdkgo.RunQuery(newCRMDexContext("find"), client.FindRecords(), crmConnection, contactByEmailInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
}

func TestFindRecordsRejectsOversizedMalformedAndCredentialReflectingPages(t *testing.T) {
	for name, body := range map[string]string{
		"oversized":         `{"data":[{"id":"` + testContactID + `","Notes":"` + strings.Repeat("x", 2048) + `"}]}`,
		"malformed":         `{"data":`,
		"record without id": `{"data":[{"Email":"jane@acme.example.com"}]}`,
		"reflected token":   `{"data":[{"id":"` + testContactID + `","Email":"` + testAccessToken + `"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, body)
			})
			client, err := crm.New(crm.Config{MaxResponseBytes: 1024}, testCredentialProvider(), crm.WithAPIBaseURL(provider.URL+"/crm/v8"))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newCRMDexContext("find"), client.FindRecords(), crmConnection, contactByEmailInput())
			require.NoError(t, err)
			require.Equal(t, crm.FindRecordsBranchInvalidResponse, result.Branch)
			requireNoSentinel(t, result)
		})
	}
}

func TestFindRecordsSelectsDefectWithoutARequestForInvalidInput(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	input := contactByEmailInput()
	input.Conditions[0].Value.Text = "o'brien@acme.example.com"
	result, err := sdkgo.RunQuery(newCRMDexContext("find"), newCRMClient(t, provider.URL).FindRecords(), crmConnection, input)
	require.NoError(t, err)
	require.Equal(t, crm.FindRecordsBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	require.NotContains(t, result.Failure.Message, "brien")
	require.Zero(t, provider.requestCount())
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "brien")
}
