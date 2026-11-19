// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive

import (
	"net/http"

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

// repeatSafeWriteAttemptForExchange maps an unsuccessful repeat-safe write: a status that does not
// prove a refusal, such as 402, a redirect, or an unlisted 4xx, is retried like an unknown outcome.
func repeatSafeWriteAttemptForExchange[T any](result pipedriveExchange, receipt sdkgo.Receipt, branches operationBranches) sdkgo.MutationAttempt[T] {
	if isUnknownWriteOutcome(result) {
		return sdkgo.NewMutationRetry[T](result.failure, result.retryAfter)
	}
	return repeatableMutationAttemptForExchange[T](result, receipt, branches)
}

// isUnknownWriteOutcome reports a write rejection whose status does not prove Pipedrive refused it.
func isUnknownWriteOutcome(result pipedriveExchange) bool {
	return (result.outcome == exchangeRejected || result.outcome == exchangeNotFound) && !isDefiniteWriteRefusal(result)
}

// isDefiniteWriteRefusal reports a status that proves Pipedrive refused a write. The daily token
// budget 429 also counts: Pipedrive refuses the request before acting, until the budget resets.
func isDefiniteWriteRefusal(result pipedriveExchange) bool {
	if result.failure.Kind == sdkgo.FailureQuotaExhausted {
		return true
	}
	switch result.response.statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
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

func singleDispatchRetry[T any](operation string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](providerFailure(operation, sdkgo.FailureAvailability, "The single-dispatch fence could not authorize a send or retry; the next attempt must reconcile the checkpoint"), 0)
}
