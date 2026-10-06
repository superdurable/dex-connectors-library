// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// singleDispatchMarker is the Dex heartbeat checkpoint recorded before a non-idempotent request is sent.
type singleDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"hiverDispatchedCallId"`
}

// singleDispatchBranches names an operation's branches for the shared single-dispatch outcome mapping.
type singleDispatchBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// sendOnce sends unless an earlier attempt recorded the marker; a non-nil attempt is terminal.
// Dex may store the marker after the request leaves, so a lost Worker can still send twice.
func sendOnce[OUT any](call sdkgo.Call, client *Client, credentials Credentials, operation string, request hiverRequest, branches singleDispatchBranches) (hiverExchange, *sdkgo.MutationAttempt[OUT]) {
	var zero OUT
	if isSingleDispatchRecorded(call) {
		attempt := sdkgo.NewMutationUncertain(zero, hiverFailure(sdkgo.FailureTransport, operation,
			"an earlier attempt of this Step may have sent the request, so it is not sent again"), client.receipt(call, hiverResponse{}, ""))
		return hiverExchange{}, &attempt
	}
	if result, isAvailable := client.awaitRequestSlot(call, operation); !isAvailable {
		attempt := sdkgo.NewMutationRetry[OUT](result.failure, 0)
		return result, &attempt
	}
	if err := call.Context.RecordHeartbeat(singleDispatchMarker{DispatchedCallID: call.ID}); err != nil {
		attempt := sdkgo.NewMutationRetry[OUT](hiverFailure(sdkgo.FailureAvailability, operation, "Dex did not record the dispatch checkpoint; nothing was sent"), 0)
		return hiverExchange{}, &attempt
	}
	result := client.send(call, credentials, operation, request)
	if attempt, isTerminal := singleDispatchAttemptForExchange[OUT](call, result, client.receipt(call, result.response, ""), branches); isTerminal {
		return result, &attempt
	}
	return result, nil
}

// isSingleDispatchRecorded reports an earlier attempt's marker; an unreadable marker counts as recorded.
func isSingleDispatchRecorded(call sdkgo.Call) bool {
	var marker singleDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	return err != nil || isMarkerFound
}

// singleDispatchAttemptForExchange maps a sent request: only a provable non-application is retried.
func singleDispatchAttemptForExchange[OUT any](call sdkgo.Call, result hiverExchange, receipt sdkgo.Receipt, branches singleDispatchBranches) (sdkgo.MutationAttempt[OUT], bool) {
	var zero OUT
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent:
		releaseSingleDispatch(call)
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeUnavailable, exchangeInvalid:
		return sdkgo.NewMutationUncertain(zero, result.failure, receipt), true
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(branches.notFound, zero, &result.failure, receipt), true
	case exchangeDefect:
		releaseSingleDispatch(call)
		return sdkgo.NewMutationBranch(branches.defect, zero, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt), true
	}
}

// releaseSingleDispatch clears the marker after Hiver provably did not apply the request.
func releaseSingleDispatch(call sdkgo.Call) {
	// A failed clear leaves the marker, so the next attempt selects uncertain instead of resending.
	_ = call.Context.RecordHeartbeat(nil)
}
