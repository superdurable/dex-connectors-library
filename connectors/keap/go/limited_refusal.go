// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package keap

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

func definiteLimitedRefusal(status int, body []byte) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil || len(envelope) == 0 {
		return false
	}
	for _, key := range []string{"success", "accepted"} {
		var confirmed bool
		if json.Unmarshal(envelope[key], &confirmed) == nil && confirmed {
			return false
		}
	}
	for _, key := range []string{"id", "content", "data"} {
		if value := strings.TrimSpace(string(envelope[key])); value != "" && value != "null" && value != "{}" {
			return false
		}
	}
	for _, key := range []string{"error", "errors", "message"} {
		value := strings.TrimSpace(string(envelope[key]))
		if value != "" && value != "null" && value != "\"\"" && value != "[]" && value != "{}" {
			return true
		}
	}
	return false
}

func classifyLimitedKeapRefusal[T any](fence *singledispatch.Fence, call sdkgo.Call, response providerResponse, priorSends int32, operation string, rejected sdkgo.BranchID, zero T) sdkgo.MutationAttempt[T] {
	if definiteLimitedRefusal(response.status, response.body) {
		if priorSends > 0 {
			return sdkgo.NewMutationUncertain(zero, keapFailure(operation, sdkgo.FailureTransport, "an earlier request may have succeeded"), sdkgo.Receipt{})
		}
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[T](keapFailure(operation, sdkgo.FailureAvailability, "rejection checkpoint failed"), 0)
		}
		return keapMutationBranch(operation, rejected, zero, sdkgo.FailureProviderRejection, "Keap rejected the request")
	}
	failure := keapFailure(operation, sdkgo.FailureTransport, "Keap write outcome is unknown")
	if priorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[T](failure, response.retryAfter)
	}
	return sdkgo.NewMutationUncertain(zero, failure, sdkgo.Receipt{})
}

func unknownLimitedKeapResponse[T any](call sdkgo.Call, response providerResponse, priorSends int32, operation string, zero T) sdkgo.MutationAttempt[T] {
	failure := keapFailure(operation, sdkgo.FailureProtocol, "Keap write response did not confirm the result")
	if priorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[T](failure, response.retryAfter)
	}
	return sdkgo.NewMutationUncertain(zero, failure, sdkgo.Receipt{})
}
