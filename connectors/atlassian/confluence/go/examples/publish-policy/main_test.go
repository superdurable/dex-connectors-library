// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	publishpolicy "github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence/examples/publish-policy/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("CONFLUENCE_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("CONFLUENCE_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("CONFLUENCE_EXAMPLE_MISSING", "fallback"))
}

func TestSpaceSelectionIsOptionalAndLoadedFromTheProjectConfiguration(t *testing.T) {
	selection, err := loadSpaceSelection(projectconfig.Configuration{})
	require.NoError(t, err)
	require.Equal(t, publishpolicy.SpaceSelection{}, selection)
}
