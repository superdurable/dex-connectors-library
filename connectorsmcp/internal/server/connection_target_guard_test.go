// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/callstate"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/canonicaljson"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/registry"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

const (
	firstAuthorizationID  = "8f14e45f-ceea-4e7a-9b1d-3c59e6a1b2c4"
	secondAuthorizationID = "c2a9e7d4-8f3b-4e61-a2d5-5b7c9e0f1a23"
)

// rebindOnceBeforeAttempt rewrites the connections file the first time an
// attempt of operationID is about to run, after the server's target checks.
type rebindOnceBeforeAttempt struct {
	once        sync.Once
	operationID string
	attempt     int
	seen        atomic.Int32
	path        func() string
	connections func() []testconnectors.ConnectionsFileConnection
}

func (rebind *rebindOnceBeforeAttempt) observe(point ProtocolPoint, operationID string) {
	if point != ProtocolPointBeforeAttempt || operationID != rebind.operationID {
		return
	}
	if int(rebind.seen.Add(1)) < max(rebind.attempt, 1) {
		return
	}
	rebind.once.Do(func() {
		if err := testconnectors.WriteConnectionsFile(rebind.path(), rebind.connections()...); err != nil {
			panic(err)
		}
	})
}

// notesConnectionWithAuthorization is the notes connection authorized as authorizationID.
func notesConnectionWithAuthorization(fakeProvider *testconnectors.FakeProvider, authorizationID string) testconnectors.ConnectionsFileConnection {
	connection := testconnectors.NotesConnection("work", fakeProvider)
	connection.AuthorizationID = authorizationID
	return connection
}

func TestARebindInsideAnAttemptSendsNothing(t *testing.T) {
	for _, rebindCase := range []struct {
		name string
		// rebound returns the record Dex Web writes; other is the second account's provider.
		rebound func(original *testconnectors.FakeProvider, other *testconnectors.FakeProvider) testconnectors.ConnectionsFileConnection
	}{
		{"another account with the same settings", func(original *testconnectors.FakeProvider, _ *testconnectors.FakeProvider) testconnectors.ConnectionsFileConnection {
			return notesConnectionWithAuthorization(original, secondAuthorizationID)
		}},
		{"another endpoint", func(_ *testconnectors.FakeProvider, other *testconnectors.FakeProvider) testconnectors.ConnectionsFileConnection {
			return notesConnectionWithAuthorization(other, secondAuthorizationID)
		}},
	} {
		for _, toolCase := range []struct {
			tool, operationID string
			arguments         map[string]any
			outcome           string
			text              string
		}{
			// saveRecord has no Fence, so its Retry cannot prove nothing was sent.
			{"example-notes_save_record", "saveRecord", map[string]any{"name": "a", "value": "1"}, outcomeUnknown,
				"example-notes saveRecord may have taken effect: the connection now points to a different target; do not repeat it"},
			// postNote's Fence records notSent before it returns Retry.
			{"example-notes_post_note", "postNote", map[string]any{"text": "hello"}, outcomeRejected,
				"example-notes postNote did not take effect: the connection now points to a different target; nothing was sent"},
		} {
			t.Run(rebindCase.name+"/"+toolCase.operationID, func(t *testing.T) {
				otherAccount := testconnectors.NewFakeProvider()
				t.Cleanup(otherAccount.Close)
				var harness *testHarness
				rebind := &rebindOnceBeforeAttempt{operationID: toolCase.operationID, path: func() string { return harness.connectionsFile }}
				harness = newTestHarness(t, harnessOptions{observe: rebind.observe})
				harness.writeConnectionRecords(notesConnectionWithAuthorization(harness.provider, firstAuthorizationID))
				rebind.connections = func() []testconnectors.ConnectionsFileConnection {
					return []testconnectors.ConnectionsFileConnection{rebindCase.rebound(harness.provider, otherAccount)}
				}

				key := "flow/turn/in-attempt/" + toolCase.operationID
				result := harness.mustCallResult(toolCase.tool, toolCase.arguments, key)
				require.Equal(t, toolCase.outcome, result.outcome(), result.text())
				require.Equal(t, toolCase.text, result.text())
				require.Zero(t, harness.provider.TotalRequests(), "the old call sent nothing with the new authorization")
				require.Zero(t, otherAccount.TotalRequests(), "nothing ran against the new target")
				require.Empty(t, harness.clock.recordedSleeps(), "the recheck after the attempt stopped it, not the check after a retry delay")

				replay := harness.mustCallResult(toolCase.tool, toolCase.arguments, key)
				require.Equal(t, true, replay.Meta[metaKeyReplayed])
				require.Equal(t, toolCase.text, replay.text())
				require.Zero(t, harness.provider.TotalRequests()+otherAccount.TotalRequests())
			})
		}
	}
}

