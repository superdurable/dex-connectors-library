// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// singleSubmissionMarker is the Dex heartbeat checkpoint that counts the possible submissions of one Call ID.
// Every attempt that may submit records it before SMTP DATA, and every attempt that returns Retry without
// an accepted submission records it again, so the next attempt can bound the earlier submissions.
type singleSubmissionMarker struct {
	SubmittedCallID sdkgo.CallID `json:"emailSubmittedCallId"`
	// Attempt is the attempt that wrote the marker. Zero marks a marker written before bounded resend,
	// which recorded only the Call ID before a submission.
	Attempt int32 `json:"emailAttempt,omitempty"`
	// SubmissionCount is the number of possibly accepted submissions of the Call ID up to and including
	// Attempt: a claim counts its own submission, and a provable non-acceptance does not.
	SubmissionCount int32 `json:"emailSubmissionCount,omitempty"`
	// IsAccepted records that the server accepted a submission, so a replayed attempt never submits again.
	IsAccepted bool `json:"emailAccepted,omitempty"`
}

// submissionDecisionKind is what this attempt may do before it submits.
type submissionDecisionKind uint8

const (
	// submissionAllowed permits one submission; MaximumPriorSubmissions may be above zero.
	submissionAllowed submissionDecisionKind = iota + 1
	// submissionCapReached forbids another submission: the earlier ones may have reached the send cap.
	submissionCapReached
	// submissionCheckpointUntrusted forbids a submission because the checkpoint is unreadable or foreign.
	submissionCheckpointUntrusted
	// submissionAlreadyAccepted forbids a submission: the server accepted an earlier one.
	submissionAlreadyAccepted
)

// submissionDecision bounds the earlier submissions of the Call ID before this attempt.
type submissionDecision struct {
	kind submissionDecisionKind
	// maximumPriorSubmissions is an upper bound on the earlier submissions that may have been accepted.
	maximumPriorSubmissions int32
	// maxSends is the send cap of this Step: call.MaxSends().
	maxSends int32
}

// decideSubmission reads the last checkpoint before credentials or any server is contacted. An attempt
// that finds no marker counts every earlier attempt as a possible submission, because a claim frame can be
// lost; a marker from an earlier attempt adds the attempts after it.
func decideSubmission(call sdkgo.Call) submissionDecision {
	var marker singleSubmissionMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	if err != nil {
		return submissionDecision{kind: submissionCheckpointUntrusted, maxSends: call.MaxSends()}
	}
	var lastMarker *singleSubmissionMarker
	if isMarkerFound {
		lastMarker = &marker
	}
	return decideSubmissionFromMarker(call.ID, call.Context.Attempt(), call.MaxSends(), lastMarker)
}

// decideSubmissionFromMarker applies the checkpoint rules to the last marker, which is nil when none was found.
func decideSubmissionFromMarker(callID sdkgo.CallID, attempt int32, maxSends int32, marker *singleSubmissionMarker) submissionDecision {
	decision := submissionDecision{maxSends: maxSends}
	switch {
	case marker == nil:
		decision.maximumPriorSubmissions = max(attempt-1, 0)
	case marker.SubmittedCallID != callID:
		decision.kind = submissionCheckpointUntrusted
		return decision
	case marker.Attempt == 0:
		decision.maximumPriorSubmissions = max(attempt-1, 0)
	case marker.Attempt >= attempt || marker.SubmissionCount < 0 || marker.SubmissionCount > marker.Attempt:
		decision.kind = submissionCheckpointUntrusted
		return decision
	case marker.IsAccepted:
		decision.kind = submissionAlreadyAccepted
		return decision
	default:
		decision.maximumPriorSubmissions = marker.SubmissionCount + (attempt - 1 - marker.Attempt)
	}
	decision.kind = submissionAllowed
	if decision.maximumPriorSubmissions >= decision.maxSends {
		decision.kind = submissionCapReached
	}
	return decision
}

// hasPossibleEarlierSubmission reports that an earlier attempt may have submitted the message, so a refusal
// of this attempt cannot prove that nothing was sent.
func (decision submissionDecision) hasPossibleEarlierSubmission() bool {
	return decision.maximumPriorSubmissions > 0
}

// canResendUnknownOutcome reports that a submission of this attempt whose answer is lost still leaves
// room under the send cap for another one.
func (decision submissionDecision) canResendUnknownOutcome() bool {
	return decision.maximumPriorSubmissions+1 < decision.maxSends
}

// claimSubmission records the claim before SMTP DATA; false means nothing may be submitted in this attempt.
func (decision submissionDecision) claimSubmission(call sdkgo.Call) bool {
	return recordSubmissionMarker(call, decision.maximumPriorSubmissions+1) == nil
}

// recordNotSubmitted records that this attempt's submission, if any, was provably not accepted, so the
// earlier count stands for the next attempt.
func (decision submissionDecision) recordNotSubmitted(call sdkgo.Call) {
	// A failed record leaves the claim or no marker, so the next attempt can only overcount.
	_ = recordSubmissionMarker(call, decision.maximumPriorSubmissions)
}

