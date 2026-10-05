// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// operationBranches names the branch each unsuccessful exchange selects for one operation.
type operationBranches struct {
	notFound        sdkgo.BranchID
	rejected        sdkgo.BranchID
	invalidResponse sdkgo.BranchID
	defect          sdkgo.BranchID
}

func (branches operationBranches) branchFor(outcome exchangeOutcome) sdkgo.BranchID {
	switch outcome {
	case exchangeNotFound:
		if branches.notFound != "" {
			return branches.notFound
		}
		return branches.rejected
	case exchangeRejected:
		return branches.rejected
	case exchangeInvalid:
		if branches.invalidResponse != "" {
			return branches.invalidResponse
		}
		return branches.rejected
	default:
		return branches.defect
	}
}

// isRetryableExchange reports an exchange that Pipedrive did not conclusively answer.
func isRetryableExchange(outcome exchangeOutcome) bool {
	return outcome == exchangeNotSent || outcome == exchangeRateLimited || outcome == exchangeUnavailable
}

// queryAttemptForExchange maps an unsuccessful read: anything Pipedrive did not conclusively answer is retried.
func queryAttemptForExchange[T any](result pipedriveExchange, receipt sdkgo.Receipt, branches operationBranches) sdkgo.QueryAttempt[T] {
	if isRetryableExchange(result.outcome) {
		return sdkgo.NewQueryRetry[T](result.failure, result.retryAfter)
	}
	var zero T
	return sdkgo.NewQueryBranch(branches.branchFor(result.outcome), zero, &result.failure, receipt)
}

// repeatableMutationAttemptForExchange maps an unsuccessful write that converges when sent again.
func repeatableMutationAttemptForExchange[T any](result pipedriveExchange, receipt sdkgo.Receipt, branches operationBranches) sdkgo.MutationAttempt[T] {
	if isRetryableExchange(result.outcome) {
		return sdkgo.NewMutationRetry[T](result.failure, result.retryAfter)
	}
	var zero T
	return sdkgo.NewMutationBranch(branches.branchFor(result.outcome), zero, &result.failure, receipt)
}

// queryAttemptForSession maps a session that could not start; nothing was sent.
func queryAttemptForSession[T any](failure *sessionFailure, branches operationBranches) sdkgo.QueryAttempt[T] {
	var zero T
	switch failure.route {
	case sessionRetry:
		return sdkgo.NewQueryRetry[T](failure.failure, 0)
	case sessionRejected:
		return sdkgo.NewQueryBranch(branches.rejected, zero, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewQueryBranch(branches.defect, zero, &failure.failure, sdkgo.Receipt{})
	}
}

// mutationAttemptForSession maps a session that could not start; nothing was sent.
func mutationAttemptForSession[T any](failure *sessionFailure, branches operationBranches) sdkgo.MutationAttempt[T] {
	var zero T
	switch failure.route {
	case sessionRetry:
		return sdkgo.NewMutationRetry[T](failure.failure, 0)
	case sessionRejected:
		return sdkgo.NewMutationBranch(branches.rejected, zero, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(branches.defect, zero, &failure.failure, sdkgo.Receipt{})
	}
}

// singleDispatchMarker is the Dex heartbeat checkpoint recorded before a create is sent.
type singleDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"pipedriveDispatchedCallId"`
}

// hasEarlierDispatch reports an earlier attempt's marker; an unreadable checkpoint counts as one, failing closed.
func hasEarlierDispatch(call sdkgo.Call) bool {
	var marker singleDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	return err != nil || isMarkerFound
}

// Dex accepts the marker before persisting it, so a Worker lost in that instant can still send twice.
func recordDispatch(call sdkgo.Call) error {
	return call.Context.RecordHeartbeat(singleDispatchMarker{DispatchedCallID: call.ID})
}

// releaseDispatch clears the marker after Pipedrive provably did not apply the create.
func releaseDispatch(call sdkgo.Call) {
	// A failed clear leaves the marker, so the next attempt treats the create as possibly sent.
	_ = call.Context.RecordHeartbeat(nil)
}

func dispatchCheckpointRetry[T any](operation string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](providerFailure(operation, sdkgo.FailureAvailability, "Dex did not record the dispatch checkpoint; nothing was sent"), 0)
}
