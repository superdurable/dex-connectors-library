// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paddle

import (
	"encoding/json"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func readBackContainsCallID(body []byte, callID sdkgo.CallID) bool {
	var envelope struct {
		Data []struct {
			CustomData map[string]string `json:"custom_data"`
		} `json:"data"`
	}
	return json.Unmarshal(body, &envelope) == nil && len(envelope.Data) == 1 && envelope.Data[0].CustomData["dex_connector_call_id"] == string(callID)
}
