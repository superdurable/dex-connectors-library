// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package localconfig loads local-development connector configuration written by Dex Web.
package localconfig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	path                 string
	configuration        map[connectionKey]json.RawMessage
	triggerConfiguration map[triggerBindingKey]json.RawMessage
}

type connectionKey struct {
	connectorID    string
	connectionName string
}

type triggerBindingKey struct {
	connectorID    string
	connectionName string
	triggerName    string
	bindingName    string
}

type localConnectionsFile struct {
	SchemaVersion   string                 `json:"schemaVersion"`
	Connections     []connectionRecord     `json:"connections"`
	TriggerBindings []triggerBindingRecord `json:"triggerBindings,omitempty"`
}

type triggerBindingRecord struct {
	ConnectorID    string          `json:"connectorId"`
	ConnectionName string          `json:"connectionName"`
	TriggerName    string          `json:"triggerName"`
	BindingName    string          `json:"bindingName"`
	Configuration  json.RawMessage `json:"configuration"`
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
	triggerConfiguration := make(map[triggerBindingKey]json.RawMessage, len(file.TriggerBindings))
	for _, record := range file.TriggerBindings {
		key := triggerBindingKey{connectorID: record.ConnectorID, connectionName: record.ConnectionName, triggerName: record.TriggerName, bindingName: record.BindingName}
		triggerConfiguration[key] = append(json.RawMessage(nil), record.Configuration...)
	}
	return &Store{path: absolutePath, configuration: configuration, triggerConfiguration: triggerConfiguration}, nil
}

// DecodeTriggerConfiguration decodes one trigger binding's startup configuration.
func (store *Store) DecodeTriggerConfiguration(connectorID string, connectionName string, triggerName string, bindingName string, destination any) error {
	if store == nil {
		return fmt.Errorf("local connector configuration store is required")
	}
	if destination == nil {
		return fmt.Errorf("trigger configuration destination is required")
	}
	key := triggerBindingKey{connectorID: connectorID, connectionName: connectionName, triggerName: triggerName, bindingName: bindingName}
	configuration, ok := store.triggerConfiguration[key]
	if !ok {
		return fmt.Errorf("connector %q connection %q trigger %q binding %q is not configured", connectorID, connectionName, triggerName, bindingName)
	}
	if err := decodeStrict(configuration, destination); err != nil {
		return fmt.Errorf("decode connector %q connection %q trigger %q binding %q configuration: %w", connectorID, connectionName, triggerName, bindingName, err)
	}
	return nil
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

type durableTriggerTarget[T any] struct {
	path   string
	target connector.TriggerTarget[T]
	mutex  sync.Mutex
}

type triggerInboxFile[T any] struct {
	SchemaVersion string                      `json:"schemaVersion"`
	Events        []connector.TriggerEvent[T] `json:"events"`
}

const triggerInboxSchemaVersion = "connectors.dex.dev/local-trigger-inbox/v1alpha1"

// NewDurableTriggerTarget stores acknowledged events beside the local connection file until Dex accepts them.
func NewDurableTriggerTarget[T any](
	store *Store,
	connectorID string,
	connectionName string,
	triggerName string,
	bindingName string,
	target connector.TriggerTarget[T],
) (connector.TriggerTarget[T], error) {
	if store == nil || target == nil {
		return nil, fmt.Errorf("local connector store and Trigger target are required")
	}
	for _, value := range []string{connectorID, connectionName, triggerName, bindingName} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("Trigger inbox identity fields are required")
		}
	}
	identity := strings.Join([]string{connectorID, connectionName, triggerName, bindingName}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	path := filepath.Join(filepath.Dir(store.path), fmt.Sprintf(".trigger-inbox-%x.json", digest[:16]))
	return &durableTriggerTarget[T]{path: path, target: target}, nil
}

func (target *durableTriggerTarget[T]) PrepareTrigger(_ context.Context, event connector.TriggerEvent[T]) error {
	if strings.TrimSpace(event.ID) == "" {
		return fmt.Errorf("Trigger event ID is required")
	}
	target.mutex.Lock()
	defer target.mutex.Unlock()
	inbox, err := target.readInbox()
	if err != nil {
		return err
	}
	for _, pendingEvent := range inbox.Events {
		if pendingEvent.ID == event.ID {
			return nil
		}
	}
	inbox.Events = append(inbox.Events, event)
	return target.writeInbox(inbox)
}

