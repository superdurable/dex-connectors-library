// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup

import (
	"context"
	"errors"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	// writeReconciliationSkew tolerates a difference between the Worker's and ClickUp's clocks.
	writeReconciliationSkew = 5 * time.Minute
	// unknownWriteSettleDelay lets an unconfirmed write appear before the next attempt reads it back.
	unknownWriteSettleDelay = 5 * time.Second
)

// fencedWrite is one attempt of an unkeyed ClickUp write behind the shared single-dispatch fence.
type fencedWrite struct {
	fence *singledispatch.Fence
	// decision is the fence's checkpoint decision for this attempt.
	decision singledispatch.Decision
	// hasPossibleEarlierSend is true when an earlier send of this call may have taken effect.
	hasPossibleEarlierSend bool
}

// singleDispatchBranches names an operation's refusal branches for the shared outcome mapping.
type singleDispatchBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// readBackStatus is what reading back an earlier send of this call found.
type readBackStatus uint8

const (
	// readBackFound means exactly one earlier write of this call is visible.
	readBackFound readBackStatus = iota + 1
	// readBackMissing means read-back could not confirm an earlier write.
	readBackMissing
	// readBackAmbiguous means several writes match, so one may be this call's.
	readBackAmbiguous
	// readBackRetry means the read itself should be repeated.
	readBackRetry
	// readBackParentNotFound means the List or task that the write targets does not exist.
	readBackParentNotFound
)

// startFencedWrite decides whether this attempt may send. It returns a terminal attempt when it may not.
func startFencedWrite[OUT any](call sdkgo.Call, definition sdkgo.MutationDefinition, operationID string, echo OUT, defect sdkgo.BranchID) (*fencedWrite, sdkgo.MutationAttempt[OUT], bool) {
	fence, err := singledispatch.NewFence(call, definition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return nil, sdkgo.NewMutationBranch(defect, echo, clickupFailurePointer(sdkgo.FailureLocalDefect, operationID, "ClickUp dispatch fence could not be constructed"), sdkgo.Receipt{}), true
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return nil, sdkgo.NewMutationRetry[OUT](clickupFailure(sdkgo.FailureAvailability, operationID,
			"the ClickUp dispatch checkpoint is unreadable, so nothing was sent in this attempt"), 0), true
	}
	return &fencedWrite{fence: fence, decision: decision, hasPossibleEarlierSend: decision.MaximumPriorSends > 0}, sdkgo.MutationAttempt[OUT]{}, false
}

// needsReadBack reports that an earlier send may have taken effect, so this attempt reads back first.
func (write *fencedWrite) needsReadBack() bool {
	return write.decision.Kind != singledispatch.SendAllowed || write.decision.Marker.ProviderID != ""
}

// canResendAfterMissingReadBack reports that a read-back that confirms nothing still permits one send.
func (write *fencedWrite) canResendAfterMissingReadBack() bool {
	return write.decision.Kind == singledispatch.ReadBackThenResendAllowed && write.decision.Marker.ProviderID == ""
}

// credentialFailureAttempt maps a credential failure before the send.
func credentialFailureAttempt[OUT any](write *fencedWrite, operationID string, failure sdkgo.Failure, echo OUT, defect sdkgo.BranchID) sdkgo.MutationAttempt[OUT] {
	// notSent after a possible earlier send would hide it, so only a first-send decision records it.
	if write.decision.Kind == singledispatch.SendAllowed {
		if err := write.fence.RecordNotSent(); err != nil {
			return sdkgo.NewMutationRetry[OUT](clickupFailure(sdkgo.FailureAvailability, operationID, "ClickUp setup failed and the not-sent checkpoint could not be recorded"), 0)
		}
	}
	return defectOrSelectUncertain(write, defect, echo, failure, sdkgo.Receipt{})
}