func TestARebindInsideAResumedAttemptSendsNothing(t *testing.T) {
	var harness *testHarness
	rebind := &rebindOnceBeforeAttempt{operationID: "saveRecord", attempt: 2, path: func() string { return harness.connectionsFile }}
	harness = newTestHarness(t, harnessOptions{callTimeBudget: 10500 * time.Millisecond, observe: rebind.observe})
	harness.writeConnectionRecords(notesConnectionWithAuthorization(harness.provider, firstAuthorizationID))
	rebind.connections = func() []testconnectors.ConnectionsFileConnection {
		return []testconnectors.ConnectionsFileConnection{notesConnectionWithAuthorization(harness.provider, secondAuthorizationID)}
	}
	harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	arguments := map[string]any{"name": "a", "value": "1"}
	require.NotNil(t, harness.callTool("example-notes_save_record", arguments, "flow/turn/resumed").Error, "the key stays running")
	requestsBefore := harness.provider.TotalRequests()

	settled := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/resumed")
	require.Equal(t, outcomeUnknown, settled.outcome(), "attempt 1 reached the first account")
	require.Contains(t, settled.text(), "the connection now points to a different target")
	require.Equal(t, requestsBefore, harness.provider.TotalRequests(), "attempt 2 sent nothing, not even its read-back")
}

func TestAnAuthorizationChangeAloneSettlesARunningKey(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{callTimeBudget: 10500 * time.Millisecond})
	harness.writeConnectionRecords(notesConnectionWithAuthorization(harness.provider, firstAuthorizationID))
	harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	arguments := map[string]any{"name": "a", "value": "1"}
	require.NotNil(t, harness.callTool("example-notes_save_record", arguments, "flow/turn/reauthorized").Error)

	harness.writeConnectionRecords(notesConnectionWithAuthorization(harness.provider, secondAuthorizationID))
	settled := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/reauthorized")
	require.Equal(t, outcomeUnknown, settled.outcome())
	require.Contains(t, settled.text(), "the connection now points to a different target")
	require.Equal(t, 1, harness.provider.RecordRequests(), "nothing ran with the second authorization")
}

func TestARefreshInsideAnAttemptKeepsTheTarget(t *testing.T) {
	var harness *testHarness
	rebind := &rebindOnceBeforeAttempt{operationID: "saveRecord", path: func() string { return harness.connectionsFile }}
	harness = newTestHarness(t, harnessOptions{observe: rebind.observe})
	harness.writeConnectionRecords(notesConnectionWithAuthorization(harness.provider, firstAuthorizationID))
	rebind.connections = func() []testconnectors.ConnectionsFileConnection {
		refreshed := notesConnectionWithAuthorization(harness.provider, firstAuthorizationID)
		expiresAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
		refreshed.CredentialExpiresAt = &expiresAt
		return []testconnectors.ConnectionsFileConnection{refreshed}
	}
	result := harness.mustCallResult("example-notes_save_record", map[string]any{"name": "a", "value": "1"}, "flow/turn/refresh-in-attempt")
	require.False(t, result.IsError, result.text())
	require.Equal(t, 1, harness.provider.AppliedRecordWrites(saveRecordIdempotencyKey("flow/turn/refresh-in-attempt")))
}

