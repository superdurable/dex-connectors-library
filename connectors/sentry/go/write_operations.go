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

// AssignIssueInput selects one issue and its desired assignee.
type AssignIssueInput struct {
	// Organization is the Sentry organization slug.
	Organization string `json:"organization"`
	// ID is the issue ID.
	ID string `json:"id"`
	// AssignedTo is a user ID, user email, or team:<team ID>.
	AssignedTo string `json:"assignedTo"`
}

// DeleteIssueInput selects one issue for asynchronous deletion.
type DeleteIssueInput struct {
	// Organization is the Sentry organization slug.
	Organization string `json:"organization"`
	// ID is the issue ID.
	ID string `json:"id"`
}

// RenameProjectInput selects a project and its new display name.
type RenameProjectInput struct {
	// Organization is the Sentry organization slug.
	Organization string `json:"organization"`
	// Slug is the current project slug.
	Slug string `json:"slug"`
	// Name is the desired display name.
	Name string `json:"name"`
}

// DeleteProjectInput selects one project for asynchronous deletion.
type DeleteProjectInput struct {
	// Organization is the Sentry organization slug.
	Organization string `json:"organization"`
	// Slug is the project slug.
	Slug string `json:"slug"`
}

// DeletedIssue records an accepted issue deletion.
type DeletedIssue struct {
	// ID is the queued issue ID.
	ID string `json:"id"`
}

// DeletedProject records an accepted project deletion.
type DeletedProject struct {
	// Slug is the queued project slug.
	Slug string `json:"slug"`
}

// AssignIssueOperation sets an issue's assignee.
type AssignIssueOperation struct{ client *Client }

// DeleteIssueOperation queues an issue for deletion.
type DeleteIssueOperation struct{ client *Client }

// RenameProjectOperation changes a project's display name.
type RenameProjectOperation struct{ client *Client }

// DeleteProjectOperation queues a project for deletion.
type DeleteProjectOperation struct{ client *Client }

// AssignIssue binds assignment to this client.
func (client *Client) AssignIssue() AssignIssueOperation { return AssignIssueOperation{client} }

// DeleteIssue binds issue deletion to this client.
func (client *Client) DeleteIssue() DeleteIssueOperation { return DeleteIssueOperation{client} }

// RenameProject binds project renaming to this client.
func (client *Client) RenameProject() RenameProjectOperation { return RenameProjectOperation{client} }

// DeleteProject binds project deletion to this client.
func (client *Client) DeleteProject() DeleteProjectOperation { return DeleteProjectOperation{client} }

// Definition returns the generated operation contract.
func (AssignIssueOperation) Definition() sdkgo.MutationDefinition { return AssignIssueDefinition }

// Definition returns the generated operation contract.
func (DeleteIssueOperation) Definition() sdkgo.MutationDefinition { return DeleteIssueDefinition }

// Definition returns the generated operation contract.
func (RenameProjectOperation) Definition() sdkgo.MutationDefinition { return RenameProjectDefinition }

// Definition returns the generated operation contract.
func (DeleteProjectOperation) Definition() sdkgo.MutationDefinition { return DeleteProjectDefinition }

