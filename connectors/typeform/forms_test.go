// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const formListJSON = `{"total_items": 3, "page_count": 2, "items": [
  {"id": "u6nXL7", "title": "Lead intake", "created_at": "2026-09-01T10:00:00+00:00", "last_updated_at": "2026-09-02T12:00:00+02:00",
   "settings": {"is_public": false}, "self": {"href": "https://api.typeform.com/forms/u6nXL7"}, "_links": {"display": "https://acme.typeform.com/to/u6nXL7"}},
  {"id": "lT4Z3j", "title": "Webhooks example"}
]}`

func TestListFormsSendsTheFiltersToTheDataCenterHostAndReturnsTheNextPage(t *testing.T) {
	for _, test := range []struct {
		dataCenter   typeform.DataCenter
		expectedHost string
	}{{"", "api.typeform.com"}, {typeform.DataCenterEu, "api.eu.typeform.com"}, {typeform.DataCenterNewEu, "api.typeform.eu"}} {
		t.Run(string(test.dataCenter), func(t *testing.T) {
			fake := newFakeTypeform(t, map[string]http.HandlerFunc{"GET /forms": respondJSON(http.StatusOK, formListJSON)})
			client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), typeform.Config{DataCenter: test.dataCenter})
			result, err := sdkgo.RunQuery(newStepContext("list"), client.ListForms(), testConnection,
				typeform.ListFormsInput{Search: "lead form", WorkspaceID: "Aw33bz", PageSize: 50})
			require.NoError(t, err)
			require.Equal(t, typeform.ListFormsBranchListed, result.Branch)
			require.Equal(t, typeform.FormPage{
				Forms: []typeform.FormSummary{
					{ID: "u6nXL7", Title: "Lead intake", IsPublic: false, DisplayURL: "https://acme.typeform.com/to/u6nXL7",
						CreatedAt: time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC), LastUpdatedAt: time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)},
					{ID: "lT4Z3j", Title: "Webhooks example", IsPublic: true},
				},
				Page: 1, PageCount: 2, TotalItems: 3, NextPage: 2,
			}, result.Value)
			requests := fake.requestsTo(http.MethodGet, "/forms")
			require.Len(t, requests, 1)
			require.Equal(t, test.expectedHost, requests[0].host)
			require.Equal(t, "Bearer "+sentinelToken, requests[0].authorization)
			require.Equal(t, map[string][]string{"page": {"1"}, "page_size": {"50"}, "search": {"lead form"}, "workspace_id": {"Aw33bz"}},
				map[string][]string(requests[0].query))
			requireSecretFree(t, result)
		})
	}
}

func TestListFormsOnTheLastPageHasNoNextPage(t *testing.T) {
	fake := newFakeTypeform(t, map[string]http.HandlerFunc{"GET /forms": respondJSON(http.StatusOK, formListJSON)})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), typeform.Config{})
	result, err := sdkgo.RunQuery(newStepContext("list"), client.ListForms(), testConnection, typeform.ListFormsInput{Page: 2})
	require.NoError(t, err)
	require.Equal(t, 2, result.Value.Page)
	require.Zero(t, result.Value.NextPage)
	require.Equal(t, []string{"2"}, fake.requestsTo(http.MethodGet, "/forms")[0].query["page"])
	require.Empty(t, fake.requestsTo(http.MethodGet, "/forms")[0].query["page_size"], "zero uses Typeform's default page size")
}

func TestListFormsRejectsInvalidInputWithoutARequest(t *testing.T) {
	for _, input := range []typeform.ListFormsInput{
		{PageSize: 201}, {PageSize: -1}, {Page: -1}, {Page: 10001}, {WorkspaceID: "../x"}, {Search: strings.Repeat("a", 257)},
	} {
		fake := newFakeTypeform(t, nil)
		client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), typeform.Config{})
		result, err := sdkgo.RunQuery(newStepContext("list"), client.ListForms(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, typeform.ListFormsBranchDefect, result.Branch, "%+v", input)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		require.Empty(t, fake.recordedRequests())
	}
}

const formDefinitionJSON = `{
  "id": "lT4Z3j", "title": "Webhooks example", "language": "en",
  "hidden": ["user_id", "campaign"],
  "_links": {"display": "https://acme.typeform.com/to/lT4Z3j", "responses": "https://api.typeform.com/forms/lT4Z3j/responses"},
  "fields": [
    {"id": "JwWggjAKtOkA", "ref": "first_name", "title": "What is your first name?", "type": "short_text", "validations": {"required": true}},
    {"id": "k6TP9oLGgHjl", "ref": "city", "title": "Favorite city?", "type": "multiple_choice",
     "properties": {"choices": [{"id": "4WIlUvKOl0UB", "ref": "london", "label": "London"}, {"id": "vLZWecsW8HM6", "ref": "sydney", "label": "Sydney"}]}},
    {"id": "GROUPfield01", "ref": "contact", "title": "Contact details", "type": "group", "properties": {"fields": [
      {"id": "SMEUb7VJz92Q", "ref": "email", "title": "Email", "type": "email", "validations": {"required": true}},
      {"id": "PH0N3fieldA1", "title": "Phone", "type": "phone_number"}
    ]}},
    {"id": "RUqkXSeXBXSd", "ref": "consent", "title": "May we follow up?", "type": "yes_no"}
  ]
}`

func TestGetFormReturnsEveryFieldWithItsRefAndGroup(t *testing.T) {
	fake := newFakeTypeform(t, map[string]http.HandlerFunc{"GET /forms/lT4Z3j": respondJSON(http.StatusOK, formDefinitionJSON)})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), typeform.Config{})
	result, err := sdkgo.RunQuery(newStepContext("get"), client.GetForm(), testConnection, typeform.GetFormInput{FormID: testFormID})
	require.NoError(t, err)
	require.Equal(t, typeform.GetFormBranchFound, result.Branch)
	require.Equal(t, typeform.Form{
		ID: "lT4Z3j", Title: "Webhooks example", Language: "en", HiddenFields: []string{"user_id", "campaign"},
		DisplayURL: "https://acme.typeform.com/to/lT4Z3j",
		Fields: []typeform.FormField{
			{ID: "JwWggjAKtOkA", Ref: "first_name", Title: "What is your first name?", Type: "short_text", IsRequired: true},
			{ID: "k6TP9oLGgHjl", Ref: "city", Title: "Favorite city?", Type: "multiple_choice", Choices: []typeform.FormFieldChoice{
				{ID: "4WIlUvKOl0UB", Ref: "london", Label: "London"}, {ID: "vLZWecsW8HM6", Ref: "sydney", Label: "Sydney"},
			}},
			{ID: "GROUPfield01", Ref: "contact", Title: "Contact details", Type: "group"},
			{ID: "SMEUb7VJz92Q", Ref: "email", Title: "Email", Type: "email", IsRequired: true, GroupFieldID: "GROUPfield01"},
			{ID: "PH0N3fieldA1", Title: "Phone", Type: "phone_number", GroupFieldID: "GROUPfield01"},
			{ID: "RUqkXSeXBXSd", Ref: "consent", Title: "May we follow up?", Type: "yes_no"},
		},
	}, result.Value)
	require.Equal(t, testFormID, result.Receipt.ProviderObjectID)
}

func TestGetFormClassifiesTypeformFailuresWithoutProviderText(t *testing.T) {
	for _, test := range []struct {
		name           string
		status         int
		body           string
		expectedBranch sdkgo.BranchID
		expectedKind   sdkgo.FailureKind
	}{
		{"missing form", http.StatusNotFound, providerError("NOT_EXISTING_ID"), typeform.GetFormBranchNotFound, sdkgo.FailureNotFound},
		{"bad token", http.StatusUnauthorized, providerError("UNAUTHORIZED"), typeform.GetFormBranchProviderRejected, sdkgo.FailureAuthentication},
		{"forbidden token", http.StatusForbidden, providerError("AUTHENTICATION_ERROR"), typeform.GetFormBranchProviderRejected, sdkgo.FailureAuthorization},
		{"plan feature", http.StatusPaymentRequired, providerError("PAYMENT_REQUIRED"), typeform.GetFormBranchProviderRejected, sdkgo.FailureProviderRejection},
		{"unreadable body", http.StatusOK, `{"id":`, typeform.GetFormBranchInvalidResponse, sdkgo.FailureProtocol},
		{"another form", http.StatusOK, strings.Replace(formDefinitionJSON, `"id": "lT4Z3j"`, `"id": "other1"`, 1), typeform.GetFormBranchInvalidResponse, sdkgo.FailureProtocol},
		{"field without an ID", http.StatusOK, strings.Replace(formDefinitionJSON, `"id": "RUqkXSeXBXSd", `, ``, 1), typeform.GetFormBranchInvalidResponse, sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeTypeform(t, map[string]http.HandlerFunc{"GET /forms/lT4Z3j": respondJSON(test.status, test.body)})
			client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), typeform.Config{})
			result, err := sdkgo.RunQuery(newStepContext("get"), client.GetForm(), testConnection, typeform.GetFormInput{FormID: testFormID})
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, test.expectedKind, result.Failure.Kind)
			requireSecretFree(t, result)
		})
	}
}

func TestGetFormRetriesRateLimitsServerErrorsAndLostAnswers(t *testing.T) {
	for _, test := range []struct {
		name          string
		handler       http.HandlerFunc
		expectedKind  sdkgo.FailureKind
		expectedDelay time.Duration
	}{
		{"rate limit with retry after", func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Retry-After", "3")
			writeJSON(response, http.StatusTooManyRequests, providerError("RATE_LIMITED"))
		}, sdkgo.FailureRateLimit, 3 * time.Second},
		{"server error", respondJSON(http.StatusServiceUnavailable, providerError("SERVICE_UNAVAILABLE")), sdkgo.FailureAvailability, 0},
		{"request timeout", respondJSON(http.StatusRequestTimeout, providerError("TIMEOUT")), sdkgo.FailureAvailability, 0},
		{"lost answer", func(response http.ResponseWriter, _ *http.Request) {
			connection, _, err := response.(http.Hijacker).Hijack()
			if err == nil {
				_ = connection.Close() // Closing without an answer is the behavior under test.
			}
		}, sdkgo.FailureTransport, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeTypeform(t, map[string]http.HandlerFunc{"GET /forms/lT4Z3j": test.handler})
			client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), typeform.Config{})
			_, err := sdkgo.RunQuery(newStepContext("get"), client.GetForm(), testConnection, typeform.GetFormInput{FormID: testFormID})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.expectedKind, retry.Failure.Kind)
			require.NotContains(t, err.Error(), providerSecretMessage)
			var retryAfter *dex.RetryAfterError
			if test.expectedDelay == 0 {
				require.False(t, errors.As(err, &retryAfter))
				return
			}
			require.ErrorAs(t, err, &retryAfter)
			require.Equal(t, test.expectedDelay, retryAfter.After)
		})
	}
}

func TestGetFormSelectsInvalidResponseForAnOversizedBodyAndDefectForAnInvalidID(t *testing.T) {
	fake := newFakeTypeform(t, map[string]http.HandlerFunc{"GET /forms/lT4Z3j": respondJSON(http.StatusOK, formDefinitionJSON)})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), typeform.Config{MaxResponseBytes: 64})
	result, err := sdkgo.RunQuery(newStepContext("get"), client.GetForm(), testConnection, typeform.GetFormInput{FormID: testFormID})
	require.NoError(t, err)
	require.Equal(t, typeform.GetFormBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)

	result, err = sdkgo.RunQuery(newStepContext("get-invalid"), client.GetForm(), testConnection, typeform.GetFormInput{FormID: "https://form.typeform.com/to/lT4Z3j"})
	require.NoError(t, err)
	require.Equal(t, typeform.GetFormBranchDefect, result.Branch)
	require.Len(t, fake.recordedRequests(), 1, "an invalid form ID sends nothing")
}

func TestOperationsSelectDefectForAnUnusableToken(t *testing.T) {
	fake := newFakeTypeform(t, nil)
	credentials := sdkgo.StaticCredentialProvider[typeform.Credentials]{testConnection: {
		AuthMethodID: typeform.PersonalAccessTokenAuthMethodID, AccessToken: sdkgo.NewSecretString("tfp_line\nbreak"),
	}}
	client := newTestClient(t, fake.redirectingClient(), credentials, typeform.Config{})
	result, err := sdkgo.RunQuery(newStepContext("get"), client.GetForm(), testConnection, typeform.GetFormInput{FormID: testFormID})
	require.NoError(t, err)
	require.Equal(t, typeform.GetFormBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Empty(t, fake.recordedRequests())

	unknownConnection := newTestClient(t, fake.redirectingClient(), sdkgo.StaticCredentialProvider[typeform.Credentials]{}, typeform.Config{})
	_, err = sdkgo.RunQuery(newStepContext("get-missing"), unknownConnection.GetForm(), testConnection, typeform.GetFormInput{FormID: testFormID})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a connection file that is not readable yet retries")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}