func TestARebindInsideAReadAttemptReadsNothing(t *testing.T) {
	var harness *testHarness
	rebind := &rebindOnceBeforeAttempt{operationID: "getNote", path: func() string { return harness.connectionsFile }}
	harness = newTestHarness(t, harnessOptions{observe: rebind.observe})
	harness.writeConnectionRecords(notesConnectionWithAuthorization(harness.provider, firstAuthorizationID))
	rebind.connections = func() []testconnectors.ConnectionsFileConnection {
		return []testconnectors.ConnectionsFileConnection{notesConnectionWithAuthorization(harness.provider, secondAuthorizationID)}
	}
	result := harness.mustCallResult("example-notes_get_note", map[string]any{"noteId": "welcome"}, "")
	require.Equal(t, outcomeRejected, result.outcome())
	require.Contains(t, result.text(), "now points to a different target")
	require.Zero(t, harness.provider.TotalRequests(), "the read used no credential of the second account")
	require.Empty(t, harness.clock.recordedSleeps())
}

// The keys connector resolves a refreshing credential and maps a credential
// error to its optional unauthorized branch, as some connectors do.
const keysConnectorID = "example-keys"

type keysCredentials struct {
	Token string `json:"token"`
}

var putKeyDefinition = sdkgo.MutationDefinition{
	Operation: sdkgo.OperationRef{ConnectorID: keysConnectorID, OperationID: "putKey"},
	Branches: []sdkgo.BranchDefinition{
		{ID: "stored", Description: "The key holds the value."},
		{ID: "unauthorized", Description: "The credential was refused; nothing was sent.", Optional: true},
		{ID: sdkgo.DefectBranchID, Description: "The call was invalid.", Optional: true},
	},
	StepDefaults: sdkgo.StepDefaults{ExecuteMethodTimeout: 10 * time.Second},
}

// keysProvider counts what reached the in-memory key store.
type keysProvider struct {
	stores    atomic.Int32
	refreshes atomic.Int32
}

func keysExposedAgentTools() []sdkgo.AgentToolMetadata {
	return []sdkgo.AgentToolMetadata{{
		Operation: putKeyDefinition.Operation, Kind: sdkgo.AgentOperationKindMutation, IsRetrySafe: true,
		ToolName: keysConnectorID + "_put_key", Description: "Store a value under a key.",
		BranchOutcomes: map[sdkgo.BranchID]sdkgo.AgentBranchOutcome{"unauthorized": sdkgo.AgentBranchOutcomeRejected},
		InputType:      "SaveRecordInput",
		InputSchema:    json.RawMessage(`{"type":"object","required":["name","value"],"properties":{"name":{"type":"string"},"value":{"type":"string"}}}`),
		Branches:       putKeyDefinition.Branches, StepDefaults: putKeyDefinition.StepDefaults,
	}}
}

func keysConnector(keys *keysProvider) registry.LinkedConnector {
	return registry.LinkedConnector{
		ConnectorID: keysConnectorID, DisplayName: "Example Keys", ModulePath: "example.com/connectors/example-keys/go",
		ModuleVersion: "v0.1.0", ExposedAgentTools: keysExposedAgentTools(),
		OpenAgentTools: func(project *projectconfig.LoadedProject, connectionName string) ([]sdkgo.AgentTool, error) {
			credentials, err := provider.NewRefreshingCredentialProvider(project.Connections, connectionKeyOf(keysConnectorID, connectionName),
				func(contents json.RawMessage) (keysCredentials, error) {
					var decoded keysCredentials
					return decoded, projectconfig.DecodeCredentials(contents, &decoded)
				},
				func(credentials keysCredentials) (json.RawMessage, error) { return json.Marshal(credentials) })
			if err != nil {
				return nil, err
			}
			tool, err := sdkgo.NewMutationAgentTool[testconnectors.SaveRecordInput, testconnectors.SavedRecord](
				putKeyMutation{credentials: credentials, keys: keys}, sdkgo.ConnectionRef{Provider: keysConnectorID, Name: connectionName}, keysExposedAgentTools()[0])
			if err != nil {
				return nil, err
			}
			return []sdkgo.AgentTool{tool}, nil
		},
	}
}

