// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
)

func TestQuoteJQLStringEscapesQuotesAndBackslashes(t *testing.T) {
	quoted, err := jira.QuoteJQLString(`C:\ops "night" shift`)
	require.NoError(t, err)
	require.Equal(t, `"C:\\ops \"night\" shift"`, quoted)
	require.Equal(t, `"OPS"`, jira.MustQuoteJQLString("OPS"))
	for _, value := range []string{"line\nbreak", "\x00", string([]byte{0xff})} {
		_, err := jira.QuoteJQLString(value)
		require.Error(t, err, "%q", value)
	}
	require.Panics(t, func() { jira.MustQuoteJQLString("tab\there") })
}

func TestFilterJQLJoinsEveryClauseAndChoosesTheOrder(t *testing.T) {
	jql, err := jira.IssueSearchFilter{
		ProjectKeys: []string{"OPS", "FAC"}, IssueTypeNames: []string{"Bug"}, StatusNames: []string{"In Review"},
		StatusCategories: []jira.StatusCategoryKey{jira.StatusCategoryDone}, AssigneeAccountIDs: []string{"712020:0e4e"},
		IncludesUnassigned: true, Labels: []string{"incident"}, SummaryPhrase: " fire drill ", Order: jira.IssueSearchOrderUpdatedDescending,
	}.JQL()
	require.NoError(t, err)
	require.Equal(t, `project in ("OPS", "FAC") AND issuetype in ("Bug") AND status in ("In Review") AND labels in ("incident")`+
		` AND statusCategory in (3) AND (assignee in ("712020:0e4e") OR assignee is EMPTY) AND summary ~ "\"fire drill\""`+
		` ORDER BY updated DESC`, jql)

	jql, err = jira.IssueSearchFilter{IncludesUnassigned: true, Order: jira.IssueSearchOrderCreatedAscending}.JQL()
	require.NoError(t, err)
	require.Equal(t, `assignee is EMPTY ORDER BY created ASC`, jql)
}

func TestFilterValuesCannotChangeTheQueryStructure(t *testing.T) {
	jql, err := jira.IssueSearchFilter{ProjectKeys: []string{`OPS") OR project is not EMPTY OR project in ("X`}}.JQL()
	require.NoError(t, err)
	require.Equal(t, `project in ("OPS\") OR project is not EMPTY OR project in (\"X") ORDER BY created DESC`, jql)

	jql, err = jira.IssueSearchFilter{SummaryPhrase: `a\" OR summary ~ "b`}.JQL()
	require.NoError(t, err)
	require.Equal(t, `summary ~ "\"a\\\\\\\" OR summary ~ \\\"b\"" ORDER BY created DESC`, jql)
}

func TestFilterRejectsUnknownValues(t *testing.T) {
	for _, filter := range []jira.IssueSearchFilter{
		{StatusCategories: []jira.StatusCategoryKey{"blocked"}},
		{AssigneeAccountIDs: []string{"ada@example.com"}},
		{ProjectKeys: []string{" "}},
		{ProjectKeys: []string{"OPS"}, Order: "priority"},
	} {
		_, err := filter.JQL()
		require.Error(t, err, "%+v", filter)
	}
}
