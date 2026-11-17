// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

import (
	"context"
	"errors"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// fencedWrite is one attempt of a non-idempotent write behind the shared single-dispatch fence.
// Jira Service Management offers no lookup by Call ID, so an unknown outcome resends while the
// send cap allows and selects uncertain at the cap.
type fencedWrite struct {
	fence *singledispatch.Fence
	// decision is the fence's checkpoint decision for this attempt.
	decision singledispatch.Decision
	// canResendUnknownOutcome is true while another send stays within the cap.
	canResendUnknownOutcome bool
	// hasPossibleEarlierSend is true when an earlier send of this call may have taken effect.
	hasPossibleEarlierSend bool
}

// singleDispatchBranches names an operation's branches for the shared single-dispatch outcome mapping.
type singleDispatchBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// startFencedWrite decides whether this attempt may send. It returns a terminal attempt when it may not.
func startFencedWrite[OUT any](call sdkgo.Call, timeout time.Duration, operationID string, echo OUT, defect sdkgo.BranchID) (*fencedWrite, sdkgo.MutationAttempt[OUT], bool) {
	fence, err := singledispatch.NewFence(call, timeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return nil, sdkgo.NewMutationBranch(defect, echo, failurePointer(operationID, sdkgo.FailureLocalDefect, "Jira Service Management dispatch fence could not be constructed"), sdkgo.Receipt{}), true
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return nil, sdkgo.NewMutationRetry[OUT](newFailure(operationID, sdkgo.FailureAvailability, "Jira Service Management dispatch checkpoint is unreadable, so nothing was sent in this attempt"), 0), true
	case singledispatch.ReadBackRequired:
		return nil, sdkgo.NewMutationUncertain(echo, newFailure(operationID, sdkgo.FailureTransport,
			"earlier attempts of this Step may have sent the request and the send cap is reached, so it is not sent again"), singleDispatchReceipt(call)), true
	case singledispatch.ReadBackThenResendAllowed:
		if decision.Marker.ProviderID != "" {
			return nil, sdkgo.NewMutationUncertain(echo, newFailure(operationID, sdkgo.FailureTransport,
				"an earlier attempt of this Step was accepted, so the request is not sent again"), singleDispatchReceipt(call)), true
		}
	}
	return &fencedWrite{
		fence: fence, decision: decision,
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
		hasPossibleEarlierSend:  decision.MaximumPriorSends > 0,
	}, sdkgo.MutationAttempt[OUT]{}, false
}

// sessionFailureAttempt maps a credential or site failure before the send.
func sessionFailureAttempt[OUT any](write *fencedWrite, operationID string, failure *sessionFailure, echo OUT, defect sdkgo.BranchID) sdkgo.MutationAttempt[OUT] {
	// notSent after a possible earlier send would hide it, so only a first-send decision records it.
	if write.decision.Kind == singledispatch.SendAllowed {
		if err := write.fence.RecordNotSent(); err != nil {
			return sdkgo.NewMutationRetry[OUT](newFailure(operationID, sdkgo.FailureAvailability, "Jira Service Management setup failed and the not-sent checkpoint could not be recorded"), failure.retryAfter)
		}
	}
	if failure.isRetryable {
		return sdkgo.NewMutationRetry[OUT](failure.failure, failure.retryAfter)
	}
	return defectOrSelectUncertain(write, defect, echo, failure.failure, sdkgo.Receipt{})
}

// dispatchFencedWrite sends once behind the fence; it returns a terminal attempt when the fence forbids sending.
func dispatchFencedWrite[OUT any](write *fencedWrite, client *Client, session *operationSession, request providerRequest, operationID string, echo OUT, defect sdkgo.BranchID) (exchangeResult, sdkgo.MutationAttempt[OUT], bool) {
	var result exchangeResult
	err := write.fence.Dispatch(func(requestContext context.Context) error {
		fencedSession := *session
		fencedSession.context = requestContext
		result = client.exchange(&fencedSession, request)
		return nil
	})
	switch {
	case err == nil:
		return result, sdkgo.MutationAttempt[OUT]{}, false
	case errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime):
		return result, sdkgo.NewMutationRetry[OUT](newFailure(operationID, sdkgo.FailureAvailability, "Jira Service Management dispatch checkpoint or request budget is unavailable; nothing was sent"), 0), true
	default:
		return result, defectOrSelectUncertain(write, defect, echo, newFailure(operationID, sdkgo.FailureLocalDefect, "Jira Service Management dispatch was not allowed"), sdkgo.Receipt{}), true
	}
}

func singleDispatchReceipt(call sdkgo.Call) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName, ObservedAt: time.Now().UTC()}
}

// attemptForWrite maps a sent request: a definite outcome selects its branch, and an unknown one
// resends while the send cap allows.
func attemptForWrite[OUT any](write *fencedWrite, operationID string, classification writeClassification, echo OUT, receipt sdkgo.Receipt, branches singleDispatchBranches) (sdkgo.MutationAttempt[OUT], bool) {
	switch classification.outcome {
	case writeAccepted:
		return sdkgo.MutationAttempt[OUT]{}, false
	case writeRetry:
		if err := write.fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[OUT](newFailure(operationID, sdkgo.FailureAvailability, "Jira Service Management retry checkpoint could not be recorded"), classification.retryAfter), true
		}
		return sdkgo.NewMutationRetry[OUT](classification.failure, classification.retryAfter), true
	case writeUncertain:
		return resendOrSelectUncertain(write, echo, classification.failure, receipt), true
	case writeNotFound:
		branch := branches.notFound
		if branch == "" {
			branch = branches.providerRejected
		}
		return rejectOrSelectUncertain(write, operationID, branch, echo, classification.failure, receipt), true
	case writeDefect:
		// The request was never built, so nothing was sent; a lost checkpoint only keeps the claim.
		_ = write.fence.RecordCanResend()
		return defectOrSelectUncertain(write, branches.defect, echo, classification.failure, receipt), true
	default:
		return rejectOrSelectUncertain(write, operationID, branches.providerRejected, echo, classification.failure, receipt), true
	}
}

// resendOrSelectUncertain retries an unknown outcome while the send cap allows another send.
func resendOrSelectUncertain[OUT any](write *fencedWrite, echo OUT, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if write.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[OUT](failure, 0)
	}
	return sdkgo.NewMutationUncertain(echo, failure, receipt)
}

// rejectOrSelectUncertain selects a refusal branch only when no earlier send may have written.
func rejectOrSelectUncertain[OUT any](write *fencedWrite, operationID string, branch sdkgo.BranchID, echo OUT, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if write.hasPossibleEarlierSend {
		return sdkgo.NewMutationUncertain(echo, failure, receipt)
	}
	if err := write.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[OUT](newFailure(operationID, sdkgo.FailureAvailability, "Jira Service Management rejection checkpoint could not be recorded"), 0)
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