type putKeyMutation struct {
	credentials *provider.RefreshingCredentialProvider[keysCredentials]
	keys        *keysProvider
}

func (putKeyMutation) Definition() sdkgo.MutationDefinition { return putKeyDefinition }

func (putKeyMutation) IdempotencyKey(id sdkgo.CallID, _ testconnectors.SaveRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

func (mutation putKeyMutation) Invoke(call sdkgo.Call, input testconnectors.SaveRecordInput) sdkgo.MutationAttempt[testconnectors.SavedRecord] {
	credentials, err := sdkgo.ResolveCredential[keysCredentials](call.Context, mutation.credentials, call, keysRefreshDriver{keys: mutation.keys})
	if err != nil {
		failure := sdkgo.Failure{Kind: sdkgo.FailureAuthentication, Provider: keysConnectorID, Operation: "putKey", Message: "the credential was refused"}
		return sdkgo.NewMutationBranch("unauthorized", testconnectors.SavedRecord{}, &failure, sdkgo.Receipt{})
	}
	if credentials.Token == "" {
		panic("putKey resolved an empty token")
	}
	mutation.keys.stores.Add(1)
	return sdkgo.NewMutationBranch("stored", testconnectors.SavedRecord{Value: input.Value}, nil, sdkgo.Receipt{})
}

// keysRefreshDriver refreshes an expired token without a network request.
type keysRefreshDriver struct{ keys *keysProvider }

func (keysRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[keysCredentials]) bool {
	return state.ExpiresAt != nil && !state.ExpiresAt.After(state.Now)
}

func (driver keysRefreshDriver) Refresh(_ context.Context, state sdkgo.CredentialRefreshState[keysCredentials]) (sdkgo.CredentialRefreshResult[keysCredentials], error) {
	count := driver.keys.refreshes.Add(1)
	return sdkgo.CredentialRefreshResult[keysCredentials]{
		Credentials: keysCredentials{Token: state.Credentials.Token + "-refreshed-" + strconv.Itoa(int(count))},
		ExpiresAt:   state.Now.Add(time.Hour),
	}, nil
}

func keysConnection(authorizationID string, expiresAt *time.Time) testconnectors.ConnectionsFileConnection {
	return testconnectors.ConnectionsFileConnection{
		ConnectorID: keysConnectorID, ConnectionName: "work", Configuration: map[string]any{},
		Credentials: keysCredentials{Token: "key-token"}, CredentialExpiresAt: expiresAt, AuthorizationID: authorizationID,
	}
}

func TestAConnectorThatMapsTheRefusedCredentialToABranchIsRechecked(t *testing.T) {
	keys := &keysProvider{}
	var harness *testHarness
	rebind := &rebindOnceBeforeAttempt{operationID: "putKey", path: func() string { return harness.connectionsFile }}
	harness = newTestHarness(t, harnessOptions{extraConnectors: []registry.LinkedConnector{keysConnector(keys)}, observe: rebind.observe})
	harness.writeConnectionRecords(testconnectors.NotesConnection("work", harness.provider), keysConnection(firstAuthorizationID, nil))
	rebind.connections = func() []testconnectors.ConnectionsFileConnection {
		return []testconnectors.ConnectionsFileConnection{testconnectors.NotesConnection("work", harness.provider), keysConnection(secondAuthorizationID, nil)}
	}
	result := harness.mustCallResult("example-keys_put_key", map[string]any{"name": "a", "value": "1"}, "flow/turn/branch")
	require.Equal(t, outcomeRejected, result.outcome(), result.text())
	require.Equal(t, "unauthorized", result.Meta[metaKeyBranch])
	require.Equal(t, "example-keys putKey returned unauthorized: AUTHENTICATION: the credential was refused; the connection now points to a different target", result.text())
	require.Zero(t, keys.stores.Load())
}

func TestARefreshUnderTheExpectedTargetKeepsItsAuthorization(t *testing.T) {
	keys := &keysProvider{}
	harness := newTestHarness(t, harnessOptions{extraConnectors: []registry.LinkedConnector{keysConnector(keys)}})
	expired := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	harness.writeConnectionRecords(testconnectors.NotesConnection("work", harness.provider), keysConnection(firstAuthorizationID, &expired))
	before := harness.server.CurrentToolSet(t.Context()).toolByName("example-keys_put_key").connectionTarget

	result := harness.mustCallResult("example-keys_put_key", map[string]any{"name": "a", "value": "1"}, "flow/turn/refresh")
	require.False(t, result.IsError, result.text())
	require.Equal(t, int32(1), keys.refreshes.Load(), "the guarded refresh ran once")
	require.Equal(t, int32(1), keys.stores.Load())

	contents, err := os.ReadFile(harness.connectionsFile)
	require.NoError(t, err)
	var file struct {
		Connections []struct {
			ConnectorID     string          `json:"connectorId"`
			AuthorizationID string          `json:"authorizationId"`
			Credentials     keysCredentials `json:"credentials"`
		} `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(contents, &file))
	var written bool
	for _, connection := range file.Connections {
		if connection.ConnectorID == keysConnectorID {
			written = true
			require.Equal(t, firstAuthorizationID, connection.AuthorizationID, "the write-back keeps the authorization")
			require.Equal(t, "key-token-refreshed-1", connection.Credentials.Token)
		}
	}
	require.True(t, written)
	require.Equal(t, before, harness.server.CurrentToolSet(t.Context()).toolByName("example-keys_put_key").connectionTarget, "a refresh keeps the target")
}

// legacyConnectionTargetDigest is the connectionTarget formula of dex-connectors-mcp 0.1.0.
func legacyConnectionTargetDigest(t *testing.T, configuration projectconfig.ConnectionConfiguration) string {
	t.Helper()
	settings, err := canonicaljson.Canonicalize(configuration.Configuration)
	require.NoError(t, err)
	encoded, err := canonicaljson.Marshal(map[string]any{
		"authMethodId": configuration.AuthMethodID, "configuration": json.RawMessage(settings),
		"connectionName": configuration.ConnectionName, "connectorId": configuration.ConnectorID,
		"provider": configuration.Provider, "sharedConnectionId": configuration.SharedConnectionID,
	})
	require.NoError(t, err)
	return "sha256:" + canonicaljson.SHA256Hex(encoded)
}

// rewriteRecordTarget stores target as the key's connectionTarget, as a 0.1.0 server would have.
func rewriteRecordTarget(t *testing.T, harness *testHarness, key string, target string) {
	t.Helper()
	store, err := callstate.Open(harness.stateDirectory, harness.clock.Now)
	require.NoError(t, err)
	lock, err := store.Lock(t.Context(), callstate.StateKey(key))
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Release()) }()
	record, isFound, err := lock.Read()
	require.NoError(t, err)
	require.True(t, isFound)
	record.ConnectionTarget = target
	require.NoError(t, lock.Write(&record))
}

func TestARunningRecordOfTheEarlierFingerprintSettlesWithoutSending(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{callTimeBudget: 10500 * time.Millisecond})
	project, err := projectconfig.LoadDexWebConnectionsFile(t.Context(), harness.connectionsFile)
	require.NoError(t, err)
	legacyTarget := legacyConnectionTargetDigest(t, project.Configuration.Connections[0])
	require.NotEqual(t, harness.server.CurrentToolSet(t.Context()).toolByName("example-notes_save_record").connectionTarget, legacyTarget)

	harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	arguments := map[string]any{"name": "a", "value": "1"}
	require.NotNil(t, harness.callTool("example-notes_save_record", arguments, "flow/turn/legacy-running").Error, "the key stays running")
	rewriteRecordTarget(t, harness, "flow/turn/legacy-running", legacyTarget)
	requestsBefore := harness.provider.TotalRequests()

	settled := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/legacy-running")
	require.Equal(t, outcomeUnknown, settled.outcome(), "attempt 1 may have applied")
	require.Contains(t, settled.text(), "the connection now points to a different target")
	require.Equal(t, requestsBefore, harness.provider.TotalRequests(), "the upgraded server never runs an earlier record")

	completed := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/legacy-completed")
	require.False(t, completed.IsError, completed.text())
	rewriteRecordTarget(t, harness, "flow/turn/legacy-completed", legacyTarget)
	replay := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/legacy-completed")
	require.Equal(t, true, replay.Meta[metaKeyReplayed], "a completed earlier record still replays")
	require.Equal(t, completed.text(), replay.text())
}

// memoryObjectStore is an in-memory projectconfig.ObjectStore, which cannot
// read a connection's target with its credential, as hosted storage cannot.
type memoryObjectStore struct {
	mu       sync.Mutex
	objects  map[string]projectconfig.Object
	sequence int
}

func (store *memoryObjectStore) ReadObject(_ context.Context, key, version string) (projectconfig.Object, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	object, isFound := store.objects[key]
	if !isFound || (version != "" && version != object.Version) {
		return projectconfig.Object{}, projectconfig.ErrObjectNotFound
	}
	object.Contents = append([]byte(nil), object.Contents...)
	return object, nil
}

func (store *memoryObjectStore) CreateObject(ctx context.Context, key string, contents []byte) (projectconfig.Object, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, isFound := store.objects[key]; isFound {
		return projectconfig.Object{}, projectconfig.ErrConflict
	}
	return store.putLocked(key, contents), nil
}

func (store *memoryObjectStore) CompareAndSwapObject(_ context.Context, key, expectedETag string, contents []byte) (projectconfig.Object, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if object, isFound := store.objects[key]; !isFound || object.ETag != expectedETag {
		return projectconfig.Object{}, projectconfig.ErrConflict
	}
	return store.putLocked(key, contents), nil
}

func (store *memoryObjectStore) putLocked(key string, contents []byte) projectconfig.Object {
	store.sequence++
	object := projectconfig.Object{Key: key, Version: "v" + strconv.Itoa(store.sequence), ETag: "e" + strconv.Itoa(store.sequence), Contents: append([]byte(nil), contents...)}
	store.objects[key] = object
	return object
}

// fixedProjectSource serves one snapshot, as hostedProjectSource does.
type fixedProjectSource struct{ snapshot projectSnapshot }

func (fixedProjectSource) fingerprint() string { return "fixed" }

func (source fixedProjectSource) load(context.Context) projectSnapshot { return source.snapshot }

func TestAHostedConnectionOffersOnlyReadsAndRunsThemWithoutAnExpectedTarget(t *testing.T) {
	fakeProvider := testconnectors.NewFakeProvider()
	t.Cleanup(fakeProvider.Close)
	scope := projectconfig.Scope{ProjectID: "project-1", Kind: "live"}
	connections, err := projectconfig.NewConnectionStore(&projectconfig.ConnectionStoreConfig{Objects: &memoryObjectStore{objects: map[string]projectconfig.Object{}}, Scope: scope})
	require.NoError(t, err)
	key := connectionKeyOf(testconnectors.NotesConnectorID, "work")
	credentials, err := json.Marshal(testconnectors.NotesCredentials{Token: testconnectors.ProviderToken})
	require.NoError(t, err)
	_, err = connections.ReplaceCredential(t.Context(), key, 0, projectconfig.CredentialMaterial{Credentials: credentials, AuthMethod: "apiKey"})
	require.NoError(t, err)
	settings, err := json.Marshal(testconnectors.NotesConfig{BaseURL: fakeProvider.URL()})
	require.NoError(t, err)
	project := &projectconfig.LoadedProject{Connections: connections, Configuration: projectconfig.Configuration{Connections: []projectconfig.ConnectionConfiguration{{
		ConnectorID: testconnectors.NotesConnectorID, ConnectionName: "work", Provider: testconnectors.NotesConnectorID, Configuration: settings,
	}}}}

	linked, err := registry.New([]registry.LinkedConnector{testconnectors.NotesConnector()})
	require.NoError(t, err)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	builder := &toolSetBuilder{registry: linked, logger: logger, newServer: func(*ToolSet) *mcp.Server { return nil }}
	source, err := newToolSetSource(t.Context(), fixedProjectSource{snapshot: projectSnapshot{project: project, identity: "project:project-1/live"}}, fixedSelectionSource{}, builder, nil, logger)
	require.NoError(t, err)
	toolSet := source.Current(t.Context())
	require.Equal(t, []string{"example-notes_get_note"}, toolSet.ToolNames(), "write tools of a hosted connection are not offered")
	require.Contains(t, toolSet.Warnings(), "connection example-notes/work: its write tools are not offered: the target of a hosted or shared connection cannot be verified while a call resolves its credential")

	store, err := callstate.Open(filepath.Join(t.TempDir(), "state"), time.Now)
	require.NoError(t, err)
	executor := newToolCallExecutor(store, newFakeClock(), executorConfig{serverVersion: "test", callTimeBudget: time.Minute, callStateRetention: 720 * time.Hour, maxResultBytes: 65536, maximumInlineAttempts: 3},
		logger, source.Current, nil)
	read, err := executor.callTool(t.Context(), toolSet, &mcp.CallToolParamsRaw{Name: "example-notes_get_note", Arguments: json.RawMessage(`{"noteId":"welcome"}`)})
	require.NoError(t, err)
	require.False(t, read.IsError, "a hosted read runs without an expected target, which hosted storage would refuse")
	require.Equal(t, 1, fakeProvider.ReadRequests())

	_, err = executor.callTool(t.Context(), toolSet, &mcp.CallToolParamsRaw{
		Name: "example-notes_save_record", Arguments: json.RawMessage(`{"name":"a","value":"1"}`), Meta: mcp.Meta{metaKeyIdempotencyKey: "flow/turn/hosted"},
	})
	var rpcErr *jsonrpc.Error
	require.ErrorAs(t, err, &rpcErr, "an unoffered write is an unknown tool")
	require.Contains(t, rpcErr.Message, "unknown tool")
	require.Zero(t, fakeProvider.RecordRequests())
}

func TestOnlyADexWebRecordThatIsNotSharedHasAVerifiableTarget(t *testing.T) {
	connection := projectconfig.ConnectionConfiguration{ConnectorID: "example-notes", ConnectionName: "work", Provider: "example-notes", Configuration: json.RawMessage(`{}`)}
	shared := connection
	shared.SharedConnectionID = "shared-1"
	for _, testCase := range []struct {
		name                       string
		connection                 projectconfig.ConnectionConfiguration
		canVerifyConnectionTargets bool
		isVerifiable               bool
	}{
		{"a Dex Web record", connection, true, true},
		{"a shared Dex Web entry, served by the credential broker", shared, true, false},
		{"a hosted connection", connection, false, false},
	} {
		snapshot := projectSnapshot{
			project:                    &projectconfig.LoadedProject{Configuration: projectconfig.Configuration{Connections: []projectconfig.ConnectionConfiguration{testCase.connection}}},
			canVerifyConnectionTargets: testCase.canVerifyConnectionTargets,
		}
		target, skipReason := connectionTargetOf(snapshot, "example-notes", "work")
		require.Empty(t, skipReason, testCase.name)
		require.Equal(t, testCase.isVerifiable, target.isVerifiable, testCase.name)
		expected, err := testCase.connection.ConnectionTargetFingerprint()
		require.NoError(t, err)
		require.Equal(t, expected, target.fingerprint, testCase.name)
	}
	_, skipReason := connectionTargetOf(projectSnapshot{}, "example-notes", "work")
	require.Equal(t, "it has no configuration", skipReason)
}
