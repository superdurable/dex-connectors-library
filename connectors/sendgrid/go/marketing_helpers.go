// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendgrid

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

type mutationBranches struct {
	completed sdkgo.BranchID
	notFound  sdkgo.BranchID
	rejected  sdkgo.BranchID
	invalid   sdkgo.BranchID
	defect    sdkgo.BranchID
}

func invokeSafeMutation[T any](call sdkgo.Call, client *Client, operation string, branches mutationBranches, method, path string, payload []byte, acceptedStatus int, decode func([]byte) (T, error)) sdkgo.MutationAttempt[T] {
	var zero T
	body, status, retryAfter, err := client.do(call, method, path, nil, payload)
	if errors.Is(err, errCredential) {
		return mutationBranch(operation, branches.defect, zero, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[T](queryFailure(operation, sdkgo.FailureRateLimit, "provider rate limited the request"), retryAfter)
	}
	if status == http.StatusServiceUnavailable || status == http.StatusRequestTimeout || status >= 500 || errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[T](queryFailure(operation, sdkgo.FailureAvailability, "provider request outcome is unknown"), retryAfter)
	}
	if status == http.StatusNotFound && branches.notFound != "" {
		return mutationBranch(operation, branches.notFound, zero, sdkgo.FailureNotFound, "provider did not find the resource")
	}
	if status != acceptedStatus {
		return mutationBranch(operation, branches.rejected, zero, sdkgo.FailureProviderRejection, "provider rejected the request with HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return mutationBranch(operation, branches.invalid, zero, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[T](queryFailure(operation, sdkgo.FailureAvailability, "provider response was interrupted"), 0)
	}
	result, decodeErr := decode(body)
	if decodeErr != nil {
		return mutationBranch(operation, branches.invalid, zero, sdkgo.FailureProtocol, "provider returned an invalid response")
	}
	return sdkgo.NewMutationBranch(branches.completed, result, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "sendgrid"})
}

func mutationBranch[T any](operation string, branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
