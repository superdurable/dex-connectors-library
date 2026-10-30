// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maximumAdditionalFields = 20
	maximumIdentifierBytes  = 255
)

var (
	issueKeyPattern       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-[1-9][0-9]{0,17}$`)
	numericIDPattern      = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)
	projectKeyPattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,254}$`)
	accountIDPattern      = regexp.MustCompile(`^[A-Za-z0-9:_-]{1,128}$`)
	jiraTimeLayouts       = []string{"2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05-0700", time.RFC3339Nano}
	standardIssueFieldIDs = []string{"summary", "status", "issuetype", "project", "assignee", "reporter", "priority", "labels", "created", "updated"}
)

// StatusCategoryKey is Jira's fixed status category key. Workflow status names vary by project;
// the category is the portable "to do", "in progress", or "done" signal.
type StatusCategoryKey string

const (
	// StatusCategoryToDo is the "To Do" category, key new.
	StatusCategoryToDo StatusCategoryKey = "new"
	// StatusCategoryInProgress is the "In Progress" category, key indeterminate.
	StatusCategoryInProgress StatusCategoryKey = "indeterminate"
	// StatusCategoryDone is the "Done" category, key done.
	StatusCategoryDone StatusCategoryKey = "done"
)

// Issue is the connector's view of one Jira issue. Status, issue type, and priority names are the
// site's own values, never mapped to another vocabulary.
type Issue struct {
	// ID is Jira's numeric issue ID, stable when the issue moves between projects.
	ID string `json:"id"`
	// Key is the human-readable issue key, such as OPS-441; it changes when the issue moves.
	Key string `json:"key"`
	// Summary is the one-line issue title.
	Summary string `json:"summary"`
	// Status is the issue's current workflow status.
	Status IssueStatus `json:"status"`
	// IssueType is the issue's type, such as Task or Bug.
	IssueType IssueTypeReference `json:"issueType"`
	// Project is the project that holds the issue.
	Project ProjectReference `json:"project"`
	// Assignee is the assigned account, or nil for an unassigned issue.
	Assignee *AccountReference `json:"assignee,omitempty"`
	// Reporter is the reporting account, or nil when Jira hides it.
	Reporter *AccountReference `json:"reporter,omitempty"`
	// Priority is the issue priority, or nil when the project does not use priorities.
	Priority *PriorityReference `json:"priority,omitempty"`
	// Labels lists the issue's labels.
	Labels []string `json:"labels,omitempty"`
	// Description is the plain text of the description, returned by getIssue only.
	Description string `json:"description,omitempty"`
	// IsDescriptionTruncated reports that Description stopped at 32767 characters.
	IsDescriptionTruncated bool `json:"isDescriptionTruncated,omitempty"`
	// CreatedAt is when the issue was created.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the issue last changed.
	UpdatedAt time.Time `json:"updatedAt"`
	// AdditionalFields holds the raw Jira JSON of each requested additional field that the issue has,
	// keyed by field ID such as customfield_10020. A requested field Jira omits is absent.
	AdditionalFields map[string]json.RawMessage `json:"additionalFields,omitempty"`
}

// IssueStatus is one Jira workflow status.
type IssueStatus struct {
	// ID is the status ID, stable across renames.
	ID string `json:"id"`
	// Name is the site's status name, such as In Review.
	Name string `json:"name"`
	// CategoryKey is the status category: new, indeterminate, or done.
	CategoryKey StatusCategoryKey `json:"categoryKey,omitempty"`
}

// IssueTypeReference identifies an issue type.
type IssueTypeReference struct {
	// ID is the issue type ID.
	ID string `json:"id"`
	// Name is the issue type name, such as Task.
	Name string `json:"name"`
	// IsSubtask reports a sub-task issue type.
	IsSubtask bool `json:"isSubtask,omitempty"`
}

// ProjectReference identifies a Jira project.
type ProjectReference struct {
	// ID is the numeric project ID.
	ID string `json:"id"`
	// Key is the project key, such as OPS.
	Key string `json:"key"`
	// Name is the project display name.
	Name string `json:"name,omitempty"`
}

// AccountReference identifies an Atlassian account without its email address.
type AccountReference struct {
	// AccountID is the Atlassian account ID used for assignment.
	AccountID string `json:"accountId"`
	// DisplayName is the account's public display name.
	DisplayName string `json:"displayName,omitempty"`
}

// PriorityReference identifies an issue priority.
type PriorityReference struct {
	// ID is the priority ID.
	ID string `json:"id"`
	// Name is the priority name, such as High.
	Name string `json:"name"`
}

type issueResource struct {
	ID     string                     `json:"id"`
	Key    string                     `json:"key"`
	Fields map[string]json.RawMessage `json:"fields"`
}

type standardIssueFields struct {
	Summary   string             `json:"summary"`
	Status    *statusResource    `json:"status"`
	IssueType *issueTypeResource `json:"issuetype"`
	Project   *projectResource   `json:"project"`
	Assignee  *accountResource   `json:"assignee"`
	Reporter  *accountResource   `json:"reporter"`
	Priority  *priorityResource  `json:"priority"`
	Labels    []string           `json:"labels"`
	Created   string             `json:"created"`
	Updated   string             `json:"updated"`
}

type statusResource struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	StatusCategory *struct {
		Key string `json:"key"`
	} `json:"statusCategory"`
}

type issueTypeResource struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Subtask bool   `json:"subtask"`
}

type projectResource struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type accountResource struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
}

