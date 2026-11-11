// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build darwin || linux

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

// signalingDriver requires a refresh, signals its first decision, and counts refresh requests.
type signalingDriver struct {
	decided   chan struct{}
	signaled  atomic.Bool
	refreshes atomic.Int32
}

func (driver *signalingDriver) RefreshRequired(RefreshState[lockTestCredentials]) bool {
	if driver.signaled.CompareAndSwap(false, true) {
		close(driver.decided)
	}
	return true
}

func (driver *signalingDriver) Refresh(_ context.Context, state RefreshState[lockTestCredentials]) (RefreshResult[lockTestCredentials], error) {
	driver.refreshes.Add(1)
	return RefreshResult[lockTestCredentials]{Credentials: lockTestCredentials{AccessToken: "access-2", RefreshToken: "refresh-2"}, ExpiresAt: state.Now.Add(time.Hour)}, nil
}

func writeLockTestConnections(t *testing.T, path, authorizationID, accessToken string) {
	t.Helper()
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": dexWebConnectionsSchema,
		"connections": []any{map[string]any{
			"connectorId": "example", "connectionName": "main", "provider": "example", "authMethodId": "oauth",
			"modulePath": "github.com/superdurable/dex-connectors-library/connectors/example/go", "authorizationId": authorizationID,
			"configuration": map[string]any{"workspace": "acme"},
			"credentials":   map[string]any{"access_token": accessToken, "refresh_token": "refresh-1"},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
}

// A rebind that lands while a guarded refresh waits for another process's lock is refused before any refresh.
func TestGuardedRefreshRefusesRebindWhileWaitingForRefreshLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	writeLockTestConnections(t, path, "first-authorization", "access-1")
	project, err := loadDexWebProject(context.Background(), path)
	require.NoError(t, err)
	fingerprint, err := project.Configuration.Connections[0].ConnectionTargetFingerprint()
	require.NoError(t, err)
	key := ConnectionKey{ConnectorID: "example", ConnectionName: "main"}
	resolver, err := NewCredentialResolver(project.Connections, key,
		func(contents json.RawMessage) (lockTestCredentials, error) {
			var credentials lockTestCredentials
			return credentials, DecodeCredentials(contents, &credentials)
		},
		func(credentials lockTestCredentials) (json.RawMessage, error) { return json.Marshal(credentials) })
	require.NoError(t, err)

	otherProcessLock := holdFileLock(t, path+refreshLockFileSuffix)
	driver := &signalingDriver{decided: make(chan struct{})}
	resolved := make(chan error, 1)
	go func() {
		_, resolveErr := resolver.ResolveWithRefresh(ContextWithExpectedConnectionTarget(context.Background(), key, fingerprint), driver)
		resolved <- resolveErr
	}()
	<-driver.decided
	writeLockTestConnections(t, path, "second-authorization", "access-other")
	require.NoError(t, otherProcessLock.Close())

	require.ErrorIs(t, <-resolved, ErrConnectionTargetChanged)
	require.Zero(t, driver.refreshes.Load())
	connection, err := project.Connections.ReadConnection(context.Background(), key)
	require.NoError(t, err)
	require.Equal(t, CredentialReady, connection.Status)
}
