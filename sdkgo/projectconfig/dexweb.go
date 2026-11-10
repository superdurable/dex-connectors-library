// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// dexWebConnectionsSchema is the schema of the connections.json file that Dex Web writes under dexcli dev.
	dexWebConnectionsSchema = "connectors.dex.dev/local-connections/v1alpha1"
	// dexWebUseConfigurationsFileName is the sidecar beside connections.json that holds Flow and Step use settings.
	dexWebUseConfigurationsFileName = "use-configurations.json"
	// dexWebUseConfigurationsSchema is the schema of that sidecar.
	dexWebUseConfigurationsSchema = "connectors.dex.dev/local-use-configurations/v1alpha1"
	// dexWebInboxDirectoryName holds the Trigger inboxes of an application beside connections.json.
	dexWebInboxDirectoryName = "trigger-inboxes"
	// dexWebDefaultAuthMethod labels credentials of a record that names no authorization method.
	dexWebDefaultAuthMethod = "local"
)

// dexWebScope is the fixed scope of the single local project that Dex Web's files describe.
var dexWebScope = Scope{ProjectID: "local", Kind: scopeKindPreview}

// dexWebConnection is one record of connections.json. Members it does not model are preserved when a refreshed
// credential is written back, because the write edits the raw record.
type dexWebConnection struct {
	ConnectorID         string          `json:"connectorId"`
	AuthMethodID        string          `json:"authMethodId"`
	AuthMethodIDs       []string        `json:"authMethodIds"`
	ModulePath          string          `json:"modulePath"`
	Provider            string          `json:"provider"`
	ConnectionName      string          `json:"connectionName"`
	Configuration       json.RawMessage `json:"configuration"`
	Credentials         json.RawMessage `json:"credentials"`
	CredentialExpiresAt *time.Time      `json:"credentialExpiresAt"`
}

type dexWebConnectionsFile struct {
	SchemaVersion   string                 `json:"schemaVersion"`
	Connections     []dexWebConnection     `json:"connections"`
	TriggerBindings []TriggerConfiguration `json:"triggerBindings"`
}

type dexWebUseConfigurationsFile struct {
	SchemaVersion           string                   `json:"schemaVersion"`
	OperationConfigurations []OperationConfiguration `json:"operationConfigurations"`
}

func (connection dexWebConnection) key() ConnectionKey {
	return ConnectionKey{ConnectorID: connection.ConnectorID, ConnectionName: connection.ConnectionName}
}

// authMethod names the method that produced the credentials. Dex Web omits it for a connector with one method.
func (connection dexWebConnection) authMethod() string {
	if connection.AuthMethodID != "" {
		return connection.AuthMethodID
	}
	if len(connection.AuthMethodIDs) == 1 {
		return connection.AuthMethodIDs[0]
	}
	return dexWebDefaultAuthMethod
}

// material returns the connection's credentials in the project protocol.
func (connection dexWebConnection) material() CredentialMaterial {
	return CredentialMaterial{Credentials: connection.Credentials, ExpiresAt: connection.CredentialExpiresAt, AuthMethod: connection.authMethod()}
}

// credentialDigest identifies credential content independently of member order and white space.
func credentialDigest(credentials json.RawMessage, expiresAt *time.Time) string {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(credentials))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return ""
	}
	canonical, _ := json.Marshal(value)
	if expiresAt != nil {
		canonical = append(canonical, '|')
		canonical = append(canonical, expiresAt.UTC().Format(time.RFC3339Nano)...)
	}
	return digestBytes(canonical)
}

