// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const responsesPath = "/forms/lT4Z3j/responses"

func responseListJSON(totalItems int, pageCount int, items ...string) string {
	return fmt.Sprintf(`{"total_items": %d, "page_count": %d, "items": [%s]}`, totalItems, pageCount, strings.Join(items, ","))
}

func TestListResponsesSendsTheBoundsAndDecodesTypedAnswers(t *testing.T) {
	fake := newFakeTypeform(t, map[string]http.HandlerFunc{
		"GET " + responsesPath: respondJSON(http.StatusOK, responseListJSON(5, 3, formResponseJSON("tokenNewest"), formResponseJSON("tokenOlder"))),
	})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), typeform.Config{DataCenter: typeform.DataCenterEu})
	pacific := time.FixedZone("PDT", -7*60*60)
	result, err := sdkgo.RunQuery(newStepContext("list"), client.ListResponses(), testConnection, typeform.ListResponsesInput{
		FormID: testFormID, PageSize: 2, Since: time.Date(2026, time.September, 29, 17, 0, 0, 999, pacific),
		Until: time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC), Before: "tokenFirst",
	})
	require.NoError(t, err)
	require.Equal(t, typeform.ListResponsesBranchListed, result.Branch)
	require.Equal(t, typeform.ResponsePage{
		FormID: testFormID, Responses: []typeform.FormResponse{expectedResponse("tokenNewest"), expectedResponse("tokenOlder")},
		TotalItems: 5, PageCount: 3, NextBeforeToken: "tokenOlder",
	}, result.Value)
	requests := fake.requestsTo(http.MethodGet, responsesPath)
	require.Len(t, requests, 1)
	require.Equal(t, "api.eu.typeform.com", requests[0].host, "responses live in the account's data center")
	require.Equal(t, map[string][]string{
		"page_size": {"2"}, "since": {"2026-09-30T00:00:00"}, "until": {"2026-09-30T12:00:00"}, "before": {"tokenFirst"},
	}, map[string][]string(requests[0].query), "since and until are UTC to the second")
	requireSecretFree(t, result)
	encoded := fmt.Sprintf("%+v", result.Value)
	require.NotContains(t, encoded, "4242", "a card's last four digits are not copied")
	require.NotContains(t, encoded, "CARDHOLDER-NAME")
}

func TestListResponsesEndsPagingOnAShortPageAndForAfterRequests(t *testing.T) {
	for _, test := range []struct {
		name  string
		input typeform.ListResponsesInput
		body  string
	}{
		{"short page", typeform.ListResponsesInput{FormID: testFormID, PageSize: 2}, responseListJSON(1, 1, formResponseJSON("onlyToken"))},
		{"after cursor", typeform.ListResponsesInput{FormID: testFormID, PageSize: 1, After: "processedToken"}, responseListJSON(4, 4, formResponseJSON("newerToken"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeTypeform(t, map[string]http.HandlerFunc{"GET " + responsesPath: respondJSON(http.StatusOK, test.body)})
			client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), typeform.Config{})
			result, err := sdkgo.RunQuery(newStepContext("list"), client.ListResponses(), testConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, typeform.ListResponsesBranchListed, result.Branch)
			require.Len(t, result.Value.Responses, 1)
			require.Empty(t, result.Value.NextBeforeToken)
		})
	}
}

func TestListResponsesAcceptsTheResponsesAPIShapes(t *testing.T) {
	item := `{"response_id": "responseOnly", "landing_id": "responseOnly", "landed_at": "2017-09-14T22:33:59Z", "submitted_at": "2017-09-14T22:38:22Z",
	  "metadata": {"user_agent": "Mozilla/5.0", "network_id": "respondent_network_id"}, "hidden": {}, "calculated": {"score": 2},
	  "answers": [
	    {"field": {"id": "hVONkQcnSNRj", "ref": "my_custom_dropdown_reference", "type": "dropdown"}, "text": "Job opportunities", "type": "text"},
	    {"field": {"id": "PNe8ZKBK8C2Q", "type": "picture_choice"}, "choices": {"labels": ["New York", "Tokyo"]}, "type": "choices"},
	    {"field": {"id": "KoJxDM3c6x8h", "type": "date"}, "date": "2012-03-20T00:00:00Z", "type": "date"},
	    {"field": {"id": "ceIXxpbP3t2q", "type": "multiple_choice"}, "choice": {"label": "Other"}, "type": "choice"}
	  ]}`
	fake := newFakeTypeform(t, map[string]http.HandlerFunc{"GET " + responsesPath: respondJSON(http.StatusOK, responseListJSON(1, 1, item))})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), typeform.Config{})
	result, err := sdkgo.RunQuery(newStepContext("list"), client.ListResponses(), testConnection, typeform.ListResponsesInput{FormID: testFormID})
	require.NoError(t, err)
	require.Equal(t, typeform.ListResponsesBranchListed, result.Branch)
	require.Equal(t, []typeform.FormResponse{{
		Token: "responseOnly", LandedAt: time.Date(2017, time.September, 14, 22, 33, 59, 0, time.UTC),
		SubmittedAt: time.Date(2017, time.September, 14, 22, 38, 22, 0, time.UTC), Score: pointerTo(2.0),
		Answers: []typeform.FormAnswer{
			{FieldID: "hVONkQcnSNRj", FieldRef: "my_custom_dropdown_reference", FieldType: "dropdown", Type: typeform.AnswerTypeText, Text: "Job opportunities"},
			{FieldID: "PNe8ZKBK8C2Q", FieldType: "picture_choice", Type: typeform.AnswerTypeChoices, Choices: &typeform.FormAnswerChoices{Labels: []string{"New York", "Tokyo"}}},
			{FieldID: "KoJxDM3c6x8h", FieldType: "date", Type: typeform.AnswerTypeDate, Date: "2012-03-20T00:00:00Z"},
			{FieldID: "ceIXxpbP3t2q", FieldType: "multiple_choice", Type: typeform.AnswerTypeChoice, Choice: &typeform.FormAnswerChoice{Label: "Other"}},
		},
	}}, result.Value.Responses, "a missing token falls back to response_id, and the date keeps Typeform's text")
}