// recordAccepted records the accepted submission, so a replay of this Step execution never submits again.
func (decision submissionDecision) recordAccepted(call sdkgo.Call) {
	// A lost record leaves the claim, so a replay may resubmit only while the send cap allows.
	_ = call.Context.RecordHeartbeat(singleSubmissionMarker{
		SubmittedCallID: call.ID, Attempt: call.Context.Attempt(), SubmissionCount: decision.maximumPriorSubmissions + 1, IsAccepted: true,
	})
}

func recordSubmissionMarker(call sdkgo.Call, submissionCount int32) error {
	return call.Context.RecordHeartbeat(singleSubmissionMarker{
		SubmittedCallID: call.ID, Attempt: call.Context.Attempt(), SubmissionCount: submissionCount,
	})
}

// singleSubmissionBranches names an operation's branches for the shared submission outcome mapping.
type singleSubmissionBranches struct {
	sent             sdkgo.BranchID
	providerRejected sdkgo.BranchID
}

// submissionAttemptBeforeSend returns the attempt for a decision that does not permit submitting.
func submissionAttemptBeforeSend(decision submissionDecision, operation string, message SentMessage) (sdkgo.MutationAttempt[SentMessage], bool) {
	switch decision.kind {
	case submissionAllowed:
		return sdkgo.MutationAttempt[SentMessage]{}, false
	case submissionCheckpointUntrusted:
		return sdkgo.NewMutationRetry[SentMessage](emailFailure(sdkgo.FailureAvailability, operation,
			"the submission checkpoint could not be read; nothing was submitted in this attempt"), 0), true
	case submissionAlreadyAccepted:
		return selectUncertainWithoutSubmitting(operation, message,
			"an earlier attempt of this Step submitted the message and the server accepted it, but its result was lost; it is not submitted again"), true
	default:
		return selectUncertainWithoutSubmitting(operation, message,
			"earlier attempts of this Step may have submitted the message as often as its send cap allows, so it is not submitted again"), true
	}
}

// selectUncertainWithoutSubmitting selects uncertain for an attempt that did not submit after an earlier possible submission.
func selectUncertainWithoutSubmitting(operation string, message SentMessage, reason string) sdkgo.MutationAttempt[SentMessage] {
	// This attempt did not submit, so the earlier attempt's Date header is unknown.
	message.SentAt = time.Time{}
	return sdkgo.NewMutationUncertain(message, emailFailure(sdkgo.FailureTransport, operation, reason),
		sdkgo.Receipt{ProviderObjectID: message.MessageID})
}

// retryBeforeSubmitting records the earlier count, so this attempt is not counted, and returns Retry.
func retryBeforeSubmitting(call sdkgo.Call, decision submissionDecision, failure sdkgo.Failure) sdkgo.MutationAttempt[SentMessage] {
	decision.recordNotSubmitted(call)
	return sdkgo.NewMutationRetry[SentMessage](failure, 0)
}

// refuseBeforeSubmitting selects branch for a definite failure before submitting, or uncertain after an
// earlier possible submission, whose outcome the failure cannot settle.
func refuseBeforeSubmitting(
	decision submissionDecision, branch sdkgo.BranchID, failure sdkgo.Failure, operation string, message SentMessage,
) sdkgo.MutationAttempt[SentMessage] {
	if decision.hasPossibleEarlierSubmission() {
		return selectUncertainWithoutSubmitting(operation, message,
			"an earlier attempt of this Step may have submitted the message, and this attempt could not submit it: "+failure.Message)
	}
	return sdkgo.NewMutationBranch(branch, SentMessage{}, &failure, sdkgo.Receipt{})
}

// submitOnce claims the submission, submits, and maps the outcome. An answer lost after the final dot
// returns Retry while the send cap allows another submission and selects uncertain at the cap. A
// provable non-acceptance is not counted: a temporary one returns Retry, and a refusal selects
// providerRejected only when no earlier submission may have been accepted.
func submitOnce(
	call sdkgo.Call, decision submissionDecision, operation string, message SentMessage, branches singleSubmissionBranches,
	submit func() submissionResult,
) sdkgo.MutationAttempt[SentMessage] {
	if !decision.claimSubmission(call) {
		return sdkgo.NewMutationRetry[SentMessage](emailFailure(sdkgo.FailureAvailability, operation,
			"Dex did not record the submission checkpoint; nothing was submitted"), 0)
	}
	result := submit()
	receipt := sdkgo.Receipt{ProviderObjectID: message.MessageID}
	switch result.outcome {
	case submissionAccepted:
		decision.recordAccepted(call)
		return sdkgo.NewMutationBranch(branches.sent, message, nil, receipt)
	case submissionUncertain:
		if decision.canResendUnknownOutcome() {
			// The claim stays the last marker, so the next attempt counts this submission.
			return sdkgo.NewMutationRetry[SentMessage](result.failure, 0)
		}
		return sdkgo.NewMutationUncertain(message, result.failure, receipt)
	}
	decision.recordNotSubmitted(call)
	switch {
	case result.isRetryable:
		return sdkgo.NewMutationRetry[SentMessage](result.failure, 0)
	case decision.hasPossibleEarlierSubmission():
		return selectUncertainWithoutSubmitting(operation, message,
			"an earlier attempt of this Step may have submitted the message, and the server refused this one: "+result.failure.Message)
	default:
		return sdkgo.NewMutationBranch(branches.providerRejected, SentMessage{}, &result.failure, sdkgo.Receipt{})
	}
}
