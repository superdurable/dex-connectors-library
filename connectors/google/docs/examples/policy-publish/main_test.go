// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	policypublish "github.com/superdurable/dex-connectors-library/connectors/google/docs/examples/policy-publish/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("GOOGLE_DOCS_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("GOOGLE_DOCS_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("GOOGLE_DOCS_EXAMPLE_MISSING", "fallback"))
}

func TestLoadOperationConfigurationTreatsAMissingPickAsBlank(t *testing.T) {
	directory := t.TempDir()
	connectionsPath := filepath.Join(directory, "connections.json")
	require.NoError(t, os.WriteFile(connectionsPath, []byte(`{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[]}`), 0o600))
	store, err := localconfig.LoadFile(connectionsPath)
	require.NoError(t, err)

	template, err := loadOperationConfiguration[policypublish.TemplateConfiguration](store, policypublish.TemplateConfigurationRef())
	require.NoError(t, err)
	require.Equal(t, policypublish.TemplateConfigurationRef(), template.Reference)
	require.Empty(t, template.Value.DocumentID)

	folder, err := loadOperationConfiguration[policypublish.FolderConfiguration](store, policypublish.DestinationFolderConfigurationRef())
	require.NoError(t, err)
	require.Empty(t, folder.Value.FolderID)
}
