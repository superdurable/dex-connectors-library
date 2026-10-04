// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package testsupport

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"

	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// ProjectScope is the live project scope every test project uses.
var ProjectScope = projectconfig.Scope{ProjectID: "openai-connector-test", Kind: "live"}

// ObjectStore is an in-memory, versioned projectconfig.ObjectStore.
type ObjectStore struct {
	mutex    sync.Mutex
	versions map[string][]projectconfig.Object
	next     int
}

// NewObjectStore returns an empty ObjectStore.
func NewObjectStore() *ObjectStore {
	return &ObjectStore{versions: map[string][]projectconfig.Object{}}
}

// ReadObject reads an exact version, or the current version when version is empty.
func (store *ObjectStore) ReadObject(_ context.Context, key, version string) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	versions := store.versions[key]
	if len(versions) == 0 {
		return projectconfig.Object{}, projectconfig.ErrObjectNotFound
	}
	if version == "" {
		return copyObject(versions[len(versions)-1]), nil
	}
	for _, object := range versions {
		if object.Version == version {
			return copyObject(object), nil
		}
	}
	return projectconfig.Object{}, projectconfig.ErrObjectNotFound
}

// CreateObject creates a previously absent key.
func (store *ObjectStore) CreateObject(_ context.Context, key string, contents []byte) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if len(store.versions[key]) > 0 {
		return projectconfig.Object{}, projectconfig.ErrConflict
	}
	return store.put(key, contents), nil
}

// CompareAndSwapObject replaces a key only when its current ETag equals expectedETag.
func (store *ObjectStore) CompareAndSwapObject(_ context.Context, key, expectedETag string, contents []byte) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	versions := store.versions[key]
	if len(versions) == 0 || versions[len(versions)-1].ETag != expectedETag {
		return projectconfig.Object{}, projectconfig.ErrConflict
	}
	return store.put(key, contents), nil
}

func (store *ObjectStore) put(key string, contents []byte) projectconfig.Object {
	store.next++
	object := projectconfig.Object{
		Key: key, Version: "v" + strconv.Itoa(store.next), ETag: "e" + strconv.Itoa(store.next), Contents: append([]byte(nil), contents...),
	}
	store.versions[key] = append(store.versions[key], object)
	return copyObject(object)
}

func copyObject(object projectconfig.Object) projectconfig.Object {
	object.Contents = append([]byte(nil), object.Contents...)
	return object
}

// ProjectConnection is one connection that NewLoadedProject saves, as Dex Web stores it.
type ProjectConnection struct {
	// Name is the connection name that dex-app.yaml declares.
	Name string
	// Configuration is the connection's ordinary settings object.
	Configuration map[string]any
	// Credentials is the connection's credential object, such as {"api_key": "..."}.
	Credentials map[string]any
}

// NewLoadedProject returns a loaded project whose snapshot holds connections
// and operations and whose in-memory storage holds each connection's credential.
func NewLoadedProject(
	t testing.TB, connectorID string, connections []ProjectConnection, operations []projectconfig.OperationConfiguration,
) *projectconfig.LoadedProject {
	t.Helper()
	store, err := projectconfig.NewConnectionStore(&projectconfig.ConnectionStoreConfig{Objects: NewObjectStore(), Scope: ProjectScope})
	if err != nil {
		t.Fatal(err)
	}
	configuration := projectconfig.Configuration{
		SchemaVersion: projectconfig.ConfigurationSchemaVersion, Scope: ProjectScope, Revision: 1,
		OperationConfigurations: operations,
	}
	for _, connection := range connections {
		settings, err := json.Marshal(connection.Configuration)
		if err != nil {
			t.Fatal(err)
		}
		configuration.Connections = append(configuration.Connections, projectconfig.ConnectionConfiguration{
			ConnectorID: connectorID, ConnectionName: connection.Name,
			ModulePath: "github.com/superdurable/dex-connectors-library/connectors/openai", Provider: "openai",
			Configuration: settings,
		})
		credentials, err := json.Marshal(connection.Credentials)
		if err != nil {
			t.Fatal(err)
		}
		key := projectconfig.ConnectionKey{ConnectorID: connectorID, ConnectionName: connection.Name}
		material := projectconfig.CredentialMaterial{Credentials: credentials, AuthMethod: "default"}
		if _, err := store.ReplaceCredential(context.Background(), key, 0, material); err != nil {
			t.Fatal(err)
		}
	}
	return &projectconfig.LoadedProject{Connections: store, Configuration: configuration}
}
