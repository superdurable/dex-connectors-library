// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type lockTestCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// signalingDriver requires a refresh of access-1, signals its first decision, and counts refresh requests.
type signalingDriver struct {
	decided   chan struct{}
	signaled  atomic.Bool
	refreshes atomic.Int32
}

func (driver *signalingDriver) RefreshRequired(state RefreshState[lockTestCredentials]) bool {
	if driver.signaled.CompareAndSwap(false, true) {
		close(driver.decided)
	}
	return state.Credentials.AccessToken == "access-1"
}

func (driver *signalingDriver) Refresh(_ context.Context, state RefreshState[lockTestCredentials]) (RefreshResult[lockTestCredentials], error) {
	driver.refreshes.Add(1)
	return RefreshResult[lockTestCredentials]{Credentials: lockTestCredentials{AccessToken: "access-2", RefreshToken: "refresh-2"}, ExpiresAt: state.Now.Add(time.Hour)}, nil
}

func newSignalingDriver() *signalingDriver { return &signalingDriver{decided: make(chan struct{})} }

// lockTestRecord is the part of the one connections.json record that these tests vary.
type lockTestRecord struct {
	authorizationID string
	workspace       string
	accessToken     string
}

var (
	lockTestKey      = ConnectionKey{ConnectorID: "example", ConnectionName: "main"}
	lockTestFirst    = lockTestRecord{authorizationID: "first-authorization", workspace: "acme", accessToken: "access-1"}
	lockTestRebound  = lockTestRecord{authorizationID: "second-authorization", workspace: "acme", accessToken: "access-other"}
	lockTestSettings = lockTestRecord{authorizationID: "first-authorization", workspace: "other", accessToken: "access-1"}
)

func writeLockTestConnections(t *testing.T, path string, record lockTestRecord) []byte {
	t.Helper()
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": dexWebConnectionsSchema,
		"connections": []any{map[string]any{
			"connectorId": "example", "connectionName": "main", "provider": "example", "authMethodId": "oauth",
			"modulePath": "github.com/superdurable/dex-connectors-library/connectors/example/go", "authorizationId": record.authorizationID,
			"configuration": map[string]any{"workspace": record.workspace},
			"credentials":   map[string]any{"access_token": record.accessToken, "refresh_token": "refresh-1"},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	return contents
}

// loadLockTestProject loads lockTestFirst and returns its resolver and a context expecting its target.
func loadLockTestProject(t *testing.T) (path string, project *LoadedProject, resolver *CredentialResolver[lockTestCredentials], guarded context.Context) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "connections.json")
	writeLockTestConnections(t, path, lockTestFirst)
	project, err := loadDexWebProject(context.Background(), path)
	require.NoError(t, err)
	fingerprint, err := project.Configuration.Connections[0].ConnectionTargetFingerprint()
	require.NoError(t, err)
	resolver, err = NewCredentialResolver(project.Connections, lockTestKey,
		func(contents json.RawMessage) (lockTestCredentials, error) {
			var credentials lockTestCredentials
			return credentials, DecodeCredentials(contents, &credentials)
		},
		func(credentials lockTestCredentials) (json.RawMessage, error) { return json.Marshal(credentials) })
	require.NoError(t, err)
	return path, project, resolver, ContextWithExpectedConnectionTarget(context.Background(), lockTestKey, fingerprint)
}

// requireFileBytes fails unless path holds exactly expected, the last write a test made.
func requireFileBytes(t *testing.T, path string, expected []byte) {
	t.Helper()
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(expected), string(after), "a later write is never overwritten")
}

// rewriteDuringUnchangedRewrite makes a writer that skips the lock replace the file after the unchanged rewrite read it.
func rewriteDuringUnchangedRewrite(t *testing.T, project *LoadedProject, path string, record lockTestRecord) *[]byte {
	t.Helper()
	written := new([]byte)
	project.objects.(*dexWebStore).beforeUnchangedRewriteCheck = func() {
		if *written == nil {
			*written = writeLockTestConnections(t, path, record)
		}
	}
	return written
}

func TestGuardedRefreshRefusesRebindDuringUnchangedRewrite(t *testing.T) {
	for name, record := range map[string]lockTestRecord{"rebind": lockTestRebound, "settings": lockTestSettings} {
		t.Run(name, func(t *testing.T) {
			path, project, resolver, guarded := loadLockTestProject(t)
			written := rewriteDuringUnchangedRewrite(t, project, path, record)
			driver := newSignalingDriver()

			_, err := resolver.ResolveWithRefresh(guarded, driver)
			require.ErrorIs(t, err, ErrConnectionTargetChanged)
			require.Zero(t, driver.refreshes.Load())
			requireFileBytes(t, path, *written)
		})
	}
}

func TestUnguardedRefreshAdoptsReauthorizationDuringUnchangedRewrite(t *testing.T) {
	path, project, resolver, _ := loadLockTestProject(t)
	written := rewriteDuringUnchangedRewrite(t, project, path, lockTestRebound)
	driver := newSignalingDriver()

	credentials, err := resolver.ResolveWithRefresh(context.Background(), driver)
	require.NoError(t, err)
	require.Equal(t, "access-other", credentials.AccessToken)
	require.Zero(t, driver.refreshes.Load(), "the superseded credential is never refreshed")
	requireFileBytes(t, path, *written)
}

// An abandoned refresh's persisted result is not published when the target changed while the caller waited.
func TestGuardedRecoveryReverifiesTargetAfterRefreshLock(t *testing.T) {
	path, project, resolver, guarded := loadLockTestProject(t)
	ctx := context.Background()
	connections := project.Connections
	record, _, err := connections.readRecord(ctx, lockTestKey)
	require.NoError(t, err)
	admission, err := connections.beginExchange(ctx, lockTestKey, record.Revision, "attempt-1", time.Now().Add(time.Minute), CredentialRefreshing)
	require.NoError(t, err)
	record, _, err = connections.readRecord(ctx, lockTestKey)
	require.NoError(t, err)
	refreshedExpiry := time.Now().Add(time.Hour)
	result, err := json.Marshal(credentialEnvelope{
		SchemaVersion: connectionSchema, Scope: dexWebScope, Key: lockTestKey, Fence: admission.Fence, AttemptID: admission.AttemptID,
		Credentials: json.RawMessage(`{"access_token":"access-2","refresh_token":"refresh-2"}`), ExpiresAt: &refreshedExpiry, AuthMethod: record.AuthMethod,
	})
	require.NoError(t, err)
	_, err = connections.createImmutable(ctx, record.Mutation.ResultKey, result)
	require.NoError(t, err)

	// Settings change, with the same credential, while the recovering caller waits for the lock.
	var settingsChanged []byte
	project.objects.(*dexWebStore).onRefreshLockAcquired = func() {
		if settingsChanged == nil {
			settingsChanged = writeLockTestConnections(t, path, lockTestSettings)
		}
	}
	driver := newSignalingDriver()
	_, err = resolver.ResolveWithRefresh(guarded, driver)
	require.ErrorIs(t, err, ErrConnectionTargetChanged)
	require.Zero(t, driver.refreshes.Load())

	record, _, err = connections.readRecord(ctx, lockTestKey)
	require.NoError(t, err)
	require.Equal(t, CredentialRefreshing, record.Status, "the old target's refresh result is not published")
	requireFileBytes(t, path, settingsChanged)
}