func (target *durableTriggerTarget[T]) HandleTrigger(ctx context.Context, event connector.TriggerEvent[T]) error {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	if err := target.target.HandleTrigger(ctx, event); err != nil {
		return err
	}
	return target.removeEvent(event.ID)
}

func (target *durableTriggerTarget[T]) ReplayTriggerDeliveries(ctx context.Context) error {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	inbox, err := target.readInbox()
	if err != nil {
		return err
	}
	for len(inbox.Events) > 0 {
		if err := target.target.HandleTrigger(ctx, inbox.Events[0]); err != nil {
			return err
		}
		inbox.Events = inbox.Events[1:]
		if err := target.writeInbox(inbox); err != nil {
			return err
		}
	}
	return nil
}

func (target *durableTriggerTarget[T]) removeEvent(eventID string) error {
	inbox, err := target.readInbox()
	if err != nil {
		return err
	}
	for index, pendingEvent := range inbox.Events {
		if pendingEvent.ID != eventID {
			continue
		}
		inbox.Events = append(inbox.Events[:index], inbox.Events[index+1:]...)
		return target.writeInbox(inbox)
	}
	return nil
}

func (target *durableTriggerTarget[T]) readInbox() (triggerInboxFile[T], error) {
	inbox := triggerInboxFile[T]{SchemaVersion: triggerInboxSchemaVersion}
	info, err := os.Lstat(target.path)
	if errors.Is(err, os.ErrNotExist) {
		return inbox, nil
	}
	if err != nil {
		return inbox, fmt.Errorf("inspect Trigger inbox: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return inbox, fmt.Errorf("Trigger inbox must be a regular 0600 file")
	}
	contents, err := os.ReadFile(target.path)
	if err != nil {
		return inbox, fmt.Errorf("read Trigger inbox: %w", err)
	}
	if err := decodeStrict(contents, &inbox); err != nil {
		return inbox, fmt.Errorf("decode Trigger inbox: %w", err)
	}
	if inbox.SchemaVersion != triggerInboxSchemaVersion {
		return inbox, fmt.Errorf("unsupported Trigger inbox schema version %q", inbox.SchemaVersion)
	}
	return inbox, nil
}

func (target *durableTriggerTarget[T]) writeInbox(inbox triggerInboxFile[T]) error {
	contents, err := json.Marshal(inbox)
	if err != nil {
		return fmt.Errorf("encode Trigger inbox: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(target.path), ".trigger-inbox-*.tmp")
	if err != nil {
		return fmt.Errorf("create Trigger inbox update: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		return errors.Join(fmt.Errorf("secure Trigger inbox update: %w", err), temporary.Close())
	}
	if _, err := temporary.Write(contents); err != nil {
		return errors.Join(fmt.Errorf("write Trigger inbox update: %w", err), temporary.Close())
	}
	if err := temporary.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync Trigger inbox update: %w", err), temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Trigger inbox update: %w", err)
	}
	if err := os.Rename(temporaryPath, target.path); err != nil {
		return fmt.Errorf("replace Trigger inbox: %w", err)
	}
	directory, err := os.Open(filepath.Dir(target.path))
	if err != nil {
		return fmt.Errorf("open Trigger inbox directory: %w", err)
	}
	return errors.Join(directory.Sync(), directory.Close())
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
	seenTriggerBindings := make(map[triggerBindingKey]bool, len(file.TriggerBindings))
	for index, record := range file.TriggerBindings {
		if strings.TrimSpace(record.ConnectorID) == "" || strings.TrimSpace(record.ConnectionName) == "" || strings.TrimSpace(record.TriggerName) == "" || strings.TrimSpace(record.BindingName) == "" {
			return localConnectionsFile{}, fmt.Errorf("trigger binding %d requires connectorId, connectionName, triggerName, and bindingName", index)
		}
		if len(record.Configuration) == 0 {
			return localConnectionsFile{}, fmt.Errorf("trigger binding %d requires configuration", index)
		}
		connection := connectionKey{connectorID: record.ConnectorID, connectionName: record.ConnectionName}
		if !seen[connection] {
			return localConnectionsFile{}, fmt.Errorf("trigger binding %d references an unknown connection", index)
		}
		key := triggerBindingKey{connectorID: record.ConnectorID, connectionName: record.ConnectionName, triggerName: record.TriggerName, bindingName: record.BindingName}
		if seenTriggerBindings[key] {
			return localConnectionsFile{}, fmt.Errorf("connector %q connection %q trigger %q binding %q is duplicated", record.ConnectorID, record.ConnectionName, record.TriggerName, record.BindingName)
		}
		seenTriggerBindings[key] = true
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
