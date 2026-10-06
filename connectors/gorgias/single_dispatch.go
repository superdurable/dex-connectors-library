// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// reconcileDelay lets a request Gorgias may still be processing finish before the next attempt looks for it.
	reconcileDelay = 2 * time.Second
	// reconcileLookupRetryDelay spaces repeated lookups, so a short outage or a slowly visible write is still found.
	reconcileLookupRetryDelay = 15 * time.Second
	// maximumReconcileDelay caps a Retry-After on a lookup inside the five-minute retry window.
	maximumReconcileDelay = 60 * time.Second
	// lastReconcileAttempt stays below the manifest's five attempts, so a lookup ends in uncertain, not a Step failure.
	lastReconcileAttempt = 4
)

// dispatchMarker is the Dex heartbeat checkpoint recorded before a non-idempotent request is sent.
type dispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"gorgiasDispatchedCallId"`
}

// singleDispatchBranches names an operation's branches for the shared single-dispatch outcome mapping.
type singleDispatchBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// hasEarlierDispatch reports whether an earlier attempt of this Step execution may have sent the request.
func hasEarlierDispatch(call sdkgo.Call) bool {
	var marker dispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	return err != nil || isMarkerFound
}

// reconcileCredentialFailureAttempt never reports a possibly sent write as a defect: it looks again later, or selects uncertain.
func reconcileCredentialFailureAttempt[OUT any](call sdkgo.Call, failure sdkgo.Failure, operation string) sdkgo.MutationAttempt[OUT] {
	return reconcileAgainOrUncertain[OUT](call, failure, gorgiasFailure(failure.Kind, operation,
		"an earlier attempt of this Step sent the request and the connection credentials are unavailable to confirm it, so it is not sent again"),
		0, sdkgo.Receipt{})
}

// isLaterAttempt reports a retry, whose earlier attempt may have sent the request before Dex stored the marker.
func isLaterAttempt(call sdkgo.Call) bool {
	return call.Context.Attempt() > 1
}

// unsentRequestAttempt maps a failed lookup made before this attempt recorded the marker; this attempt sent nothing.
func unsentRequestAttempt[OUT any](result gorgiasExchange, receipt sdkgo.Receipt, branches singleDispatchBranches) sdkgo.MutationAttempt[OUT] {
	var zero OUT
	switch result.outcome {
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable, exchangeInvalid:
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter)
	case exchangeNotFound:
		if branches.notFound != "" {
			return sdkgo.NewMutationBranch(branches.notFound, zero, &result.failure, receipt)
		}
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewMutationBranch(branches.defect, zero, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt)
	}
}

// recordDispatch stores the marker before the first send; an error means nothing may be sent yet.
func recordDispatch(call sdkgo.Call) error {
	return call.Context.RecordHeartbeat(dispatchMarker{DispatchedCallID: call.ID})
}

// dispatchNotRecordedAttempt retries without sending, because Dex did not store the marker.
func dispatchNotRecordedAttempt[OUT any](operation string) sdkgo.MutationAttempt[OUT] {
	return sdkgo.NewMutationRetry[OUT](gorgiasFailure(sdkgo.FailureAvailability, operation, "Dex did not record the dispatch checkpoint; nothing was sent"), 0)
}

// singleDispatchAttemptForSend resends only after a provable non-application; other unconfirmed outcomes retry to look for the write.
func singleDispatchAttemptForSend[OUT any](call sdkgo.Call, result gorgiasExchange, receipt sdkgo.Receipt, branches singleDispatchBranches) (sdkgo.MutationAttempt[OUT], bool) {
	var zero OUT
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent:
		releaseDispatch(call)
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeUnavailable, exchangeInvalid:
		return sdkgo.NewMutationRetry[OUT](result.failure, reconcileDelayAfter(result.retryAfter, reconcileDelay)), true
	case exchangeNotFound:
		if branches.notFound != "" {
			return sdkgo.NewMutationBranch(branches.notFound, zero, &result.failure, receipt), true
		}
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt), true
	case exchangeDefect:
		releaseDispatch(call)
		return sdkgo.NewMutationBranch(branches.defect, zero, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt), true
	}
}

// reconcileLookupFailureAttempt repeats a lookup Gorgias did not answer, within the lookup budget; a conclusive failure selects uncertain.
func reconcileLookupFailureAttempt[OUT any](call sdkgo.Call, result gorgiasExchange, receipt sdkgo.Receipt, operation string) sdkgo.MutationAttempt[OUT] {
	uncertainFailure := gorgiasFailure(result.failure.Kind, operation,
		"an earlier attempt of this Step sent the request and Gorgias could not confirm it, so it is not sent again: "+result.failure.Message)
	switch result.outcome {
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable, exchangeInvalid:
		return reconcileAgainOrUncertain[OUT](call, result.failure, uncertainFailure, result.retryAfter, receipt)
	default:
		var zero OUT
		return sdkgo.NewMutationUncertain(zero, uncertainFailure, receipt)
	}
}

// reconcileNotFoundAttempt looks again for an earlier attempt's write that Gorgias does not show yet, then selects uncertain.
func reconcileNotFoundAttempt[OUT any](call sdkgo.Call, receipt sdkgo.Receipt, operation string, resource string) sdkgo.MutationAttempt[OUT] {
	failure := gorgiasFailure(sdkgo.FailureTransport, operation,
		"an earlier attempt of this Step sent the request and Gorgias shows no "+resource+" with its external ID, so it is not sent again")
	return reconcileAgainOrUncertain[OUT](call, failure, failure, 0, receipt)
}

// reconcileAgainOrUncertain keeps the marker and retries the lookup until lastReconcileAttempt, which selects uncertain.
func reconcileAgainOrUncertain[OUT any](call sdkgo.Call, retryFailure sdkgo.Failure, uncertainFailure sdkgo.Failure, retryAfter time.Duration, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	// Recording the marker again keeps it for the next attempt; when Dex refuses it, uncertain is the safe answer.
	if call.Context.Attempt() < lastReconcileAttempt && recordDispatch(call) == nil {
		return sdkgo.NewMutationRetry[OUT](retryFailure, reconcileDelayAfter(retryAfter, reconcileLookupRetryDelay))
	}
	var zero OUT
	return sdkgo.NewMutationUncertain(zero, uncertainFailure, receipt)
}

// releaseDispatch clears the marker after Gorgias provably did not apply the request.
func releaseDispatch(call sdkgo.Call) {
	// A failed clear leaves the marker, so the next attempt reconciles instead of resending.
	_ = call.Context.RecordHeartbeat(nil)
}

// reconcileDelayAfter honors a Gorgias Retry-After of at least minimumDelay, capped by maximumReconcileDelay.
func reconcileDelayAfter(retryAfter time.Duration, minimumDelay time.Duration) time.Duration {
	return min(max(retryAfter, minimumDelay), maximumReconcileDelay)
}
