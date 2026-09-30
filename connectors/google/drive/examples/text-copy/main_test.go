// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	textcopy "github.com/superdurable/dex-connectors-library/connectors/google/drive/examples/text-copy/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("GOOGLE_DRIVE_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("GOOGLE_DRIVE_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("GOOGLE_DRIVE_EXAMPLE_MISSING", "fallback"))
}

func TestLoadFolderConfigurationTreatsAMissingPickAsBlank(t *testing.T) {
	directory := t.TempDir()
	connectionsPath := filepath.Join(directory, "connections.json")
	require.NoError(t, os.WriteFile(connectionsPath, []byte(`{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[]}`), 0o600))
	store, err := localconfig.LoadFile(connectionsPath)
	require.NoError(t, err)

	loaded, err := loadFolderConfiguration(store, textcopy.DestinationFolderConfigurationRef())
	require.NoError(t, err)
	require.Equal(t, textcopy.DestinationFolderConfigurationRef(), loaded.Reference)
	require.Empty(t, loaded.Value.FolderID)
}
