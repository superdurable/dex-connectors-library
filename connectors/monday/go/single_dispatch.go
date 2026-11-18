// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"context"
	"errors"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// fencedWrite is one attempt of a createItem or addUpdate behind the shared single-dispatch fence.
// monday.com's idempotency cache can be bypassed, so an unknown outcome resends under the same key
// only while the send cap allows, and selects uncertain at the cap.
type fencedWrite struct {
	fence *singledispatch.Fence
	// decision is the fence's checkpoint decision for this attempt.
	decision singledispatch.Decision
	// canResendUnknownOutcome is true while another send stays within the cap.
	canResendUnknownOutcome bool
	// hasPossibleEarlierSend is true when an earlier send of this call may have taken effect.
	hasPossibleEarlierSend bool
}

// fencedWriteBranches names an operation's refusal branches for the shared outcome mapping.
type fencedWriteBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// startFencedWrite decides whether this attempt may send. It returns a terminal attempt when it may not.
func startFencedWrite[OUT any](call sdkgo.Call, definition sdkgo.MutationDefinition, operationID string, echo OUT, receipt sdkgo.Receipt, defect sdkgo.BranchID) (*fencedWrite, sdkgo.MutationAttempt[OUT], bool) {
	fence, err := singledispatch.NewFence(call, definition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		failure := mondayFailure(sdkgo.FailureLocalDefect, operationID, "monday.com dispatch fence could not be constructed")
		if call.Context != nil && call.Context.Attempt() > 1 {
			return nil, sdkgo.NewMutationUncertain(echo, failure, receipt), true
		}
		return nil, sdkgo.NewMutationBranch(defect, echo, &failure, sdkgo.Receipt{}), true
	}
	decision := fence.Decide()
	switch {
	case decision.Kind == singledispatch.FailClosed:
		return nil, sdkgo.NewMutationRetry[OUT](mondayFailure(sdkgo.FailureAvailability, operationID,
			"monday.com dispatch checkpoint is unreadable, so nothing was sent in this attempt"), 0), true
	case decision.Kind == singledispatch.ReadBackRequired:
		return nil, sdkgo.NewMutationUncertain(echo, mondayFailure(sdkgo.FailureTransport, operationID,
			"earlier attempts of this Step may have sent the request and the send cap is reached, so it is not sent again"), receipt), true
	case decision.Marker.ProviderID != "":
		return nil, sdkgo.NewMutationUncertain(echo, mondayFailure(sdkgo.FailureTransport, operationID,
			"an earlier attempt of this Step was accepted, so the request is not sent again"), receipt), true
	}
	return &fencedWrite{
		fence: fence, decision: decision,
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
		hasPossibleEarlierSend:  decision.MaximumPriorSends > 0,
	}, sdkgo.MutationAttempt[OUT]{}, false
}

// canRecordNotSent reports whether a pre-send Retry may checkpoint notSent. After a possible earlier
// send, even a sendAllowed decision after canResend, notSent would hide that send.
func (write *fencedWrite) canRecordNotSent() bool {
	return write.decision.Kind == singledispatch.SendAllowed && !write.hasPossibleEarlierSend
}

// preSendFailureAttempt maps a credential failure that ended the attempt before any send.
func preSendFailureAttempt[OUT any](write *fencedWrite, operationID string, result mondayExchange, echo OUT, branches fencedWriteBranches) sdkgo.MutationAttempt[OUT] {
	if write.canRecordNotSent() {
		if err := write.fence.RecordNotSent(); err != nil {
			return sdkgo.NewMutationRetry[OUT](mondayFailure(sdkgo.FailureAvailability, operationID, "monday.com not-sent checkpoint could not be recorded"), 0)
		}
	}
	switch {
	case result.outcome == exchangeRetry:
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter)
	case write.hasPossibleEarlierSend:
		return sdkgo.NewMutationUncertain(echo, result.failure, sdkgo.Receipt{})
	case result.outcome == exchangeRejected:
		return sdkgo.NewMutationBranch(branches.providerRejected, echo, &result.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(branches.defect, echo, &result.failure, sdkgo.Receipt{})
	}
}

