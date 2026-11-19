// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitlab

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateIssueInput supplies the project, title, and optional description.
type CreateIssueInput struct {
	// ProjectID is a numeric project ID or namespace path.
	ProjectID string `json:"projectId"`
	// Title is the nonblank issue title.
	Title string `json:"title"`
	// Description is optional Markdown content.
	Description string `json:"description,omitempty"`
}

// CreateIssueCommentInput supplies the issue and comment body.
type CreateIssueCommentInput struct {
	// ProjectID is a numeric project ID or namespace path.
	ProjectID string `json:"projectId"`
	// IssueIID is the project-local issue number.
	IssueIID int64 `json:"issueIid"`
	// Body is the nonblank comment content.
	Body string `json:"body"`
}

// IssueComment is the identity and content of a GitLab issue note.
type IssueComment struct {
	// ID is the global note ID.
	ID int64 `json:"id"`
	// Body is GitLab's stored comment content.
	Body string `json:"body"`
}

// GetIssueInput selects an issue by project and project-local IID.
type GetIssueInput struct {
	// ProjectID is a numeric project ID or namespace path.
	ProjectID string `json:"projectId"`
	// IssueIID is the positive project-local issue number.
	IssueIID int64 `json:"issueIid"`
}

// EditIssueInput supplies absolute issue fields to replace.
type EditIssueInput struct {
	// ProjectID is a numeric project ID or namespace path.
	ProjectID string `json:"projectId"`
	// IssueIID is the positive project-local issue number.
	IssueIID int64 `json:"issueIid"`
	// Title replaces the title when present.
	Title *string `json:"title,omitempty"`
	// Description replaces Markdown content when present, including empty to clear it.
	Description *string `json:"description,omitempty"`
	// StateEvent is close or reopen; blank leaves state unchanged.
	StateEvent string `json:"stateEvent,omitempty"`
}

// DeleteIssueOutput identifies the issue removed by the API.
type DeleteIssueOutput struct {
	// ProjectID is the original project identifier.
	ProjectID string `json:"projectId"`
	// IssueIID is the removed project-local issue number.
	IssueIID int64 `json:"issueIid"`
}

// GetIssueOperation reads one issue.
type GetIssueOperation struct{ client *Client }

// EditIssueOperation replaces selected issue fields.
type EditIssueOperation struct{ client *Client }

// LockIssueOperation locks issue discussion.
type LockIssueOperation struct{ client *Client }

// DeleteIssueOperation deletes an issue.
type DeleteIssueOperation struct{ client *Client }

// GetIssue binds the get Query to this client.
func (client *Client) GetIssue() GetIssueOperation { return GetIssueOperation{client} }

// EditIssue binds the edit Mutation to this client.
func (client *Client) EditIssue() EditIssueOperation { return EditIssueOperation{client} }

// LockIssue binds the lock Mutation to this client.
func (client *Client) LockIssue() LockIssueOperation { return LockIssueOperation{client} }

// DeleteIssue binds the delete Mutation to this client.
func (client *Client) DeleteIssue() DeleteIssueOperation { return DeleteIssueOperation{client} }

// Definition returns the generated getIssue contract.
func (GetIssueOperation) Definition() sdkgo.QueryDefinition { return GetIssueDefinition }

// Definition returns the generated editIssue contract.
func (EditIssueOperation) Definition() sdkgo.MutationDefinition { return EditIssueDefinition }

// Definition returns the generated lockIssue contract.
func (LockIssueOperation) Definition() sdkgo.MutationDefinition { return LockIssueDefinition }

// Definition returns the generated deleteIssue contract.
func (DeleteIssueOperation) Definition() sdkgo.MutationDefinition { return DeleteIssueDefinition }

