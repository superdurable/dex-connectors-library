// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// queryFailureBranches names a Query's failure branches; a blank notFound sends 404 to providerRejected.
type queryFailureBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
}

// classifyQueryExchange returns false for a 2xx; a read retries transport failures, 429, and 5xx.
func classifyQueryExchange[T any](
	client *Client, operationID string, response calendlyResponse, err error, branches queryFailureBranches, objectURI string,
) (sdkgo.QueryAttempt[T], bool) {
	var zero T
	if err != nil {
		if errors.Is(err, errCalendlyRequestInvalid) {
			return sdkgo.NewQueryBranch(sdkgo.DefectBranchID, zero,
				calendlyFailurePointer(operationID, sdkgo.FailureLocalDefect, errCalendlyRequestInvalid.Error()), sdkgo.Receipt{}), true
		}
		if errors.Is(err, errCalendlyResponseTooLarge) || errors.Is(err, errCalendlyResponseMalformed) {
			return sdkgo.NewQueryBranch(branches.invalidResponse, zero,
				calendlyFailurePointer(operationID, responseFailureKind(err), responseFailureMessage(err)), client.calendlyReceipt(objectURI)), true
		}
		return sdkgo.NewQueryRetry[T](calendlyFailure(operationID, sdkgo.FailureTransport, "Calendly could not be reached"), 0), true
	}
	if response.statusCode >= 200 && response.statusCode < 300 {
		return sdkgo.QueryAttempt[T]{}, false
	}
	outcome := client.classifyCalendlyFailure(operationID, response)
	if outcome.isRetry {
		return sdkgo.NewQueryRetry[T](outcome.failure, outcome.retryAfter), true
	}
	branch := branches.providerRejected
	if response.statusCode == http.StatusNotFound && branches.notFound != "" {
		branch = branches.notFound
	}
	return sdkgo.NewQueryBranch(branch, zero, &outcome.failure, client.calendlyReceipt(objectURI)), true
}

// credentialQueryAttempt retries a refresh that may still succeed, such as Calendly's token rate limit.
func credentialQueryAttempt[T any](operationID string, defect sdkgo.BranchID, err error) sdkgo.QueryAttempt[T] {
	if errors.Is(err, errCalendlyCredentialsUnavailable) {
		return sdkgo.NewQueryRetry[T](calendlyFailure(operationID, sdkgo.FailureAvailability, err.Error()), 0)
	}
	var zero T
	return sdkgo.NewQueryBranch(defect, zero, calendlyFailurePointer(operationID, sdkgo.FailureAuthentication, err.Error()), sdkgo.Receipt{})
}

// credentialMutationAttempt retries before any request is sent, which is safe for every Mutation.
func credentialMutationAttempt[T any](operationID string, defect sdkgo.BranchID, output T, err error) sdkgo.MutationAttempt[T] {
	if errors.Is(err, errCalendlyCredentialsUnavailable) {
		return sdkgo.NewMutationRetry[T](calendlyFailure(operationID, sdkgo.FailureAvailability, err.Error()), 0)
	}
	return sdkgo.NewMutationBranch(defect, output, calendlyFailurePointer(operationID, sdkgo.FailureAuthentication, err.Error()), sdkgo.Receipt{})
}

// decodeCalendlyJSON decodes a 2xx body; unknown fields are ignored because Calendly adds fields over time.
func decodeCalendlyJSON(body []byte, destination any) error {
	if len(body) == 0 || json.Unmarshal(body, destination) != nil {
		return errCalendlyResponseMalformed
	}
	return nil
}

// calendlyPagination is the pagination object of every Calendly collection response.
type calendlyPagination struct {
	NextPageToken *string `json:"next_page_token"`
}

func (pagination calendlyPagination) nextPageToken() (string, error) {
	token := stringValue(pagination.NextPageToken)
	if token != "" && !calendlyPageTokenPattern.MatchString(token) {
		return "", errCalendlyResponseMalformed
	}
	return token, nil
}
