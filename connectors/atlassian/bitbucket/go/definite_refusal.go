// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"encoding/json"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

func isDefiniteRefusalResponse(response *http.Response, maxResponseBytes int64) bool {
	if !isDefiniteRefusalStatus(response.StatusCode) {
		return false
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, maxResponseBytes)
	if err != nil {
		return false
	}
	var rejection struct {
		Type  string `json:"type"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	return json.Unmarshal(body, &rejection) == nil && rejection.Type == "error" && rejection.Error.Message != ""
}

// isDefiniteRefusalStatus identifies HTTP statuses that prove a provider refused a write.
func isDefiniteRefusalStatus(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	}
	return false
}
