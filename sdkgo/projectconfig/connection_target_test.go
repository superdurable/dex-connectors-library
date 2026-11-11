// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

const (
	firstAuthorizationID  = "6f1d3c1e-2b0a-4c55-9d43-0a8f6c2e7b11"
	secondAuthorizationID = "c2a9e7d4-8f3b-4e61-a2d5-5b7c9e0f1a23"
)

func fingerprintEntry() projectconfig.ConnectionConfiguration {
	return projectconfig.ConnectionConfiguration{
		ConnectorID: "slack", ConnectionName: "main", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/slack/go",
		Provider: "slack", AuthMethodIDs: []string{"oauth", "bot_token"}, AuthorizationID: firstAuthorizationID,
		Configuration: json.RawMessage(`{"workspace": "acme", "note": "<a&b> café", "limits": {"b": 2, "a": 1.50, "big": 12345678901234567890123}}`),
	}
}

func requireFingerprint(t *testing.T, entry projectconfig.ConnectionConfiguration) string {
	t.Helper()
	fingerprint, err := entry.ConnectionTargetFingerprint()
	require.NoError(t, err)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, fingerprint)
	return fingerprint
}

func sha256Fingerprint(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// The canonical documents below are the documented format; another implementation hashing them must agree.
func TestConnectionTargetFingerprintGoldenVectors(t *testing.T) {
	const canonical = `{"authMethodId":"","authMethodIds":["oauth","bot_token"],"authorizationId":"6f1d3c1e-2b0a-4c55-9d43-0a8f6c2e7b11",` +
		`"configuration":{"limits":{"a":1.50,"b":2,"big":12345678901234567890123},"note":"<a&b> café","workspace":"acme"},` +
		`"connectionName":"main","connectorId":"slack","provider":"slack","schema":"connectors.dex.dev/connection-target/v1","sharedConnectionId":""}`
	fingerprint := requireFingerprint(t, fingerprintEntry())
	require.Equal(t, sha256Fingerprint(canonical), fingerprint)
	require.Equal(t, "sha256:f5fb086f78b4593e12ccc78d4ecafaa11609e7c1515b9ebc7d1470686ca31185", fingerprint)

	older := projectconfig.ConnectionConfiguration{
		ConnectorID: "example", ConnectionName: "main", Provider: "example", AuthMethodID: "api_key",
		Configuration: json.RawMessage(`{}`),
	}
	const olderCanonical = `{"authMethodId":"api_key","authMethodIds":[],"authorizationId":"","configuration":{},` +
		`"connectionName":"main","connectorId":"example","provider":"example","schema":"connectors.dex.dev/connection-target/v1","sharedConnectionId":""}`
	require.Equal(t, sha256Fingerprint(olderCanonical), requireFingerprint(t, older))
	require.Equal(t, "sha256:079cb7b9b073b46e90dfa39bbca2df08fe1540859e73c2025e9bb58ef8276c25", requireFingerprint(t, older))

	older.AuthMethodIDs = []string{}
	require.Equal(t, sha256Fingerprint(olderCanonical), requireFingerprint(t, older), "nil and empty authMethodIds are equal")

	older.Configuration = nil
	const emptyCanonical = `{"authMethodId":"api_key","authMethodIds":[],"authorizationId":"","configuration":null,` +
		`"connectionName":"main","connectorId":"example","provider":"example","schema":"connectors.dex.dev/connection-target/v1","sharedConnectionId":""}`
	require.Equal(t, sha256Fingerprint(emptyCanonical), requireFingerprint(t, older))
}

func TestConnectionTargetFingerprintIgnoresEncodingModuleAndCredentials(t *testing.T) {
	base := requireFingerprint(t, fingerprintEntry())

	reordered := fingerprintEntry()
	reordered.Configuration = json.RawMessage("\n{ \"limits\" : {\"big\":12345678901234567890123, \"a\":1.50,\"b\":2},\t\"workspace\":\"acme\",\"note\":\"\\u003ca\\u0026b> caf\\u00e9\" }\n")
	require.Equal(t, base, requireFingerprint(t, reordered))

	upgraded := fingerprintEntry()
	upgraded.ModulePath = "github.com/superdurable/dex-connectors-library/connectors/slack/go/v2"
	require.Equal(t, base, requireFingerprint(t, upgraded))
}

func TestConnectionTargetFingerprintChangesWithEveryTargetField(t *testing.T) {
	changes := map[string]func(*projectconfig.ConnectionConfiguration){
		"authorizationId":        func(entry *projectconfig.ConnectionConfiguration) { entry.AuthorizationID = secondAuthorizationID },
		"authorizationId absent": func(entry *projectconfig.ConnectionConfiguration) { entry.AuthorizationID = "" },
		"configuration value": func(entry *projectconfig.ConnectionConfiguration) {
			entry.Configuration = json.RawMessage(`{"workspace": "other", "note": "<a&b> café", "limits": {"b": 2, "a": 1.50, "big": 12345678901234567890123}}`)
		},
		"configuration number text": func(entry *projectconfig.ConnectionConfiguration) {
			entry.Configuration = json.RawMessage(`{"workspace": "acme", "note": "<a&b> café", "limits": {"b": 2, "a": 1.5, "big": 12345678901234567890123}}`)
		},
		"provider":      func(entry *projectconfig.ConnectionConfiguration) { entry.Provider = "slack-enterprise" },
		"authMethodId":  func(entry *projectconfig.ConnectionConfiguration) { entry.AuthMethodID = "oauth" },
		"authMethodIds": func(entry *projectconfig.ConnectionConfiguration) { entry.AuthMethodIDs = []string{"oauth"} },
		"authMethodIds order": func(entry *projectconfig.ConnectionConfiguration) {
			entry.AuthMethodIDs = []string{"bot_token", "oauth"}
		},
		"sharedConnectionId": func(entry *projectconfig.ConnectionConfiguration) { entry.SharedConnectionID = "shared-1" },
		"connectionName":     func(entry *projectconfig.ConnectionConfiguration) { entry.ConnectionName = "other" },
		"connectorId":        func(entry *projectconfig.ConnectionConfiguration) { entry.ConnectorID = "slack-two" },
	}
	seen := map[string]string{"base": requireFingerprint(t, fingerprintEntry())}
	for name, change := range changes {
		entry := fingerprintEntry()
		change(&entry)
		fingerprint := requireFingerprint(t, entry)
		for previous, previousFingerprint := range seen {
			require.NotEqual(t, previousFingerprint, fingerprint, "%s collides with %s", name, previous)
		}
		seen[name] = fingerprint
	}
}

func TestConnectionTargetFingerprintRejectsInvalidConfiguration(t *testing.T) {
	for _, configuration := range []string{`{"workspace":`, `{} {}`, `not json`} {
		entry := fingerprintEntry()
		entry.Configuration = json.RawMessage(configuration)
		_, err := entry.ConnectionTargetFingerprint()
		require.Error(t, err, configuration)
	}
}

// targetRecord is the part of a Dex Web connection record that the guard tests vary.
type targetRecord struct {
	authorizationID string
	workspace       string
	accessToken     string
	refreshToken    string
	expiresAt       *time.Time
}

// writeTargetConnections writes connections.json with one record, an unknown record member and an unknown top-level
// member. An empty authorizationID writes a record from before Dex Web added it.
func writeTargetConnections(t *testing.T, path string, record targetRecord) {
	t.Helper()
	connection := map[string]any{
		"connectorId": "example", "authMethodId": "oauth", "authMethodIds": []string{"oauth", "api_key"},
		"modulePath": "github.com/superdurable/dex-connectors-library/connectors/example/go", "provider": "example",
		"connectionName": "main", "futureMember": "kept",
		"configuration": map[string]any{"workspace": record.workspace},
		"credentials":   map[string]any{"access_token": record.accessToken, "refresh_token": record.refreshToken},
	}
	if record.authorizationID != "" {
		connection["authorizationId"] = record.authorizationID
	}
	if record.expiresAt != nil {
		connection["credentialExpiresAt"] = record.expiresAt.UTC().Format(time.RFC3339Nano)
	}
	writeJSON(t, path, map[string]any{
		"schemaVersion":   "connectors.dex.dev/local-connections/v1alpha1",
		"connections":     []any{connection},
		"triggerBindings": []any{},
		"futureTopLevel":  map[string]any{"kept": true},
	})
}

func firstTarget(expiresAt time.Time) targetRecord {
	return targetRecord{authorizationID: firstAuthorizationID, workspace: "acme", accessToken: "access-1", refreshToken: "refresh-1", expiresAt: &expiresAt}
}

func loadTargetProject(t *testing.T, record targetRecord) (path string, project *projectconfig.LoadedProject, fingerprint string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "connections.json")
	writeTargetConnections(t, path, record)
	project, err := projectconfig.LoadDexWebConnectionsFile(context.Background(), path)
	require.NoError(t, err)
	return path, project, requireFingerprint(t, project.Configuration.Connections[0])
}

