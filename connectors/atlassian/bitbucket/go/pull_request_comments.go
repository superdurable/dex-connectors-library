// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// PullRequestCommentContent is the Markdown source of a Bitbucket comment.
type PullRequestCommentContent struct {
	// Raw is the Markdown text.
	Raw string `json:"raw"`
}

// PullRequestComment is one repository-local pull request comment.
type PullRequestComment struct {
	// ID is the positive comment ID.
	ID int64 `json:"id"`
	// Content contains the provider's Markdown source.
	Content PullRequestCommentContent `json:"content"`
	// Deleted reports whether Bitbucket has deleted the comment.
	Deleted bool `json:"deleted"`
	// Resolution is present when the comment thread is resolved.
	Resolution *PullRequestCommentResolution `json:"resolution,omitempty"`
}

// PullRequestCommentResolution identifies a resolved comment state.
type PullRequestCommentResolution struct {
	// CreatedOn is the provider timestamp when the resolution was created.
	CreatedOn string `json:"created_on"`
}

// PullRequestCommentPage is one provider page of comments.
type PullRequestCommentPage struct {
	// Values contains the returned comments.
	Values []PullRequestComment `json:"values"`
	// Next is an untrusted provider pagination URL.
	Next string `json:"next"`
	// Page is the one-based page number.
	Page int `json:"page"`
	// PageLength is the provider page length.
	PageLength int `json:"pagelen"`
}

// PullRequestCommentIDInput identifies a comment in one pull request.
type PullRequestCommentIDInput struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug.
	Repository string `json:"repository"`
	// PullRequestID is the repository-local pull request ID.
	PullRequestID int64 `json:"pullRequestId"`
	// CommentID is the positive comment ID.
	CommentID int64 `json:"commentId"`
}

// ListPullRequestCommentsOperation reads one page of pull request comments.
type ListPullRequestCommentsOperation struct{ client *Client }

// ListPullRequestComments binds the Query to this client.
func (client *Client) ListPullRequestComments() ListPullRequestCommentsOperation {
	return ListPullRequestCommentsOperation{client}
}

// Definition returns the manifest-generated Query definition.
func (ListPullRequestCommentsOperation) Definition() sdkgo.QueryDefinition {
	return ListPullRequestCommentsDefinition
}

// Invoke lists comments for one pull request.
func (operation ListPullRequestCommentsOperation) Invoke(call sdkgo.Call, input PullRequestActionInput) sdkgo.QueryAttempt[PullRequestCommentPage] {
	if !validPullRequestActionInput(input) {
		return failedAttempt[PullRequestCommentPage]("listPullRequestComments", ListPullRequestCommentsBranchDefect, sdkgo.FailureValidation, "workspace, repository, or pull request ID is invalid")
	}
	return executeRead(operation.client, call, "listPullRequestComments", pullRequestCommentsPath(input), nil, ListPullRequestCommentsBranchListed, ListPullRequestCommentsBranchNotFound, ListPullRequestCommentsBranchProviderRejected, ListPullRequestCommentsBranchInvalidResponse, ListPullRequestCommentsBranchDefect, decodePullRequestCommentPage)
}

// GetPullRequestCommentOperation reads one pull request comment.
type GetPullRequestCommentOperation struct{ client *Client }

// GetPullRequestComment binds the Query to this client.
func (client *Client) GetPullRequestComment() GetPullRequestCommentOperation {
	return GetPullRequestCommentOperation{client}
}

// Definition returns the manifest-generated Query definition.
func (GetPullRequestCommentOperation) Definition() sdkgo.QueryDefinition {
	return GetPullRequestCommentDefinition
}

// Invoke reads a comment by its repository-local ID.
func (operation GetPullRequestCommentOperation) Invoke(call sdkgo.Call, input PullRequestCommentIDInput) sdkgo.QueryAttempt[PullRequestComment] {
	if !validPullRequestCommentIDInput(input) {
		return failedAttempt[PullRequestComment]("getPullRequestComment", GetPullRequestCommentBranchDefect, sdkgo.FailureValidation, "workspace, repository, pull request ID, or comment ID is invalid")
	}
	return executeRead(operation.client, call, "getPullRequestComment", pullRequestCommentPath(input), nil, GetPullRequestCommentBranchFound, GetPullRequestCommentBranchNotFound, GetPullRequestCommentBranchProviderRejected, GetPullRequestCommentBranchInvalidResponse, GetPullRequestCommentBranchDefect, decodePullRequestComment)
}

func decodePullRequestComment(body []byte) (PullRequestComment, error) {
	var comment PullRequestComment
	if err := json.Unmarshal(body, &comment); err != nil {
		return PullRequestComment{}, err
	}
	if comment.ID <= 0 {
		return PullRequestComment{}, errors.New("comment ID is missing")
	}
	return comment, nil
}

func decodePullRequestCommentPage(body []byte) (PullRequestCommentPage, error) {
	var page PullRequestCommentPage
	if err := json.Unmarshal(body, &page); err != nil {
		return PullRequestCommentPage{}, err
	}
	if page.Values == nil {
		return PullRequestCommentPage{}, errors.New("comment values are missing")
	}
	for _, comment := range page.Values {
		if comment.ID <= 0 {
			return PullRequestCommentPage{}, errors.New("comment ID is missing")
		}
	}
	return page, nil
}

func validPullRequestActionInput(input PullRequestActionInput) bool {
	return validPathSegment(input.Workspace) && validPathSegment(input.Repository) && input.PullRequestID > 0
}

func validPullRequestCommentIDInput(input PullRequestCommentIDInput) bool {
	return validPullRequestActionInput(PullRequestActionInput{Workspace: input.Workspace, Repository: input.Repository, PullRequestID: input.PullRequestID}) && input.CommentID > 0
}

func pullRequestCommentsPath(input PullRequestActionInput) string {
	return pullRequestPath(input.Workspace, input.Repository, input.PullRequestID) + "/comments"
}

func pullRequestCommentPath(input PullRequestCommentIDInput) string {
	return pullRequestCommentsPath(PullRequestActionInput{Workspace: input.Workspace, Repository: input.Repository, PullRequestID: input.PullRequestID}) + "/" + strconv.FormatInt(input.CommentID, 10)
}
