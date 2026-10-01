// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout

import (
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// failureBranches names an operation's failure branches; a blank notFound sends 404 to providerRejected.
type failureBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
}

// classifyQueryExchange returns false for a 2xx. A read retries transport failures, 408, 429, and 5xx.
func classifyQueryExchange[T any](
	client *Client, call sdkgo.Call, operationID string, credentials Credentials, response helpScoutResponse, err error,
	branches failureBranches, objectID int64,
) (sdkgo.QueryAttempt[T], bool) {
	var zero T
	if err != nil {
		switch {
		case errors.Is(err, errHelpScoutRequestInvalid):
			return sdkgo.NewQueryBranch(sdkgo.DefectBranchID, zero,
				helpScoutFailurePointer(operationID, sdkgo.FailureLocalDefect, errHelpScoutRequestInvalid.Error()), sdkgo.Receipt{}), true
		case errors.Is(err, errHelpScoutResponseTooLarge), errors.Is(err, errHelpScoutResponseMalformed):
			return sdkgo.NewQueryBranch(branches.invalidResponse, zero,
				helpScoutFailurePointer(operationID, responseFailureKind(err), responseFailureMessage(err)), client.receipt(call, objectID, "")), true
		default:
			return sdkgo.NewQueryRetry[T](helpScoutFailure(operationID, sdkgo.FailureTransport, "Help Scout could not be reached"), 0), true
		}
	}
	if response.statusCode >= 200 && response.statusCode < 300 {
		return sdkgo.QueryAttempt[T]{}, false
	}
	outcome := client.classifyFailure(operationID, response, credentials)
	if outcome.isRetry {
		return sdkgo.NewQueryRetry[T](outcome.failure, outcome.retryAfter), true
	}
	branch := branches.providerRejected
	if (response.statusCode == http.StatusNotFound || response.statusCode == http.StatusGone) && branches.notFound != "" {
		branch = branches.notFound
	}
	return sdkgo.NewQueryBranch(branch, zero, &outcome.failure, client.receipt(call, objectID, outcome.logRef)), true
}

// classifyIdempotentMutationExchange returns false for a 2xx; a repeatable write retries a lost answer.
func classifyIdempotentMutationExchange[T any](
	client *Client, call sdkgo.Call, operationID string, credentials Credentials, response helpScoutResponse, err error,
	branches failureBranches, objectID int64, value T,
) (sdkgo.MutationAttempt[T], bool) {
	if err != nil {
		switch {
		case errors.Is(err, errHelpScoutRequestInvalid):
			return sdkgo.NewMutationBranch(sdkgo.DefectBranchID, value,
				helpScoutFailurePointer(operationID, sdkgo.FailureLocalDefect, errHelpScoutRequestInvalid.Error()), sdkgo.Receipt{}), true
		case errors.Is(err, errHelpScoutResponseTooLarge), errors.Is(err, errHelpScoutResponseMalformed):
			return sdkgo.NewMutationBranch(branches.invalidResponse, value,
				helpScoutFailurePointer(operationID, responseFailureKind(err), responseFailureMessage(err)), client.receipt(call, objectID, "")), true
		default:
			return sdkgo.NewMutationRetry[T](helpScoutFailure(operationID, sdkgo.FailureTransport, "Help Scout could not be reached"), 0), true
		}
	}
	if response.statusCode >= 200 && response.statusCode < 300 {
		return sdkgo.MutationAttempt[T]{}, false
	}
	outcome := client.classifyFailure(operationID, response, credentials)
	if outcome.isRetry {
		return sdkgo.NewMutationRetry[T](outcome.failure, outcome.retryAfter), true
	}
	branch := branches.providerRejected
	if (response.statusCode == http.StatusNotFound || response.statusCode == http.StatusGone) && branches.notFound != "" {
		branch = branches.notFound
	}
	return sdkgo.NewMutationBranch(branch, value, &outcome.failure, client.receipt(call, objectID, outcome.logRef)), true
}

// credentialQueryAttempt retries a refresh that may still succeed, such as a token endpoint outage.
func credentialQueryAttempt[T any](operationID string, defect sdkgo.BranchID, err error) sdkgo.QueryAttempt[T] {
	if errors.Is(err, errHelpScoutCredentialsUnavailable) {
		return sdkgo.NewQueryRetry[T](helpScoutFailure(operationID, sdkgo.FailureAvailability, err.Error()), 0)
	}
	var zero T
	return sdkgo.NewQueryBranch(defect, zero, helpScoutFailurePointer(operationID, sdkgo.FailureAuthentication, err.Error()), sdkgo.Receipt{})
}

// credentialMutationAttempt retries before any request is sent, which is safe for every Mutation.
func credentialMutationAttempt[T any](operationID string, defect sdkgo.BranchID, value T, err error) sdkgo.MutationAttempt[T] {
	if errors.Is(err, errHelpScoutCredentialsUnavailable) {
		return sdkgo.NewMutationRetry[T](helpScoutFailure(operationID, sdkgo.FailureAvailability, err.Error()), 0)
	}
	return sdkgo.NewMutationBranch(defect, value, helpScoutFailurePointer(operationID, sdkgo.FailureAuthentication, err.Error()), sdkgo.Receipt{})
}