// requiringDriver always asks for a refresh and counts refresh requests.
type requiringDriver struct {
	mu        sync.Mutex
	refreshes int
}

func (*requiringDriver) RefreshRequired(state projectconfig.RefreshState[localCredentials]) bool {
	return state.Credentials.AccessToken != "access-2"
}

func (driver *requiringDriver) Refresh(_ context.Context, state projectconfig.RefreshState[localCredentials]) (projectconfig.RefreshResult[localCredentials], error) {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	driver.refreshes++
	return projectconfig.RefreshResult[localCredentials]{
		Credentials: localCredentials{AccessToken: "access-2", RefreshToken: "refresh-2"}, ExpiresAt: state.Now.Add(time.Hour),
	}, nil
}

func (driver *requiringDriver) refreshCount() int {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	return driver.refreshes
}

func TestDexWebEntryExposesAuthorizationIDAndAuthMethodIDs(t *testing.T) {
	_, project, _ := loadTargetProject(t, firstTarget(time.Now().Add(time.Hour)))
	entry := project.Configuration.Connections[0]
	require.Equal(t, firstAuthorizationID, entry.AuthorizationID)
	require.Equal(t, []string{"oauth", "api_key"}, entry.AuthMethodIDs)
	require.Empty(t, entry.AuthMethodID)

	encoded, err := json.Marshal(entry)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "authorizationId", "the configuration document never carries it")

	older := firstTarget(time.Now().Add(time.Hour))
	older.authorizationID = ""
	_, olderProject, _ := loadTargetProject(t, older)
	require.Empty(t, olderProject.Configuration.Connections[0].AuthorizationID)
}

