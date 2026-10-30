// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	triageissue "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/examples/triage-issue/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("JIRA_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("JIRA_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("JIRA_EXAMPLE_MISSING", "fallback"))
}

func TestProjectSelectionIsOptionalAndLoadedFromTheProjectConfiguration(t *testing.T) {
	selection, err := loadProjectSelection(projectconfig.Configuration{})
	require.NoError(t, err)
	require.Equal(t, triageissue.ProjectSelection{}, selection)
}
