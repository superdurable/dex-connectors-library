// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	policypublish "github.com/superdurable/dex-connectors-library/connectors/google/docs/examples/policy-publish/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("GOOGLE_DOCS_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("GOOGLE_DOCS_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("GOOGLE_DOCS_EXAMPLE_MISSING", "fallback"))
}

func TestLoadOperationConfigurationTreatsAMissingPickAsBlank(t *testing.T) {
	configuration := projectconfig.Configuration{}

	template, err := loadOperationConfiguration[policypublish.TemplateConfiguration](configuration, policypublish.TemplateConfigurationRef())
	require.NoError(t, err)
	require.Equal(t, policypublish.TemplateConfigurationRef(), template.Reference)
	require.Empty(t, template.Value.DocumentID)

	folder, err := loadOperationConfiguration[policypublish.FolderConfiguration](configuration, policypublish.DestinationFolderConfigurationRef())
	require.NoError(t, err)
	require.Empty(t, folder.Value.FolderID)
}
