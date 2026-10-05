// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// singleDispatchMarker is the Dex heartbeat checkpoint recorded before a non-idempotent request is sent.
type singleDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"reamazeDispatchedCallId"`
}

// singleDispatchBranches names an operation's branches for the shared single-dispatch outcome mapping.
type singleDispatchBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// uncertainNotFoundMessage explains a reconciliation that found no trace of the earlier dispatch.
const uncertainNotFoundMessage = "an earlier attempt of this Step sent the request without a confirmed outcome, and Re:amaze shows no record with its key, so it is not sent again"

// hasEarlierDispatch is checked before credentials resolve, so a credential failure cannot report a sent write as defect.
func hasEarlierDispatch(call sdkgo.Call) bool {
	var marker singleDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	return err != nil || isMarkerFound
}

// singleDispatchAttemptForCredentials never selects defect after an earlier dispatch, which Re:amaze may have applied.
func singleDispatchAttemptForCredentials[OUT any](operation string, defect sdkgo.BranchID, value OUT, err error, isAlreadyDispatched bool, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if !isAlreadyDispatched || errors.Is(err, errReamazeCredentialsUnavailable) {
		return credentialMutationAttempt(operation, defect, value, err)
	}
	return sdkgo.NewMutationUncertain(value, reamazeFailure(sdkgo.FailureAuthentication, operation,
		"an earlier attempt of this Step sent the request, and the connection credentials can no longer read it back"), receipt)
}

// recordSingleDispatch records the marker before the first send; false means nothing may be sent yet.
func recordSingleDispatch(call sdkgo.Call) bool {
	return call.Context.RecordHeartbeat(singleDispatchMarker{DispatchedCallID: call.ID}) == nil
}

// singleDispatchAttemptNotRecorded retries without sending, because the marker is not stored.
func singleDispatchAttemptNotRecorded[OUT any](operation string) sdkgo.MutationAttempt[OUT] {
	return sdkgo.NewMutationRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, operation, "Dex did not record the dispatch checkpoint; nothing was sent"), 0)
}

// singleDispatchAttemptForSend maps a sent request; only a provable non-application releases the marker.
func singleDispatchAttemptForSend[OUT any](call sdkgo.Call, result reamazeExchange, receipt sdkgo.Receipt, branches singleDispatchBranches) (sdkgo.MutationAttempt[OUT], bool) {
	var zero OUT
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent:
		releaseSingleDispatch(call)
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeUnavailable, exchangeInvalid:
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeNotFound:
		if branches.notFound != "" {
			return sdkgo.NewMutationBranch(branches.notFound, zero, &result.failure, receipt), true
		}
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt), true
	case exchangeDefect:
		releaseSingleDispatch(call)
		return sdkgo.NewMutationBranch(branches.defect, zero, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt), true
	}
}

// releaseSingleDispatch clears the marker after Re:amaze provably did not apply the request.
func releaseSingleDispatch(call sdkgo.Call) {
	// A failed clear leaves the marker, so the next attempt reconciles instead of resending.
	_ = call.Context.RecordHeartbeat(nil)
}

// singleDispatchAttemptForReconciliationRead maps the read-back of an earlier dispatch; it never resends.
func singleDispatchAttemptForReconciliationRead[OUT any](result reamazeExchange, receipt sdkgo.Receipt, operation string, notFound sdkgo.BranchID) (sdkgo.MutationAttempt[OUT], bool) {
	var zero OUT
	switch {
	case result.outcome == exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case result.isRetryableRead():
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case result.outcome == exchangeNotFound && notFound != "":
		return sdkgo.NewMutationBranch(notFound, zero, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationUncertain(zero, reamazeFailure(result.failure.Kind, operation,
			"an earlier attempt of this Step sent the request, and it could not be read back: "+result.failure.Message), receipt), true
	}
}

// dispatchKey is the Re:amaze-visible identity of one Step execution's write, derived from its idempotency key.
func dispatchKey(call sdkgo.Call) string {
	key := string(call.IdempotencyKey)
	if key == "" {
		key = string(call.ID)
	}
	return "dex-" + key
}
