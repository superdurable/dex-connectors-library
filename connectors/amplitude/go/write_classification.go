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
