// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import "github.com/superdurable/dex-connectors-library/sdkgo"

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

// repeatableMutationAttemptForExchange retries every unconfirmed outcome of a write that sets absolute values.
func repeatableMutationAttemptForExchange[OUT any](result mailchimpExchange, receipt sdkgo.Receipt, branches repeatableBranches) (sdkgo.MutationAttempt[OUT], bool) {
	var zero OUT
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	default:
		return sdkgo.NewMutationBranch(branches.branchFor(result.outcome), zero, &result.failure, receipt), true
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
