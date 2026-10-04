// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package apikeymethods

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestDecodeCredentialsKeepsTheSelectedMethodAndItsKey(t *testing.T) {
	credentials, err := decodeCredentials(json.RawMessage(`{"auth_method": "anthropic", "anthropic_api_key": "anthropic-test-key"}`))

	require.NoError(t, err)
	require.Equal(t, "anthropic", credentials.AuthMethodID)
	require.Equal(t, "anthropic-test-key", credentials.AnthropicAPIKey.Reveal())
	require.Empty(t, credentials.OpenAIAPIKey.Reveal())
	require.Empty(t, credentials.GeminiAPIKey.Reveal())
}

func TestDecodeCredentialsRejectsInvalidSelections(t *testing.T) {
	testCases := map[string]struct {
		contents string
		message  string
	}{
		"missing method":       {`{"anthropic_api_key": "anthropic-test-key"}`, "credential auth_method is invalid"},
		"undeclared method":    {`{"auth_method": "mistral", "anthropic_api_key": "anthropic-test-key"}`, "credential auth_method is invalid"},
		"selected key missing": {`{"auth_method": "gemini", "anthropic_api_key": "anthropic-test-key"}`, "credential gemini_api_key is required"},
		"method list":          {`{"auth_method": "anthropic", "auth_methods": ["anthropic"], "anthropic_api_key": "anthropic-test-key"}`, "stored credential does not match the connector's credential fields"},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := decodeCredentials(json.RawMessage(testCase.contents))

			require.ErrorContains(t, err, testCase.message)
			require.NotContains(t, err.Error(), "anthropic-test-key")
		})
	}
}

func TestCredentialsValidateChecksOnlyTheSelectedMethod(t *testing.T) {
	require.NoError(t, Credentials{AuthMethodID: "gemini", GeminiAPIKey: sdkgo.NewSecretString("gemini-test-key")}.Validate())
	require.ErrorContains(t, Credentials{AuthMethodID: "anthropic", GeminiAPIKey: sdkgo.NewSecretString("gemini-test-key")}.Validate(), "credential anthropic_api_key is required")
	require.ErrorContains(t, Credentials{}.Validate(), "credential auth_method is invalid")
}

func TestConfigTypeChecksButNeverRequiresMethodConfiguration(t *testing.T) {
	require.NoError(t, Config{}.Validate(), "openaiProjectId is required only while openai is selected, which Config cannot see")
	require.Equal(t, GeminiAPIVersionV1beta, DefaultConfig().GeminiAPIVersion)
	require.Equal(t, GeminiAPIVersionV1beta, withConfigDefaults(Config{}).GeminiAPIVersion)
	require.Equal(t, GeminiAPIVersionV1, withConfigDefaults(Config{GeminiAPIVersion: GeminiAPIVersionV1}).GeminiAPIVersion)
	require.ErrorContains(t, Config{GeminiAPIVersion: "v2"}.Validate(), "configuration geminiApiVersion is invalid")
}

func TestNewProjectConnectionDecodesMethodConfigurationAndSelectedCredentials(t *testing.T) {
	project := newProject(t, `{"model": "claude-sonnet-5", "anthropicWorkspaceId": "wrkspc_test"}`)

	connection, err := NewProjectConnection(project, "providers")

	require.NoError(t, err)
	require.Equal(t, Config{
		Model: "claude-sonnet-5", AnthropicWorkspaceID: "wrkspc_test", GeminiAPIVersion: GeminiAPIVersionV1beta,
	}, connection.client.config)
	credentials, err := connection.client.credentials.Resolve(sdkgo.Call{
		Connection: sdkgo.ConnectionRef{Provider: "example", Name: "providers"},
		Operation:  GetThingDefinition.Operation,
	})
	require.NoError(t, err)
	require.Equal(t, "anthropic", credentials.AuthMethodID)
	require.Equal(t, "anthropic-test-key", credentials.AnthropicAPIKey.Reveal())
}

