// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type memoryObjects struct {
	mutex    sync.Mutex
	versions map[string][]Object
	sequence uint64
}

func newMemoryObjects() *memoryObjects { return &memoryObjects{versions: map[string][]Object{}} }
func (objects *memoryObjects) ReadObject(ctx context.Context, key, version string) (Object, error) {
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	objects.mutex.Lock()
	defer objects.mutex.Unlock()
	versions := objects.versions[key]
	if len(versions) == 0 {
		return Object{}, ErrObjectNotFound
	}
	if version == "" {
		return versions[len(versions)-1], nil
	}
	for _, object := range versions {
		if object.Version == version {
			return object, nil
		}
	}
	return Object{}, ErrObjectNotFound
}
func (objects *memoryObjects) CreateObject(ctx context.Context, key string, contents []byte) (Object, error) {
	return objects.write(ctx, key, "", contents)
}
func (objects *memoryObjects) CompareAndSwapObject(ctx context.Context, key, etag string, contents []byte) (Object, error) {
	return objects.write(ctx, key, etag, contents)
}
func (objects *memoryObjects) write(ctx context.Context, key, etag string, contents []byte) (Object, error) {
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	objects.mutex.Lock()
	defer objects.mutex.Unlock()
	versions := objects.versions[key]
	if (etag == "" && len(versions) > 0) || (etag != "" && (len(versions) == 0 || versions[len(versions)-1].ETag != etag)) {
		return Object{}, ErrConflict
	}
	objects.sequence++
	identity := fmt.Sprint(objects.sequence)
	object := Object{Key: key, Version: identity, ETag: identity, Contents: contents}
	objects.versions[key] = append(versions, object)
	return object, nil
}

type faultObjects struct {
	ObjectStore
	failWrite  func(string, []byte) bool
	failRead   func(string) bool
	isAccepted bool
}

func (objects *faultObjects) ReadObject(ctx context.Context, key, version string) (Object, error) {
	if objects.failRead != nil && objects.failRead(key) {
		return Object{}, errors.New("unavailable")
	}
	return objects.ObjectStore.ReadObject(ctx, key, version)
}
func (objects *faultObjects) CreateObject(ctx context.Context, key string, contents []byte) (Object, error) {
	shouldFail := objects.failWrite != nil && objects.failWrite(key, contents)
	if shouldFail && !objects.isAccepted {
		return Object{}, ErrOutcomeUnknown
	}
	object, err := objects.ObjectStore.CreateObject(ctx, key, contents)
	if err == nil && shouldFail {
		return Object{}, ErrOutcomeUnknown
	}
	return object, err
}
func (objects *faultObjects) CompareAndSwapObject(ctx context.Context, key, etag string, contents []byte) (Object, error) {
	shouldFail := objects.failWrite != nil && objects.failWrite(key, contents)
	if shouldFail && !objects.isAccepted {
		return Object{}, ErrOutcomeUnknown
	}
	object, err := objects.ObjectStore.CompareAndSwapObject(ctx, key, etag, contents)
	if err == nil && shouldFail {
		return Object{}, ErrOutcomeUnknown
	}
	return object, err
}

type testCredentials struct {
	Token string `json:"token"`
}
type testDriver struct {
	calls   atomic.Int64
	entered chan struct{}
	proceed chan struct{}
	failure error
}

func (*testDriver) RefreshRequired(RefreshState[testCredentials]) bool { return true }
func (driver *testDriver) Refresh(ctx context.Context, state RefreshState[testCredentials]) (RefreshResult[testCredentials], error) {
	driver.calls.Add(1)
	if driver.entered != nil {
		driver.entered <- struct{}{}
	}
	if driver.proceed != nil {
		select {
		case <-ctx.Done():
			return RefreshResult[testCredentials]{}, ctx.Err()
		case <-driver.proceed:
		}
	}
	if driver.failure != nil {
		return RefreshResult[testCredentials]{}, driver.failure
	}
	return RefreshResult[testCredentials]{Credentials: testCredentials{Token: "rotated-private"}, ExpiresAt: state.Now.Add(time.Hour)}, nil
}

var testKey = ConnectionKey{ConnectorID: "example", ConnectionName: "primary"}
var testScope = Scope{ProjectID: "a7k2", Kind: "live"}

