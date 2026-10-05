// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxDescriptionCharacters bounds the description an Issue carries; a longer one is cut on a character
	// boundary and flagged IsDescriptionTruncated.
	MaxDescriptionCharacters = 32768

	// issueSummaryFields selects scalar lists instead of nested connections, so a 100-issue page stays far
	// below Linear's 10,000-point query complexity limit.
	issueSummaryFields = `id identifier title url priority dueDate labelIds createdAt updatedAt archivedAt
      team { id key name } state { id name type } assignee { id }`
	issueFields = `id identifier number title url priority priorityLabel estimate dueDate labelIds branchName trashed
      description createdAt updatedAt archivedAt startedAt completedAt canceledAt
      team { id key name } state { id name type } assignee { id name displayName } creator { id name displayName }
      labels(first: 50) { nodes { id name color } } project { id name } cycle { id number name } parent { id identifier }`
)

var errIssueMalformed = errors.New("Linear returned a malformed issue")

// WorkflowStateType is Linear's category of a team's workflow state. Linear's own value is passed through
// unchanged, so a type Linear adds later reaches the application as returned.
type WorkflowStateType string

const (
	// WorkflowStateTypeTriage is an issue waiting in a team's Triage inbox.
	WorkflowStateTypeTriage WorkflowStateType = "triage"
	// WorkflowStateTypeBacklog is an issue in the backlog, such as the Backlog state.
	WorkflowStateTypeBacklog WorkflowStateType = "backlog"
	// WorkflowStateTypeUnstarted is planned work not yet started, such as the Todo state.
	WorkflowStateTypeUnstarted WorkflowStateType = "unstarted"
	// WorkflowStateTypeStarted is work in progress, such as the In Progress or In Review states.
	WorkflowStateTypeStarted WorkflowStateType = "started"
	// WorkflowStateTypeCompleted is finished work, such as the Done state.
	WorkflowStateTypeCompleted WorkflowStateType = "completed"
	// WorkflowStateTypeCanceled is work that will not be done, such as the Canceled state.
	WorkflowStateTypeCanceled WorkflowStateType = "canceled"
	// WorkflowStateTypeDuplicate is an issue closed as a duplicate of another.
	WorkflowStateTypeDuplicate WorkflowStateType = "duplicate"
)

// workflowStateTypeOrder orders Linear's state types from intake to closure.
var workflowStateTypeOrder = map[WorkflowStateType]int{
	WorkflowStateTypeTriage: 1, WorkflowStateTypeBacklog: 2, WorkflowStateTypeUnstarted: 3, WorkflowStateTypeStarted: 4,
	WorkflowStateTypeCompleted: 5, WorkflowStateTypeCanceled: 6, WorkflowStateTypeDuplicate: 7,
}

// IsKnown reports whether the type is one of the workflow state types Linear documents.
func (stateType WorkflowStateType) IsKnown() bool {
	return workflowStateTypeOrder[stateType] != 0
}

// IsClosed reports whether the type ends an issue's workflow: completed, canceled, or duplicate.
func (stateType WorkflowStateType) IsClosed() bool {
	return stateType == WorkflowStateTypeCompleted || stateType == WorkflowStateTypeCanceled || stateType == WorkflowStateTypeDuplicate
}

// TeamReference identifies the team that owns an issue or workflow state.
type TeamReference struct {
	// ID is the team's UUID.
	ID string `json:"id"`
	// Key is the team's identifier prefix, such as ENG in ENG-123.
	Key string `json:"key"`
	// Name is the team's display name.
	Name string `json:"name"`
}

// WorkflowStateReference identifies an issue's workflow state, Linear's issue status.
type WorkflowStateReference struct {
	// ID is the workflow state's UUID.
	ID string `json:"id"`
	// Name is the team's own name for the state, such as In Progress.
	Name string `json:"name"`
	// Type is the state's category, such as started.
	Type WorkflowStateType `json:"type"`
}

// UserReference identifies a Linear user without the user's email address.
type UserReference struct {
	// ID is the user's UUID.
	ID string `json:"id"`
	// Name is the user's full name.
	Name string `json:"name,omitempty"`
	// DisplayName is the user's short display name.
	DisplayName string `json:"displayName,omitempty"`
}

// IssueLabel is one label applied to an issue.
type IssueLabel struct {
	// ID is the label's UUID.
	ID string `json:"id"`
	// Name is the label's name, such as Bug.
	Name string `json:"name"`
	// Color is the label's hex color, such as #eb5757.
	Color string `json:"color,omitempty"`
}

// ProjectReference identifies the project an issue belongs to.
type ProjectReference struct {
	// ID is the project's UUID.
	ID string `json:"id"`
	// Name is the project's name.
	Name string `json:"name"`
}

// CycleReference identifies the cycle an issue is planned in.
type CycleReference struct {
	// ID is the cycle's UUID.
	ID string `json:"id"`
	// Number is the cycle's number within its team.
	Number int `json:"number"`
	// Name is the cycle's optional name.
	Name string `json:"name,omitempty"`
}