func TestGuardedResolutionReturnsCredentialWhenTargetMatches(t *testing.T) {
	_, project, fingerprint := loadTargetProject(t, firstTarget(time.Now().Add(-time.Minute)))
	ctx := projectconfig.ContextWithExpectedConnectionTarget(context.Background(), localKey, fingerprint)
	resolver := localResolver(t, project)

	driver := &requiringDriver{}
	credentials, err := resolver.ResolveWithRefresh(ctx, driver)
	require.NoError(t, err)
	require.Equal(t, "access-2", credentials.AccessToken)
	require.Equal(t, 1, driver.refreshCount())

	credentials, err = resolver.Resolve(ctx)
	require.NoError(t, err)
	require.Equal(t, "access-2", credentials.AccessToken)
	material, _, err := project.Connections.ReadCredentialMaterial(ctx, localKey)
	require.NoError(t, err)
	require.JSONEq(t, `{"access_token":"access-2","refresh_token":"refresh-2"}`, string(material.Credentials))
}

func TestGuardedResolutionRefusesRebindAfterCallerCheck(t *testing.T) {
	path, project, fingerprint := loadTargetProject(t, firstTarget(time.Now().Add(-time.Minute)))
	resolver := localResolver(t, project)
	// The caller checked the target; Dex Web then rebinds the connection to another account.
	rebound := targetRecord{authorizationID: secondAuthorizationID, workspace: "acme", accessToken: "access-other", refreshToken: "refresh-other"}
	writeTargetConnections(t, path, rebound)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	ctx := projectconfig.ContextWithExpectedConnectionTarget(context.Background(), localKey, fingerprint)

	driver := &requiringDriver{}
	_, err = resolver.ResolveWithRefresh(ctx, driver)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
	require.NotErrorIs(t, err, projectconfig.ErrRefreshFailed)
	require.NotErrorIs(t, err, projectconfig.ErrReauthorizationRequired)
	_, err = resolver.Resolve(ctx)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
	_, err = resolver.ResolveAfterRejection(ctx, driver)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
	_, _, err = project.Connections.ReadCredentialMaterial(ctx, localKey)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
	require.Zero(t, driver.refreshCount())

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "a refused resolution writes nothing")
	connection, err := project.Connections.ReadConnection(context.Background(), localKey)
	require.NoError(t, err)
	require.Equal(t, projectconfig.CredentialReady, connection.Status)

	// The new target is usable by a caller that expects it.
	reboundFingerprint := requireFingerprint(t, reloadEntry(t, path))
	credentials, err := resolver.Resolve(projectconfig.ContextWithExpectedConnectionTarget(context.Background(), localKey, reboundFingerprint))
	require.NoError(t, err)
	require.Equal(t, "access-other", credentials.AccessToken)
}

