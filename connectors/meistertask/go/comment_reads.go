// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CommentSummary contains non-secret comment identity and attribution.
type CommentSummary struct {
	// ID is the positive comment ID.
	ID string `json:"id"`
	// TaskID is the positive parent task ID.
	TaskID string `json:"taskId"`
	// PersonID is the positive author ID.
	PersonID string `json:"personId"`
}

// CommentsPage contains one page of comment identities.
type CommentsPage struct {
	// Comments are returned in provider order.
	Comments []CommentSummary `json:"comments"`
	// Page is the requested one-based page number.
	Page int `json:"page"`
}

// ListCommentsInput selects a task and one page of comments.
type ListCommentsInput struct {
	// TaskID is the positive parent task ID.
	TaskID string `json:"taskId"`
	// Page is one-based; zero selects the first page.
	Page int `json:"page"`
	// Items is the positive page size; zero uses the provider default.
	Items int `json:"items"`
}

// GetCommentInput selects one comment.
type GetCommentInput struct {
	// CommentID is a positive decimal comment ID.
	CommentID string `json:"commentId"`
}

// ListCommentsOperation reads task comments.
type ListCommentsOperation struct{ client *Client }

// ListComments binds the Query to this client.
func (client *Client) ListComments() ListCommentsOperation { return ListCommentsOperation{client} }

// Definition returns the generated comment-list Query contract.
func (ListCommentsOperation) Definition() sdkgo.QueryDefinition { return ListCommentsDefinition }

// Invoke reads one page of comment identities without persisting comment text.
func (operation ListCommentsOperation) Invoke(call sdkgo.Call, input ListCommentsInput) sdkgo.QueryAttempt[CommentsPage] {
	if !validTaskID(input.TaskID) || input.Page < 0 || input.Items < 0 || input.Items > 100 {
		return commentQueryBranch("listComments", ListCommentsBranchDefect, CommentsPage{}, sdkgo.FailureValidation, "task ID or comment page is invalid")
	}
	page := input.Page
	if page == 0 {
		page = 1
	}
	query := url.Values{"page": {strconv.Itoa(page)}}
	if input.Items > 0 {
		query.Set("items", strconv.Itoa(input.Items))
	}
	response := operation.client.get(call, "/tasks/"+input.TaskID+"/comments?"+query.Encode())
	if decision, handled := classifyCommentQueryResponse("listComments", response, CommentsPage{}, "", ListCommentsBranchProviderRejected, ListCommentsBranchInvalidResponse, ListCommentsBranchDefect); handled {
		return decision
	}
	var wire []json.RawMessage
	if err := json.Unmarshal(response.body, &wire); err != nil || wire == nil {
		return commentQueryBranch("listComments", ListCommentsBranchInvalidResponse, CommentsPage{}, sdkgo.FailureProtocol, "MeisterTask returned invalid comments JSON")
	}
	comments := make([]CommentSummary, 0, len(wire))
	for _, raw := range wire {
		comment, err := decodeComment(raw, "")
		if err != nil || comment.TaskID != input.TaskID {
			return commentQueryBranch("listComments", ListCommentsBranchInvalidResponse, CommentsPage{}, sdkgo.FailureProtocol, "MeisterTask returned incomplete comment metadata")
		}
		comments = append(comments, comment)
	}
	return sdkgo.NewQueryBranch(ListCommentsBranchListed, CommentsPage{Comments: comments, Page: page}, nil, sdkgo.Receipt{})
}

// GetCommentOperation reads one comment's identity.
type GetCommentOperation struct{ client *Client }

// GetComment binds the Query to this client.
func (client *Client) GetComment() GetCommentOperation { return GetCommentOperation{client} }

// Definition returns the generated comment Query contract.
func (GetCommentOperation) Definition() sdkgo.QueryDefinition { return GetCommentDefinition }

// Invoke reads a comment without persisting its text or HTML.
func (operation GetCommentOperation) Invoke(call sdkgo.Call, input GetCommentInput) sdkgo.QueryAttempt[CommentSummary] {
	if !validTaskID(input.CommentID) {
		return commentQueryBranch("getComment", GetCommentBranchDefect, CommentSummary{}, sdkgo.FailureValidation, "comment ID is invalid")
	}
	response := operation.client.get(call, "/comments/"+input.CommentID)
	if decision, handled := classifyCommentQueryResponse("getComment", response, CommentSummary{}, GetCommentBranchNotFound, GetCommentBranchProviderRejected, GetCommentBranchInvalidResponse, GetCommentBranchDefect); handled {
		return decision
	}
	comment, err := decodeComment(response.body, input.CommentID)
	if err != nil {
		return commentQueryBranch("getComment", GetCommentBranchInvalidResponse, CommentSummary{}, sdkgo.FailureProtocol, "MeisterTask returned invalid comment JSON")
	}
	return sdkgo.NewQueryBranch(GetCommentBranchFound, comment, nil, sdkgo.Receipt{})
}

func decodeComment(body []byte, requestedID string) (CommentSummary, error) {
	var wire struct {
		ID       int64 `json:"id"`
		TaskID   int64 `json:"task_id"`
		PersonID int64 `json:"person_id"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CommentSummary{}, err
	}
	id, taskID, personID := strconv.FormatInt(wire.ID, 10), strconv.FormatInt(wire.TaskID, 10), strconv.FormatInt(wire.PersonID, 10)
	if !validTaskID(id) || !validTaskID(taskID) || !validTaskID(personID) || (requestedID != "" && requestedID != id) {
		return CommentSummary{}, errors.New("comment identity or attribution missing")
	}
	return CommentSummary{ID: id, TaskID: taskID, PersonID: personID}, nil
}

func classifyCommentQueryResponse[Output any](operation string, response providerResponse, empty Output, notFound, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[Output], bool) {
	if errors.Is(response.err, errCredential) {
		return commentQueryBranch(operation, defect, empty, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[Output](commentFailure(operation, sdkgo.FailureTransport, "MeisterTask request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[Output](commentFailure(operation, sdkgo.FailureRateLimit, "MeisterTask rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[Output](commentFailure(operation, sdkgo.FailureAvailability, "MeisterTask is temporarily unavailable"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound && notFound != "" {
		return commentQueryBranch(operation, notFound, empty, sdkgo.FailureNotFound, "MeisterTask did not expose the comment"), true
	}
	if response.status != http.StatusOK {
		return commentQueryBranch(operation, rejected, empty, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return commentQueryBranch(operation, invalid, empty, sdkgo.FailureResponseTooLarge, "MeisterTask response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[Output](commentFailure(operation, sdkgo.FailureTransport, "MeisterTask response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[Output]{}, false
}

func commentFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "meistertask", Operation: operation, Message: message}
}
func commentQueryBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Output] {
	failure := commentFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
