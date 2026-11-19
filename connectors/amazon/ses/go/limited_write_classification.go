// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"encoding/json"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

func limitedSESUnknown[T any](call sdkgo.Call, decision singledispatch.Decision, output T, failure sdkgo.Failure) sdkgo.MutationAttempt[T] {
	if decision.MaximumPriorSends+1 >= call.MaxSends() {
		return sdkgo.NewMutationUncertain(output, failure, sdkgo.Receipt{})
	}
	return sdkgo.NewMutationRetry[T](failure, 0)
}

func isDefiniteSESRefusal(status int, body []byte) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	var response struct {
		Type string `json:"__type"`
		Code string `json:"code"`
	}
	return json.Unmarshal(body, &response) == nil && (response.Type != "" || response.Code != "")
}
