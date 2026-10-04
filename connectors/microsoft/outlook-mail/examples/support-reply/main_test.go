// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	supportreply "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/examples/support-reply/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
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
	selection, err := loadArchiveFolderSelection(projectconfig.Configuration{})
	require.NoError(t, err)
	require.Empty(t, selection.FolderID, "an unsaved picker leaves the Flow on the well-known Archive folder")

	reference := supportreply.ArchiveFolderConfigurationRef()
	configuration := projectconfig.Configuration{OperationConfigurations: []projectconfig.OperationConfiguration{{
		ConnectorID: outlookmail.ConnectorID, ConnectionName: reference.ConnectionName, OperationID: reference.OperationID,
		FlowType: reference.FlowType, StepType: reference.StepType,
		Configuration: json.RawMessage(`{"folderId": "AAMkFolder-9_answered=", "folderName": "Answered"}`),
	}}}
	selection, err = loadArchiveFolderSelection(configuration)
	require.NoError(t, err)
	require.Equal(t, supportreply.ArchiveFolderSelection{FolderID: "AAMkFolder-9_answered=", FolderName: "Answered"}, selection)
}
