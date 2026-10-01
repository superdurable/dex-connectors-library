// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform

import (
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// failureBranches names an operation's failure branches; a blank notFound sends 404 to providerRejected.
type failureBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
}

// exchangeOutcome is the classification of one exchange that did not return a usable 2xx.
type exchangeOutcome struct {
	branch     sdkgo.BranchID
	failure    sdkgo.Failure
	isRetry    bool
	retryAfter time.Duration
	hasReceipt bool
}

// classifyExchange returns false for a 2xx. Every operation is a read or idempotent PUT, so failures retry.
func classifyExchange(
	client *Client, operationID string, response typeformResponse, err error, branches failureBranches,
) (exchangeOutcome, bool) {
	if err != nil {
		switch {
		case errors.Is(err, errTypeformRequestInvalid):
			return exchangeOutcome{branch: sdkgo.DefectBranchID,
				failure: typeformFailure(operationID, sdkgo.FailureLocalDefect, errTypeformRequestInvalid.Error())}, true
		case errors.Is(err, errTypeformResponseTooLarge), errors.Is(err, errTypeformResponseMalformed):
			return exchangeOutcome{branch: branches.invalidResponse, hasReceipt: true,
				failure: typeformFailure(operationID, responseFailureKind(err), responseFailureMessage(err))}, true
		default:
			return exchangeOutcome{isRetry: true,
				failure: typeformFailure(operationID, sdkgo.FailureTransport, "Typeform could not be reached or its answer was lost")}, true
		}
	}
	if response.statusCode >= 200 && response.statusCode < 300 {
		return exchangeOutcome{}, false
	}
	outcome := client.classifyTypeformFailure(operationID, response)
	if outcome.isRetry {
		return exchangeOutcome{isRetry: true, retryAfter: outcome.retryAfter, failure: outcome.failure}, true
	}
	branch := branches.providerRejected
	if response.statusCode == http.StatusNotFound && branches.notFound != "" {
		branch = branches.notFound
	}
	return exchangeOutcome{branch: branch, failure: outcome.failure, hasReceipt: true}, true
}

// queryAttempt converts an exchange outcome into a Query attempt.
func queryAttempt[T any](client *Client, outcome exchangeOutcome, objectID string) sdkgo.QueryAttempt[T] {
	var zero T
	if outcome.isRetry {
		return sdkgo.NewQueryRetry[T](outcome.failure, outcome.retryAfter)
	}
	receipt := sdkgo.Receipt{}
	if outcome.hasReceipt {
		receipt = client.typeformReceipt(objectID)
	}
	failure := outcome.failure
	return sdkgo.NewQueryBranch(outcome.branch, zero, &failure, receipt)
}

// mutationAttempt converts an exchange outcome into a Mutation attempt.
func mutationAttempt[T any](client *Client, outcome exchangeOutcome, objectID string) sdkgo.MutationAttempt[T] {
	var zero T
	if outcome.isRetry {
		return sdkgo.NewMutationRetry[T](outcome.failure, outcome.retryAfter)
	}
	receipt := sdkgo.Receipt{}
	if outcome.hasReceipt {
		receipt = client.typeformReceipt(objectID)
	}
	failure := outcome.failure
	return sdkgo.NewMutationBranch(outcome.branch, zero, &failure, receipt)
}

// credentialOutcome retries a connection file that may still become readable; an unusable token is a defect.
func credentialOutcome(operationID string, err error) exchangeOutcome {
	if errors.Is(err, errTypeformCredentialsUnavailable) {
		return exchangeOutcome{isRetry: true, failure: typeformFailure(operationID, sdkgo.FailureAvailability, err.Error())}
	}
	return exchangeOutcome{branch: sdkgo.DefectBranchID, failure: typeformFailure(operationID, sdkgo.FailureAuthentication, err.Error())}
}

// validationOutcome selects defect for invalid input before any request.
func validationOutcome(operationID string, err error) exchangeOutcome {
	return exchangeOutcome{branch: sdkgo.DefectBranchID, failure: typeformFailure(operationID, sdkgo.FailureValidation, err.Error())}
}

// malformedOutcome selects invalidResponse for a 2xx body the connector cannot use.
func malformedOutcome(operationID string, branch sdkgo.BranchID) exchangeOutcome {
	return exchangeOutcome{branch: branch, hasReceipt: true,
		failure: typeformFailure(operationID, sdkgo.FailureProtocol, errTypeformResponseMalformed.Error())}
}
