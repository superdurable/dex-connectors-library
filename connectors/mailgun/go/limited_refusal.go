// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailgun

import (
	"encoding/json"
	"net/http"
	"strings"
)

func definiteLimitedRefusal(status int, body []byte) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil || len(envelope) == 0 {
		return false
	}
	for _, key := range []string{"success", "accepted"} {
		var confirmed bool
		if json.Unmarshal(envelope[key], &confirmed) == nil && confirmed {
			return false
		}
	}
	for _, key := range []string{"id", "content", "data"} {
		if value := strings.TrimSpace(string(envelope[key])); value != "" && value != "null" && value != "{}" {
			return false
		}
	}
	for _, key := range []string{"error", "errors", "message"} {
		value := strings.TrimSpace(string(envelope[key]))
		if value != "" && value != "null" && value != "\"\"" && value != "[]" && value != "{}" {
			return true
		}
	}
	return false
}
