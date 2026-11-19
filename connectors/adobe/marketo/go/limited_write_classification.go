// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"encoding/json"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

func isDefiniteMarketoRefusal(status int, body []byte) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	var envelope struct {
		Success *bool `json:"success"`
		Errors  []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Success == nil || *envelope.Success || len(envelope.Errors) == 0 {
		return false
	}
	return envelope.Errors[0].Code != ""
}

func limitedMarketoUnknown[T any](call sdkgo.Call, decision singledispatch.Decision, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[T] {
	var output T
	if decision.MaximumPriorSends+1 >= call.MaxSends() {
		return sdkgo.NewMutationUncertain(output, failure, receipt)
	}
	return sdkgo.NewMutationRetry[T](failure, 0)
}
