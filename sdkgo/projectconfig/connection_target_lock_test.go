// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build darwin || linux

package projectconfig

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// A writer that holds the refresh lock, as Dex Web does, rebinds while a guarded refresh waits for it.
func TestGuardedRefreshRefusesRebindWhileWaitingForRefreshLock(t *testing.T) {
	path, project, resolver, guarded := loadLockTestProject(t)
	writerLock := holdFileLock(t, path+refreshLockFileSuffix)
	driver := newSignalingDriver()
	resolved := make(chan error, 1)
	go func() {
		_, resolveErr := resolver.ResolveWithRefresh(guarded, driver)
		resolved <- resolveErr
	}()
	<-driver.decided
	written := writeLockTestConnections(t, path, lockTestRebound)
	require.NoError(t, writerLock.Close())

	require.ErrorIs(t, <-resolved, ErrConnectionTargetChanged)
	require.Zero(t, driver.refreshes.Load())
	connection, err := project.Connections.ReadConnection(context.Background(), lockTestKey)
	require.NoError(t, err)
	require.Equal(t, CredentialReady, connection.Status)
	requireFileBytes(t, path, written)
}