// dispatchFencedWrite sends once behind the fence; it returns a terminal attempt when the fence forbids sending.
func dispatchFencedWrite[OUT any](write *fencedWrite, client *Client, call sdkgo.Call, credentials Credentials, operationID string, request graphQLRequest, echo OUT, defect sdkgo.BranchID) (mondayExchange, sdkgo.MutationAttempt[OUT], bool) {
	var result mondayExchange
	if !hasRequestBudget(call.Context, requestDeadlineMargin+minimumMutationRequestWindow) {
		if write.canRecordNotSent() {
			if err := write.fence.RecordNotSent(); err != nil {
				return result, sdkgo.NewMutationRetry[OUT](mondayFailure(sdkgo.FailureAvailability, operationID, "monday.com not-sent checkpoint could not be recorded"), 0), true
			}
		}
		return result, sdkgo.NewMutationRetry[OUT](mondayFailure(sdkgo.FailureAvailability, operationID, "not enough time remains to send the mutation"), 0), true
	}
	request.isFencedWrite = true
	err := write.fence.Dispatch(func(requestContext context.Context) error {
		result = client.sendWithRejectionRefresh(requestContext, call, credentials, operationID, request)
		return nil
	})
	switch {
	case err == nil:
		return result, sdkgo.MutationAttempt[OUT]{}, false
	case errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime):
		return result, sdkgo.NewMutationRetry[OUT](mondayFailure(sdkgo.FailureAvailability, operationID, "dispatch checkpoint could not be recorded, so nothing was sent"), 0), true
	default:
		failure := mondayFailure(sdkgo.FailureLocalDefect, operationID, "monday.com dispatch was not allowed")
		if write.hasPossibleEarlierSend {
			return result, sdkgo.NewMutationUncertain(echo, failure, sdkgo.Receipt{}), true
		}
		return result, sdkgo.NewMutationBranch(defect, echo, &failure, sdkgo.Receipt{}), true
	}
}

// attemptForWrite maps every sent outcome except an accepted one: a definite outcome selects its
// branch, and an unknown one resends while the send cap allows.
func attemptForWrite[OUT any](write *fencedWrite, operationID string, result mondayExchange, mutationField string, echo OUT, receipt sdkgo.Receipt, branches fencedWriteBranches) (sdkgo.MutationAttempt[OUT], bool) {
	if result.isSuccessBodyUnreadable || hasMutationFieldValue(result.response.data, mutationField) {
		// monday.com may have accepted the write, so a resend could create it again.
		failure := result.failure
		if result.outcome == exchangeSucceeded {
			failure = mondayFailure(sdkgo.FailureProtocol, operationID, "monday.com accepted the write but returned an unusable result")
		}
		return sdkgo.NewMutationUncertain(echo, failure, receipt), true
	}
	switch classifyWrite(result, mutationField) {
	case writeAccepted:
		return sdkgo.MutationAttempt[OUT]{}, false
	case writeNotProcessed:
		if err := write.fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[OUT](mondayFailure(sdkgo.FailureAvailability, operationID, "monday.com retry checkpoint could not be recorded"), result.retryAfter), true
		}
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case writeRefused:
		branch := branches.providerRejected
		switch result.outcome {
		case exchangeNotFound:
			branch = branches.notFound
		case exchangeDefect:
			branch = branches.defect
		}
		return rejectOrSelectUncertain(write, operationID, branch, echo, result.failure, receipt), true
	default:
		return resendOrSelectUncertain(write, echo, result.failure, result.retryAfter, receipt), true
	}
}

// recordAcceptedWrite saves the accepted identity so a replayed attempt never resends it.
func recordAcceptedWrite(write *fencedWrite, providerID string) {
	// The accepted result stands; a lost identity checkpoint only keeps the claim.
	_ = write.fence.RecordProviderID(providerID)
}

// resendOrSelectUncertain retries an unknown outcome while the send cap allows another send.
func resendOrSelectUncertain[OUT any](write *fencedWrite, echo OUT, failure sdkgo.Failure, retryAfter time.Duration, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if write.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[OUT](failure, retryAfter)
	}
	return sdkgo.NewMutationUncertain(echo, failure, receipt)
}

// rejectOrSelectUncertain selects a refusal branch only when no earlier send may have written.
func rejectOrSelectUncertain[OUT any](write *fencedWrite, operationID string, branch sdkgo.BranchID, echo OUT, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if write.hasPossibleEarlierSend {
		return sdkgo.NewMutationUncertain(echo, failure, receipt)
	}
	if err := write.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[OUT](mondayFailure(sdkgo.FailureAvailability, operationID, "monday.com rejection checkpoint could not be recorded"), 0)
	}
	return sdkgo.NewMutationBranch(branch, echo, &failure, receipt)
}

// hasRequestBudget reports whether ctx leaves at least minimum before its deadline.
func hasRequestBudget(ctx context.Context, minimum time.Duration) bool {
	deadline, hasDeadline := ctx.Deadline()
	return !hasDeadline || time.Until(deadline) >= minimum
}
