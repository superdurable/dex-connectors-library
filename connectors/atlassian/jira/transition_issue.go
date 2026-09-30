// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	transitionIssueOperationID    = "transitionIssue"
	transitionIssueFailureSubject = "transition"
	maximumListedTransitions      = 50
)

// TransitionIssueInput selects one workflow transition. Set at least one of TransitionID, TransitionName,
// and DestinationStatusName; every one that is set must match the same available transition.
type TransitionIssueInput struct {
	// IssueIDOrKey is an issue key such as OPS-441 or a numeric issue ID.
	IssueIDOrKey string `json:"issueIdOrKey"`
	// TransitionID is the workflow transition ID, such as 31.
	TransitionID string `json:"transitionId,omitempty"`
	// TransitionName is the transition's name as Jira shows it on the issue, such as Start Progress,
	// compared without case.
	TransitionName string `json:"transitionName,omitempty"`
	// DestinationStatusName is the status the transition leads to, such as In Progress, compared without
	// case. When the issue is already in it, no transition is performed. Set it whenever the destination
	// is known: a retried Step then recognizes a transition Jira already made.
	DestinationStatusName string `json:"destinationStatusName,omitempty"`
	// ResolutionName sets the resolution, such as Done, for a transition whose screen requires one;
	// blank sends no fields. Jira rejects it for a transition without a resolution field.
	ResolutionName string `json:"resolutionName,omitempty"`
}

// TransitionIssueOutput describes the issue's workflow position after the operation.
type TransitionIssueOutput struct {
	// IssueID is the issue's numeric ID.
	IssueID string `json:"issueId"`
	// IssueKey is the issue's current key.
	IssueKey string `json:"issueKey"`
	// PreviousStatus is the status the operation read before it acted.
	PreviousStatus IssueStatus `json:"previousStatus"`
	// Status is the issue's status after the operation.
	Status IssueStatus `json:"status"`
	// Transition is the matched transition, or nil when none matched.
	Transition *TransitionReference `json:"transition,omitempty"`
	// IsAlreadyInDestinationStatus reports that the issue was already in the destination, so nothing was sent.
	IsAlreadyInDestinationStatus bool `json:"isAlreadyInDestinationStatus,omitempty"`
	// AvailableTransitions lists up to 50 transitions available from Status, set on transitionUnavailable.
	AvailableTransitions []TransitionReference `json:"availableTransitions,omitempty"`
	// RejectedFieldIDs lists the field IDs Jira named in a rejection, such as resolution.
	RejectedFieldIDs []string `json:"rejectedFieldIds,omitempty"`
}

// TransitionReference is one workflow transition available on an issue.
type TransitionReference struct {
	// ID is the transition ID.
	ID string `json:"id"`
	// Name is the transition name.
	Name string `json:"name"`
	// DestinationStatus is the status the transition leads to.
	DestinationStatus IssueStatus `json:"destinationStatus"`
}

// TransitionIssueOperation implements the transitionIssue Mutation.
type TransitionIssueOperation struct{ client *Client }

type transitionSelection struct {
	issueIDOrKey          string
	transitionID          string
	transitionName        string
	destinationStatusName string
	resolutionName        string
}

// issueTransitionSnapshot is the issue's status and available transitions read in one request.
type issueTransitionSnapshot struct {
	issueID     string
	issueKey    string
	status      IssueStatus
	transitions []TransitionReference
}

// transitionReadFailure is a read that ended the operation before any transition was sent.
type transitionReadFailure struct {
	classification readClassification
}

type issueTransitionsResource struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Status *statusResource `json:"status"`
	} `json:"fields"`
	Transitions []struct {
		ID   string          `json:"id"`
		Name string          `json:"name"`
		To   *statusResource `json:"to"`
	} `json:"transitions"`
}

