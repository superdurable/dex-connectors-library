// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package adyen

import (
	"encoding/json"
	"net/http"
)

func isDefiniteAdyenRefusal(status int, body []byte) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	var response struct {
		Status    int    `json:"status"`
		ErrorCode string `json:"errorCode"`
		ID        string `json:"id"`
		PSPRef    string `json:"pspReference"`
	}
	return json.Unmarshal(body, &response) == nil && response.Status == status && response.ErrorCode != "" && response.ID == "" && response.PSPRef == ""
}