// IdempotencyKey derives a stable call key.
func (AssignIssueOperation) IdempotencyKey(callID sdkgo.CallID, _ AssignIssueInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call key.
func (DeleteIssueOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteIssueInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call key.
func (RenameProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ RenameProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call key.
func (DeleteProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces an issue's assignee.
func (operation AssignIssueOperation) Invoke(call sdkgo.Call, input AssignIssueInput) sdkgo.MutationAttempt[Issue] {
	if !validSentrySegments(input.Organization, input.ID, input.AssignedTo) {
		return mutationFailure[Issue]("assignIssue", AssignIssueBranchDefect, sdkgo.FailureValidation, "organization, issue ID, and assignee are required")
	}
	body, err := json.Marshal(struct {
		AssignedTo string `json:"assignedTo"`
	}{input.AssignedTo})
	if err != nil {
		return mutationFailure[Issue]("assignIssue", AssignIssueBranchDefect, sdkgo.FailureValidation, "assignee cannot be encoded")
	}
	return runMutation(operation.client, call, "assignIssue", http.MethodPut, sentryIssuePath(input.Organization, input.ID), body, "", "", AssignIssueBranchAssigned, AssignIssueBranchNotFound, AssignIssueBranchProviderRejected, AssignIssueBranchInvalidResponse, AssignIssueBranchDefect, decodeIssue)
}

// Invoke queues an issue for deletion.
func (operation DeleteIssueOperation) Invoke(call sdkgo.Call, input DeleteIssueInput) sdkgo.MutationAttempt[DeletedIssue] {
	if !validSentrySegments(input.Organization, input.ID) {
		return mutationFailure[DeletedIssue]("deleteIssue", DeleteIssueBranchDefect, sdkgo.FailureValidation, "organization and issue ID are required")
	}
	return runMutation(operation.client, call, "deleteIssue", http.MethodDelete, sentryIssuePath(input.Organization, input.ID), nil, "", "", DeleteIssueBranchDeleted, DeleteIssueBranchNotFound, DeleteIssueBranchProviderRejected, DeleteIssueBranchInvalidResponse, DeleteIssueBranchDefect, func(body []byte) (DeletedIssue, error) {
		if len(body) != 0 {
			return DeletedIssue{}, fmt.Errorf("unexpected deletion content")
		}
		return DeletedIssue{ID: input.ID}, nil
	})
}

// Invoke replaces a project's display name.
func (operation RenameProjectOperation) Invoke(call sdkgo.Call, input RenameProjectInput) sdkgo.MutationAttempt[Project] {
	if !validSentrySegments(input.Organization, input.Slug, input.Name) {
		return mutationFailure[Project]("renameProject", RenameProjectBranchDefect, sdkgo.FailureValidation, "organization, project slug, and name are required")
	}
	body, err := json.Marshal(struct {
		Name string `json:"name"`
	}{input.Name})
	if err != nil {
		return mutationFailure[Project]("renameProject", RenameProjectBranchDefect, sdkgo.FailureValidation, "name cannot be encoded")
	}
	return runMutation(operation.client, call, "renameProject", http.MethodPut, sentryProjectPath(input.Organization, input.Slug), body, "", "", RenameProjectBranchRenamed, RenameProjectBranchNotFound, RenameProjectBranchProviderRejected, RenameProjectBranchInvalidResponse, RenameProjectBranchDefect, func(body []byte) (Project, error) {
		var project Project
		err := json.Unmarshal(body, &project)
		if err == nil && (project.ID == "" || project.Slug == "") {
			err = fmt.Errorf("project ID or slug missing")
		}
		return project, err
	})
}

// Invoke queues a project for deletion.
func (operation DeleteProjectOperation) Invoke(call sdkgo.Call, input DeleteProjectInput) sdkgo.MutationAttempt[DeletedProject] {
	if !validSentrySegments(input.Organization, input.Slug) {
		return mutationFailure[DeletedProject]("deleteProject", DeleteProjectBranchDefect, sdkgo.FailureValidation, "organization and project slug are required")
	}
	return runMutation(operation.client, call, "deleteProject", http.MethodDelete, sentryProjectPath(input.Organization, input.Slug), nil, "", "", DeleteProjectBranchDeleted, DeleteProjectBranchNotFound, DeleteProjectBranchProviderRejected, DeleteProjectBranchInvalidResponse, DeleteProjectBranchDefect, func(body []byte) (DeletedProject, error) {
		if len(body) != 0 {
			return DeletedProject{}, fmt.Errorf("unexpected deletion content")
		}
		return DeletedProject{Slug: input.Slug}, nil
	})
}

func validSentrySegments(segments ...string) bool {
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			return false
		}
	}
	return true
}
func sentryIssuePath(organization, issueID string) string {
	return "/organizations/" + url.PathEscape(organization) + "/issues/" + url.PathEscape(issueID) + "/"
}
func sentryProjectPath(organization, slug string) string {
	return "/projects/" + url.PathEscape(organization) + "/" + url.PathEscape(slug) + "/"
}
