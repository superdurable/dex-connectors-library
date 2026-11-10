// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build darwin

package projectconfig_test

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// userImmutableFlag is UF_IMMUTABLE, which an owner may set and which forbids renaming over the file.
const userImmutableFlag = 0x2

// An immutable connections.json still reads and admits a temporary file, but the unchanged rewrite fails first.
func TestDexWebRefreshOfImmutableFileSendsNoProviderRequest(t *testing.T) {
	directory := t.TempDir()
	expired := time.Now().Add(-time.Minute)
	path := writeDexWebFiles(t, directory, "access-1", &expired)
	resolver := localResolver(t, loadLocal(t, path))
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, syscall.Chflags(path, userImmutableFlag))
	t.Cleanup(func() { _ = syscall.Chflags(path, 0) }) // Restores removal by t.TempDir.

	driver := &localDriver{}
	_, err = resolver.ResolveWithRefresh(context.Background(), driver)
	require.ErrorIs(t, err, projectconfig.ErrRefreshFailed)
	require.ErrorContains(t, err, "cannot be written back")
	require.Zero(t, driver.refreshes)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	requireNoTemporaryFiles(t, directory)
}
