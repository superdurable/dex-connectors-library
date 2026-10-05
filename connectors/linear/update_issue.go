// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	updateIssueOperationID = "updateIssue"

	updateIssueDocument = `mutation LinearUpdateIssue($id: String!, $input: IssueUpdateInput!) {
  issueUpdate(id: $id, input: $input) { success issue { ` + issueSummaryFields + ` } }
}`
)

// UpdateIssueInput changes one issue. Every field is optional, but at least one change is required.
// Values are absolute and labels are added or removed as sets, so repeating the update changes nothing.
type UpdateIssueInput struct {
	// IssueID is the issue's UUID or its identifier, such as ENG-123.
	IssueID string `json:"issueId"`
	// StateID moves the issue to this workflow state of its team, from listWorkflowStates.
	StateID string `json:"stateId,omitempty"`
	// AssigneeID assigns the issue to this user UUID, from findUserByEmail.
	AssigneeID string `json:"assigneeId,omitempty"`
	// Unassigns removes the assignee; it excludes AssigneeID.
	Unassigns bool `json:"unassigns,omitempty"`
	// AddedLabelIDs adds these label UUIDs and keeps the issue's other labels.
	AddedLabelIDs []string `json:"addedLabelIds,omitempty"`
	// RemovedLabelIDs removes these label UUIDs; it cannot repeat an added label.
	RemovedLabelIDs []string `json:"removedLabelIds,omitempty"`
	// Priority sets 0 (none), 1 (urgent), 2 (high), 3 (medium), or 4 (low); nil keeps it.
	Priority *int `json:"priority,omitempty"`
	// DueDate sets a YYYY-MM-DD due date; blank keeps it.
	DueDate string `json:"dueDate,omitempty"`
	// ClearsDueDate removes the due date; it excludes DueDate.
	ClearsDueDate bool `json:"clearsDueDate,omitempty"`
	// Title replaces the title with 1 to MaxIssueTitleCharacters characters on one line; blank keeps it.
	Title string `json:"title,omitempty"`
}

// UpdateIssueOutput is the issue after the change.
type UpdateIssueOutput struct {
	// IssueID echoes the requested issue UUID or identifier.
	IssueID string `json:"issueId"`
	// Issue is the issue as Linear returned it after the change; nil on every other branch.
	Issue *IssueSummary `json:"issue,omitempty"`
}

// UpdateIssueOperation implements the updateIssue Mutation.
type UpdateIssueOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (UpdateIssueOperation) Definition() sdkgo.MutationDefinition { return UpdateIssueDefinition }

// IdempotencyKey is the stable Call ID; Linear has no idempotency key, and the update is absolute.
func (UpdateIssueOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateIssueInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies the change. After an input rejection it reads the issue, so a missing issue selects
// notFound and any other rejection selects providerRejected; availability failures are retried.
func (operation UpdateIssueOperation) Invoke(call sdkgo.Call, input UpdateIssueInput) sdkgo.MutationAttempt[UpdateIssueOutput] {
	requested := UpdateIssueOutput{IssueID: input.IssueID}
	request, locator, err := buildUpdateIssueRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateIssueBranchDefect, requested, linearFailurePointer(sdkgo.FailureValidation, updateIssueOperationID, err.Error()), sdkgo.Receipt{})
	}
	requested.IssueID = locator.mutationID()
	session, cancel, failed := operation.client.openSession(call, updateIssueOperationID)
	defer cancel()
	if failed != nil {
		return updateAttemptForExchange(*failed, requested, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName})
	}
	result := session.exchange(request)
	receipt := session.receipt(result.response, locator.mutationID())
	if summary, isUpdated := decodeMutationIssue(result.response.data, "issueUpdate"); isUpdated {
		output := requested
		output.Issue = &summary
		receipt.ProviderObjectID = summary.ID
		return sdkgo.NewMutationBranch(UpdateIssueBranchUpdated, output, nil, receipt)
	}
	result = withUnusablePayloadClassified(result, "issueUpdate", updateIssueOperationID)
	if result.outcome == exchangeRejected && isReadBackWorthy(result) {
		readBack := readIssueSummary(session, locator)
		switch {
		case readBack.result.outcome == exchangeSucceeded && !readBack.isFound:
			return sdkgo.NewMutationBranch(UpdateIssueBranchNotFound, requested, linearFailurePointer(sdkgo.FailureNotFound, updateIssueOperationID,
				"Linear has no issue with this ID that the connection can see"), receipt)
		case readBack.result.outcome == exchangeRetry:
			return sdkgo.NewMutationRetry[UpdateIssueOutput](readBack.result.failure, readBack.result.retryAfter)
		}
	}
	return updateAttemptForExchange(result, requested, receipt)
}