// IdempotencyKey uses the call ID for an absolute update.
func (EditIssueOperation) IdempotencyKey(callID sdkgo.CallID, _ EditIssueInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey uses the call ID for a repeatable lock.
func (LockIssueOperation) IdempotencyKey(callID sdkgo.CallID, _ GetIssueInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey uses the call ID for a repeatable deletion.
func (DeleteIssueOperation) IdempotencyKey(callID sdkgo.CallID, _ GetIssueInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one issue and validates its identity.
func (operation GetIssueOperation) Invoke(call sdkgo.Call, input GetIssueInput) sdkgo.QueryAttempt[Issue] {
	path, err := issuePath(input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetIssueBranchDefect, Issue{}, failure(sdkgo.FailureValidation, "getIssue", err.Error()), sdkgo.Receipt{})
	}
	return executeQuery(operation.client, call, "getIssue", requestSpec{method: http.MethodGet, path: path}, GetIssueBranchFound, GetIssueBranchNotFound, GetIssueBranchProviderRejected, GetIssueBranchInvalidResponse, GetIssueBranchDefect, decodeIssue)
}

// Invoke replaces the supplied issue fields with absolute values.
func (operation EditIssueOperation) Invoke(call sdkgo.Call, input EditIssueInput) sdkgo.MutationAttempt[Issue] {
	path, err := issuePath(GetIssueInput{ProjectID: input.ProjectID, IssueIID: input.IssueIID})
	if err != nil || (input.Title == nil && input.Description == nil && input.StateEvent == "") || (input.Title != nil && strings.TrimSpace(*input.Title) == "") || (input.StateEvent != "" && input.StateEvent != "close" && input.StateEvent != "reopen") {
		return sdkgo.NewMutationBranch(EditIssueBranchDefect, Issue{}, failure(sdkgo.FailureValidation, "editIssue", "project, issue IID, and valid fields are required"), sdkgo.Receipt{})
	}
	form := url.Values{}
	if input.Title != nil {
		form.Set("title", *input.Title)
	}
	if input.Description != nil {
		form.Set("description", *input.Description)
	}
	if input.StateEvent != "" {
		form.Set("state_event", input.StateEvent)
	}
	spec := requestSpec{method: http.MethodPut, path: path, body: []byte(form.Encode()), contentType: "application/x-www-form-urlencoded"}
	return executeMutation(operation.client, call, "editIssue", spec, EditIssueBranchUpdated, EditIssueBranchNotFound, EditIssueBranchProviderRejected, EditIssueBranchInvalidResponse, EditIssueBranchDefect, decodeIssue)
}

// Invoke locks discussion on an issue.
func (operation LockIssueOperation) Invoke(call sdkgo.Call, input GetIssueInput) sdkgo.MutationAttempt[Issue] {
	path, err := issuePath(input)
	if err != nil {
		return sdkgo.NewMutationBranch(LockIssueBranchDefect, Issue{}, failure(sdkgo.FailureValidation, "lockIssue", err.Error()), sdkgo.Receipt{})
	}
	return executeMutation(operation.client, call, "lockIssue", requestSpec{method: http.MethodPut, path: path, body: []byte("discussion_locked=true"), contentType: "application/x-www-form-urlencoded"}, LockIssueBranchLocked, LockIssueBranchNotFound, LockIssueBranchProviderRejected, LockIssueBranchInvalidResponse, LockIssueBranchDefect, decodeIssue)
}

// Invoke deletes an issue by IID.
func (operation DeleteIssueOperation) Invoke(call sdkgo.Call, input GetIssueInput) sdkgo.MutationAttempt[DeleteIssueOutput] {
	path, err := issuePath(input)
	if err != nil {
		return sdkgo.NewMutationBranch(DeleteIssueBranchDefect, DeleteIssueOutput{}, failure(sdkgo.FailureValidation, "deleteIssue", err.Error()), sdkgo.Receipt{})
	}
	response, localFailure, err := operation.client.send(call, "deleteIssue", requestSpec{method: http.MethodDelete, path: path})
	if localFailure != nil {
		return sdkgo.NewMutationBranch(DeleteIssueBranchDefect, DeleteIssueOutput{}, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[DeleteIssueOutput](sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "gitlab", Operation: "deleteIssue", Message: "GitLab delete request failed"}, 0)
	}
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		retry, after := retryFailure(response, "deleteIssue")
		return sdkgo.NewMutationRetry[DeleteIssueOutput](retry, after)
	}
	if response.StatusCode == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteIssueBranchNotFound, DeleteIssueOutput{}, failure(sdkgo.FailureNotFound, "deleteIssue", "issue not found"), sdkgo.Receipt{})
	}
	if response.StatusCode != http.StatusNoContent {
		return sdkgo.NewMutationBranch(DeleteIssueBranchProviderRejected, DeleteIssueOutput{}, failure(sdkgo.FailureProviderRejection, "deleteIssue", "GitLab rejected HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(DeleteIssueBranchDeleted, DeleteIssueOutput{ProjectID: input.ProjectID, IssueIID: input.IssueIID}, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "gitlab"})
}

func issuePath(input GetIssueInput) (string, error) {
	if strings.TrimSpace(input.ProjectID) == "" || input.IssueIID <= 0 {
		return "", fmt.Errorf("project ID and positive issue IID are required")
	}
	return "/projects/" + url.PathEscape(input.ProjectID) + "/issues/" + strconv.FormatInt(input.IssueIID, 10), nil
}

func decodeIssue(body []byte) (Issue, error) {
	var value Issue
	if err := json.Unmarshal(body, &value); err != nil {
		return Issue{}, err
	}
	if value.ID <= 0 || value.IID <= 0 {
		return Issue{}, fmt.Errorf("missing issue identity")
	}
	return value, nil
}