func reloadEntry(t *testing.T, path string) projectconfig.ConnectionConfiguration {
	t.Helper()
	project, err := projectconfig.LoadDexWebConnectionsFile(context.Background(), path)
	require.NoError(t, err)
	return project.Configuration.Connections[0]
}

func TestGuardedResolutionRefusesSettingsChangeAndRemoval(t *testing.T) {
	future := time.Now().Add(time.Hour)
	path, project, fingerprint := loadTargetProject(t, firstTarget(future))
	resolver := localResolver(t, project)
	ctx := projectconfig.ContextWithExpectedConnectionTarget(context.Background(), localKey, fingerprint)

	settingsOnly := firstTarget(future)
	settingsOnly.workspace = "other"
	writeTargetConnections(t, path, settingsOnly)
	_, err := resolver.Resolve(ctx)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)

	writeEmptyConnections(t, path)
	_, err = resolver.Resolve(ctx)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
	require.ErrorContains(t, err, "no longer in the file")
}

func writeEmptyConnections(t *testing.T, path string) {
	t.Helper()
	writeJSON(t, path, map[string]any{"schemaVersion": "connectors.dex.dev/local-connections/v1alpha1", "connections": []any{}, "triggerBindings": []any{}})
}

// A connection removed from the file is never used or refreshed, with or without an expected target.
func TestRemovedConnectionIsNeitherResolvedNorRefreshed(t *testing.T) {
	path, project, _ := loadTargetProject(t, firstTarget(time.Now().Add(-time.Minute)))
	resolver := localResolver(t, project)
	writeEmptyConnections(t, path)

	_, err := resolver.Resolve(context.Background())
	require.ErrorContains(t, err, "no longer in the file")
	require.NotErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
	_, _, err = project.Connections.ReadCredentialMaterial(context.Background(), localKey)
	require.ErrorContains(t, err, "no longer in the file")
	driver := &requiringDriver{}
	_, err = resolver.ResolveWithRefresh(context.Background(), driver)
	require.ErrorIs(t, err, projectconfig.ErrRefreshFailed)
	require.ErrorContains(t, err, "no longer in the file")
	_, err = resolver.ResolveAfterRejection(context.Background(), driver)
	require.ErrorContains(t, err, "no longer in the file")
	require.Zero(t, driver.refreshCount())
	requireFileUnchangedByResolution(t, path)
}

func requireFileUnchangedByResolution(t *testing.T, path string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.JSONEq(t, `{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[],"triggerBindings":[]}`, string(contents))
}

// The removal lands after the check under the refresh lock and before the unchanged rewrite.
func TestRemovalBetweenCheckAndRewriteSendsNoRefresh(t *testing.T) {
	for name, isGuarded := range map[string]bool{"unguarded": false, "guarded": true} {
		t.Run(name, func(t *testing.T) {
			path, project, fingerprint := loadTargetProject(t, firstTarget(time.Now().Add(time.Hour)))
			driver := &rewritingDriver{t: t, path: path, isRemoval: true}
			ctx := context.Background()
			if isGuarded {
				ctx = projectconfig.ContextWithExpectedConnectionTarget(ctx, localKey, fingerprint)
			}

			_, err := localResolver(t, project).ResolveWithRefresh(ctx, driver)
			if isGuarded {
				require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
			} else {
				require.ErrorIs(t, err, projectconfig.ErrRefreshFailed)
				require.NotErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
			}
			require.ErrorContains(t, err, "no longer in the file")
			require.Zero(t, driver.refreshes)
			requireFileUnchangedByResolution(t, path)
		})
	}
}

