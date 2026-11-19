// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

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
	// case. When the issue is already in it, no transition is performed.
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
	// IsAlreadyInDestinationStatus reports that the issue was already in the destination, so this attempt
	// sent nothing; on a retried Step an earlier attempt may have made the transition.
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

// transitionReadFailure is a read that ended the attempt before any transition was sent in it.
type transitionReadFailure struct {
	classification readClassification
}

// transitionCheckpoint is the heartbeat recorded before the transition request. A later attempt of the
// same Call resends only this transition, and only while the request is still in its source status.
// Python and TypeScript encode the same JSON.
type transitionCheckpoint struct {
	CallID              sdkgo.CallID `json:"callId"`
	Attempt             int32        `json:"attempt"`
	TransitionID        string       `json:"transitionId"`
	TransitionName      string       `json:"transitionName"`
	SourceStatusID      string       `json:"sourceStatusId"`
	DestinationStatusID string       `json:"destinationStatusId"`
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

// Invoke reads the request and skips a transition it has already made. The first attempt selects the
// transition and checkpoints its ID, source status, and destination status before sending it. A later
// attempt never selects again: it succeeds when the request is in the recorded destination, resends the
// recorded transition while the request is still in the recorded source, and otherwise selects
// transitionUnavailable without sending. An unknown outcome returns Retry.
func (operation TransitionTicketOperation) Invoke(call sdkgo.Call, input TransitionTicketInput) sdkgo.MutationAttempt[TransitionTicketOutput] {
	client := operation.client
	selection, err := validateTransitionSelection(input)
	if err != nil {
		return sdkgo.NewMutationBranch(TransitionTicketBranchDefect, TransitionTicketOutput{}, failurePointer(transitionTicketOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	history, err := readTransitionHistory(call)
	if err != nil {
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, "Jira Service Management transition checkpoint is unreadable, so nothing was sent in this attempt"), 0)
	}
	session, cancel, sessionErr := client.startSession(call, transitionTicketOperationID)
	if sessionErr != nil {
		// After a possible earlier transition, a credential failure is no proof that nothing moved.
		if sessionErr.isRetryable || !history.canSelect {
			history.recordNotSent(call)
			return sdkgo.NewMutationRetry[TransitionTicketOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewMutationBranch(TransitionTicketBranchDefect, TransitionTicketOutput{}, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	snapshot, response, readFailure := operation.readIssueTransitions(session, selection.issueIDOrKey)
	if readFailure != nil {
		if readFailure.classification.outcome == readRetry {
			history.recordNotSent(call)
		}
		return readFailure.attempt(client.receipt(session, response, selection.issueIDOrKey))
	}
	receipt := client.receipt(session, response, snapshot.issueKey)
	output := TransitionTicketOutput{IssueID: snapshot.issueID, IssueKey: snapshot.issueKey, PreviousStatus: snapshot.status, Status: snapshot.status}
	if history.hasPlan {
		return operation.resumeRecordedTransition(session, selection, history.plan, snapshot, output, receipt)
	}
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
	if !history.canSelect {
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, unprovenEarlierTransitionMessage), 0)
	}
	if len(matches) != 1 {
		output.AvailableTransitions = snapshot.transitions
		return sdkgo.NewMutationBranch(TransitionTicketBranchTransitionUnavailable, output, describeUnavailableTransition(snapshot.status, len(matches)), receipt)
	}
	chosen := matches[0]
	recorded := transitionCheckpoint{
		CallID: call.ID, Attempt: call.Context.Attempt(), TransitionID: chosen.ID, TransitionName: chosen.Name,
		SourceStatusID: snapshot.status.ID, DestinationStatusID: chosen.DestinationStatus.ID,
	}
	if err := call.Context.RecordHeartbeat(recorded); err != nil {
		history.recordNotSent(call)
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, "Jira Service Management transition checkpoint could not be recorded, so the request was not transitioned"), 0)
	}
	return operation.sendTransition(session, selection, chosen, output)
}

// unprovenEarlierTransitionMessage explains a retry whose earlier attempt may have sent a transition it did not record.
const unprovenEarlierTransitionMessage = "an earlier attempt may have transitioned the request and its transition checkpoint is missing, " +
	"so no transition is selected or sent; the Step retries until its budget ends"

// transitionHistory is what the last heartbeat proves about earlier attempts of this Call.
type transitionHistory struct {
	plan    transitionCheckpoint
	hasPlan bool
	// canSelect reports that no earlier attempt can have sent a transition.
	canSelect bool
}

// transitionNotSentCheckpoint records a Retry before any send; it matches a single-dispatch notSent marker.
type transitionNotSentCheckpoint struct {
	CallID  sdkgo.CallID `json:"callId"`
	Attempt int32        `json:"attempt"`
	State   string       `json:"state"`
}

// transitionHeartbeat decodes this release's checkpoints and an earlier release's single-dispatch markers.
type transitionHeartbeat struct {
	transitionCheckpoint
	State                 string                                  `json:"state"`
	ProviderID            string                                  `json:"providerId"`
	SingleDispatchMarkers map[sdkgo.CallID]transitionLegacyMarker `json:"singleDispatchMarkers"`
}

type transitionLegacyMarker struct {
	CallID     sdkgo.CallID `json:"callId"`
	Attempt    int32        `json:"attempt"`
	State      string       `json:"state"`
	ProviderID string       `json:"providerId"`
}

// recordNotSent lets the next attempt select when this attempt returns Retry before any send. It never
// overwrites a recorded plan or a possible earlier send.
func (history transitionHistory) recordNotSent(call sdkgo.Call) {
	if !history.canSelect || history.hasPlan {
		return
	}
	// A lost record only makes the next attempt read without transitioning.
	_ = call.Context.RecordHeartbeat(transitionNotSentCheckpoint{CallID: call.ID, Attempt: call.Context.Attempt(), State: "notSent"})
}

// resumeRecordedTransition continues the transition an earlier attempt of this Call recorded.
func (operation TransitionTicketOperation) resumeRecordedTransition(
	session *operationSession,
	selection transitionSelection,
	checkpoint transitionCheckpoint,
	snapshot issueTransitionSnapshot,
	output TransitionTicketOutput,
	receipt sdkgo.Receipt,
) sdkgo.MutationAttempt[TransitionTicketOutput] {
	if snapshot.status.ID == checkpoint.DestinationStatusID {
		output.Transition = &TransitionReference{ID: checkpoint.TransitionID, Name: checkpoint.TransitionName, DestinationStatus: snapshot.status}
		output.IsAlreadyInDestinationStatus = true
		return sdkgo.NewMutationBranch(TransitionTicketBranchTransitioned, output, nil, receipt)
	}
	output.AvailableTransitions = snapshot.transitions
	if snapshot.status.ID != checkpoint.SourceStatusID {
		return sdkgo.NewMutationBranch(TransitionTicketBranchTransitionUnavailable, output, failurePointer(transitionTicketOperationID, sdkgo.FailureConflict,
			fmt.Sprintf("the request moved to status %s, which is neither the source nor the destination of transition %s, so it was not sent again", snapshot.status.ID, checkpoint.TransitionID)), receipt)
	}
	for _, transition := range snapshot.transitions {
		if transition.ID == checkpoint.TransitionID && transition.DestinationStatus.ID == checkpoint.DestinationStatusID {
			output.AvailableTransitions = nil
			return operation.sendTransition(session, selection, transition, output)
		}
	}
	return sdkgo.NewMutationBranch(TransitionTicketBranchTransitionUnavailable, output, failurePointer(transitionTicketOperationID, sdkgo.FailureConflict,
		fmt.Sprintf("transition %s is no longer available from status %s", checkpoint.TransitionID, snapshot.status.ID)), receipt)
}

// sendTransition posts the chosen transition once in this attempt and classifies Jira's answer.
func (operation TransitionTicketOperation) sendTransition(
	session *operationSession,
	selection transitionSelection,
	chosen TransitionReference,
	output TransitionTicketOutput,
) sdkgo.MutationAttempt[TransitionTicketOutput] {
	client := operation.client
	output.Transition = &chosen
	result := client.exchange(session, providerRequest{
		method: http.MethodPost, api: jiraPlatformAPI, path: issuePath(selection.issueIDOrKey) + "/transitions", payload: selection.requestBody(chosen.ID),
	})
	receipt := client.receipt(session, result.response, output.IssueKey)
	statusCode := result.response.statusCode
	switch {
	case errors.Is(result.err, errRequestNotBuilt):
		return sdkgo.NewMutationBranch(TransitionTicketBranchDefect, output, failurePointer(transitionTicketOperationID, sdkgo.FailureLocalDefect, "Jira request could not be built"), receipt)
	case result.err == nil && isSuccessStatus(statusCode):
		output.Status = chosen.DestinationStatus
		return sdkgo.NewMutationBranch(TransitionTicketBranchTransitioned, output, nil, receipt)
	case result.err == nil && statusCode == http.StatusNotFound && isCompleteResponse(result.response):
		return sdkgo.NewMutationBranch(TransitionTicketBranchNotFound, output, failurePointer(transitionTicketOperationID, sdkgo.FailureNotFound, "Jira issue was not found or is not visible to the connection"), receipt)
	case result.err == nil && statusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureRateLimit, "Jira rate limited the transition before performing it"), client.retryAfter(result.response))
	case result.err == nil && statusCode == http.StatusServiceUnavailable:
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, "Jira transitions are temporarily unavailable"), client.retryAfter(result.response))
	case result.err != nil && !result.isDispatched:
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureTransport, "Jira could not be reached, so the issue was not transitioned"), 0)
	case result.err == nil && isDefiniteWriteRefusalStatus(statusCode) && isCompleteResponse(result.response):
		return operation.classifyRefusedTransition(session, selection, chosen, output, result.response, receipt)
	case result.err == nil && statusCode < 400:
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureProtocol, fmt.Sprintf("Jira returned unexpected HTTP %d for the transition", statusCode)), 0)
	case result.err == nil && statusCode == http.StatusConflict:
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureConflict, "Jira reported a conflicting update; the outcome of the transition is unknown"), client.retryAfter(result.response))
	default:
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, "Jira did not confirm the transition; the next attempt reads the request before sending it again"), client.retryAfter(result.response))
	}
}

