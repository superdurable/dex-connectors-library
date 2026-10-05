// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maximumFilterValues       = 50
	maximumJQLValueCharacters = 1024
	jqlDateLayout             = "2006-01-02"
)

// TicketSearchOrder selects the ORDER BY clause of a ticket search.
type TicketSearchOrder string

const (
	// TicketSearchOrderCreatedDescending lists the newest requests first. It is the default.
	TicketSearchOrderCreatedDescending TicketSearchOrder = "createdDescending"
	// TicketSearchOrderUpdatedDescending lists the most recently changed requests first.
	TicketSearchOrderUpdatedDescending TicketSearchOrder = "updatedDescending"
	// TicketSearchOrderCreatedAscending lists the oldest requests first.
	TicketSearchOrderCreatedAscending TicketSearchOrder = "createdAscending"
)

// BuildTicketSearchJQL returns the bounded, escaped JQL that searchTickets sends for input, so a Flow
// can review it. Every set filter adds one clause, joined with AND after the required project clause;
// no caller text can change the query's structure.
func BuildTicketSearchJQL(input SearchTicketsInput) (string, error) {
	projectKey := strings.TrimSpace(input.ProjectKey)
	if !projectKeyPattern.MatchString(projectKey) {
		return "", errors.New("projectKey must be the service desk's uppercase project key such as ITH")
	}
	clauses := []string{"project = " + quoteJQLString(projectKey)}
	if len(input.StatusCategories) > 0 {
		clause, err := buildStatusCategoryClause(input.StatusCategories)
		if err != nil {
			return "", err
		}
		clauses = append(clauses, clause)
	}
	for _, reporterAccountID := range input.ReporterAccountIDs {
		if !accountIDPattern.MatchString(reporterAccountID) {
			return "", errors.New("reporterAccountIds must be Atlassian account IDs")
		}
	}
	for _, list := range []struct {
		field  string
		values []string
	}{
		{"status", input.StatusNames},
		{"labels", input.Labels},
		{"reporter", input.ReporterAccountIDs},
	} {
		clause, err := buildInClause(list.field, list.values)
		if err != nil {
			return "", err
		}
		if clause != "" {
			clauses = append(clauses, clause)
		}
	}
	if phrase := strings.TrimSpace(input.SummaryPhrase); phrase != "" {
		clause, err := buildSummaryPhraseClause(phrase)
		if err != nil {
			return "", err
		}
		clauses = append(clauses, clause)
	}
	if date := strings.TrimSpace(input.UpdatedSinceDate); date != "" {
		if _, err := time.Parse(jqlDateLayout, date); err != nil {
			return "", errors.New("updatedSinceDate must be a calendar date written YYYY-MM-DD")
		}
		clauses = append(clauses, "updated >= "+quoteJQLString(date))
	}
	order, err := buildOrderClause(input.Order)
	if err != nil {
		return "", err
	}
	return strings.Join(clauses, " AND ") + " " + order, nil
}

// quoteJQLString returns a validated value as one double-quoted JQL string literal.
func quoteJQLString(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

// validateJQLValue rejects invalid UTF-8, control characters, and values longer than 1024 characters.
func validateJQLValue(value string) error {
	if !utf8.ValidString(value) {
		return errors.New("value must be valid UTF-8")
	}
	if utf8.RuneCountInString(value) > maximumJQLValueCharacters {
		return fmt.Errorf("value cannot exceed %d characters", maximumJQLValueCharacters)
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f {
			return errors.New("value cannot contain control characters")
		}
	}
	return nil
}

func buildInClause(field string, values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	if len(values) > maximumFilterValues {
		return "", fmt.Errorf("search accepts at most %d %s values", maximumFilterValues, field)
	}
	quotedValues := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return "", fmt.Errorf("search %s values cannot be blank", field)
		}
		if err := validateJQLValue(trimmed); err != nil {
			return "", fmt.Errorf("search %s %w", field, err)
		}
		quotedValues = append(quotedValues, quoteJQLString(trimmed))
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
			return "", errors.New("statusCategories accepts new, indeterminate, and done")
		}
		selected = append(selected, categoryID)
	}
	return "statusCategory in (" + strings.Join(selected, ", ") + ")", nil
}

// buildSummaryPhraseClause quotes the phrase once for Jira's text search and once for JQL.
func buildSummaryPhraseClause(phrase string) (string, error) {
	textSearchPhrase := quoteJQLString(phrase)
	if err := validateJQLValue(textSearchPhrase); err != nil {
		return "", fmt.Errorf("summaryPhrase %w", err)
	}
	return "summary ~ " + quoteJQLString(textSearchPhrase), nil
}

func buildOrderClause(order TicketSearchOrder) (string, error) {
	switch order {
	case "", TicketSearchOrderCreatedDescending:
		return "ORDER BY created DESC", nil
	case TicketSearchOrderUpdatedDescending:
		return "ORDER BY updated DESC", nil
	case TicketSearchOrderCreatedAscending:
		return "ORDER BY created ASC", nil
	default:
		return "", errors.New("order must be createdDescending, updatedDescending, or createdAscending")
	}
}
