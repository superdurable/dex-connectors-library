// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package localconfig loads local-development connector configuration written by Dex Web.
package localconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

const (
	// EnvironmentVariable names the file-level connector configuration environment variable.
	EnvironmentVariable = "DEX_CONNECTOR_CONFIG_FILE"
	// SchemaVersion identifies the supported local connection file contract.
	SchemaVersion = "connectors.dex.dev/local-connections/v1alpha1"
)

// Store retains startup configuration while reloading credentials for every provider call.
type Store struct {
	path          string
	configuration map[connectionKey]json.RawMessage
}

type connectionKey struct {
	connectorID    string
	connectionName string
}

type localConnectionsFile struct {
	SchemaVersion string             `json:"schemaVersion"`
	Connections   []connectionRecord `json:"connections"`
}

type connectionRecord struct {
	ConnectorID         string          `json:"connectorId"`
	ModulePath          string          `json:"modulePath"`
	ModuleVersion       string          `json:"moduleVersion"`
	Provider            string          `json:"provider"`
	ConnectionName      string          `json:"connectionName"`
	Configuration       json.RawMessage `json:"configuration"`
	Credentials         json.RawMessage `json:"credentials"`
	CredentialExpiresAt *time.Time      `json:"credentialExpiresAt,omitempty"`
}

// LoadFile validates path and snapshots each connection's non-secret configuration.
func LoadFile(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("connector configuration file path is required")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve connector configuration file path: %w", err)
	}
	file, err := readFile(absolutePath)
	if err != nil {
		return nil, err
	}
	configuration := make(map[connectionKey]json.RawMessage, len(file.Connections))
	for _, record := range file.Connections {
		key := connectionKey{connectorID: record.ConnectorID, connectionName: record.ConnectionName}
		configuration[key] = append(json.RawMessage(nil), record.Configuration...)
	}
	return &Store{path: absolutePath, configuration: configuration}, nil
}

// LoadFromEnvironment loads the file named by DEX_CONNECTOR_CONFIG_FILE.
func LoadFromEnvironment() (*Store, error) {
	path, isSet := os.LookupEnv(EnvironmentVariable)
	if !isSet || strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%s is required", EnvironmentVariable)
	}
	return LoadFile(path)
}

// Path returns the absolute local connection file path.
func (store *Store) Path() string {
	if store == nil {
		return ""
	}
	return store.path
}

// DecodeConfiguration decodes the startup snapshot for one named connection.
func (store *Store) DecodeConfiguration(connectorID string, connectionName string, destination any) error {
	if store == nil {
		return fmt.Errorf("local connector configuration store is required")
	}
	if destination == nil {
		return fmt.Errorf("configuration destination is required")
	}
	configuration, ok := store.configuration[connectionKey{connectorID: connectorID, connectionName: connectionName}]
	if !ok {
		return fmt.Errorf("connector %q connection %q is not configured", connectorID, connectionName)
	}
	if err := decodeStrict(configuration, destination); err != nil {
		return fmt.Errorf("decode connector %q connection %q configuration: %w", connectorID, connectionName, err)
	}
	return nil
}

// CredentialDecoder converts strict credential JSON into a generated credential type.
type CredentialDecoder[C any] func(json.RawMessage) (C, error)

// DecodeCredentials strictly decodes generated credential wire fields without exposing them through formatting.
func DecodeCredentials(contents json.RawMessage, destination any) error {
	if destination == nil {
		return fmt.Errorf("credential destination is required")
	}
	return decodeStrict(contents, destination)
}

// NewCredentialProvider creates a provider that reloads credentials before every connector call.
func NewCredentialProvider[C any](
	store *Store,
	connectorID string,
	connectionName string,
	decoder CredentialDecoder[C],
) connector.CredentialProvider[C] {
	if store == nil {
		panic("local connector configuration store is required")
	}
	if strings.TrimSpace(connectorID) == "" || strings.TrimSpace(connectionName) == "" {
		panic("connector ID and connection name are required")
	}
	if decoder == nil {
		panic("credential decoder is required")
	}
	return credentialProvider[C]{
		store: store, connectorID: connectorID, connectionName: connectionName, decoder: decoder,
	}
}

type credentialProvider[C any] struct {
	store          *Store
	connectorID    string
	connectionName string
	decoder        CredentialDecoder[C]
}

func (provider credentialProvider[C]) Resolve(call connector.Call) (C, error) {
	var zero C
	if call.Connection.Name != provider.connectionName {
		return zero, fmt.Errorf("connector connection name %q does not match local connection %q", call.Connection.Name, provider.connectionName)
	}
	file, err := readFile(provider.store.path)
	if err != nil {
		return zero, err
	}
	record, ok := findRecord(file.Connections, provider.connectorID, provider.connectionName)
	if !ok {
		return zero, fmt.Errorf("connector %q connection %q is not configured", provider.connectorID, provider.connectionName)
	}
	if record.CredentialExpiresAt != nil && !time.Now().Before(*record.CredentialExpiresAt) {
		return zero, fmt.Errorf("connector %q connection %q credentials are expired", provider.connectorID, provider.connectionName)
	}
	credentials, err := provider.decoder(record.Credentials)
	if err != nil {
		return zero, fmt.Errorf("decode connector %q connection %q credentials: %w", provider.connectorID, provider.connectionName, err)
	}
	return credentials, nil
}

func readFile(path string) (localConnectionsFile, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return localConnectionsFile{}, fmt.Errorf("inspect connector configuration file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return localConnectionsFile{}, fmt.Errorf("connector configuration file must be a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		return localConnectionsFile{}, fmt.Errorf("connector configuration file permissions must be 0600")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return localConnectionsFile{}, fmt.Errorf("read connector configuration file: %w", err)
	}
	var file localConnectionsFile
	if err := decodeStrict(contents, &file); err != nil {
		return localConnectionsFile{}, fmt.Errorf("decode connector configuration file: %w", err)
	}
	if file.SchemaVersion != SchemaVersion {
		return localConnectionsFile{}, fmt.Errorf("unsupported connector configuration schema version %q", file.SchemaVersion)
	}
	seen := make(map[connectionKey]bool, len(file.Connections))
	for index, record := range file.Connections {
		if strings.TrimSpace(record.ConnectorID) == "" || strings.TrimSpace(record.ConnectionName) == "" {
			return localConnectionsFile{}, fmt.Errorf("connection %d requires connectorId and connectionName", index)
		}
		if strings.TrimSpace(record.ModulePath) == "" || strings.TrimSpace(record.ModuleVersion) == "" || strings.TrimSpace(record.Provider) == "" {
			return localConnectionsFile{}, fmt.Errorf("connection %d requires modulePath, moduleVersion, and provider", index)
		}
		if len(record.Configuration) == 0 || len(record.Credentials) == 0 {
			return localConnectionsFile{}, fmt.Errorf("connection %d requires configuration and credentials", index)
		}
		key := connectionKey{connectorID: record.ConnectorID, connectionName: record.ConnectionName}
		if seen[key] {
			return localConnectionsFile{}, fmt.Errorf("connector %q connection %q is duplicated", record.ConnectorID, record.ConnectionName)
		}
		seen[key] = true
	}
	return file, nil
}

func findRecord(records []connectionRecord, connectorID string, connectionName string) (connectionRecord, bool) {
	for _, record := range records {
		if record.ConnectorID == connectorID && record.ConnectionName == connectionName {
			return record, true
		}
	}
	return connectionRecord{}, false
}

func decodeStrict(contents []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}
