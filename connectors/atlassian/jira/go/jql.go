// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maximumJQLCharacters      = 16384
	maximumFilterValues       = 50
	maximumJQLValueCharacters = 1024
)

// IssueSearchOrder selects the ORDER BY clause of a filter's JQL.
type IssueSearchOrder string

const (
	// IssueSearchOrderCreatedDescending lists the newest issues first. It is the default.
	IssueSearchOrderCreatedDescending IssueSearchOrder = "createdDescending"
	// IssueSearchOrderUpdatedDescending lists the most recently changed issues first.
	IssueSearchOrderUpdatedDescending IssueSearchOrder = "updatedDescending"
	// IssueSearchOrderCreatedAscending lists the oldest issues first.
	IssueSearchOrderCreatedAscending IssueSearchOrder = "createdAscending"
)

// IssueSearchFilter is a typed search that the connector turns into escaped JQL, so no caller text can
// change the query's structure. Every set field adds one clause, and clauses are joined with AND.
// At least one clause is required because Jira accepts only bounded queries.
type IssueSearchFilter struct {
	// ProjectKeys limits the search to these project keys, such as OPS.
	ProjectKeys []string `json:"projectKeys,omitempty"`
	// IssueTypeNames limits the search to these issue type names, such as Task or Bug.
	IssueTypeNames []string `json:"issueTypeNames,omitempty"`
	// StatusNames limits the search to these workflow status names, such as In Review.
	StatusNames []string `json:"statusNames,omitempty"`
	// StatusCategories limits the search to these status categories: new, indeterminate, or done.
	StatusCategories []StatusCategoryKey `json:"statusCategories,omitempty"`
	// AssigneeAccountIDs limits the search to issues assigned to these Atlassian account IDs.
	AssigneeAccountIDs []string `json:"assigneeAccountIds,omitempty"`
	// IncludesUnassigned also matches unassigned issues; alone it matches only unassigned issues.
	IncludesUnassigned bool `json:"includesUnassigned,omitempty"`
	// Labels matches issues that carry at least one of these labels.
	Labels []string `json:"labels,omitempty"`
	// SummaryPhrase matches issues whose summary contains this phrase as Jira's text search tokenizes it.
	// Text search ignores case and punctuation, so compare the returned summaries for an exact match.
	SummaryPhrase string `json:"summaryPhrase,omitempty"`
	// Order selects the result order; blank uses IssueSearchOrderCreatedDescending.
	Order IssueSearchOrder `json:"order,omitempty"`
}