func TestGuardedResolutionKeepsTargetAcrossAnotherProcessRefresh(t *testing.T) {
	future := time.Now().Add(time.Hour)
	path, project, fingerprint := loadTargetProject(t, firstTarget(future))
	refreshed := firstTarget(future.Add(time.Hour))
	refreshed.accessToken, refreshed.refreshToken = "access-refreshed", "refresh-refreshed"
	writeTargetConnections(t, path, refreshed)

	credentials, err := localResolver(t, project).Resolve(projectconfig.ContextWithExpectedConnectionTarget(context.Background(), localKey, fingerprint))
	require.NoError(t, err)
	require.Equal(t, "access-refreshed", credentials.AccessToken)
}

func TestGuardedResolutionOfOlderRecordWithoutAuthorizationID(t *testing.T) {
	future := time.Now().Add(time.Hour)
	older := firstTarget(future)
	older.authorizationID = ""
	path, project, fingerprint := loadTargetProject(t, older)
	entry := project.Configuration.Connections[0]
	entry.AuthorizationID = ""
	require.Equal(t, fingerprint, requireFingerprint(t, entry))
	resolver := localResolver(t, project)
	ctx := projectconfig.ContextWithExpectedConnectionTarget(context.Background(), localKey, fingerprint)

	credentials, err := resolver.Resolve(ctx)
	require.NoError(t, err)
	require.Equal(t, "access-1", credentials.AccessToken)

	// The first reauthorization adds an authorizationId, which changes the target.
	writeTargetConnections(t, path, firstTarget(future))
	_, err = resolver.Resolve(ctx)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
}

func TestGuardedResolutionAppliesOnlyToItsConnection(t *testing.T) {
	_, project, fingerprint := loadTargetProject(t, firstTarget(time.Now().Add(time.Hour)))
	resolver := localResolver(t, project)
	other := projectconfig.ConnectionKey{ConnectorID: "example", ConnectionName: "other"}

	_, err := resolver.Resolve(projectconfig.ContextWithExpectedConnectionTarget(context.Background(), other, "sha256:unrelated"))
	require.NoError(t, err)

	for _, expected := range []string{"", "sha256:malformed", fingerprint + "0"} {
		_, err = resolver.Resolve(projectconfig.ContextWithExpectedConnectionTarget(context.Background(), localKey, expected))
		require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged, expected)
	}

	replaced := projectconfig.ContextWithExpectedConnectionTarget(context.Background(), localKey, "sha256:stale")
	replaced = projectconfig.ContextWithExpectedConnectionTarget(replaced, other, "sha256:unrelated")
	replaced = projectconfig.ContextWithExpectedConnectionTarget(replaced, localKey, fingerprint)
	_, err = resolver.Resolve(replaced)
	require.NoError(t, err)
}

