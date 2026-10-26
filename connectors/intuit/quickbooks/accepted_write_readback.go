// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"encoding/json"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// acceptedEntityID extracts only an entity ID from a 2xx answer that failed full validation.
func acceptedEntityID(body []byte, entityName string) string {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil {
		return ""
	}
	var identity struct {
		ID string `json:"Id"`
	}
	if json.Unmarshal(envelope[entityName], &identity) != nil || !entityIDPattern.MatchString(identity.ID) {
		return ""
	}
	return identity.ID
}

// readAcceptedEntity checks a provider-assigned ID without resending an accepted write.
func readAcceptedEntity(client *Client, call sdkgo.Call, operation string, entityName string, entityID string) ([]byte, bool) {
	if entityID == "" {
		return nil, false
	}
	result := client.exchange(call, operation, quickbooksRequest{method: http.MethodGet, path: "/" + entityName + "/" + entityID})
	return result.response.body, result.outcome == exchangeSucceeded
}