// QuoteJQLString returns value as one double-quoted JQL string literal, escaping backslashes and
// double quotes, so it can be spliced into caller-built JQL as a value but never as syntax.
// It rejects invalid UTF-8, control characters, and values longer than 1024 characters.
//
//	projectClause := "project = " + jira.MustQuoteJQLString("OPS")
func QuoteJQLString(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errors.New("JQL value must be valid UTF-8")
	}
	if utf8.RuneCountInString(value) > maximumJQLValueCharacters {
		return "", fmt.Errorf("JQL value cannot exceed %d characters", maximumJQLValueCharacters)
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f {
			return "", errors.New("JQL value cannot contain control characters")
		}
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`, nil
}

// MustQuoteJQLString is QuoteJQLString for a trusted constant; it panics when the value is rejected.
func MustQuoteJQLString(value string) string {
	quoted, err := QuoteJQLString(value)
	if err != nil {
		panic(err)
	}
	return quoted
}

// JQL returns the bounded, escaped JQL query the filter describes.
func (filter IssueSearchFilter) JQL() (string, error) {
	var clauses []string
	for _, list := range []struct {
		field  string
		values []string
	}{
		{"project", filter.ProjectKeys},
		{"issuetype", filter.IssueTypeNames},
		{"status", filter.StatusNames},
		{"labels", filter.Labels},
	} {
		clause, err := buildInClause(list.field, list.values)
		if err != nil {
			return "", err
		}
		if clause != "" {
			clauses = append(clauses, clause)
		}
	}
	if len(filter.StatusCategories) > 0 {
		clause, err := buildStatusCategoryClause(filter.StatusCategories)
		if err != nil {
			return "", err
		}
		clauses = append(clauses, clause)
	}
	assigneeClause, err := buildAssigneeClause(filter.AssigneeAccountIDs, filter.IncludesUnassigned)
	if err != nil {
		return "", err
	}
	if assigneeClause != "" {
		clauses = append(clauses, assigneeClause)
	}
	if strings.TrimSpace(filter.SummaryPhrase) != "" {
		clause, err := buildSummaryPhraseClause(strings.TrimSpace(filter.SummaryPhrase))
		if err != nil {
			return "", err
		}
		clauses = append(clauses, clause)
	}
	if len(clauses) == 0 {
		return "", errors.New("filter needs at least one clause, because Jira accepts only bounded queries")
	}
	order, err := buildOrderClause(filter.Order)
	if err != nil {
		return "", err
	}
	return strings.Join(clauses, " AND ") + " " + order, nil
}

func buildInClause(field string, values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	if len(values) > maximumFilterValues {
		return "", fmt.Errorf("filter accepts at most %d %s values", maximumFilterValues, field)
	}
	quotedValues := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return "", fmt.Errorf("filter %s values cannot be blank", field)
		}
		quoted, err := QuoteJQLString(trimmed)
		if err != nil {
			return "", fmt.Errorf("filter %s value: %w", field, err)
		}
		quotedValues = append(quotedValues, quoted)
	}
	return field + " in (" + strings.Join(quotedValues, ", ") + ")", nil
}

// buildStatusCategoryClause uses category IDs, which unlike category names are not translated.
func buildStatusCategoryClause(categories []StatusCategoryKey) (string, error) {
	categoryIDs := map[StatusCategoryKey]string{StatusCategoryToDo: "2", StatusCategoryInProgress: "4", StatusCategoryDone: "3"}
	selected := make([]string, 0, len(categories))
	for _, category := range categories {
		categoryID, isKnown := categoryIDs[category]
		if !isKnown {
			return "", errors.New("filter statusCategories accepts new, indeterminate, and done")
		}
		selected = append(selected, categoryID)
	}
	return "statusCategory in (" + strings.Join(selected, ", ") + ")", nil
}

func buildAssigneeClause(accountIDs []string, includesUnassigned bool) (string, error) {
	if len(accountIDs) > maximumFilterValues {
		return "", fmt.Errorf("filter accepts at most %d assignee account IDs", maximumFilterValues)
	}
	for _, accountID := range accountIDs {
		if !accountIDPattern.MatchString(accountID) {
			return "", errors.New("filter assigneeAccountIds must be Atlassian account IDs")
		}
	}
	inClause, err := buildInClause("assignee", accountIDs)
	if err != nil {
		return "", err
	}
	switch {
	case inClause != "" && includesUnassigned:
		return "(" + inClause + " OR assignee is EMPTY)", nil
	case includesUnassigned:
		return "assignee is EMPTY", nil
	default:
		return inClause, nil
	}
}

// buildSummaryPhraseClause quotes the phrase once for Jira's text search and once for JQL.
func buildSummaryPhraseClause(phrase string) (string, error) {
	textSearchPhrase := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(phrase) + `"`
	quoted, err := QuoteJQLString(textSearchPhrase)
	if err != nil {
		return "", fmt.Errorf("filter summaryPhrase: %w", err)
	}
	return "summary ~ " + quoted, nil
}

func buildOrderClause(order IssueSearchOrder) (string, error) {
	switch order {
	case "", IssueSearchOrderCreatedDescending:
		return "ORDER BY created DESC", nil
	case IssueSearchOrderUpdatedDescending:
		return "ORDER BY updated DESC", nil
	case IssueSearchOrderCreatedAscending:
		return "ORDER BY created ASC", nil
	default:
		return "", errors.New("filter order must be createdDescending, updatedDescending, or createdAscending")
	}
}

// validateCallerJQL checks caller-built JQL without parsing it; Jira reports syntax errors.
func validateCallerJQL(jql string) (string, error) {
	trimmed := strings.TrimSpace(jql)
	switch {
	case trimmed == "":
		return "", errors.New("jql is required when no filter is set")
	case !utf8.ValidString(trimmed):
		return "", errors.New("jql must be valid UTF-8")
	case utf8.RuneCountInString(trimmed) > maximumJQLCharacters:
		return "", fmt.Errorf("jql cannot exceed %d characters", maximumJQLCharacters)
	case strings.ContainsRune(trimmed, 0):
		return "", errors.New("jql cannot contain NUL characters")
	}
	return trimmed, nil
}