func TestGuardedResolutionOfConcurrentCallersSendsOneRefresh(t *testing.T) {
	_, project, fingerprint := loadTargetProject(t, firstTarget(time.Now().Add(-time.Minute)))
	ctx := projectconfig.ContextWithExpectedConnectionTarget(context.Background(), localKey, fingerprint)
	driver := &requiringDriver{}
	var group sync.WaitGroup
	errs := make(chan error, 8)
	resolvers := make([]*projectconfig.CredentialResolver[localCredentials], 8)
	for index := range resolvers {
		resolvers[index] = localResolver(t, project)
	}
	for _, resolver := range resolvers {
		group.Add(1)
		go func() {
			defer group.Done()
			credentials, err := resolver.ResolveWithRefresh(ctx, driver)
			if err == nil && credentials.AccessToken != "access-2" {
				err = os.ErrInvalid
			}
			errs <- err
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, driver.refreshCount())
}

func TestRefreshWriteBackPreservesAuthorizationIDAndUnknownMembers(t *testing.T) {
	path, project, fingerprint := loadTargetProject(t, firstTarget(time.Now().Add(-time.Minute)))
	driver := &requiringDriver{}
	_, err := localResolver(t, project).ResolveWithRefresh(context.Background(), driver)
	require.NoError(t, err)
	require.Equal(t, 1, driver.refreshCount())

	var file struct {
		FutureTopLevel json.RawMessage              `json:"futureTopLevel"`
		Connections    []map[string]json.RawMessage `json:"connections"`
	}
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(contents, &file))
	require.JSONEq(t, `{"kept":true}`, string(file.FutureTopLevel))
	record := file.Connections[0]
	require.JSONEq(t, `"`+firstAuthorizationID+`"`, string(record["authorizationId"]))
	require.JSONEq(t, `"kept"`, string(record["futureMember"]))
	require.JSONEq(t, `{"access_token":"access-2","refresh_token":"refresh-2"}`, string(record["credentials"]))

	// A restart re-reads the settings and keeps the authorization identity and the target.
	reloaded := reloadEntry(t, path)
	require.Equal(t, firstAuthorizationID, reloaded.AuthorizationID)
	require.Equal(t, fingerprint, requireFingerprint(t, reloaded))
}

func TestGuardedResolutionThroughGeneratedConnectorProvider(t *testing.T) {
	path, project, fingerprint := loadTargetProject(t, firstTarget(time.Now().Add(time.Hour)))
	refreshing, err := provider.NewRefreshingCredentialProvider(project.Connections, localKey, decodeLocalCredentials, encodeLocalCredentials)
	require.NoError(t, err)
	call := sdkgo.Call{
		Connection: sdkgo.ConnectionRef{Provider: "example", Name: "main"},
		Operation:  sdkgo.OperationRef{ConnectorID: "example", OperationID: "send"},
	}
	stepContext := testsupport.NewDexContext("flow-1", "step-1")
	stepContext.Context = projectconfig.ContextWithExpectedConnectionTarget(context.Background(), localKey, fingerprint)
	call.Context = stepContext
	driver := sdkDriver{}

	credentials, err := sdkgo.ResolveCredential[localCredentials](stepContext, refreshing, call, driver)
	require.NoError(t, err)
	require.Equal(t, "access-1", credentials.AccessToken)

	writeTargetConnections(t, path, targetRecord{authorizationID: secondAuthorizationID, workspace: "acme", accessToken: "access-other", refreshToken: "refresh-other"})
	_, err = sdkgo.ResolveCredential[localCredentials](stepContext, refreshing, call, driver)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
	require.False(t, sdkgo.IsReauthorizationRequired(err))

	apiKey, err := provider.NewCredentialProvider(project.Connections, localKey, decodeLocalCredentials)
	require.NoError(t, err)
	_, err = apiKey.Resolve(call)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
}

// sdkDriver is a connector-side refresh driver that never needs a refresh.
type sdkDriver struct{}

func (sdkDriver) RefreshRequired(sdkgo.CredentialRefreshState[localCredentials]) bool { return false }

func (sdkDriver) Refresh(context.Context, sdkgo.CredentialRefreshState[localCredentials]) (sdkgo.CredentialRefreshResult[localCredentials], error) {
	return sdkgo.CredentialRefreshResult[localCredentials]{}, os.ErrInvalid
}

