// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

func limitedS3Unknown[T any](call sdkgo.Call, decision singledispatch.Decision, value T, failure sdkgo.Failure) sdkgo.MutationAttempt[T] {
	if decision.MaximumPriorSends+1 >= call.MaxSends() {
		return sdkgo.NewMutationUncertain(value, failure, sdkgo.Receipt{})
	}
	return sdkgo.NewMutationRetry[T](failure, 0)
}

func isDefiniteS3Refusal(response providerErrorResponse) bool {
	switch response.statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return response.code != ""
	case http.StatusMovedPermanently:
		return response.code == "PermanentRedirect"
	case http.StatusNotImplemented:
		return response.code == "NotImplemented"
	case http.StatusConflict:
		return response.code == "BucketNotEmpty"
	default:
		return false
	}
}
