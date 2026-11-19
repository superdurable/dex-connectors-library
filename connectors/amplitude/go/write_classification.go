// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package amplitude

import (
	"encoding/json"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func isDefiniteIngestionRefusal(call sdkgo.Call, response providerResponse) bool {
	if call.Context != nil && call.Context.Attempt() > 1 {
		return false
	}
	switch response.status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	var body struct {
		Error          string `json:"error"`
		Code           int    `json:"code"`
		EventsIngested int    `json:"events_ingested"`
	}
	return json.Unmarshal(response.body, &body) == nil && body.Error != "" && body.Code == 0 && body.EventsIngested == 0
}

func hasContradictoryIdentifyConfirmation(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var confirmation struct {
		Code  int    `json:"code"`
		Error string `json:"error"`
	}
	return json.Unmarshal(body, &confirmation) != nil || confirmation.Error != "" || confirmation.Code != http.StatusOK
}