// dispatchFencedWrite sends once behind the fence; it returns a terminal attempt when the fence forbids sending.
func dispatchFencedWrite[OUT any](write *fencedWrite, client *Client, credentials Credentials, operationID string, request clickupRequest, echo OUT, defect sdkgo.BranchID) (clickupExchange, sdkgo.MutationAttempt[OUT], bool) {
	var result clickupExchange
	err := write.fence.Dispatch(func(requestContext context.Context) error {
		result = client.exchange(requestContext, credentials, operationID, request)
		return nil
	})
	switch {
	case err == nil:
		return result, sdkgo.MutationAttempt[OUT]{}, false
	case errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime):
		return result, sdkgo.NewMutationRetry[OUT](clickupFailure(sdkgo.FailureAvailability, operationID,
			"the ClickUp dispatch checkpoint or request budget is unavailable, so nothing was sent"), 0), true
	default:
		return result, defectOrSelectUncertain(write, defect, echo, clickupFailure(sdkgo.FailureLocalDefect, operationID, "ClickUp dispatch was not allowed"), sdkgo.Receipt{}), true
	}
}

// attemptForUnacceptedWrite maps a sent write without a 2xx: a definite outcome selects its branch,
// and an unknown one retries, so the next attempt reads back, then resends within the cap or selects uncertain.
func attemptForUnacceptedWrite[OUT any](write *fencedWrite, operationID string, result clickupExchange, echo OUT, receipt sdkgo.Receipt, branches singleDispatchBranches) sdkgo.MutationAttempt[OUT] {
	switch result.classifyWrite() {
	case writeNotApplied:
		if err := write.fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[OUT](clickupFailure(sdkgo.FailureAvailability, operationID, "ClickUp retry checkpoint could not be recorded"), result.retryAfter)
		}
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter)
	case writeDefect:
		// The request was never built, so nothing was sent; a lost checkpoint only keeps the claim.
		_ = write.fence.RecordCanResend()
		return defectOrSelectUncertain(write, branches.defect, echo, result.failure, receipt)
	case writeNotFound:
		return rejectOrSelectUncertain(write, operationID, branches.notFound, echo, result.failure, receipt)
	case writeRejected:
		return rejectOrSelectUncertain(write, operationID, branches.providerRejected, echo, result.failure, receipt)
	default:
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[OUT](result.failure, max(result.retryAfter, unknownWriteSettleDelay))
	}
}

// recordAcceptedWrite saves the accepted identity so a replayed attempt never resends it.
func recordAcceptedWrite(write *fencedWrite, providerID string) {
	// The accepted result stands; a lost identity checkpoint only keeps the claim.
	_ = write.fence.RecordProviderID(providerID)
}

// rejectOrSelectUncertain selects a refusal branch only when no earlier send may have written.
func rejectOrSelectUncertain[OUT any](write *fencedWrite, operationID string, branch sdkgo.BranchID, echo OUT, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if write.hasPossibleEarlierSend {
		return sdkgo.NewMutationUncertain(echo, failure, receipt)
	}
	if err := write.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[OUT](clickupFailure(sdkgo.FailureAvailability, operationID, "ClickUp rejection checkpoint could not be recorded"), 0)
	}
	return sdkgo.NewMutationBranch(branch, echo, &failure, receipt)
}

// defectOrSelectUncertain selects defect only when no earlier send may have written.
func defectOrSelectUncertain[OUT any](write *fencedWrite, defect sdkgo.BranchID, echo OUT, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if write.hasPossibleEarlierSend {
		return sdkgo.NewMutationUncertain(echo, failure, receipt)
	}
	return sdkgo.NewMutationBranch(defect, echo, &failure, receipt)
}

// readBackCreatedSince is the earliest creation time an earlier send of this Step can have; zero means unknown.
func readBackCreatedSince(call sdkgo.Call) time.Time {
	since := call.Context.FirstAttemptAt().Add(-writeReconciliationSkew)
	if since.UnixMilli() <= 0 {
		return time.Time{}
	}
	return since
}
