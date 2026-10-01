// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	publishpolicy "github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence/examples/publish-policy/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("CONFLUENCE_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("CONFLUENCE_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("CONFLUENCE_EXAMPLE_MISSING", "fallback"))
}

func TestSpaceSelectionIsOptionalAndLoadedFromTheSidecar(t *testing.T) {
	directory := t.TempDir()
	connectionsPath := filepath.Join(directory, "connections.json")
	require.NoError(t, os.WriteFile(connectionsPath, []byte(`{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[]}`), 0o600))
	store, err := localconfig.LoadFile(connectionsPath)
	require.NoError(t, err)
	selection, err := loadSpaceSelection(store)
	require.NoError(t, err)
	require.Equal(t, publishpolicy.SpaceSelection{}, selection)
}