// dexWebStore serves the project protocol from the files Dex Web writes under dexcli dev, so an application reads
// its local connections through the same LoadFromEnvironment and NewProjectConnection as a hosted one.
//
// The configuration document is built once, so settings need an application restart as Dex Web documents.
// Connection records are projected from connections.json whenever the application reads or writes one: a
// credential that Dex Web saved since the last read replaces the stored one, and a credential the application
// refreshed is written back to connections.json while that file still holds the credential it was read from.
// CredentialResolver refreshes while holding refreshLock, the flock of connections.json.lock beside the file.
// Trigger inboxes persist in files beside connections.json; every other object stays in memory.
type dexWebStore struct {
	path        string
	memory      *versionedStore
	inbox       *versionedStore
	connections *ConnectionStore
	refreshLock *fileRefreshLock

	mu       sync.Mutex
	imported map[ConnectionKey]string
}

// loadDexWebProject loads the local project that the Dex Web connections file at path describes.
func loadDexWebProject(ctx context.Context, path string) (*LoadedProject, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve connector configuration file path: %w", err)
	}
	file, err := readDexWebConnections(path)
	if err != nil {
		return nil, err
	}
	useConfigurations, err := readDexWebUseConfigurations(filepath.Join(filepath.Dir(path), dexWebUseConfigurationsFileName))
	if err != nil {
		return nil, err
	}
	memory, err := newVersionedStore("")
	if err != nil {
		return nil, err
	}
	inbox, err := newVersionedStore(filepath.Join(filepath.Dir(path), dexWebInboxDirectoryName))
	if err != nil {
		return nil, err
	}
	if !isCrossProcessRefreshLockSupported {
		slog.Warn("local connector credential refresh is coordinated only within one loaded project on this platform; run one application per connections file", "os", runtime.GOOS)
	}
	store := &dexWebStore{
		path: path, memory: memory, inbox: inbox, imported: map[ConnectionKey]string{},
		refreshLock: newFileRefreshLock(path+refreshLockFileSuffix, refreshLockMaxWait),
	}
	if store.connections, err = NewConnectionStore(&ConnectionStoreConfig{Objects: memory, Scope: dexWebScope}); err != nil {
		return nil, err
	}
	if err = store.importCredentials(ctx, file); err != nil {
		return nil, err
	}
	configurations, err := NewConfigurationStore(&ConfigurationStoreConfig{Objects: memory, Scope: dexWebScope})
	if err != nil {
		return nil, err
	}
	document := Configuration{TriggerBindings: file.TriggerBindings, OperationConfigurations: useConfigurations.OperationConfigurations}
	for _, connection := range file.Connections {
		authMethodID := ""
		if len(connection.AuthMethodIDs) == 0 {
			authMethodID = connection.AuthMethodID
		}
		document.Connections = append(document.Connections, ConnectionConfiguration{
			ConnectorID: connection.ConnectorID, ConnectionName: connection.ConnectionName, ModulePath: connection.ModulePath,
			Provider: connection.Provider, AuthMethodID: authMethodID, Configuration: connection.Configuration,
		})
	}
	if document.TriggerBindings == nil {
		document.TriggerBindings = []TriggerConfiguration{}
	}
	if document.OperationConfigurations == nil {
		document.OperationConfigurations = []OperationConfiguration{}
	}
	if document.Connections == nil {
		document.Connections = []ConnectionConfiguration{}
	}
	written, _, err := configurations.WriteConfiguration(ctx, 0, document)
	if err != nil {
		return nil, fmt.Errorf("connector configuration file: %w", err)
	}
	reference, err := configurations.FreezeConfiguration(ctx, written.Revision)
	if err != nil {
		return nil, err
	}
	return loadProject(ctx, store, dexWebScope, reference, brokerClientOptions{})
}

// readDexWebFile reads a file Dex Web wrote, which must be a regular, owner-only file as Dex Web creates it.
func readDexWebFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", filepath.Base(path))
	}
	if info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("%s permissions must be 0600", filepath.Base(path))
	}
	return os.ReadFile(path)
}

