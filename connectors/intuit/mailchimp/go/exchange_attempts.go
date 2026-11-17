// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// repeatableBranches names an operation's branches; an empty notFound or invalidResponse means providerRejected.
type repeatableBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// queryAttemptForExchange returns the terminal Query attempt for every outcome except success.
func queryAttemptForExchange[OUT any](result mailchimpExchange, receipt sdkgo.Receipt, branches repeatableBranches) (sdkgo.QueryAttempt[OUT], bool) {
	var zero OUT
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.QueryAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewQueryRetry[OUT](result.failure, result.retryAfter), true
	default:
		return sdkgo.NewQueryBranch(branches.branchFor(result.outcome), zero, &result.failure, receipt), true
	}
}

// repeatableMutationAttemptForExchange retries every unconfirmed outcome of a write that sets absolute values,
// including a status that is not a definite refusal. An unusable 2xx keeps its declared invalidResponse branch.
func repeatableMutationAttemptForExchange[OUT any](result mailchimpExchange, receipt sdkgo.Receipt, branches repeatableBranches) (sdkgo.MutationAttempt[OUT], bool) {
	var zero OUT
	switch {
	case result.outcome == exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case result.outcome != exchangeInvalid && result.isOutcomeUnknownAfterDispatch():
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	default:
		return sdkgo.NewMutationBranch(branches.branchFor(result.outcome), zero, &result.failure, receipt), true
	}
}

// isOutcomeUnknownAfterDispatch reports an outcome that does not prove Mailchimp refused or skipped a write.
func (result mailchimpExchange) isOutcomeUnknownAfterDispatch() bool {
	switch result.outcome {
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable, exchangeInvalid:
		return true
	case exchangeRejected:
		return !isDefiniteRefusalStatus(result.response.statusCode)
	default:
		return false
	}
}

// isDefiniteRefusalStatus lists the statuses that prove a write was refused; 409, 3xx, and others are unknown.
func isDefiniteRefusalStatus(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}

func (branches repeatableBranches) branchFor(outcome exchangeOutcome) sdkgo.BranchID {
	switch {
	case outcome == exchangeNotFound && branches.notFound != "":
		return branches.notFound
	case outcome == exchangeInvalid && branches.invalidResponse != "":
		return branches.invalidResponse
	case outcome == exchangeDefect:
		return branches.defect
	default:
		return branches.providerRejected
	}
}
