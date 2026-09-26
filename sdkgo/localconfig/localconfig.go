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
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/triggerlog"
)

const (
	// EnvironmentVariable names the file-level connector configuration environment variable.
	EnvironmentVariable = "DEX_CONNECTOR_CONFIG_FILE"
	// SchemaVersion identifies the supported local connection file contract.
	SchemaVersion = "connectors.dex.dev/local-connections/v1alpha1"
	// UseConfigurationsFileName is the non-secret sidecar written beside the connection file.
	UseConfigurationsFileName = "use-configurations.json"
	// UseConfigurationsSchemaVersion identifies the operation-use sidecar contract.
	UseConfigurationsSchemaVersion = "connectors.dex.dev/local-use-configurations/v1alpha1"
)

// Store retains startup configuration while reloading credentials for every provider call.
type Store struct {
	path                   string
	useConfigurationsPath  string
	configuration          map[connectionKey]json.RawMessage
	triggerConfiguration   map[triggerBindingKey]json.RawMessage
	operationConfiguration map[sdkgo.ConnectorConfigurationRef]json.RawMessage
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

type localUseConfigurationsFile struct {
	SchemaVersion           string                         `json:"schemaVersion"`
	OperationConfigurations []operationConfigurationRecord `json:"operationConfigurations"`
}

type operationConfigurationRecord struct {
	ConnectorID    string          `json:"connectorId"`
	ConnectionName string          `json:"connectionName"`
	OperationID    string          `json:"operationId"`
	FlowType       string          `json:"flowType"`
	StepType       string          `json:"stepType"`
	Configuration  json.RawMessage `json:"configuration"`
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
	useConfigurationsPath := filepath.Join(filepath.Dir(absolutePath), UseConfigurationsFileName)
	useConfigurations, err := readUseConfigurationsFile(useConfigurationsPath, file.Connections)
	if err != nil {
		return nil, err
	}
	operationConfiguration := make(map[sdkgo.ConnectorConfigurationRef]json.RawMessage, len(useConfigurations.OperationConfigurations))
	for _, record := range useConfigurations.OperationConfigurations {
		reference := sdkgo.ConnectorConfigurationRef{
			ConnectorID: record.ConnectorID, ConnectionName: record.ConnectionName,
			OperationID: record.OperationID, FlowType: record.FlowType, StepType: record.StepType,
		}
		operationConfiguration[reference] = append(json.RawMessage(nil), record.Configuration...)
	}
	return &Store{
		path: absolutePath, useConfigurationsPath: useConfigurationsPath,
		configuration: configuration, triggerConfiguration: triggerConfiguration,
		operationConfiguration: operationConfiguration,
	}, nil
}

// LoadOperationConfiguration strictly decodes one Connector Step use's
// startup configuration snapshot. Dex retries never reload this value.
func LoadOperationConfiguration[T any](
	store *Store,
	reference sdkgo.ConnectorConfigurationRef,
) (sdkgo.ConnectorLoadedConfiguration[T], error) {
	var value T
	if store == nil {
		return sdkgo.ConnectorLoadedConfiguration[T]{}, fmt.Errorf("local connector configuration store is required")
	}
	if err := reference.Validate(); err != nil {
		return sdkgo.ConnectorLoadedConfiguration[T]{}, err
	}
	configuration, ok := store.operationConfiguration[reference]
	if !ok {
		return sdkgo.ConnectorLoadedConfiguration[T]{}, fmt.Errorf(
			"connector %q connection %q operation %q Flow %q Step %q is not configured",
			reference.ConnectorID, reference.ConnectionName, reference.OperationID, reference.FlowType, reference.StepType,
		)
	}
	if err := decodeStrict(configuration, &value); err != nil {
		return sdkgo.ConnectorLoadedConfiguration[T]{}, fmt.Errorf(
			"decode connector %q connection %q operation %q Flow %q Step %q configuration: %w",
			reference.ConnectorID, reference.ConnectionName, reference.OperationID, reference.FlowType, reference.StepType, err,
		)
	}
	return sdkgo.ConnectorLoadedConfiguration[T]{Reference: reference, Value: value}, nil
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

// UseConfigurationsPath returns the expected non-secret operation-use sidecar path.
func (store *Store) UseConfigurationsPath() string {
	if store == nil {
		return ""
	}
	return store.useConfigurationsPath
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
) sdkgo.CredentialProvider[C] {
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
	target sdkgo.TriggerTarget[T]
	log    triggerlog.Logger
	mutex  sync.Mutex
	// pendingRemoval holds the IDs of events the target consumed whose removal from the inbox failed, and
	// whether the target consumed each as undeliverable. A retry of such an event only removes it, so a
	// failing inbox write does not invoke the target again.
	pendingRemoval map[string]bool
}

// DurableTriggerOption configures NewDurableTriggerTarget.
type DurableTriggerOption interface {
	applyDurableTriggerOption(*durableTriggerOptions)
}

type durableTriggerOptions struct {
	logger *slog.Logger
}

type durableTriggerLoggerOption struct{ logger *slog.Logger }

func (option durableTriggerLoggerOption) applyDurableTriggerOption(options *durableTriggerOptions) {
	options.logger = option.logger
}

// WithTriggerLogger sends the inbox's records to logger. Without this option, or with a nil logger,
// records go to slog.Default() as of each record. Every record carries the connector, connection,
// trigger, and binding attributes, and event IDs rather than payloads.
func WithTriggerLogger(logger *slog.Logger) DurableTriggerOption {
	return durableTriggerLoggerOption{logger: logger}
}

type triggerInboxFile[T any] struct {
	SchemaVersion string                  `json:"schemaVersion"`
	Events        []sdkgo.TriggerEvent[T] `json:"events"`
}

const triggerInboxSchemaVersion = "connectors.dex.dev/local-trigger-inbox/v1alpha1"

// NewDurableTriggerTarget stores acknowledged events beside the local connection file until the target
// consumes them. The target consumes an event by returning nil or an UndeliverableTriggerError; any other
// error keeps the event pending. Replay delivers pending events in order with sdkgo.DeliverTrigger.
// Inbox read and write failures are returned like target failures, so callers retry them. When only the
// removal of a consumed event fails, a retry removes the event without invoking the target again.
//
// The inbox logs a WARN "trigger event skipped: undeliverable" record for every event it consumes as
// undeliverable, an ERROR record for every inbox read, write, or removal failure, and INFO records at the
// start and end of a replay that finds pending events. Pass WithTriggerLogger to choose the logger.
func NewDurableTriggerTarget[T any](
	store *Store,
	connectorID string,
	connectionName string,
	triggerName string,
	bindingName string,
	target sdkgo.TriggerTarget[T],
	options ...DurableTriggerOption,
) (sdkgo.TriggerTarget[T], error) {
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
	var resolved durableTriggerOptions
	for _, option := range options {
		if option != nil {
			option.applyDurableTriggerOption(&resolved)
		}
	}
	log := triggerlog.New(resolved.logger,
		slog.String("connector", connectorID), slog.String("connection", connectionName),
		slog.String("trigger", triggerName), slog.String("binding", bindingName),
	)
	return &durableTriggerTarget[T]{path: path, target: target, log: log, pendingRemoval: make(map[string]bool)}, nil
}

func (target *durableTriggerTarget[T]) PrepareTrigger(ctx context.Context, event sdkgo.TriggerEvent[T]) error {
	if strings.TrimSpace(event.ID) == "" {
		return fmt.Errorf("Trigger event ID is required")
	}
	target.mutex.Lock()
	defer target.mutex.Unlock()
	inbox, err := target.readInbox()
	if err != nil {
		target.log.Error(ctx, "trigger inbox read failed", slog.String("event_id", event.ID), triggerlog.Err(err))
		return err
	}
	for _, pendingEvent := range inbox.Events {
		if pendingEvent.ID == event.ID {
			return nil
		}
	}
	inbox.Events = append(inbox.Events, event)
	if err := target.writeInbox(inbox); err != nil {
		target.log.Error(ctx, "trigger inbox write failed", slog.String("event_id", event.ID), triggerlog.Err(err))
		return err
	}
	return nil
}

func (target *durableTriggerTarget[T]) HandleTrigger(ctx context.Context, event sdkgo.TriggerEvent[T]) error {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	_, err := target.consume(ctx, event)
	return err
}

// ReplayTriggerDeliveries delivers the events pending at the call in order. It holds the inbox lock only
// for one delivery attempt, so PrepareTrigger does not wait for a pending event's backoff. A source must
// still deliver new events only after replay returns, or a new event could overtake an older pending one.
func (target *durableTriggerTarget[T]) ReplayTriggerDeliveries(ctx context.Context) error {
	target.mutex.Lock()
	inbox, err := target.readInbox()
	target.mutex.Unlock()
	if err != nil {
		target.log.Error(ctx, "trigger inbox read failed", triggerlog.Err(err))
		return err
	}
	if len(inbox.Events) == 0 {
		return nil
	}
	target.log.Info(ctx, "replaying pending trigger events", slog.Int("count", len(inbox.Events)))
	deliveryLogger := sdkgo.WithTriggerLogger(target.log.Slog())
	delivered, skipped := 0, 0
	for index, event := range inbox.Events {
		eventSkipped := false
		err := sdkgo.DeliverTrigger(ctx, sdkgo.TriggerTargetFunc[T](func(ctx context.Context, event sdkgo.TriggerEvent[T]) error {
			var err error
			eventSkipped, err = target.handlePending(ctx, event)
			return err
		}), event, deliveryLogger)
		if err != nil {
			target.log.Info(ctx, "finished replaying pending trigger events", slog.Int("delivered", delivered),
				slog.Int("skipped", skipped), slog.Int("remaining", len(inbox.Events)-index), triggerlog.Err(err))
			return err
		}
		if eventSkipped {
			skipped++
		} else {
			delivered++
		}
	}
	target.log.Info(ctx, "finished replaying pending trigger events",
		slog.Int("delivered", delivered), slog.Int("skipped", skipped), slog.Int("remaining", 0))
	return nil
}

// handlePending makes one replay attempt, skipping an event that another delivery already consumed. It
// reports whether the target consumed the event as undeliverable.
func (target *durableTriggerTarget[T]) handlePending(ctx context.Context, event sdkgo.TriggerEvent[T]) (bool, error) {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	inbox, err := target.readInbox()
	if err != nil {
		target.log.Error(ctx, "trigger inbox read failed", slog.String("event_id", event.ID), triggerlog.Err(err))
		return false, err
	}
	for _, pendingEvent := range inbox.Events {
		if pendingEvent.ID == event.ID {
			return target.consume(ctx, pendingEvent)
		}
	}
	return false, nil
}

// consume delivers one event and removes it when the target consumes it. It reports whether the target
// consumed the event as undeliverable, and logs that skip once. The caller holds target.mutex.
func (target *durableTriggerTarget[T]) consume(ctx context.Context, event sdkgo.TriggerEvent[T]) (bool, error) {
	skipped, removalPending := target.pendingRemoval[event.ID]
	if !removalPending {
		err := target.target.HandleTrigger(ctx, event)
		if err != nil && !sdkgo.IsTriggerUndeliverable(err) {
			return false, err
		}
		if err != nil {
			skipped = true
			target.log.Warn(ctx, "trigger event skipped: undeliverable",
				append([]slog.Attr{slog.String("event_id", event.ID)}, triggerlog.ErrAttrs(err)...)...)
		}
	}
	if skipped {
		// The enclosing delivery attempt must not also report this event as delivered.
		triggerlog.RecordSkip(ctx)
	}
	if err := target.removeEvent(event.ID); err != nil {
		target.pendingRemoval[event.ID] = skipped
		target.log.Error(ctx, "trigger inbox remove failed", slog.String("event_id", event.ID), triggerlog.Err(err))
		return skipped, err
	}
	delete(target.pendingRemoval, event.ID)
	return skipped, nil
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

func (provider credentialProvider[C]) Resolve(call sdkgo.Call) (C, error) {
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

func readUseConfigurationsFile(
	path string,
	connections []connectionRecord,
) (localUseConfigurationsFile, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return localUseConfigurationsFile{
			SchemaVersion:           UseConfigurationsSchemaVersion,
			OperationConfigurations: []operationConfigurationRecord{},
		}, nil
	}
	if err != nil {
		return localUseConfigurationsFile{}, fmt.Errorf("inspect connector use configuration file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return localUseConfigurationsFile{}, fmt.Errorf("connector use configuration file must be a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		return localUseConfigurationsFile{}, fmt.Errorf("connector use configuration file permissions must be 0600")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return localUseConfigurationsFile{}, fmt.Errorf("read connector use configuration file: %w", err)
	}
	var file localUseConfigurationsFile
	if err := decodeStrict(contents, &file); err != nil {
		return localUseConfigurationsFile{}, fmt.Errorf("decode connector use configuration file: %w", err)
	}
	if file.SchemaVersion != UseConfigurationsSchemaVersion {
		return localUseConfigurationsFile{}, fmt.Errorf("unsupported connector use configuration schema version %q", file.SchemaVersion)
	}
	knownConnections := make(map[connectionKey]bool, len(connections))
	for _, connection := range connections {
		knownConnections[connectionKey{connectorID: connection.ConnectorID, connectionName: connection.ConnectionName}] = true
	}
	seen := make(map[sdkgo.ConnectorConfigurationRef]bool, len(file.OperationConfigurations))
	for index, record := range file.OperationConfigurations {
		reference := sdkgo.ConnectorConfigurationRef{
			ConnectorID: record.ConnectorID, ConnectionName: record.ConnectionName,
			OperationID: record.OperationID, FlowType: record.FlowType, StepType: record.StepType,
		}
		if err := reference.Validate(); err != nil {
			return localUseConfigurationsFile{}, fmt.Errorf("operation configuration %d identity: %w", index, err)
		}
		if !knownConnections[connectionKey{connectorID: record.ConnectorID, connectionName: record.ConnectionName}] {
			return localUseConfigurationsFile{}, fmt.Errorf("operation configuration %d references an unknown connection", index)
		}
		if seen[reference] {
			return localUseConfigurationsFile{}, fmt.Errorf(
				"connector %q connection %q operation %q Flow %q Step %q configuration is duplicated",
				reference.ConnectorID, reference.ConnectionName, reference.OperationID, reference.FlowType, reference.StepType,
			)
		}
		seen[reference] = true
		var object map[string]json.RawMessage
		if len(record.Configuration) == 0 || decodeStrict(record.Configuration, &object) != nil || object == nil {
			return localUseConfigurationsFile{}, fmt.Errorf("operation configuration %d configuration must be a JSON object", index)
		}
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
