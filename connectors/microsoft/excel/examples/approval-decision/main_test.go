// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	approvaldecision "github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/examples/approval-decision/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("MICROSOFT_EXCEL_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("MICROSOFT_EXCEL_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("MICROSOFT_EXCEL_EXAMPLE_MISSING", "fallback"))
}

func TestConnectionOptionsRedirectOnlyWhenTheLocalVariableIsSet(t *testing.T) {
	t.Setenv(localProviderURLEnvironmentVariable, "")
	require.Empty(t, connectionOptions())
	t.Setenv(localProviderURLEnvironmentVariable, "http://127.0.0.1:9")
	require.Len(t, connectionOptions(), 1)
}

func TestLoadOperationConfigurationTreatsAMissingPickAsBlank(t *testing.T) {
	connectionsPath := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(connectionsPath, []byte(`{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[]}`), 0o600))
	store, err := localconfig.LoadFile(connectionsPath)
	require.NoError(t, err)

	policy, err := loadOperationConfiguration[approvaldecision.TableConfiguration](store, approvaldecision.PolicyTableConfigurationRef())
	require.NoError(t, err)
	require.Equal(t, approvaldecision.PolicyTableConfigurationRef(), policy.Reference)
	require.Empty(t, policy.Value.Table)

	summary, err := loadOperationConfiguration[approvaldecision.WorksheetConfiguration](store, approvaldecision.SummaryWorksheetConfigurationRef())
	require.NoError(t, err)
	require.Empty(t, summary.Value.Worksheet)
}
