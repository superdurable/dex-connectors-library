// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

import (
	"context"
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
	transitionTicketOperationID    = "transitionTicket"
	transitionTicketFailureSubject = "transition"
	transitionTicketReadSubject    = "request"
	maximumListedTransitions       = 50
)

// TransitionTicketInput selects one Jira workflow transition of a request. Set at least one of TransitionID,
// TransitionName, and DestinationStatusName; every one that is set must match the same available transition.
type TransitionTicketInput struct {
	// IssueIDOrKey is a request key such as ITH-12 or a numeric issue ID.
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

// TransitionTicketOutput describes the issue's workflow position after the operation.
type TransitionTicketOutput struct {
	// IssueID is the issue's numeric ID.
	IssueID string `json:"issueId"`
	// IssueKey is the issue's current key.
	IssueKey string `json:"issueKey"`
	// PreviousStatus is the status the operation read before it acted.
	PreviousStatus TicketStatus `json:"previousStatus"`
	// Status is the issue's status after the operation.
	Status TicketStatus `json:"status"`
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
	DestinationStatus TicketStatus `json:"destinationStatus"`
}

// TransitionTicketOperation implements the transitionTicket Mutation through the Jira platform API, which
// offers an agent every workflow transition rather than only the customer transitions of the request API.
type TransitionTicketOperation struct{ client *Client }

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
	status      TicketStatus
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

// jiraObjectReference names a Jira object by ID or name in a write body.
type jiraObjectReference struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (TransitionTicketOperation) Definition() sdkgo.MutationDefinition {
	return TransitionTicketDefinition
}

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID. Jira needs none:
// it refuses a transition that is not available from the issue's current status.
func (TransitionTicketOperation) IdempotencyKey(callID sdkgo.CallID, _ TransitionTicketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the issue, skips a transition the issue has already made, performs the matched transition
// behind a single-dispatch fence, and reads the issue again after any ambiguous response. After a possible
// earlier transition it never transitions again: it succeeds only when the issue is provably in the
// destination status and otherwise returns Retry.
func (operation TransitionTicketOperation) Invoke(call sdkgo.Call, input TransitionTicketInput) sdkgo.MutationAttempt[TransitionTicketOutput] {
	client := operation.client
	selection, err := validateTransitionSelection(input)
	if err != nil {
		return sdkgo.NewMutationBranch(TransitionTicketBranchDefect, TransitionTicketOutput{}, failurePointer(transitionTicketOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	// The default cap of one send: a repeated transition could move the request again.
	fence, err := singledispatch.NewFence(call, TransitionTicketDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return sdkgo.NewMutationBranch(TransitionTicketBranchDefect, TransitionTicketOutput{}, failurePointer(transitionTicketOperationID, sdkgo.FailureLocalDefect, "Jira Service Management dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, "Jira Service Management dispatch checkpoint is unreadable, so nothing was sent in this attempt"), 0)
	}
	attempt := transitionAttempt{fence: fence, hasPossibleEarlierTransition: hasPossibleEarlierSend(decision)}
	session, cancel, sessionErr := client.startSession(call, transitionTicketOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable || attempt.hasPossibleEarlierTransition {
			return attempt.retryBeforeSend(sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewMutationBranch(TransitionTicketBranchDefect, TransitionTicketOutput{}, sessionErr.pointer(), sdkgo.Receipt{})
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
	output := TransitionTicketOutput{IssueID: snapshot.issueID, IssueKey: snapshot.issueKey, PreviousStatus: snapshot.status, Status: snapshot.status}
	if selection.destinationStatusName != "" && strings.EqualFold(snapshot.status.Name, selection.destinationStatusName) {
		output.IsAlreadyInDestinationStatus = true
		return sdkgo.NewMutationBranch(TransitionTicketBranchTransitioned, output, nil, receipt)
	}
	matches := selection.matchingTransitions(snapshot.transitions)
	if len(matches) == 1 && matches[0].DestinationStatus.ID == snapshot.status.ID {
		output.Transition = &matches[0]
		output.IsAlreadyInDestinationStatus = true
		return sdkgo.NewMutationBranch(TransitionTicketBranchTransitioned, output, nil, receipt)
	}
	if attempt.hasPossibleEarlierTransition {
		return attempt.retryBeforeSend(newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, unprovenEarlierTransitionMessage), 0)
	}
	if len(matches) != 1 {
		output.AvailableTransitions = snapshot.transitions
		return sdkgo.NewMutationBranch(TransitionTicketBranchTransitionUnavailable, output, describeUnavailableTransition(snapshot.status, len(matches)), receipt)
	}
	chosen := matches[0]
	output.Transition = &chosen
	write := &transitionWrite{client: client, session: session, request: providerRequest{
		method: http.MethodPost, api: jiraPlatformAPI, path: issuePath(selection.issueIDOrKey) + "/transitions", payload: selection.requestBody(chosen.ID),
	}}
	if err := fence.Dispatch(write.send); err != nil {
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, "Jira Service Management dispatch checkpoint or request budget is unavailable, so the request was not transitioned"), 0)
	}
	result := write.result
	receipt = client.receipt(session, result.response, snapshot.issueKey)
	switch {
	case errors.Is(result.err, errRequestNotBuilt):
		// Nothing was sent; a lost checkpoint only keeps the claim.
		_ = fence.RecordCanResend()
		return sdkgo.NewMutationBranch(TransitionTicketBranchDefect, output, failurePointer(transitionTicketOperationID, sdkgo.FailureLocalDefect, "Jira request could not be built"), receipt)
	case result.err == nil && isSuccessStatus(result.response.statusCode):
		output.Status = chosen.DestinationStatus
		return sdkgo.NewMutationBranch(TransitionTicketBranchTransitioned, output, nil, receipt)
	case result.err == nil && result.response.statusCode == http.StatusNotFound && isCompleteResponse(result.response):
		return attempt.rejectFirstSend(TransitionTicketBranchNotFound, output, newFailure(transitionTicketOperationID, sdkgo.FailureNotFound, "Jira issue was not found or is not visible to the connection"), receipt)
	case result.err == nil && result.response.statusCode == http.StatusTooManyRequests:
		return attempt.retryUnprocessedSend(newFailure(transitionTicketOperationID, sdkgo.FailureRateLimit, "Jira rate limited the transition before performing it"), client.retryAfter(result.response))
	case result.err == nil && result.response.statusCode == http.StatusServiceUnavailable:
		return attempt.retryUnprocessedSend(newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, "Jira transitions are temporarily unavailable"), client.retryAfter(result.response))
	case result.err != nil && !result.isDispatched:
		return attempt.retryUnprocessedSend(newFailure(transitionTicketOperationID, sdkgo.FailureTransport, "Jira could not be reached, so the issue was not transitioned"), 0)
	}
	return operation.reconcileTransition(session, attempt, selection, chosen, output, result)
}

// unprovenEarlierTransitionMessage explains why a repeated attempt does not transition again.
const unprovenEarlierTransitionMessage = "an earlier attempt may have transitioned the request and its status does not prove the destination, " +
	"so the transition is not sent again; set destinationStatusName so a retried Step recognizes it"

// transitionWrite is one fenced transition request and its result.
type transitionWrite struct {
	client  *Client
	session *operationSession
	request providerRequest
	result  exchangeResult
}

func (write *transitionWrite) send(requestContext context.Context) error {
	fencedSession := *write.session
	fencedSession.context = requestContext
	write.result = write.client.exchange(&fencedSession, write.request)
	return nil
}

// transitionAttempt is one attempt's fence and whether an earlier attempt may have transitioned the request.
type transitionAttempt struct {
	fence                        *singledispatch.Fence
	hasPossibleEarlierTransition bool
}

// hasPossibleEarlierSend reports a possible earlier send, including SendAllowed after a canResend marker.
func hasPossibleEarlierSend(decision singledispatch.Decision) bool {
	return decision.Kind != singledispatch.SendAllowed || decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != ""
}

// retryBeforeSend returns Retry before any send; a first attempt records notSent so the next may send.
func (attempt transitionAttempt) retryBeforeSend(failure sdkgo.Failure, retryAfter time.Duration) sdkgo.MutationAttempt[TransitionTicketOutput] {
	if !attempt.hasPossibleEarlierTransition {
		// A lost notSent only makes the next attempt read without transitioning.
		_ = attempt.fence.RecordNotSent()
	}
	return sdkgo.NewMutationRetry[TransitionTicketOutput](failure, retryAfter)
}

// retryUnprocessedSend records that Jira did not process this send, then returns Retry.
func (attempt transitionAttempt) retryUnprocessedSend(failure sdkgo.Failure, retryAfter time.Duration) sdkgo.MutationAttempt[TransitionTicketOutput] {
	if err := attempt.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, "Jira Service Management retry checkpoint could not be recorded"), retryAfter)
	}
	return sdkgo.NewMutationRetry[TransitionTicketOutput](failure, retryAfter)
}

// rejectFirstSend selects a definite refusal of this attempt's only possible send.
func (attempt transitionAttempt) rejectFirstSend(branch sdkgo.BranchID, output TransitionTicketOutput, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[TransitionTicketOutput] {
	if err := attempt.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, "Jira Service Management rejection checkpoint could not be recorded"), 0)
	}
	return sdkgo.NewMutationBranch(branch, output, &failure, receipt)
}

// reconcileTransition rereads the issue, because a concurrent duplicate may have made the transition first.
// An unknown outcome returns Retry with the claim kept, so later attempts never transition again.
func (operation TransitionTicketOperation) reconcileTransition(
	session *operationSession,
	attempt transitionAttempt,
	selection transitionSelection,
	chosen TransitionReference,
	output TransitionTicketOutput,
	result exchangeResult,
) sdkgo.MutationAttempt[TransitionTicketOutput] {
	client := operation.client
	receipt := client.receipt(session, result.response, output.IssueKey)
	reread, _, readFailure := operation.readIssueTransitions(session, selection.issueIDOrKey)
	if readFailure == nil && reread.status.ID == chosen.DestinationStatus.ID {
		output.Status = reread.status
		return sdkgo.NewMutationBranch(TransitionTicketBranchTransitioned, output, nil, receipt)
	}
	statusCode := result.response.statusCode
	isDefiniteRefusal := result.err == nil && isDefiniteWriteRefusalStatus(statusCode) && isCompleteResponse(result.response)
	switch {
	case readFailure != nil && isDefiniteRefusal:
		return attempt.retryUnprocessedSend(newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, "Jira refused the transition and the request could not be read back"), 0)
	case readFailure != nil:
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, "Jira transition outcome could not be read back"), 0)
	case isDefiniteRefusal:
		output.Status = reread.status
		output.RejectedFieldIDs = readProviderRejection(result.response.body).fieldIDs
		failure := rejectionFailure(transitionTicketOperationID, transitionTicketFailureSubject, statusCode, providerRejection{fieldIDs: output.RejectedFieldIDs})
		return attempt.rejectFirstSend(TransitionTicketBranchProviderRejected, output, failure, receipt)
	case result.err == nil && statusCode < 400:
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureProtocol, fmt.Sprintf("Jira returned unexpected HTTP %d for the transition", statusCode)), 0)
	case result.err == nil && statusCode == http.StatusConflict:
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureConflict, "Jira reported a conflicting update; the outcome of the transition is unknown"), client.retryAfter(result.response))
	default:
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, "Jira did not confirm the transition and the issue has not moved yet"), client.retryAfter(result.response))
	}
}