// classifyRefusedTransition rereads the request, because a concurrent duplicate may have made the
// transition first and so caused the refusal.
func (operation TransitionTicketOperation) classifyRefusedTransition(
	session *operationSession,
	selection transitionSelection,
	chosen TransitionReference,
	output TransitionTicketOutput,
	response providerResponse,
	receipt sdkgo.Receipt,
) sdkgo.MutationAttempt[TransitionTicketOutput] {
	reread, _, readFailure := operation.readIssueTransitions(session, selection.issueIDOrKey)
	if readFailure != nil {
		return sdkgo.NewMutationRetry[TransitionTicketOutput](newFailure(transitionTicketOperationID, sdkgo.FailureAvailability, "Jira refused the transition and the request could not be read back"), 0)
	}
	output.Status = reread.status
	if reread.status.ID == chosen.DestinationStatus.ID {
		return sdkgo.NewMutationBranch(TransitionTicketBranchTransitioned, output, nil, receipt)
	}
	output.RejectedFieldIDs = readProviderRejection(response.body).fieldIDs
	failure := rejectionFailure(transitionTicketOperationID, transitionTicketFailureSubject, response.statusCode, providerRejection{fieldIDs: output.RejectedFieldIDs})
	return sdkgo.NewMutationBranch(TransitionTicketBranchProviderRejected, output, &failure, receipt)
}

