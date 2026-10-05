// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	supportrequest "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management/examples/support-request/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("JSM_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("JSM_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("JSM_EXAMPLE_MISSING", "fallback"))
}

func TestSelectionsAreOptionalAndLoadedFromTheProjectConfiguration(t *testing.T) {
	desk, err := loadSelection[supportrequest.DeskSelection](projectconfig.Configuration{}, supportrequest.DeskSelectionConfigurationRef())
	require.NoError(t, err)
	require.Equal(t, supportrequest.DeskSelection{}, desk)
	requestType, err := loadSelection[supportrequest.RequestTypeSelection](projectconfig.Configuration{}, supportrequest.RequestTypeSelectionConfigurationRef())
	require.NoError(t, err)
	require.Equal(t, supportrequest.RequestTypeSelection{}, requestType)
}
