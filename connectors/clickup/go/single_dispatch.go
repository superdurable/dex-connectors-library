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
// SendAllowed after a canResend marker can still carry earlier possible sends.
func (write *fencedWrite) needsReadBack() bool {
	return write.decision.Kind != singledispatch.SendAllowed || write.hasPossibleEarlierSend || write.decision.Marker.ProviderID != ""
}

// canResendAfterMissingReadBack reports that a read-back that confirms nothing still permits one send.
func (write *fencedWrite) canResendAfterMissingReadBack() bool {
	isSendDecision := write.decision.Kind == singledispatch.ReadBackThenResendAllowed || write.decision.Kind == singledispatch.SendAllowed
	return isSendDecision && write.decision.Marker.ProviderID == ""
}

// credentialFailureAttempt maps a credential failure before this attempt's send. After a possible
// earlier send it never records notSent: a transient failure retries and a terminal one selects uncertain.
// Before any send, a transient failure records notSent and retries, and a terminal one selects defect.
func credentialFailureAttempt[OUT any](write *fencedWrite, operationID string, failure sdkgo.Failure, echo OUT, defect sdkgo.BranchID) sdkgo.MutationAttempt[OUT] {
	isTerminal := failure.Kind == sdkgo.FailureAuthentication
	if write.needsReadBack() {
		if isTerminal {
			return sdkgo.NewMutationUncertain(echo, clickupFailure(failure.Kind, operationID,
				"an earlier attempt of this Step may have sent the write and the connection credentials cannot read it back: "+failure.Message), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[OUT](clickupFailure(failure.Kind, operationID,
			"an earlier attempt of this Step may have sent the write and the connection credentials are unavailable to read it back: "+failure.Message), 0)
	}
	if err := write.fence.RecordNotSent(); err != nil {
		return sdkgo.NewMutationRetry[OUT](clickupFailure(sdkgo.FailureAvailability, operationID, "ClickUp setup failed and the not-sent checkpoint could not be recorded"), 0)
	}
	if !isTerminal {
		return sdkgo.NewMutationRetry[OUT](failure, 0)
	}
	return sdkgo.NewMutationBranch(defect, echo, &failure, sdkgo.Receipt{})
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
	case writeAcceptedUnreadable:
		// ClickUp accepted the write but its ID is unreadable, so it is never sent again.
		return sdkgo.NewMutationUncertain(echo, clickupFailure(result.failure.Kind, operationID,
			"ClickUp accepted the write but its response is unusable, so it is not sent again: "+result.failure.Message), receipt)
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

// retryAcceptedReadBackOrSelectUncertain retries a 2xx with an unusable body only when its ID is saved,
// so the next attempt reads it back and never resends it.
func retryAcceptedReadBackOrSelectUncertain[OUT any](write *fencedWrite, operationID string, providerID string, echo OUT, message string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if providerID != "" && write.fence.RecordProviderID(providerID) == nil {
		return sdkgo.NewMutationRetry[OUT](clickupFailure(sdkgo.FailureProtocol, operationID, message+", so the next attempt reads it back"), unknownWriteSettleDelay)
	}
	return sdkgo.NewMutationUncertain(echo, clickupFailure(sdkgo.FailureProtocol, operationID, message+", so it is not sent again"), receipt)
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
