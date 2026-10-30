// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestBuildTicketSearchJQLEscapesEveryValue(t *testing.T) {
	jql, err := jiraservicemanagement.BuildTicketSearchJQL(jiraservicemanagement.SearchTicketsInput{
		ProjectKey:         "ITH",
		StatusCategories:   []jiraservicemanagement.StatusCategoryKey{jiraservicemanagement.StatusCategoryToDo, jiraservicemanagement.StatusCategoryInProgress},
		StatusNames:        []string{`Waiting "for" support`},
		Labels:             []string{`vip") OR project != "ITH`},
		ReporterAccountIDs: []string{testCustomerAccount},
		SummaryPhrase:      `Laptop "boot" \ loop`,
		UpdatedSinceDate:   "2026-09-01",
		Order:              jiraservicemanagement.TicketSearchOrderUpdatedDescending,
	})
	require.NoError(t, err)
	require.Equal(t, `project = "ITH" AND statusCategory in (2, 4) AND status in ("Waiting \"for\" support") AND `+
		`labels in ("vip\") OR project != \"ITH") AND reporter in ("`+testCustomerAccount+`") AND `+
		`summary ~ "\"Laptop \\\"boot\\\" \\\\ loop\"" AND updated >= "2026-09-01" ORDER BY updated DESC`, jql)

	for name, input := range map[string]jiraservicemanagement.SearchTicketsInput{
		"lowercase project": {ProjectKey: "ith"},
		"unknown category":  {ProjectKey: "ITH", StatusCategories: []jiraservicemanagement.StatusCategoryKey{"open"}},
		"reporter email":    {ProjectKey: "ITH", ReporterAccountIDs: []string{"jane@example.com"}},
		"control character": {ProjectKey: "ITH", Labels: []string{"a\nb"}},
		"date with time":    {ProjectKey: "ITH", UpdatedSinceDate: "2026-09-01T00:00:00Z"},
		"unknown order":     {ProjectKey: "ITH", Order: "priority"},
	} {
		_, err := jiraservicemanagement.BuildTicketSearchJQL(input)
		require.Error(t, err, name)
	}
}

func TestSearchTicketsReadsOnePageAndContinuesWithTheToken(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"issues":[`+issueJSON("10042", "ITH-42", "Laptop will not boot", `["hardware"]`)+`],"nextPageToken":"page-2"}`)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("search"), newTestClient(t, provider.URL).SearchTickets(), jsmConnection,
		jiraservicemanagement.SearchTicketsInput{ProjectKey: "ITH", SummaryPhrase: "Laptop", PageSize: 20, NextPageToken: "page-1"})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.SearchTicketsBranchSearched, result.Branch)
	require.Len(t, result.Value.Tickets, 1)
	require.Empty(t, result.Value.Tickets[0].Description, "search pages omit descriptions")
	require.Equal(t, "page-2", result.Value.NextPageToken)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, testPlatformPrefix+"/search/jql", request.path)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(request.body), &body))
	require.Equal(t, `project = "ITH" AND summary ~ "\"Laptop\"" ORDER BY created DESC`, body["jql"])
	require.Equal(t, float64(20), body["maxResults"])
	require.Equal(t, "page-1", body["nextPageToken"])
	require.NotContains(t, body["fields"], "description")
	requireNoSentinel(t, result)
}

func TestSearchTicketsMapsRejectionAndInvalidPages(t *testing.T) {
	rejected := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, `{"errorMessages":["SENTINEL The value 'ITH' does not exist for the field 'project'."]}`)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("search-rejected"), newTestClient(t, rejected.URL).SearchTickets(), jsmConnection,
		jiraservicemanagement.SearchTicketsInput{ProjectKey: "ITH"})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.SearchTicketsBranchProviderRejected, result.Branch)
	requireNoSentinel(t, result)

	invalid := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"issues":[{"id":"x","key":"SENTINEL"}]}`)
	})
	result, err = sdkgo.RunQuery(newTestDexContext("search-invalid"), newTestClient(t, invalid.URL).SearchTickets(), jsmConnection,
		jiraservicemanagement.SearchTicketsInput{ProjectKey: "ITH"})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.SearchTicketsBranchInvalidResponse, result.Branch)

	_, err = sdkgo.RunQuery(newTestDexContext("search-page-size"), newTestClient(t, invalid.URL).SearchTickets(), jsmConnection,
		jiraservicemanagement.SearchTicketsInput{ProjectKey: "ITH", PageSize: 101})
	require.NoError(t, err)
	require.Equal(t, 1, invalid.requestCount(), "an invalid page size sends nothing")
}
