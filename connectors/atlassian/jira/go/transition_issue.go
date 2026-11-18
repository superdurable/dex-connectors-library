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
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
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

// Invoke reads the issue, skips a transition the issue has already made, performs the matched transition
// behind a single-dispatch fence, and reads the issue again after any ambiguous response. After a possible
// earlier transition it never transitions again: it succeeds only when the issue is provably in the
// destination status and otherwise returns Retry.
func (operation TransitionIssueOperation) Invoke(call sdkgo.Call, input TransitionIssueInput) sdkgo.MutationAttempt[TransitionIssueOutput] {
	client := operation.client
	selection, err := validateTransitionSelection(input)
	if err != nil {
		return sdkgo.NewMutationBranch(TransitionIssueBranchDefect, TransitionIssueOutput{}, failurePointer(transitionIssueOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	// The default cap of one send: a repeated transition could move the issue again.
	fence, err := singledispatch.NewFence(call, TransitionIssueDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return sdkgo.NewMutationBranch(TransitionIssueBranchDefect, TransitionIssueOutput{}, failurePointer(transitionIssueOperationID, sdkgo.FailureLocalDefect, "Jira dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationRetry[TransitionIssueOutput](newFailure(transitionIssueOperationID, sdkgo.FailureAvailability, "Jira dispatch checkpoint is unreadable, so nothing was sent in this attempt"), 0)
	}
	attempt := transitionAttempt{fence: fence, hasPossibleEarlierTransition: hasPossibleEarlierTransition(decision)}
	session, cancel, sessionErr := client.startSession(call, transitionIssueOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable || attempt.hasPossibleEarlierTransition {
			return attempt.retryBeforeSend(sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewMutationBranch(TransitionIssueBranchDefect, TransitionIssueOutput{}, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	snapshot, response, readFailure := operation.readIssueTransitions(session, selection.issueIDOrKey)
	if readFailure != nil {
		if readFailure.classification.outcome == readRetry || attempt.hasPossibleEarlierTransition {
			return attempt.retryBeforeSend(readFailure.classification.failure, readFailure.classification.retryAfter)
		}
		return readFailure.attempt(client.receipt(session, response, selection.issueIDOrKey))
	}
	receipt := client.receipt(session, response, snapshot.issueKey)
	output := TransitionIssueOutput{IssueID: snapshot.issueID, IssueKey: snapshot.issueKey, PreviousStatus: snapshot.status, Status: snapshot.status}
	if selection.destinationStatusName != "" && strings.EqualFold(snapshot.status.Name, selection.destinationStatusName) {
		output.IsAlreadyInDestinationStatus = true
		return sdkgo.NewMutationBranch(TransitionIssueBranchTransitioned, output, nil, receipt)
	}
	matches := selection.matchingTransitions(snapshot.transitions)
	if len(matches) == 1 && matches[0].DestinationStatus.ID == snapshot.status.ID {
		output.Transition = &matches[0]
		output.IsAlreadyInDestinationStatus = true
		return sdkgo.NewMutationBranch(TransitionIssueBranchTransitioned, output, nil, receipt)
	}
	if attempt.hasPossibleEarlierTransition {
		return attempt.retryBeforeSend(newFailure(transitionIssueOperationID, sdkgo.FailureAvailability, unprovenEarlierTransitionMessage), 0)
	}
	if len(matches) != 1 {
		output.AvailableTransitions = snapshot.transitions
		return sdkgo.NewMutationBranch(TransitionIssueBranchTransitionUnavailable, output, describeUnavailableTransition(snapshot.status, len(matches)), receipt)
	}
	chosen := matches[0]
	output.Transition = &chosen
	write := &jiraWriteAttempt{client: client, session: session, request: jiraRequest{
		method: http.MethodPost, path: issuePath(selection.issueIDOrKey) + "/transitions", payload: selection.requestBody(chosen.ID),
	}}
	if err := fence.Dispatch(write.send); err != nil {
		return sdkgo.NewMutationRetry[TransitionIssueOutput](newFailure(transitionIssueOperationID, sdkgo.FailureAvailability, "Jira dispatch checkpoint or request budget is unavailable, so the issue was not transitioned"), 0)
	}
	result := write.result
	receipt = client.receipt(session, result.response, snapshot.issueKey)
	switch {
	case errors.Is(result.err, errRequestNotBuilt):
		// Nothing was sent; a lost checkpoint only keeps the claim.
		_ = fence.RecordCanResend()
		return sdkgo.NewMutationBranch(TransitionIssueBranchDefect, output, failurePointer(transitionIssueOperationID, sdkgo.FailureLocalDefect, "Jira request could not be built"), receipt)
	case result.err == nil && isSuccessStatus(result.response.statusCode):
		output.Status = chosen.DestinationStatus
		return sdkgo.NewMutationBranch(TransitionIssueBranchTransitioned, output, nil, receipt)
	case result.err == nil && result.response.statusCode == http.StatusNotFound && isCompleteResponse(result.response):
		return attempt.rejectFirstSend(TransitionIssueBranchNotFound, output, newFailure(transitionIssueOperationID, sdkgo.FailureNotFound, "Jira issue was not found or is not visible to the connection"), receipt)
	case result.err == nil && result.response.statusCode == http.StatusTooManyRequests:
		return attempt.retryUnprocessedSend(newFailure(transitionIssueOperationID, sdkgo.FailureRateLimit, "Jira rate limited the transition before performing it"), client.retryAfter(result.response))
	case result.err == nil && result.response.statusCode == http.StatusServiceUnavailable:
		return attempt.retryUnprocessedSend(newFailure(transitionIssueOperationID, sdkgo.FailureAvailability, "Jira transitions are temporarily unavailable"), client.retryAfter(result.response))
	case result.err != nil && !result.isDispatched:
		return attempt.retryUnprocessedSend(newFailure(transitionIssueOperationID, sdkgo.FailureTransport, "Jira could not be reached, so the issue was not transitioned"), 0)
	}
	return operation.reconcileTransition(session, attempt, selection, chosen, output, result)
}

// unprovenEarlierTransitionMessage explains why a repeated attempt does not transition again.
const unprovenEarlierTransitionMessage = "an earlier attempt may have transitioned the issue and its status does not prove the destination, " +
	"so the transition is not sent again; set destinationStatusName so a retried Step recognizes it"

// transitionAttempt is one attempt's fence and whether an earlier attempt may have transitioned the issue.
type transitionAttempt struct {
	fence                        *singledispatch.Fence
	hasPossibleEarlierTransition bool
}

// hasPossibleEarlierTransition ignores MaximumPriorSends: a cap-1 fence writes no send count, so SendAllowed proves no earlier send.
func hasPossibleEarlierTransition(decision singledispatch.Decision) bool {
	return decision.Kind != singledispatch.SendAllowed || decision.Marker.ProviderID != ""
}

// retryBeforeSend returns Retry before any send; a first attempt records notSent so the next may send.
func (attempt transitionAttempt) retryBeforeSend(failure sdkgo.Failure, retryAfter time.Duration) sdkgo.MutationAttempt[TransitionIssueOutput] {
	if !attempt.hasPossibleEarlierTransition {
		// A lost notSent only makes the next attempt read without transitioning.
		_ = attempt.fence.RecordNotSent()
	}
	return sdkgo.NewMutationRetry[TransitionIssueOutput](failure, retryAfter)
}

// retryUnprocessedSend records that Jira did not process this send, then returns Retry.
func (attempt transitionAttempt) retryUnprocessedSend(failure sdkgo.Failure, retryAfter time.Duration) sdkgo.MutationAttempt[TransitionIssueOutput] {
	if err := attempt.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[TransitionIssueOutput](newFailure(transitionIssueOperationID, sdkgo.FailureAvailability, "Jira retry checkpoint could not be recorded"), retryAfter)
	}
	return sdkgo.NewMutationRetry[TransitionIssueOutput](failure, retryAfter)
}

// rejectFirstSend selects a definite refusal of this attempt's only possible send.
func (attempt transitionAttempt) rejectFirstSend(branch sdkgo.BranchID, output TransitionIssueOutput, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[TransitionIssueOutput] {
	if err := attempt.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[TransitionIssueOutput](newFailure(transitionIssueOperationID, sdkgo.FailureAvailability, "Jira rejection checkpoint could not be recorded"), 0)
	}
	return sdkgo.NewMutationBranch(branch, output, &failure, receipt)
}

// reconcileTransition rereads the issue, because a concurrent duplicate may have made the transition first.
// An unknown outcome returns Retry with the claim kept, so later attempts never transition again.
func (operation TransitionIssueOperation) reconcileTransition(
	session *operationSession,
	attempt transitionAttempt,
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
	isDefiniteRefusal := result.err == nil && isDefiniteWriteRefusalStatus(statusCode) && isCompleteResponse(result.response)
	switch {
	case readFailure != nil && isDefiniteRefusal:
		return attempt.retryUnprocessedSend(newFailure(transitionIssueOperationID, sdkgo.FailureAvailability, "Jira refused the transition and the issue could not be read back"), 0)
	case readFailure != nil:
		return sdkgo.NewMutationRetry[TransitionIssueOutput](newFailure(transitionIssueOperationID, sdkgo.FailureAvailability, "Jira transition outcome could not be read back"), 0)
	case isDefiniteRefusal:
		output.Status = reread.status
		output.RejectedFieldIDs = readJiraRejectedFieldIDs(result.response.body)
		failure := rejectionFailure(transitionIssueOperationID, transitionIssueFailureSubject, statusCode, output.RejectedFieldIDs)
		return attempt.rejectFirstSend(TransitionIssueBranchProviderRejected, output, failure, receipt)
	case result.err == nil && statusCode < 400:
		return sdkgo.NewMutationRetry[TransitionIssueOutput](newFailure(transitionIssueOperationID, sdkgo.FailureProtocol, fmt.Sprintf("Jira returned unexpected HTTP %d for the transition", statusCode)), 0)
	case result.err == nil && statusCode == http.StatusConflict:
		return sdkgo.NewMutationRetry[TransitionIssueOutput](newFailure(transitionIssueOperationID, sdkgo.FailureConflict, "Jira reported a conflicting update; the outcome of the transition is unknown"), client.retryAfter(result.response))
	default:
		return sdkgo.NewMutationRetry[TransitionIssueOutput](newFailure(transitionIssueOperationID, sdkgo.FailureAvailability, "Jira did not confirm the transition and the issue has not moved yet"), client.retryAfter(result.response))
	}
}

// isCompleteResponse reports a response whose body was read in full; only then can it prove a refusal.
func isCompleteResponse(response jiraResponse) bool {
	return !response.hasBodyReadFailed && !response.isBodyTooLarge && !response.reflectsSecret
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
