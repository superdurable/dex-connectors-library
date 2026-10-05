// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"encoding/json"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	createIssueOperationID = "createIssue"

	createIssueDocument = `mutation LinearCreateIssue($input: IssueCreateInput!) {
  issueCreate(input: $input) { success issue { ` + issueSummaryFields + ` } }
}`
)

// CreateIssueInput describes one new issue.
type CreateIssueInput struct {
	// TeamID is the UUID of the team that owns the issue, such as the teamPicker unit's teamId.
	TeamID string `json:"teamId"`
	// Title is the issue title, 1 to MaxIssueTitleCharacters characters on one line.
	Title string `json:"title"`
	// Description is optional Markdown of at most MaxMarkdownCharacters characters.
	Description string `json:"description,omitempty"`
	// StateID is a workflow state of the team from listWorkflowStates; blank uses the team's default state.
	StateID string `json:"stateId,omitempty"`
	// AssigneeID is the assignee's user UUID from findUserByEmail; blank leaves the issue unassigned.
	AssigneeID string `json:"assigneeId,omitempty"`
	// LabelIDs are up to MaxLabelIDs label UUIDs of the team or workspace.
	LabelIDs []string `json:"labelIds,omitempty"`
	// Priority is 0 (none), 1 (urgent), 2 (high), 3 (medium), or 4 (low); nil leaves Linear's default.
	Priority *int `json:"priority,omitempty"`
	// DueDate is an optional YYYY-MM-DD date.
	DueDate string `json:"dueDate,omitempty"`
	// ProjectID is an optional project UUID.
	ProjectID string `json:"projectId,omitempty"`
	// ParentID makes the issue a sub-issue of this issue UUID or identifier, such as ENG-123.
	ParentID string `json:"parentId,omitempty"`
}

// CreateIssueOutput is the created issue. On every other branch IssueID is blank and the requested team
// and title are echoed so the application can reconcile.
type CreateIssueOutput struct {
	// TeamID echoes the requested team.
	TeamID string `json:"teamId"`
	// Title echoes the requested title.
	Title string `json:"title"`
	// IssueID is the created issue's UUID, the client-supplied ID every attempt of the Step sent.
	IssueID string `json:"issueId,omitempty"`
	// Issue is the created issue as Linear returned or read it.
	Issue *IssueSummary `json:"issue,omitempty"`
	// IsReplayed reports that an earlier attempt of this Step had already created the issue, which this
	// attempt read back by its UUID.
	IsReplayed bool `json:"replayed,omitempty"`
}

// CreateIssueOperation implements the createIssue Mutation.
type CreateIssueOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (CreateIssueOperation) Definition() sdkgo.MutationDefinition { return CreateIssueDefinition }

