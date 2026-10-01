// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	createIssueOperationID    = "createIssue"
	createIssueFailureSubject = "issue"
	maximumSummaryCharacters  = 255
)

// CreateIssueInput describes one issue. Set exactly one of ProjectKey and ProjectID, and exactly one of
// IssueTypeName and IssueTypeID.
type CreateIssueInput struct {
	// ProjectKey is the project key, such as OPS.
	ProjectKey string `json:"projectKey,omitempty"`
	// ProjectID is the numeric project ID, as the project picker stores it.
	ProjectID string `json:"projectId,omitempty"`
	// IssueTypeName is an issue type available in the project, such as Task or Bug.
	IssueTypeName string `json:"issueTypeName,omitempty"`
	// IssueTypeID is the numeric issue type ID.
	IssueTypeID string `json:"issueTypeId,omitempty"`
	// Summary is the one-line title, at most 255 characters.
	Summary string `json:"summary"`
	// Description is plain text of at most 32767 characters. Blank lines separate paragraphs and other
	// line breaks are kept; the text is never read as markup. Blank sends no description.
	Description string `json:"description,omitempty"`
	// Labels lists labels without whitespace, such as incident or customer-reported.
	Labels []string `json:"labels,omitempty"`
	// AssigneeAccountID assigns the issue to this Atlassian account ID; blank leaves the project default.
	AssigneeAccountID string `json:"assigneeAccountId,omitempty"`
}

// CreateIssueOutput identifies the created issue. On providerRejected and uncertain, IssueID and IssueKey
// are empty and the requested project and summary are echoed so the application can reconcile.
type CreateIssueOutput struct {
	// IssueID is the new issue's numeric ID.
	IssueID string `json:"issueId,omitempty"`
	// IssueKey is the new issue's key, such as OPS-442.
	IssueKey string `json:"issueKey,omitempty"`
	// ProjectKey echoes the requested project key.
	ProjectKey string `json:"projectKey,omitempty"`
	// ProjectID echoes the requested project ID.
	ProjectID string `json:"projectId,omitempty"`
	// Summary echoes the requested summary.
	Summary string `json:"summary"`
	// RejectedFieldIDs lists the field IDs Jira named in a rejection, such as summary or issuetype.
	RejectedFieldIDs []string `json:"rejectedFieldIds,omitempty"`
}

// CreateIssueOperation implements the createIssue Mutation.
type CreateIssueOperation struct{ client *Client }

type createIssueRequestBody struct {
	Fields createIssueFields `json:"fields"`
}

type createIssueFields struct {
	Project     jiraObjectReference  `json:"project"`
	IssueType   jiraObjectReference  `json:"issuetype"`
	Summary     string               `json:"summary"`
	Description *documentNode        `json:"description,omitempty"`
	Labels      []string             `json:"labels,omitempty"`
	Assignee    *jiraAccountSelector `json:"assignee,omitempty"`
}

type jiraObjectReference struct {
	ID   string `json:"id,omitempty"`
	Key  string `json:"key,omitempty"`
	Name string `json:"name,omitempty"`
}

type jiraAccountSelector struct {
	AccountID string `json:"accountId"`
}

type createdIssueResource struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