func TestListResponsesRejectsInvalidInputWithoutARequest(t *testing.T) {
	since := time.Date(2026, time.September, 30, 0, 0, 0, 0, time.UTC)
	for _, input := range []typeform.ListResponsesInput{
		{FormID: ""}, {FormID: "lT4Z3j/../x"}, {FormID: testFormID, PageSize: 1001}, {FormID: testFormID, PageSize: -1},
		{FormID: testFormID, Since: since, Until: since.Add(-time.Second)}, {FormID: testFormID, Before: "a", After: "b"},
		{FormID: testFormID, Before: "not a token"}, {FormID: testFormID, After: "not&a&token"},
	} {
		fake := newFakeTypeform(t, nil)
		client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), typeform.Config{})
		result, err := sdkgo.RunQuery(newStepContext("list"), client.ListResponses(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, typeform.ListResponsesBranchDefect, result.Branch, "%+v", input)
		require.Empty(t, fake.recordedRequests())
	}
}

func TestListResponsesSelectsNotFoundAndInvalidResponse(t *testing.T) {
	for _, test := range []struct {
		name           string
		status         int
		body           string
		expectedBranch sdkgo.BranchID
	}{
		{"missing form", http.StatusNotFound, providerError("NOT_EXISTING_ID"), typeform.ListResponsesBranchNotFound},
		{"answer without a field ID", http.StatusOK, responseListJSON(1, 1, strings.Replace(formResponseJSON("t1"), `"id": "JwWggjAKtOkA", `, ``, 1)), typeform.ListResponsesBranchInvalidResponse},
		{"number sent as text", http.StatusOK, responseListJSON(1, 1, strings.Replace(formResponseJSON("t1"), `"number": 4.5`, `"number": "4.5"`, 1)), typeform.ListResponsesBranchInvalidResponse},
		{"answer without its value", http.StatusOK, responseListJSON(1, 1, strings.Replace(formResponseJSON("t1"), `"email": "ada@example.com", `, ``, 1)), typeform.ListResponsesBranchInvalidResponse},
		{"more items than the page size", http.StatusOK, responseListJSON(2, 1, formResponseJSON("t1"), formResponseJSON("t2")), typeform.ListResponsesBranchInvalidResponse},
		{"missing totals", http.StatusOK, `{"items": []}`, typeform.ListResponsesBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeTypeform(t, map[string]http.HandlerFunc{"GET " + responsesPath: respondJSON(test.status, test.body)})
			client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), typeform.Config{})
			result, err := sdkgo.RunQuery(newStepContext("list"), client.ListResponses(), testConnection, typeform.ListResponsesInput{FormID: testFormID, PageSize: 1})
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			requireSecretFree(t, result)
		})
	}
}

func TestFormResponseFindsAnswersByFieldRefAndID(t *testing.T) {
	response := expectedResponse("token")
	answer, isAnswered := response.AnswerByFieldRef("email")
	require.True(t, isAnswered)
	require.Equal(t, "ada@example.com", answer.Email)
	answer, isAnswered = response.AnswerByFieldID("KoJxDM3c6x8h")
	require.True(t, isAnswered)
	require.Equal(t, "2005-10-15", answer.Date)
	_, isAnswered = response.AnswerByFieldRef("skipped")
	require.False(t, isAnswered)
	_, isAnswered = response.AnswerByFieldRef("")
	require.False(t, isAnswered, "a blank ref never matches an answer without one")
}
