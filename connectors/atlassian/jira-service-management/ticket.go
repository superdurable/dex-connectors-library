// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maximumIdentifierCharacters = 255
	maximumLabels               = 50
)

var (
	issueKeyPattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-[1-9][0-9]{0,17}$`)
	numericIDPattern  = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)
	projectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,254}$`)
	accountIDPattern  = regexp.MustCompile(`^[A-Za-z0-9:_-]{1,128}$`)
	jiraTimeLayouts   = []string{"2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05-0700", time.RFC3339Nano}
	// ticketFieldIDs are the Jira fields every Ticket carries; getTicket adds description.
	ticketFieldIDs = []string{"summary", "status", "project", "assignee", "reporter", "priority", "labels", "created", "updated"}
)

// StatusCategoryKey is Jira's fixed status category key. Workflow status names vary by service desk;
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

// RequestStatusCategory is Jira Service Management's own status category of a customer request, as its
// request API reports it: NEW, INDETERMINATE, DONE, or UNDEFINED. It names the same three categories as
// StatusCategoryKey in upper case.
type RequestStatusCategory string

const (
	// RequestStatusCategoryNew matches StatusCategoryToDo.
	RequestStatusCategoryNew RequestStatusCategory = "NEW"
	// RequestStatusCategoryInProgress matches StatusCategoryInProgress.
	RequestStatusCategoryInProgress RequestStatusCategory = "INDETERMINATE"
	// RequestStatusCategoryDone matches StatusCategoryDone.
	RequestStatusCategoryDone RequestStatusCategory = "DONE"
	// RequestStatusCategoryUndefined is a status without a category.
	RequestStatusCategoryUndefined RequestStatusCategory = "UNDEFINED"
)

// Ticket is the connector's view of one Jira Service Management customer request, read through the Jira
// issue that holds it. Status and priority names are the site's own values, never mapped to another
// vocabulary, and accounts carry IDs and display names but never email addresses.
type Ticket struct {
	// ID is the numeric issue ID, stable when the request moves between projects.
	ID string `json:"id"`
	// Key is the request key, such as ITH-12; it changes when the request moves.
	Key string `json:"key"`
	// ProjectKey is the key of the service desk's project, such as ITH.
	ProjectKey string `json:"projectKey,omitempty"`
	// Summary is the one-line request title.
	Summary string `json:"summary"`
	// Status is the request's current workflow status.
	Status TicketStatus `json:"status"`
	// Priority is the request priority, or nil when the project does not use priorities.
	Priority *PriorityReference `json:"priority,omitempty"`
	// Assignee is the assigned agent, or nil when unassigned.
	Assignee *AccountReference `json:"assignee,omitempty"`
	// Reporter is the customer the request was raised for, or nil when Jira hides it.
	Reporter *AccountReference `json:"reporter,omitempty"`
	// Labels lists the request's labels, Jira's tags.
	Labels []string `json:"labels,omitempty"`
	// Description is the plain text of the description, returned by getTicket only.
	Description string `json:"description,omitempty"`
	// IsDescriptionTruncated reports that Description stopped at 32767 characters.
	IsDescriptionTruncated bool `json:"isDescriptionTruncated,omitempty"`
	// CreatedAt is when the request was raised.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the request last changed.
	UpdatedAt time.Time `json:"updatedAt"`
}

// TicketStatus is one Jira workflow status.
type TicketStatus struct {
	// ID is the status ID, stable across renames.
	ID string `json:"id"`
	// Name is the site's status name, such as Waiting for support.
	Name string `json:"name"`
	// CategoryKey is the status category: new, indeterminate, or done.
	CategoryKey StatusCategoryKey `json:"categoryKey,omitempty"`
}

// AccountReference identifies an Atlassian account without its email address.
type AccountReference struct {
	// AccountID is the Atlassian account ID, which createTicket accepts as RaiseOnBehalfOfAccountID.
	AccountID string `json:"accountId"`
	// DisplayName is the account's display name, which privacy settings may replace.
	DisplayName string `json:"displayName,omitempty"`
}

// PriorityReference identifies a priority.
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

