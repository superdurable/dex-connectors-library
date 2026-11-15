// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fileintake

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func createdEvent(fileID string) sdkgo.TriggerEvent[drive.FileEvent] {
	created := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	return sdkgo.TriggerEvent[drive.FileEvent]{
		ID: fileID + "@created", OccurredAt: created,
		Payload: drive.FileEvent{FileID: fileID, Name: "Invoice.pdf", MimeType: "application/pdf", CreatedTime: created, ModifiedTime: created},
	}
}

func TestTriggerMappersStartOneFlowPerCreatedFile(t *testing.T) {
	event := createdEvent("file_invoice")
	require.Equal(t, "google-drive-file-intake-google-drive-files-file_invoice", ResolveFlowID(event))
	require.Equal(t, Input{EventID: "file_invoice@created", File: event.Payload}, MapToFlowInput(event))
	require.True(t, AcceptCreatedFile(event))
	event.ID = "file_invoice@modified@1"
	require.False(t, AcceptCreatedFile(event))
	require.Equal(t, drive.GetFileInput{FileID: "file_invoice"}, MapToGetFileInput(Input{File: drive.FileEvent{FileID: "file_invoice"}}))
}

func TestTriggerBindingExposesGuidedFolderAndTypeUnits(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.Equal(t, drive.FileCreatedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, NewFileTriggerBinding, binding.BindingName)
	units := binding.ConfigurationUI.Units
	require.Len(t, units, 2)
	require.Equal(t, drive.UIUnitFolderPicker, units[0].UnitID)
	require.Equal(t, []sdkgo.ConnectorUIBinding{
		{Port: drive.UIFolderPickerPortFolderID, JSONPointer: "/folderId"},
		{Port: drive.UIFolderPickerPortFolderName, JSONPointer: "/folderName"},
	}, units[0].Bindings)
	require.Equal(t, drive.UIUnitMimeTypePicker, units[1].UnitID)
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: drive.UIMimeTypePickerPortMimeType, JSONPointer: "/mimeType"}}, units[1].Bindings)
	for _, unit := range units {
		require.False(t, unit.Required, unit.ID)
		require.Contains(t, unit.Description, "eave blank", unit.ID)
	}
}

func TestStepIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(&Flow{}))
	require.Equal(t, recordNewFileStepType, dex.GetFinalStepType[Input](recordNewFile{}))
	require.Equal(t, completeFileIntakeStepType, dex.GetFinalStepType[drive.GetFileResult](completeFileIntake{}))
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := drive.New(drive.Config{}, sdkgo.StaticCredentialProvider[drive.Credentials]{})
	require.NoError(t, err)
	connection, err := drive.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
	otherConnection, err := drive.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection)}) })
}
