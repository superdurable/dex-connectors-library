// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sns

import (
	"encoding/xml"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

func limitedSNSUnknown[T any](call sdkgo.Call, decision singledispatch.Decision, failure sdkgo.Failure) sdkgo.MutationAttempt[T] {
	var output T
	if decision.MaximumPriorSends+1 >= call.MaxSends() {
		return sdkgo.NewMutationUncertain(output, failure, sdkgo.Receipt{})
	}
	return sdkgo.NewMutationRetry[T](failure, 0)
}

func isDefiniteSNSRefusal(status int, body []byte) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	var response struct {
		XMLName xml.Name `xml:"ErrorResponse"`
		Error   struct {
			Code string `xml:"Code"`
		} `xml:"Error"`
	}
	return xml.Unmarshal(body, &response) == nil && response.XMLName.Local == "ErrorResponse" && response.Error.Code != ""
}