func newTestStore(t *testing.T, objects ObjectStore) *ConnectionStore {
	t.Helper()
	store, err := NewConnectionStore(&ConnectionStoreConfig{Objects: objects, Scope: testScope})
	require.NoError(t, err)
	return store
}
func newTestProvider(t *testing.T, store *ConnectionStore) *CredentialResolver[testCredentials] {
	t.Helper()
	provider, err := NewCredentialResolver(&CredentialResolverConfig[testCredentials]{Store: store, Key: testKey, Decode: func(contents json.RawMessage) (testCredentials, error) {
		var credentials testCredentials
		err := json.Unmarshal(contents, &credentials)
		return credentials, err
	}, Encode: func(credentials testCredentials) (json.RawMessage, error) { return json.Marshal(credentials) }})
	require.NoError(t, err)
	return provider
}
func testMaterial(token string, expiry *time.Time) CredentialMaterial {
	contents, _ := json.Marshal(testCredentials{Token: token})
	return CredentialMaterial{Credentials: contents, ExpiresAt: expiry, ModuleVersion: "v1.0.0", AuthMethod: "oauth2"}
}

func TestOnUseExpiryAndScopeAdmission(t *testing.T) {
	for _, kind := range []string{"unknown", "future", "exact"} {
		t.Run(kind, func(t *testing.T) {
			store := newTestStore(t, newMemoryObjects())
			now := time.Now()
			store.now = func() time.Time { return now }
			var expiry *time.Time
			if kind != "unknown" {
				value := now
				if kind == "future" {
					value = now.Add(time.Minute)
				}
				expiry = &value
			}
			_, err := store.ReplaceCredential(context.Background(), testKey, 0, testMaterial("old-private", expiry))
			require.NoError(t, err)
			provider := newTestProvider(t, store)
			driver := &testDriver{}
			_, err = store.ReadConnection(context.Background(), testKey)
			require.NoError(t, err)
			require.Zero(t, driver.calls.Load())
			credentials, err := provider.ResolveWithRefresh(context.Background(), driver.Refresh)
			require.NoError(t, err)
			if kind == "exact" {
				require.Equal(t, "rotated-private", credentials.Token)
				require.EqualValues(t, 1, driver.calls.Load())
			} else {
				require.Equal(t, "old-private", credentials.Token)
				require.Zero(t, driver.calls.Load())
				_, err = provider.ResolveAfterRejection(context.Background(), driver.Refresh)
				require.Error(t, err)
				require.Zero(t, driver.calls.Load())
			}
		})
	}
}

func TestConcurrentReplicaRefreshDispatchesOnce(t *testing.T) {
	objects := newMemoryObjects()
	store := newTestStore(t, objects)
	expired := time.Now().Add(-time.Second)
	_, err := store.ReplaceCredential(context.Background(), testKey, 0, testMaterial("old", &expired))
	require.NoError(t, err)
	driver := &testDriver{entered: make(chan struct{}, 20), proceed: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan error, 12)
	for range 12 {
		provider := newTestProvider(t, newTestStore(t, objects))
		go func() {
			credentials, err := provider.ResolveWithRefresh(ctx, driver.Refresh)
			if err == nil && credentials.Token != "rotated-private" {
				err = errors.New("wrong credential")
			}
			results <- err
		}()
	}
	select {
	case <-driver.entered:
	case <-ctx.Done():
		t.Fatal("provider never admitted")
	}
	close(driver.proceed)
	for range 12 {
		require.NoError(t, <-results)
	}
	require.EqualValues(t, 1, driver.calls.Load())
	connection, err := store.ReadConnection(ctx, testKey)
	require.NoError(t, err)
	require.Equal(t, CredentialReady, connection.Status)
	require.EqualValues(t, 3, connection.Revision)
}

func TestLateRefreshCannotOverwriteReplacementOrRevocation(t *testing.T) {
	for _, kind := range []string{"replace", "revoke"} {
		t.Run(kind, func(t *testing.T) {
			objects := newMemoryObjects()
			store := newTestStore(t, objects)
			expiry := time.Now().Add(-time.Minute)
			_, err := store.ReplaceCredential(context.Background(), testKey, 0, testMaterial("old", &expiry))
			require.NoError(t, err)
			driver := &testDriver{entered: make(chan struct{}, 1), proceed: make(chan struct{})}
			result := make(chan error, 1)
			go func() {
				_, err := newTestProvider(t, store).ResolveWithRefresh(context.Background(), driver.Refresh)
				result <- err
			}()
			<-driver.entered
			connection, err := store.ReadConnection(context.Background(), testKey)
			require.NoError(t, err)
			if kind == "replace" {
				_, err = store.ReplaceCredential(context.Background(), testKey, connection.Revision, testMaterial("replacement", nil))
			} else {
				_, err = store.RevokeConnection(context.Background(), testKey, connection.Revision)
			}
			require.NoError(t, err)
			close(driver.proceed)
			require.ErrorIs(t, <-result, ErrConflict)
			connection, err = store.ReadConnection(context.Background(), testKey)
			require.NoError(t, err)
			if kind == "replace" {
				material, _, err := store.ReadCredentialMaterial(context.Background(), testKey)
				require.NoError(t, err)
				require.Contains(t, string(material.Credentials), "replacement")
			} else {
				require.Equal(t, CredentialRevoked, connection.Status)
			}
		})
	}
}