func readDexWebConnections(path string) (dexWebConnectionsFile, error) {
	contents, err := readDexWebFile(path)
	if err != nil {
		return dexWebConnectionsFile{}, fmt.Errorf("read connector configuration file: %w", err)
	}
	var file dexWebConnectionsFile
	if err = json.Unmarshal(contents, &file); err != nil {
		return dexWebConnectionsFile{}, errors.New("connector configuration file is not valid JSON")
	}
	if file.SchemaVersion != dexWebConnectionsSchema {
		return dexWebConnectionsFile{}, fmt.Errorf("unsupported connector configuration schema version %q", file.SchemaVersion)
	}
	for index, connection := range file.Connections {
		if strings.TrimSpace(connection.ConnectorID) == "" || strings.TrimSpace(connection.ConnectionName) == "" || strings.TrimSpace(connection.ModulePath) == "" || strings.TrimSpace(connection.Provider) == "" {
			return dexWebConnectionsFile{}, fmt.Errorf("connection %d requires connectorId, connectionName, modulePath and provider", index)
		}
		if len(connection.Configuration) == 0 || len(connection.Credentials) == 0 {
			return dexWebConnectionsFile{}, fmt.Errorf("connection %d requires configuration and credentials", index)
		}
	}
	return file, nil
}

// readDexWebUseConfigurations reads the sidecar, which Dex Web writes only after an operation setting is saved.
func readDexWebUseConfigurations(path string) (dexWebUseConfigurationsFile, error) {
	contents, err := readDexWebFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return dexWebUseConfigurationsFile{}, nil
	}
	if err != nil {
		return dexWebUseConfigurationsFile{}, fmt.Errorf("read connector use configuration file: %w", err)
	}
	var file dexWebUseConfigurationsFile
	if err = json.Unmarshal(contents, &file); err != nil {
		return dexWebUseConfigurationsFile{}, errors.New("connector use configuration file is not valid JSON")
	}
	if file.SchemaVersion != dexWebUseConfigurationsSchema {
		return dexWebUseConfigurationsFile{}, fmt.Errorf("unsupported connector use configuration schema version %q", file.SchemaVersion)
	}
	return file, nil
}

// importCredentials stores every credential of file that differs from the one last synchronized with it.
func (store *dexWebStore) importCredentials(ctx context.Context, file dexWebConnectionsFile) error {
	for _, connection := range file.Connections {
		key := connection.key()
		digest := credentialDigest(connection.Credentials, connection.CredentialExpiresAt)
		if digest == "" {
			return fmt.Errorf("connection %q of connector %q has credentials that are not a JSON value", key.ConnectionName, key.ConnectorID)
		}
		if store.imported[key] == digest {
			continue
		}
		revision := uint64(0)
		if current, err := store.connections.ReadConnection(ctx, key); err == nil {
			revision = current.Revision
		} else if !errors.Is(err, ErrObjectNotFound) {
			return err
		}
		if _, err := store.connections.ReplaceCredential(ctx, key, revision, connection.material()); err != nil {
			return fmt.Errorf("import connection %q of connector %q: %w", key.ConnectionName, key.ConnectorID, err)
		}
		store.imported[key] = digest
	}
	return nil
}

// sync adopts credentials that Dex Web saved since the last call.
func (store *dexWebStore) sync(ctx context.Context) error {
	file, err := readDexWebConnections(store.path)
	if err != nil {
		return err
	}
	return store.importCredentials(ctx, file)
}

func (store *dexWebStore) partition(key string) *versionedStore {
	if strings.Contains(key, "/trigger-inboxes/") {
		return store.inbox
	}
	return store.memory
}

func isConnectionHead(key string) bool {
	return strings.Contains(key, "/connections/") && strings.HasSuffix(key, "/head")
}

// ReadObject reads an object, first adopting credentials that Dex Web saved when it is a connection record.
func (store *dexWebStore) ReadObject(ctx context.Context, key, version string) (Object, error) {
	if isConnectionHead(key) {
		store.mu.Lock()
		defer store.mu.Unlock()
		if err := store.sync(ctx); err != nil {
			return Object{}, err
		}
	}
	return store.partition(key).ReadObject(ctx, key, version)
}

