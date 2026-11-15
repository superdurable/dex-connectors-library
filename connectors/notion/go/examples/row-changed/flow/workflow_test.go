// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rowchanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	notion "github.com/superdurable/dex-connectors-library/connectors/notion/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const sampleRowID = "2a4d6f80-1b3c-4d5e-8f70-112233445577"

func sampleTriggerEvent(kind notion.DatabaseRowChangeKind) sdkgo.TriggerEvent[notion.DatabaseRowChangedEvent] {
	return sdkgo.TriggerEvent[notion.DatabaseRowChangedEvent]{
		ID: "notion-row-0123456789abcdef0123456789abcdef",
		Payload: notion.DatabaseRowChangedEvent{
			DataSourceID: "2a4d6f80-1b3c-4d5e-8f70-112233445566", ChangeKind: kind,
			Row: notion.Page{ID: sampleRowID, Title: "Ada Lovelace"},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerRowVersion(t *testing.T) {
	event := sampleTriggerEvent(notion.DatabaseRowUpdated)
	require.Equal(t, "notion-row-change-notion-row-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryRowChange(event))
	require.Equal(t, notion.GetPageInput{PageID: sampleRowID, ShouldSkipContent: true}, MapToGetPageInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, kind := range []notion.DatabaseRowChangeKind{notion.DatabaseRowCreated, notion.DatabaseRowUpdated} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(kind))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(notion.DatabaseRowCreated))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingRow := MapToFlowInput(sampleTriggerEvent(notion.DatabaseRowCreated))
	missingRow.Change.Row.ID = ""
	require.ErrorContains(t, validateInput(missingRow), "change.row.id")
	unknownKind := MapToFlowInput(sampleTriggerEvent("trashed"))
	require.ErrorContains(t, validateInput(unknownKind), "changeKind")
}

func TestTriggerBindingShowsOneGuidedDataSourcePicker(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, notion.DatabaseRowChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, DatabaseRowChangedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1, "the picker is the only visible Trigger field")
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, notion.UIUnitDataSourcePicker, unit.UnitID)
	require.Equal(t, "Watched database", unit.Label)
	for _, guidance := range []string{
		"https://app.notion.com", "Copy link", "Manage data sources", "Copy data source ID", "Share the database",
		"data source ID and name", "does not start until it is saved", "baseline", "Restart the Worker",
	} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{
		{Port: notion.UIDataSourcePickerPortDataSourceID, JSONPointer: "/dataSourceId"},
		{Port: notion.UIDataSourcePickerPortDataSourceName, JSONPointer: "/dataSourceName"},
	}, unit.Bindings)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(notion.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordRowChangeStepType, dex.GetFinalStepType[Input](recordRowChange{}))
	require.Equal(t, completeRowChangeStepType, dex.GetFinalStepType[notion.GetPageResult](completeRowChange{}))
	wait, err := recordRowChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := notion.New(notion.Config{}, sdkgo.StaticCredentialProvider[notion.Credentials]{})
	require.NoError(t, err)
	connection, err := notion.NewConnection(client, sdkgo.ConnectionRef{Provider: "notion", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
