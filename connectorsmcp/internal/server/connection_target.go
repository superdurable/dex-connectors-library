// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/canonicaljson"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// connectionTargetDigest fingerprints what a connection points at, from
// non-secret fields only; credentials, their expiry, and the module never enter it.
func connectionTargetDigest(configuration projectconfig.ConnectionConfiguration) string {
	settings := json.RawMessage("null")
	if trimmed := bytes.TrimSpace(configuration.Configuration); len(trimmed) > 0 {
		canonical, err := canonicaljson.Canonicalize(trimmed)
		if err != nil {
			// Settings that are not JSON still fingerprint by their exact bytes.
			canonical, _ = json.Marshal(string(trimmed))
		}
		settings = canonical
	}
	encoded, err := canonicaljson.Marshal(map[string]any{
		"authMethodId":       configuration.AuthMethodID,
		"configuration":      settings,
		"connectionName":     configuration.ConnectionName,
		"connectorId":        configuration.ConnectorID,
		"provider":           configuration.Provider,
		"sharedConnectionId": configuration.SharedConnectionID,
	})
	if err != nil {
		panic(fmt.Sprintf("encode connection target: %v", err))
	}
	return "sha256:" + canonicaljson.SHA256Hex(encoded)
}

// connectionTargetOf returns the target digest of one project connection, or false.
func connectionTargetOf(project *projectconfig.LoadedProject, connectorID string, connectionName string) (string, bool) {
	if project == nil {
		return "", false
	}
	for _, configuration := range project.Configuration.Connections {
		if configuration.ConnectorID == connectorID && configuration.ConnectionName == connectionName {
			return connectionTargetDigest(configuration), true
		}
	}
	return "", false
}