func updateAttemptForExchange(result linearExchange, requested UpdateIssueOutput, receipt sdkgo.Receipt) sdkgo.MutationAttempt[UpdateIssueOutput] {
	switch result.outcome {
	case exchangeRetry:
		return sdkgo.NewMutationRetry[UpdateIssueOutput](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewMutationBranch(UpdateIssueBranchInvalidResponse, requested, &result.failure, receipt)
	case exchangeRejected:
		return sdkgo.NewMutationBranch(UpdateIssueBranchProviderRejected, requested, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(UpdateIssueBranchDefect, requested, &result.failure, receipt)
	}
}

func buildUpdateIssueRequest(input UpdateIssueInput) (graphQLRequest, issueLocator, error) {
	locator, err := parseIssueLocator(input.IssueID, "issueId")
	if err != nil {
		return graphQLRequest{}, issueLocator{}, err
	}
	fields := map[string]any{}
	stateID, err := validateUUIDField(input.StateID, "stateId", false)
	if err != nil {
		return graphQLRequest{}, locator, err
	}
	if stateID != "" {
		fields["stateId"] = stateID
	}
	assigneeID, err := validateUUIDField(input.AssigneeID, "assigneeId", false)
	if err != nil {
		return graphQLRequest{}, locator, err
	}
	switch {
	case assigneeID != "" && input.Unassigns:
		return graphQLRequest{}, locator, errors.New("set at most one of assigneeId and unassigns")
	case assigneeID != "":
		fields["assigneeId"] = assigneeID
	case input.Unassigns:
		fields["assigneeId"] = nil
	}
	if err := addLabelChanges(fields, input.AddedLabelIDs, input.RemovedLabelIDs); err != nil {
		return graphQLRequest{}, locator, err
	}
	if err := validatePriority(input.Priority); err != nil {
		return graphQLRequest{}, locator, err
	}
	if input.Priority != nil {
		fields["priority"] = *input.Priority
	}
	dueDate, err := validateDueDate(input.DueDate)
	if err != nil {
		return graphQLRequest{}, locator, err
	}
	switch {
	case dueDate != "" && input.ClearsDueDate:
		return graphQLRequest{}, locator, errors.New("set at most one of dueDate and clearsDueDate")
	case dueDate != "":
		fields["dueDate"] = dueDate
	case input.ClearsDueDate:
		fields["dueDate"] = nil
	}
	if input.Title != "" {
		title, err := validateOneLineText(input.Title, "title", MaxIssueTitleCharacters)
		if err != nil {
			return graphQLRequest{}, locator, err
		}
		fields["title"] = title
	}
	if len(fields) == 0 {
		return graphQLRequest{}, locator, errors.New("updateIssue needs at least one change")
	}
	return graphQLRequest{operationName: "LinearUpdateIssue", document: updateIssueDocument,
		variables: map[string]any{"id": locator.mutationID(), "input": fields}}, locator, nil
}

// addLabelChanges adds Linear's set operations, which keep labels the Step does not name.
func addLabelChanges(fields map[string]any, added []string, removed []string) error {
	addedLabelIDs, err := validateUUIDList(added, "addedLabelIds")
	if err != nil {
		return err
	}
	removedLabelIDs, err := validateUUIDList(removed, "removedLabelIds")
	if err != nil {
		return err
	}
	for _, removedLabelID := range removedLabelIDs {
		for _, addedLabelID := range addedLabelIDs {
			if removedLabelID == addedLabelID {
				return errors.New("a label cannot be both added and removed")
			}
		}
	}
	if len(addedLabelIDs) != 0 {
		fields["addedLabelIds"] = addedLabelIDs
	}
	if len(removedLabelIDs) != 0 {
		fields["removedLabelIds"] = removedLabelIDs
	}
	return nil
}
