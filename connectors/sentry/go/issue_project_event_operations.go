// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sentry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// UpdateIssueInput sets selected absolute issue fields; nil fields are omitted.
type UpdateIssueInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// ID is the issue ID.
	ID string `json:"id"`
	// Status is resolved, unresolved, ignored, or resolvedInNextRelease.
	Status *string `json:"status,omitempty"`
	// AssignedTo is the user ID, email, or team identifier.
	AssignedTo *string `json:"assignedTo,omitempty"`
	// HasSeen records whether the connected user has viewed the issue.
	HasSeen *bool `json:"hasSeen,omitempty"`
	// IsBookmarked sets the connected user's bookmark state.
	IsBookmarked *bool `json:"isBookmarked,omitempty"`
	// IsPublic sets public issue visibility.
	IsPublic *bool `json:"isPublic,omitempty"`
	// IsSubscribed sets the connected user's subscription state.
	IsSubscribed *bool `json:"isSubscribed,omitempty"`
}

// UpdateProjectInput sets selected absolute project fields; nil fields are omitted.
type UpdateProjectInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// Slug is the current project slug.
	Slug string `json:"slug"`
	// NewSlug is the desired project slug.
	NewSlug *string `json:"newSlug,omitempty"`
	// Name is the desired display name.
	Name *string `json:"name,omitempty"`
	// Team is the desired owning team slug.
	Team *string `json:"team,omitempty"`
	// Platform is the Sentry platform identifier.
	Platform *string `json:"platform,omitempty"`
	// IsBookmarked sets the connected user's bookmark state.
	IsBookmarked *bool `json:"isBookmarked,omitempty"`
	// DigestsMinDelay is the minimum issue digest delay in seconds.
	DigestsMinDelay *int `json:"digestsMinDelay,omitempty"`
	// DigestsMaxDelay is the maximum issue digest delay in seconds.
	DigestsMaxDelay *int `json:"digestsMaxDelay,omitempty"`
}

// ListProjectEventsInput selects one page of project error events.
type ListProjectEventsInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// Project is the project slug.
	Project string `json:"project"`
	// Cursor is the opaque provider pagination cursor.
	Cursor string `json:"cursor,omitempty"`
}

// GetProjectEventInput selects one project event by its provider ID.
type GetProjectEventInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// Project is the project slug.
	Project string `json:"project"`
	// EventID is the 32-character hexadecimal event ID.
	EventID string `json:"eventId"`
}

// UpdateIssueOperation updates an issue.
type UpdateIssueOperation struct{ client *Client }

// UpdateProjectOperation updates a project.
type UpdateProjectOperation struct{ client *Client }

// ListProjectEventsOperation lists a project's error events.
type ListProjectEventsOperation struct{ client *Client }

// GetProjectEventOperation retrieves a project event.
type GetProjectEventOperation struct{ client *Client }

// UpdateIssue binds issue updates to this client.
func (client *Client) UpdateIssue() UpdateIssueOperation { return UpdateIssueOperation{client} }

// UpdateProject binds project updates to this client.
func (client *Client) UpdateProject() UpdateProjectOperation { return UpdateProjectOperation{client} }

// ListProjectEvents binds project event listing to this client.
func (client *Client) ListProjectEvents() ListProjectEventsOperation {
	return ListProjectEventsOperation{client}
}

// GetProjectEvent binds project event retrieval to this client.
func (client *Client) GetProjectEvent() GetProjectEventOperation {
	return GetProjectEventOperation{client}
}

// Definition returns the generated updateIssue contract.
func (UpdateIssueOperation) Definition() sdkgo.MutationDefinition { return UpdateIssueDefinition }

// Definition returns the generated updateProject contract.
func (UpdateProjectOperation) Definition() sdkgo.MutationDefinition { return UpdateProjectDefinition }

// Definition returns the generated listProjectEvents contract.
func (ListProjectEventsOperation) Definition() sdkgo.QueryDefinition {
	return ListProjectEventsDefinition
}

