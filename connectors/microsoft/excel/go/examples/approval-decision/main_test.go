// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	approvaldecision "github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/examples/approval-decision/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
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
	configuration := projectconfig.Configuration{}

	policy, err := loadOperationConfiguration[approvaldecision.TableConfiguration](configuration, approvaldecision.PolicyTableConfigurationRef())
	require.NoError(t, err)
	require.Equal(t, approvaldecision.PolicyTableConfigurationRef(), policy.Reference)
	require.Empty(t, policy.Value.Table)

	summary, err := loadOperationConfiguration[approvaldecision.WorksheetConfiguration](configuration, approvaldecision.SummaryWorksheetConfigurationRef())
	require.NoError(t, err)
	require.Empty(t, summary.Value.Worksheet)
}
