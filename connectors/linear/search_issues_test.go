// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestSearchIssuesSendsATypedFilterAndReturnsSummariesWithACursor(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"issues": map[string]any{
			"nodes":    []any{issueSummaryJSON(testIssueID, "ENG-7", "Fire panel wiring")},
			"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "cursor-2"},
		}})
	})
	updatedAfter := time.Date(2026, 1, 1, 8, 30, 0, 0, time.FixedZone("CET", 3600))
	result, err := runQuery("search", newAPIKeyClient(t, provider.URL).SearchIssues(), linear.SearchIssuesInput{
		Filter: linear.IssueSearchFilter{
			TeamKey: "eng", StateTypes: []linear.WorkflowStateType{linear.WorkflowStateTypeUnstarted, linear.WorkflowStateTypeStarted},
			AssigneeEmail: "Alice@Example.com", LabelNames: []string{"Bug"}, TitleContains: "fire panel", UpdatedAfter: &updatedAfter,
		},
		Order: linear.IssueOrderUpdatedAt, PageSize: 25,
	})
	require.NoError(t, err)
	require.Equal(t, linear.SearchIssuesBranchSearched, result.Branch)
	require.Equal(t, "cursor-2", result.Value.NextCursor)
	require.Len(t, result.Value.Issues, 1)
	issue := result.Value.Issues[0]
	require.Equal(t, testIssueID, issue.ID)
	require.Equal(t, "ENG-7", issue.Identifier)
	require.Equal(t, linear.TeamReference{ID: testTeamID, Key: "ENG", Name: "Engineering"}, issue.Team)
	require.Equal(t, linear.WorkflowStateReference{ID: testStateID, Name: "Todo", Type: linear.WorkflowStateTypeUnstarted}, issue.State)
	require.Equal(t, testUserID, issue.AssigneeID)
	require.Equal(t, 2, issue.Priority)
	require.Equal(t, []string{testLabelID}, issue.LabelIDs)
	require.Equal(t, time.Date(2026, 1, 28, 10, 5, 0, 0, time.UTC), issue.UpdatedAt)

	request := provider.request(0)
	require.Equal(t, "LinearSearchIssues", request.operationName)
	require.Equal(t, float64(25), request.variables["first"])
	require.Equal(t, "updatedAt", request.variables["orderBy"])
	require.Equal(t, false, request.variables["includeArchived"])
	require.NotContains(t, request.variables, "after")
	require.Equal(t, map[string]any{
		"team":      map[string]any{"key": map[string]any{"eq": "ENG"}},
		"state":     map[string]any{"type": map[string]any{"in": []any{"unstarted", "started"}}},
		"assignee":  map[string]any{"email": map[string]any{"eqIgnoreCase": "Alice@Example.com"}},
		"labels":    map[string]any{"some": map[string]any{"name": map[string]any{"in": []any{"Bug"}}}},
		"title":     map[string]any{"containsIgnoreCase": "fire panel"},
		"updatedAt": map[string]any{"gt": "2026-01-01T07:30:00.000Z"},
	}, request.variables["filter"])
}

func TestSearchIssuesContinuesWithTheCursorAndEndsWithoutOne(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"issues": map[string]any{
			"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
		}})
	})
	result, err := runQuery("next", newAPIKeyClient(t, provider.URL).SearchIssues(), linear.SearchIssuesInput{
		Filter: linear.IssueSearchFilter{TeamID: testTeamID, IsUnassigned: true}, Cursor: "cursor-2", IncludesArchived: true,
	})
	require.NoError(t, err)
	require.Equal(t, linear.SearchIssuesBranchSearched, result.Branch)
	require.Empty(t, result.Value.NextCursor)
	require.NotNil(t, result.Value.Issues, "an empty page is an empty list, not null")
	request := provider.request(0)
	require.Equal(t, "cursor-2", request.variables["after"])
	require.Equal(t, float64(linear.DefaultSearchPageSize), request.variables["first"])
	require.Equal(t, "createdAt", request.variables["orderBy"])
	require.Equal(t, true, request.variables["includeArchived"])
	require.Equal(t, map[string]any{
		"team": map[string]any{"id": map[string]any{"eq": testTeamID}}, "assignee": map[string]any{"null": true},
	}, request.variables["filter"])
}

func TestSearchIssuesRejectsAContinuingPageWithoutACursor(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"issues": map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": true}}})
	})
	result, err := runQuery("no-cursor", newAPIKeyClient(t, provider.URL).SearchIssues(), linear.SearchIssuesInput{})
	require.NoError(t, err)
	require.Equal(t, linear.SearchIssuesBranchInvalidResponse, result.Branch)
}

func TestSearchIssuesRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingLinear(t, func(http.ResponseWriter, recordedRequest, int) { t.Error("no request may be sent") })
	client := newAPIKeyClient(t, provider.URL)
	for name, input := range map[string]linear.SearchIssuesInput{
		"page too large":       {PageSize: linear.MaxSearchPageSize + 1},
		"unknown order":        {Order: "priority"},
		"team id and key":      {Filter: linear.IssueSearchFilter{TeamID: testTeamID, TeamKey: "ENG"}},
		"team id not a uuid":   {Filter: linear.IssueSearchFilter{TeamID: "ENG"}},
		"unknown state type":   {Filter: linear.IssueSearchFilter{StateTypes: []linear.WorkflowStateType{"done"}}},
		"unassigned and email": {Filter: linear.IssueSearchFilter{IsUnassigned: true, AssigneeEmail: "a@example.com"}},
		"bad email":            {Filter: linear.IssueSearchFilter{AssigneeEmail: "alice"}},
		"multiline title":      {Filter: linear.IssueSearchFilter{Title: "a\nb"}},
		"duplicate label":      {Filter: linear.IssueSearchFilter{LabelIDs: []string{testLabelID, testLabelID}}},
		"forged cursor":        {Cursor: "abc def"},
	} {
		result, err := runQuery("invalid-"+name, client.SearchIssues(), input)
		require.NoError(t, err, name)
		require.Equal(t, linear.SearchIssuesBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
	require.Zero(t, provider.requestCount())
}