// CreateObject creates a previously absent key and returns ErrConflict if it exists.
func (store *dexWebStore) CreateObject(ctx context.Context, key string, contents []byte) (Object, error) {
	return store.partition(key).CreateObject(ctx, key, contents)
}

// lockCredentialRefresh holds the cross-process refresh lock of connections.json until the returned lock is released.
func (store *dexWebStore) lockCredentialRefresh(ctx context.Context) (*heldRefreshLock, error) {
	return store.refreshLock.acquire(ctx)
}

// CompareAndSwapObject replaces a key conditionally; a refreshed connection credential is also written back to connections.json.
func (store *dexWebStore) CompareAndSwapObject(ctx context.Context, key, expectedETag string, contents []byte) (Object, error) {
	if !isConnectionHead(key) {
		return store.partition(key).CompareAndSwapObject(ctx, key, expectedETag, contents)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.sync(ctx); err != nil {
		return Object{}, err
	}
	object, err := store.memory.CompareAndSwapObject(ctx, key, expectedETag, contents)
	if err == nil {
		store.writeBack(ctx, object)
	}
	return object, err
}

// writeBack records a refreshed credential in connections.json, so a restarted application, another process and
// Dex Web see the current refresh token. A CredentialResolver refresh reaches it while holding refreshLock.
// It writes only while the file still holds the credential that was last synchronized; otherwise Dex Web saved a
// newer one, which the next read adopts. A failure is logged and does not fail the refresh, whose result is
// already committed.
func (store *dexWebStore) writeBack(ctx context.Context, head Object) {
	var record connectionRecord
	if json.Unmarshal(head.Contents, &record) != nil || record.Status != CredentialReady || record.Credential == nil {
		return
	}
	material, err := store.connections.readMaterial(ctx, record)
	if err != nil {
		slog.Warn("local connector credential write-back skipped", "connector", record.Key.ConnectorID, "connection", record.Key.ConnectionName, "reason", "stored credential is unreadable")
		return
	}
	digest := credentialDigest(material.Credentials, material.ExpiresAt)
	if digest == store.imported[record.Key] {
		return
	}
	if err = store.replaceInFile(record.Key, material); err != nil {
		slog.Warn("local connector credential write-back failed", "connector", record.Key.ConnectorID, "connection", record.Key.ConnectionName, "error", err.Error())
		return
	}
	store.imported[record.Key] = digest
}

// replaceInFile edits one raw record of connections.json, keeping every other member as Dex Web wrote it.
func (store *dexWebStore) replaceInFile(key ConnectionKey, material CredentialMaterial) error {
	contents, err := readDexWebFile(store.path)
	if err != nil {
		return err
	}
	var file map[string]json.RawMessage
	var records []map[string]json.RawMessage
	if err = json.Unmarshal(contents, &file); err != nil || json.Unmarshal(file["connections"], &records) != nil {
		return errors.New("connector configuration file is not valid JSON")
	}
	for _, raw := range records {
		var identity dexWebConnection
		remarshaled, _ := json.Marshal(raw)
		if json.Unmarshal(remarshaled, &identity) != nil || identity.key() != key {
			continue
		}
		if credentialDigest(identity.Credentials, identity.CredentialExpiresAt) != store.imported[key] {
			return errors.New("the file holds a newer credential")
		}
		raw["credentials"] = material.Credentials
		if material.ExpiresAt != nil {
			raw["credentialExpiresAt"], _ = json.Marshal(material.ExpiresAt.UTC().Format(time.RFC3339Nano))
		} else {
			delete(raw, "credentialExpiresAt")
		}
		if file["connections"], err = json.Marshal(records); err != nil {
			return err
		}
		updated, err := json.MarshalIndent(file, "", "  ")
		if err != nil {
			return err
		}
		return writePrivateFile(store.path, append(updated, '\n'))
	}
	return errors.New("the connection is no longer in the file")
}