// Definition returns the immutable connector operation definition.
func (CreateIssueOperation) Definition() sdkgo.MutationDefinition { return CreateIssueDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID.
// Jira accepts no idempotency key, so it is never sent and cannot deduplicate a repeated create.
func (CreateIssueOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateIssueInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one issue and never resends a request Jira may have received.
func (operation CreateIssueOperation) Invoke(call sdkgo.Call, input CreateIssueInput) sdkgo.MutationAttempt[CreateIssueOutput] {
	client := operation.client
	body, requested, err := buildCreateIssueRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateIssueBranchDefect, requested, failurePointer(createIssueOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, sessionErr := client.startSession(call, createIssueOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewMutationRetry[CreateIssueOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewMutationBranch(CreateIssueBranchDefect, requested, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, jiraRequest{method: http.MethodPost, path: "/issue", payload: body})
	classification := client.classifyUnkeyedWrite(createIssueOperationID, createIssueFailureSubject, result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case writeAccepted:
	case writeRetry:
		return sdkgo.NewMutationRetry[CreateIssueOutput](classification.failure, classification.retryAfter)
	case writeDefect:
		return sdkgo.NewMutationBranch(CreateIssueBranchDefect, requested, &classification.failure, receipt)
	case writeUncertain:
		return sdkgo.NewMutationUncertain(requested, classification.failure, receipt)
	default:
		requested.RejectedFieldIDs = classification.rejectedFieldIDs
		return sdkgo.NewMutationBranch(CreateIssueBranchProviderRejected, requested, &classification.failure, receipt)
	}
	var created createdIssueResource
	if err := json.Unmarshal(result.response.body, &created); err != nil || !numericIDPattern.MatchString(created.ID) || !issueKeyPattern.MatchString(created.Key) {
		return sdkgo.NewMutationUncertain(requested, newFailure(createIssueOperationID, sdkgo.FailureProtocol, "Jira accepted the issue but returned an unusable issue reference"), receipt)
	}
	output := requested
	output.IssueID, output.IssueKey = created.ID, created.Key
	receipt.ProviderObjectID = created.Key
	return sdkgo.NewMutationBranch(CreateIssueBranchCreated, output, nil, receipt)
}

// buildCreateIssueRequest validates input and returns the request plus the echo used by every branch.
func buildCreateIssueRequest(input CreateIssueInput) (createIssueRequestBody, CreateIssueOutput, error) {
	requested := CreateIssueOutput{
		ProjectKey: strings.TrimSpace(input.ProjectKey), ProjectID: strings.TrimSpace(input.ProjectID), Summary: strings.TrimSpace(input.Summary),
	}
	fields := createIssueFields{}
	switch {
	case (requested.ProjectKey == "") == (requested.ProjectID == ""):
		return createIssueRequestBody{}, requested, errors.New("set exactly one of projectKey and projectId")
	case requested.ProjectKey != "" && !projectKeyPattern.MatchString(requested.ProjectKey):
		return createIssueRequestBody{}, requested, errors.New("projectKey must be an uppercase project key such as OPS")
	case requested.ProjectID != "" && !numericIDPattern.MatchString(requested.ProjectID):
		return createIssueRequestBody{}, requested, errors.New("projectId must be a numeric project ID")
	}
	fields.Project = jiraObjectReference{ID: requested.ProjectID, Key: requested.ProjectKey}
	issueTypeName, issueTypeID := strings.TrimSpace(input.IssueTypeName), strings.TrimSpace(input.IssueTypeID)
	switch {
	case (issueTypeName == "") == (issueTypeID == ""):
		return createIssueRequestBody{}, requested, errors.New("set exactly one of issueTypeName and issueTypeId")
	case issueTypeID != "" && !numericIDPattern.MatchString(issueTypeID):
		return createIssueRequestBody{}, requested, errors.New("issueTypeId must be a numeric issue type ID")
	case issueTypeName != "":
		if _, err := validateSingleLineText(issueTypeName, "issueTypeName", maximumIdentifierBytes); err != nil {
			return createIssueRequestBody{}, requested, err
		}
	}
	fields.IssueType = jiraObjectReference{ID: issueTypeID, Name: issueTypeName}
	summary, err := validateSingleLineText(input.Summary, "summary", maximumSummaryCharacters)
	if err != nil {
		return createIssueRequestBody{}, requested, err
	}
	fields.Summary = summary
	if err := validatePlainText(input.Description, "description", false); err != nil {
		return createIssueRequestBody{}, requested, err
	}
	if strings.TrimSpace(input.Description) != "" {
		document := convertPlainTextToDocument(input.Description)
		fields.Description = &document
	}
	if fields.Labels, err = validateLabels(input.Labels); err != nil {
		return createIssueRequestBody{}, requested, err
	}
	if accountID := strings.TrimSpace(input.AssigneeAccountID); accountID != "" {
		if !accountIDPattern.MatchString(accountID) {
			return createIssueRequestBody{}, requested, errors.New("assigneeAccountId must be an Atlassian account ID")
		}
		fields.Assignee = &jiraAccountSelector{AccountID: accountID}
	}
	return createIssueRequestBody{Fields: fields}, requested, nil
}
