// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	textcopy "github.com/superdurable/dex-connectors-library/connectors/google/drive/examples/text-copy/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("GOOGLE_DRIVE_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("GOOGLE_DRIVE_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("GOOGLE_DRIVE_EXAMPLE_MISSING", "fallback"))
}

func TestLoadFolderConfigurationTreatsAMissingPickAsBlank(t *testing.T) {
	loaded, err := loadFolderConfiguration(projectconfig.Configuration{}, textcopy.DestinationFolderConfigurationRef())
	require.NoError(t, err)
	require.Equal(t, textcopy.DestinationFolderConfigurationRef(), loaded.Reference)
	require.Empty(t, loaded.Value.FolderID)
}
