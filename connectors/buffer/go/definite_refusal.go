// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package buffer

import "net/http"

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
