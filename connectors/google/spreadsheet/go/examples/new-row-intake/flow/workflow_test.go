// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newrowintake

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func addedRowEvent() sdkgo.TriggerEvent[spreadsheet.RowAddedEvent] {
	return sdkgo.TriggerEvent[spreadsheet.RowAddedEvent]{
		ID: "row-7-0f3a9c1d2b4e5f60", OccurredAt: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC),
		Payload: spreadsheet.RowAddedEvent{
			SpreadsheetID: "sheet-1", SheetName: "Leads", RowNumber: 7,
			Values: map[string]string{"email": "ada@example.test", "name": "Ada"}, Cells: []string{"ada@example.test", "Ada"},
		},
	}
}

func TestResolveFlowIDAndMapToFlowInputAreDeterministic(t *testing.T) {
	event := addedRowEvent()
	require.Equal(t, "google-sheets-row-row-7-0f3a9c1d2b4e5f60", ResolveFlowID(event))
	require.Equal(t, ResolveFlowID(event), ResolveFlowID(addedRowEvent()), "a redelivered row maps to the same Flow")
	require.True(t, AcceptAddedRow(event))
	require.Equal(t, AddedRow{
		EventID: event.ID, SpreadsheetID: "sheet-1", SheetName: "Leads", RowNumber: 7,
		Values: map[string]string{"email": "ada@example.test", "name": "Ada"}, DetectedAt: event.OccurredAt,
	}, MapToFlowInput(event))
}

func TestMapToGetValuesInputReadsTheReportedWholeRow(t *testing.T) {
	row := MapToFlowInput(addedRowEvent())
	require.Equal(t, spreadsheet.GetValuesInput{SpreadsheetID: "sheet-1", Range: "'Leads'!7:7"}, MapToGetValuesInput(row))
	row.SheetName = "Q4 'leads'"
	require.Equal(t, "'Q4 ''leads'''!7:7", MapToGetValuesInput(row).Range, "a quote in the tab title is doubled")
}

func intakeConnection(t *testing.T, name string) spreadsheet.Connection {
	t.Helper()
	client, err := spreadsheet.New(spreadsheet.Config{}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{})
	require.NoError(t, err)
	connection, err := spreadsheet.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: name})
	require.NoError(t, err)
	return connection
}

func TestFlowDeclaresItsRowAddedBindingWithSpreadsheetAndTabPickers(t *testing.T) {
	bindings := NewFlow(intakeConnection(t, ConnectionName)).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	require.Equal(t, NewRowTriggerBinding, bindings[0].BindingName)
	require.Equal(t, ConnectionName, bindings[0].ConnectionName)
	require.Equal(t, spreadsheet.RowAddedTriggerDefinition, bindings[0].Definition)
	units := bindings[0].ConfigurationUI.Units
	require.Len(t, units, 2, "the binding shows exactly the spreadsheet and tab fields")
	require.Equal(t, []string{spreadsheet.UIUnitSpreadsheetPicker, spreadsheet.UIUnitSheetTabPicker}, []string{units[0].UnitID, units[1].UnitID})
	for _, unit := range units {
		require.True(t, unit.Required, unit.ID)
		require.Contains(t, unit.Description, "It cannot be blank.", unit.ID)
	}
	require.Contains(t, units[0].Description, "https://docs.google.com/spreadsheets/d/")
	require.Contains(t, units[0].Description, "Google Picker")
	require.Contains(t, units[1].Description, "Row 1 must hold the column headers")
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: spreadsheet.UISpreadsheetPickerPortSpreadsheetID, JSONPointer: "/spreadsheetId"}}, units[0].Bindings)
	require.Equal(t, []sdkgo.ConnectorUIBinding{
		{Port: spreadsheet.UISheetTabPickerPortSpreadsheetID, JSONPointer: "/spreadsheetId"},
		{Port: spreadsheet.UISheetTabPickerPortTab, JSONPointer: "/sheetName"},
	}, units[1].Bindings)

	// The picker outputs decode strictly into the Trigger configuration the runner loads.
	var configuration spreadsheet.RowAddedTriggerConfiguration
	require.NoError(t, json.Unmarshal([]byte(`{"spreadsheetId":"sheet-1","sheetName":"Leads"}`), &configuration))
	require.NoError(t, configuration.Validate())
}

func TestFlowIdentitiesAndSchema(t *testing.T) {
	flow := NewFlow(intakeConnection(t, ConnectionName))
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordAddedRowStepType, dex.GetFinalStepType[AddedRow](recordAddedRow{}))
	require.Equal(t, completeRowIntakeStepType, dex.GetFinalStepType[spreadsheet.GetValuesResult](completeRowIntake{}))
	require.Equal(t, []dex.AttributeDef{addedRowAttribute}, flow.GetPersistenceSchema().Attributes)
	_, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	other := NewFlow(intakeConnection(t, "another-connection"))
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{other}) })
}