func TestAbandonedAdmissionNeverDispatchesAfterDeadline(t *testing.T) {
	objects := newMemoryObjects()
	store := newTestStore(t, objects)
	now := time.Now()
	store.now = func() time.Time { return now }
	expiry := now.Add(-time.Minute)
	_, err := store.ReplaceCredential(context.Background(), testKey, 0, testMaterial("old", &expiry))
	require.NoError(t, err)
	admission, err := store.beginExchange(context.Background(), testKey, 1, "attempt-1", now.Add(time.Second), CredentialRefreshing)
	require.NoError(t, err)
	require.True(t, admission.ProviderDispatchAllowed)
	observer, err := store.beginExchange(context.Background(), testKey, 1, "attempt-1", now.Add(time.Second), CredentialRefreshing)
	require.NoError(t, err)
	require.False(t, observer.ProviderDispatchAllowed)
	now = now.Add(2 * time.Second)
	driver := &testDriver{}
	_, err = newTestProvider(t, store).ResolveWithRefresh(context.Background(), driver.Refresh)
	require.ErrorIs(t, err, ErrReauthorizationRequired)
	require.Zero(t, driver.calls.Load())
	_, err = store.CommitCredentialExchange(context.Background(), admission, testMaterial("late", nil))
	require.ErrorIs(t, err, ErrConflict)
}

func TestRefreshUnknownProviderOutcomeRequiresReauthorization(t *testing.T) {
	store := newTestStore(t, newMemoryObjects())
	expiry := time.Now().Add(-time.Minute)
	_, err := store.ReplaceCredential(context.Background(), testKey, 0, testMaterial("old", &expiry))
	require.NoError(t, err)
	driver := &testDriver{failure: errors.New("provider leaked private-token")}
	provider := newTestProvider(t, store)
	_, err = provider.ResolveWithRefresh(context.Background(), driver.Refresh)
	require.ErrorIs(t, err, ErrReauthorizationRequired)
	require.NotContains(t, err.Error(), "private-token")
	_, err = provider.ResolveWithRefresh(context.Background(), driver.Refresh)
	require.ErrorIs(t, err, ErrReauthorizationRequired)
	require.EqualValues(t, 1, driver.calls.Load())
}

func TestOAuthAdmissionAndRestartRecovery(t *testing.T) {
	objects := newMemoryObjects()
	store := newTestStore(t, objects)
	ctx := context.Background()
	deadline := time.Now().Add(time.Minute)
	admission, err := store.BeginCredentialExchange(ctx, testKey, 0, "oauth-one", deadline)
	require.NoError(t, err)
	require.True(t, admission.ProviderDispatchAllowed)
	observer, err := newTestStore(t, objects).BeginCredentialExchange(ctx, testKey, 0, "oauth-one", deadline)
	require.NoError(t, err)
	require.False(t, observer.ProviderDispatchAllowed)
	_, err = store.BeginCredentialExchange(ctx, testKey, 0, "oauth-stale", deadline)
	require.ErrorIs(t, err, ErrConflict)
	_, err = store.CommitCredentialExchange(ctx, admission, testMaterial("authorized", nil))
	require.NoError(t, err)
	restarted := newTestStore(t, objects)
	connection, err := restarted.RecoverCredentialExchange(ctx, admission)
	require.NoError(t, err)
	require.Equal(t, CredentialReady, connection.Status)
	repeated, err := restarted.BeginCredentialExchange(ctx, testKey, 0, "oauth-one", deadline)
	require.NoError(t, err)
	require.False(t, repeated.ProviderDispatchAllowed)
	_, err = store.ReplaceCredential(ctx, testKey, connection.Revision, testMaterial("new", nil))
	require.NoError(t, err)
	_, err = restarted.RecoverCredentialExchange(ctx, admission)
	require.ErrorIs(t, err, ErrConflict)
}