type priorityResource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// decodeIssue converts one Jira issue resource. includesDescription adds the plain-text description.
func decodeIssue(resource issueResource, additionalFieldIDs []string, includesDescription bool) (Issue, error) {
	if !numericIDPattern.MatchString(resource.ID) || !issueKeyPattern.MatchString(resource.Key) {
		return Issue{}, errors.New("issue ID or key is invalid")
	}
	var fields standardIssueFields
	if resource.Fields != nil {
		encoded, err := json.Marshal(resource.Fields)
		if err != nil {
			return Issue{}, errors.New("issue fields are invalid")
		}
		if err := json.Unmarshal(encoded, &fields); err != nil {
			return Issue{}, errors.New("issue fields are invalid")
		}
	}
	issue := Issue{ID: resource.ID, Key: resource.Key, Summary: fields.Summary, Labels: fields.Labels}
	if fields.Status != nil {
		issue.Status = fields.Status.view()
	}
	if fields.IssueType != nil {
		issue.IssueType = IssueTypeReference{ID: fields.IssueType.ID, Name: fields.IssueType.Name, IsSubtask: fields.IssueType.Subtask}
	}
	if fields.Project != nil {
		issue.Project = ProjectReference{ID: fields.Project.ID, Key: fields.Project.Key, Name: fields.Project.Name}
	}
	issue.Assignee = fields.Assignee.view()
	issue.Reporter = fields.Reporter.view()
	if fields.Priority != nil {
		issue.Priority = &PriorityReference{ID: fields.Priority.ID, Name: fields.Priority.Name}
	}
	var err error
	if issue.CreatedAt, err = parseJiraTime(fields.Created); err != nil {
		return Issue{}, fmt.Errorf("issue created time: %w", err)
	}
	if issue.UpdatedAt, err = parseJiraTime(fields.Updated); err != nil {
		return Issue{}, fmt.Errorf("issue updated time: %w", err)
	}
	if includesDescription {
		if issue.Description, issue.IsDescriptionTruncated, err = extractDocumentText(resource.Fields["description"]); err != nil {
			return Issue{}, fmt.Errorf("issue description: %w", err)
		}
	}
	for _, fieldID := range additionalFieldIDs {
		value, exists := resource.Fields[fieldID]
		if !exists {
			continue
		}
		if issue.AdditionalFields == nil {
			issue.AdditionalFields = map[string]json.RawMessage{}
		}
		issue.AdditionalFields[fieldID] = value
	}
	return issue, nil
}

func (status *statusResource) view() IssueStatus {
	view := IssueStatus{ID: status.ID, Name: status.Name}
	if status.StatusCategory != nil {
		view.CategoryKey = StatusCategoryKey(status.StatusCategory.Key)
	}
	return view
}

func (account *accountResource) view() *AccountReference {
	if account == nil || account.AccountID == "" {
		return nil
	}
	return &AccountReference{AccountID: account.AccountID, DisplayName: account.DisplayName}
}

// parseJiraTime accepts Jira's millisecond timestamp with a ±hhmm offset; empty is the zero time.
func parseJiraTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	for _, layout := range jiraTimeLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, errors.New("timestamp is not a Jira date-time")
}

// validateIssueIDOrKey accepts an issue key such as OPS-441 or a numeric issue ID.
func validateIssueIDOrKey(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !issueKeyPattern.MatchString(trimmed) && !numericIDPattern.MatchString(trimmed) {
		return "", errors.New("issueIdOrKey must be an issue key such as OPS-441 or a numeric issue ID")
	}
	return trimmed, nil
}

// validateAdditionalFieldIDs checks requested field IDs and returns the request's complete field list.
func validateAdditionalFieldIDs(additionalFieldIDs []string, includesDescription bool) ([]string, error) {
	if len(additionalFieldIDs) > maximumAdditionalFields {
		return nil, fmt.Errorf("additionalFields accepts at most %d field IDs", maximumAdditionalFields)
	}
	requested := append([]string(nil), standardIssueFieldIDs...)
	if includesDescription {
		requested = append(requested, "description")
	}
	seen := map[string]bool{}
	for _, fieldID := range requested {
		seen[fieldID] = true
	}
	for _, fieldID := range additionalFieldIDs {
		if !jiraFieldIDPattern.MatchString(fieldID) {
			return nil, errors.New("additionalFields must be Jira field IDs such as customfield_10020 or duedate")
		}
		if seen[fieldID] {
			continue
		}
		seen[fieldID] = true
		requested = append(requested, fieldID)
	}
	return requested, nil
}

// validateSingleLineText checks a bounded single-line value such as a summary or a name.
func validateSingleLineText(value string, fieldName string, maximumCharacters int) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "":
		return "", errors.New(fieldName + " is required")
	case !utf8.ValidString(trimmed):
		return "", errors.New(fieldName + " must be valid UTF-8")
	case utf8.RuneCountInString(trimmed) > maximumCharacters:
		return "", fmt.Errorf("%s cannot exceed %d characters", fieldName, maximumCharacters)
	}
	for _, character := range trimmed {
		if character < ' ' || character == 0x7f {
			return "", errors.New(fieldName + " must be one line without control characters")
		}
	}
	return trimmed, nil
}

// validateLabels checks Jira labels, which cannot contain whitespace.
func validateLabels(labels []string) ([]string, error) {
	if len(labels) > 50 {
		return nil, errors.New("labels accepts at most 50 labels")
	}
	validated := make([]string, 0, len(labels))
	for _, label := range labels {
		if label == "" || utf8.RuneCountInString(label) > maximumIdentifierBytes || strings.IndexFunc(label, isWhitespaceOrControl) >= 0 {
			return nil, errors.New("each label must be 1 to 255 characters without whitespace")
		}
		validated = append(validated, label)
	}
	return validated, nil
}

func isWhitespaceOrControl(character rune) bool {
	return character <= ' ' || character == 0x7f || character == 0x85 || character == 0xa0 || character == 0x2028 || character == 0x2029
}
