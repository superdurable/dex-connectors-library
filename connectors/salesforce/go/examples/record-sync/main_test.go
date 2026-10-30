// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	recordsync "github.com/superdurable/dex-connectors-library/connectors/salesforce/examples/record-sync/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("SALESFORCE_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("SALESFORCE_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("SALESFORCE_EXAMPLE_MISSING", "fallback"))
}

func TestLoadSyncConfigurationTreatsMissingUnitsAsBlank(t *testing.T) {
	loaded, err := loadSyncConfiguration(projectconfig.Configuration{})
	require.NoError(t, err)
	require.Equal(t, recordsync.SyncConfigurationRef(), loaded.Reference)
	require.Empty(t, loaded.Value.ExternalIDField)
}