func TestUnknownAdmissionReconcilesOriginalInvocationOnly(t *testing.T) {
	for _, isAccepted := range []bool{false, true} {
		t.Run(fmt.Sprint(isAccepted), func(t *testing.T) {
			objects := newMemoryObjects()
			fault := &faultObjects{ObjectStore: objects, isAccepted: isAccepted, failWrite: func(key string, _ []byte) bool { return strings.HasSuffix(key, "/head") }}
			store := newTestStore(t, fault)
			admission, err := store.BeginCredentialExchange(context.Background(), testKey, 0, "oauth-one", time.Now().Add(time.Minute))
			if isAccepted {
				require.NoError(t, err)
				require.True(t, admission.ProviderDispatchAllowed)
				observer, err := store.BeginCredentialExchange(context.Background(), testKey, 0, "oauth-one", admission.Deadline)
				require.NoError(t, err)
				require.False(t, observer.ProviderDispatchAllowed)
			} else {
				require.ErrorIs(t, err, ErrOutcomeUnknown)
				require.False(t, admission.ProviderDispatchAllowed)
			}
		})
	}
}

func TestImmutableExchangeResultRecoversAcrossCrash(t *testing.T) {
	objects := newMemoryObjects()
	store := newTestStore(t, objects)
	ctx := context.Background()
	admission, err := store.BeginCredentialExchange(ctx, testKey, 0, "oauth-one", time.Now().Add(time.Minute))
	require.NoError(t, err)
	hasWrittenResult := false
	fault := &faultObjects{ObjectStore: objects, isAccepted: false, failWrite: func(key string, contents []byte) bool {
		if strings.Contains(key, "/exchange-results/") {
			hasWrittenResult = true
		}
		return strings.HasSuffix(key, "/head") && hasWrittenResult
	}, failRead: func(key string) bool { return strings.HasSuffix(key, "/head") && hasWrittenResult }}
	crashed := newTestStore(t, fault)
	_, err = crashed.CommitCredentialExchange(ctx, admission, testMaterial("recovered", nil))
	require.ErrorIs(t, err, ErrOutcomeUnknown)
	restarted := newTestStore(t, objects)
	connection, err := restarted.RecoverCredentialExchange(ctx, admission)
	require.NoError(t, err)
	require.Equal(t, CredentialReady, connection.Status)
	material, _, err := restarted.ReadCredentialMaterial(ctx, testKey)
	require.NoError(t, err)
	require.Contains(t, string(material.Credentials), "recovered")
}

func TestConfigurationFreezePinsSettingsAndNotCredentials(t *testing.T) {
	objects := newMemoryObjects()
	ctx := context.Background()
	store, err := NewConfigurationStore(&ConfigurationStoreConfig{Objects: objects, Scope: testScope})
	require.NoError(t, err)
	configuration := Configuration{Connections: []ConnectionConfiguration{{ConnectorID: "example", ConnectionName: "primary", ModulePath: "example/module", ModuleVersion: "v1.0.0", Provider: "example", Configuration: json.RawMessage(`{"value":1}`)}}, OperationConfigurations: []OperationConfiguration{{ConnectorID: "example", ConnectionName: "primary", OperationID: "readValue", FlowType: "ExampleFlow", StepType: "ReadValue", Configuration: json.RawMessage(`{"value":2}`)}}, TriggerBindings: []TriggerConfiguration{{ConnectorID: "example", ConnectionName: "primary", TriggerName: "onEvent", BindingName: "events", Configuration: json.RawMessage(`{"value":3}`)}}}
	configuration, _, err = store.WriteConfiguration(ctx, 0, configuration)
	require.NoError(t, err)
	snapshot, err := store.FreezeConfiguration(ctx, 1)
	require.NoError(t, err)
	configuration.Connections[0].Configuration = json.RawMessage(`{"value":4}`)
	_, _, err = store.WriteConfiguration(ctx, 1, configuration)
	require.NoError(t, err)
	_, _, err = store.WriteConfiguration(ctx, 1, configuration)
	require.ErrorIs(t, err, ErrConflict)
	pinned, err := store.ReadSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.EqualValues(t, 1, pinned.Revision)
	require.JSONEq(t, `{"value":1}`, string(pinned.Connections[0].Configuration))
	var settings struct {
		Value int `json:"value"`
	}
	require.NoError(t, pinned.DecodeConnectionConfiguration(testKey, &settings))
	require.Equal(t, 1, settings.Value)
	require.NoError(t, pinned.DecodeTriggerConfiguration("example", "primary", "onEvent", "events", &settings))
	require.Equal(t, 3, settings.Value)
	require.NoError(t, pinned.DecodeOperationConfiguration("example", "primary", "readValue", "ExampleFlow", "ReadValue", &settings))
	require.Equal(t, 2, settings.Value)
	snapshot.Digest = strings.Repeat("0", 64)
	_, err = store.ReadSnapshot(ctx, snapshot)
	require.Error(t, err)
}