type transitionRequestBody struct {
	Transition jiraObjectReference            `json:"transition"`
	Fields     map[string]jiraObjectReference `json:"fields,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (TransitionIssueOperation) Definition() sdkgo.MutationDefinition {
	return TransitionIssueDefinition
}

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID. Jira needs none:
// it refuses a transition that is not available from the issue's current status.
func (TransitionIssueOperation) IdempotencyKey(callID sdkgo.CallID, _ TransitionIssueInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the issue, skips a transition the issue has already made, performs the matched transition,
// and reads the issue again after any ambiguous response instead of guessing its outcome.
func (operation TransitionIssueOperation) Invoke(call sdkgo.Call, input TransitionIssueInput) sdkgo.MutationAttempt[TransitionIssueOutput] {
	client := operation.client
	selection, err := validateTransitionSelection(input)
	if err != nil {
		return sdkgo.NewMutationBranch(TransitionIssueBranchDefect, TransitionIssueOutput{}, failurePointer(transitionIssueOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, sessionErr := client.startSession(call, transitionIssueOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewMutationRetry[TransitionIssueOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewMutationBranch(TransitionIssueBranchDefect, TransitionIssueOutput{}, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	snapshot, response, readFailure := operation.readIssueTransitions(session, selection.issueIDOrKey)
	if readFailure != nil {
		return readFailure.attempt(client.receipt(session, response, selection.issueIDOrKey))
	}
	receipt := client.receipt(session, response, snapshot.issueKey)
	output := TransitionIssueOutput{IssueID: snapshot.issueID, IssueKey: snapshot.issueKey, PreviousStatus: snapshot.status, Status: snapshot.status}
	if selection.destinationStatusName != "" && strings.EqualFold(snapshot.status.Name, selection.destinationStatusName) {
		output.IsAlreadyInDestinationStatus = true
		return sdkgo.NewMutationBranch(TransitionIssueBranchTransitioned, output, nil, receipt)
	}
	matches := selection.matchingTransitions(snapshot.transitions)
	if len(matches) != 1 {
		output.AvailableTransitions = snapshot.transitions
		return sdkgo.NewMutationBranch(TransitionIssueBranchTransitionUnavailable, output, describeUnavailableTransition(snapshot.status, len(matches)), receipt)
	}
	chosen := matches[0]
	output.Transition = &chosen
	if chosen.DestinationStatus.ID == snapshot.status.ID {
		output.IsAlreadyInDestinationStatus = true
		return sdkgo.NewMutationBranch(TransitionIssueBranchTransitioned, output, nil, receipt)
	}
	result := client.exchange(session, jiraRequest{
		method: http.MethodPost, path: issuePath(selection.issueIDOrKey) + "/transitions", payload: selection.requestBody(chosen.ID),
	})
	receipt = client.receipt(session, result.response, snapshot.issueKey)
	switch {
	case errors.Is(result.err, errRequestNotBuilt):
		return sdkgo.NewMutationBranch(TransitionIssueBranchDefect, output, failurePointer(transitionIssueOperationID, sdkgo.FailureLocalDefect, "Jira request could not be built"), receipt)
	case result.err == nil && isSuccessStatus(result.response.statusCode):
		output.Status = chosen.DestinationStatus
		return sdkgo.NewMutationBranch(TransitionIssueBranchTransitioned, output, nil, receipt)
	case result.err == nil && result.response.statusCode == http.StatusNotFound:
		return sdkgo.NewMutationBranch(TransitionIssueBranchNotFound, output, failurePointer(transitionIssueOperationID, sdkgo.FailureNotFound, "Jira issue was not found or is not visible to the connection"), receipt)
	case result.err == nil && result.response.statusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[TransitionIssueOutput](newFailure(transitionIssueOperationID, sdkgo.FailureRateLimit, "Jira rate limited the transition before performing it"), client.retryAfter(result.response))
	case result.err != nil && !result.isDispatched:
		return sdkgo.NewMutationRetry[TransitionIssueOutput](newFailure(transitionIssueOperationID, sdkgo.FailureTransport, "Jira could not be reached, so the issue was not transitioned"), 0)
	}
	return operation.reconcileTransition(session, selection, chosen, output, result)
}

// reconcileTransition rereads the issue, because a concurrent duplicate may have made the transition first.
func (operation TransitionIssueOperation) reconcileTransition(
	session *operationSession,
	selection transitionSelection,
	chosen TransitionReference,
	output TransitionIssueOutput,
	result exchangeResult,
) sdkgo.MutationAttempt[TransitionIssueOutput] {
	client := operation.client
	receipt := client.receipt(session, result.response, output.IssueKey)
	reread, _, readFailure := operation.readIssueTransitions(session, selection.issueIDOrKey)
	if readFailure == nil && reread.status.ID == chosen.DestinationStatus.ID {
		output.Status = reread.status
		return sdkgo.NewMutationBranch(TransitionIssueBranchTransitioned, output, nil, receipt)
	}
	statusCode := result.response.statusCode
	switch {
	case readFailure != nil:
		return sdkgo.NewMutationRetry[TransitionIssueOutput](newFailure(transitionIssueOperationID, sdkgo.FailureAvailability, "Jira transition outcome could not be read back"), 0)
	case result.err == nil && isConclusiveRejectionStatus(statusCode) && statusCode != http.StatusConflict:
		output.Status = reread.status
		output.RejectedFieldIDs = readJiraRejectedFieldIDs(result.response.body)
		failure := rejectionFailure(transitionIssueOperationID, transitionIssueFailureSubject, statusCode, output.RejectedFieldIDs)
		return sdkgo.NewMutationBranch(TransitionIssueBranchProviderRejected, output, &failure, receipt)
	case result.err == nil && statusCode < 400:
		return sdkgo.NewMutationBranch(TransitionIssueBranchInvalidResponse, output, failurePointer(transitionIssueOperationID, sdkgo.FailureProtocol, fmt.Sprintf("Jira returned unexpected HTTP %d for the transition", statusCode)), receipt)
	case result.err == nil && statusCode == http.StatusConflict:
		return sdkgo.NewMutationRetry[TransitionIssueOutput](newFailure(transitionIssueOperationID, sdkgo.FailureConflict, "Jira reported a conflicting update, so the transition is retried"), client.retryAfter(result.response))
	default:
		return sdkgo.NewMutationRetry[TransitionIssueOutput](newFailure(transitionIssueOperationID, sdkgo.FailureAvailability, "Jira did not confirm the transition and the issue has not moved yet"), client.retryAfter(result.response))
	}
}

// readIssueTransitions reads the issue's status and its available transitions in one request.
func (operation TransitionIssueOperation) readIssueTransitions(session *operationSession, issueIDOrKey string) (issueTransitionSnapshot, jiraResponse, *transitionReadFailure) {
	client := operation.client
	result := client.exchange(session, jiraRequest{
		method: http.MethodGet, path: issuePath(issueIDOrKey), query: url.Values{"fields": {"status"}, "expand": {"transitions"}},
	})
	classification := client.classifyRead(transitionIssueOperationID, getIssueFailureSubject, result)
	if classification.outcome != readSucceeded {
		return issueTransitionSnapshot{}, result.response, &transitionReadFailure{classification: classification}
	}
	var resource issueTransitionsResource
	if err := json.Unmarshal(result.response.body, &resource); err != nil || resource.Fields.Status == nil ||
		!numericIDPattern.MatchString(resource.ID) || !issueKeyPattern.MatchString(resource.Key) {
		return issueTransitionSnapshot{}, result.response, &transitionReadFailure{classification: readClassification{
			outcome: readInvalid, failure: newFailure(transitionIssueOperationID, sdkgo.FailureProtocol, "Jira returned an invalid issue or transition list"),
		}}
	}
	snapshot := issueTransitionSnapshot{issueID: resource.ID, issueKey: resource.Key, status: resource.Fields.Status.view()}
	for _, transition := range resource.Transitions {
		if transition.ID == "" || transition.To == nil || len(snapshot.transitions) == maximumListedTransitions {
			continue
		}
		snapshot.transitions = append(snapshot.transitions, TransitionReference{ID: transition.ID, Name: transition.Name, DestinationStatus: transition.To.view()})
	}
	return snapshot, result.response, nil
}

func (failure *transitionReadFailure) attempt(receipt sdkgo.Receipt) sdkgo.MutationAttempt[TransitionIssueOutput] {
	classification := failure.classification
	switch classification.outcome {
	case readRetry:
		return sdkgo.NewMutationRetry[TransitionIssueOutput](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewMutationBranch(TransitionIssueBranchNotFound, TransitionIssueOutput{}, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewMutationBranch(TransitionIssueBranchDefect, TransitionIssueOutput{}, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewMutationBranch(TransitionIssueBranchInvalidResponse, TransitionIssueOutput{}, &classification.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(TransitionIssueBranchProviderRejected, TransitionIssueOutput{}, &classification.failure, receipt)
	}
}

func validateTransitionSelection(input TransitionIssueInput) (transitionSelection, error) {
	issueIDOrKey, err := validateIssueIDOrKey(input.IssueIDOrKey)
	if err != nil {
		return transitionSelection{}, err
	}
	selection := transitionSelection{issueIDOrKey: issueIDOrKey, transitionID: strings.TrimSpace(input.TransitionID)}
	if selection.transitionID != "" && !numericIDPattern.MatchString(selection.transitionID) {
		return transitionSelection{}, errors.New("transitionId must be a numeric transition ID")
	}
	for _, text := range []struct {
		value       string
		fieldName   string
		destination *string
	}{
		{input.TransitionName, "transitionName", &selection.transitionName},
		{input.DestinationStatusName, "destinationStatusName", &selection.destinationStatusName},
		{input.ResolutionName, "resolutionName", &selection.resolutionName},
	} {
		if strings.TrimSpace(text.value) == "" {
			continue
		}
		validated, err := validateSingleLineText(text.value, text.fieldName, maximumIdentifierBytes)
		if err != nil {
			return transitionSelection{}, err
		}
		*text.destination = validated
	}
	if selection.transitionID == "" && selection.transitionName == "" && selection.destinationStatusName == "" {
		return transitionSelection{}, errors.New("set transitionId, transitionName, or destinationStatusName")
	}
	return selection, nil
}

func (selection transitionSelection) matchingTransitions(transitions []TransitionReference) []TransitionReference {
	var matches []TransitionReference
	for _, transition := range transitions {
		switch {
		case selection.transitionID != "" && transition.ID != selection.transitionID:
		case selection.transitionName != "" && !strings.EqualFold(strings.TrimSpace(transition.Name), selection.transitionName):
		case selection.destinationStatusName != "" && !strings.EqualFold(transition.DestinationStatus.Name, selection.destinationStatusName):
		default:
			matches = append(matches, transition)
		}
	}
	return matches
}

func (selection transitionSelection) requestBody(transitionID string) transitionRequestBody {
	body := transitionRequestBody{Transition: jiraObjectReference{ID: transitionID}}
	if selection.resolutionName != "" {
		body.Fields = map[string]jiraObjectReference{"resolution": {Name: selection.resolutionName}}
	}
	return body
}

func describeUnavailableTransition(status IssueStatus, matchCount int) *sdkgo.Failure {
	if matchCount == 0 {
		return failurePointer(transitionIssueOperationID, sdkgo.FailureConflict,
			fmt.Sprintf("no transition available from status %s matches the request", status.ID))
	}
	return failurePointer(transitionIssueOperationID, sdkgo.FailureValidation,
		fmt.Sprintf("%d transitions available from status %s match the request; set transitionId", matchCount, status.ID))
}