func TestNewProjectConnectionRejectsUnknownConfiguration(t *testing.T) {
	project := newProject(t, `{"mistralEndpoint": "https://mistral.example"}`)

	_, err := NewProjectConnection(project, "providers")

	require.ErrorContains(t, err, `api-key-methods-fixture connection "providers" settings`)
	require.NotErrorIs(t, err, projectconfig.ErrObjectNotFound, "the connection exists; only its settings are invalid")
}

func TestNewProjectConnectionRequiresTheLoadedProject(t *testing.T) {
	_, err := NewProjectConnection(nil, "providers")

	require.ErrorContains(t, err, "api-key-methods-fixture connection requires the loaded project configuration")
}

func TestNewProjectConnectionRejectsAnUndeclaredConnection(t *testing.T) {
	project := newProject(t, `{}`)

	_, err := NewProjectConnection(project, "assistants")

	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)
	require.ErrorContains(t, err, `api-key-methods-fixture connection "assistants" settings`)
}

var fixtureScope = projectconfig.Scope{ProjectID: "fixture", Kind: "live"}

// newProject returns a loaded project that declares the "providers" connection with settings and
// stores its credential, which selects the anthropic method, as Dex Web does.
func newProject(t *testing.T, settings string) *projectconfig.LoadedProject {
	t.Helper()
	store, err := projectconfig.NewConnectionStore(&projectconfig.ConnectionStoreConfig{Objects: newObjectStore(), Scope: fixtureScope})
	require.NoError(t, err)
	key := projectconfig.ConnectionKey{ConnectorID: ConnectorID, ConnectionName: "providers"}
	_, err = store.ReplaceCredential(context.Background(), key, 0, projectconfig.CredentialMaterial{
		Credentials: json.RawMessage(`{"auth_method": "anthropic", "anthropic_api_key": "anthropic-test-key"}`),
		AuthMethod:  "anthropic",
	})
	require.NoError(t, err)
	return &projectconfig.LoadedProject{
		Configuration: projectconfig.Configuration{
			SchemaVersion: projectconfig.ConfigurationSchemaVersion, Scope: fixtureScope, Revision: 1,
			Connections: []projectconfig.ConnectionConfiguration{{
				ConnectorID: ConnectorID, ConnectionName: "providers", ModulePath: "example.com/apikeymethods",
				Provider: "example", AuthMethodID: "anthropic", Configuration: json.RawMessage(settings),
			}},
		},
		Connections: store,
	}
}

// objectStore is an in-memory projectconfig.ObjectStore with immutable versions and compare-and-swap.
type objectStore struct {
	mutex    sync.Mutex
	versions map[string][]projectconfig.Object
	next     int
}

func newObjectStore() *objectStore {
	return &objectStore{versions: map[string][]projectconfig.Object{}}
}

// ReadObject reads an exact version, or the current version when version is empty.
func (store *objectStore) ReadObject(_ context.Context, key, version string) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	versions := store.versions[key]
	for index := len(versions) - 1; index >= 0; index-- {
		if version == "" || versions[index].Version == version {
			return copyObject(versions[index]), nil
		}
	}
	return projectconfig.Object{}, projectconfig.ErrObjectNotFound
}

// CreateObject creates a previously absent key.
func (store *objectStore) CreateObject(_ context.Context, key string, contents []byte) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if len(store.versions[key]) > 0 {
		return projectconfig.Object{}, projectconfig.ErrConflict
	}
	return store.put(key, contents), nil
}

// CompareAndSwapObject replaces a key only when its current ETag equals expectedETag.
func (store *objectStore) CompareAndSwapObject(_ context.Context, key, expectedETag string, contents []byte) (projectconfig.Object, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	versions := store.versions[key]
	if len(versions) == 0 || versions[len(versions)-1].ETag != expectedETag {
		return projectconfig.Object{}, projectconfig.ErrConflict
	}
	return store.put(key, contents), nil
}

func (store *objectStore) put(key string, contents []byte) projectconfig.Object {
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
