// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// ConfigurationSchemaVersion identifies the secret-free project configuration document shared with Dex Web.
const ConfigurationSchemaVersion = "connectors.dex.dev/project-configuration/v1alpha1"

// ConnectionConfiguration pins ordinary settings and a logical credential identity, never secret values or credential versions.
type ConnectionConfiguration struct {
	// ConnectorID selects the exact connector manifest.
	ConnectorID string `json:"connectorId"`
	// ConnectionName is the application-declared connection.
	ConnectionName string `json:"connectionName"`
	// ModulePath identifies the released connector module.
	ModulePath string `json:"modulePath"`
	// ModuleVersion pins the connector release used to validate configuration.
	ModuleVersion string `json:"moduleVersion"`
	// LocalArtifact pins explicit unpublished source in local-only images; absent for official releases.
	LocalArtifact *LocalConnectorArtifact `json:"localArtifact,omitempty"`
	// Provider identifies the provider within the connector contract.
	Provider string `json:"provider"`
	// AuthMethodID is the selected single authorization method when applicable.
	AuthMethodID string `json:"authMethodId,omitempty"`
	// AuthMethodIDs preserves explicitly selected multi-method authorization declarations.
	AuthMethodIDs []string `json:"authMethodIds,omitempty"`
	// Configuration contains manifest-validated ordinary values; secret fields must be excluded by Dex Web.
	Configuration json.RawMessage `json:"configuration"`
}

// TriggerConfiguration pins one trigger binding's non-secret startup configuration.
type TriggerConfiguration struct {
	// ConnectorID identifies the connector manifest.
	ConnectorID string `json:"connectorId"`
	// ConnectionName identifies the logical named connection.
	ConnectionName string `json:"connectionName"`
	// TriggerName identifies the declared trigger.
	TriggerName string `json:"triggerName"`
	// BindingName identifies the consuming application's binding.
	BindingName string `json:"bindingName"`
	// Configuration contains manifest-validated, non-secret trigger settings.
	Configuration json.RawMessage `json:"configuration"`
}

// OperationConfiguration pins non-secret settings for an exact Flow and Step connector use.
type OperationConfiguration struct {
	// ConnectorID identifies the connector manifest.
	ConnectorID string `json:"connectorId"`
	// ConnectionName identifies the logical named connection.
	ConnectionName string `json:"connectionName"`
	// OperationID identifies the manifest operation.
	OperationID string `json:"operationId"`
	// FlowType identifies the consuming Flow definition.
	FlowType string `json:"flowType"`
	// StepType identifies the consuming Step definition.
	StepType string `json:"stepType"`
	// Configuration contains manifest-validated, non-secret operation settings.
	Configuration json.RawMessage `json:"configuration"`
}

// Configuration is a revisioned ordinary configuration document. Dex Web owns manifest validation before writes.
type Configuration struct {
	// SchemaVersion must equal ConfigurationSchemaVersion.
	SchemaVersion string `json:"schemaVersion"`
	// Scope is the trusted project boundary assigned by the store.
	Scope Scope `json:"scope"`
	// Revision is assigned monotonically by the store, never accepted from a browser as authority.
	Revision uint64 `json:"revision"`
	// Connections contains logical credential references and ordinary settings.
	Connections []ConnectionConfiguration `json:"connections"`
	// TriggerBindings contains trigger startup settings.
	TriggerBindings []TriggerConfiguration `json:"triggerBindings"`
	// OperationConfigurations contains exact Flow/Step use settings.
	OperationConfigurations []OperationConfiguration `json:"operationConfigurations"`
	// Environment contains ordinary values and exact private object references, never plaintext secrets.
	Environment map[string]EnvironmentValue `json:"environment,omitempty"`
}

// SnapshotRef pins an exact immutable, secret-free document version for Preview or Live deployment.
type SnapshotRef struct {
	// Key is the scoped configuration head key, not an arbitrary external target.
	Key string `json:"key"`
	// Version identifies the accepted immutable S3 version.
	Version string `json:"version"`
	// Digest is sha256: followed by lowercase hexadecimal of the complete document bytes.
	Digest string `json:"digest"`
	// MediaType is application/json.
	MediaType string `json:"mediaType"`
}

// ConfigurationStoreConfig binds ordinary configuration to authenticated project storage.
type ConfigurationStoreConfig struct {
	// Objects is the same versioned store used for credentials and OAuth temporary state.
	Objects ObjectStore
	// Scope is fixed trusted deployment configuration.
	Scope Scope
}

