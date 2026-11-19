// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaignmonitor

import (
	"bytes"
	"encoding/json"
	"net/http"
)

// isDefiniteRefusalStatus identifies HTTP statuses that prove a provider refused a write.
func isDefiniteRefusalStatus(status int, body []byte) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		var rejection map[string]json.RawMessage
		if json.Unmarshal(body, &rejection) != nil {
			return false
		}
		if accepted, ok := rejection["success"]; ok && string(accepted) == "true" {
			return false
		}
		for _, field := range []string{"code", "message", "error", "errors", "errorInfo", "Code", "Message"} {
			value := bytes.TrimSpace(rejection[field])
			if len(value) > 0 && !bytes.Equal(value, []byte("null")) && !bytes.Equal(value, []byte(`""`)) && !bytes.Equal(value, []byte("[]")) && !bytes.Equal(value, []byte("{}")) {
				return true
			}
		}
	}
	return false
}