type ticketFields struct {
	Summary  string            `json:"summary"`
	Status   *statusResource   `json:"status"`
	Project  *projectResource  `json:"project"`
	Assignee *accountResource  `json:"assignee"`
	Reporter *accountResource  `json:"reporter"`
	Priority *priorityResource `json:"priority"`
	Labels   []string          `json:"labels"`
	Created  string            `json:"created"`
	Updated  string            `json:"updated"`
}

type statusResource struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	StatusCategory *struct {
		Key string `json:"key"`
	} `json:"statusCategory"`
}

type projectResource struct {
	Key string `json:"key"`
}

type accountResource struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
}

type priorityResource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// decodeTicket converts one Jira issue resource. includesDescription adds the plain-text description.
func decodeTicket(resource issueResource, includesDescription bool) (Ticket, error) {
	if !numericIDPattern.MatchString(resource.ID) || !issueKeyPattern.MatchString(resource.Key) {
		return Ticket{}, errors.New("issue ID or key is invalid")
	}
	var fields ticketFields
	if resource.Fields != nil {
		encoded, err := json.Marshal(resource.Fields)
		if err != nil {
			return Ticket{}, errors.New("issue fields are invalid")
		}
		if err := json.Unmarshal(encoded, &fields); err != nil {
			return Ticket{}, errors.New("issue fields are invalid")
		}
	}
	ticket := Ticket{ID: resource.ID, Key: resource.Key, Summary: fields.Summary, Labels: fields.Labels}
	if fields.Status != nil {
		ticket.Status = fields.Status.view()
	}
	if fields.Project != nil {
		ticket.ProjectKey = fields.Project.Key
	}
	ticket.Assignee = fields.Assignee.view()
	ticket.Reporter = fields.Reporter.view()
	if fields.Priority != nil {
		ticket.Priority = &PriorityReference{ID: fields.Priority.ID, Name: fields.Priority.Name}
	}
	var err error
	if ticket.CreatedAt, err = parseJiraTime(fields.Created); err != nil {
		return Ticket{}, fmt.Errorf("issue created time: %w", err)
	}
	if ticket.UpdatedAt, err = parseJiraTime(fields.Updated); err != nil {
		return Ticket{}, fmt.Errorf("issue updated time: %w", err)
	}
	if includesDescription {
		if ticket.Description, ticket.IsDescriptionTruncated, err = extractDocumentText(resource.Fields["description"]); err != nil {
			return Ticket{}, fmt.Errorf("issue description: %w", err)
		}
	}
	return ticket, nil
}

func (status *statusResource) view() TicketStatus {
	view := TicketStatus{ID: status.ID, Name: status.Name}
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

func issuePath(issueIDOrKey string) string {
	return "/issue/" + url.PathEscape(issueIDOrKey)
}

func requestPath(issueIDOrKey string) string {
	return "/request/" + url.PathEscape(issueIDOrKey)
}

// validateIssueIDOrKey accepts a request key such as ITH-12 or a numeric issue ID.
func validateIssueIDOrKey(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !issueKeyPattern.MatchString(trimmed) && !numericIDPattern.MatchString(trimmed) {
		return "", errors.New("issueIdOrKey must be a request key such as ITH-12 or a numeric issue ID")
	}
	return trimmed, nil
}

// validateNumericID accepts a positive numeric ID such as a service desk or request type ID.
func validateNumericID(value string, fieldName string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !numericIDPattern.MatchString(trimmed) {
		return "", errors.New(fieldName + " must be a numeric ID such as 10")
	}
	return trimmed, nil
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
func validateLabels(labels []string, fieldName string) ([]string, error) {
	if len(labels) > maximumLabels {
		return nil, fmt.Errorf("%s accepts at most %d labels", fieldName, maximumLabels)
	}
	validated := make([]string, 0, len(labels))
	for _, label := range labels {
		if label == "" || utf8.RuneCountInString(label) > maximumIdentifierCharacters || strings.IndexFunc(label, isWhitespaceOrControl) >= 0 {
			return nil, errors.New("each " + fieldName + " label must be 1 to 255 characters without whitespace")
		}
		validated = append(validated, label)
	}
	return validated, nil
}

func isWhitespaceOrControl(character rune) bool {
	return character <= ' ' || character == 0x7f || character == 0x85 || character == 0xa0 || character == 0x2028 || character == 0x2029
}
