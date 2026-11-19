// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
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
		if accepted, ok := rejection["success"]; ok && bytes.Equal(bytes.TrimSpace(accepted), []byte("true")) {
			return false
		}
		for _, field := range []string{"data", "result", "results"} {
			value := bytes.TrimSpace(rejection[field])
			if len(value) > 0 && !bytes.Equal(value, []byte("null")) && !bytes.Equal(value, []byte("{}")) && !bytes.Equal(value, []byte("[]")) {
				return false
			}
		}
		for _, field := range []string{"code", "message", "error", "errors", "errorInfo"} {
			value := bytes.TrimSpace(rejection[field])
			if len(value) > 0 && !bytes.Equal(value, []byte("null")) && !bytes.Equal(value, []byte(`""`)) && !bytes.Equal(value, []byte("[]")) && !bytes.Equal(value, []byte("{}")) {
				return true
			}
		}
	}
	return false
}

func isDefiniteAshbyBusinessRefusal(body []byte) bool {
	var response struct {
		Success   *bool `json:"success"`
		ErrorInfo struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
		Results json.RawMessage `json:"results"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil || response.Success == nil || *response.Success || strings.TrimSpace(response.ErrorInfo.Code) == "" {
		return false
	}
	for _, value := range []json.RawMessage{response.Results, response.Data} {
		value = bytes.TrimSpace(value)
		if len(value) > 0 && !bytes.Equal(value, []byte("null")) && !bytes.Equal(value, []byte("{}")) && !bytes.Equal(value, []byte("[]")) {
			return false
		}
	}
	return true
}