// readTransitionHistory reads this Call's state from the last heartbeat. Dex can lose a recorded heartbeat,
// so a later attempt may select only after this Call's notSent record from the previous attempt, including
// an earlier release's notSent or canResend marker; anything else counts as a possible send.
func readTransitionHistory(call sdkgo.Call) (transitionHistory, error) {
	attempt := call.Context.Attempt()
	history := transitionHistory{canSelect: attempt == 1}
	var raw json.RawMessage
	hasHeartbeat, err := call.Context.GetLastHeartbeatValue(&raw)
	if err != nil || !hasHeartbeat {
		return history, err
	}
	var heartbeat transitionHeartbeat
	if json.Unmarshal(raw, &heartbeat) != nil {
		return history, nil
	}
	plan := heartbeat.transitionCheckpoint
	if heartbeat.State == "" && plan.CallID == call.ID && plan.TransitionID != "" && plan.SourceStatusID != "" && plan.DestinationStatusID != "" {
		return transitionHistory{plan: plan, hasPlan: true}, nil
	}
	marker := transitionLegacyMarker{CallID: plan.CallID, Attempt: plan.Attempt, State: heartbeat.State, ProviderID: heartbeat.ProviderID}
	if heartbeat.State == "" {
		marker = heartbeat.SingleDispatchMarkers[call.ID]
	}
	if marker.CallID == call.ID && marker.Attempt == attempt-1 && marker.ProviderID == "" &&
		(marker.State == "notSent" || marker.State == "canResend") {
		history.canSelect = true
	}
	return history, nil
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