func TestSecretsRejectAccidentalSerializationAndFormatting(t *testing.T) {
	material := testMaterial("private-token", nil)
	object := Object{Key: "scope/head", Contents: []byte("private-token")}
	for _, value := range []any{material, object} {
		require.NotContains(t, fmt.Sprintf("%v %#v", value, value), "private-token")
		_, err := json.Marshal(value)
		require.Error(t, err)
		_, err = yaml.Marshal(value)
		require.Error(t, err)
	}
}

func TestEnvironmentBoundaryRejectsUntrustedScopeAndEndpoints(t *testing.T) {
	for _, test := range []struct{ name, scope, session, endpoint, allow string }{
		{name: "missing scope"}, {name: "Live session", scope: "live", session: "unexpected"},
		{name: "unapproved local endpoint", scope: "live", endpoint: "http://127.0.0.1:29000"},
		{name: "public plaintext", scope: "live", endpoint: "http://example.com", allow: "true"},
		{name: "userinfo", scope: "live", endpoint: "http://user:secret@127.0.0.1:29000", allow: "true"},
		{name: "ambiguous local flag", scope: "live", allow: "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for name, value := range map[string]string{"DEX_PROJECT_ID": "a7k2", "DEX_PROJECT_SCOPE": test.scope, "DEX_PROJECT_SESSION_ID": test.session, "DEX_PROJECT_STORAGE_ENDPOINT": test.endpoint, "DEX_PROJECT_ALLOW_LOCAL_STORAGE": test.allow} {
				t.Setenv(name, value)
			}
			_, err := LoadFromEnvironment(context.Background())
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestProviderDispatchPermissionCannotBeSerialized(t *testing.T) {
	admission := ExchangeAdmission{Key: testKey, AttemptID: "oauth-one", Fence: 1, Deadline: time.Now().Add(time.Minute), ProviderDispatchAllowed: true}
	contents, err := json.Marshal(admission)
	require.NoError(t, err)
	require.NotContains(t, string(contents), "ProviderDispatchAllowed")
	var restored ExchangeAdmission
	require.NoError(t, json.Unmarshal(contents, &restored))
	require.False(t, restored.ProviderDispatchAllowed)
}

func TestExpiredOAuthAdmissionConvergesOnActualUseAfterRestart(t *testing.T) {
	for _, hasResult := range []bool{false, true} {
		t.Run(fmt.Sprint(hasResult), func(t *testing.T) {
			objects := newMemoryObjects()
			store := newTestStore(t, objects)
			now := time.Now()
			store.now = func() time.Time { return now }
			admission, err := store.BeginCredentialExchange(context.Background(), testKey, 0, "oauth-abandoned", now.Add(time.Minute))
			require.NoError(t, err)
			if hasResult {
				hasWrittenResult := false
				fault := &faultObjects{ObjectStore: objects, isAccepted: false, failWrite: func(key string, _ []byte) bool {
					if strings.Contains(key, "/exchange-results/") {
						hasWrittenResult = true
					}
					return strings.HasSuffix(key, "/head") && hasWrittenResult
				}, failRead: func(key string) bool { return strings.HasSuffix(key, "/head") && hasWrittenResult }}
				_, err = newTestStore(t, fault).CommitCredentialExchange(context.Background(), admission, testMaterial("recovered-authorization", nil))
				require.ErrorIs(t, err, ErrOutcomeUnknown)
			}
			restarted := newTestStore(t, objects)
			restarted.now = func() time.Time { return now.Add(2 * time.Minute) }
			driver := &testDriver{}
			credentials, err := newTestProvider(t, restarted).ResolveWithRefresh(context.Background(), driver.Refresh)
			if hasResult {
				require.NoError(t, err)
				require.Equal(t, "recovered-authorization", credentials.Token)
			} else {
				require.ErrorIs(t, err, ErrReauthorizationRequired)
			}
			require.Zero(t, driver.calls.Load())
		})
	}
}
