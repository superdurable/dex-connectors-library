// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
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

func classifyLimitedMeistertaskRefusal[T any](fence *singledispatch.Fence, call sdkgo.Call, response *http.Response, maxResponseBytes int64, priorSends int32, retryAfter time.Duration, operation string, rejected sdkgo.BranchID, zero T) sdkgo.MutationAttempt[T] {
	body, readErr := providerhttp.ReadBoundedBody(response.Body, maxResponseBytes)
	failure := sdkgo.Failure{Provider: "meistertask", Operation: operation, Kind: sdkgo.FailureTransport, Message: "MeisterTask write outcome is unknown"}
	if readErr == nil && definiteLimitedRefusal(response.StatusCode, body) {
		if priorSends > 0 {
			return sdkgo.NewMutationUncertain(zero, failure, sdkgo.Receipt{})
		}
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[T](failure, 0)
		}
		failure.Kind = sdkgo.FailureProviderRejection
		failure.Message = "MeisterTask rejected the request"
		return sdkgo.NewMutationBranch(rejected, zero, &failure, sdkgo.Receipt{})
	}
	if priorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[T](failure, retryAfter)
	}
	return sdkgo.NewMutationUncertain(zero, failure, sdkgo.Receipt{})
}

func readBackLimitedMeistertask[T any](call sdkgo.Call, client *Client, path, providerID, operation string, success sdkgo.BranchID, atSendCap bool, decode func([]byte, string) (T, error)) sdkgo.MutationAttempt[T] {
	response := client.get(call, path)
	if response.status == http.StatusOK && response.err == nil {
		value, err := decode(response.body, providerID)
		if err == nil {
			return sdkgo.NewMutationBranch(success, value, nil, sdkgo.Receipt{ProviderObjectID: providerID})
		}
	}
	failure := sdkgo.Failure{Provider: "meistertask", Operation: operation, Kind: sdkgo.FailureTransport, Message: "stored provider ID could not be confirmed by read-back"}
	if atSendCap {
		var zero T
		return sdkgo.NewMutationUncertain(zero, failure, sdkgo.Receipt{ProviderObjectID: providerID})
	}
	return sdkgo.NewMutationRetry[T](failure, response.retryAfter)
}

func retryUnconfirmedMeistertaskWrite[T any](operation string, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](sdkgo.Failure{Provider: "meistertask", Operation: operation, Kind: kind, Message: message}, retryAfter)
}

func classifySafeMeistertaskNonSuccess[T any](response providerResponse, operation string, rejected sdkgo.BranchID, zero T) sdkgo.MutationAttempt[T] {
	if definiteLimitedRefusal(response.status, response.body) {
		failure := sdkgo.Failure{Provider: "meistertask", Operation: operation, Kind: sdkgo.FailureProviderRejection, Message: "MeisterTask rejected the request"}
		return sdkgo.NewMutationBranch(rejected, zero, &failure, sdkgo.Receipt{})
	}
	return retryUnconfirmedMeistertaskWrite[T](operation, sdkgo.FailureTransport, "MeisterTask write outcome is unknown", response.retryAfter)
}
