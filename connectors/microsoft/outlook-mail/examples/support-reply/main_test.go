// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	supportreply "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/examples/support-reply/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("OUTLOOK_MAIL_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("OUTLOOK_MAIL_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("OUTLOOK_MAIL_EXAMPLE_MISSING", "fallback"))
}

func TestConnectionOptionsRouteToALocalStandInOnlyWhenSet(t *testing.T) {
	t.Setenv(localProviderURLEnvironmentVariable, "")
	require.Empty(t, connectionOptions())
	t.Setenv(localProviderURLEnvironmentVariable, "http://127.0.0.1:9")
	require.Len(t, connectionOptions(), 1)
}

func TestArchiveFolderSelectionDefaultsUntilThePickerIsSaved(t *testing.T) {
	directory := t.TempDir()
	writeJSONFile(t, filepath.Join(directory, "connections.json"), map[string]any{"schemaVersion": localconfig.SchemaVersion, "connections": []any{
		map[string]any{
			"connectorId": outlookmail.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail",
			"moduleVersion": "v0.1.0", "provider": "microsoft", "connectionName": supportreply.ConnectionName,
			"configuration": map[string]any{}, "credentials": map[string]any{"auth_method": outlookmail.MicrosoftOAuthAuthMethodID},
		},
	}})
	store, err := localconfig.LoadFile(filepath.Join(directory, "connections.json"))
	require.NoError(t, err)
	selection, err := loadArchiveFolderSelection(store)
	require.NoError(t, err)
	require.Empty(t, selection.FolderID, "an unsaved picker leaves the Flow on the well-known Archive folder")

	reference := supportreply.ArchiveFolderConfigurationRef()
	writeJSONFile(t, store.UseConfigurationsPath(), map[string]any{
		"schemaVersion": localconfig.UseConfigurationsSchemaVersion,
		"operationConfigurations": []any{map[string]any{
			"connectorId": outlookmail.ConnectorID, "connectionName": reference.ConnectionName, "operationId": reference.OperationID,
			"flowType": reference.FlowType, "stepType": reference.StepType,
			"configuration": map[string]any{"folderId": "AAMkFolder-9_answered=", "folderName": "Answered"},
		}},
	})
	store, err = localconfig.LoadFile(filepath.Join(directory, "connections.json"))
	require.NoError(t, err)
	selection, err = loadArchiveFolderSelection(store)
	require.NoError(t, err)
	require.Equal(t, supportreply.ArchiveFolderSelection{FolderID: "AAMkFolder-9_answered=", FolderName: "Answered"}, selection)
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	contents, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
}
