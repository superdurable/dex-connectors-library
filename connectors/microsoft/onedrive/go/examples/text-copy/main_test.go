// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	textcopy "github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/examples/text-copy/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("ONEDRIVE_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("ONEDRIVE_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("ONEDRIVE_EXAMPLE_MISSING", "fallback"))
}

func TestLoadLocationConfigurationTreatsMissingPicksAsTheOneDriveRoot(t *testing.T) {
	loaded, err := loadLocationConfiguration(projectconfig.Configuration{}, textcopy.DestinationLocationConfigurationRef())
	require.NoError(t, err)
	require.Equal(t, textcopy.DestinationLocationConfigurationRef(), loaded.Reference)
	require.Empty(t, loaded.Value.DriveID)
	require.Empty(t, loaded.Value.FolderID)
}