// ConfigurationStore owns revision admission and exact-version reads, while Dex Web owns field-level manifest validation.
type ConfigurationStore struct {
	objects ObjectStore
	scope   Scope
	key     string
}

// NewConfigurationStore validates the immutable scope without reading or writing any objects.
func NewConfigurationStore(config *ConfigurationStoreConfig) (*ConfigurationStore, error) {
	if config == nil || config.Objects == nil {
		return nil, errors.New("configuration object store is required")
	}
	prefix, err := config.Scope.Prefix()
	if err != nil {
		return nil, err
	}
	return &ConfigurationStore{objects: config.Objects, scope: config.Scope, key: prefix + "/configuration/head"}, nil
}

// ReadConfiguration returns the current document and exact version; an absent document returns ErrObjectNotFound.
func (store *ConfigurationStore) ReadConfiguration(ctx context.Context) (Configuration, Object, error) {
	object, err := store.objects.ReadObject(ctx, store.key, "")
	if err != nil {
		return Configuration{}, Object{}, err
	}
	configuration, err := store.decodeConfiguration(object)
	return configuration, object, err
}

// WriteConfiguration atomically replaces ordinary settings using an explicit expected revision; zero creates the document.
// This method does not inspect arbitrary values for secrets: callers must validate against trusted connector manifests first.
func (store *ConfigurationStore) WriteConfiguration(ctx context.Context, expectedRevision uint64, configuration Configuration) (Configuration, Object, error) {
	previous, object, err := store.ReadConfiguration(ctx)
	if errors.Is(err, ErrObjectNotFound) && expectedRevision == 0 {
		err = nil
	}
	if err != nil {
		return Configuration{}, Object{}, err
	}
	if previous.Revision != expectedRevision {
		return Configuration{}, Object{}, ErrConflict
	}
	configuration.SchemaVersion, configuration.Scope, configuration.Revision = ConfigurationSchemaVersion, store.scope, expectedRevision+1
	if err = validateConfiguration(configuration); err != nil {
		return Configuration{}, Object{}, err
	}
	contents, err := json.Marshal(configuration)
	if err != nil {
		return Configuration{}, Object{}, errors.New("configuration encoding failed")
	}
	if expectedRevision == 0 {
		object, err = store.objects.CreateObject(ctx, store.key, contents)
	} else {
		object, err = store.objects.CompareAndSwapObject(ctx, store.key, object.ETag, contents)
	}
	if errors.Is(err, ErrOutcomeUnknown) {
		observed, readErr := store.objects.ReadObject(ctx, store.key, "")
		if readErr == nil && bytes.Equal(observed.Contents, contents) {
			object, err = observed, nil
		}
	}
	if err != nil {
		return Configuration{}, Object{}, err
	}
	return configuration, object, nil
}

// FreezeConfiguration pins the current document only when its revision matches; later edits do not alter the snapshot.
// Credentials remain logical references and are resolved from current heads during actual connector use.
func (store *ConfigurationStore) FreezeConfiguration(ctx context.Context, expectedRevision uint64) (SnapshotRef, error) {
	configuration, object, err := store.ReadConfiguration(ctx)
	if err != nil {
		return SnapshotRef{}, err
	}
	if configuration.Revision != expectedRevision {
		return SnapshotRef{}, ErrConflict
	}
	return SnapshotRef{Key: object.Key, Version: object.Version, Digest: "sha256:" + digestBytes(object.Contents), MediaType: "application/json"}, nil
}

// ReadSnapshot validates scope, exact version, content digest, and document identity without reading any credentials.
func (store *ConfigurationStore) ReadSnapshot(ctx context.Context, reference SnapshotRef) (Configuration, error) {
	if reference.Key != store.key || reference.Version == "" || reference.Version == "null" || reference.MediaType != "application/json" || len(reference.Digest) != 71 || !strings.HasPrefix(reference.Digest, "sha256:") {
		return Configuration{}, errors.New("invalid configuration snapshot reference")
	}
	object, err := store.objects.ReadObject(ctx, reference.Key, reference.Version)
	if err != nil {
		return Configuration{}, err
	}
	if "sha256:"+digestBytes(object.Contents) != reference.Digest {
		return Configuration{}, errors.New("configuration snapshot digest differs")
	}
	return store.decodeConfiguration(object)
}