// readIssueTransitions reads the issue's status and its available transitions in one request.
func (operation TransitionTicketOperation) readIssueTransitions(session *operationSession, issueIDOrKey string) (issueTransitionSnapshot, providerResponse, *transitionReadFailure) {
	client := operation.client
	result := client.exchange(session, providerRequest{
		method: http.MethodGet, api: jiraPlatformAPI, path: issuePath(issueIDOrKey), query: url.Values{"fields": {"status"}, "expand": {"transitions"}},
	})
	classification := client.classifyRead(transitionTicketOperationID, transitionTicketReadSubject, result)
	if classification.outcome != readSucceeded {
		return issueTransitionSnapshot{}, result.response, &transitionReadFailure{classification: classification}
	}
	var resource issueTransitionsResource
	if err := json.Unmarshal(result.response.body, &resource); err != nil || resource.Fields.Status == nil ||
		!numericIDPattern.MatchString(resource.ID) || !issueKeyPattern.MatchString(resource.Key) {
		return issueTransitionSnapshot{}, result.response, &transitionReadFailure{classification: readClassification{
			outcome: readInvalid, failure: newFailure(transitionTicketOperationID, sdkgo.FailureProtocol, "Jira returned an invalid issue or transition list"),
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

func (failure *transitionReadFailure) attempt(receipt sdkgo.Receipt) sdkgo.MutationAttempt[TransitionTicketOutput] {
	classification := failure.classification
	switch classification.outcome {
	case readRetry:
		return sdkgo.NewMutationRetry[TransitionTicketOutput](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewMutationBranch(TransitionTicketBranchNotFound, TransitionTicketOutput{}, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewMutationBranch(TransitionTicketBranchDefect, TransitionTicketOutput{}, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewMutationBranch(TransitionTicketBranchInvalidResponse, TransitionTicketOutput{}, &classification.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(TransitionTicketBranchProviderRejected, TransitionTicketOutput{}, &classification.failure, receipt)
	}
}

func validateTransitionSelection(input TransitionTicketInput) (transitionSelection, error) {
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
		validated, err := validateSingleLineText(text.value, text.fieldName, maximumIdentifierCharacters)
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

func describeUnavailableTransition(status TicketStatus, matchCount int) *sdkgo.Failure {
	if matchCount == 0 {
		return failurePointer(transitionTicketOperationID, sdkgo.FailureConflict,
			fmt.Sprintf("no transition available from status %s matches the request", status.ID))
	}
	return failurePointer(transitionTicketOperationID, sdkgo.FailureValidation,
		fmt.Sprintf("%d transitions available from status %s match the request; set transitionId", matchCount, status.ID))
}