func TestGuardedResolutionFailsClosedWithoutTargetSnapshots(t *testing.T) {
	objects := testsupport.NewObjectStore()
	store, err := projectconfig.NewConnectionStore(&projectconfig.ConnectionStoreConfig{Objects: objects, Scope: projectconfig.Scope{ProjectID: "project-1", Kind: "live"}})
	require.NoError(t, err)
	_, err = store.ReplaceCredential(context.Background(), localKey, 0, projectconfig.CredentialMaterial{
		Credentials: json.RawMessage(`{"access_token":"access-1","refresh_token":"refresh-1"}`), AuthMethod: "oauth",
	})
	require.NoError(t, err)
	resolver, err := projectconfig.NewCredentialResolver(store, localKey, decodeLocalCredentials, encodeLocalCredentials)
	require.NoError(t, err)
	fingerprint := requireFingerprint(t, projectconfig.ConnectionConfiguration{ConnectorID: "example", ConnectionName: "main", Provider: "example", Configuration: json.RawMessage(`{}`)})
	ctx := projectconfig.ContextWithExpectedConnectionTarget(context.Background(), localKey, fingerprint)

	_, err = resolver.Resolve(ctx)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetUnverifiable)
	driver := &requiringDriver{}
	_, err = resolver.ResolveWithRefresh(ctx, driver)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetUnverifiable)
	_, err = resolver.ResolveAfterRejection(ctx, driver)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetUnverifiable)
	require.Zero(t, driver.refreshCount())

	credentials, err := resolver.Resolve(context.Background())
	require.NoError(t, err)
	require.Equal(t, "access-1", credentials.AccessToken)
}

func TestGuardedSharedEntryFailsClosedWithoutBrokerRequest(t *testing.T) {
	fixture := newSharedFixture(t, readyReply(1, sharedAccessOne, epoch, time.Hour))
	requestsBefore := fixture.broker.requestCount()
	var entry projectconfig.ConnectionConfiguration
	for _, candidate := range fixture.project.Configuration.Connections {
		if candidate.ConnectionName == sharedKey.ConnectionName {
			entry = candidate
		}
	}
	require.NotEmpty(t, entry.SharedConnectionID)
	ctx := projectconfig.ContextWithExpectedConnectionTarget(context.Background(), sharedKey, requireFingerprint(t, entry))
	resolver := fixture.resolver(t)

	_, err := resolver.Resolve(ctx)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetUnverifiable)
	_, err = resolver.ResolveWithRefresh(ctx, forbiddenDriver{t})
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetUnverifiable)
	_, err = resolver.ResolveAfterRejection(ctx, forbiddenDriver{t})
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetUnverifiable)
	require.Equal(t, requestsBefore, fixture.broker.requestCount())
}

// rewritingDriver writes rewrite into connections.json on its second refresh decision of an unexpired credential, made under the
// refresh lock just before the unchanged rewrite, and then requires a refresh only for access-1.
type rewritingDriver struct {
	t         *testing.T
	path      string
	rewrite   targetRecord
	isRemoval bool
	written   []byte
	decisions int
	refreshes int
}

func (driver *rewritingDriver) RefreshRequired(state projectconfig.RefreshState[localCredentials]) bool {
	driver.decisions++
	if driver.decisions == 2 {
		if driver.isRemoval {
			writeEmptyConnections(driver.t, driver.path)
		} else {
			writeTargetConnections(driver.t, driver.path, driver.rewrite)
		}
		written, err := os.ReadFile(driver.path)
		require.NoError(driver.t, err)
		driver.written = written
		return true
	}
	return state.Credentials.AccessToken == "access-1"
}

func (driver *rewritingDriver) Refresh(_ context.Context, state projectconfig.RefreshState[localCredentials]) (projectconfig.RefreshResult[localCredentials], error) {
	driver.refreshes++
	return projectconfig.RefreshResult[localCredentials]{
		Credentials: localCredentials{AccessToken: "access-2", RefreshToken: "refresh-2"}, ExpiresAt: state.Now.Add(time.Hour),
	}, nil
}

func reboundTarget() targetRecord {
	future := time.Now().Add(time.Hour)
	return targetRecord{authorizationID: secondAuthorizationID, workspace: "acme", accessToken: "access-other", refreshToken: "refresh-other", expiresAt: &future}
}