// IssueReference identifies another issue, such as a parent.
type IssueReference struct {
	// ID is the issue's UUID.
	ID string `json:"id"`
	// Identifier is the issue's team-scoped identifier, such as ENG-123.
	Identifier string `json:"identifier"`
}

// IssueSummary is the partial issue that searches and writes return.
type IssueSummary struct {
	// ID is the issue's UUID, which never changes.
	ID string `json:"id"`
	// Identifier is the team key and number, such as ENG-123; it changes when the issue moves to another team.
	Identifier string `json:"identifier"`
	// Title is the issue's title.
	Title string `json:"title"`
	// URL is the issue's page in Linear.
	URL string `json:"url"`
	// Team is the team that owns the issue.
	Team TeamReference `json:"team"`
	// State is the issue's current workflow state.
	State WorkflowStateReference `json:"state"`
	// AssigneeID is the assigned user's UUID, or blank when the issue is unassigned.
	AssigneeID string `json:"assigneeId,omitempty"`
	// Priority is 0 for no priority, 1 urgent, 2 high, 3 medium, or 4 low.
	Priority int `json:"priority"`
	// LabelIDs are the UUIDs of the issue's labels.
	LabelIDs []string `json:"labelIds,omitempty"`
	// DueDate is the YYYY-MM-DD due date, or blank.
	DueDate string `json:"dueDate,omitempty"`
	// CreatedAt is when the issue was created.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the issue last changed; searches compare UpdatedAfter with it.
	UpdatedAt time.Time `json:"updatedAt"`
	// ArchivedAt is when the issue was archived, or nil.
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
}

// Issue is one issue as getIssue reads it: the summary fields plus its description and relations.
type Issue struct {
	IssueSummary
	// Number is the issue's number within its team, such as 123 in ENG-123.
	Number int `json:"number"`
	// PriorityLabel is Linear's name for Priority, such as High.
	PriorityLabel string `json:"priorityLabel,omitempty"`
	// Estimate is the issue's estimate in the team's points, or nil.
	Estimate *float64 `json:"estimate,omitempty"`
	// Description is the issue's Markdown description, at most MaxDescriptionCharacters characters.
	Description string `json:"description,omitempty"`
	// IsDescriptionTruncated reports that Description was cut at MaxDescriptionCharacters.
	IsDescriptionTruncated bool `json:"descriptionTruncated,omitempty"`
	// BranchName is the Git branch name Linear suggests for the issue.
	BranchName string `json:"branchName,omitempty"`
	// IsTrashed reports that the issue is in the trash.
	IsTrashed bool `json:"trashed,omitempty"`
	// Assignee is the assigned user, or nil.
	Assignee *UserReference `json:"assignee,omitempty"`
	// Creator is the user who created the issue, or nil for an integration.
	Creator *UserReference `json:"creator,omitempty"`
	// Labels are the issue's labels, at most 50.
	Labels []IssueLabel `json:"labels,omitempty"`
	// Project is the issue's project, or nil.
	Project *ProjectReference `json:"project,omitempty"`
	// Cycle is the issue's cycle, or nil.
	Cycle *CycleReference `json:"cycle,omitempty"`
	// Parent is the parent issue of a sub-issue, or nil.
	Parent *IssueReference `json:"parent,omitempty"`
	// StartedAt is when the issue entered a started state, or nil.
	StartedAt *time.Time `json:"startedAt,omitempty"`
	// CompletedAt is when the issue was completed, or nil.
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	// CanceledAt is when the issue was canceled, or nil.
	CanceledAt *time.Time `json:"canceledAt,omitempty"`
}