// IdempotencyKey is the stable Call ID, from which every attempt of one Step execution derives the same
// client-supplied issue UUID.
func (CreateIssueOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateIssueInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates the issue under the Step's client-supplied UUID. After any answer other than a usable
// issue, a rate limit, a credential or permission rejection, or a rejected GraphQL document, it reads the
// issue back by that UUID, so a create that an earlier attempt or a lost response already applied selects
// created; every retry resends the same UUID.
func (operation CreateIssueOperation) Invoke(call sdkgo.Call, input CreateIssueInput) sdkgo.MutationAttempt[CreateIssueOutput] {
	request, requested, issueID, err := buildCreateIssueRequest(call.IdempotencyKey, input)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateIssueBranchDefect, requested, linearFailurePointer(sdkgo.FailureValidation, createIssueOperationID, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failed := operation.client.openSession(call, createIssueOperationID)
	defer cancel()
	if failed != nil {
		return writeAttemptForExchange(*failed, requested, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName},
			CreateIssueBranchProviderRejected, CreateIssueBranchDefect)
	}
	result := session.exchange(request)
	receipt := session.receipt(result.response, issueID)
	if summary, isCreated := decodeMutationIssue(result.response.data, "issueCreate"); isCreated {
		return createdIssueAttempt(requested, summary, false, receipt)
	}
	result = withUnusablePayloadClassified(result, "issueCreate", createIssueOperationID)
	if isReadBackWorthy(result) {
		readBack := readIssueSummary(session, issueLocator{uuid: issueID})
		switch {
		case readBack.isFound:
			return createdIssueAttempt(requested, readBack.summary, true, session.receipt(readBack.result.response, issueID))
		case readBack.result.outcome != exchangeSucceeded:
			return sdkgo.NewMutationRetry[CreateIssueOutput](readBack.result.failure, readBack.result.retryAfter)
		case result.outcome == exchangeInvalid:
			return sdkgo.NewMutationRetry[CreateIssueOutput](result.failure, 0)
		}
	}
	return writeAttemptForExchange(result, requested, receipt, CreateIssueBranchProviderRejected, CreateIssueBranchDefect)
}

func createdIssueAttempt(requested CreateIssueOutput, summary IssueSummary, isReplayed bool, receipt sdkgo.Receipt) sdkgo.MutationAttempt[CreateIssueOutput] {
	output := requested
	output.IssueID, output.Issue, output.IsReplayed = summary.ID, &summary, isReplayed
	receipt.ProviderObjectID = summary.ID
	return sdkgo.NewMutationBranch(CreateIssueBranchCreated, output, nil, receipt)
}

// buildCreateIssueRequest validates input and returns the request, the echo, and the client-supplied UUID.
func buildCreateIssueRequest(key sdkgo.IdempotencyKey, input CreateIssueInput) (graphQLRequest, CreateIssueOutput, string, error) {
	requested := CreateIssueOutput{TeamID: input.TeamID, Title: input.Title}
	teamID, err := validateUUIDField(input.TeamID, "teamId", true)
	if err != nil {
		return graphQLRequest{}, requested, "", err
	}
	title, err := validateOneLineText(input.Title, "title", MaxIssueTitleCharacters)
	if err != nil {
		return graphQLRequest{}, requested, "", err
	}
	requested.TeamID, requested.Title = teamID, title
	description, err := validateMarkdown(input.Description, "description", false)
	if err != nil {
		return graphQLRequest{}, requested, "", err
	}
	if err := validatePriority(input.Priority); err != nil {
		return graphQLRequest{}, requested, "", err
	}
	dueDate, err := validateDueDate(input.DueDate)
	if err != nil {
		return graphQLRequest{}, requested, "", err
	}
	labelIDs, err := validateUUIDList(input.LabelIDs, "labelIds")
	if err != nil {
		return graphQLRequest{}, requested, "", err
	}
	issueID, err := clientEntityID(key, "issue")
	if err != nil {
		return graphQLRequest{}, requested, "", err
	}
	fields := map[string]any{"id": issueID, "teamId": teamID, "title": title}
	for _, reference := range []struct{ field, value string }{
		{"stateId", input.StateID}, {"assigneeId", input.AssigneeID}, {"projectId", input.ProjectID},
	} {
		validated, err := validateUUIDField(reference.value, reference.field, false)
		if err != nil {
			return graphQLRequest{}, requested, "", err
		}
		if validated != "" {
			fields[reference.field] = validated
		}
	}
	if input.ParentID != "" {
		parent, err := parseIssueLocator(input.ParentID, "parentId")
		if err != nil {
			return graphQLRequest{}, requested, "", err
		}
		fields["parentId"] = parent.mutationID()
	}
	if description != "" {
		fields["description"] = description
	}
	if len(labelIDs) != 0 {
		fields["labelIds"] = labelIDs
	}
	if input.Priority != nil {
		fields["priority"] = *input.Priority
	}
	if dueDate != "" {
		fields["dueDate"] = dueDate
	}
	return graphQLRequest{operationName: "LinearCreateIssue", document: createIssueDocument, variables: map[string]any{"input": fields}},
		requested, issueID, nil
}

// mutationPayloadWire is the IssuePayload or CommentPayload a mutation field returns.
type mutationPayloadWire struct {
	Success *bool           `json:"success"`
	Issue   *issueWire      `json:"issue"`
	Comment json.RawMessage `json:"comment"`
}

// decodeMutationPayload reads the named mutation field, even beside a nested field error.
func decodeMutationPayload(data json.RawMessage, mutationField string) (mutationPayloadWire, bool) {
	if data == nil {
		return mutationPayloadWire{}, false
	}
	var document map[string]*mutationPayloadWire
	if err := json.Unmarshal(data, &document); err != nil || document[mutationField] == nil {
		return mutationPayloadWire{}, false
	}
	return *document[mutationField], true
}

// decodeMutationIssue returns the issue a successful issueCreate or issueUpdate returned.
func decodeMutationIssue(data json.RawMessage, mutationField string) (IssueSummary, bool) {
	payload, isPresent := decodeMutationPayload(data, mutationField)
	if !isPresent || payload.Success == nil || !*payload.Success || payload.Issue == nil {
		return IssueSummary{}, false
	}
	summary, err := decodeIssueSummary(*payload.Issue)
	return summary, err == nil
}

// withUnusablePayloadClassified turns a 2xx without a usable payload into a rejection or an invalid answer.
func withUnusablePayloadClassified(result linearExchange, mutationField string, operation string) linearExchange {
	if result.outcome != exchangeSucceeded {
		return result
	}
	payload, isPresent := decodeMutationPayload(result.response.data, mutationField)
	if isPresent && payload.Success != nil && !*payload.Success {
		return linearExchange{outcome: exchangeRejected, response: result.response,
			failure: linearFailure(sdkgo.FailureProviderRejection, operation, "Linear reported that the write did not succeed")}
	}
	return linearExchange{outcome: exchangeInvalid, response: result.response,
		failure: linearFailure(sdkgo.FailureProtocol, operation, "Linear accepted the write but returned an unusable record")}
}

// writeAttemptForExchange maps a create failure that no read-back resolved; retrying is safe because every
// attempt carries the Step's client-supplied UUID.
func writeAttemptForExchange[OUT any](result linearExchange, requested OUT, receipt sdkgo.Receipt, providerRejected sdkgo.BranchID, defect sdkgo.BranchID) sdkgo.MutationAttempt[OUT] {
	switch result.outcome {
	case exchangeRetry, exchangeInvalid:
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter)
	case exchangeRejected:
		return sdkgo.NewMutationBranch(providerRejected, requested, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(defect, requested, &result.failure, receipt)
	}
}