// A writer that skips the refresh lock rebinds after the guarded check under the lock; the rewrite must notice.
func TestGuardedRefreshRefusesRebindBetweenCheckAndRewrite(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour)
	// The settings-only edit keeps the credential and its expiry, so nothing but the guard notices it.
	settingsOnly := firstTarget(expiresAt)
	settingsOnly.workspace = "other"
	for name, rewrite := range map[string]targetRecord{"rebind": reboundTarget(), "settings": settingsOnly} {
		t.Run(name, func(t *testing.T) {
			path, project, fingerprint := loadTargetProject(t, firstTarget(expiresAt))
			driver := &rewritingDriver{t: t, path: path, rewrite: rewrite}

			_, err := localResolver(t, project).ResolveWithRefresh(projectconfig.ContextWithExpectedConnectionTarget(context.Background(), localKey, fingerprint), driver)
			require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
			require.Zero(t, driver.refreshes)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, driver.written, after, "the rewrite is never overwritten")
		})
	}
}

func TestUnguardedRefreshAdoptsReauthorizationBetweenCheckAndRewrite(t *testing.T) {
	path, project, _ := loadTargetProject(t, firstTarget(time.Now().Add(time.Hour)))
	driver := &rewritingDriver{t: t, path: path, rewrite: reboundTarget()}

	credentials, err := localResolver(t, project).ResolveWithRefresh(context.Background(), driver)
	require.NoError(t, err)
	require.Equal(t, "access-other", credentials.AccessToken)
	require.Zero(t, driver.refreshes, "the superseded credential is never refreshed")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, driver.written, after)
}

// A duplicated key fails only its own resolutions; another connection in the file resolves normally.
func TestDuplicateConnectionRecordFailsOnlyItsKey(t *testing.T) {
	path, project, fingerprint := loadTargetProject(t, firstTarget(time.Now().Add(time.Hour)))
	resolver := localResolver(t, project)
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	var file map[string]any
	require.NoError(t, json.Unmarshal(contents, &file))
	original := file["connections"].([]any)[0].(map[string]any)
	copyRecord := func(changes map[string]any) map[string]any {
		record := map[string]any{}
		for name, value := range original {
			record[name] = value
		}
		for name, value := range changes {
			record[name] = value
		}
		return record
	}
	duplicate := copyRecord(map[string]any{
		"authorizationId": secondAuthorizationID, "credentials": map[string]any{"access_token": "access-other", "refresh_token": "refresh-other"},
	})
	other := copyRecord(map[string]any{
		"connectionName": "other", "credentials": map[string]any{"access_token": "access-unrelated", "refresh_token": "refresh-unrelated"},
	})
	file["connections"] = []any{original, duplicate, other}
	writeJSON(t, path, file)
	guarded := projectconfig.ContextWithExpectedConnectionTarget(context.Background(), localKey, fingerprint)

	_, err = resolver.Resolve(guarded)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
	require.ErrorContains(t, err, "more than once")
	driver := &requiringDriver{}
	_, err = resolver.ResolveWithRefresh(guarded, driver)
	require.ErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
	_, err = resolver.Resolve(context.Background())
	require.ErrorContains(t, err, "more than once")
	require.NotErrorIs(t, err, projectconfig.ErrConnectionTargetChanged)
	_, err = resolver.ResolveWithRefresh(context.Background(), driver)
	require.ErrorContains(t, err, "more than once")
	require.Zero(t, driver.refreshCount())

	otherKey := projectconfig.ConnectionKey{ConnectorID: "example", ConnectionName: "other"}
	otherResolver, err := projectconfig.NewCredentialResolver(project.Connections, otherKey, decodeLocalCredentials, encodeLocalCredentials)
	require.NoError(t, err)
	otherFingerprint := requireFingerprint(t, projectconfig.ConnectionConfiguration{
		ConnectorID: "example", ConnectionName: "other", Provider: "example", AuthMethodIDs: []string{"oauth", "api_key"},
		AuthorizationID: firstAuthorizationID, Configuration: json.RawMessage(`{"workspace":"acme"}`),
	})
	for _, ctx := range []context.Context{context.Background(), projectconfig.ContextWithExpectedConnectionTarget(context.Background(), otherKey, otherFingerprint)} {
		credentials, err := otherResolver.Resolve(ctx)
		require.NoError(t, err)
		require.Equal(t, "access-unrelated", credentials.AccessToken)
	}

	// The configuration document cannot hold a duplicated connection, so loading the file fails.
	_, err = projectconfig.LoadDexWebConnectionsFile(context.Background(), path)
	require.ErrorContains(t, err, "invalid or duplicated")
}