type idNameWire struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type userReferenceWire struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type issueWire struct {
	ID            string             `json:"id"`
	Identifier    string             `json:"identifier"`
	Number        float64            `json:"number"`
	Title         string             `json:"title"`
	URL           string             `json:"url"`
	Priority      float64            `json:"priority"`
	PriorityLabel string             `json:"priorityLabel"`
	Estimate      *float64           `json:"estimate"`
	DueDate       *string            `json:"dueDate"`
	LabelIDs      []string           `json:"labelIds"`
	BranchName    string             `json:"branchName"`
	Trashed       *bool              `json:"trashed"`
	Description   *string            `json:"description"`
	CreatedAt     string             `json:"createdAt"`
	UpdatedAt     string             `json:"updatedAt"`
	ArchivedAt    *string            `json:"archivedAt"`
	StartedAt     *string            `json:"startedAt"`
	CompletedAt   *string            `json:"completedAt"`
	CanceledAt    *string            `json:"canceledAt"`
	Team          *idNameWire        `json:"team"`
	State         *idNameWire        `json:"state"`
	Assignee      *userReferenceWire `json:"assignee"`
	Creator       *userReferenceWire `json:"creator"`
	Labels        *struct {
		Nodes []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"nodes"`
	} `json:"labels"`
	Project *idNameWire `json:"project"`
	Cycle   *struct {
		ID     string  `json:"id"`
		Number float64 `json:"number"`
		Name   *string `json:"name"`
	} `json:"cycle"`
	Parent *struct {
		ID         string `json:"id"`
		Identifier string `json:"identifier"`
	} `json:"parent"`
}

// decodeIssueSummary validates the fields every issue selection carries.
func decodeIssueSummary(wire issueWire) (IssueSummary, error) {
	if !isLinearUUID(wire.ID) || strings.TrimSpace(wire.Identifier) == "" || wire.Team == nil || wire.State == nil ||
		!isLinearUUID(wire.Team.ID) || !isLinearUUID(wire.State.ID) || wire.Priority < 0 || wire.Priority > 4 {
		return IssueSummary{}, errIssueMalformed
	}
	createdAt, createdErr := parseLinearTime(wire.CreatedAt)
	updatedAt, updatedErr := parseLinearTime(wire.UpdatedAt)
	archivedAt, archivedErr := parseOptionalLinearTime(wire.ArchivedAt)
	if createdErr != nil || updatedErr != nil || archivedErr != nil {
		return IssueSummary{}, errIssueMalformed
	}
	summary := IssueSummary{
		ID: wire.ID, Identifier: wire.Identifier, Title: wire.Title, URL: wire.URL,
		Team:     TeamReference{ID: wire.Team.ID, Key: wire.Team.Key, Name: wire.Team.Name},
		State:    WorkflowStateReference{ID: wire.State.ID, Name: wire.State.Name, Type: WorkflowStateType(wire.State.Type)},
		Priority: int(wire.Priority), LabelIDs: wire.LabelIDs, CreatedAt: createdAt, UpdatedAt: updatedAt, ArchivedAt: archivedAt,
	}
	if wire.Assignee != nil {
		summary.AssigneeID = wire.Assignee.ID
	}
	if wire.DueDate != nil {
		summary.DueDate = *wire.DueDate
	}
	return summary, nil
}

// decodeIssue adds the description and relations that getIssue selects.
func decodeIssue(wire issueWire) (Issue, error) {
	summary, err := decodeIssueSummary(wire)
	if err != nil {
		return Issue{}, err
	}
	startedAt, startedErr := parseOptionalLinearTime(wire.StartedAt)
	completedAt, completedErr := parseOptionalLinearTime(wire.CompletedAt)
	canceledAt, canceledErr := parseOptionalLinearTime(wire.CanceledAt)
	if startedErr != nil || completedErr != nil || canceledErr != nil || wire.Number < 0 || wire.Number > math.MaxInt32 {
		return Issue{}, errIssueMalformed
	}
	issue := Issue{
		IssueSummary: summary, Number: int(wire.Number), PriorityLabel: wire.PriorityLabel, Estimate: wire.Estimate,
		BranchName: wire.BranchName, IsTrashed: wire.Trashed != nil && *wire.Trashed,
		Assignee: decodeUserReference(wire.Assignee), Creator: decodeUserReference(wire.Creator),
		StartedAt: startedAt, CompletedAt: completedAt, CanceledAt: canceledAt,
	}
	if wire.Description != nil {
		issue.Description, issue.IsDescriptionTruncated = truncateCharacters(*wire.Description, MaxDescriptionCharacters)
	}
	if wire.Labels != nil {
		for _, label := range wire.Labels.Nodes {
			issue.Labels = append(issue.Labels, IssueLabel{ID: label.ID, Name: label.Name, Color: label.Color})
		}
	}
	if wire.Project != nil {
		issue.Project = &ProjectReference{ID: wire.Project.ID, Name: wire.Project.Name}
	}
	if wire.Cycle != nil {
		issue.Cycle = &CycleReference{ID: wire.Cycle.ID, Number: int(wire.Cycle.Number)}
		if wire.Cycle.Name != nil {
			issue.Cycle.Name = *wire.Cycle.Name
		}
	}
	if wire.Parent != nil {
		issue.Parent = &IssueReference{ID: wire.Parent.ID, Identifier: wire.Parent.Identifier}
	}
	return issue, nil
}

func decodeUserReference(wire *userReferenceWire) *UserReference {
	if wire == nil {
		return nil
	}
	return &UserReference{ID: wire.ID, Name: wire.Name, DisplayName: wire.DisplayName}
}

// parseLinearTime reads Linear's ISO 8601 DateTime, such as 2026-10-04T21:10:49.123Z.
func parseLinearTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func parseOptionalLinearTime(value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := parseLinearTime(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

// truncateCharacters cuts value to at most limit characters on a UTF-8 boundary.
func truncateCharacters(value string, limit int) (string, bool) {
	if utf8.RuneCountInString(value) <= limit {
		return value, false
	}
	count := 0
	for index := range value {
		if count == limit {
			return value[:index], true
		}
		count++
	}
	return value, false
}
