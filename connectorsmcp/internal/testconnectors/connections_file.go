// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package testconnectors

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ConnectionsFileConnection is one record of a Dex Web connections.json.
type ConnectionsFileConnection struct {
	// ConnectorID is the connector the connection belongs to.
	ConnectorID string
	// ConnectionName is the Dex Web connection name.
	ConnectionName string
	// Configuration is the connection's ordinary settings.
	Configuration any
	// Credentials is the connection's credential.
	Credentials any
	// CredentialExpiresAt is the credential's expiry; a refresh changes it.
	CredentialExpiresAt *time.Time
}

// NotesConnection returns a notes connection served by provider.
func NotesConnection(connectionName string, provider *FakeProvider) ConnectionsFileConnection {
	return ConnectionsFileConnection{
		ConnectorID:    NotesConnectorID,
		ConnectionName: connectionName,
		Configuration:  NotesConfig{BaseURL: provider.URL()},
		Credentials:    NotesCredentials{Token: ProviderToken},
	}
}

// WriteConnectionsFile writes path as Dex Web writes connections.json: a
// regular file with mode 0600, replaced by a rename.
func WriteConnectionsFile(path string, connections ...ConnectionsFileConnection) error {
	type record struct {
		ConnectorID         string     `json:"connectorId"`
		ModulePath          string     `json:"modulePath"`
		Provider            string     `json:"provider"`
		ConnectionName      string     `json:"connectionName"`
		Configuration       any        `json:"configuration"`
		Credentials         any        `json:"credentials"`
		CredentialExpiresAt *time.Time `json:"credentialExpiresAt,omitempty"`
	}
	records := make([]record, 0, len(connections))
	for _, connection := range connections {
		records = append(records, record{
			ConnectorID: connection.ConnectorID, ModulePath: "example.com/connectors/" + connection.ConnectorID + "/go",
			Provider: connection.ConnectorID, ConnectionName: connection.ConnectionName,
			Configuration: connection.Configuration, Credentials: connection.Credentials,
			CredentialExpiresAt: connection.CredentialExpiresAt,
		})
	}
	contents, err := json.MarshalIndent(map[string]any{
		"schemaVersion":   "connectors.dex.dev/local-connections/v1alpha1",
		"connections":     records,
		"triggerBindings": []any{},
	}, "", "  ")
	if err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(temporary, contents, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