// Definition returns the generated getProjectEvent contract.
func (GetProjectEventOperation) Definition() sdkgo.QueryDefinition { return GetProjectEventDefinition }

// IdempotencyKey correlates absolute issue updates.
func (UpdateIssueOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateIssueInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute project updates.
func (UpdateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets selected absolute issue fields.
func (operation UpdateIssueOperation) Invoke(call sdkgo.Call, input UpdateIssueInput) sdkgo.MutationAttempt[Issue] {
	if !validSentrySegments(input.Organization, input.ID) || (input.Status == nil && input.AssignedTo == nil && input.HasSeen == nil && input.IsBookmarked == nil && input.IsPublic == nil && input.IsSubscribed == nil) {
		return mutationFailure[Issue]("updateIssue", UpdateIssueBranchDefect, sdkgo.FailureValidation, "organization, issue ID, and at least one update field are required")
	}
	if input.Status != nil {
		switch *input.Status {
		case "resolved", "unresolved", "ignored", "resolvedInNextRelease":
		default:
			return mutationFailure[Issue]("updateIssue", UpdateIssueBranchDefect, sdkgo.FailureValidation, "unsupported issue status")
		}
	}
	body, err := json.Marshal(input.issueUpdateBody())
	if err != nil {
		return mutationFailure[Issue]("updateIssue", UpdateIssueBranchDefect, sdkgo.FailureValidation, "issue update cannot be encoded")
	}
	path := "/organizations/" + url.PathEscape(input.Organization) + "/issues/" + url.PathEscape(input.ID) + "/"
	return runMutation(operation.client, call, "updateIssue", http.MethodPut, path, body, "", "", UpdateIssueBranchUpdated, UpdateIssueBranchNotFound, UpdateIssueBranchProviderRejected, UpdateIssueBranchInvalidResponse, UpdateIssueBranchDefect, decodeIssue)
}

// Invoke sets selected absolute project fields.
func (operation UpdateProjectOperation) Invoke(call sdkgo.Call, input UpdateProjectInput) sdkgo.MutationAttempt[Project] {
	if !validSentrySegments(input.Organization, input.Slug) || (input.NewSlug == nil && input.Name == nil && input.Team == nil && input.Platform == nil && input.IsBookmarked == nil && input.DigestsMinDelay == nil && input.DigestsMaxDelay == nil) {
		return mutationFailure[Project]("updateProject", UpdateProjectBranchDefect, sdkgo.FailureValidation, "organization, project slug, and at least one update field are required")
	}
	for _, value := range []*string{input.NewSlug, input.Name, input.Team, input.Platform} {
		if value != nil && strings.TrimSpace(*value) == "" {
			return mutationFailure[Project]("updateProject", UpdateProjectBranchDefect, sdkgo.FailureValidation, "project update strings must be nonempty")
		}
	}
	if (input.DigestsMinDelay != nil && *input.DigestsMinDelay < 0) || (input.DigestsMaxDelay != nil && *input.DigestsMaxDelay < 0) || (input.DigestsMinDelay != nil && input.DigestsMaxDelay != nil && *input.DigestsMinDelay > *input.DigestsMaxDelay) {
		return mutationFailure[Project]("updateProject", UpdateProjectBranchDefect, sdkgo.FailureValidation, "digest delays must be nonnegative and ordered")
	}
	body, err := json.Marshal(input.projectUpdateBody())
	if err != nil {
		return mutationFailure[Project]("updateProject", UpdateProjectBranchDefect, sdkgo.FailureValidation, "project update cannot be encoded")
	}
	path := "/projects/" + url.PathEscape(input.Organization) + "/" + url.PathEscape(input.Slug) + "/"
	return runMutation(operation.client, call, "updateProject", http.MethodPut, path, body, "", "", UpdateProjectBranchUpdated, UpdateProjectBranchNotFound, UpdateProjectBranchProviderRejected, UpdateProjectBranchInvalidResponse, UpdateProjectBranchDefect, decodeCreatedProject)
}

// Invoke lists one page of project error events.
func (operation ListProjectEventsOperation) Invoke(call sdkgo.Call, input ListProjectEventsInput) sdkgo.QueryAttempt[IssueEventPage] {
	if !validSentrySegments(input.Organization, input.Project) {
		return queryFailure[IssueEventPage]("listProjectEvents", ListProjectEventsBranchDefect, sdkgo.FailureValidation, "organization and project are required")
	}
	path := projectEventsPath(input.Organization, input.Project)
	if input.Cursor != "" {
		path += "?cursor=" + url.QueryEscape(input.Cursor)
	}
	return runQuery(operation.client, call, "listProjectEvents", path, ListProjectEventsBranchListed, "", ListProjectEventsBranchProviderRejected, ListProjectEventsBranchInvalidResponse, ListProjectEventsBranchDefect, decodeProjectEventPage)
}

// Invoke retrieves one project event.
func (operation GetProjectEventOperation) Invoke(call sdkgo.Call, input GetProjectEventInput) sdkgo.QueryAttempt[IssueEvent] {
	if !validSentrySegments(input.Organization, input.Project) || len(input.EventID) != 32 || !isHexadecimal(input.EventID) {
		return queryFailure[IssueEvent]("getProjectEvent", GetProjectEventBranchDefect, sdkgo.FailureValidation, "organization, project, and 32-character hexadecimal event ID are required")
	}
	path := projectEventsPath(input.Organization, input.Project) + url.PathEscape(input.EventID) + "/"
	return runQuery(operation.client, call, "getProjectEvent", path, GetProjectEventBranchFound, GetProjectEventBranchNotFound, GetProjectEventBranchProviderRejected, GetProjectEventBranchInvalidResponse, GetProjectEventBranchDefect, decodeProjectEvent)
}

func (input UpdateIssueInput) issueUpdateBody() map[string]any {
	fields := make(map[string]any)
	if input.Status != nil {
		fields["status"] = *input.Status
	}
	if input.AssignedTo != nil {
		fields["assignedTo"] = *input.AssignedTo
	}
	if input.HasSeen != nil {
		fields["hasSeen"] = *input.HasSeen
	}
	if input.IsBookmarked != nil {
		fields["isBookmarked"] = *input.IsBookmarked
	}
	if input.IsPublic != nil {
		fields["isPublic"] = *input.IsPublic
	}
	if input.IsSubscribed != nil {
		fields["isSubscribed"] = *input.IsSubscribed
	}
	return fields
}

func (input UpdateProjectInput) projectUpdateBody() map[string]any {
	fields := make(map[string]any)
	if input.NewSlug != nil {
		fields["slug"] = *input.NewSlug
	}
	if input.Name != nil {
		fields["name"] = *input.Name
	}
	if input.Team != nil {
		fields["team"] = *input.Team
	}
	if input.Platform != nil {
		fields["platform"] = *input.Platform
	}
	if input.IsBookmarked != nil {
		fields["isBookmarked"] = *input.IsBookmarked
	}
	if input.DigestsMinDelay != nil {
		fields["digestsMinDelay"] = *input.DigestsMinDelay
	}
	if input.DigestsMaxDelay != nil {
		fields["digestsMaxDelay"] = *input.DigestsMaxDelay
	}
	return fields
}

func decodeProjectEventPage(body []byte) (IssueEventPage, error) {
	events, err := decodeSentryList[IssueEvent](body)
	if err != nil {
		return IssueEventPage{}, err
	}
	for _, event := range events {
		if event.ID == "" {
			return IssueEventPage{}, fmt.Errorf("event page contains a missing ID")
		}
	}
	return IssueEventPage{Events: events}, nil
}

func decodeProjectEvent(body []byte) (IssueEvent, error) {
	var event IssueEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return IssueEvent{}, err
	}
	if event.ID == "" {
		return IssueEvent{}, fmt.Errorf("event ID missing")
	}
	return event, nil
}

func projectEventsPath(organization, project string) string {
	return "/projects/" + url.PathEscape(organization) + "/" + url.PathEscape(project) + "/events/"
}

func isHexadecimal(value string) bool {
	for _, character := range value {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') && !(character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}