func (store *ConfigurationStore) decodeConfiguration(object Object) (Configuration, error) {
	var configuration Configuration
	if object.Key != store.key || object.Version == "" || object.Version == "null" || object.ETag == "" || strictJSON(object.Contents, &configuration) != nil || configuration.SchemaVersion != ConfigurationSchemaVersion || configuration.Scope != store.scope || configuration.Revision == 0 {
		return Configuration{}, errors.New("configuration document identity is invalid")
	}
	if err := validateConfiguration(configuration); err != nil {
		return Configuration{}, err
	}
	return configuration, nil
}

// DecodeConnectionConfiguration strictly decodes a connection's settings from an already validated immutable snapshot.
func (configuration Configuration) DecodeConnectionConfiguration(key ConnectionKey, destination any) error {
	for _, connection := range configuration.Connections {
		if connection.ConnectorID == key.ConnectorID && connection.ConnectionName == key.ConnectionName {
			return strictJSON(connection.Configuration, destination)
		}
	}
	return ErrObjectNotFound
}

// DecodeTriggerConfiguration strictly decodes a trigger binding's immutable startup settings.
func (configuration Configuration) DecodeTriggerConfiguration(connectorID, connectionName, triggerName, bindingName string, destination any) error {
	for _, binding := range configuration.TriggerBindings {
		if binding.ConnectorID == connectorID && binding.ConnectionName == connectionName && binding.TriggerName == triggerName && binding.BindingName == bindingName {
			return strictJSON(binding.Configuration, destination)
		}
	}
	return ErrObjectNotFound
}

// DecodeOperationConfiguration strictly decodes settings for an exact connector, Flow, and Step use.
func (configuration Configuration) DecodeOperationConfiguration(connectorID, connectionName, operationID, flowType, stepType string, destination any) error {
	for _, operation := range configuration.OperationConfigurations {
		if operation.ConnectorID == connectorID && operation.ConnectionName == connectionName && operation.OperationID == operationID && operation.FlowType == flowType && operation.StepType == stepType {
			return strictJSON(operation.Configuration, destination)
		}
	}
	return ErrObjectNotFound
}

func validateConfiguration(configuration Configuration) error {
	if err := validateEnvironmentReferences(configuration); err != nil {
		return err
	}
	if len(configuration.Connections) > 1000 || len(configuration.TriggerBindings) > 10000 || len(configuration.OperationConfigurations) > 10000 {
		return errors.New("configuration document exceeds binding limits")
	}
	connections := make(map[ConnectionKey]bool)
	for _, connection := range configuration.Connections {
		key := ConnectionKey{ConnectorID: connection.ConnectorID, ConnectionName: connection.ConnectionName}
		if !regexpConnectorID(key.ConnectorID) || key.ConnectionName == "" || connection.ModulePath == "" || connection.ModuleVersion == "" || connection.Provider == "" || !isJSONObject(connection.Configuration) || connections[key] {
			return errors.New("configuration connection is invalid or duplicated")
		}
		if connection.LocalArtifact != nil && (!connection.LocalArtifact.Valid() || connection.LocalArtifact.BaselineVersion != connection.ModuleVersion) {
			return errors.New("configuration local connector pin is invalid")
		}
		connections[key] = true
	}
	triggers := make(map[[4]string]bool)
	for _, binding := range configuration.TriggerBindings {
		key := [4]string{binding.ConnectorID, binding.ConnectionName, binding.TriggerName, binding.BindingName}
		if !connections[ConnectionKey{ConnectorID: binding.ConnectorID, ConnectionName: binding.ConnectionName}] || binding.TriggerName == "" || binding.BindingName == "" || !isJSONObject(binding.Configuration) || triggers[key] {
			return errors.New("configuration trigger is invalid or duplicated")
		}
		triggers[key] = true
	}
	operations := make(map[[5]string]bool)
	for _, operation := range configuration.OperationConfigurations {
		key := [5]string{operation.ConnectorID, operation.ConnectionName, operation.OperationID, operation.FlowType, operation.StepType}
		if !connections[ConnectionKey{ConnectorID: operation.ConnectorID, ConnectionName: operation.ConnectionName}] || operation.OperationID == "" || operation.FlowType == "" || operation.StepType == "" || !isJSONObject(operation.Configuration) || operations[key] {
			return errors.New("configuration operation is invalid or duplicated")
		}
		operations[key] = true
	}
	return nil
}

func isJSONObject(contents json.RawMessage) bool {
	var value map[string]json.RawMessage
	return strictJSON(contents, &value) == nil && value != nil
}
