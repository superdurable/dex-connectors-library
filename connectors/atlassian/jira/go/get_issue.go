// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	getIssueOperationID    = "getIssue"
	getIssueFailureSubject = "issue"
)

// GetIssueInput identifies one issue.
type GetIssueInput struct {
	// IssueIDOrKey is an issue key such as OPS-441 or a numeric issue ID. Jira also finds a moved
	// issue by its old key and returns its current key.
	IssueIDOrKey string `json:"issueIdOrKey"`
	// AdditionalFields lists up to 20 more field IDs to return raw, such as customfield_10020 or duedate.
	AdditionalFields []string `json:"additionalFields,omitempty"`
}

// GetIssueOperation implements the getIssue Query.
type GetIssueOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetIssueOperation) Definition() sdkgo.QueryDefinition { return GetIssueDefinition }

// Invoke reads one issue. Transport failures, 408, 429, and 5xx responses are retried.
func (operation GetIssueOperation) Invoke(call sdkgo.Call, input GetIssueInput) sdkgo.QueryAttempt[Issue] {
	client := operation.client
	issueIDOrKey, err := validateIssueIDOrKey(input.IssueIDOrKey)
	if err != nil {
		return sdkgo.NewQueryBranch(GetIssueBranchDefect, Issue{}, failurePointer(getIssueOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	fields, err := validateAdditionalFieldIDs(input.AdditionalFields, true)
	if err != nil {
		return sdkgo.NewQueryBranch(GetIssueBranchDefect, Issue{}, failurePointer(getIssueOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, sessionErr := client.startSession(call, getIssueOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewQueryRetry[Issue](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewQueryBranch(GetIssueBranchDefect, Issue{}, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, jiraRequest{
		method: http.MethodGet, path: issuePath(issueIDOrKey), query: url.Values{"fields": {strings.Join(fields, ",")}},
	})
	classification := client.classifyRead(getIssueOperationID, getIssueFailureSubject, result)
	receipt := client.receipt(session, result.response, issueIDOrKey)
	switch classification.outcome {
	case readSucceeded:
	case readRetry:
		return sdkgo.NewQueryRetry[Issue](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewQueryBranch(GetIssueBranchNotFound, Issue{}, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewQueryBranch(GetIssueBranchDefect, Issue{}, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(GetIssueBranchInvalidResponse, Issue{}, &classification.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetIssueBranchProviderRejected, Issue{}, &classification.failure, receipt)
	}
	var resource issueResource
	if err := json.Unmarshal(result.response.body, &resource); err != nil {
		return sdkgo.NewQueryBranch(GetIssueBranchInvalidResponse, Issue{}, failurePointer(getIssueOperationID, sdkgo.FailureProtocol, "Jira returned an invalid issue"), receipt)
	}
	issue, err := decodeIssue(resource, input.AdditionalFields, true)
	if err != nil {
		return sdkgo.NewQueryBranch(GetIssueBranchInvalidResponse, Issue{}, failurePointer(getIssueOperationID, sdkgo.FailureProtocol, "Jira returned an invalid issue: "+err.Error()), receipt)
	}
	receipt.ProviderObjectID = issue.Key
	return sdkgo.NewQueryBranch(GetIssueBranchFound, issue, nil, receipt)
}

func issuePath(issueIDOrKey string) string {
	return "/issue/" + url.PathEscape(issueIDOrKey)
}
